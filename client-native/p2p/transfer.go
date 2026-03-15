package p2p

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

const chunkSize = 64 * 1024 // 64KB per data channel message

// Směr přenosu
const (
	DirSend    = 0
	DirReceive = 1
)

// Stavy přenosu
const (
	StatusWaiting      = 0
	StatusConnecting   = 1
	StatusTransferring = 2
	StatusDone         = 3
	StatusError        = 4
	StatusCancelled    = 5
)

// SendWSFunc — callback pro odeslání WS eventu.
type SendWSFunc func(eventType string, payload any) error

// RegisteredFile — soubor nabídnutý ke stažení přes kanálovou zprávu.
type RegisteredFile struct {
	TransferID  string
	FilePath    string
	FileName    string
	FileSize    int64
	IsTemp      bool // dočasný soubor (ZIP z temp adresáře) — nabídnout smazání při unshare
	IsTransient bool // dočasná registrace (share transfer/upload) — nezobrazovat v UI
}

// Transfer — jeden P2P přenos souboru.
type Transfer struct {
	ID        string
	PeerID    string
	FileName  string
	FileSize  int64
	Direction int
	Status    int
	Progress  int64
	Error     string

	mu         sync.Mutex
	pc         *webrtc.PeerConnection
	dc         *webrtc.DataChannel
	pendingICE []webrtc.ICECandidateInit // ICE kandidáti bufferovaní před remote description
	remoteSet  bool                      // remote description nastavena (ICE lze přidávat)
	file       *os.File                  // příjemce: otevřený soubor (pro cleanup)
	sendWS     SendWSFunc
	stunURL    string

	filePath    string    // odesílatel: cesta ke zdrojovému souboru
	savePath    string    // příjemce: kam uložit
	baseID      string    // původní transferID (bez composit suffixu, pro WS eventy)
	Offset      int64     // resume offset (kolik bytů už příjemce má)
	StartTime   time.Time // čas zahájení transferu (pro rychlost/ETA)

	offerSDP string // uložený SDP pro deferred accept

	onProgress   func()
	onDone       func()
	onMarkDone   func(string)                          // callback pro označení transferu jako staženého (transferID)
	onMarkSent   func(string)                          // callback pro označení transferu jako odeslaného (sender-side)
	onAutoRetry  func(peerID, transferID, savePath string, offset int64) // auto-retry při selhání
	onZipStart   func(savePath string)                  // callback: .zip transfer zahájen
	retries      int
}

// Manager — spravuje všechny P2P přenosy a registrované soubory.
type Manager struct {
	mu         sync.Mutex
	userID     string
	stunURL    string
	sendWS     SendWSFunc
	invalidate func()

	transfers       map[string]*Transfer      // transferID → Transfer
	registeredFiles map[string]*RegisteredFile // transferID → RegisteredFile
	downloadedIDs   map[string]bool            // transferIDs úspěšně stažené
	unavailableIDs  map[string]bool            // transferIDs nedostupné (rejected)
	sentIDs         map[string]bool            // transferIDs úspěšně odeslané (sender-side)

	// Callback: příchozí cold offer (přímý P2P bez kanálu) → UI se zeptá uživatele
	onOffer func(t *Transfer)
	// Callback: .zip transfer zahájen (StatusTransferring) → UI zobrazí dialog
	onZipStart func(savePath string)
	// Callback: .zip soubor úspěšně stažen → UI nabídne extrakci
	onZipDone func(savePath string)
}

// sharesFilePath vrátí cestu k souboru s persistovanými sdílenými soubory.
func sharesFilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".nora", "p2p_shares.json")
}

// persistedShare — JSON formát pro persistenci registrovaného souboru.
type persistedShare struct {
	TransferID string `json:"transfer_id"`
	FilePath   string `json:"file_path"`
	FileName   string `json:"file_name"`
	FileSize   int64  `json:"file_size"`
	IsTemp     bool   `json:"is_temp,omitempty"`
}

// loadShares načte registrované soubory z disku. Validuje existenci souborů.
func loadShares() map[string]*RegisteredFile {
	result := make(map[string]*RegisteredFile)
	data, err := os.ReadFile(sharesFilePath())
	if err != nil {
		return result
	}
	var shares []persistedShare
	if err := json.Unmarshal(data, &shares); err != nil {
		return result
	}
	for _, s := range shares {
		// Ověřit, že soubor stále existuje
		if _, err := os.Stat(s.FilePath); err != nil {
			continue
		}
		result[s.TransferID] = &RegisteredFile{
			TransferID: s.TransferID,
			FilePath:   s.FilePath,
			FileName:   s.FileName,
			FileSize:   s.FileSize,
			IsTemp:     s.IsTemp,
		}
	}
	return result
}

// saveShares uloží registrované soubory na disk.
func (m *Manager) saveShares() {
	shares := make([]persistedShare, 0, len(m.registeredFiles))
	for _, rf := range m.registeredFiles {
		shares = append(shares, persistedShare{
			TransferID: rf.TransferID,
			FilePath:   rf.FilePath,
			FileName:   rf.FileName,
			FileSize:   rf.FileSize,
			IsTemp:     rf.IsTemp,
		})
	}
	data, err := json.Marshal(shares)
	if err != nil {
		log.Printf("p2p: save shares: %v", err)
		return
	}
	if err := os.WriteFile(sharesFilePath(), data, 0600); err != nil {
		log.Printf("p2p: save shares: %v", err)
	}
}

// NewManager vytvoří P2P Manager. Načte persistované sdílené soubory z disku.
func NewManager(userID, stunURL string, sendWS SendWSFunc, invalidate func()) *Manager {
	return &Manager{
		userID:          userID,
		stunURL:         stunURL,
		sendWS:          sendWS,
		invalidate:      invalidate,
		transfers:       make(map[string]*Transfer),
		registeredFiles: loadShares(),
		downloadedIDs:   make(map[string]bool),
		unavailableIDs:  make(map[string]bool),
		sentIDs:         make(map[string]bool),
	}
}

// SetOnOffer nastaví callback pro příchozí cold offer (přímé nabídky souborů).
func (m *Manager) SetOnOffer(fn func(t *Transfer)) {
	m.mu.Lock()
	m.onOffer = fn
	m.mu.Unlock()
}

// SetOnZipStart nastaví callback volaný při zahájení přenosu .zip (StatusTransferring).
func (m *Manager) SetOnZipStart(fn func(savePath string)) {
	m.mu.Lock()
	m.onZipStart = fn
	m.mu.Unlock()
}

// SetOnZipDone nastaví callback volaný po úspěšném stažení .zip souboru.
func (m *Manager) SetOnZipDone(fn func(savePath string)) {
	m.mu.Lock()
	m.onZipDone = fn
	m.mu.Unlock()
}

// IsDownloaded vrátí true pokud byl transfer úspěšně stažen.
func (m *Manager) IsDownloaded(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.downloadedIDs[id]
}

// IsUnavailable vrátí true pokud je transfer nedostupný (rejected).
func (m *Manager) IsUnavailable(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.unavailableIDs[id]
}

// MarkDownloaded označí transfer jako úspěšně stažený.
// Pokud je to DirReceive a soubor je .zip, zavolá onZipDone callback.
func (m *Manager) MarkDownloaded(id string) {
	m.mu.Lock()
	m.downloadedIDs[id] = true
	t := m.transfers[id]
	fn := m.onZipDone
	m.mu.Unlock()

	if fn != nil && t != nil && t.Direction == DirReceive && t.savePath != "" {
		if strings.HasSuffix(strings.ToLower(t.savePath), ".zip") {
			fn(t.savePath)
		}
	}
}

// IsTransferSent vrátí true pokud byl transfer úspěšně odeslán (sender-side).
func (m *Manager) IsTransferSent(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sentIDs[id]
}

// MarkSent označí transfer jako úspěšně odeslaný (sender-side).
func (m *Manager) MarkSent(id string) {
	m.mu.Lock()
	m.sentIDs[id] = true
	m.mu.Unlock()
}

// MarkUnavailable označí transfer jako nedostupný.
func (m *Manager) MarkUnavailable(id string) {
	m.mu.Lock()
	m.unavailableIDs[id] = true
	m.mu.Unlock()
}

// DismissTransfer smaže transfer z aktivních transferů (dismiss z UI).
func (m *Manager) DismissTransfer(id string) {
	m.mu.Lock()
	if t, ok := m.transfers[id]; ok {
		delete(m.transfers, id)
		m.mu.Unlock()
		t.cleanup()
	} else {
		m.mu.Unlock()
	}
}

// IsRegistered vrátí true pokud je soubor stále registrovaný pro sdílení.
func (m *Manager) IsRegistered(transferID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.registeredFiles[transferID]
	return ok
}

// GetRegisteredFiles vrátí kopii registrovaných souborů viditelných v UI (seřazeno podle názvu).
// Transient soubory (share transfer/upload) jsou vynechány.
func (m *Manager) GetRegisteredFiles() []RegisteredFile {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]RegisteredFile, 0, len(m.registeredFiles))
	for _, rf := range m.registeredFiles {
		if rf.IsTransient {
			continue
		}
		result = append(result, *rf)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].FileName < result[j].FileName
	})
	return result
}

// UnregisterFile odstraní soubor z registrovaných sdílení a persistuje změnu.
// Vrátí info o odstraněném souboru (nil pokud nebyl nalezen).
func (m *Manager) UnregisterFile(transferID string) *RegisteredFile {
	m.mu.Lock()
	rf := m.registeredFiles[transferID]
	var result *RegisteredFile
	if rf != nil {
		copy := *rf
		result = &copy
	}
	delete(m.registeredFiles, transferID)
	m.saveShares()
	m.mu.Unlock()
	return result
}

// GetSavePath vrátí uloženou cestu pro příjem (pro retry).
func (m *Manager) GetSavePath(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.transfers[id]; ok {
		return t.savePath
	}
	return ""
}

// RegisterFileForShare registruje soubor pro jednorázový share transfer.
// Použije zadané transferID (od serveru) místo generování nového.
func (m *Manager) RegisterFileForShare(transferID, filePath, fileName string, fileSize int64) {
	m.mu.Lock()
	m.registeredFiles[transferID] = &RegisteredFile{
		TransferID:  transferID,
		FilePath:    filePath,
		FileName:    fileName,
		FileSize:    fileSize,
		IsTemp:      false,
		IsTransient: true,
	}
	m.mu.Unlock()
	// Nepersistujeme — dočasná registrace pro tento transfer
}

// --- Channel-link flow (hlavní) ---

// RegisterFile registruje soubor pro P2P stahování. Vrátí transferID.
// Soubor zůstane dostupný dokud se Manager nezruší nebo soubor neodregistruje.
func (m *Manager) RegisterFile(filePath, fileName string, fileSize int64) string {
	return m.registerFile(filePath, fileName, fileSize, false)
}

// RegisterTempFile registruje dočasný soubor (ZIP). Při unshare se nabídne smazání.
func (m *Manager) RegisterTempFile(filePath, fileName string, fileSize int64) string {
	return m.registerFile(filePath, fileName, fileSize, true)
}

func (m *Manager) registerFile(filePath, fileName string, fileSize int64, isTemp bool) string {
	id := uuid.New().String()
	m.mu.Lock()
	m.registeredFiles[id] = &RegisteredFile{
		TransferID: id,
		FilePath:   filePath,
		FileName:   fileName,
		FileSize:   fileSize,
		IsTemp:     isTemp,
	}
	m.saveShares()
	m.mu.Unlock()
	return id
}

// RequestDownload — příjemce klikl na P2P link, žádá odesílatele o soubor.
// Pošle file.request přes WS a vytvoří Transfer pro sledování stavu.
// Pokud existuje partial file na savePath, pošle offset pro resume.
func (m *Manager) RequestDownload(senderID, transferID, savePath string) {
	// Detekce partial file pro resume
	var offset int64
	if fi, err := os.Stat(savePath); err == nil {
		offset = fi.Size()
	}

	t := &Transfer{
		ID:         transferID,
		PeerID:     senderID,
		Direction:  DirReceive,
		Status:     StatusWaiting,
		Progress:   offset,
		savePath:   savePath,
		Offset:     offset,
		sendWS:     m.sendWS,
		stunURL:    m.stunURL,
		onProgress: m.invalidate,
		onDone:     m.invalidate,
		onMarkDone: func(id string) { m.MarkDownloaded(id) },
		onAutoRetry: func(peerID, id, sp string, off int64) {
			m.retryDownload(peerID, id, sp, off)
		},
		onZipStart: func(sp string) {
			m.mu.Lock()
			fn := m.onZipStart
			m.mu.Unlock()
			if fn != nil {
				fn(sp)
			}
		},
	}

	m.mu.Lock()
	m.transfers[transferID] = t
	m.mu.Unlock()

	// Poslat request odesílateli (s offsetem pro resume)
	m.sendWS("file.request", map[string]any{
		"to":          senderID,
		"transfer_id": transferID,
		"offset":      offset,
	})
}

// retryDownload — interní auto-retry: recykluje transfer a pošle nový file.request.
func (m *Manager) retryDownload(senderID, transferID, savePath string, offset int64) {
	m.mu.Lock()
	old, hasOld := m.transfers[transferID]
	retries := 0
	if hasOld {
		retries = old.retries
		delete(m.transfers, transferID)
	}
	m.mu.Unlock()

	t := &Transfer{
		ID:         transferID,
		PeerID:     senderID,
		Direction:  DirReceive,
		Status:     StatusWaiting,
		Progress:   offset,
		savePath:   savePath,
		Offset:     offset,
		retries:    retries,
		sendWS:     m.sendWS,
		stunURL:    m.stunURL,
		onProgress: m.invalidate,
		onDone:     m.invalidate,
		onMarkDone: func(id string) { m.MarkDownloaded(id) },
		onAutoRetry: func(peerID, id, sp string, off int64) {
			m.retryDownload(peerID, id, sp, off)
		},
		onZipStart: func(sp string) {
			m.mu.Lock()
			fn := m.onZipStart
			m.mu.Unlock()
			if fn != nil {
				fn(sp)
			}
		},
	}

	m.mu.Lock()
	m.transfers[transferID] = t
	m.mu.Unlock()

	m.sendWS("file.request", map[string]any{
		"to":          senderID,
		"transfer_id": transferID,
		"offset":      offset,
	})
	if m.invalidate != nil {
		m.invalidate()
	}
}

// HandleRequest — odesílatel dostal žádost o soubor. Vytvoří WebRTC offer a začne posílat.
func (m *Manager) HandleRequest(from string, payload json.RawMessage) {
	var p struct {
		TransferID string `json:"transfer_id"`
		Offset     int64  `json:"offset"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		log.Printf("p2p: parse request: %v", err)
		return
	}

	m.mu.Lock()
	rf, ok := m.registeredFiles[p.TransferID]
	m.mu.Unlock()
	if !ok {
		log.Printf("p2p: request for unknown file: %s", p.TransferID)
		// Soubor není registrovaný (odesílatel offline/restartoval)
		m.sendWS("file.reject", map[string]string{
			"to":          from,
			"transfer_id": p.TransferID,
		})
		return
	}

	t := &Transfer{
		ID:         p.TransferID + "-" + from, // unikátní per requester
		PeerID:     from,
		FileName:   rf.FileName,
		FileSize:   rf.FileSize,
		Direction:  DirSend,
		Status:     StatusConnecting,
		filePath:   rf.FilePath,
		baseID:     p.TransferID, // původní ID pro WS eventy
		Offset:     p.Offset,
		Progress:   p.Offset,
		sendWS:     m.sendWS,
		stunURL:    m.stunURL,
		onProgress: m.invalidate,
		onDone:     m.invalidate,
		onMarkSent: func(id string) { m.MarkSent(id) },
	}

	m.mu.Lock()
	m.transfers[t.ID] = t
	m.mu.Unlock()

	go t.initSendOffer(from, p.TransferID)
}

// initSendOffer — odesílatel vytvoří PeerConnection + DataChannel + offer.
func (t *Transfer) initSendOffer(requesterID, transferID string) {
	config := webrtc.Configuration{}
	if t.stunURL != "" {
		config.ICEServers = []webrtc.ICEServer{
			{URLs: []string{t.stunURL}},
		}
	}

	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		t.fail("create peer connection: " + err.Error())
		return
	}
	t.mu.Lock()
	t.pc = pc
	t.mu.Unlock()

	dc, err := pc.CreateDataChannel("file-transfer", nil)
	if err != nil {
		t.fail("create data channel: " + err.Error())
		return
	}
	t.dc = dc

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		cj := c.ToJSON()
		t.sendWS("file.ice", map[string]any{
			"to":            requesterID,
			"transfer_id":   transferID,
			"candidate":     cj.Candidate,
			"sdpMLineIndex": cj.SDPMLineIndex,
			"sdpMid":        cj.SDPMid,
		})
	})

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateFailed {
			t.fail("connection failed")
		}
	})

	dc.OnOpen(func() {
		t.Status = StatusTransferring
		t.StartTime = time.Now()
		if t.onProgress != nil {
			t.onProgress()
		}
		go t.sendFile()
	})

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.fail("create offer: " + err.Error())
		return
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		t.fail("set local description: " + err.Error())
		return
	}

	t.sendWS("file.offer", map[string]any{
		"to":          requesterID,
		"transfer_id": transferID,
		"file_name":   t.FileName,
		"file_size":   t.FileSize,
		"offset":      t.Offset,
		"sdp":         offer.SDP,
	})
}

// sendFile čte soubor po chunkách a posílá přes DataChannel s flow control.
func (t *Transfer) sendFile() {
	f, err := os.Open(t.filePath)
	if err != nil {
		t.fail("open file: " + err.Error())
		return
	}
	defer f.Close()

	// Resume: přeskočit na offset
	if t.Offset > 0 {
		if _, err := f.Seek(t.Offset, io.SeekStart); err != nil {
			t.fail("seek file: " + err.Error())
			return
		}
		log.Printf("p2p: resuming send from offset %d", t.Offset)
	}

	// Flow control: čekat když je SCTP buffer plný
	const maxBuffered = 512 * 1024 // 512KB
	sendMore := make(chan struct{}, 1)
	t.dc.SetBufferedAmountLowThreshold(maxBuffered / 2)
	t.dc.OnBufferedAmountLow(func() {
		select {
		case sendMore <- struct{}{}:
		default:
		}
	})

	buf := make([]byte, chunkSize)
	for {
		if t.Status == StatusCancelled {
			return
		}

		n, readErr := f.Read(buf)
		if n > 0 {
			if sendErr := t.dc.Send(buf[:n]); sendErr != nil {
				t.fail("send chunk: " + sendErr.Error())
				return
			}
			t.Progress += int64(n)
			if t.onProgress != nil {
				t.onProgress()
			}
			// Backpressure: čekat pokud buffer přeteče
			if t.dc.BufferedAmount() > maxBuffered {
				<-sendMore
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			t.fail("read file: " + readErr.Error())
			return
		}
	}

	t.Status = StatusDone
	// Zavřít DataChannel → příjemcův dc.OnClose uzavře soubor
	t.dc.Close()
	tid := t.ID
	if t.baseID != "" {
		tid = t.baseID
	}
	t.sendWS("file.complete", map[string]string{
		"to":          t.PeerID,
		"transfer_id": tid,
	})
	if t.onMarkSent != nil {
		t.onMarkSent(tid)
	}
	if t.onDone != nil {
		t.onDone()
	}
	log.Printf("p2p: sent %s (%d bytes) to %s", t.FileName, t.Progress, t.PeerID)
}

// --- Příjem file.offer (obě flow — channel-link i přímý) ---

// HandleOffer — příjemce dostal offer od odesílatele.
// Pokud už existuje Transfer (channel-link flow / RequestDownload) → auto-accept.
// Pokud ne → cold offer (přímý P2P) → zavolá onOffer callback.
func (m *Manager) HandleOffer(from string, payload json.RawMessage) {
	var p struct {
		TransferID string `json:"transfer_id"`
		FileName   string `json:"file_name"`
		FileSize   int64  `json:"file_size"`
		Offset     int64  `json:"offset"`
		SDP        string `json:"sdp"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		log.Printf("p2p: parse offer: %v", err)
		return
	}

	m.mu.Lock()
	existing, hasExisting := m.transfers[p.TransferID]
	m.mu.Unlock()

	if hasExisting && existing.Direction == DirReceive {
		// Channel-link flow: příjemce už žádal o tento soubor → auto-accept
		existing.FileName = p.FileName
		existing.FileSize = p.FileSize
		existing.Offset = p.Offset
		existing.Progress = p.Offset
		existing.offerSDP = p.SDP
		existing.Status = StatusConnecting
		if existing.onProgress != nil {
			existing.onProgress()
		}
		go existing.initReceive(p.TransferID)
		return
	}

	// Cold offer (přímý P2P bez kanálu)
	t := &Transfer{
		ID:         p.TransferID,
		PeerID:     from,
		FileName:   p.FileName,
		FileSize:   p.FileSize,
		Direction:  DirReceive,
		Status:     StatusWaiting,
		offerSDP:   p.SDP,
		sendWS:     m.sendWS,
		stunURL:    m.stunURL,
		onProgress: m.invalidate,
		onDone:     m.invalidate,
		onMarkDone: func(id string) { m.MarkDownloaded(id) },
		onAutoRetry: func(peerID, id, sp string, off int64) {
			m.retryDownload(peerID, id, sp, off)
		},
		onZipStart: func(sp string) {
			m.mu.Lock()
			fn := m.onZipStart
			m.mu.Unlock()
			if fn != nil {
				fn(sp)
			}
		},
	}

	m.mu.Lock()
	m.transfers[t.ID] = t
	onOffer := m.onOffer
	m.mu.Unlock()

	if onOffer != nil {
		onOffer(t)
	}
}

// AcceptTransfer — příjemce akceptuje cold offer (P2POfferDialog → Save as...).
func (m *Manager) AcceptTransfer(transferID, savePath string) {
	m.mu.Lock()
	t, ok := m.transfers[transferID]
	m.mu.Unlock()
	if !ok {
		return
	}

	t.savePath = savePath
	t.Status = StatusConnecting
	if t.onProgress != nil {
		t.onProgress()
	}

	go t.initReceive(transferID)
}

// initReceive — příjemce vytvoří PeerConnection, nastaví remote offer, pošle answer.
func (t *Transfer) initReceive(transferID string) {
	config := webrtc.Configuration{}
	if t.stunURL != "" {
		config.ICEServers = []webrtc.ICEServer{
			{URLs: []string{t.stunURL}},
		}
	}

	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		t.fail("create peer connection: " + err.Error())
		return
	}
	t.mu.Lock()
	t.pc = pc
	t.mu.Unlock()

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		cj := c.ToJSON()
		t.sendWS("file.ice", map[string]any{
			"to":            t.PeerID,
			"transfer_id":   transferID,
			"candidate":     cj.Candidate,
			"sdpMLineIndex": cj.SDPMLineIndex,
			"sdpMid":        cj.SDPMid,
		})
	})

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateFailed {
			t.fail("connection failed")
		}
	})

	// DataChannel přijde od odesílatele → přijímat chunky
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		t.dc = dc

		var f *os.File
		var err error
		if t.Offset > 0 {
			// Resume: otevřít existující soubor a seeknout na offset
			f, err = os.OpenFile(t.savePath, os.O_WRONLY, 0644)
			if err == nil {
				_, err = f.Seek(t.Offset, io.SeekStart)
			}
			if err == nil {
				log.Printf("p2p: resuming receive from offset %d", t.Offset)
			}
		} else {
			f, err = os.Create(t.savePath)
		}
		if err != nil {
			t.fail("open file: " + err.Error())
			return
		}
		t.file = f

		dc.OnOpen(func() {
			t.Status = StatusTransferring
			t.StartTime = time.Now()
			if t.onProgress != nil {
				t.onProgress()
			}
			if t.onZipStart != nil && t.Direction == DirReceive && t.savePath != "" && strings.HasSuffix(strings.ToLower(t.savePath), ".zip") {
				t.onZipStart(t.savePath)
			}
		})

		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			if t.Status == StatusCancelled {
				return
			}

			n, err := f.Write(msg.Data)
			if err != nil {
				t.fail("write file: " + err.Error())
				return
			}
			t.Progress += int64(n)
			if t.onProgress != nil {
				t.onProgress()
			}
		})

		dc.OnClose(func() {
			if t.file != nil {
				t.file.Close()
				t.file = nil
			}
			if t.Status == StatusTransferring && t.Progress >= t.FileSize {
				t.Status = StatusDone
				if t.onMarkDone != nil {
					t.onMarkDone(t.ID)
				}
				if t.onDone != nil {
					t.onDone()
				}
				log.Printf("p2p: received %s (%d bytes) from %s", t.FileName, t.Progress, t.PeerID)
			} else if t.Status == StatusTransferring && t.Progress < t.FileSize {
				// Předčasné uzavření — trigger auto-retry
				t.fail("connection lost")
			}
		})
	})

	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  t.offerSDP,
	}
	if err := pc.SetRemoteDescription(offer); err != nil {
		t.fail("set remote description: " + err.Error())
		return
	}
	t.drainPendingICE()

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		t.fail("create answer: " + err.Error())
		return
	}
	if err := pc.SetLocalDescription(answer); err != nil {
		t.fail("set local description: " + err.Error())
		return
	}

	t.sendWS("file.accept", map[string]any{
		"to":          t.PeerID,
		"transfer_id": transferID,
		"sdp":         answer.SDP,
	})
}

// --- Signaling handlery ---

// HandleAccept — odesílatel dostal answer SDP od příjemce.
func (m *Manager) HandleAccept(from string, payload json.RawMessage) {
	var p struct {
		TransferID string `json:"transfer_id"`
		SDP        string `json:"sdp"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return
	}

	// Hledat transfer: buď přesný ID nebo composit ID (transferID-from)
	m.mu.Lock()
	t, ok := m.transfers[p.TransferID]
	if !ok {
		t, ok = m.transfers[p.TransferID+"-"+from]
	}
	m.mu.Unlock()
	if !ok || t.pc == nil {
		return
	}

	answer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  p.SDP,
	}
	if err := t.pc.SetRemoteDescription(answer); err != nil {
		log.Printf("p2p: set remote answer: %v", err)
		return
	}
	t.drainPendingICE()
}

// HandleIce — přidá ICE candidate.
func (m *Manager) HandleIce(from string, payload json.RawMessage) {
	var p struct {
		TransferID string `json:"transfer_id"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return
	}

	m.mu.Lock()
	t, ok := m.transfers[p.TransferID]
	if !ok {
		t, ok = m.transfers[p.TransferID+"-"+from]
	}
	m.mu.Unlock()
	if !ok {
		return
	}

	var candidate webrtc.ICECandidateInit
	if err := json.Unmarshal(payload, &candidate); err != nil {
		log.Printf("p2p: parse ICE: %v", err)
		return
	}

	t.mu.Lock()
	if t.pc == nil || !t.remoteSet {
		// PC nebo remote description ještě nejsou ready — bufferovat
		t.pendingICE = append(t.pendingICE, candidate)
		t.mu.Unlock()
		return
	}
	pc := t.pc
	t.mu.Unlock()

	if err := pc.AddICECandidate(candidate); err != nil {
		log.Printf("p2p: add ICE: %v", err)
	}
}

// HandleReject — příjemce odmítl / soubor není dostupný.
func (m *Manager) HandleReject(from string, payload json.RawMessage) {
	var p struct {
		TransferID string `json:"transfer_id"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return
	}

	m.mu.Lock()
	t, ok := m.transfers[p.TransferID]
	if ok {
		delete(m.transfers, p.TransferID)
	}
	m.mu.Unlock()

	if ok {
		t.Status = StatusCancelled
		t.Error = "rejected or unavailable"
		m.MarkUnavailable(p.TransferID)
		t.cleanup()
		if t.onDone != nil {
			t.onDone()
		}
	}
}

// HandleCancel — druhá strana zrušila přenos.
func (m *Manager) HandleCancel(from string, payload json.RawMessage) {
	var p struct {
		TransferID string `json:"transfer_id"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return
	}

	m.mu.Lock()
	t, ok := m.transfers[p.TransferID]
	if ok {
		delete(m.transfers, p.TransferID)
	}
	if !ok {
		t, ok = m.transfers[p.TransferID+"-"+from]
		if ok {
			delete(m.transfers, p.TransferID+"-"+from)
		}
	}
	m.mu.Unlock()

	if ok {
		t.Status = StatusCancelled
		t.cleanup()
		if t.onDone != nil {
			t.onDone()
		}
	}
}

// HandleComplete — odesílatel potvrdil dokončení přenosu.
func (m *Manager) HandleComplete(from string, payload json.RawMessage) {
	var p struct {
		TransferID string `json:"transfer_id"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return
	}

	m.mu.Lock()
	t, ok := m.transfers[p.TransferID]
	m.mu.Unlock()

	if ok && t.Direction == DirReceive {
		t.Status = StatusDone
		m.MarkDownloaded(p.TransferID)
		if t.onDone != nil {
			t.onDone()
		}
	}
}

// RejectTransfer — příjemce odmítl cold offer.
func (m *Manager) RejectTransfer(transferID string) {
	m.mu.Lock()
	t, ok := m.transfers[transferID]
	if ok {
		delete(m.transfers, transferID)
	}
	m.mu.Unlock()

	if ok {
		t.Status = StatusCancelled
		t.sendWS("file.reject", map[string]string{
			"to":          t.PeerID,
			"transfer_id": t.ID,
		})
		t.cleanup()
	}
}

// CancelTransfer zruší transfer a notifikuje druhou stranu.
func (m *Manager) CancelTransfer(transferID string) {
	m.mu.Lock()
	t, ok := m.transfers[transferID]
	if ok {
		delete(m.transfers, transferID)
	}
	m.mu.Unlock()

	if ok {
		t.Status = StatusCancelled
		t.sendWS("file.cancel", map[string]string{
			"to":          t.PeerID,
			"transfer_id": t.ID,
		})
		t.cleanup()
		if t.onDone != nil {
			t.onDone()
		}
	}
}

// GetActiveTransfers vrátí kopii aktivních transferů pro UI (seřazeno podle ID).
func (m *Manager) GetActiveTransfers() []*Transfer {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []*Transfer
	for _, t := range m.transfers {
		result = append(result, t)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result
}

// Cleanup — uzavře všechny transfery (při odpojení od serveru).
// Registrované soubory zůstávají — jsou persistované a po reconnectu opět dostupné.
func (m *Manager) Cleanup() {
	m.mu.Lock()
	transfers := make(map[string]*Transfer, len(m.transfers))
	for k, v := range m.transfers {
		transfers[k] = v
	}
	m.transfers = make(map[string]*Transfer)
	m.downloadedIDs = make(map[string]bool)
	m.unavailableIDs = make(map[string]bool)
	m.sentIDs = make(map[string]bool)
	m.mu.Unlock()

	for _, t := range transfers {
		t.Status = StatusCancelled
		t.cleanup()
	}
}

// drainPendingICE přidá všechny bufferované ICE kandidáty do PeerConnection.
// Volat PO SetRemoteDescription.
func (t *Transfer) drainPendingICE() {
	t.mu.Lock()
	t.remoteSet = true
	pending := t.pendingICE
	t.pendingICE = nil
	t.mu.Unlock()
	for _, c := range pending {
		if err := t.pc.AddICECandidate(c); err != nil {
			log.Printf("p2p: add buffered ICE: %v", err)
		}
	}
}

// SavePath vrátí cestu kam se soubor ukládá (pro retry).
func (t *Transfer) SavePath() string {
	return t.savePath
}

const maxAutoRetries = 3

// fail nastaví chybový stav transferu.
// Pokud je to příjem a přenos byl aktivní, automaticky zkusí retry.
func (t *Transfer) fail(msg string) {
	log.Printf("p2p: transfer %s error: %s", t.ID, msg)
	t.cleanup()

	// Auto-retry pro příjem pokud měl nějaký progress a ještě nepřekročil limit
	if t.Direction == DirReceive && t.Progress > 0 && t.retries < maxAutoRetries && t.onAutoRetry != nil {
		t.retries++
		t.Status = StatusWaiting
		t.Error = ""
		log.Printf("p2p: auto-retry %d/%d for %s (offset %d)", t.retries, maxAutoRetries, t.FileName, t.Progress)
		if t.onProgress != nil {
			t.onProgress()
		}
		peerID := t.PeerID
		id := t.ID
		savePath := t.savePath
		progress := t.Progress
		onAutoRetry := t.onAutoRetry
		go func() {
			time.Sleep(2 * time.Second)
			onAutoRetry(peerID, id, savePath, progress)
		}()
		return
	}

	t.Status = StatusError
	t.Error = msg
	if t.onDone != nil {
		t.onDone()
	}
}

// cleanup uzavře PeerConnection a DataChannel.
func (t *Transfer) cleanup() {
	t.mu.Lock()
	f := t.file
	t.file = nil
	dc := t.dc
	t.dc = nil
	pc := t.pc
	t.pc = nil
	t.mu.Unlock()

	if f != nil {
		f.Close()
	}
	if dc != nil {
		dc.Close()
	}
	if pc != nil {
		pc.Close()
	}
}
