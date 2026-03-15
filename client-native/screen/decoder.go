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

// Decoder wraps an ffmpeg process for H.264 decoding into raw RGBA frames.
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

// NewDecoder starts an ffmpeg H.264 decoder.
// Input: H.264 Annex-B data via stdin, output: raw RGBA frames via stdout.
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

// WriteData sends H.264 data to the decoder.
func (d *Decoder) WriteData(data []byte) error {
	_, err := d.stdin.Write(data)
	return err
}

// Frames returns a channel with decoded frames.
func (d *Decoder) Frames() <-chan *image.NRGBA {
	return d.frames
}

// Close terminates the ffmpeg decoder process and releases resources.
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

// readLoop reads raw RGBA frames from ffmpeg stdout.
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
			// Drop older frame — take the new one
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
