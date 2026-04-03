package p2p

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/pion/webrtc/v4"
)

// MountSession maintains a persistent WebRTC connection for on-demand file transfers.
// The receiver requests files one at a time; the owner sends them over the same connection.
type MountSession struct {
	mu         sync.Mutex
	stunURL    string
	sendWS     SendWSFunc
	pc         *webrtc.PeerConnection
	dc         *webrtc.DataChannel
	pendingICE []webrtc.ICECandidateInit
	remoteSet  bool
	connected  chan struct{} // closed when DataChannel opens

	SessionID string // unique ID for WS signaling
	PeerID    string // remote user ID
	LocalRoot string // owner: local root path for share
	ShareID   string

	// Owner side: serialize file sending (one at a time)
	reqQueue chan mountRequest

	// Owner progress callback: called when sending a file (fileName, bytesSent, fileSize)
	OnSendProgress func(fileName string, sent, total int64)
	OnSendDone     func(fileName string)

	// Receiver side: pending file requests waiting for response
	pendingReqs   map[string]chan error // requestID → done channel
	pendingPaths  map[string]string    // requestID → savePath
	pendingMu     sync.Mutex
	currentReqID  string // requestID currently being received
	currentFile   *os.File
	remaining     int64
}

type mountRequest struct {
	RequestID string `json:"req_id"`
	FileID    string `json:"file_id"`
	FileName  string `json:"file_name"`
	RelPath   string `json:"rel_path"`
}

type mountResponse struct {
	RequestID string `json:"req_id"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	Error     string `json:"error,omitempty"`
}

// --- Owner side ---

func NewMountSessionOwner(stunURL string, sendWS SendWSFunc, sessionID, requesterID, localRoot, shareID string) *MountSession {
	ms := &MountSession{
		stunURL:      stunURL,
		sendWS:       sendWS,
		SessionID:    sessionID,
		PeerID:       requesterID,
		LocalRoot:    localRoot,
		ShareID:      shareID,
		connected:    make(chan struct{}),
		pendingReqs:  make(map[string]chan error),
		pendingPaths: make(map[string]string),
		reqQueue:     make(chan mountRequest, 64),
	}
	// Process file requests one at a time
	go func() {
		for req := range ms.reqQueue {
			ms.handleFileRequest(req)
		}
	}()
	return ms
}

// StartOwner creates a PeerConnection with two DataChannels and sends an offer.
func (ms *MountSession) StartOwner() {
	config := webrtc.Configuration{}
	if ms.stunURL != "" {
		config.ICEServers = []webrtc.ICEServer{{URLs: []string{ms.stunURL}}}
	}

	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		log.Printf("mount-session: create pc: %v", err)
		return
	}
	ms.mu.Lock()
	ms.pc = pc
	ms.mu.Unlock()

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		ms.sendWS("mount.ice", map[string]any{
			"to":         ms.PeerID,
			"session_id": ms.SessionID,
			"candidate":  c.ToJSON().Candidate,
			"sdp_mid":    c.ToJSON().SDPMid,
			"sdp_mline":  c.ToJSON().SDPMLineIndex,
		})
	})

	// Data channel for bidirectional file transfer
	dc, err := pc.CreateDataChannel("mount-fs", nil)
	if err != nil {
		log.Printf("mount-session: create dc: %v", err)
		return
	}
	ms.mu.Lock()
	ms.dc = dc
	ms.mu.Unlock()

	dc.OnOpen(func() {
		log.Printf("mount-session: owner dc open")
		close(ms.connected)
	})

	// Owner receives file requests from receiver — queue for serial processing
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		var req mountRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			log.Printf("mount-session: parse request: %v", err)
			return
		}
		select {
		case ms.reqQueue <- req:
		default:
			log.Printf("mount-session: request queue full, dropping %s", req.FileName)
		}
	})

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		log.Printf("mount-session: create offer: %v", err)
		return
	}
	pc.SetLocalDescription(offer)

	ms.sendWS("mount.offer", map[string]any{
		"to":         ms.PeerID,
		"session_id": ms.SessionID,
		"share_id":   ms.ShareID,
		"sdp":        offer.SDP,
	})
}

func (ms *MountSession) handleFileRequest(req mountRequest) {
	cleanRoot := filepath.Clean(ms.LocalRoot)
	relPath := strings.TrimPrefix(req.RelPath, "/")
	var fullPath string
	if relPath == "" {
		fullPath = filepath.Join(ms.LocalRoot, req.FileName)
	} else {
		fullPath = filepath.Join(ms.LocalRoot, relPath, req.FileName)
	}

	cleanPath := filepath.Clean(fullPath)
	if !strings.HasPrefix(cleanPath, cleanRoot+string(filepath.Separator)) && cleanPath != cleanRoot {
		ms.sendResponse(mountResponse{RequestID: req.RequestID, Error: "path traversal"})
		return
	}

	fi, err := os.Stat(cleanPath)
	if err != nil || fi.IsDir() {
		ms.sendResponse(mountResponse{RequestID: req.RequestID, Error: "not found"})
		return
	}

	entryName := req.FileName
	if relPath != "" {
		entryName = relPath + "/" + req.FileName
	}

	// Send response header
	ms.sendResponse(mountResponse{RequestID: req.RequestID, Name: entryName, Size: fi.Size()})

	// Send file data with progress
	ms.sendFileData(cleanPath, req.FileName, fi.Size())
}

func (ms *MountSession) sendResponse(resp mountResponse) {
	hdr, _ := json.Marshal(resp)
	msg := make([]byte, 4+len(hdr))
	binary.BigEndian.PutUint32(msg[:4], uint32(len(hdr)))
	copy(msg[4:], hdr)
	ms.mu.Lock()
	dc := ms.dc
	ms.mu.Unlock()
	if dc != nil {
		dc.Send(msg)
	}
}

func (ms *MountSession) sendFileData(path, fileName string, fileSize int64) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	ms.mu.Lock()
	dc := ms.dc
	ms.mu.Unlock()
	if dc == nil {
		return
	}

	const maxBuffered = 512 * 1024
	sendMore := make(chan struct{}, 1)
	dc.SetBufferedAmountLowThreshold(maxBuffered / 2)
	dc.OnBufferedAmountLow(func() {
		select {
		case sendMore <- struct{}{}:
		default:
		}
	})

	buf := make([]byte, chunkSize)
	var sent int64
	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			dc.Send(buf[:n])
			sent += int64(n)
			if ms.OnSendProgress != nil {
				ms.OnSendProgress(fileName, sent, fileSize)
			}
			if dc.BufferedAmount() > maxBuffered {
				<-sendMore
			}
		}
		if readErr != nil {
			break
		}
	}
	if ms.OnSendDone != nil {
		ms.OnSendDone(fileName)
	}
}

// --- Receiver side ---

func NewMountSessionReceiver(stunURL string, sendWS SendWSFunc, sessionID, ownerID, shareID string) *MountSession {
	return &MountSession{
		stunURL:      stunURL,
		sendWS:       sendWS,
		SessionID:    sessionID,
		PeerID:       ownerID,
		ShareID:      shareID,
		connected:    make(chan struct{}),
		pendingReqs:  make(map[string]chan error),
		pendingPaths: make(map[string]string),
	}
}

// RequestFile requests a file from the owner over the persistent connection.
// Blocks until the file is downloaded to savePath or an error occurs.
func (ms *MountSession) RequestFile(fileID, fileName, relPath, savePath string) error {
	// Wait for connection
	select {
	case <-ms.connected:
	default:
		return fmt.Errorf("mount session not connected")
	}

	reqID := fmt.Sprintf("%s-%d", fileID, ms.nextReqNum())
	done := make(chan error, 1)

	ms.pendingMu.Lock()
	ms.pendingReqs[reqID] = done
	ms.pendingPaths[reqID] = savePath
	ms.pendingMu.Unlock()

	defer func() {
		ms.pendingMu.Lock()
		delete(ms.pendingReqs, reqID)
		delete(ms.pendingPaths, reqID)
		ms.pendingMu.Unlock()
	}()

	// Ensure save directory exists
	os.MkdirAll(filepath.Dir(savePath), 0755)

	// Send request
	reqData, _ := json.Marshal(mountRequest{
		RequestID: reqID,
		FileID:    fileID,
		FileName:  fileName,
		RelPath:   relPath,
	})
	ms.mu.Lock()
	dc := ms.dc
	ms.mu.Unlock()
	if dc == nil {
		return fmt.Errorf("data channel closed")
	}
	dc.Send(reqData)

	return <-done
}

var reqCounter uint64
var reqCounterMu sync.Mutex

func (ms *MountSession) nextReqNum() uint64 {
	reqCounterMu.Lock()
	reqCounter++
	n := reqCounter
	reqCounterMu.Unlock()
	return n
}

// HandleOffer processes the owner's SDP offer (receiver side).
func (ms *MountSession) HandleOffer(sdp string) {
	config := webrtc.Configuration{}
	if ms.stunURL != "" {
		config.ICEServers = []webrtc.ICEServer{{URLs: []string{ms.stunURL}}}
	}

	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		log.Printf("mount-session: create pc: %v", err)
		return
	}
	ms.mu.Lock()
	ms.pc = pc
	ms.mu.Unlock()

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		ms.sendWS("mount.ice", map[string]any{
			"to":         ms.PeerID,
			"session_id": ms.SessionID,
			"candidate":  c.ToJSON().Candidate,
			"sdp_mid":    c.ToJSON().SDPMid,
			"sdp_mline":  c.ToJSON().SDPMLineIndex,
		})
	})

	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		ms.mu.Lock()
		ms.dc = dc
		ms.mu.Unlock()

		dc.OnOpen(func() {
			log.Printf("mount-session: receiver dc open")
			close(ms.connected)
		})

		// Receiver gets file data (headers + chunks)
		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			ms.handleIncomingData(msg.Data)
		})
	})

	err = pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: sdp})
	if err != nil {
		log.Printf("mount-session: set remote: %v", err)
		return
	}

	ms.mu.Lock()
	ms.remoteSet = true
	pending := ms.pendingICE
	ms.pendingICE = nil
	ms.mu.Unlock()
	for _, c := range pending {
		pc.AddICECandidate(c)
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		log.Printf("mount-session: create answer: %v", err)
		return
	}
	pc.SetLocalDescription(answer)

	ms.sendWS("mount.accept", map[string]any{
		"to":         ms.PeerID,
		"session_id": ms.SessionID,
		"sdp":        answer.SDP,
	})
}

func (ms *MountSession) handleIncomingData(data []byte) {
	for len(data) > 0 {
		if ms.remaining == 0 {
			// Close previous file if any
			if ms.currentFile != nil {
				ms.currentFile.Close()
				ms.currentFile = nil
				// Signal completion
				ms.pendingMu.Lock()
				if ch, ok := ms.pendingReqs[ms.currentReqID]; ok {
					ch <- nil
				}
				ms.pendingMu.Unlock()
				ms.currentReqID = ""
			}

			// Expecting header
			if len(data) < 4 {
				return
			}
			hdrLen := int(binary.BigEndian.Uint32(data[:4]))
			if len(data) < 4+hdrLen {
				return
			}
			var resp mountResponse
			if err := json.Unmarshal(data[4:4+hdrLen], &resp); err != nil {
				log.Printf("mount-session: parse response: %v", err)
				return
			}
			data = data[4+hdrLen:]

			if resp.Error != "" {
				ms.pendingMu.Lock()
				if ch, ok := ms.pendingReqs[resp.RequestID]; ok {
					ch <- fmt.Errorf("%s", resp.Error)
				}
				ms.pendingMu.Unlock()
				continue
			}

			if resp.Size == 0 {
				ms.pendingMu.Lock()
				if ch, ok := ms.pendingReqs[resp.RequestID]; ok {
					ch <- nil
				}
				ms.pendingMu.Unlock()
				continue
			}

			ms.currentReqID = resp.RequestID
			ms.remaining = resp.Size

			// Open file for writing
			ms.pendingMu.Lock()
			savePath := ms.pendingPaths[resp.RequestID]
			ms.pendingMu.Unlock()

			f, err := os.Create(savePath)
			if err != nil {
				log.Printf("mount-session: create file: %v", err)
				ms.remaining = 0
				ms.pendingMu.Lock()
				if ch, ok := ms.pendingReqs[resp.RequestID]; ok {
					ch <- err
				}
				ms.pendingMu.Unlock()
				continue
			}
			ms.currentFile = f
		} else {
			// Receiving file data
			toWrite := data
			if int64(len(toWrite)) > ms.remaining {
				toWrite = toWrite[:ms.remaining]
			}
			if ms.currentFile != nil {
				ms.currentFile.Write(toWrite)
			}
			ms.remaining -= int64(len(toWrite))
			data = data[len(toWrite):]

			// File complete
			if ms.remaining == 0 && ms.currentFile != nil {
				ms.currentFile.Close()
				ms.currentFile = nil
				ms.pendingMu.Lock()
				if ch, ok := ms.pendingReqs[ms.currentReqID]; ok {
					ch <- nil
				}
				ms.pendingMu.Unlock()
				ms.currentReqID = ""
			}
		}
	}
}

// HandleAccept processes receiver's SDP answer (owner side).
func (ms *MountSession) HandleAccept(sdp string) {
	ms.mu.Lock()
	pc := ms.pc
	ms.mu.Unlock()
	if pc == nil {
		return
	}
	err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: sdp})
	if err != nil {
		log.Printf("mount-session: set answer: %v", err)
		return
	}
	ms.mu.Lock()
	ms.remoteSet = true
	pending := ms.pendingICE
	ms.pendingICE = nil
	ms.mu.Unlock()
	for _, c := range pending {
		pc.AddICECandidate(c)
	}
}

// AddICECandidate adds an ICE candidate.
func (ms *MountSession) AddICECandidate(candidate string, sdpMid *string, sdpMLine *uint16) {
	init := webrtc.ICECandidateInit{
		Candidate:     candidate,
		SDPMid:        sdpMid,
		SDPMLineIndex: sdpMLine,
	}
	ms.mu.Lock()
	if ms.remoteSet && ms.pc != nil {
		ms.pc.AddICECandidate(init)
	} else {
		ms.pendingICE = append(ms.pendingICE, init)
	}
	ms.mu.Unlock()
}

// IsConnected returns true if the DataChannel is open.
func (ms *MountSession) IsConnected() bool {
	select {
	case <-ms.connected:
		return true
	default:
		return false
	}
}

// Close shuts down the session.
func (ms *MountSession) Close() {
	ms.mu.Lock()
	pc := ms.pc
	ms.pc = nil
	q := ms.reqQueue
	ms.reqQueue = nil
	ms.mu.Unlock()
	if pc != nil {
		pc.Close()
	}
	if q != nil {
		close(q)
	}
}
