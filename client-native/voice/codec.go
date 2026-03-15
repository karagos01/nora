package voice

// Opus codec — pure Go (kazzmir/opus-go, ccgo-transpiled libopus, no CGO).
// 48kHz, mono, 20ms frames (960 samples), VoIP optimized.

import (
	"log"
	"sync"

	"github.com/kazzmir/opus-go/opus"
)

const (
	OpusSampleRate  = 48000 // Hz
	OpusChannels    = 1     // mono
	OpusFrameMs     = 20    // ms
	OpusFrameSize   = OpusSampleRate * OpusFrameMs / 1000 // 960 samples per frame
	OpusBitrate     = 32000 // bps (voice optimized, 24-64kbps range)
	OpusComplexity  = 5     // 0-10, 5 = good performance/quality tradeoff
	OpusMaxPacket   = 4000  // max Opus packet size in bytes
)

// OpusCodec holds an encoder and decoder for a single voice connection.
type OpusCodec struct {
	mu  sync.Mutex
	enc *opus.Encoder
	dec *opus.Decoder
}

// NewOpusCodec creates a new Opus encoder+decoder (48kHz, mono, VoIP).
func NewOpusCodec() (*OpusCodec, error) {
	enc, err := opus.NewEncoder(OpusSampleRate, OpusChannels, opus.ApplicationVoIP)
	if err != nil {
		return nil, err
	}
	if err := enc.SetBitrate(OpusBitrate); err != nil {
		enc.Close()
		return nil, err
	}
	if err := enc.SetComplexity(OpusComplexity); err != nil {
		log.Printf("opus: set complexity failed (non-fatal): %v", err)
	}

	dec, err := opus.NewDecoder(OpusSampleRate, OpusChannels)
	if err != nil {
		enc.Close()
		return nil, err
	}

	return &OpusCodec{enc: enc, dec: dec}, nil
}

// Encode encodes 960 PCM int16 samples (20ms @ 48kHz) into an Opus packet.
// Returns the Opus packet (bytes) or an error.
func (c *OpusCodec) Encode(pcm []int16) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	packet := make([]byte, OpusMaxPacket)
	n, err := c.enc.Encode(pcm, OpusFrameSize, packet)
	if err != nil {
		return nil, err
	}
	return packet[:n], nil
}

// Decode decodes an Opus packet into PCM int16 samples.
// Returns decoded samples (960 for 20ms @ 48kHz) or an error.
func (c *OpusCodec) Decode(packet []byte) ([]int16, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	pcm := make([]int16, OpusFrameSize)
	n, err := c.dec.Decode(packet, pcm, OpusFrameSize, false)
	if err != nil {
		return nil, err
	}
	return pcm[:n], nil
}

// Close releases the encoder and decoder.
func (c *OpusCodec) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.enc != nil {
		c.enc.Close()
		c.enc = nil
	}
	if c.dec != nil {
		c.dec.Close()
		c.dec = nil
	}
}

// EncodeSilence returns an Opus packet representing silence (960 zero samples).
func (c *OpusCodec) EncodeSilence() ([]byte, error) {
	silence := make([]int16, OpusFrameSize)
	return c.Encode(silence)
}
