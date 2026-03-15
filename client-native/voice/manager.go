package voice

import (
	"encoding/json"
	"log"
	"math"
	"net"
	"sync"
	"time"

	"nora-client/screen"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

// Peer represents a single WebRTC connection to another user.
type Peer struct {
	UserID   string
	PC       *webrtc.PeerConnection
	Track    *webrtc.TrackLocalStaticSample
	ScreenDC *webrtc.DataChannel
}

// SendWSFunc is a callback to send a WS event.
type SendWSFunc func(eventType string, payload any) error

// Manager handles voice channel connections, WebRTC peers, and audio.
type Manager struct {
	mu sync.Mutex

	ChannelID string
	UserID    string
	StunURL   string // STUN server URL from server config (empty = no STUN)
	Muted     bool
	Deafened  bool

	peers map[string]*Peer // remote userID -> Peer
	audio *AudioManager
	mixer *Mixer
	codec *OpusCodec // Opus encoder/decoder

	// Volume controls
	MicVolume     float32 // 0.0-2.0, default 1.0
	SpeakerVolume float32 // 0.0-2.0, default 1.0
	UserVolumes   map[string]float32 // per-user volume overrides

	// Noise suppression
	NoiseGate *NoiseGate

	// Speaking state
	SelfSpeaking bool
	SelfLevel    float32
	PeerLevels   map[string]float32
	PeerSpeaking map[string]bool
	speakingThreshold float32

	// Screen sharing
	Streaming     bool
	StreamViewers map[string]bool              // userID → watching
	OnScreenFrame func(from string, data []byte) // callback pro příchozí frames
	stopStream    chan struct{}

	// Stream settings (set before StartStream)
	StreamFPS     int // default 20
	StreamMaxW    int // default 1920
	StreamMaxH    int // default 1080
	StreamQuality int // 0=fast(crf23,ultrafast), 1=balanced(crf20,veryfast), 2=quality(crf18,veryfast)

	// H.264 encoder
	encoder       *screen.Encoder
	encoderWidth  int
	encoderHeight int
	useH264       bool

	sendWS     SendWSFunc
	invalidate func()

	stopCapture chan struct{}
	stopMixer   chan struct{}
}

// NewManager creates a VoiceManager.
func NewManager(userID, stunURL string, sendWS SendWSFunc, invalidate func()) *Manager {
	return &Manager{
		UserID:        userID,
		StunURL:       stunURL,
		peers:         make(map[string]*Peer),
		audio:         NewAudioManager(),
		mixer:         NewMixer(),
		NoiseGate:     NewNoiseGate(),
		MicVolume:     1.0,
		SpeakerVolume: 1.0,
		UserVolumes:   make(map[string]float32),
		PeerLevels:    make(map[string]float32),
		PeerSpeaking:  make(map[string]bool),
		speakingThreshold: 0.01,
		StreamFPS:     20,
		StreamMaxW:    1920,
		StreamMaxH:    1080,
		StreamQuality: 1, // balanced
		sendWS:        sendWS,
		invalidate:    invalidate,
	}
}

// Join sends voice.join to the server and starts audio.
func (m *Manager) Join(channelID string) {
	m.JoinWithOptions(channelID, "", "")
}

// JoinWithOptions sends voice.join with optional name and password (for lobby channels).
func (m *Manager) JoinWithOptions(channelID, name, password string) {
	m.mu.Lock()
	if m.ChannelID != "" {
		m.mu.Unlock()
		m.Leave()
		m.mu.Lock()
	}
	m.ChannelID = channelID
	speakerVol := m.SpeakerVolume
	m.mu.Unlock()

	payload := map[string]string{"channel_id": channelID}
	if name != "" {
		payload["name"] = name
	}
	if password != "" {
		payload["password"] = password
	}
	m.sendWS("voice.join", payload)

	// Vytvořit Opus codec
	codec, err := NewOpusCodec()
	if err != nil {
		log.Printf("voice: opus codec init failed: %v", err)
		return
	}
	m.mu.Lock()
	m.codec = codec
	m.mu.Unlock()

	// Start audio capture/playback
	if err := m.audio.Start(); err != nil {
		log.Printf("voice audio start: %v", err)
	}

	// Apply current speaker volume to mixer
	m.mixer.SetMasterVolume(speakerVol)

	// Start capture loop
	stopC := make(chan struct{})
	stopM := make(chan struct{})
	m.mu.Lock()
	m.stopCapture = stopC
	m.stopMixer = stopM
	m.mu.Unlock()

	go m.captureLoop()
	go m.mixerLoop()

	if m.invalidate != nil {
		m.invalidate()
	}
}

// Leave disconnects from the voice channel.
func (m *Manager) Leave() {
	m.mu.Lock()
	if m.ChannelID == "" {
		m.mu.Unlock()
		return
	}
	wasStreaming := m.Streaming
	channelID := m.ChannelID
	m.ChannelID = ""
	stopC := m.stopCapture
	m.stopCapture = nil
	stopM := m.stopMixer
	m.stopMixer = nil
	m.mu.Unlock()

	// Stop streaming if active
	if wasStreaming {
		m.StopStream()
		m.sendWS("screen.share", map[string]any{
			"channel_id": channelID,
			"sharing":    false,
		})
	}

	m.sendWS("voice.leave", map[string]string{})

	// Stop capture
	if stopC != nil {
		select {
		case <-stopC:
		default:
			close(stopC)
		}
	}
	if stopM != nil {
		select {
		case <-stopM:
		default:
			close(stopM)
		}
	}

	// Close all peer connections
	m.mu.Lock()
	for _, p := range m.peers {
		p.PC.Close()
	}
	m.peers = make(map[string]*Peer)
	m.SelfSpeaking = false
	m.SelfLevel = 0
	m.PeerSpeaking = make(map[string]bool)
	m.PeerLevels = make(map[string]float32)
	m.mu.Unlock()

	// Stop audio
	m.audio.Stop()

	// Uvolnit Opus codec
	m.mu.Lock()
	if m.codec != nil {
		m.codec.Close()
		m.codec = nil
	}
	m.mu.Unlock()

	if m.invalidate != nil {
		m.invalidate()
	}
}

// IsActive returns true if currently in a voice channel.
func (m *Manager) IsActive() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ChannelID != ""
}

// ToggleMute toggles microphone mute.
func (m *Manager) ToggleMute() {
	m.mu.Lock()
	m.Muted = !m.Muted
	m.mu.Unlock()
	if m.invalidate != nil {
		m.invalidate()
	}
}

// GetState returns current voice state (channelID, muted, deafened).
func (m *Manager) GetState() (string, bool, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ChannelID, m.Muted, m.Deafened
}

// ToggleDeafen toggles deafen (also mutes).
func (m *Manager) ToggleDeafen() {
	m.mu.Lock()
	m.Deafened = !m.Deafened
	if m.Deafened {
		m.Muted = true
	}
	m.mu.Unlock()
	if m.invalidate != nil {
		m.invalidate()
	}
}

// SetNoiseSuppression zapne/vypne noise suppression.
func (m *Manager) SetNoiseSuppression(enabled bool) {
	if m.NoiseGate != nil {
		m.NoiseGate.Enabled = enabled
	}
}

// IsNoiseSuppressionEnabled vrátí true pokud je noise suppression zapnutý.
func (m *Manager) IsNoiseSuppressionEnabled() bool {
	if m.NoiseGate != nil {
		return m.NoiseGate.Enabled
	}
	return false
}

// SetMicVolume sets microphone volume (0.0-2.0).
func (m *Manager) SetMicVolume(v float32) {
	m.mu.Lock()
	m.MicVolume = v
	m.mu.Unlock()
}

// SetSpeakerVolume sets speaker volume (0.0-2.0).
func (m *Manager) SetSpeakerVolume(v float32) {
	m.mu.Lock()
	m.SpeakerVolume = v
	m.mu.Unlock()
	m.mixer.SetMasterVolume(v)
}

// SetUserVolume sets per-user volume (0.0-2.0).
func (m *Manager) SetUserVolume(userID string, v float32) {
	m.mu.Lock()
	m.UserVolumes[userID] = v
	m.mu.Unlock()
	m.mixer.SetUserVolume(userID, v)
}

// GetSpeakingState returns self and peer speaking states.
func (m *Manager) GetSpeakingState() (selfSpeaking bool, peerSpeaking map[string]bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ps := make(map[string]bool, len(m.PeerSpeaking))
	for k, v := range m.PeerSpeaking {
		ps[k] = v
	}
	return m.SelfSpeaking, ps
}

// GetLevels returns self and peer audio levels.
func (m *Manager) GetLevels() (selfLevel float32, peerLevels map[string]float32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pl := make(map[string]float32, len(m.PeerLevels))
	for k, v := range m.PeerLevels {
		pl[k] = v
	}
	return m.SelfLevel, pl
}

// EnumDevices returns available audio input and output devices.
func (m *Manager) EnumDevices() (inputs, outputs []AudioDevice, err error) {
	return m.audio.EnumDevices()
}

// SetInputDevice sets the input device (empty = default). Takes effect on next Join.
func (m *Manager) SetInputDevice(id string) {
	m.audio.mu.Lock()
	m.audio.InputDeviceID = id
	m.audio.mu.Unlock()
}

// SetOutputDevice sets the output device (empty = default). Takes effect on next Join.
func (m *Manager) SetOutputDevice(id string) {
	m.audio.mu.Lock()
	m.audio.OutputDeviceID = id
	m.audio.mu.Unlock()
}

// HandleVoiceState processes a voice.state event.
// Connects to new peers and disconnects from departed ones.
func (m *Manager) HandleVoiceState(channelID string, users []string, joined, left string) {
	m.mu.Lock()
	myChannel := m.ChannelID
	m.mu.Unlock()

	if myChannel == "" || channelID != myChannel {
		return
	}

	// New user joined — I create an offer to them
	if joined != "" && joined != m.UserID {
		go m.connectToPeer(joined)
	}

	// User left — close their peer connection
	if left != "" && left != m.UserID {
		m.mu.Lock()
		if p, ok := m.peers[left]; ok {
			p.PC.Close()
			delete(m.peers, left)
		}
		m.mu.Unlock()
		m.mixer.RemoveTrack(left)
	}
}

// HandleOffer processes an incoming voice.offer from a remote peer.
func (m *Manager) HandleOffer(from string, sdpStr string) {
	m.mu.Lock()
	if m.ChannelID == "" {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()

	peer, err := m.createPeer(from)
	if err != nil {
		log.Printf("voice: create peer for offer: %v", err)
		return
	}

	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  sdpStr,
	}
	if err := peer.PC.SetRemoteDescription(offer); err != nil {
		log.Printf("voice: set remote offer: %v", err)
		return
	}

	answer, err := peer.PC.CreateAnswer(nil)
	if err != nil {
		log.Printf("voice: create answer: %v", err)
		return
	}
	if err := peer.PC.SetLocalDescription(answer); err != nil {
		log.Printf("voice: set local answer: %v", err)
		return
	}

	m.sendWS("voice.answer", map[string]string{
		"to":  from,
		"sdp": answer.SDP,
	})
}

// HandleAnswer processes an incoming voice.answer from a remote peer.
func (m *Manager) HandleAnswer(from string, sdpStr string) {
	m.mu.Lock()
	peer, ok := m.peers[from]
	m.mu.Unlock()
	if !ok {
		return
	}

	answer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  sdpStr,
	}
	if err := peer.PC.SetRemoteDescription(answer); err != nil {
		log.Printf("voice: set remote answer from %s: %v", from, err)
	}
}

// HandleICE processes an incoming ICE candidate from a remote peer.
func (m *Manager) HandleICE(from string, candidateJSON json.RawMessage) {
	m.mu.Lock()
	peer, ok := m.peers[from]
	m.mu.Unlock()
	if !ok {
		return
	}

	var candidate webrtc.ICECandidateInit
	if err := json.Unmarshal(candidateJSON, &candidate); err != nil {
		log.Printf("voice: parse ICE from %s: %v", from, err)
		return
	}
	if err := peer.PC.AddICECandidate(candidate); err != nil {
		log.Printf("voice: add ICE from %s: %v", from, err)
	}
}

// isPrivateCandidate kontroluje zda ICE candidate obsahuje private/loopback IP
func isPrivateCandidate(c *webrtc.ICECandidate) bool {
	addr := c.Address
	// Kandidáty typu relay (TURN) nikdy nefiltrovat
	if c.Typ == webrtc.ICECandidateTypeRelay {
		return false
	}
	ip := net.ParseIP(addr)
	if ip == nil {
		return false
	}
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()
}

// connectToPeer creates a WebRTC peer connection and sends an offer.
func (m *Manager) connectToPeer(userID string) {
	peer, err := m.createPeer(userID)
	if err != nil {
		log.Printf("voice: create peer: %v", err)
		return
	}

	offer, err := peer.PC.CreateOffer(nil)
	if err != nil {
		log.Printf("voice: create offer: %v", err)
		return
	}
	if err := peer.PC.SetLocalDescription(offer); err != nil {
		log.Printf("voice: set local offer: %v", err)
		return
	}

	m.sendWS("voice.offer", map[string]string{
		"to":  userID,
		"sdp": offer.SDP,
	})
}

// createPeer creates a new PeerConnection for a remote user.
func (m *Manager) createPeer(userID string) (*Peer, error) {
	// Close existing peer if any
	m.mu.Lock()
	if existing, ok := m.peers[userID]; ok {
		existing.PC.Close()
		delete(m.peers, userID)
	}
	m.mu.Unlock()

	config := webrtc.Configuration{}
	if m.StunURL != "" {
		config.ICEServers = []webrtc.ICEServer{
			{URLs: []string{m.StunURL}},
		}
	}

	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		return nil, err
	}

	// Přidat lokální audio track (Opus, 48kHz, mono)
	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeOpus,
			ClockRate: 48000,
			Channels:  2, // Opus RTP vždy hlásí 2 kanály (RFC 7587), reálně mono
		},
		"audio", "nora-voice",
	)
	if err != nil {
		pc.Close()
		return nil, err
	}
	if _, err := pc.AddTrack(track); err != nil {
		pc.Close()
		return nil, err
	}

	peer := &Peer{
		UserID: userID,
		PC:     pc,
		Track:  track,
	}

	// Pre-negotiated DataChannel for screen sharing (ID=0, no SDP renegotiation)
	negotiated := true
	dcID := uint16(0)
	dc, dcErr := pc.CreateDataChannel("screen", &webrtc.DataChannelInit{
		Negotiated: &negotiated,
		ID:         &dcID,
	})
	if dcErr != nil {
		log.Printf("voice: create screen DataChannel: %v", dcErr)
	} else {
		peer.ScreenDC = dc
		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			if m.OnScreenFrame != nil {
				m.OnScreenFrame(userID, msg.Data)
			}
		})
	}

	// Handle ICE candidates — filtrovat private IP adresy
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		if isPrivateCandidate(c) {
			return
		}
		candidateJSON := c.ToJSON()
		m.sendWS("voice.ice", map[string]any{
			"to":            userID,
			"candidate":     candidateJSON.Candidate,
			"sdpMLineIndex": candidateJSON.SDPMLineIndex,
			"sdpMid":        candidateJSON.SDPMid,
		})
	})

	// Handle incoming audio tracks
	pc.OnTrack(func(remote *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		go m.playRemoteTrack(userID, remote)
	})

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("voice: peer %s connection state: %s", userID, state)
		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			m.mu.Lock()
			delete(m.peers, userID)
			m.mu.Unlock()
			m.mixer.RemoveTrack(userID)
		}
	})

	m.mu.Lock()
	m.peers[userID] = peer
	m.mu.Unlock()

	return peer, nil
}

// captureLoop reads audio from microphone and writes to all peer tracks.
func (m *Manager) captureLoop() {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCapture:
			return
		case <-ticker.C:
			m.mu.Lock()
			muted := m.Muted
			micVol := m.MicVolume
			threshold := m.speakingThreshold
			m.mu.Unlock()

			samples := m.audio.ReadFrame()
			if samples == nil {
				continue
			}

			// Apply mic volume
			if micVol != 1.0 {
				applyVolume(samples, micVol)
			}

			// Noise gate — potlačí šum pod thresholdem
			if m.NoiseGate != nil {
				m.NoiseGate.Process(samples)
			}

			// Calculate self RMS for speaking detection
			selfLevel := calcRMS(samples)
			wasSpeaking := m.SelfSpeaking
			nowSpeaking := !muted && selfLevel > threshold

			m.mu.Lock()
			m.SelfLevel = selfLevel
			m.SelfSpeaking = nowSpeaking
			m.mu.Unlock()

			if wasSpeaking != nowSpeaking && m.invalidate != nil {
				m.invalidate()
			}

			// Encode do Opus
			var encoded []byte
			if muted {
				// Poslat ticho (Opus kódované nulové samples)
				var err error
				encoded, err = m.codec.EncodeSilence()
				if err != nil {
					continue
				}
			} else {
				var err error
				encoded, err = m.codec.Encode(samples)
				if err != nil {
					log.Printf("voice: opus encode: %v", err)
					continue
				}
			}

			// Write to all peer tracks
			m.mu.Lock()
			for _, p := range m.peers {
				p.Track.WriteSample(media.Sample{
					Data:     encoded,
					Duration: 20 * time.Millisecond,
				})
			}
			m.mu.Unlock()
		}
	}
}

// mixerLoop reads mixed audio from mixer and writes to playback ring buffer.
func (m *Manager) mixerLoop() {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	mixed := make([]int16, FrameSize)

	for {
		select {
		case <-m.stopMixer:
			return
		case <-ticker.C:
			m.mu.Lock()
			deafened := m.Deafened
			m.mu.Unlock()

			if deafened {
				continue
			}

			// Mix all tracks
			m.mixer.Mix(mixed)

			// Write to playback ring buffer
			m.audio.PlaybackRing.Write(mixed)

			// Update peer speaking states
			levels := m.mixer.GetLevels()
			threshold := m.speakingThreshold
			changed := false

			m.mu.Lock()
			for uid, level := range levels {
				m.PeerLevels[uid] = level
				speaking := level > threshold
				if m.PeerSpeaking[uid] != speaking {
					m.PeerSpeaking[uid] = speaking
					changed = true
				}
			}
			m.mu.Unlock()

			if changed && m.invalidate != nil {
				m.invalidate()
			}
		}
	}
}

// playRemoteTrack čte audio z remote WebRTC tracku, dekóduje Opus a zapisuje do mixeru.
func (m *Manager) playRemoteTrack(userID string, remote *webrtc.TrackRemote) {
	track := m.mixer.AddTrack(userID)

	// Aplikovat per-user volume pokud nastavený
	m.mu.Lock()
	if vol, ok := m.UserVolumes[userID]; ok {
		track.Volume = vol
	}
	m.mu.Unlock()

	// Vytvořit per-track Opus decoder
	dec, err := NewOpusCodec()
	if err != nil {
		log.Printf("voice: opus decoder for %s: %v", userID, err)
		return
	}
	defer dec.Close()

	buf := make([]byte, 1500)
	for {
		n, _, err := remote.Read(buf)
		if err != nil {
			return
		}

		if n > 0 {
			pcm, err := dec.Decode(buf[:n])
			if err != nil {
				log.Printf("voice: opus decode from %s: %v", userID, err)
				continue
			}
			track.ring.Write(pcm)
		}
	}
}

// StartStream begins capturing screen and sending frames to viewers.
// Pokusí se použít H.264 encoding přes ffmpeg, jinak fallback na JPEG.
func (m *Manager) StartStream(captureFn func() ([]byte, error)) {
	m.mu.Lock()
	if m.Streaming {
		m.mu.Unlock()
		return
	}
	m.Streaming = true
	m.StreamViewers = make(map[string]bool)
	m.stopStream = make(chan struct{})
	m.mu.Unlock()

	m.mu.Lock()
	fps := m.StreamFPS
	maxW := m.StreamMaxW
	maxH := m.StreamMaxH
	quality := m.StreamQuality
	m.mu.Unlock()
	if fps <= 0 {
		fps = 20
	}
	if maxW <= 0 {
		maxW = 1920
	}
	if maxH <= 0 {
		maxH = 1080
	}

	// Mapování kvality na CRF + preset
	crf, preset := streamQualityParams(quality)

	// Zkusit H.264 encoder
	if screen.FFmpegAvailable() {
		rgba, w, h, err := screen.CaptureRaw(maxW, maxH)
		if err == nil && len(rgba) > 0 {
			enc, err := screen.NewEncoder(w, h, fps, crf, preset)
			if err == nil {
				m.mu.Lock()
				m.encoder = enc
				m.encoderWidth = w
				m.encoderHeight = h
				m.useH264 = true
				m.mu.Unlock()

				log.Printf("screen: H.264 encoder started (%dx%d @ %dfps, crf=%d, preset=%s)", w, h, fps, crf, preset)
				go m.streamLoopH264(fps, maxW, maxH, crf, preset)
				go m.encoderSendLoop()

				if m.invalidate != nil {
					m.invalidate()
				}
				return
			}
			log.Printf("screen: H.264 encoder failed, fallback to JPEG: %v", err)
		}
	}

	// Fallback na JPEG
	log.Printf("screen: using JPEG fallback")
	go m.streamLoop(captureFn)

	if m.invalidate != nil {
		m.invalidate()
	}
}

// streamLoopH264 zachytává framy a posílá je do H.264 encoderu.
func (m *Manager) streamLoopH264(fps, maxW, maxH, crf int, preset string) {
	interval := time.Second / time.Duration(fps)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopStream:
			return
		case <-ticker.C:
			m.mu.Lock()
			hasViewers := len(m.StreamViewers) > 0
			enc := m.encoder
			encW := m.encoderWidth
			encH := m.encoderHeight
			m.mu.Unlock()

			if !hasViewers || enc == nil {
				continue
			}

			rgba, w, h, err := screen.CaptureRaw(maxW, maxH)
			if err != nil {
				log.Printf("screen capture: %v", err)
				continue
			}

			// Detekce změny rozlišení → restart encoder
			if w != encW || h != encH {
				log.Printf("screen: resolution changed %dx%d → %dx%d, restarting encoder", encW, encH, w, h)
				enc.Close()

				newEnc, err := screen.NewEncoder(w, h, fps, crf, preset)
				if err != nil {
					log.Printf("screen: encoder restart failed, stopping stream: %v", err)
					go m.stopStreamOnError()
					return
				}

				m.mu.Lock()
				m.encoder = newEnc
				m.encoderWidth = w
				m.encoderHeight = h
				m.mu.Unlock()

				// Poslat novou metadata všem viewerům
				m.sendScreenData(screen.EncodeMetadata(w, h, fps))

				// Nový encoder send loop
				go m.encoderSendLoop()

				enc = newEnc
			}

			if err := enc.WriteFrame(rgba); err != nil {
				// WriteFrame error je normální při StopStream (stdin closed)
				m.mu.Lock()
				streaming := m.Streaming
				m.mu.Unlock()
				if streaming {
					log.Printf("screen: encoder write failed, stopping stream: %v", err)
					go m.stopStreamOnError()
				}
				return
			}
		}
	}
}

// encoderSendLoop čte H.264 chunky z encoderu a posílá je viewerům.
func (m *Manager) encoderSendLoop() {
	m.mu.Lock()
	enc := m.encoder
	m.mu.Unlock()

	if enc == nil {
		return
	}

	for chunk := range enc.Chunks() {
		m.sendScreenData(screen.EncodeH264Chunk(chunk))
	}
}

// sendScreenData posílá typovaná data všem aktivním viewerům přes DataChannel.
func (m *Manager) sendScreenData(data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for uid := range m.StreamViewers {
		if p, ok := m.peers[uid]; ok && p.ScreenDC != nil {
			if p.ScreenDC.ReadyState() == webrtc.DataChannelStateOpen {
				p.ScreenDC.Send(data)
			}
		}
	}
}

func (m *Manager) streamLoop(captureFn func() ([]byte, error)) {
	ticker := time.NewTicker(100 * time.Millisecond) // ~10 fps
	defer ticker.Stop()

	for {
		select {
		case <-m.stopStream:
			return
		case <-ticker.C:
			m.mu.Lock()
			hasViewers := len(m.StreamViewers) > 0
			m.mu.Unlock()

			if !hasViewers {
				continue
			}

			data, err := captureFn()
			if err != nil {
				log.Printf("screen capture: %v", err)
				continue
			}
			m.SendScreenFrame(data)
		}
	}
}

// stopStreamOnError uklidí stream po fatální chybě encoderu.
func (m *Manager) stopStreamOnError() {
	m.mu.Lock()
	if !m.Streaming {
		m.mu.Unlock()
		return
	}
	channelID := m.ChannelID
	m.mu.Unlock()

	m.StopStream()

	if channelID != "" {
		m.sendWS("screen.share", map[string]any{
			"channel_id": channelID,
			"sharing":    false,
		})
	}
}

// StopStream stops screen capture.
func (m *Manager) StopStream() {
	m.mu.Lock()
	if !m.Streaming {
		m.mu.Unlock()
		return
	}
	m.Streaming = false
	m.StreamViewers = nil
	enc := m.encoder
	m.encoder = nil
	m.useH264 = false
	stopCh := m.stopStream
	m.stopStream = nil
	m.mu.Unlock()

	if stopCh != nil {
		select {
		case <-stopCh:
		default:
			close(stopCh)
		}
	}

	// Cleanup H.264 encoder
	if enc != nil {
		enc.Close()
	}

	if m.invalidate != nil {
		m.invalidate()
	}
}

// IsStreaming returns true if currently sharing screen.
func (m *Manager) IsStreaming() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Streaming
}

// AddViewer adds a viewer for screen sharing.
func (m *Manager) AddViewer(userID string) {
	m.mu.Lock()
	if m.StreamViewers == nil {
		m.StreamViewers = make(map[string]bool)
	}
	m.StreamViewers[userID] = true
	h264 := m.useH264
	w := m.encoderWidth
	h := m.encoderHeight
	fps := m.StreamFPS
	m.mu.Unlock()

	// Při H.264 módu poslat metadata novému viewerovi
	if h264 && w > 0 && h > 0 {
		meta := screen.EncodeMetadata(w, h, fps)
		m.mu.Lock()
		if p, ok := m.peers[userID]; ok && p.ScreenDC != nil {
			if p.ScreenDC.ReadyState() == webrtc.DataChannelStateOpen {
				p.ScreenDC.Send(meta)
			}
		}
		m.mu.Unlock()
	}
}

// RemoveViewer removes a viewer from screen sharing.
func (m *Manager) RemoveViewer(userID string) {
	m.mu.Lock()
	delete(m.StreamViewers, userID)
	m.mu.Unlock()
}

// SendScreenFrame sends a JPEG frame to all active viewers via DataChannel.
func (m *Manager) SendScreenFrame(data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for uid := range m.StreamViewers {
		if p, ok := m.peers[uid]; ok && p.ScreenDC != nil {
			if p.ScreenDC.ReadyState() == webrtc.DataChannelStateOpen {
				p.ScreenDC.Send(data)
			}
		}
	}
}

// Destroy cleans up all resources.
func (m *Manager) Destroy() {
	m.Leave()
}

// streamQualityParams vrací CRF a preset pro daný quality level.
func streamQualityParams(quality int) (crf int, preset string) {
	switch quality {
	case 0:
		return 23, "ultrafast"
	case 2:
		return 18, "veryfast"
	default: // 1 = balanced
		return 20, "veryfast"
	}
}

// applyVolume multiplies samples by volume factor in-place.
func applyVolume(samples []int16, vol float32) {
	for i, s := range samples {
		v := float32(s) * vol
		if v > 32767 {
			v = 32767
		} else if v < -32768 {
			v = -32768
		}
		samples[i] = int16(v)
	}
}

// calcRMS calculates root mean square level normalized to 0.0-1.0.
func calcRMS(samples []int16) float32 {
	if len(samples) == 0 {
		return 0
	}
	var sumSq float64
	for _, s := range samples {
		v := float64(s)
		sumSq += v * v
	}
	return float32(math.Sqrt(sumSq/float64(len(samples))) / 32768.0)
}
