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
	currentFrame atomic.Pointer[image.NRGBA]
	position     atomic.Int64 // milliseconds
	volume       atomic.Int32 // 0-200 (percent)
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

	// Pause/seek pozice
	pausedAt int64 // ms
}

// CheckFFmpeg vrátí cestu k ffmpeg nebo prázdný string.
func CheckFFmpeg() string {
	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		return ""
	}
	return path
}

// CheckFFprobe vrátí cestu k ffprobe nebo prázdný string.
func CheckFFprobe() string {
	path, err := exec.LookPath("ffprobe")
	if err != nil {
		return ""
	}
	return path
}

// FFmpegInstallHint vrátí platformově specifický návod na instalaci.
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

// NewPlayer vytvoří nový přehrávač.
func NewPlayer(invalidate func(), onError func(string)) *Player {
	p := &Player{
		invalidate: invalidate,
		onError:    onError,
		fps:        30,
	}
	p.volume.Store(100)
	return p
}

// LoadAndPlay spustí přehrávání z URL.
func (p *Player) LoadAndPlay(url string) {
	p.Stop()

	if CheckFFmpeg() == "" {
		p.state.Store(int32(StateError))
		if p.onError != nil {
			p.onError("ffmpeg not found. " + FFmpegInstallHint())
		}
		return
	}

	p.mu.Lock()
	p.url = url
	p.pausedAt = 0
	p.state.Store(int32(StateLoading))
	p.mu.Unlock()

	go p.probeAndStart(url, 0)
}

// ffprobe výstup
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
		// Pokračovat s defaults
		meta = Metadata{Width: 640, Height: 360, HasAudio: true}
	}

	p.mu.Lock()
	p.meta = meta

	// Omezit výstupní rozlišení na max 854x480
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
	// Zarovnat na sudé (ffmpeg vyžaduje)
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

func probeMetadata(url string) (Metadata, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "quiet",
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
	p.mu.Unlock()

	seekSec := fmt.Sprintf("%.3f", float64(seekMs)/1000.0)

	// Video stream
	videoCtx, videoCancel := context.WithCancel(context.Background())
	args := []string{
		"-hide_banner", "-loglevel", "error",
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
	videoCmd := exec.CommandContext(videoCtx, "ffmpeg", args...)
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

	// Audio stream (pokud existuje)
	if hasAudio {
		audioCtx, audioCancel := context.WithCancel(context.Background())
		audioArgs := []string{
			"-hide_banner", "-loglevel", "error",
		}
		if seekMs > 0 {
			audioArgs = append(audioArgs, "-ss", seekSec)
		}
		audioArgs = append(audioArgs,
			"-i", url,
			"-vn",
			"-f", "f32le",
			"-ar", "48000",
			"-ac", "2",
			"pipe:1",
		)
		audioCmd := exec.CommandContext(audioCtx, "ffmpeg", audioArgs...)
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

				// Audio player — Start se volá až po pre-bufferingu v readAudioLoop
				if p.audio == nil {
					p.audio = NewAudioPlayer()
				}
				vol := float32(p.volume.Load()) / 100.0
				p.audio.SetVolume(vol)

				go p.readAudioLoop(audioPipe, audioCtx)
			}
		}
	}

	p.state.Store(int32(StatePlaying))
	p.position.Store(seekMs)
	go p.readFrameLoop(videoPipe, videoCtx, w, h, fps, seekMs)
}

func (p *Player) readFrameLoop(pipe io.ReadCloser, ctx context.Context, w, h, fps int, startMs int64) {
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
			// Video skončilo — zastavit i audio stream
			p.state.Store(int32(StateIdle))
			go p.killStreams()
			if p.invalidate != nil {
				p.invalidate()
			}
			return
		}

		// Vytvořit NRGBA frame
		img := image.NewNRGBA(image.Rect(0, 0, w, h))
		copy(img.Pix, buf)

		p.currentFrame.Store(img)
		frameNum++
		p.position.Store(startMs + (frameNum * 1000 / int64(fps)))

		if p.invalidate != nil {
			p.invalidate()
		}
	}
}

func (p *Player) readAudioLoop(pipe io.ReadCloser, ctx context.Context) {
	// Větší čtecí chunky = méně syscallů, stabilnější tok dat
	// 100ms @ 48kHz stereo = 4800 frames * 2 ch = 9600 samplů * 4 bytes = 38400 bytes
	const readFrames = 4800
	const readSamples = readFrames * audioChannels
	const readBytes = readSamples * 4

	buf := make([]byte, readBytes)
	samples := make([]float32, readSamples)

	// Pre-buffering: 500ms (5 chunků po 100ms) — dostatečná rezerva
	// proti GC pauzám a síťové latenci
	const preBufferChunks = 5
	audioStarted := false
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

		// Konvertovat bytes na float32
		for i := range samples {
			bits := uint32(buf[i*4]) | uint32(buf[i*4+1])<<8 | uint32(buf[i*4+2])<<16 | uint32(buf[i*4+3])<<24
			samples[i] = math.Float32frombits(bits)
		}

		if !audioStarted {
			// Pre-buffering fáze: zapisovat bez blokování (buffer je prázdný)
			p.audio.Write(samples)
			chunksBuffered++
			if chunksBuffered >= preBufferChunks {
				if err := p.audio.Start(); err != nil {
					log.Printf("video: audio start failed: %v", err)
					return
				}
				audioStarted = true
			}
		} else {
			// Normální fáze: blokující zápis s notifikací od malgo callbacku.
			// Backpressure na ffmpeg pipe (ffmpeg zpomalí když buffer plný).
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

// Pause pozastaví přehrávání.
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

// Resume obnoví přehrávání.
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

// SeekTo přeskočí na pozici v ms.
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

// Stop zastaví přehrávání.
func (p *Player) Stop() {
	p.killStreams()
	p.state.Store(int32(StateIdle))
	p.currentFrame.Store(nil)
	p.position.Store(0)

	p.mu.Lock()
	p.url = ""
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

// Frame vrátí aktuální frame.
func (p *Player) Frame() *image.NRGBA {
	return p.currentFrame.Load()
}

// Position vrátí aktuální pozici v ms.
func (p *Player) Position() int64 {
	return p.position.Load()
}

// Duration vrátí délku videa.
func (p *Player) Duration() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.meta.Duration
}

// SetVolume nastaví hlasitost (0-200).
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

// GetState vrátí aktuální stav.
func (p *Player) GetState() State {
	return State(p.state.Load())
}

// Destroy uvolní resources.
func (p *Player) Destroy() {
	p.Stop()
	p.mu.Lock()
	if p.audio != nil {
		p.audio.Destroy()
		p.audio = nil
	}
	p.mu.Unlock()
}

// GenerateThumbnail extrahuje první frame videa jako NRGBA obrázek.
// Volat v goroutině.
func GenerateThumbnail(url string) *image.NRGBA {
	if CheckFFmpeg() == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Extrahovat 1 frame na pozici 1s (nebo 0s pro krátká videa)
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

	// Potřebujeme taky rozlišení — probe
	meta, err := probeMetadata(url)
	if err != nil {
		return nil
	}

	w := meta.Width
	h := meta.Height
	if w <= 0 || h <= 0 {
		return nil
	}

	// Přepočítat na max šířku 640
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

// DrawPlayButton nakreslí play trojúhelník na existující frame.
func DrawPlayButton(img *image.NRGBA) {
	bounds := img.Bounds()
	cx := bounds.Dx() / 2
	cy := bounds.Dy() / 2
	r := 20

	// Semi-transparentní kruh
	for y := cy - r - 5; y <= cy+r+5; y++ {
		for x := cx - r - 5; x <= cx+r+5; x++ {
			dx := x - cx
			dy := y - cy
			if dx*dx+dy*dy <= (r+5)*(r+5) {
				if x >= 0 && x < bounds.Dx() && y >= 0 && y < bounds.Dy() {
					existing := img.NRGBAAt(x, y)
					// Blend s tmavým overlay
					existing.R = uint8(int(existing.R) * 128 / 255)
					existing.G = uint8(int(existing.G) * 128 / 255)
					existing.B = uint8(int(existing.B) * 128 / 255)
					img.SetNRGBA(x, y, existing)
				}
			}
		}
	}

	// Bílý trojúhelník (play symbol)
	white := color.NRGBA{R: 255, G: 255, B: 255, A: 230}
	triSize := r * 2 / 3
	for y := cy - triSize; y <= cy+triSize; y++ {
		dy := y - cy
		// Šířka trojúhelníku na tomto řádku
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
