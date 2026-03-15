package voice

import (
	"math"
	"testing"
)

// generateSilence vytvoří frame s ticho (nulové vzorky).
func generateSilence(n int) []int16 {
	return make([]int16, n)
}

// generateTone vytvoří sinusový tón dané amplitudy.
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

// generateNoise vytvoří nízko-úrovňový šum (simulace mikrofonu).
func generateNoise(n int, amplitude float64) []int16 {
	samples := make([]int16, n)
	for i := range samples {
		// Deterministický "šum" pro reprodukovatelnost
		val := amplitude * math.Sin(float64(i)*0.7+float64(i*i)*0.003)
		samples[i] = int16(val)
	}
	return samples
}

// rms spočítá RMS hodnotu vzorků.
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
			t.Fatalf("vzorek %d by měl být 0, je %d", i, s)
		}
	}
}

func TestNoiseGate_LoudSignalPassesThrough(t *testing.T) {
	ng := NewNoiseGate()

	// Nejdřív pár tichých framů pro kalibraci noise floor
	for i := 0; i < 30; i++ {
		ng.Process(generateSilence(FrameSize))
	}

	// Hlasitý tón — měl by projít (po attack)
	tone := generateTone(FrameSize, 8000, 440, float64(SampleRate))
	originalRMS := rms(tone)

	// Procesovat několik framů aby se gate otevřel
	for i := 0; i < 5; i++ {
		ng.Process(generateTone(FrameSize, 8000, 440, float64(SampleRate)))
	}

	// Teď by měl gate být plně otevřený
	result := ng.Process(generateTone(FrameSize, 8000, 440, float64(SampleRate)))
	resultRMS := rms(result)

	// RMS by se nemělo výrazně lišit (gate je plně otevřený)
	ratio := resultRMS / originalRMS
	if ratio < 0.9 {
		t.Fatalf("hlasitý signál by měl projít beze změny, ratio=%.3f", ratio)
	}
}

func TestNoiseGate_LowNoiseIsSuppressed(t *testing.T) {
	ng := NewNoiseGate()

	// Kalibrační fáze — ticho
	for i := 0; i < 50; i++ {
		ng.Process(generateSilence(FrameSize))
	}

	// Nízký šum pod thresholdem
	noise := generateNoise(FrameSize, 50) // velmi tichý
	result := ng.Process(noise)

	// Výstup by měl být ztlumený (gate uzavřený)
	resultRMS := rms(result)
	if resultRMS > 0.001 {
		t.Fatalf("nízký šum by měl být potlačen, resultRMS=%.6f", resultRMS)
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
			t.Fatalf("při vypnutém gate by se vzorky neměly měnit, index %d: %d != %d", i, s, original[i])
		}
	}
}

func TestNoiseGate_GateOpensAndCloses(t *testing.T) {
	ng := NewNoiseGate()

	// Inicializace noise floor — ticho
	for i := 0; i < 50; i++ {
		ng.Process(generateSilence(FrameSize))
	}

	if ng.IsOpen() {
		t.Fatal("gate by měl být zavřený po tichu")
	}

	// Hlasitý signál — gate se otevře
	for i := 0; i < 5; i++ {
		ng.Process(generateTone(FrameSize, 10000, 440, float64(SampleRate)))
	}

	if !ng.IsOpen() {
		t.Fatal("gate by měl být otevřený po hlasitém signálu")
	}

	// Ticho — gate se zavře (po hold + release)
	for i := 0; i < 30; i++ {
		ng.Process(generateSilence(FrameSize))
	}

	if ng.IsOpen() {
		t.Fatal("gate by měl být zavřený po 30 tichých framech")
	}
}

func TestNoiseGate_Reset(t *testing.T) {
	ng := NewNoiseGate()

	// Otevřít gate
	for i := 0; i < 10; i++ {
		ng.Process(generateTone(FrameSize, 10000, 440, float64(SampleRate)))
	}

	if !ng.IsOpen() {
		t.Fatal("gate by měl být otevřený")
	}

	ng.Reset()

	if ng.IsOpen() {
		t.Fatal("gate by měl být zavřený po resetu")
	}
	if ng.frameCount != 0 {
		t.Fatal("frameCount by měl být 0 po resetu")
	}
}

func TestCalcFrameRMS(t *testing.T) {
	// Ticho
	silence := generateSilence(160)
	if r := calcFrameRMS(silence); r != 0 {
		t.Fatalf("RMS ticha by mělo být 0, je %f", r)
	}

	// Plná hlasitost
	full := make([]int16, 160)
	for i := range full {
		full[i] = 32767
	}
	r := calcFrameRMS(full)
	if r < 0.99 {
		t.Fatalf("RMS plné hlasitosti by mělo být ~1.0, je %f", r)
	}

	// Prázdný slice
	if r := calcFrameRMS(nil); r != 0 {
		t.Fatalf("RMS prázdného slice by mělo být 0, je %f", r)
	}
}
