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

// Transfer direction
const (
	DirSend    = 0
	DirReceive = 1
)

// Transfer states
const (
	StatusWaiting      = 0
	StatusConnecting   = 1
	StatusTransferring = 2
	StatusDone         = 3
	StatusError        = 4
	StatusCancelled    = 5
)

// SendWSFunc — callback for sending a WS event.
type SendWSFunc func(eventType string, payload any) error

// RegisteredFile — a file offered for download via a channel message.
type RegisteredFile struct {
	TransferID  string
	FilePath    string
	FileName    string
	FileSize    int64
	IsTemp      bool // temporary file (ZIP from temp directory) — offer deletion on unshare
	IsTransient bool // transient registration (share transfer/upload) — do not show in UI
}

// Transfer — a single P2P file transfer.
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
	pendingICE []webrtc.ICECandidateInit // ICE candidates buffered before remote description
	remoteSet  bool                      // remote description set (ICE can be added)
	file       *os.File                  // receiver: open file (for cleanup)
	sendWS     SendWSFunc
	stunURL    string

	filePath    string    // sender: path to source file
	savePath    string    // receiver: where to save
	baseID      string    // original transferID (without composite suffix, for WS events)
	Offset      int64     // resume offset (how many bytes receiver already has)
	StartTime   time.Time // transfer start time (for speed/ETA)

	offerSDP string // stored SDP for deferred accept

	onProgress   func()
	onDone       func()
	onMarkDone   func(string)                          // callback to mark transfer as downloaded (transferID)
	onMarkSent   func(string)                          // callback to mark transfer as sent (sender-side)
	onAutoRetry  func(peerID, transferID, savePath string, offset int64) // auto-retry on failure
	onZipStart   func(savePath string)                  // callback: .zip transfer started
	retries      int
}

// Manager — manages all P2P transfers and registered files.
type Manager struct {
	mu         sync.Mutex
	userID     string
	stunURL    string
	sendWS     SendWSFunc
	invalidate func()

	transfers       map[string]*Transfer      // transferID → Transfer
	registeredFiles map[string]*RegisteredFile // transferID → RegisteredFile
	downloadedIDs   map[string]bool            // transferIDs successfully downloaded
	unavailableIDs  map[string]bool            // transferIDs unavailable (rejected)
	sentIDs         map[string]bool            // transferIDs successfully sent (sender-side)

	// Callback: incoming cold offer (direct P2P without channel) → UI asks user
	onOffer func(t *Transfer)
	// Callback: .zip transfer started (StatusTransferring) → UI shows dialog
	onZipStart func(savePath string)
	// Callback: .zip file successfully downloaded → UI offers extraction
	onZipDone func(savePath string)
}

// sharesFilePath returns the path to the file with persisted shared files.
func sharesFilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".nora", "p2p_shares.json")
}

// persistedShare — JSON format for persisting a registered file.
type persistedShare struct {
	TransferID string `json:"transfer_id"`
	FilePath   string `json:"file_path"`
	FileName   string `json:"file_name"`
	FileSize   int64  `json:"file_size"`
	IsTemp     bool   `json:"is_temp,omitempty"`
}

// loadShares loads registered files from disk. Validates file existence.
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
		// Verify that the file still exists
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

// saveShares saves registered files to disk.
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

// NewManager creates a P2P Manager. Loads persisted shared files from disk.
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

// SetOnOffer sets the callback for incoming cold offers (direct file offers).
func (m *Manager) SetOnOffer(fn func(t *Transfer)) {
	m.mu.Lock()
	m.onOffer = fn
	m.mu.Unlock()
}

// SetOnZipStart sets the callback called when a .zip transfer starts (StatusTransferring).
func (m *Manager) SetOnZipStart(fn func(savePath string)) {
	m.mu.Lock()
	m.onZipStart = fn
	m.mu.Unlock()
}

// SetOnZipDone sets the callback called after a .zip file is successfully downloaded.
func (m *Manager) SetOnZipDone(fn func(savePath string)) {
	m.mu.Lock()
	m.onZipDone = fn
	m.mu.Unlock()
}

// IsDownloaded returns true if the transfer was successfully downloaded.
func (m *Manager) IsDownloaded(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.downloadedIDs[id]
}

// IsUnavailable returns true if the transfer is unavailable (rejected).
func (m *Manager) IsUnavailable(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.unavailableIDs[id]
}

// MarkDownloaded marks the transfer as successfully downloaded.
// If it is DirReceive and the file is .zip, calls the onZipDone callback.
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

// IsTransferSent returns true if the transfer was successfully sent (sender-side).
func (m *Manager) IsTransferSent(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sentIDs[id]
}

// MarkSent marks the transfer as successfully sent (sender-side).
func (m *Manager) MarkSent(id string) {
	m.mu.Lock()
	m.sentIDs[id] = true
	m.mu.Unlock()
}

// MarkUnavailable marks the transfer as unavailable.
func (m *Manager) MarkUnavailable(id string) {
	m.mu.Lock()
	m.unavailableIDs[id] = true
	m.mu.Unlock()
}

// DismissTransfer removes a transfer from active transfers (dismiss from UI).
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

// IsRegistered returns true if the file is still registered for sharing.
func (m *Manager) IsRegistered(transferID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.registeredFiles[transferID]
	return ok
}

// GetRegisteredFiles returns a copy of registered files visible in UI (sorted by name).
// Transient files (share transfer/upload) are omitted.
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

// UnregisterFile removes a file from registered shares and persists the change.
// Returns info about the removed file (nil if not found).
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

// GetSavePath returns the saved path for receiving (for retry).
func (m *Manager) GetSavePath(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.transfers[id]; ok {
		return t.savePath
	}
	return ""
}

// RegisterFileForShare registers a file for a one-time share transfer.
// Uses the provided transferID (from the server) instead of generating a new one.
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
	// Not persisted — transient registration for this transfer
}

// --- Channel-link flow (main) ---

// RegisterFile registers a file for P2P download. Returns transferID.
// The file remains available until the Manager is closed or the file is unregistered.
func (m *Manager) RegisterFile(filePath, fileName string, fileSize int64) string {
	return m.registerFile(filePath, fileName, fileSize, false)
}

// RegisterTempFile registers a temporary file (ZIP). Deletion is offered on unshare.
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

// RequestDownload — receiver clicked on a P2P link, requesting a file from the sender.
// Sends file.request via WS and creates a Transfer for state tracking.
// If a partial file exists at savePath, sends an offset for resume.
func (m *Manager) RequestDownload(senderID, transferID, savePath string) {
	// Detect partial file for resume
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

	// Send request to sender (with offset for resume)
	m.sendWS("file.request", map[string]any{
		"to":          senderID,
		"transfer_id": transferID,
		"offset":      offset,
	})
}

// retryDownload — internal auto-retry: recycles the transfer and sends a new file.request.
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

// HandleRequest — sender received a file request. Creates a WebRTC offer and starts sending.
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
		// File is not registered (sender offline/restarted)
		m.sendWS("file.reject", map[string]string{
			"to":          from,
			"transfer_id": p.TransferID,
		})
		return
	}

	t := &Transfer{
		ID:         p.TransferID + "-" + from, // unique per requester
		PeerID:     from,
		FileName:   rf.FileName,
		FileSize:   rf.FileSize,
		Direction:  DirSend,
		Status:     StatusConnecting,
		filePath:   rf.FilePath,
		baseID:     p.TransferID, // original ID for WS events
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

// initSendOffer — sender creates PeerConnection + DataChannel + offer.
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

// sendFile reads the file in chunks and sends via DataChannel with flow control.
func (t *Transfer) sendFile() {
	f, err := os.Open(t.filePath)
	if err != nil {
		t.fail("open file: " + err.Error())
		return
	}
	defer f.Close()

	// Resume: skip to offset
	if t.Offset > 0 {
		if _, err := f.Seek(t.Offset, io.SeekStart); err != nil {
			t.fail("seek file: " + err.Error())
			return
		}
		log.Printf("p2p: resuming send from offset %d", t.Offset)
	}

	// Flow control: wait when SCTP buffer is full
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
			// Backpressure: wait if buffer overflows
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
	// Close DataChannel → receiver's dc.OnClose closes the file
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

// --- Receiving file.offer (both flows — channel-link and direct) ---

// HandleOffer — receiver got an offer from the sender.
// If a Transfer already exists (channel-link flow / RequestDownload) → auto-accept.
// If not → cold offer (direct P2P) → calls onOffer callback.
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
		// Channel-link flow: receiver already requested this file → auto-accept
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

	// Cold offer (direct P2P without channel)
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

// AcceptTransfer — receiver accepts a cold offer (P2POfferDialog → Save as...).
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

// initReceive — receiver creates PeerConnection, sets remote offer, sends answer.
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

	// DataChannel comes from sender → receive chunks
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		t.dc = dc

		var f *os.File
		var err error
		if t.Offset > 0 {
			// Resume: open existing file and seek to offset
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
				// Premature close — trigger auto-retry
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

// --- Signaling handlers ---

// HandleAccept — sender received answer SDP from receiver.
func (m *Manager) HandleAccept(from string, payload json.RawMessage) {
	var p struct {
		TransferID string `json:"transfer_id"`
		SDP        string `json:"sdp"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return
	}

	// Look for transfer: either exact ID or composite ID (transferID-from)
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

// HandleIce — adds an ICE candidate.
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
		// PC or remote description not ready yet — buffer
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

// HandleReject — receiver declined / file is not available.
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

// HandleCancel — the other side cancelled the transfer.
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

// HandleComplete — sender confirmed transfer completion.
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

// RejectTransfer — receiver declined the cold offer.
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

// CancelTransfer cancels the transfer and notifies the other side.
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

// GetActiveTransfers returns a copy of active transfers for UI (sorted by ID).
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

// Cleanup — closes all transfers (on disconnect from server).
// Registered files remain — they are persisted and available again after reconnect.
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

// drainPendingICE adds all buffered ICE candidates to the PeerConnection.
// Call AFTER SetRemoteDescription.
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

// SavePath returns the path where the file is being saved (for retry).
func (t *Transfer) SavePath() string {
	return t.savePath
}

const maxAutoRetries = 3

// fail sets the error state of the transfer.
// If it is a receive and the transfer was active, automatically tries retry.
func (t *Transfer) fail(msg string) {
	log.Printf("p2p: transfer %s error: %s", t.ID, msg)
	t.cleanup()

	// Auto-retry for receive if there was some progress and retry limit not exceeded
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

// cleanup closes PeerConnection and DataChannel.
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
