package voice

import (
	"math"
)

// NoiseGate implements a noise gate with adaptive noise floor estimation.
// Frames with RMS below the threshold are muted (fade-out), frames above the threshold
// pass through unchanged (fade-in). Transitions are smoothed using attack/release
// ramps to prevent clicking.
type NoiseGate struct {
	// Enabled determines whether the noise gate is active
	Enabled bool

	// Configurable parameters
	OpenThreshold  float32 // RMS level for opening the gate (0.0-1.0)
	CloseThreshold float32 // RMS level for closing the gate (0.0-1.0), typically lower than open

	// Attack/release in frame count (1 frame = 20ms)
	AttackFrames  int // how many frames the fade-in takes (gate open)
	ReleaseFrames int // how many frames the fade-out takes (gate close)
	HoldFrames    int // how many frames the gate stays open after dropping below threshold

	// Internal state
	gain         float32 // current gain 0.0-1.0
	holdCounter  int     // countdown for the hold phase
	noiseFloor   float64 // adaptive noise floor estimate (RMS)
	noiseAlpha   float64 // coefficient for EMA noise floor update
	frameCount   int     // number of processed frames (for initialization)
}

// NewNoiseGate creates a noise gate with reasonable default values.
// Parameters are optimized for voice chat at 8kHz, 20ms frames.
func NewNoiseGate() *NoiseGate {
	return &NoiseGate{
		Enabled:        true,
		OpenThreshold:  0.015, // ~-36dB — opens on speech
		CloseThreshold: 0.010, // ~-40dB — closes on quiet noise
		AttackFrames:   1,     // 20ms — fast attack for natural speech
		ReleaseFrames:  5,     // 100ms — slow release, no clipping of word endings
		HoldFrames:     15,    // 300ms — holds open after speech ends (pauses between words)
		gain:           0.0,
		noiseAlpha:     0.005, // slow noise floor adaptation
	}
}

// Process processes a single audio frame (typically 160 samples at 8kHz/20ms).
// If the gate is closed, returns muted samples (silence).
// If the gate is open, returns the original samples.
// Samples are modified in-place and the same slice is returned.
func (ng *NoiseGate) Process(samples []int16) []int16 {
	if !ng.Enabled || len(samples) == 0 {
		return samples
	}

	// Calculate RMS of this frame
	rms := calcFrameRMS(samples)

	ng.frameCount++

	// Adaptive noise floor — update only when the gate is closed
	// (so that loud speech doesn't raise the noise floor)
	if ng.gain < 0.1 {
		if ng.frameCount < 25 {
			// First 500ms: fast noise floor initialization
			ng.noiseFloor = ng.noiseFloor*0.8 + float64(rms)*0.2
		} else {
			// Slow adaptation
			ng.noiseFloor = ng.noiseFloor*(1-ng.noiseAlpha) + float64(rms)*ng.noiseAlpha
		}
	}

	// Dynamic threshold — above noise floor
	dynOpen := float32(ng.noiseFloor*2.5) + ng.OpenThreshold*0.5
	dynClose := float32(ng.noiseFloor*1.8) + ng.CloseThreshold*0.5

	// Minimum threshold (never below configured values)
	if dynOpen < ng.OpenThreshold {
		dynOpen = ng.OpenThreshold
	}
	if dynClose < ng.CloseThreshold {
		dynClose = ng.CloseThreshold
	}

	// Gate logic
	if rms >= dynOpen {
		// Above open threshold -> open gate, reset hold
		ng.holdCounter = ng.HoldFrames
		// Attack: fast gain increase
		if ng.AttackFrames > 0 {
			ng.gain += 1.0 / float32(ng.AttackFrames)
		} else {
			ng.gain = 1.0
		}
		if ng.gain > 1.0 {
			ng.gain = 1.0
		}
	} else if rms < dynClose {
		// Below close threshold
		if ng.holdCounter > 0 {
			// Hold phase — gate stays open
			ng.holdCounter--
		} else {
			// Release: gradual gain decrease
			if ng.ReleaseFrames > 0 {
				ng.gain -= 1.0 / float32(ng.ReleaseFrames)
			} else {
				ng.gain = 0.0
			}
			if ng.gain < 0.0 {
				ng.gain = 0.0
			}
		}
	}
	// Between close and open threshold — hysteresis, state doesn't change

	// Apply gain to samples
	if ng.gain <= 0.001 {
		// Complete silence — zero out samples
		for i := range samples {
			samples[i] = 0
		}
	} else if ng.gain < 0.999 {
		// Partial gain — fade
		g := ng.gain
		for i, s := range samples {
			samples[i] = int16(float32(s) * g)
		}
	}
	// gain >= 1.0 — samples pass through unchanged

	return samples
}

// IsOpen returns true if the gate is currently open (passing audio through).
func (ng *NoiseGate) IsOpen() bool {
	return ng.gain > 0.1
}

// Reset resets the internal gate state (on reconnect etc.).
func (ng *NoiseGate) Reset() {
	ng.gain = 0.0
	ng.holdCounter = 0
	ng.noiseFloor = 0
	ng.frameCount = 0
}

// calcFrameRMS calculates the RMS level of a frame normalized to 0.0-1.0.
func calcFrameRMS(samples []int16) float32 {
	if len(samples) == 0 {
		return 0
	}
	var sumSq float64
	for _, s := range samples {
		v := float64(s)
		sumSq += v * v
	}
	return float32(math.Sqrt(sumSq/float64(len(samples))) / 32768.0)
}
