package screen

import (
	"fmt"
	"image"
	"io"
	"log"
	"os/exec"
	"sync"
	"time"
)

// Decoder obaluje ffmpeg process pro H.264 dekódování do raw RGBA framů.
type Decoder struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	width  int
	height int
	frames chan *image.NRGBA
	done   chan struct{}
	once   sync.Once
}

// NewDecoder spustí ffmpeg H.264 decoder.
// Vstup: H.264 Annex-B data přes stdin, výstup: raw RGBA frames přes stdout.
func NewDecoder(width, height int) (*Decoder, error) {
	cmd := exec.Command("ffmpeg",
		"-hide_banner", "-loglevel", "warning", "-nostats",
		"-flags", "+low_delay",
		"-fflags", "nobuffer",
		"-analyzeduration", "1000000",
		"-probesize", "1000000",
		"-f", "h264",
		"-i", "pipe:0",
		"-f", "rawvideo",
		"-pix_fmt", "rgba",
		"pipe:1",
	)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}
	go logStderr("decoder", stderr)

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
		return nil, fmt.Errorf("ffmpeg decoder start: %w", err)
	}

	d := &Decoder{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		width:  width,
		height: height,
		frames: make(chan *image.NRGBA, 5),
		done:   make(chan struct{}),
	}

	go d.readLoop()

	return d, nil
}

// WriteData posílá H.264 data do decoderu.
func (d *Decoder) WriteData(data []byte) error {
	_, err := d.stdin.Write(data)
	return err
}

// Frames vrací channel s dekódovanými framy.
func (d *Decoder) Frames() <-chan *image.NRGBA {
	return d.frames
}

// Close ukončí ffmpeg decoder process a uvolní zdroje.
func (d *Decoder) Close() {
	d.once.Do(func() {
		d.stdin.Close()
		select {
		case <-d.done:
		case <-time.After(3 * time.Second):
			log.Printf("screen decoder: ffmpeg not responding, killing")
			d.cmd.Process.Kill()
			<-d.done
		}
		d.cmd.Wait()
	})
}

// readLoop čte raw RGBA framy z ffmpeg stdout.
func (d *Decoder) readLoop() {
	defer close(d.done)
	defer close(d.frames)

	frameSize := d.width * d.height * 4
	buf := make([]byte, frameSize)

	for {
		_, err := io.ReadFull(d.stdout, buf)
		if err != nil {
			if err != io.EOF && err != io.ErrUnexpectedEOF {
				log.Printf("screen decoder: read error: %v", err)
			}
			return
		}

		frame := image.NewNRGBA(image.Rect(0, 0, d.width, d.height))
		copy(frame.Pix, buf)

		select {
		case d.frames <- frame:
		default:
			// Dropnout starší frame — vzít nový
			select {
			case <-d.frames:
			default:
			}
			select {
			case d.frames <- frame:
			default:
			}
		}
	}
}
