package video

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gen2brain/malgo"
)

const (
	audioSampleRate = 48000
	audioChannels   = 2
	audioFrameSize  = 2048 // ~42ms @ 48kHz (větší = méně callbacků, méně šance na underrun)
	audioFadeLen    = 128  // samplů pro fade-in/fade-out při underrunu
)

// AudioRingBuf je thread-safe ring buffer pro float32 audio samply.
type AudioRingBuf struct {
	mu     sync.Mutex
	buf    []float32
	size   int
	r, w   int
	count  int
	notify chan struct{} // signalizace že je volné místo (pro WriteBlocking)
}

func NewAudioRingBuf(size int) *AudioRingBuf {
	return &AudioRingBuf{
		buf:    make([]float32, size),
		size:   size,
		notify: make(chan struct{}, 1),
	}
}

func (rb *AudioRingBuf) Write(samples []float32) int {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	n := len(samples)
	if n == 0 {
		return 0
	}

	written := 0
	for written < n {
		block := rb.size - rb.w
		remaining := n - written
		if block > remaining {
			block = remaining
		}

		copy(rb.buf[rb.w:rb.w+block], samples[written:written+block])
		written += block

		rb.w = (rb.w + block) % rb.size

		rb.count += block
		if rb.count > rb.size {
			overflow := rb.count - rb.size
			rb.r = (rb.r + overflow) % rb.size
			rb.count = rb.size
		}
	}
	return n
}

func (rb *AudioRingBuf) Read(dst []float32) int {
	rb.mu.Lock()

	n := len(dst)
	if n > rb.count {
		n = rb.count
	}
	if n == 0 {
		rb.mu.Unlock()
		return 0
	}

	read := 0
	for read < n {
		block := rb.size - rb.r
		remaining := n - read
		if block > remaining {
			block = remaining
		}
		copy(dst[read:read+block], rb.buf[rb.r:rb.r+block])
		read += block
		rb.r = (rb.r + block) % rb.size
	}
	rb.count -= n
	rb.mu.Unlock()

	// Signalizovat že je volné místo — probudí WriteBlocking
	select {
	case rb.notify <- struct{}{}:
	default:
	}
	return n
}

func (rb *AudioRingBuf) Available() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.count
}

func (rb *AudioRingBuf) Free() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.size - rb.count
}

func (rb *AudioRingBuf) Reset() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.r = 0
	rb.w = 0
	rb.count = 0
	// Vyprázdnit notify kanál
	select {
	case <-rb.notify:
	default:
	}
}

// AudioPlayer přehrává float32 stereo audio přes malgo.
type AudioPlayer struct {
	mu          sync.Mutex
	ctx         *malgo.AllocatedContext
	device      *malgo.Device
	ring        *AudioRingBuf
	volume      atomic.Int32 // 0-200
	running     bool
	playbackBuf []float32 // pre-alokovaný buffer pro malgo callback
	wasUnderrun bool      // sledování underrunu pro fade-in
}

func NewAudioPlayer() *AudioPlayer {
	ap := &AudioPlayer{
		ring:        NewAudioRingBuf(audioSampleRate * audioChannels * 10), // 10s buffer
		playbackBuf: make([]float32, audioFrameSize*audioChannels*4),
	}
	ap.volume.Store(100)
	return ap
}

// Start spustí malgo playback device.
func (ap *AudioPlayer) Start() error {
	ap.mu.Lock()
	defer ap.mu.Unlock()

	if ap.running {
		return nil
	}

	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return err
	}
	ap.ctx = ctx

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Playback)
	deviceConfig.Playback.Format = malgo.FormatF32
	deviceConfig.Playback.Channels = audioChannels
	deviceConfig.SampleRate = audioSampleRate
	deviceConfig.PeriodSizeInFrames = audioFrameSize
	deviceConfig.Periods = 4 // ~170ms hardware buffer

	callbacks := malgo.DeviceCallbacks{
		Data: func(outputSamples, inputSamples []byte, frameCount uint32) {
			ap.onPlaybackData(outputSamples, frameCount)
		},
	}

	device, err := malgo.InitDevice(ap.ctx.Context, deviceConfig, callbacks)
	if err != nil {
		ap.ctx.Uninit()
		ap.ctx = nil
		return err
	}

	if err := device.Start(); err != nil {
		device.Uninit()
		ap.ctx.Uninit()
		ap.ctx = nil
		return err
	}

	ap.device = device
	ap.running = true
	ap.wasUnderrun = false
	return nil
}

func (ap *AudioPlayer) onPlaybackData(outputSamples []byte, frameCount uint32) {
	n := int(frameCount) * audioChannels

	samples := ap.playbackBuf
	if n > len(samples) {
		samples = make([]float32, n)
	} else {
		samples = samples[:n]
	}

	read := ap.ring.Read(samples)

	// Aplikovat volume
	vol := float32(ap.volume.Load()) / 100.0
	for i := 0; i < read; i++ {
		s := samples[i] * vol
		if s > 1.0 {
			s = 1.0
		} else if s < -1.0 {
			s = -1.0
		}
		samples[i] = s
	}

	// Fade-in po underrunu — plynulý přechod z ticha na audio
	if ap.wasUnderrun && read > 0 {
		fadeLen := audioFadeLen
		if fadeLen > read {
			fadeLen = read
		}
		for i := 0; i < fadeLen; i++ {
			samples[i] *= float32(i) / float32(fadeLen)
		}
	}

	// Fade-out před underrunem — plynulý přechod na ticho (ne abruptní ořez)
	if read > 0 && read < n {
		fadeLen := audioFadeLen
		if fadeLen > read {
			fadeLen = read
		}
		fadeStart := read - fadeLen
		for i := 0; i < fadeLen; i++ {
			samples[fadeStart+i] *= float32(fadeLen-i) / float32(fadeLen)
		}
	}

	ap.wasUnderrun = read < n

	// Ticho pro zbytek
	for i := read; i < n; i++ {
		samples[i] = 0
	}

	// Konvertovat float32 → bytes (little-endian)
	for i := 0; i < n; i++ {
		off := i * 4
		if off+3 >= len(outputSamples) {
			break
		}
		bits := math.Float32bits(samples[i])
		outputSamples[off] = byte(bits)
		outputSamples[off+1] = byte(bits >> 8)
		outputSamples[off+2] = byte(bits >> 16)
		outputSamples[off+3] = byte(bits >> 24)
	}
}

// Write zapíše audio samply do ring bufferu (bez blokování).
func (ap *AudioPlayer) Write(samples []float32) {
	ap.ring.Write(samples)
}

// WriteBlocking zapíše samply a čeká dokud není v bufferu místo.
// Používá notifikace z malgo callbacku místo pollingu.
func (ap *AudioPlayer) WriteBlocking(ctx context.Context, samples []float32) bool {
	for {
		if ap.ring.Free() >= len(samples) {
			ap.ring.Write(samples)
			return true
		}
		// Čekat na signál od Read (malgo callback) nebo timeout
		select {
		case <-ctx.Done():
			return false
		case <-ap.ring.notify:
			// Malgo spotřebovalo data, zkusit znovu
		case <-time.After(100 * time.Millisecond):
			// Fallback timeout pro případ že notify přišlo dřív
		}
	}
}

// SetVolume nastaví hlasitost (0.0 - 2.0).
func (ap *AudioPlayer) SetVolume(v float32) {
	iv := int32(v * 100)
	if iv < 0 {
		iv = 0
	}
	if iv > 200 {
		iv = 200
	}
	ap.volume.Store(iv)
}

// Stop zastaví playback.
func (ap *AudioPlayer) Stop() {
	ap.mu.Lock()
	defer ap.mu.Unlock()

	if !ap.running {
		return
	}

	if ap.device != nil {
		ap.device.Uninit()
		ap.device = nil
	}
	if ap.ctx != nil {
		ap.ctx.Uninit()
		ap.ctx = nil
	}
	ap.ring.Reset()
	ap.running = false
	ap.wasUnderrun = false
}

// Destroy uvolní všechny resources.
func (ap *AudioPlayer) Destroy() {
	ap.Stop()
}
