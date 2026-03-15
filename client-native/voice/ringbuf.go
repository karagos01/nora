package voice

import "sync"

// RingBuf is a thread-safe ring buffer for int16 audio samples.
// Connects callback-based malgo with goroutine-based architecture.
type RingBuf struct {
	mu    sync.Mutex
	buf   []int16
	size  int
	r, w  int
	count int
}

// NewRingBuf creates a ring buffer with the given capacity in samples.
func NewRingBuf(size int) *RingBuf {
	return &RingBuf{
		buf:  make([]int16, size),
		size: size,
	}
}

// Write writes samples into the buffer, returning the number actually written.
// If the buffer is full, oldest samples are overwritten.
func (rb *RingBuf) Write(samples []int16) int {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	n := len(samples)
	for i := 0; i < n; i++ {
		rb.buf[rb.w] = samples[i]
		rb.w = (rb.w + 1) % rb.size
		if rb.count < rb.size {
			rb.count++
		} else {
			// Overwrite oldest — advance read pointer
			rb.r = (rb.r + 1) % rb.size
		}
	}
	return n
}

// Read reads up to len(dst) samples from the buffer, returning the number read.
func (rb *RingBuf) Read(dst []int16) int {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	n := len(dst)
	if n > rb.count {
		n = rb.count
	}
	for i := 0; i < n; i++ {
		dst[i] = rb.buf[rb.r]
		rb.r = (rb.r + 1) % rb.size
	}
	rb.count -= n
	return n
}

// Available returns the number of samples available for reading.
func (rb *RingBuf) Available() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.count
}
