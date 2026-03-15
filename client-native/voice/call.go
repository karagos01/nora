package voice

import (
	"encoding/json"
	"log"
	"math"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

// CallState represents the state of a DM call.
type CallState int

const (
	CallIdle       CallState = iota
	CallRingingOut           // Calling the other party
	CallRingingIn            // Someone is calling me
	CallConnected            // Call in progress
)

const callRingTimeout = 30 * time.Second

// CallManager manages 1:1 DM calls (WebRTC + audio).
type CallManager struct {
	mu             sync.Mutex
	State          CallState
	PeerID         string // User ID of the other party
	ConversationID string // DM conversation ID
	StartTime      time.Time

	peer  *Peer
	audio *AudioManager
	mixer *Mixer
	codec *OpusCodec // Opus encoder/decoder

	StunURL string

	ringTimer *time.Timer

	Muted         bool
	Deafened      bool
	MicVolume     float32
	SpeakerVolume float32
	SelfSpeaking  bool
	PeerSpeaking  bool
	speakingThreshold float32

	sendWS       SendWSFunc
	invalidate   func()
	onLeaveVoice func() // Callback: leave voice channel before a call
	onRingStop   func() // Callback: stop the ring sound

	stopCapture chan struct{}
	stopMixer   chan struct{}
}

// NewCallManager creates a new CallManager with its own audio+mixer.
func NewCallManager(stunURL string, sendWS SendWSFunc, invalidate func(), onLeaveVoice func(), onRingStop func()) *CallManager {
	return &CallManager{
		State:             CallIdle,
		StunURL:           stunURL,
		audio:             NewAudioManager(),
		mixer:             NewMixer(),
		MicVolume:         1.0,
		SpeakerVolume:     1.0,
		speakingThreshold: 0.01,
		sendWS:            sendWS,
		invalidate:        invalidate,
		onLeaveVoice:      onLeaveVoice,
		onRingStop:        onRingStop,
	}
}

// IsActive returns true if a call is in progress (any state except idle).
func (c *CallManager) IsActive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.State != CallIdle
}

// GetState returns the current call state and peer ID.
func (c *CallManager) GetCallState() (CallState, string, string, time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.State, c.PeerID, c.ConversationID, c.StartTime
}

// StartCall initiates an outgoing call — leaves the voice channel, sends call.ring.
func (c *CallManager) StartCall(conversationID, peerID string) {
	c.mu.Lock()
	if c.State != CallIdle {
		c.mu.Unlock()
		return
	}
	c.State = CallRingingOut
	c.PeerID = peerID
	c.ConversationID = conversationID
	c.Muted = false
	c.Deafened = false
	c.mu.Unlock()

	// Leave voice channel (mutual exclusion)
	if c.onLeaveVoice != nil {
		c.onLeaveVoice()
	}

	c.sendWS("call.ring", map[string]string{
		"to":              peerID,
		"conversation_id": conversationID,
	})

	// 30s timeout
	c.mu.Lock()
	c.ringTimer = time.AfterFunc(callRingTimeout, func() {
		c.mu.Lock()
		if c.State == CallRingingOut {
			c.mu.Unlock()
			log.Printf("call: ring timeout, cancelling")
			if c.onRingStop != nil {
				c.onRingStop()
			}
			c.HangupCall()
			return
		}
		c.mu.Unlock()
	})
	c.mu.Unlock()

	if c.invalidate != nil {
		c.invalidate()
	}
}

// HandleRing processes an incoming call.ring — someone is calling us.
func (c *CallManager) HandleRing(from, conversationID string) {
	c.mu.Lock()
	if c.State != CallIdle {
		// Already in a call — decline
		c.mu.Unlock()
		c.sendWS("call.decline", map[string]string{
			"to": from,
		})
		return
	}
	c.State = CallRingingIn
	c.PeerID = from
	c.ConversationID = conversationID
	c.Muted = false
	c.Deafened = false

	// 30s auto-decline timer
	c.ringTimer = time.AfterFunc(callRingTimeout, func() {
		c.mu.Lock()
		if c.State == CallRingingIn {
			c.mu.Unlock()
			log.Printf("call: ring timeout, auto-declining")
			if c.onRingStop != nil {
				c.onRingStop()
			}
			c.DeclineCall()
			return
		}
		c.mu.Unlock()
	})
	c.mu.Unlock()

	// Leave voice channel
	if c.onLeaveVoice != nil {
		c.onLeaveVoice()
	}

	if c.invalidate != nil {
		c.invalidate()
	}
}

// AcceptCall accepts an incoming call — sends call.accept, starts audio, waits for offer.
func (c *CallManager) AcceptCall() {
	c.mu.Lock()
	if c.State != CallRingingIn {
		c.mu.Unlock()
		return
	}
	peerID := c.PeerID
	c.stopRingTimer()
	c.State = CallConnected
	c.StartTime = time.Now()
	c.mu.Unlock()

	c.sendWS("call.accept", map[string]string{
		"to": peerID,
	})

	c.startAudio()

	if c.invalidate != nil {
		c.invalidate()
	}
}

// DeclineCall declines an incoming call.
func (c *CallManager) DeclineCall() {
	c.mu.Lock()
	if c.State != CallRingingIn {
		c.mu.Unlock()
		return
	}
	peerID := c.PeerID
	c.stopRingTimer()
	c.reset()
	c.mu.Unlock()

	c.sendWS("call.decline", map[string]string{
		"to": peerID,
	})

	if c.invalidate != nil {
		c.invalidate()
	}
}

// HangupCall ends the call (outgoing ring or in-progress).
func (c *CallManager) HangupCall() {
	c.mu.Lock()
	if c.State == CallIdle {
		c.mu.Unlock()
		return
	}
	peerID := c.PeerID
	wasConnected := c.State == CallConnected
	c.stopRingTimer()
	c.reset()
	c.mu.Unlock()

	c.sendWS("call.hangup", map[string]string{
		"to": peerID,
	})

	if wasConnected {
		c.stopAudio(peerID)
	}

	if c.invalidate != nil {
		c.invalidate()
	}
}

// HandleAccept processes call.accept — peer accepted, starts audio and SDP offer.
func (c *CallManager) HandleAccept(from string) {
	c.mu.Lock()
	if c.State != CallRingingOut || c.PeerID != from {
		c.mu.Unlock()
		return
	}
	c.stopRingTimer()
	c.State = CallConnected
	c.StartTime = time.Now()
	c.mu.Unlock()

	c.startAudio()

	// Caller sends the offer
	go c.connectToPeer(from)

	if c.invalidate != nil {
		c.invalidate()
	}
}

// HandleDecline processes call.decline.
func (c *CallManager) HandleDecline(from string) {
	c.mu.Lock()
	if c.PeerID != from {
		c.mu.Unlock()
		return
	}
	c.stopRingTimer()
	c.reset()
	c.mu.Unlock()

	if c.invalidate != nil {
		c.invalidate()
	}
}

// HandleHangup processes call.hangup.
func (c *CallManager) HandleHangup(from string) {
	c.mu.Lock()
	if c.PeerID != from {
		c.mu.Unlock()
		return
	}
	peerID := c.PeerID
	wasConnected := c.State == CallConnected
	c.stopRingTimer()
	c.reset()
	c.mu.Unlock()

	if wasConnected {
		c.stopAudio(peerID)
	}

	if c.invalidate != nil {
		c.invalidate()
	}
}

// HandleOffer processes an incoming SDP offer (callee side).
func (c *CallManager) HandleOffer(from string, sdpStr string) {
	c.mu.Lock()
	if c.State != CallConnected || c.PeerID != from {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()

	peer, err := c.createPeer(from)
	if err != nil {
		log.Printf("call: create peer for offer: %v", err)
		return
	}

	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  sdpStr,
	}
	if err := peer.PC.SetRemoteDescription(offer); err != nil {
		log.Printf("call: set remote offer: %v", err)
		return
	}

	answer, err := peer.PC.CreateAnswer(nil)
	if err != nil {
		log.Printf("call: create answer: %v", err)
		return
	}
	if err := peer.PC.SetLocalDescription(answer); err != nil {
		log.Printf("call: set local answer: %v", err)
		return
	}

	c.sendWS("call.answer", map[string]string{
		"to":  from,
		"sdp": answer.SDP,
	})
}

// HandleAnswer processes an incoming SDP answer (caller side).
func (c *CallManager) HandleAnswer(from string, sdpStr string) {
	c.mu.Lock()
	peer := c.peer
	if peer == nil || c.PeerID != from {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()

	answer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  sdpStr,
	}
	if err := peer.PC.SetRemoteDescription(answer); err != nil {
		log.Printf("call: set remote answer from %s: %v", from, err)
	}
}

// HandleICE processes an incoming ICE candidate.
func (c *CallManager) HandleICE(from string, candidateJSON json.RawMessage) {
	c.mu.Lock()
	peer := c.peer
	if peer == nil || c.PeerID != from {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()

	var candidate webrtc.ICECandidateInit
	if err := json.Unmarshal(candidateJSON, &candidate); err != nil {
		log.Printf("call: parse ICE from %s: %v", from, err)
		return
	}
	if err := peer.PC.AddICECandidate(candidate); err != nil {
		log.Printf("call: add ICE from %s: %v", from, err)
	}
}

// GetMuteState returns the muted and deafened state.
func (c *CallManager) GetMuteState() (muted, deafened bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Muted, c.Deafened
}

// GetSpeakingState returns the self and peer speaking state.
func (c *CallManager) GetSpeakingState() (selfSpeaking, peerSpeaking bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.SelfSpeaking, c.PeerSpeaking
}

// ToggleMute toggles microphone mute.
func (c *CallManager) ToggleMute() {
	c.mu.Lock()
	c.Muted = !c.Muted
	c.mu.Unlock()
	if c.invalidate != nil {
		c.invalidate()
	}
}

// ToggleDeafen toggles deafen (also mutes).
func (c *CallManager) ToggleDeafen() {
	c.mu.Lock()
	c.Deafened = !c.Deafened
	if c.Deafened {
		c.Muted = true
	}
	c.mu.Unlock()
	if c.invalidate != nil {
		c.invalidate()
	}
}

// connectToPeer creates a WebRTC peer and sends an SDP offer.
func (c *CallManager) connectToPeer(userID string) {
	peer, err := c.createPeer(userID)
	if err != nil {
		log.Printf("call: create peer: %v", err)
		return
	}

	offer, err := peer.PC.CreateOffer(nil)
	if err != nil {
		log.Printf("call: create offer: %v", err)
		return
	}
	if err := peer.PC.SetLocalDescription(offer); err != nil {
		log.Printf("call: set local offer: %v", err)
		return
	}

	c.sendWS("call.offer", map[string]string{
		"to":  userID,
		"sdp": offer.SDP,
	})
}

// createPeer creates a PeerConnection for a 1:1 call.
func (c *CallManager) createPeer(userID string) (*Peer, error) {
	c.mu.Lock()
	if c.peer != nil {
		c.peer.PC.Close()
		c.peer = nil
	}
	c.mu.Unlock()

	config := webrtc.Configuration{}
	if c.StunURL != "" {
		config.ICEServers = []webrtc.ICEServer{
			{URLs: []string{c.StunURL}},
		}
	}

	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		return nil, err
	}

	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeOpus,
			ClockRate: 48000,
			Channels:  2, // Opus RTP always reports 2 channels (RFC 7587), actually mono
		},
		"audio", "nora-call",
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

	pc.OnICECandidate(func(cand *webrtc.ICECandidate) {
		if cand == nil {
			return
		}
		candidateJSON := cand.ToJSON()
		c.sendWS("call.ice", map[string]any{
			"to":            userID,
			"candidate":     candidateJSON.Candidate,
			"sdpMLineIndex": candidateJSON.SDPMLineIndex,
			"sdpMid":        candidateJSON.SDPMid,
		})
	})

	pc.OnTrack(func(remote *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		go c.playRemoteTrack(userID, remote)
	})

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("call: peer %s state: %s", userID, state)
		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			c.mu.Lock()
			if c.peer != nil && c.peer.UserID == userID {
				c.peer = nil
			}
			c.mu.Unlock()
		}
	})

	c.mu.Lock()
	c.peer = peer
	c.mu.Unlock()

	return peer, nil
}

// startAudio starts audio I/O and capture/mixer loops.
func (c *CallManager) startAudio() {
	// Create Opus codec
	codec, err := NewOpusCodec()
	if err != nil {
		log.Printf("call: opus codec init failed: %v", err)
		return
	}
	c.mu.Lock()
	c.codec = codec
	c.mu.Unlock()

	if err := c.audio.Start(); err != nil {
		log.Printf("call audio start: %v", err)
	}
	c.mixer.SetMasterVolume(c.SpeakerVolume)

	c.stopCapture = make(chan struct{})
	go c.captureLoop()

	c.stopMixer = make(chan struct{})
	go c.mixerLoop()
}

// stopAudio stops audio I/O and loops. peerID must be passed explicitly
// (after reset() c.PeerID is empty).
func (c *CallManager) stopAudio(peerID string) {
	if c.stopCapture != nil {
		select {
		case <-c.stopCapture:
		default:
			close(c.stopCapture)
		}
	}
	if c.stopMixer != nil {
		select {
		case <-c.stopMixer:
		default:
			close(c.stopMixer)
		}
	}

	c.mu.Lock()
	if c.peer != nil {
		c.peer.PC.Close()
		c.peer = nil
	}
	c.mu.Unlock()

	if peerID != "" {
		c.mixer.RemoveTrack(peerID)
	}
	c.audio.Stop()

	// Release Opus codec
	c.mu.Lock()
	if c.codec != nil {
		c.codec.Close()
		c.codec = nil
	}
	c.mu.Unlock()
}

// captureLoop reads mic, encodes Opus, writes to peer track.
func (c *CallManager) captureLoop() {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCapture:
			return
		case <-ticker.C:
			c.mu.Lock()
			muted := c.Muted
			micVol := c.MicVolume
			threshold := c.speakingThreshold
			peer := c.peer
			codec := c.codec
			c.mu.Unlock()

			if peer == nil || codec == nil {
				continue
			}

			samples := c.audio.ReadFrame()
			if samples == nil {
				continue
			}

			if micVol != 1.0 {
				applyVolume(samples, micVol)
			}

			selfLevel := calcRMS(samples)
			wasSpeaking := c.SelfSpeaking
			nowSpeaking := !muted && selfLevel > threshold

			c.mu.Lock()
			c.SelfSpeaking = nowSpeaking
			c.mu.Unlock()

			if wasSpeaking != nowSpeaking && c.invalidate != nil {
				c.invalidate()
			}

			// Encode to Opus
			var encoded []byte
			if muted {
				var err error
				encoded, err = codec.EncodeSilence()
				if err != nil {
					continue
				}
			} else {
				var err error
				encoded, err = codec.Encode(samples)
				if err != nil {
					log.Printf("call: opus encode: %v", err)
					continue
				}
			}

			peer.Track.WriteSample(media.Sample{
				Data:     encoded,
				Duration: 20 * time.Millisecond,
			})
		}
	}
}

// mixerLoop mixes remote tracks and writes to playback.
func (c *CallManager) mixerLoop() {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	mixed := make([]int16, FrameSize)

	for {
		select {
		case <-c.stopMixer:
			return
		case <-ticker.C:
			c.mu.Lock()
			deafened := c.Deafened
			threshold := c.speakingThreshold
			c.mu.Unlock()

			if deafened {
				continue
			}

			c.mixer.Mix(mixed)
			c.audio.PlaybackRing.Write(mixed)

			levels := c.mixer.GetLevels()
			changed := false

			c.mu.Lock()
			for _, level := range levels {
				speaking := level > threshold
				if c.PeerSpeaking != speaking {
					c.PeerSpeaking = speaking
					changed = true
				}
			}
			c.mu.Unlock()

			if changed && c.invalidate != nil {
				c.invalidate()
			}
		}
	}
}

// playRemoteTrack reads audio from a remote WebRTC track, decodes Opus, and writes to mixer.
func (c *CallManager) playRemoteTrack(userID string, remote *webrtc.TrackRemote) {
	track := c.mixer.AddTrack(userID)

	// Create per-track Opus decoder
	dec, err := NewOpusCodec()
	if err != nil {
		log.Printf("call: opus decoder for %s: %v", userID, err)
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
				log.Printf("call: opus decode from %s: %v", userID, err)
				continue
			}
			track.ring.Write(pcm)
		}
	}
}

// stopRingTimer stops the ring timer (must be called under lock).
func (c *CallManager) stopRingTimer() {
	if c.ringTimer != nil {
		c.ringTimer.Stop()
		c.ringTimer = nil
	}
}

// reset resets the state to idle (must be called under lock).
func (c *CallManager) reset() {
	c.State = CallIdle
	c.PeerID = ""
	c.ConversationID = ""
	c.StartTime = time.Time{}
	c.SelfSpeaking = false
	c.PeerSpeaking = false
}

// FormatCallDuration formats call duration as M:SS.
func FormatCallDuration(d time.Duration) string {
	m := int(d.Minutes())
	s := int(math.Mod(d.Seconds(), 60))
	if m > 0 {
		return itoa(m) + ":" + padZero(s)
	}
	return "0:" + padZero(s)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

func padZero(n int) string {
	if n < 10 {
		return "0" + itoa(n)
	}
	return itoa(n)
}
