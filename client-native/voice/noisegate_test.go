package voice

import (
	"math"
	"testing"
)

// generateSilence creates a frame of silence (zero samples).
func generateSilence(n int) []int16 {
	return make([]int16, n)
}

// generateTone creates a sine tone of given amplitude.
func generateTone(n int, amplitude float64, freq float64, sampleRate float64) []int16 {
	samples := make([]int16, n)
	for i := range samples {
		t := float64(i) / sampleRate
		val := amplitude * math.Sin(2*math.Pi*freq*t)
		if val > 32767 {
			val = 32767
		} else if val < -32768 {
			val = -32768
		}
		samples[i] = int16(val)
	}
	return samples
}

// generateNoise creates low-level noise (microphone simulation).
func generateNoise(n int, amplitude float64) []int16 {
	samples := make([]int16, n)
	for i := range samples {
		// Deterministic "noise" for reproducibility
		val := amplitude * math.Sin(float64(i)*0.7+float64(i*i)*0.003)
		samples[i] = int16(val)
	}
	return samples
}

// rms calculates the RMS value of samples.
func rms(samples []int16) float64 {
	var sumSq float64
	for _, s := range samples {
		v := float64(s)
		sumSq += v * v
	}
	return math.Sqrt(sumSq/float64(len(samples))) / 32768.0
}

func TestNoiseGate_SilenceStaysSilent(t *testing.T) {
	ng := NewNoiseGate()
	silence := generateSilence(FrameSize)
	result := ng.Process(silence)

	for i, s := range result {
		if s != 0 {
			t.Fatalf("sample %d should be 0, got %d", i, s)
		}
	}
}

func TestNoiseGate_LoudSignalPassesThrough(t *testing.T) {
	ng := NewNoiseGate()

	// First a few silent frames for noise floor calibration
	for i := 0; i < 30; i++ {
		ng.Process(generateSilence(FrameSize))
	}

	// Loud tone — should pass through (after attack)
	tone := generateTone(FrameSize, 8000, 440, float64(SampleRate))
	originalRMS := rms(tone)

	// Process several frames for the gate to open
	for i := 0; i < 5; i++ {
		ng.Process(generateTone(FrameSize, 8000, 440, float64(SampleRate)))
	}

	// Now the gate should be fully open
	result := ng.Process(generateTone(FrameSize, 8000, 440, float64(SampleRate)))
	resultRMS := rms(result)

	// RMS should not differ significantly (gate is fully open)
	ratio := resultRMS / originalRMS
	if ratio < 0.9 {
		t.Fatalf("loud signal should pass through unchanged, ratio=%.3f", ratio)
	}
}

func TestNoiseGate_LowNoiseIsSuppressed(t *testing.T) {
	ng := NewNoiseGate()

	// Calibration phase — silence
	for i := 0; i < 50; i++ {
		ng.Process(generateSilence(FrameSize))
	}

	// Low noise below threshold
	noise := generateNoise(FrameSize, 50) // very quiet
	result := ng.Process(noise)

	// Output should be muted (gate closed)
	resultRMS := rms(result)
	if resultRMS > 0.001 {
		t.Fatalf("low noise should be suppressed, resultRMS=%.6f", resultRMS)
	}
}

func TestNoiseGate_DisabledPassesThrough(t *testing.T) {
	ng := NewNoiseGate()
	ng.Enabled = false

	noise := generateNoise(FrameSize, 200)
	original := make([]int16, len(noise))
	copy(original, noise)

	result := ng.Process(noise)

	for i, s := range result {
		if s != original[i] {
			t.Fatalf("with disabled gate samples should not change, index %d: %d != %d", i, s, original[i])
		}
	}
}

func TestNoiseGate_GateOpensAndCloses(t *testing.T) {
	ng := NewNoiseGate()

	// Noise floor initialization — silence
	for i := 0; i < 50; i++ {
		ng.Process(generateSilence(FrameSize))
	}

	if ng.IsOpen() {
		t.Fatal("gate should be closed after silence")
	}

	// Loud signal — gate opens
	for i := 0; i < 5; i++ {
		ng.Process(generateTone(FrameSize, 10000, 440, float64(SampleRate)))
	}

	if !ng.IsOpen() {
		t.Fatal("gate should be open after loud signal")
	}

	// Silence — gate closes (after hold + release)
	for i := 0; i < 30; i++ {
		ng.Process(generateSilence(FrameSize))
	}

	if ng.IsOpen() {
		t.Fatal("gate should be closed after 30 silent frames")
	}
}

func TestNoiseGate_Reset(t *testing.T) {
	ng := NewNoiseGate()

	// Open the gate
	for i := 0; i < 10; i++ {
		ng.Process(generateTone(FrameSize, 10000, 440, float64(SampleRate)))
	}

	if !ng.IsOpen() {
		t.Fatal("gate should be open")
	}

	ng.Reset()

	if ng.IsOpen() {
		t.Fatal("gate should be closed after reset")
	}
	if ng.frameCount != 0 {
		t.Fatal("frameCount should be 0 after reset")
	}
}

func TestCalcFrameRMS(t *testing.T) {
	// Silence
	silence := generateSilence(160)
	if r := calcFrameRMS(silence); r != 0 {
		t.Fatalf("RMS of silence should be 0, got %f", r)
	}

	// Full volume
	full := make([]int16, 160)
	for i := range full {
		full[i] = 32767
	}
	r := calcFrameRMS(full)
	if r < 0.99 {
		t.Fatalf("RMS of full volume should be ~1.0, got %f", r)
	}

	// Empty slice
	if r := calcFrameRMS(nil); r != 0 {
		t.Fatalf("RMS of empty slice should be 0, got %f", r)
	}
}
