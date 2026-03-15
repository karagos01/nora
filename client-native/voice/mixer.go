package voice

import (
	"math"
	"sync"
)

// TrackBuffer holds per-user audio ring buffer and volume state.
type TrackBuffer struct {
	UserID string
	ring   *RingBuf
	Volume float32 // 0.0-2.0, default 1.0
	Level  float32 // RMS 0.0-1.0 (speaking detection)
}

// Mixer combines multiple remote audio tracks into one output.
type Mixer struct {
	mu     sync.Mutex
	tracks map[string]*TrackBuffer
	master float32 // master speaker volume, default 1.0
}

// NewMixer creates a new audio mixer.
func NewMixer() *Mixer {
	return &Mixer{
		tracks: make(map[string]*TrackBuffer),
		master: 1.0,
	}
}

// AddTrack adds a new track for the given user, returning the TrackBuffer.
// If a track already exists, it is returned as-is.
func (mx *Mixer) AddTrack(userID string) *TrackBuffer {
	mx.mu.Lock()
	defer mx.mu.Unlock()

	if t, ok := mx.tracks[userID]; ok {
		return t
	}
	t := &TrackBuffer{
		UserID: userID,
		ring:   NewRingBuf(48000), // 1s at 48kHz
		Volume: 1.0,
	}
	mx.tracks[userID] = t
	return t
}

// RemoveTrack removes a user's track.
func (mx *Mixer) RemoveTrack(userID string) {
	mx.mu.Lock()
	defer mx.mu.Unlock()
	delete(mx.tracks, userID)
}

// SetUserVolume sets per-user volume (0.0-2.0).
func (mx *Mixer) SetUserVolume(userID string, vol float32) {
	mx.mu.Lock()
	defer mx.mu.Unlock()
	if t, ok := mx.tracks[userID]; ok {
		t.Volume = vol
	}
}

// SetMasterVolume sets master output volume (0.0-2.0).
func (mx *Mixer) SetMasterVolume(vol float32) {
	mx.mu.Lock()
	defer mx.mu.Unlock()
	mx.master = vol
}

// Mix reads FrameSize samples from all tracks, mixes them, and writes to dst.
// Returns the number of samples written (always FrameSize or 0 if no tracks).
func (mx *Mixer) Mix(dst []int16) int {
	mx.mu.Lock()
	defer mx.mu.Unlock()

	if len(mx.tracks) == 0 {
		for i := range dst {
			dst[i] = 0
		}
		return len(dst)
	}

	n := len(dst)
	accum := make([]float32, n)
	tmp := make([]int16, n)

	for _, t := range mx.tracks {
		read := t.ring.Read(tmp[:n])
		if read == 0 {
			t.Level = 0
			continue
		}

		// Calculate RMS for speaking detection
		var sumSq float64
		for i := 0; i < read; i++ {
			s := float64(tmp[i])
			sumSq += s * s
		}
		rms := math.Sqrt(sumSq/float64(read)) / 32768.0
		t.Level = float32(rms)

		// Accumulate with per-user volume
		vol := t.Volume
		for i := 0; i < read; i++ {
			accum[i] += float32(tmp[i]) * vol
		}
	}

	// Apply master volume and clamp
	master := mx.master
	for i := 0; i < n; i++ {
		val := accum[i] * master
		if val > 32767 {
			val = 32767
		} else if val < -32768 {
			val = -32768
		}
		dst[i] = int16(val)
	}

	return n
}

// GetLevels returns a copy of per-user RMS levels (0.0-1.0).
func (mx *Mixer) GetLevels() map[string]float32 {
	mx.mu.Lock()
	defer mx.mu.Unlock()

	levels := make(map[string]float32, len(mx.tracks))
	for uid, t := range mx.tracks {
		levels[uid] = t.Level
	}
	return levels
}
