package ui

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"image/color"
	"log"
	"math"

	"gioui.org/font/gofont"
	"gioui.org/font/opentype"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

//go:embed NotoEmoji.ttf
var notoEmojiTTF []byte

//go:embed fonts/GoMono-Regular.ttf
var goMonoTTF []byte

// NORA dark theme colors
var (
	ColorBg       = color.NRGBA{R: 26, G: 26, B: 46, A: 255}   // #1a1a2e
	ColorSidebar  = color.NRGBA{R: 22, G: 22, B: 42, A: 255}   // #16162a
	ColorCard     = color.NRGBA{R: 32, G: 32, B: 56, A: 255}   // #202038
	ColorInput    = color.NRGBA{R: 38, G: 38, B: 66, A: 255}   // #262642
	ColorAccent      = color.NRGBA{R: 124, G: 92, B: 191, A: 255} // #7c5cbf
	ColorAccentHover = color.NRGBA{R: 148, G: 116, B: 215, A: 255} // lighter accent for hover
	ColorText     = color.NRGBA{R: 224, G: 224, B: 224, A: 255} // #e0e0e0
	ColorTextDim  = color.NRGBA{R: 140, G: 140, B: 160, A: 255} // #8c8ca0
	ColorOnline     = color.NRGBA{R: 80, G: 200, B: 120, A: 255}   // green
	ColorOffline    = color.NRGBA{R: 100, G: 100, B: 120, A: 255} // gray
	ColorStatusAway = color.NRGBA{R: 240, G: 180, B: 40, A: 255}  // yellow
	ColorStatusDND  = color.NRGBA{R: 230, G: 70, B: 70, A: 255}   // red
	ColorDanger   = color.NRGBA{R: 220, G: 60, B: 60, A: 255}   // red
	ColorDivider  = color.NRGBA{R: 50, G: 50, B: 80, A: 255}    // #323250
	ColorHover    = color.NRGBA{R: 50, G: 50, B: 82, A: 255}    // #323252
	ColorSelected = color.NRGBA{R: 50, G: 45, B: 80, A: 255}    // #322d50
	ColorSuccess  = color.NRGBA{R: 80, G: 200, B: 120, A: 255}  // green (= ColorOnline)
	ColorWarning  = color.NRGBA{R: 240, G: 180, B: 40, A: 255}  // yellow
	ColorAccentDim = color.NRGBA{R: 100, G: 72, B: 160, A: 255} // darker accent
	ColorWhite     = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
)

type Theme struct {
	Material    *material.Theme
	FontScale   float32 // 0.7-1.6, default 1.0
	CompactMode bool    // IRC-style compact messages (name:content on one line)
}

// Sp returns a unit.Sp scaled by the font scale factor.
func (t *Theme) Sp(base float32) unit.Sp {
	s := t.FontScale
	if s == 0 {
		s = 1.0
	}
	return unit.Sp(base * s)
}

// ApplyFontScale applies font scale to the material theme.
func (t *Theme) ApplyFontScale(scale float32) {
	if scale < 0.7 {
		scale = 0.7
	}
	if scale > 1.6 {
		scale = 1.6
	}
	t.FontScale = scale
	t.Material.TextSize = unit.Sp(16 * scale)
}

func NewNORATheme() *Theme {
	// Emoji font (monochrome Noto Emoji — outline glyphs, Gio can render them)
	var emojiFaces []text.FontFace
	if faces, err := opentype.ParseCollection(notoEmojiTTF); err == nil {
		emojiFaces = faces
	} else {
		log.Printf("warning: failed to parse emoji font: %v", err)
	}

	th := material.NewTheme()
	th.Palette.Bg = ColorBg
	th.Palette.Fg = ColorText
	th.Palette.ContrastBg = ColorAccent
	th.Palette.ContrastFg = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	// Monospace font (Go Mono — for code blocks)
	var monoFaces []text.FontFace
	if faces, err := opentype.ParseCollection(goMonoTTF); err == nil {
		// Set font variant for identification
		for i := range faces {
			faces[i].Font.Typeface = "Go Mono"
		}
		monoFaces = faces
	} else {
		log.Printf("warning: failed to parse mono font: %v", err)
	}

	allFaces := append(gofont.Collection(), emojiFaces...)
	allFaces = append(allFaces, monoFaces...)
	th.Shaper = text.NewShaper(text.WithCollection(allFaces))
	return &Theme{Material: th}
}

// UserColor — deterministic HSL color from username (compatible with JS client utils/color.ts)
func UserColor(username string) color.NRGBA {
	h := sha256.Sum256([]byte(username))
	hue := float64(uint(h[0])<<8|uint(h[1])) / 65535.0 * 360.0
	r, g, b := hslToRGB(hue, 0.7, 0.65)
	return color.NRGBA{R: r, G: g, B: b, A: 255}
}

func hslToRGB(h, s, l float64) (uint8, uint8, uint8) {
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := l - c/2

	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}

	return uint8((r + m) * 255), uint8((g + m) * 255), uint8((b + m) * 255)
}

// FormatTime formats time in 24h format (HH:MM)
func FormatTime(t interface{ Format(string) string }) string {
	return t.Format("15:04")
}

// FormatDate formats a date
func FormatDate(t interface{ Format(string) string }) string {
	return t.Format("02.01.2006")
}

// FormatDateTime formats date and time
func FormatDateTime(t interface{ Format(string) string }) string {
	return fmt.Sprintf("%s %s", t.Format("02.01."), t.Format("15:04"))
}
