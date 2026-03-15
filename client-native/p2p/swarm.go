package p2p

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
)

const (
	swarmPieceSize    = 256 * 1024       // 256KB pieces
	swarmChunkSize    = 64 * 1024        // 64KB sub-chunky přes DataChannel
	swarmMaxRetries   = 3
	swarmPieceTimeout = 30 * time.Second // timeout na stuck piece
	swarmMaxBuffered  = 512 * 1024       // flow control: max buffered bytes
)

// Piece stavy
const (
	PiecePending     = 0
	PieceDownloading = 1
	PieceDone        = 2
	PieceError       = 3
)

// SwarmDownload stavy
const (
	SwarmIdle        = 0
	SwarmDownloading = 1
	SwarmDone        = 2
	SwarmError       = 3
)

type PieceState struct {
	Index     int
	Offset    int64
	Size      int
	Status    int
	Retries   int
	PeerID    string // kdo stahuje
	Data      []byte // přijatá data (akumulace sub-chunků)
	StartedAt time.Time
}

type SwarmPeerConn struct {
	PeerID     string
	PC         *webrtc.PeerConnection
	DC         *webrtc.DataChannel
	Busy       bool
	RemoteSet  bool
	PendingICE []webrtc.ICECandidateInit
}

// SeederConn — PeerConnection vytvořený seederem pro odesílání piece
type SeederConn struct {
	PC         *webrtc.PeerConnection
	DC         *webrtc.DataChannel
	RemoteSet  bool
	PendingICE []webrtc.ICECandidateInit
}

// SwarmDownload — jeden multi-source download
type SwarmDownload struct {
	mu          sync.Mutex
	TransferID  string
	ShareID     string
	FileID      string
	FileName    string
	FileSize    int64
	PieceSize   int
	TotalPieces int
	Pieces      []PieceState
	Peers       map[string]*SwarmPeerConn
	File        *os.File
	SavePath    string
	Status      int
	Done        int // počet hotových pieces
	ActivePeers int
	StartTime   time.Time
	stopCh      chan struct{}
}

// SwarmSeedFile — soubor nabídnutý ke swarm seedingu
type SwarmSeedFile struct {
	ShareID  string
	FileID   string
	FilePath string
	FileName string
	FileSize int64
}

// SwarmManager spravuje swarm downloads a seeding
type SwarmManager struct {
	mu         sync.Mutex
	userID     string
	stunURL    string
	sendWS     SendWSFunc
	invalidate func()
	closed     bool

	downloads   map[string]*SwarmDownload // transferID → download
	seedFiles   map[string]*SwarmSeedFile // fileID → seeded file
	seederConns map[string]*SeederConn    // "transferID:peerID:pieceIdx" → seeder PC
}

func NewSwarmManager(userID, stunURL string, sendWS SendWSFunc, invalidate func()) *SwarmManager {
	return &SwarmManager{
		userID:      userID,
		stunURL:     stunURL,
		sendWS:      sendWS,
		invalidate:  invalidate,
		downloads:   make(map[string]*SwarmDownload),
		seedFiles:   make(map[string]*SwarmSeedFile),
		seederConns: make(map[string]*SeederConn),
	}
}

func seederConnKey(transferID, peerID string, pieceIdx int) string {
	return fmt.Sprintf("%s:%s:%d", transferID, peerID, pieceIdx)
}

// Close — zavře všechny downloady, seeder konekce a uvolní resources
func (m *SwarmManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true

	for _, dl := range m.downloads {
		dl.mu.Lock()
		if dl.Status == SwarmDownloading {
			dl.Status = SwarmError
			select {
			case <-dl.stopCh:
			default:
				close(dl.stopCh)
			}
		}
		if dl.File != nil {
			dl.File.Close()
			dl.File = nil
		}
		for _, peer := range dl.Peers {
			if peer.PC != nil {
				peer.PC.Close()
				peer.PC = nil
			}
		}
		dl.mu.Unlock()
	}
	m.downloads = make(map[string]*SwarmDownload)

	for key, sc := range m.seederConns {
		if sc.PC != nil {
			sc.PC.Close()
		}
		delete(m.seederConns, key)
	}
}

// RegisterSeedFile — zaregistruje lokální soubor pro swarm seeding
func (m *SwarmManager) RegisterSeedFile(shareID, fileID, filePath, fileName string, fileSize int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seedFiles[fileID] = &SwarmSeedFile{
		ShareID:  shareID,
		FileID:   fileID,
		FilePath: filePath,
		FileName: fileName,
		FileSize: fileSize,
	}
}

// UnregisterSeedFile — odregistruje soubor ze seedingu
func (m *SwarmManager) UnregisterSeedFile(fileID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.seedFiles, fileID)
}

// StartDownload — zahájí multi-source download
func (m *SwarmManager) StartDownload(transferID, shareID, fileID, fileName, savePath string, fileSize int64, pieceSize, totalPieces int, sources []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return fmt.Errorf("swarm manager closed")
	}
	if _, exists := m.downloads[transferID]; exists {
		return fmt.Errorf("download already exists")
	}

	f, err := os.Create(savePath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}

	// Pre-allocate soubor
	if fileSize > 0 {
		if err := f.Truncate(fileSize); err != nil {
			f.Close()
			return fmt.Errorf("truncate file: %w", err)
		}
	}

	pieces := make([]PieceState, totalPieces)
	remaining := fileSize
	for i := 0; i < totalPieces; i++ {
		size := int64(pieceSize)
		if remaining < size {
			size = remaining
		}
		pieces[i] = PieceState{
			Index:  i,
			Offset: int64(i) * int64(pieceSize),
			Size:   int(size),
			Status: PiecePending,
		}
		remaining -= size
	}

	stopCh := make(chan struct{})
	dl := &SwarmDownload{
		TransferID:  transferID,
		ShareID:     shareID,
		FileID:      fileID,
		FileName:    fileName,
		FileSize:    fileSize,
		PieceSize:   pieceSize,
		TotalPieces: totalPieces,
		Pieces:      pieces,
		Peers:       make(map[string]*SwarmPeerConn),
		File:        f,
		SavePath:    savePath,
		Status:      SwarmDownloading,
		StartTime:   time.Now(),
		stopCh:      stopCh,
	}

	m.downloads[transferID] = dl

	// Scheduler goroutine
	go m.runScheduler(transferID, sources, stopCh)

	return nil
}

// runScheduler — round-robin scheduler, přiděluje pending pieces volným peerům
func (m *SwarmManager) runScheduler(transferID string, sources []string, stopCh <-chan struct{}) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
		}

		m.mu.Lock()
		dl, ok := m.downloads[transferID]
		m.mu.Unlock()
		if !ok {
			return
		}

		dl.mu.Lock()
		if dl.Status != SwarmDownloading {
			dl.mu.Unlock()
			return
		}

		// Timeout na stuck pieces
		now := time.Now()
		for i := range dl.Pieces {
			p := &dl.Pieces[i]
			if p.Status == PieceDownloading && !p.StartedAt.IsZero() && now.Sub(p.StartedAt) > swarmPieceTimeout {
				log.Printf("swarm: piece %d timed out (peer %s), retrying", i, p.PeerID)
				if peer := dl.Peers[p.PeerID]; peer != nil {
					peer.Busy = false
					if peer.PC != nil {
						peer.PC.Close()
						peer.PC = nil
					}
				}
				if dl.ActivePeers > 0 {
					dl.ActivePeers--
				}
				p.Retries++
				if p.Retries >= swarmMaxRetries {
					p.Status = PieceError
				} else {
					p.Status = PiecePending
				}
				p.Data = nil
				p.PeerID = ""
			}
		}

		// Zkontrolovat jestli jsou všechny pieces hotové nebo failed
		allDoneOrFailed := true
		hasError := false
		for i := range dl.Pieces {
			if dl.Pieces[i].Status == PiecePending || dl.Pieces[i].Status == PieceDownloading {
				allDoneOrFailed = false
			}
			if dl.Pieces[i].Status == PieceError {
				hasError = true
			}
		}
		if allDoneOrFailed && hasError {
			dl.Status = SwarmError
			if dl.File != nil {
				dl.File.Close()
				dl.File = nil
			}
			log.Printf("swarm: download failed: %s (pieces exceeded max retries)", dl.FileName)
			dl.mu.Unlock()
			if m.invalidate != nil {
				m.invalidate()
			}
			return
		}

		// Najít volné peery a přidělit pending pieces
		for _, peerID := range sources {
			peer := dl.Peers[peerID]
			if peer != nil && peer.Busy {
				continue
			}

			// Najít pending piece
			pieceIdx := -1
			for i := range dl.Pieces {
				if dl.Pieces[i].Status == PiecePending && dl.Pieces[i].Retries < swarmMaxRetries {
					pieceIdx = i
					break
				}
			}
			if pieceIdx < 0 {
				break // žádné pending pieces
			}

			dl.Pieces[pieceIdx].Status = PieceDownloading
			dl.Pieces[pieceIdx].PeerID = peerID
			dl.Pieces[pieceIdx].Data = nil
			dl.Pieces[pieceIdx].StartedAt = time.Now()

			if peer == nil {
				dl.Peers[peerID] = &SwarmPeerConn{PeerID: peerID, Busy: true}
			} else {
				peer.Busy = true
			}
			dl.ActivePeers++

			// C2 fix: poslat file_id a share_id v piece requestu
			go m.requestPiece(transferID, dl.ShareID, dl.FileID, peerID, pieceIdx, dl.Pieces[pieceIdx].Offset, dl.Pieces[pieceIdx].Size)
		}
		dl.mu.Unlock()
	}
}

// requestPiece — pošle swarm.piece_request peerovi (přes WS relay)
func (m *SwarmManager) requestPiece(transferID, shareID, fileID, peerID string, pieceIdx int, offset int64, size int) {
	m.sendWS("swarm.piece_request", map[string]any{
		"to":           peerID,
		"transfer_id":  transferID,
		"directory_id": shareID,
		"file_id":      fileID,
		"piece_index":  pieceIdx,
		"offset":       offset,
		"size":         size,
	})
}

// HandlePieceRequest — seeder obdržel požadavek na piece, pošle offer
func (m *SwarmManager) HandlePieceRequest(from string, payload json.RawMessage) {
	var req struct {
		TransferID string `json:"transfer_id"`
		PieceIndex int    `json:"piece_index"`
		Offset     int64  `json:"offset"`
		Size       int    `json:"size"`
		FileID     string `json:"file_id"`
		ShareID    string `json:"directory_id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		log.Printf("swarm: piece_request unmarshal: %v", err)
		return
	}

	m.mu.Lock()
	seed, ok := m.seedFiles[req.FileID]
	if !ok {
		m.mu.Unlock()
		log.Printf("swarm: no seed for file %s", req.FileID)
		return
	}

	// C9 fix: validovat offset a size proti skutečné velikosti souboru
	if req.Offset < 0 || req.Size <= 0 || req.Offset+int64(req.Size) > seed.FileSize {
		m.mu.Unlock()
		log.Printf("swarm: invalid offset/size: offset=%d size=%d fileSize=%d", req.Offset, req.Size, seed.FileSize)
		return
	}
	filePath := seed.FilePath
	m.mu.Unlock()

	go m.sendPiece(from, req.TransferID, req.PieceIndex, req.Offset, req.Size, filePath)
}

// sendPiece — otevře soubor, vytvoří WebRTC DC a pošle piece data
func (m *SwarmManager) sendPiece(to, transferID string, pieceIdx int, offset int64, size int, filePath string) {
	stunURL := m.stunURL
	if stunURL == "" {
		stunURL = "stun:stun.l.google.com:19302"
	}

	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{{URLs: []string{stunURL}}},
	}

	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		log.Printf("swarm: peer conn: %v", err)
		return
	}

	dc, err := pc.CreateDataChannel(fmt.Sprintf("piece_%d", pieceIdx), nil)
	if err != nil {
		pc.Close()
		log.Printf("swarm: data channel: %v", err)
		return
	}

	// C1+C3 fix: uložit seeder connection pro answer/ICE
	key := seederConnKey(transferID, to, pieceIdx)
	m.mu.Lock()
	m.seederConns[key] = &SeederConn{PC: pc, DC: dc}
	m.mu.Unlock()

	cleanup := func() {
		m.mu.Lock()
		delete(m.seederConns, key)
		m.mu.Unlock()
	}

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		candidateJSON := c.ToJSON()
		b, _ := json.Marshal(candidateJSON)
		m.sendWS("swarm.piece_ice", map[string]any{
			"to":          to,
			"transfer_id": transferID,
			"piece_index": pieceIdx,
			"candidate":   json.RawMessage(b),
		})
	})

	dc.OnOpen(func() {
		defer func() {
			time.Sleep(500 * time.Millisecond)
			dc.Close()
			pc.Close()
			cleanup()
		}()

		f, err := os.Open(filePath)
		if err != nil {
			log.Printf("swarm: open file: %v", err)
			return
		}
		defer f.Close()

		// C10 fix: kontrola Seek chyby
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			log.Printf("swarm: seek error: %v", err)
			return
		}

		// C12 fix: flow control (SCTP backpressure)
		sendMore := make(chan struct{}, 1)
		dc.SetBufferedAmountLowThreshold(swarmMaxBuffered / 2)
		dc.OnBufferedAmountLow(func() {
			select {
			case sendMore <- struct{}{}:
			default:
			}
		})

		buf := make([]byte, swarmChunkSize)
		sent := 0
		for sent < size {
			toRead := swarmChunkSize
			if sent+toRead > size {
				toRead = size - sent
			}
			n, err := f.Read(buf[:toRead])
			if err != nil {
				break
			}
			if err := dc.Send(buf[:n]); err != nil {
				break
			}
			sent += n

			// Backpressure: čekat pokud buffer přeteče
			if dc.BufferedAmount() > swarmMaxBuffered {
				<-sendMore
			}
		}

		// Signalizace dokončení — prázdný message
		dc.Send([]byte{})
	})

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		pc.Close()
		cleanup()
		return
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		pc.Close()
		cleanup()
		return
	}

	m.sendWS("swarm.piece_offer", map[string]any{
		"to":          to,
		"transfer_id": transferID,
		"piece_index": pieceIdx,
		"sdp":         offer.SDP,
	})
}

// HandlePieceOffer — downloader obdržel SDP offer od seedera
func (m *SwarmManager) HandlePieceOffer(from string, payload json.RawMessage) {
	var msg struct {
		TransferID string `json:"transfer_id"`
		PieceIndex int    `json:"piece_index"`
		SDP        string `json:"sdp"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		return
	}

	m.mu.Lock()
	dl, ok := m.downloads[msg.TransferID]
	m.mu.Unlock()
	if !ok {
		return
	}

	stunURL := m.stunURL
	if stunURL == "" {
		stunURL = "stun:stun.l.google.com:19302"
	}

	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{{URLs: []string{stunURL}}},
	}

	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		return
	}

	dl.mu.Lock()
	peer := dl.Peers[from]
	if peer == nil {
		peer = &SwarmPeerConn{PeerID: from, Busy: true}
		dl.Peers[from] = peer
	}
	peer.PC = pc
	pieceIdx := msg.PieceIndex

	// C7 fix: zjistit expected size pro validaci
	expectedSize := 0
	if pieceIdx >= 0 && pieceIdx < len(dl.Pieces) {
		dl.Pieces[pieceIdx].Data = nil
		expectedSize = dl.Pieces[pieceIdx].Size
	}
	dl.mu.Unlock()

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		candidateJSON := c.ToJSON()
		b, _ := json.Marshal(candidateJSON)
		m.sendWS("swarm.piece_ice", map[string]any{
			"to":          from,
			"transfer_id": msg.TransferID,
			"piece_index": pieceIdx,
			"candidate":   json.RawMessage(b),
		})
	})

	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		dc.OnMessage(func(dcMsg webrtc.DataChannelMessage) {
			if len(dcMsg.Data) == 0 {
				// Piece dokončen
				m.onPieceDone(dl, pieceIdx, from)
				dc.Close()
				pc.Close()
				return
			}

			dl.mu.Lock()
			if pieceIdx >= 0 && pieceIdx < len(dl.Pieces) {
				piece := &dl.Pieces[pieceIdx]
				// C7 fix: validace velikosti — nepřekročit expected size
				if len(piece.Data)+len(dcMsg.Data) > expectedSize {
					log.Printf("swarm: piece %d data exceeds expected size (%d > %d)", pieceIdx, len(piece.Data)+len(dcMsg.Data), expectedSize)
					dl.mu.Unlock()
					dc.Close()
					pc.Close()
					return
				}
				piece.Data = append(piece.Data, dcMsg.Data...)
			}
			dl.mu.Unlock()

			if m.invalidate != nil {
				m.invalidate()
			}
		})
	})

	err = pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  msg.SDP,
	})
	if err != nil {
		// C17 fix: cleanup peer state on error
		dl.mu.Lock()
		peer.PC = nil
		peer.Busy = false
		dl.mu.Unlock()
		pc.Close()
		return
	}

	dl.mu.Lock()
	peer.RemoteSet = true
	for _, ice := range peer.PendingICE {
		pc.AddICECandidate(ice)
	}
	peer.PendingICE = nil
	dl.mu.Unlock()

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		dl.mu.Lock()
		peer.PC = nil
		peer.Busy = false
		dl.mu.Unlock()
		pc.Close()
		return
	}
	if err := pc.SetLocalDescription(answer); err != nil {
		dl.mu.Lock()
		peer.PC = nil
		peer.Busy = false
		dl.mu.Unlock()
		pc.Close()
		return
	}

	m.sendWS("swarm.piece_accept", map[string]any{
		"to":          from,
		"transfer_id": msg.TransferID,
		"piece_index": pieceIdx,
		"sdp":         answer.SDP,
	})
}

// C1 fix: HandlePieceAccept — seeder obdržel SDP answer od downloadera
func (m *SwarmManager) HandlePieceAccept(from string, payload json.RawMessage) {
	var msg struct {
		TransferID string `json:"transfer_id"`
		PieceIndex int    `json:"piece_index"`
		SDP        string `json:"sdp"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		return
	}

	key := seederConnKey(msg.TransferID, from, msg.PieceIndex)
	m.mu.Lock()
	sc, ok := m.seederConns[key]
	m.mu.Unlock()
	if !ok || sc.PC == nil {
		log.Printf("swarm: no seeder conn for accept (key=%s)", key)
		return
	}

	err := sc.PC.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  msg.SDP,
	})
	if err != nil {
		log.Printf("swarm: seeder set remote desc: %v", err)
		sc.PC.Close()
		m.mu.Lock()
		delete(m.seederConns, key)
		m.mu.Unlock()
		return
	}

	m.mu.Lock()
	sc.RemoteSet = true
	for _, ice := range sc.PendingICE {
		sc.PC.AddICECandidate(ice)
	}
	sc.PendingICE = nil
	m.mu.Unlock()
}

// C3 fix: HandlePieceIce — ICE candidate relay (pro downloader i seeder)
func (m *SwarmManager) HandlePieceIce(from string, payload json.RawMessage) {
	var msg struct {
		TransferID string                  `json:"transfer_id"`
		PieceIndex int                     `json:"piece_index"`
		Candidate  webrtc.ICECandidateInit `json:"candidate"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		return
	}

	// Zkusit downloader peers
	m.mu.Lock()
	dl, dlOK := m.downloads[msg.TransferID]
	m.mu.Unlock()

	if dlOK {
		dl.mu.Lock()
		peer := dl.Peers[from]
		if peer == nil {
			peer = &SwarmPeerConn{PeerID: from}
			dl.Peers[from] = peer
		}
		if peer.PC != nil && peer.RemoteSet {
			peer.PC.AddICECandidate(msg.Candidate)
		} else {
			peer.PendingICE = append(peer.PendingICE, msg.Candidate)
		}
		dl.mu.Unlock()
		return
	}

	// Zkusit seeder connections
	key := seederConnKey(msg.TransferID, from, msg.PieceIndex)
	m.mu.Lock()
	sc, scOK := m.seederConns[key]
	if scOK {
		if sc.PC != nil && sc.RemoteSet {
			sc.PC.AddICECandidate(msg.Candidate)
		} else {
			sc.PendingICE = append(sc.PendingICE, msg.Candidate)
		}
	}
	m.mu.Unlock()
}

// onPieceDone — piece stažen, zapsat na disk
func (m *SwarmManager) onPieceDone(dl *SwarmDownload, pieceIdx int, peerID string) {
	dl.mu.Lock()
	defer dl.mu.Unlock()

	if pieceIdx >= len(dl.Pieces) {
		return
	}
	piece := &dl.Pieces[pieceIdx]
	if piece.Status == PieceDone {
		return
	}

	// Validovat velikost dat
	if len(piece.Data) != piece.Size {
		log.Printf("swarm: piece %d size mismatch: got %d, expected %d", pieceIdx, len(piece.Data), piece.Size)
		piece.Retries++
		if piece.Retries >= swarmMaxRetries {
			piece.Status = PieceError
		} else {
			piece.Status = PiecePending
		}
		piece.Data = nil
		piece.PeerID = ""
		if peer := dl.Peers[peerID]; peer != nil {
			peer.Busy = false
			if peer.PC != nil {
				peer.PC.Close()
				peer.PC = nil
			}
		}
		if dl.ActivePeers > 0 {
			dl.ActivePeers--
		}
		return
	}

	// C15 fix: kontrola WriteAt chyby
	if dl.File != nil && len(piece.Data) > 0 {
		if _, err := dl.File.WriteAt(piece.Data, piece.Offset); err != nil {
			log.Printf("swarm: WriteAt piece %d error: %v", pieceIdx, err)
			dl.Status = SwarmError
			dl.File.Close()
			dl.File = nil
			if m.invalidate != nil {
				m.invalidate()
			}
			return
		}
	}

	piece.Status = PieceDone
	piece.Data = nil
	dl.Done++

	// Uvolnit peera
	if peer := dl.Peers[peerID]; peer != nil {
		peer.Busy = false
		if peer.PC != nil {
			peer.PC.Close()
			peer.PC = nil
		}
	}
	// C11 fix: clamp ActivePeers
	if dl.ActivePeers > 0 {
		dl.ActivePeers--
	}

	// Zkontrolovat dokončení
	if dl.Done >= dl.TotalPieces {
		dl.Status = SwarmDone
		// C16 fix: kontrola Close chyby
		if dl.File != nil {
			if err := dl.File.Close(); err != nil {
				log.Printf("swarm: file close error: %v", err)
			}
			dl.File = nil
		}
		log.Printf("swarm: download complete: %s (%d pieces)", dl.FileName, dl.TotalPieces)
	}

	if m.invalidate != nil {
		m.invalidate()
	}
}

// GetDownload vrátí download info
func (m *SwarmManager) GetDownload(transferID string) *SwarmDownload {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.downloads[transferID]
}

// IsDownloaded — je download hotový?
func (m *SwarmManager) IsDownloaded(transferID string) bool {
	m.mu.Lock()
	dl := m.downloads[transferID]
	m.mu.Unlock()
	if dl == nil {
		return false
	}
	dl.mu.Lock()
	defer dl.mu.Unlock()
	return dl.Status == SwarmDone
}

// IsFailed — selhal download?
func (m *SwarmManager) IsFailed(transferID string) bool {
	m.mu.Lock()
	dl := m.downloads[transferID]
	m.mu.Unlock()
	if dl == nil {
		return false
	}
	dl.mu.Lock()
	defer dl.mu.Unlock()
	return dl.Status == SwarmError
}

// GetProgress vrátí progres (0.0 - 1.0)
func (m *SwarmManager) GetProgress(transferID string) (float64, int, int) {
	m.mu.Lock()
	dl := m.downloads[transferID]
	m.mu.Unlock()
	if dl == nil {
		return 0, 0, 0
	}
	dl.mu.Lock()
	defer dl.mu.Unlock()
	if dl.TotalPieces == 0 {
		return 0, 0, 0
	}
	return float64(dl.Done) / float64(dl.TotalPieces), dl.Done, dl.ActivePeers
}

// ActiveDownloads vrátí aktivní downloady
func (m *SwarmManager) ActiveDownloads() []*SwarmDownload {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*SwarmDownload
	for _, dl := range m.downloads {
		dl.mu.Lock()
		if dl.Status == SwarmDownloading {
			result = append(result, dl)
		}
		dl.mu.Unlock()
	}
	return result
}

// Cleanup — vyčistí hotové/chybné downloady
func (m *SwarmManager) Cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, dl := range m.downloads {
		dl.mu.Lock()
		done := dl.Status == SwarmDone || dl.Status == SwarmError
		dl.mu.Unlock()
		if done {
			delete(m.downloads, id)
		}
	}
}
