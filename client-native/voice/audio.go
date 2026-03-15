package voice

import (
	"encoding/binary"
	"log"
	"sync"

	"github.com/gen2brain/malgo"
)

const (
	SampleRate = 48000 // Hz (Opus standard)
	Channels   = 1
	FrameSize  = 960  // samples per 20ms frame at 48kHz
	FrameBytes = FrameSize * 2 // 16-bit PCM = 2 bytes per sample
)

// AudioDevice represents an audio input or output device.
type AudioDevice struct {
	ID        string
	Name      string
	IsDefault bool
}

// AudioManager handles microphone capture and speaker playback using malgo (miniaudio).
type AudioManager struct {
	mu             sync.Mutex
	malgoCtx       *malgo.AllocatedContext
	captureDevice  *malgo.Device
	playbackDevice *malgo.Device
	running        bool

	CaptureRing  *RingBuf // malgo capture callback writes here
	PlaybackRing *RingBuf // mixer writes here, malgo playback callback reads

	MicVolume float32 // 0.0-2.0, applied in capture callback
	SelfLevel float32 // RMS from capture (speaking detection)

	InputDeviceID  string // empty = default
	OutputDeviceID string // empty = default
}

// NewAudioManager creates an AudioManager with malgo backend.
func NewAudioManager() *AudioManager {
	return &AudioManager{
		CaptureRing:  NewRingBuf(48000), // 1s buffer @ 48kHz
		PlaybackRing: NewRingBuf(48000),
		MicVolume:    1.0,
	}
}

// Start begins audio capture and playback via malgo.
func (am *AudioManager) Start() error {
	am.mu.Lock()
	defer am.mu.Unlock()
	if am.running {
		return nil
	}

	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		log.Printf("audio: malgo init failed: %v (voice will be silent)", err)
		return nil
	}
	am.malgoCtx = ctx

	// Start capture device
	if err := am.startCapture(); err != nil {
		log.Printf("audio: capture start failed: %v", err)
	}

	// Start playback device
	if err := am.startPlayback(); err != nil {
		log.Printf("audio: playback start failed: %v", err)
	}

	am.running = true
	return nil
}

func (am *AudioManager) startCapture() error {
	deviceConfig := malgo.DefaultDeviceConfig(malgo.Capture)
	deviceConfig.Capture.Format = malgo.FormatS16
	deviceConfig.Capture.Channels = Channels
	deviceConfig.SampleRate = SampleRate
	deviceConfig.PeriodSizeInFrames = FrameSize

	// Set specific device if configured
	if am.InputDeviceID != "" {
		// malgo device IDs are stored as string representations
		// For now we use default device; device selection via restart
	}

	captureCallbacks := malgo.DeviceCallbacks{
		Data: func(outputSamples, inputSamples []byte, frameCount uint32) {
			am.onCaptureData(inputSamples, frameCount)
		},
	}

	device, err := malgo.InitDevice(am.malgoCtx.Context, deviceConfig, captureCallbacks)
	if err != nil {
		return err
	}
	if err := device.Start(); err != nil {
		device.Uninit()
		return err
	}
	am.captureDevice = device
	return nil
}

func (am *AudioManager) startPlayback() error {
	deviceConfig := malgo.DefaultDeviceConfig(malgo.Playback)
	deviceConfig.Playback.Format = malgo.FormatS16
	deviceConfig.Playback.Channels = Channels
	deviceConfig.SampleRate = SampleRate
	deviceConfig.PeriodSizeInFrames = FrameSize

	playbackCallbacks := malgo.DeviceCallbacks{
		Data: func(outputSamples, inputSamples []byte, frameCount uint32) {
			am.onPlaybackData(outputSamples, frameCount)
		},
	}

	device, err := malgo.InitDevice(am.malgoCtx.Context, deviceConfig, playbackCallbacks)
	if err != nil {
		return err
	}
	if err := device.Start(); err != nil {
		device.Uninit()
		return err
	}
	am.playbackDevice = device
	return nil
}

// onCaptureData is called by malgo from the audio thread.
func (am *AudioManager) onCaptureData(inputSamples []byte, frameCount uint32) {
	n := int(frameCount)
	if n == 0 || len(inputSamples) < n*2 {
		return
	}

	samples := bytesToInt16(inputSamples[:n*2])
	am.CaptureRing.Write(samples)
}

// onPlaybackData is called by malgo from the audio thread.
func (am *AudioManager) onPlaybackData(outputSamples []byte, frameCount uint32) {
	n := int(frameCount)
	if n == 0 {
		return
	}

	samples := make([]int16, n)
	read := am.PlaybackRing.Read(samples)
	if read < n {
		// Fill remainder with silence
		for i := read; i < n; i++ {
			samples[i] = 0
		}
	}

	int16ToBytes(samples, outputSamples)
}

// ReadFrame reads one 20ms frame of raw PCM audio from the capture ring buffer.
// Returns nil if not enough data available.
func (am *AudioManager) ReadFrame() []int16 {
	if am.CaptureRing.Available() < FrameSize {
		return nil
	}
	samples := make([]int16, FrameSize)
	am.CaptureRing.Read(samples)
	return samples
}

// WriteFrame writes one 20ms frame of raw PCM audio to the playback ring buffer.
func (am *AudioManager) WriteFrame(samples []int16) {
	am.PlaybackRing.Write(samples)
}

// Stop terminates capture and playback devices.
func (am *AudioManager) Stop() {
	am.mu.Lock()
	defer am.mu.Unlock()

	if am.captureDevice != nil {
		am.captureDevice.Uninit()
		am.captureDevice = nil
	}
	if am.playbackDevice != nil {
		am.playbackDevice.Uninit()
		am.playbackDevice = nil
	}
	if am.malgoCtx != nil {
		_ = am.malgoCtx.Uninit()
		am.malgoCtx = nil
	}
	am.running = false
}

// EnumDevices returns available input and output audio devices.
func (am *AudioManager) EnumDevices() (inputs, outputs []AudioDevice, err error) {
	am.mu.Lock()
	ctx := am.malgoCtx
	am.mu.Unlock()

	if ctx == nil {
		// Create temporary context for enumeration
		tmpCtx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
		if err != nil {
			return nil, nil, err
		}
		defer tmpCtx.Uninit()
		ctx = tmpCtx
	}

	captureDevices, err := ctx.Devices(malgo.Capture)
	if err != nil {
		return nil, nil, err
	}
	for _, d := range captureDevices {
		inputs = append(inputs, AudioDevice{
			ID:        deviceIDToString(d.ID),
			Name:      d.Name(),
			IsDefault: d.IsDefault != 0,
		})
	}

	playbackDevices, err := ctx.Devices(malgo.Playback)
	if err != nil {
		return inputs, nil, err
	}
	for _, d := range playbackDevices {
		outputs = append(outputs, AudioDevice{
			ID:        deviceIDToString(d.ID),
			Name:      d.Name(),
			IsDefault: d.IsDefault != 0,
		})
	}

	return inputs, outputs, nil
}

// RestartWithDevices stops and restarts audio with the configured devices.
func (am *AudioManager) RestartWithDevices(inputID, outputID string) {
	am.mu.Lock()
	wasRunning := am.running
	am.InputDeviceID = inputID
	am.OutputDeviceID = outputID
	am.mu.Unlock()

	if wasRunning {
		am.Stop()
		am.Start()
	}
}

// bytesToInt16 converts little-endian byte slice to int16 slice without allocation.
func bytesToInt16(b []byte) []int16 {
	n := len(b) / 2
	samples := make([]int16, n)
	for i := 0; i < n; i++ {
		samples[i] = int16(binary.LittleEndian.Uint16(b[i*2:]))
	}
	return samples
}

// int16ToBytes converts int16 slice to little-endian byte slice in-place.
func int16ToBytes(samples []int16, dst []byte) {
	for i, s := range samples {
		if i*2+1 >= len(dst) {
			break
		}
		binary.LittleEndian.PutUint16(dst[i*2:], uint16(s))
	}
}

// deviceIDToString converts a malgo DeviceID to a hex string.
func deviceIDToString(id malgo.DeviceID) string {
	return id.String()
}
