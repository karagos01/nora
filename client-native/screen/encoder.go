package screen

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os/exec"
	"sync"
	"time"
)

// Encoder obaluje ffmpeg process pro H.264 kódování raw RGBA framů.
type Encoder struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	width  int
	height int
	fps    int
	chunks chan []byte
	done   chan struct{}
	once   sync.Once
}

// NewEncoder spustí ffmpeg H.264 encoder.
// Vstup: raw RGBA frames přes stdin, výstup: H.264 Annex-B přes stdout.
// crf: 0-51, nižší = lepší kvalita (18=výborná, 23=default). preset: ultrafast/veryfast/fast/...
func NewEncoder(width, height, fps int, crf int, preset string) (*Encoder, error) {
	if crf < 0 || crf > 51 {
		crf = 20
	}
	if preset == "" {
		preset = "veryfast"
	}
	cmd := exec.Command("ffmpeg",
		"-hide_banner", "-loglevel", "warning", "-nostats",
		"-f", "rawvideo",
		"-pix_fmt", "rgba",
		"-video_size", fmt.Sprintf("%dx%d", width, height),
		"-framerate", fmt.Sprintf("%d", fps),
		"-i", "pipe:0",
		"-c:v", "libx264",
		"-tune", "zerolatency",
		"-preset", preset,
		"-crf", fmt.Sprintf("%d", crf),
		"-pix_fmt", "yuv444p",
		"-g", fmt.Sprintf("%d", fps), // keyframe každou sekundu
		"-x264-params", "repeat-headers=1", // SPS/PPS s každým keyframem (pro mid-stream join)
		"-f", "h264",
		"pipe:1",
	)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}
	go logStderr("encoder", stderr)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("ffmpeg start: %w", err)
	}

	e := &Encoder{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		width:  width,
		height: height,
		fps:    fps,
		chunks: make(chan []byte, 30),
		done:   make(chan struct{}),
	}

	go e.readLoop()

	return e, nil
}

// WriteFrame pošle jeden RGBA frame do encoderu.
// Data musí mít přesně width*height*4 bytů.
func (e *Encoder) WriteFrame(rgba []byte) error {
	expected := e.width * e.height * 4
	if len(rgba) != expected {
		return fmt.Errorf("frame size mismatch: got %d, want %d", len(rgba), expected)
	}
	_, err := e.stdin.Write(rgba)
	return err
}

// Chunks vrací channel s H.264 chunky.
func (e *Encoder) Chunks() <-chan []byte {
	return e.chunks
}

// Close ukončí ffmpeg process a uvolní zdroje.
func (e *Encoder) Close() {
	e.once.Do(func() {
		e.stdin.Close()
		// Počkat na konec readLoop s timeoutem
		select {
		case <-e.done:
		case <-time.After(3 * time.Second):
			log.Printf("screen encoder: ffmpeg not responding, killing")
			e.cmd.Process.Kill()
			<-e.done
		}
		e.cmd.Wait()
	})
}

// readLoop čte H.264 data z ffmpeg stdout a posílá do chunks channelu.
func (e *Encoder) readLoop() {
	defer close(e.done)
	defer close(e.chunks)

	buf := make([]byte, 64*1024)
	for {
		n, err := e.stdout.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			select {
			case e.chunks <- chunk:
			default:
				log.Printf("screen encoder: chunk dropped (channel full)")
			}
		}
		if err != nil {
			return
		}
	}
}

// FFmpegAvailable zkontroluje jestli je ffmpeg dostupný v PATH.
func FFmpegAvailable() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

// logStderr čte stderr z ffmpeg procesu a loguje řádky.
func logStderr(label string, r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		log.Printf("screen %s: %s", label, scanner.Text())
	}
}
