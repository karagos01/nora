package p2p

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
)

// BulkFileRequest describes one file to transfer in a bulk session.
type BulkFileRequest struct {
	FileID       string `json:"file_id"`
	FileName     string `json:"file_name"`
	FileSize     int64  `json:"file_size"`
	RelativePath string `json:"relative_path"`
}

// bulkHeader is sent before each file's data over the DataChannel.
// Format: 4 bytes (header JSON length, big-endian) + header JSON
type bulkHeader struct {
	Name string `json:"name"` // "relative/path/file.txt"
	Size int64  `json:"size"`
}

// BulkSession manages a single persistent WebRTC connection for transferring multiple files.
type BulkSession struct {
	mu         sync.Mutex
	stunURL    string
	sendWS     SendWSFunc
	pc         *webrtc.PeerConnection
	dc         *webrtc.DataChannel
	pendingICE []webrtc.ICECandidateInit
	remoteSet  bool

	TransferID string
	PeerID     string // remote user ID
	Files      []BulkFileRequest
	LocalRoot  string // sender: local root path for share
	SaveRoot   string // receiver: where to save files

	// Progress tracking
	TotalFiles int
	DoneFiles  int
	TotalBytes int64
	DoneBytes  int64

	OnProgress func(doneFiles, totalFiles int, doneBytes, totalBytes int64)
	OnDone     func()
	OnError    func(err string)
}

// --- Sender (owner) side ---

// NewBulkSender creates a bulk session for the owner to send files.
func NewBulkSender(stunURL string, sendWS SendWSFunc, transferID, requesterID, localRoot string, files []BulkFileRequest) *BulkSession {
	var totalBytes int64
	for _, f := range files {
		totalBytes += f.FileSize
	}
	return &BulkSession{
		stunURL:    stunURL,
		sendWS:     sendWS,
		TransferID: transferID,
		PeerID:     requesterID,
		Files:      files,
		LocalRoot:  localRoot,
		TotalFiles: len(files),
		TotalBytes: totalBytes,
	}
}

// StartSend creates PeerConnection, DataChannel, and sends an offer.
func (bs *BulkSession) StartSend() {
	config := webrtc.Configuration{}
	if bs.stunURL != "" {
		config.ICEServers = []webrtc.ICEServer{
			{URLs: []string{bs.stunURL}},
		}
	}

	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		bs.doError("create peer connection: " + err.Error())
		return
	}
	bs.mu.Lock()
	bs.pc = pc
	bs.mu.Unlock()

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		bs.sendWS("bulk.ice", map[string]any{
			"to":          bs.PeerID,
			"transfer_id": bs.TransferID,
			"candidate":   c.ToJSON().Candidate,
			"sdp_mid":     c.ToJSON().SDPMid,
			"sdp_mline":   c.ToJSON().SDPMLineIndex,
		})
	})

	dc, err := pc.CreateDataChannel("bulk-transfer", nil)
	if err != nil {
		bs.doError("create data channel: " + err.Error())
		return
	}
	bs.mu.Lock()
	bs.dc = dc
	bs.mu.Unlock()

	dc.OnOpen(func() {
		go bs.sendAllFiles()
	})

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		bs.doError("create offer: " + err.Error())
		return
	}
	pc.SetLocalDescription(offer)

	bs.sendWS("bulk.offer", map[string]any{
		"to":          bs.PeerID,
		"transfer_id": bs.TransferID,
		"sdp":         offer.SDP,
		"total_files": bs.TotalFiles,
		"total_bytes": bs.TotalBytes,
	})
}

func (bs *BulkSession) sendAllFiles() {
	cleanRoot := filepath.Clean(bs.LocalRoot)

	for _, f := range bs.Files {
		relPath := strings.TrimPrefix(f.RelativePath, "/")
		var fullPath string
		if relPath == "" {
			fullPath = filepath.Join(bs.LocalRoot, f.FileName)
		} else {
			fullPath = filepath.Join(bs.LocalRoot, relPath, f.FileName)
		}

		cleanPath := filepath.Clean(fullPath)
		if !strings.HasPrefix(cleanPath, cleanRoot+string(filepath.Separator)) && cleanPath != cleanRoot {
			log.Printf("bulk: path traversal skipped: %s", fullPath)
			continue
		}

		entryName := f.FileName
		if relPath != "" {
			entryName = relPath + "/" + f.FileName
		}

		fi, err := os.Stat(cleanPath)
		if err != nil || fi.IsDir() {
			log.Printf("bulk: skip missing/dir: %s", cleanPath)
			// Send zero-size header so receiver knows to skip
			bs.sendHeader(entryName, 0)
			continue
		}

		bs.sendHeader(entryName, fi.Size())
		bs.sendFileData(cleanPath, fi.Size())

		bs.mu.Lock()
		bs.DoneFiles++
		bs.mu.Unlock()
		if bs.OnProgress != nil {
			bs.OnProgress(bs.DoneFiles, bs.TotalFiles, bs.DoneBytes, bs.TotalBytes)
		}
	}

	// Small delay so last data chunk is flushed before close
	time.Sleep(200 * time.Millisecond)
	bs.dc.Close()

	if bs.OnDone != nil {
		bs.OnDone()
	}
	log.Printf("bulk: sent %d files (%d bytes) to %s", bs.DoneFiles, bs.DoneBytes, bs.PeerID)
}

func (bs *BulkSession) sendHeader(name string, size int64) {
	hdr, _ := json.Marshal(bulkHeader{Name: name, Size: size})
	// Prefix: 4 bytes length (to distinguish header from data)
	msg := make([]byte, 4+len(hdr))
	binary.BigEndian.PutUint32(msg[:4], uint32(len(hdr)))
	copy(msg[4:], hdr)
	bs.dc.Send(msg)
}

func (bs *BulkSession) sendFileData(path string, size int64) {
	f, err := os.Open(path)
	if err != nil {
		log.Printf("bulk: open error: %s: %v", path, err)
		return
	}
	defer f.Close()

	// Flow control
	const maxBuffered = 512 * 1024
	sendMore := make(chan struct{}, 1)
	bs.dc.SetBufferedAmountLowThreshold(maxBuffered / 2)
	bs.dc.OnBufferedAmountLow(func() {
		select {
		case sendMore <- struct{}{}:
		default:
		}
	})

	buf := make([]byte, chunkSize)
	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			bs.dc.Send(buf[:n])
			bs.mu.Lock()
			bs.DoneBytes += int64(n)
			bs.mu.Unlock()

			if bs.dc.BufferedAmount() > maxBuffered {
				<-sendMore
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				log.Printf("bulk: read error: %s: %v", path, readErr)
			}
			break
		}
	}
}

// HandleAccept processes the receiver's SDP answer.
func (bs *BulkSession) HandleAccept(sdp string) {
	bs.mu.Lock()
	pc := bs.pc
	bs.mu.Unlock()
	if pc == nil {
		return
	}

	err := pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  sdp,
	})
	if err != nil {
		log.Printf("bulk: set remote desc: %v", err)
		return
	}

	bs.mu.Lock()
	bs.remoteSet = true
	pending := bs.pendingICE
	bs.pendingICE = nil
	bs.mu.Unlock()
	for _, c := range pending {
		pc.AddICECandidate(c)
	}
}

// AddICECandidate adds an ICE candidate (buffered if remote desc not yet set).
func (bs *BulkSession) AddICECandidate(candidate string, sdpMid *string, sdpMLine *uint16) {
	init := webrtc.ICECandidateInit{
		Candidate:     candidate,
		SDPMid:        sdpMid,
		SDPMLineIndex: sdpMLine,
	}
	bs.mu.Lock()
	if bs.remoteSet && bs.pc != nil {
		bs.pc.AddICECandidate(init)
	} else {
		bs.pendingICE = append(bs.pendingICE, init)
	}
	bs.mu.Unlock()
}

func (bs *BulkSession) Close() {
	bs.mu.Lock()
	if bs.pc != nil {
		bs.pc.Close()
	}
	bs.mu.Unlock()
}

func (bs *BulkSession) doError(msg string) {
	log.Printf("bulk: error: %s", msg)
	if bs.OnError != nil {
		bs.OnError(msg)
	}
}

// --- Receiver side ---

// NewBulkReceiver creates a bulk session for the receiver.
func NewBulkReceiver(stunURL string, sendWS SendWSFunc, transferID, ownerID, saveRoot string, totalFiles int, totalBytes int64) *BulkSession {
	return &BulkSession{
		stunURL:    stunURL,
		sendWS:     sendWS,
		TransferID: transferID,
		PeerID:     ownerID,
		SaveRoot:   saveRoot,
		TotalFiles: totalFiles,
		TotalBytes: totalBytes,
	}
}

// HandleOffer processes the sender's SDP offer and starts receiving.
func (bs *BulkSession) HandleOffer(sdp string) {
	config := webrtc.Configuration{}
	if bs.stunURL != "" {
		config.ICEServers = []webrtc.ICEServer{
			{URLs: []string{bs.stunURL}},
		}
	}

	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		bs.doError("create peer connection: " + err.Error())
		return
	}
	bs.mu.Lock()
	bs.pc = pc
	bs.mu.Unlock()

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		bs.sendWS("bulk.ice", map[string]any{
			"to":          bs.PeerID,
			"transfer_id": bs.TransferID,
			"candidate":   c.ToJSON().Candidate,
			"sdp_mid":     c.ToJSON().SDPMid,
			"sdp_mline":   c.ToJSON().SDPMLineIndex,
		})
	})

	// Receive data channel
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		bs.mu.Lock()
		bs.dc = dc
		bs.mu.Unlock()

		dc.OnOpen(func() {
			log.Printf("bulk: receiver data channel open")
		})

		// State machine for receiving: expecting header or data
		var currentFile *os.File
		var currentName string
		var remaining int64

		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			data := msg.Data

			for len(data) > 0 {
				if remaining == 0 {
					// Expecting a header
					if currentFile != nil {
						currentFile.Close()
						currentFile = nil
						bs.mu.Lock()
						bs.DoneFiles++
						bs.mu.Unlock()
						if bs.OnProgress != nil {
							bs.OnProgress(bs.DoneFiles, bs.TotalFiles, bs.DoneBytes, bs.TotalBytes)
						}
					}

					if len(data) < 4 {
						log.Printf("bulk: short header prefix")
						return
					}
					hdrLen := int(binary.BigEndian.Uint32(data[:4]))
					if len(data) < 4+hdrLen {
						log.Printf("bulk: short header data")
						return
					}
					var hdr bulkHeader
					if err := json.Unmarshal(data[4:4+hdrLen], &hdr); err != nil {
						log.Printf("bulk: parse header: %v", err)
						return
					}
					data = data[4+hdrLen:]

					if hdr.Size == 0 {
						// Skipped file
						continue
					}

					currentName = hdr.Name
					remaining = hdr.Size

					cleanRoot := filepath.Clean(bs.SaveRoot)
					savePath := filepath.Join(bs.SaveRoot, filepath.FromSlash(currentName))
					if !strings.HasPrefix(filepath.Clean(savePath), cleanRoot+string(filepath.Separator)) {
						log.Printf("bulk: path traversal skipped: %s", currentName)
						remaining = 0
						continue
					}

					os.MkdirAll(filepath.Dir(savePath), 0755)
					f, err := os.Create(savePath)
					if err != nil {
						log.Printf("bulk: create file error %s: %v", savePath, err)
						remaining = 0
						continue
					}
					currentFile = f
				} else {
					// Receiving file data
					toWrite := data
					if int64(len(toWrite)) > remaining {
						toWrite = toWrite[:remaining]
					}
					if currentFile != nil {
						n, _ := currentFile.Write(toWrite)
						bs.mu.Lock()
						bs.DoneBytes += int64(n)
						bs.mu.Unlock()
					}
					remaining -= int64(len(toWrite))
					data = data[len(toWrite):]
				}
			}
		})

		dc.OnClose(func() {
			if currentFile != nil {
				currentFile.Close()
				bs.mu.Lock()
				bs.DoneFiles++
				bs.mu.Unlock()
			}
			log.Printf("bulk: receiver done — %d files, %d bytes", bs.DoneFiles, bs.DoneBytes)
			if bs.OnDone != nil {
				bs.OnDone()
			}
		})
	})

	// Set remote offer
	err = pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  sdp,
	})
	if err != nil {
		bs.doError("set remote offer: " + err.Error())
		return
	}

	bs.mu.Lock()
	bs.remoteSet = true
	pending := bs.pendingICE
	bs.pendingICE = nil
	bs.mu.Unlock()
	for _, c := range pending {
		pc.AddICECandidate(c)
	}

	// Create answer
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		bs.doError("create answer: " + err.Error())
		return
	}
	pc.SetLocalDescription(answer)

	bs.sendWS("bulk.accept", map[string]any{
		"to":          bs.PeerID,
		"transfer_id": bs.TransferID,
		"sdp":         answer.SDP,
	})
}
