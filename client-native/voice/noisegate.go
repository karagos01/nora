package voice

import (
	"math"
)

// NoiseGate implementuje noise gate s adaptivním noise floor odhadem.
// Framy s RMS pod thresholdem jsou ztlumeny (fade-out), framy nad thresholdem
// procházejí beze změny (fade-in). Přechody jsou vyhlazené pomocí attack/release
// ramp, aby se zabránilo klikání.
type NoiseGate struct {
	// Enabled určuje zda je noise gate aktivní
	Enabled bool

	// Konfigurovatelné parametry
	OpenThreshold  float32 // RMS úroveň pro otevření gate (0.0-1.0)
	CloseThreshold float32 // RMS úroveň pro zavření gate (0.0-1.0), typicky nižší než open

	// Attack/release v počtu framů (1 frame = 20ms)
	AttackFrames  int // kolik framů trvá fade-in (gate open)
	ReleaseFrames int // kolik framů trvá fade-out (gate close)
	HoldFrames    int // kolik framů zůstává gate otevřený po poklesu pod threshold

	// Vnitřní stav
	gain         float32 // aktuální gain 0.0-1.0
	holdCounter  int     // countdown pro hold fázi
	noiseFloor   float64 // adaptivní odhad noise floor (RMS)
	noiseAlpha   float64 // koeficient pro EMA noise floor update
	frameCount   int     // počet zpracovaných framů (pro inicializaci)
}

// NewNoiseGate vytvoří noise gate s rozumnými výchozími hodnotami.
// Parametry jsou optimalizované pro voice chat na 8kHz, 20ms framy.
func NewNoiseGate() *NoiseGate {
	return &NoiseGate{
		Enabled:        true,
		OpenThreshold:  0.015, // ~-36dB — otevře se při řeči
		CloseThreshold: 0.010, // ~-40dB — zavře se při tichém šumu
		AttackFrames:   1,     // 20ms — rychlý attack pro přirozenou řeč
		ReleaseFrames:  5,     // 100ms — pomalý release, žádné usekávání konců slov
		HoldFrames:     15,    // 300ms — drží otevřeno po konci řeči (pauzy mezi slovy)
		gain:           0.0,
		noiseAlpha:     0.005, // pomalá adaptace noise floor
	}
}

// Process zpracuje jeden audio frame (typicky 160 vzorků při 8kHz/20ms).
// Pokud je gate uzavřený, vrátí ztlumené vzorky (ticho).
// Pokud je gate otevřený, vrátí původní vzorky.
// Vzorky se modifikují in-place a vrátí se stejný slice.
func (ng *NoiseGate) Process(samples []int16) []int16 {
	if !ng.Enabled || len(samples) == 0 {
		return samples
	}

	// Spočítat RMS tohoto framu
	rms := calcFrameRMS(samples)

	ng.frameCount++

	// Adaptivní noise floor — aktualizovat jen když je gate zavřený
	// (aby hlasitá řeč nezvýšila noise floor)
	if ng.gain < 0.1 {
		if ng.frameCount < 25 {
			// Prvních 500ms: rychlá inicializace noise floor
			ng.noiseFloor = ng.noiseFloor*0.8 + float64(rms)*0.2
		} else {
			// Pomalá adaptace
			ng.noiseFloor = ng.noiseFloor*(1-ng.noiseAlpha) + float64(rms)*ng.noiseAlpha
		}
	}

	// Dynamický threshold — nad noise floor
	dynOpen := float32(ng.noiseFloor*2.5) + ng.OpenThreshold*0.5
	dynClose := float32(ng.noiseFloor*1.8) + ng.CloseThreshold*0.5

	// Minimální threshold (nikdy pod konfigurované hodnoty)
	if dynOpen < ng.OpenThreshold {
		dynOpen = ng.OpenThreshold
	}
	if dynClose < ng.CloseThreshold {
		dynClose = ng.CloseThreshold
	}

	// Gate logika
	if rms >= dynOpen {
		// Nad open threshold → otevřít gate, resetovat hold
		ng.holdCounter = ng.HoldFrames
		// Attack: rychlé zvýšení gain
		if ng.AttackFrames > 0 {
			ng.gain += 1.0 / float32(ng.AttackFrames)
		} else {
			ng.gain = 1.0
		}
		if ng.gain > 1.0 {
			ng.gain = 1.0
		}
	} else if rms < dynClose {
		// Pod close threshold
		if ng.holdCounter > 0 {
			// Hold fáze — gate zůstává otevřený
			ng.holdCounter--
		} else {
			// Release: postupné snížení gain
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
	// Mezi close a open threshold — hystereze, stav se nemění

	// Aplikovat gain na vzorky
	if ng.gain <= 0.001 {
		// Úplné ticho — vynulovat vzorky
		for i := range samples {
			samples[i] = 0
		}
	} else if ng.gain < 0.999 {
		// Částečný gain — fade
		g := ng.gain
		for i, s := range samples {
			samples[i] = int16(float32(s) * g)
		}
	}
	// gain >= 1.0 — vzorky procházejí beze změny

	return samples
}

// IsOpen vrátí true pokud je gate aktuálně otevřený (propouští zvuk).
func (ng *NoiseGate) IsOpen() bool {
	return ng.gain > 0.1
}

// Reset resetuje vnitřní stav gate (při reconnectu apod.).
func (ng *NoiseGate) Reset() {
	ng.gain = 0.0
	ng.holdCounter = 0
	ng.noiseFloor = 0
	ng.frameCount = 0
}

// calcFrameRMS spočítá RMS úroveň framu normalizovanou na 0.0-1.0.
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
