package video

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// State represents the player state.
type State int32

const (
	StateIdle    State = 0
	StateLoading State = 1
	StatePlaying State = 2
	StatePaused  State = 3
	StateError   State = 4
)

// Metadata holds video stream information.
type Metadata struct {
	Duration  time.Duration
	Width     int
	Height    int
	HasAudio  bool
}

// Player manages ffmpeg subprocesses for video/audio playback.
type Player struct {
	mu           sync.Mutex
	state        atomic.Int32
	meta         Metadata
	url          string
	audioURL     string    // separate audio URL (for adaptive YouTube formats)
	currentFrame atomic.Pointer[image.NRGBA]
	position     atomic.Int64 // milliseconds
	volume       atomic.Int32 // 0-200 (percent)
	buffering    atomic.Int32 // 1 = buffering, 0 = ready
	videoReady   atomic.Int32 // 1 = first video frame received
	invalidate   func()
	onError      func(string)

	videoCmd    *exec.Cmd
	audioCmd   *exec.Cmd
	videoCancel context.CancelFunc
	audioCancel context.CancelFunc

	outputWidth  int
	outputHeight int
	fps          int

	audio *AudioPlayer

	// Pause/seek position
	pausedAt int64 // ms
}

// CheckFFmpeg returns the path to ffmpeg or an empty string.
func CheckFFmpeg() string {
	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		return ""
	}
	return path
}

// CheckFFprobe returns the path to ffprobe or an empty string.
func CheckFFprobe() string {
	path, err := exec.LookPath("ffprobe")
	if err != nil {
		return ""
	}
	return path
}

// FFmpegInstallHint returns a platform-specific installation hint.
func FFmpegInstallHint() string {
	switch runtime.GOOS {
	case "windows":
		return "Install ffmpeg: winget install ffmpeg"
	case "darwin":
		return "Install ffmpeg: brew install ffmpeg"
	default:
		return "Install ffmpeg: sudo apt install ffmpeg"
	}
}

// NewPlayer creates a new player.
func NewPlayer(invalidate func(), onError func(string)) *Player {
	p := &Player{
		invalidate: invalidate,
		onError:    onError,
		fps:        30,
	}
	p.volume.Store(100)
	return p
}

// LoadAndPlay starts playback from a URL.
func (p *Player) LoadAndPlay(url string) {
	p.LoadAndPlayDual(url, "")
}

// LoadAndPlayDual starts playback with separate video and audio URLs.
// If audioURL is empty, audio is extracted from the video URL.
func (p *Player) LoadAndPlayDual(videoURL, audioURL string) {
	p.LoadAndPlayWithHints(videoURL, audioURL, 0, 0, 0, 0)
}

// LoadAndPlayWithHints starts playback with optional metadata hints.
// If width/height/duration are non-zero, ffprobe is skipped (useful for YouTube).
// seekMs allows resuming from a specific position.
func (p *Player) LoadAndPlayWithHints(videoURL, audioURL string, width, height int, duration time.Duration, seekMs int64) {
	// Kill running streams but keep last frame visible (overwritten by first new frame)
	p.killStreams()

	if CheckFFmpeg() == "" {
		p.state.Store(int32(StateError))
		if p.onError != nil {
			p.onError("ffmpeg not found. " + FFmpegInstallHint())
		}
		return
	}

	p.mu.Lock()
	p.url = videoURL
	p.audioURL = audioURL
	p.pausedAt = seekMs
	p.state.Store(int32(StateLoading))
	p.mu.Unlock()

	if width > 0 && height > 0 {
		meta := Metadata{
			Width:    width,
			Height:   height,
			Duration: duration,
			HasAudio: audioURL != "" || true, // assume audio present
		}
		go p.startWithMeta(videoURL, meta, seekMs)
	} else {
		go p.probeAndStart(videoURL, seekMs)
	}
}

func (p *Player) startWithMeta(url string, meta Metadata, seekMs int64) {
	p.mu.Lock()
	p.meta = meta

	w, h := meta.Width, meta.Height
	maxW, maxH := 1280, 720
	if w > maxW {
		h = h * maxW / w
		w = maxW
	}
	if h > maxH {
		w = w * maxH / h
		h = maxH
	}
	w = w &^ 1
	h = h &^ 1
	if w < 2 {
		w = 2
	}
	if h < 2 {
		h = 2
	}
	p.outputWidth = w
	p.outputHeight = h
	p.mu.Unlock()

	p.startStreams(url, seekMs)
}

// ffprobe output
type probeResult struct {
	Streams []probeStream `json:"streams"`
	Format  probeFormat   `json:"format"`
}

type probeStream struct {
	CodecType string `json:"codec_type"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

type probeFormat struct {
	Duration string `json:"duration"`
}

func (p *Player) probeAndStart(url string, seekMs int64) {
	// Probe metadata
	meta, err := probeMetadata(url)
	if err != nil {
		log.Printf("video: probe failed: %v", err)
		// Continue with defaults
		meta = Metadata{Width: 640, Height: 360, HasAudio: true}
	}

	p.mu.Lock()
	p.meta = meta

	// Limit output resolution to max 1280x720
	w, h := meta.Width, meta.Height
	if w <= 0 {
		w = 640
	}
	if h <= 0 {
		h = 360
	}
	maxW, maxH := 1280, 720
	if w > maxW {
		h = h * maxW / w
		w = maxW
	}
	if h > maxH {
		w = w * maxH / h
		h = maxH
	}
	// Align to even dimensions (ffmpeg requires it)
	w = w &^ 1
	h = h &^ 1
	if w < 2 {
		w = 2
	}
	if h < 2 {
		h = 2
	}
	p.outputWidth = w
	p.outputHeight = h
	p.mu.Unlock()

	p.startStreams(url, seekMs)
}

const ffUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

func probeMetadata(url string) (Metadata, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "quiet",
		"-user_agent", ffUserAgent,
		"-print_format", "json",
		"-show_streams", "-show_format",
		url,
	)
	out, err := cmd.Output()
	if err != nil {
		return Metadata{}, fmt.Errorf("ffprobe: %w", err)
	}

	var result probeResult
	if err := json.Unmarshal(out, &result); err != nil {
		return Metadata{}, fmt.Errorf("ffprobe json: %w", err)
	}

	var meta Metadata
	for _, s := range result.Streams {
		switch s.CodecType {
		case "video":
			meta.Width = s.Width
			meta.Height = s.Height
		case "audio":
			meta.HasAudio = true
		}
	}

	if result.Format.Duration != "" {
		if dur, err := strconv.ParseFloat(result.Format.Duration, 64); err == nil {
			meta.Duration = time.Duration(dur * float64(time.Second))
		}
	}

	return meta, nil
}

func (p *Player) startStreams(url string, seekMs int64) {
	p.mu.Lock()
	w := p.outputWidth
	h := p.outputHeight
	fps := p.fps
	hasAudio := p.meta.HasAudio
	separateAudioURL := p.audioURL
	p.mu.Unlock()

	if separateAudioURL != "" {
		hasAudio = true
	}

	seekSec := fmt.Sprintf("%.3f", float64(seekMs)/1000.0)

	isHTTP := strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://")

	// Video stream
	videoCtx, videoCancel := context.WithCancel(context.Background())
	args := []string{
		"-hide_banner", "-loglevel", "error",
	}
	if isHTTP {
		args = append(args, "-user_agent", ffUserAgent,
			"-reconnect", "1",
			"-reconnect_streamed", "1",
			"-reconnect_delay_max", "5")
	}
	if seekMs > 0 {
		args = append(args, "-ss", seekSec)
	}
	args = append(args,
		"-i", url,
		"-f", "rawvideo",
		"-pix_fmt", "rgba",
		"-vf", fmt.Sprintf("scale=%d:%d", w, h),
		"-r", strconv.Itoa(fps),
		"pipe:1",
	)
	log.Printf("video: starting ffmpeg video url=%s size=%dx%d", url, w, h)
	videoCmd := exec.CommandContext(videoCtx, "ffmpeg", args...)
	videoCmd.Stderr = os.Stderr
	videoPipe, err := videoCmd.StdoutPipe()
	if err != nil {
		videoCancel()
		p.setError("Failed to create video pipe: " + err.Error())
		return
	}

	if err := videoCmd.Start(); err != nil {
		videoCancel()
		p.setError("Failed to start ffmpeg video: " + err.Error())
		return
	}

	p.mu.Lock()
	p.videoCmd = videoCmd
	p.videoCancel = videoCancel
	p.mu.Unlock()

	// Audio stream
	audioStarted := false
	if hasAudio {
		audioInputURL := url
		if separateAudioURL != "" {
			audioInputURL = separateAudioURL
		}
		isAudioHTTP := strings.HasPrefix(audioInputURL, "http://") || strings.HasPrefix(audioInputURL, "https://")
		audioCtx, audioCancel := context.WithCancel(context.Background())
		audioArgs := []string{
			"-hide_banner", "-loglevel", "error",
		}
		if isAudioHTTP {
			audioArgs = append(audioArgs, "-user_agent", ffUserAgent,
				"-reconnect", "1",
				"-reconnect_streamed", "1",
				"-reconnect_delay_max", "5")
		}
		if seekMs > 0 {
			audioArgs = append(audioArgs, "-ss", seekSec)
		}
		audioArgs = append(audioArgs,
			"-i", audioInputURL,
			"-vn",
			"-f", "f32le",
			"-ar", "48000",
			"-ac", "2",
			"pipe:1",
		)
		audioCmd := exec.CommandContext(audioCtx, "ffmpeg", audioArgs...)
		audioCmd.Stderr = os.Stderr
		audioPipe, err := audioCmd.StdoutPipe()
		if err != nil {
			audioCancel()
			log.Printf("video: audio pipe failed: %v", err)
		} else {
			if err := audioCmd.Start(); err != nil {
				audioCancel()
				log.Printf("video: ffmpeg audio start failed: %v", err)
			} else {
				p.mu.Lock()
				p.audioCmd = audioCmd
				p.audioCancel = audioCancel
				p.mu.Unlock()

				if p.audio == nil {
					p.audio = NewAudioPlayer()
				}
				vol := float32(p.volume.Load()) / 100.0
				p.audio.SetVolume(vol)

				audioStarted = true
				go p.readAudioLoop(audioPipe, audioCtx)
			}
		}
	}

	p.videoReady.Store(0)
	p.buffering.Store(1)
	p.state.Store(int32(StatePlaying))
	p.position.Store(seekMs)
	go p.readFrameLoop(videoPipe, videoCtx, w, h, fps, seekMs, audioStarted)
}

func (p *Player) readFrameLoop(pipe io.ReadCloser, ctx context.Context, w, h, fps int, startMs int64, audioStarted bool) {
	frameSize := w * h * 4 // RGBA
	buf := make([]byte, frameSize)
	ticker := time.NewTicker(time.Second / time.Duration(fps))
	defer ticker.Stop()

	frameNum := int64(0)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		n, err := io.ReadFull(pipe, buf)
		if err != nil {
			if err != io.EOF && err != io.ErrUnexpectedEOF && !strings.Contains(err.Error(), "closed") {
				log.Printf("video: frame read error: %v (read %d/%d)", err, n, frameSize)
			}
			p.state.Store(int32(StateIdle))
			go p.killStreams()
			if p.invalidate != nil {
				p.invalidate()
			}
			return
		}

		img := image.NewNRGBA(image.Rect(0, 0, w, h))
		copy(img.Pix, buf)

		p.currentFrame.Store(img)
		frameNum++
		p.position.Store(startMs + (frameNum * 1000 / int64(fps)))

		// Signal first video frame arrived
		if frameNum == 1 {
			p.videoReady.Store(1)
			// If no audio stream, clear buffering now
			if !audioStarted {
				p.buffering.Store(0)
			}
		}

		if p.invalidate != nil {
			p.invalidate()
		}
	}
}

func (p *Player) readAudioLoop(pipe io.ReadCloser, ctx context.Context) {
	// Larger read chunks = fewer syscalls, more stable data flow
	// 100ms @ 48kHz stereo = 4800 frames * 2 ch = 9600 samples * 4 bytes = 38400 bytes
	const readFrames = 4800
	const readSamples = readFrames * audioChannels
	const readBytes = readSamples * 4

	buf := make([]byte, readBytes)
	samples := make([]float32, readSamples)

	// Pre-buffering: 500ms (5 chunks of 100ms) — sufficient reserve
	// against GC pauses and network latency
	const preBufferChunks = 5
	audioReady := false
	chunksBuffered := 0

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		n, err := io.ReadFull(pipe, buf)
		if err != nil {
			return
		}

		if p.audio == nil || n != readBytes {
			continue
		}

		// Convert bytes to float32
		for i := range samples {
			bits := uint32(buf[i*4]) | uint32(buf[i*4+1])<<8 | uint32(buf[i*4+2])<<16 | uint32(buf[i*4+3])<<24
			samples[i] = math.Float32frombits(bits)
		}

		if !audioReady {
			// Pre-buffering: write without blocking (buffer is empty)
			p.audio.Write(samples)
			chunksBuffered++
			if chunksBuffered >= preBufferChunks {
				// Wait for first video frame before starting audio playback
				for p.videoReady.Load() == 0 {
					select {
					case <-ctx.Done():
						return
					default:
						time.Sleep(5 * time.Millisecond)
					}
				}
				if err := p.audio.Start(); err != nil {
					log.Printf("video: audio start failed: %v", err)
					return
				}
				audioReady = true
				p.buffering.Store(0)
				if p.invalidate != nil {
					p.invalidate()
				}
			}
		} else {
			// Normal phase: blocking write with notification from malgo callback.
			if !p.audio.WriteBlocking(ctx, samples) {
				return // ctx cancelled (Pause/Stop)
			}
		}
	}
}

func (p *Player) setError(msg string) {
	p.state.Store(int32(StateError))
	if p.onError != nil {
		p.onError(msg)
	}
}

// Pause pauses playback.
func (p *Player) Pause() {
	if State(p.state.Load()) != StatePlaying {
		return
	}
	p.mu.Lock()
	p.pausedAt = p.position.Load()
	p.mu.Unlock()

	p.killStreams()
	p.state.Store(int32(StatePaused))
}

// Resume resumes playback.
func (p *Player) Resume() {
	if State(p.state.Load()) != StatePaused {
		return
	}
	p.mu.Lock()
	url := p.url
	pos := p.pausedAt
	p.state.Store(int32(StateLoading))
	p.mu.Unlock()

	go p.startStreams(url, pos)
}

// SeekTo seeks to a position in ms.
func (p *Player) SeekTo(posMs int64) {
	st := State(p.state.Load())
	if st != StatePlaying && st != StatePaused {
		return
	}

	p.killStreams()

	p.mu.Lock()
	url := p.url
	p.pausedAt = posMs
	p.position.Store(posMs)
	p.state.Store(int32(StateLoading))
	p.mu.Unlock()

	go p.startStreams(url, posMs)
}

// Stop stops playback.
func (p *Player) Stop() {
	p.killStreams()
	p.state.Store(int32(StateIdle))
	p.currentFrame.Store(nil)
	p.position.Store(0)

	p.mu.Lock()
	p.url = ""
	p.audioURL = ""
	p.pausedAt = 0
	p.meta = Metadata{}
	p.mu.Unlock()
}

func (p *Player) killStreams() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.videoCancel != nil {
		p.videoCancel()
		p.videoCancel = nil
	}
	if p.audioCancel != nil {
		p.audioCancel()
		p.audioCancel = nil
	}
	if p.videoCmd != nil {
		p.videoCmd.Wait()
		p.videoCmd = nil
	}
	if p.audioCmd != nil {
		p.audioCmd.Wait()
		p.audioCmd = nil
	}
	if p.audio != nil {
		p.audio.Stop()
	}
}

// Frame returns the current frame.
func (p *Player) Frame() *image.NRGBA {
	return p.currentFrame.Load()
}

// Position returns the current position in ms.
func (p *Player) Position() int64 {
	return p.position.Load()
}

// Duration returns the video duration.
func (p *Player) Duration() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.meta.Duration
}

// SetVolume sets the volume (0-200).
func (p *Player) SetVolume(v int) {
	if v < 0 {
		v = 0
	}
	if v > 200 {
		v = 200
	}
	p.volume.Store(int32(v))
	if p.audio != nil {
		p.audio.SetVolume(float32(v) / 100.0)
	}
}

// GetState returns the current state.
func (p *Player) GetState() State {
	return State(p.state.Load())
}

// Buffering returns true while audio pre-buffer is filling.
func (p *Player) Buffering() bool {
	return p.buffering.Load() != 0
}

// Destroy releases resources.
func (p *Player) Destroy() {
	p.Stop()
	p.mu.Lock()
	if p.audio != nil {
		p.audio.Destroy()
		p.audio = nil
	}
	p.mu.Unlock()
}

// GenerateThumbnail extracts the first frame of a video as an NRGBA image.
// Call in a goroutine.
func GenerateThumbnail(url string) *image.NRGBA {
	if CheckFFmpeg() == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Extract 1 frame at position 1s (or 0s for short videos)
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-hide_banner", "-loglevel", "error",
		"-ss", "1",
		"-i", url,
		"-frames:v", "1",
		"-vf", "scale=640:-2",
		"-f", "rawvideo",
		"-pix_fmt", "rgba",
		"pipe:1",
	)

	// We also need the resolution — probe
	meta, err := probeMetadata(url)
	if err != nil {
		return nil
	}

	w := meta.Width
	h := meta.Height
	if w <= 0 || h <= 0 {
		return nil
	}

	// Rescale to max width 640
	if w > 640 {
		h = h * 640 / w
		w = 640
	}
	w = w &^ 1
	h = h &^ 1
	if w < 2 || h < 2 {
		return nil
	}

	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	frameSize := w * h * 4
	if len(out) < frameSize {
		return nil
	}

	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	copy(img.Pix, out[:frameSize])

	return img
}

// DrawPlayButton draws a play triangle on an existing frame.
func DrawPlayButton(img *image.NRGBA) {
	bounds := img.Bounds()
	cx := bounds.Dx() / 2
	cy := bounds.Dy() / 2
	r := 20

	// Semi-transparent circle
	for y := cy - r - 5; y <= cy+r+5; y++ {
		for x := cx - r - 5; x <= cx+r+5; x++ {
			dx := x - cx
			dy := y - cy
			if dx*dx+dy*dy <= (r+5)*(r+5) {
				if x >= 0 && x < bounds.Dx() && y >= 0 && y < bounds.Dy() {
					existing := img.NRGBAAt(x, y)
					// Blend with dark overlay
					existing.R = uint8(int(existing.R) * 128 / 255)
					existing.G = uint8(int(existing.G) * 128 / 255)
					existing.B = uint8(int(existing.B) * 128 / 255)
					img.SetNRGBA(x, y, existing)
				}
			}
		}
	}

	// White triangle (play symbol)
	white := color.NRGBA{R: 255, G: 255, B: 255, A: 230}
	triSize := r * 2 / 3
	for y := cy - triSize; y <= cy+triSize; y++ {
		dy := y - cy
		// Triangle width at this row
		maxX := triSize - abs(dy)*triSize/triSize
		startX := cx - triSize/3
		for x := startX; x <= startX+maxX; x++ {
			if x >= 0 && x < bounds.Dx() && y >= 0 && y < bounds.Dy() {
				img.SetNRGBA(x, y, white)
			}
		}
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
