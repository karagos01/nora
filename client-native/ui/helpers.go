package ui

import (
	"fmt"
	"image"
	"image/color"
	neturl "net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"

	"nora-client/api"
)

// layoutField — sdílený input field s labelem.
func layoutField(gtx layout.Context, th *Theme, label string, editor *widget.Editor) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(th.Material, label)
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Background{}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
					rr := gtx.Dp(8)
					paint.FillShape(gtx.Ops, ColorInput, clip.RRect{
						Rect: bounds,
						NE:   rr, NW: rr, SE: rr, SW: rr,
					}.Op(gtx.Ops))
					return layout.Dimensions{Size: bounds.Max}
				},
				func(gtx layout.Context) layout.Dimensions {
					return layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						ed := material.Editor(th.Material, editor, "")
						ed.Color = ColorText
						ed.HintColor = ColorTextDim
						return ed.Layout(gtx)
					})
				},
			)
		}),
	)
}

// layoutColoredBg — vyplní celou plochu barvou.
func layoutColoredBg(gtx layout.Context, c color.NRGBA) layout.Dimensions {
	paint.FillShape(gtx.Ops, c, clip.Rect{Max: gtx.Constraints.Max}.Op())
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

// FormatBytes formátuje velikost souboru.
func FormatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	}
	return fmt.Sprintf("%d B", b)
}

// removeWithRetry smaže soubor s retry pro Windows (antivirus/indexer drží handle).
func removeWithRetry(path string) {
	for i := 0; i < 5; i++ {
		if err := os.Remove(path); err == nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// --- Message formatting ---

type segStyle int

const (
	stylePlain segStyle = iota
	styleBold
	styleItalic
	styleCode
	styleCodeBlock
	styleLink
	styleEmoji
	styleMention
)

type styledSeg struct {
	text  string
	style segStyle
	lang  string // jazyk pro code block (syntax highlighting)
}

// parseDelimited splits text by delimiter pairs into styled segments.
func parseDelimited(text, delim string, style segStyle) []styledSeg {
	var result []styledSeg
	for {
		start := strings.Index(text, delim)
		if start == -1 {
			break
		}
		end := strings.Index(text[start+len(delim):], delim)
		if end == -1 {
			break
		}
		if start > 0 {
			result = append(result, styledSeg{text[:start], stylePlain, ""})
		}
		inner := text[start+len(delim) : start+len(delim)+end]
		if inner != "" {
			result = append(result, styledSeg{inner, style, ""})
		}
		text = text[start+len(delim)+end+len(delim):]
	}
	if text != "" {
		result = append(result, styledSeg{text, stylePlain, ""})
	}
	return result
}

// expandPlain applies a parser to all plain segments.
func expandPlain(segs []styledSeg, fn func(string) []styledSeg) []styledSeg {
	var result []styledSeg
	for _, s := range segs {
		if s.style == stylePlain {
			result = append(result, fn(s.text)...)
		} else {
			result = append(result, s)
		}
	}
	return result
}

func parseURLSegments(text string) []styledSeg {
	var result []styledSeg
	for {
		idx := -1
		for _, pfx := range []string{"https://", "http://"} {
			i := strings.Index(text, pfx)
			if i != -1 && (idx == -1 || i < idx) {
				idx = i
			}
		}
		if idx == -1 {
			break
		}
		if idx > 0 {
			result = append(result, styledSeg{text[:idx], stylePlain, ""})
		}
		end := strings.IndexAny(text[idx:], " \n\t")
		if end == -1 {
			result = append(result, styledSeg{text[idx:], styleLink, ""})
			text = ""
			break
		}
		result = append(result, styledSeg{text[idx : idx+end], styleLink, ""})
		text = text[idx+end:]
	}
	if text != "" {
		result = append(result, styledSeg{text, stylePlain, ""})
	}
	return result
}

func parseEmojiSegments(text string, emojiNames map[string]bool) []styledSeg {
	if len(emojiNames) == 0 {
		return []styledSeg{{text, stylePlain, ""}}
	}

	// Najít všechny emoji shody (:name: kde name je v emojiNames)
	type emojiMatch struct {
		start, end int
		name       string
	}
	var matches []emojiMatch
	pos := 0
	for pos < len(text) {
		idx := strings.Index(text[pos:], ":")
		if idx == -1 {
			break
		}
		absIdx := pos + idx
		endIdx := strings.Index(text[absIdx+1:], ":")
		if endIdx == -1 {
			break
		}
		name := text[absIdx+1 : absIdx+1+endIdx]
		if emojiNames[name] {
			matches = append(matches, emojiMatch{absIdx, absIdx + 1 + endIdx + 1, name})
			pos = absIdx + 1 + endIdx + 1
		} else {
			pos = absIdx + 1
		}
	}

	if len(matches) == 0 {
		return []styledSeg{{text, stylePlain, ""}}
	}

	// Rozdělit text jen na místech kde je skutečný emoji match
	var result []styledSeg
	prev := 0
	for _, m := range matches {
		if m.start > prev {
			result = append(result, styledSeg{text[prev:m.start], stylePlain, ""})
		}
		result = append(result, styledSeg{":" + m.name + ":", styleEmoji, ""})
		prev = m.end
	}
	if prev < len(text) {
		result = append(result, styledSeg{text[prev:], stylePlain, ""})
	}
	return result
}

func parseMentionSegments(text string, usernames map[string]bool) []styledSeg {
	if len(usernames) == 0 {
		return []styledSeg{{text, stylePlain, ""}}
	}
	var result []styledSeg
	for len(text) > 0 {
		idx := strings.Index(text, "@")
		if idx == -1 {
			break
		}
		// @ musí být na začátku nebo za mezerou/newline
		if idx > 0 {
			prev := text[idx-1]
			if prev != ' ' && prev != '\n' && prev != '\t' {
				result = append(result, styledSeg{text[:idx+1], stylePlain, ""})
				text = text[idx+1:]
				continue
			}
		}
		// Extrahovat username (vše za @ do mezery/newline/interpunkce/konce)
		rest := text[idx+1:]
		end := 0
		for end < len(rest) {
			c := rest[end]
			if c == ' ' || c == '\n' || c == '\t' || c == ',' || c == '.' || c == '!' || c == '?' || c == ':' || c == ';' || c == ')' || c == '(' {
				break
			}
			end++
		}
		if end == 0 {
			// Jen @ bez username
			result = append(result, styledSeg{text[:idx+1], stylePlain, ""})
			text = text[idx+1:]
			continue
		}
		name := rest[:end]
		if usernames[name] {
			if idx > 0 {
				result = append(result, styledSeg{text[:idx], stylePlain, ""})
			}
			result = append(result, styledSeg{"@" + name, styleMention, ""})
			text = rest[end:]
		} else {
			result = append(result, styledSeg{text[:idx+1+end], stylePlain, ""})
			text = rest[end:]
		}
	}
	if text != "" {
		result = append(result, styledSeg{text, stylePlain, ""})
	}
	return result
}

func parseFormattedText(text string, emojiNames map[string]bool, usernames map[string]bool) []styledSeg {
	// Stage 1: Code blocks (``` ... ```)
	segs := parseDelimited(text, "```", styleCodeBlock)
	for i, s := range segs {
		if s.style == styleCodeBlock {
			// Extrahovat language tag (```go, ```python, ...)
			if nl := strings.Index(s.text, "\n"); nl != -1 && nl < 20 && !strings.Contains(s.text[:nl], " ") {
				segs[i].lang = strings.TrimSpace(s.text[:nl])
				segs[i].text = s.text[nl+1:]
			}
			segs[i].text = strings.TrimRight(segs[i].text, "\n")
		}
	}
	// Stage 2: Inline code (` ... `)
	segs = expandPlain(segs, func(t string) []styledSeg { return parseDelimited(t, "`", styleCode) })
	// Stage 3: URLs
	segs = expandPlain(segs, parseURLSegments)
	// Stage 4: Bold (** ... **)
	segs = expandPlain(segs, func(t string) []styledSeg { return parseDelimited(t, "**", styleBold) })
	// Stage 5: Italic (* ... *)
	segs = expandPlain(segs, func(t string) []styledSeg { return parseDelimited(t, "*", styleItalic) })
	// Stage 6: Emoji (:name:)
	segs = expandPlain(segs, func(t string) []styledSeg { return parseEmojiSegments(t, emojiNames) })
	// Stage 7: Mentions (@username)
	segs = expandPlain(segs, func(t string) []styledSeg { return parseMentionSegments(t, usernames) })
	// Merge sousedních plain segmentů (emoji parser je může rozdělit na ":")
	segs = mergePlainSegs(segs)
	return segs
}

// mergePlainSegs spojí sousední plain segmenty do jednoho.
func mergePlainSegs(segs []styledSeg) []styledSeg {
	if len(segs) <= 1 {
		return segs
	}
	var result []styledSeg
	for _, s := range segs {
		if len(result) > 0 && result[len(result)-1].style == stylePlain && s.style == stylePlain {
			result[len(result)-1].text += s.text
		} else {
			result = append(result, s)
		}
	}
	return result
}

// MsgLinks holds clickable state for up to 4 links per message.
type MsgLinks struct {
	Btns [4]widget.Clickable
	URLs [4]string
	N    int
}

// MsgMentions holds clickable state for up to 4 mentions per message.
type MsgMentions struct {
	Btns    [4]widget.Clickable
	UserIDs [4]string
	N       int
}

// layoutMessageContent renders message text with formatting (bold, italic, code, links, emoji, mentions).
// If links is non-nil, URL segments become clickable.
// If sels is non-nil, text labels become selectable (for copy).
// If mentions is non-nil, @username segments become clickable.
func layoutMessageContent(gtx layout.Context, th *Theme, text string, emojis []api.CustomEmoji, links *MsgLinks, mentions *MsgMentions, usernameToID map[string]string, usernames map[string]bool, sels *[]widget.Selectable, app *App, serverURL string) layout.Dimensions {
	emojiNames := make(map[string]bool, len(emojis))
	emojiMap := make(map[string]string, len(emojis))
	for _, e := range emojis {
		emojiNames[e.Name] = true
		if e.URL != "" {
			emojiMap[e.Name] = e.URL
		}
	}

	segs := parseFormattedText(text, emojiNames, usernames)

	// Pre-allocate selectables (upper bound = len(segs))
	if sels != nil {
		for len(*sels) < len(segs) {
			*sels = append(*sels, widget.Selectable{})
		}
	}
	selIdx := 0

	// Reset mention counter
	if mentions != nil {
		mentions.N = 0
	}

	// Fast path: no formatting
	if len(segs) == 1 && segs[0].style == stylePlain {
		lbl := material.Body2(th.Material, text)
		lbl.Color = ColorText
		if sels != nil {
			lbl.State = &(*sels)[0]
		}
		return lbl.Layout(gtx)
	}

	// Check for code blocks — need vertical layout
	hasBlock := false
	for _, s := range segs {
		if s.style == styleCodeBlock {
			hasBlock = true
			break
		}
	}
	// Handle link clicks (před resetem N)
	if links != nil {
		for i := 0; i < links.N && i < 4; i++ {
			if links.Btns[i].Clicked(gtx) {
				go openURL(links.URLs[i])
			}
		}
		links.N = 0
	}

	if !hasBlock {
		return layoutInlineSegs(gtx, th, segs, links, mentions, usernameToID, sels, &selIdx, emojiMap, app, serverURL)
	}

	// Vertical layout: code blocks as separate rows, inline segments grouped
	var rows []layout.FlexChild
	var inline []styledSeg
	flush := func() {
		if len(inline) > 0 {
			run := make([]styledSeg, len(inline))
			copy(run, inline)
			rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layoutInlineSegs(gtx, th, run, links, mentions, usernameToID, sels, &selIdx, emojiMap, app, serverURL)
			}))
			inline = nil
		}
	}
	for _, s := range segs {
		if s.style == styleCodeBlock {
			flush()
			code := s.text
			lang := s.lang
			rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				si := -1
				if sels != nil {
					si = selIdx
					selIdx++
				}
				return layoutCodeBlockSeg(gtx, th, code, lang, sels, si)
			}))
		} else {
			inline = append(inline, s)
		}
	}
	flush()

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
}

func layoutInlineSegs(gtx layout.Context, th *Theme, segs []styledSeg, links *MsgLinks, mentions *MsgMentions, usernameToID map[string]string, sels *[]widget.Selectable, selIdx *int, emojiMap map[string]string, app *App, serverURL string) layout.Dimensions {
	// Zjistit, které emoji mají image (pro přeskočení selIdx)
	emojiHasImage := make(map[string]bool)
	if app != nil && serverURL != "" {
		for _, seg := range segs {
			if seg.style == styleEmoji {
				name := strings.TrimPrefix(strings.TrimSuffix(seg.text, ":"), ":")
				if _, ok := emojiMap[name]; ok {
					emojiHasImage[name] = true
				}
			}
		}
	}

	var items []layout.FlexChild
	for _, seg := range segs {
		s := seg
		// Přiřadit selectable index pro textové segmenty (ne linky, ne image emoji)
		mySelIdx := -1
		isImageEmoji := false
		if s.style == styleEmoji {
			name := strings.TrimPrefix(strings.TrimSuffix(s.text, ":"), ":")
			isImageEmoji = emojiHasImage[name]
		}
		if sels != nil && s.style != styleLink && s.style != styleMention && !isImageEmoji {
			mySelIdx = *selIdx
			*selIdx++
		}
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if s.style == styleCode {
				return layoutInlineCodeSeg(gtx, th, s.text, sels, mySelIdx)
			}
			if s.style == styleMention && mentions != nil && mentions.N < 4 {
				mIdx := mentions.N
				name := strings.TrimPrefix(s.text, "@")
				mentions.UserIDs[mIdx] = usernameToID[name]
				mentions.N++
				return mentions.Btns[mIdx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Body2(th.Material, s.text)
					lbl.Color = ColorAccent
					lbl.Font.Weight = font.Bold
					if mentions.Btns[mIdx].Hovered() {
						lbl.Color = ColorAccentHover
					}
					return lbl.Layout(gtx)
				})
			}
			if s.style == styleLink && links != nil && links.N < 4 {
				linkIdx := links.N
				links.URLs[linkIdx] = s.text
				links.N++
				return links.Btns[linkIdx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					if links.Btns[linkIdx].Hovered() {
						pointer.CursorPointer.Add(gtx.Ops)
					}
					lbl := material.Body2(th.Material, s.text)
					lbl.Color = ColorAccent
					if links.Btns[linkIdx].Hovered() {
						lbl.Color = ColorAccentHover
					}
					return lbl.Layout(gtx)
				})
			}
			// Custom emoji — render as image if available
			if s.style == styleEmoji && app != nil && serverURL != "" {
				name := strings.TrimPrefix(strings.TrimSuffix(s.text, ":"), ":")
				if url, ok := emojiMap[name]; ok {
					fullURL := serverURL + url
					ci := app.Images.Get(fullURL, func() { app.Window.Invalidate() })
					if ci != nil && ci.ok {
						h := gtx.Dp(20)
						imgBounds := ci.img.Bounds()
						imgW := imgBounds.Dx()
						imgH := imgBounds.Dy()
						w := h
						if imgH > 0 {
							w = h * imgW / imgH
						}
						scaleX := float32(w) / float32(imgW)
						scaleY := float32(h) / float32(imgH)
						defer clip.Rect{Max: image.Pt(w, h)}.Push(gtx.Ops).Pop()
						defer op.Affine(f32.Affine2D{}.Scale(f32.Pt(0, 0), f32.Pt(scaleX, scaleY))).Push(gtx.Ops).Pop()
						ci.op.Add(gtx.Ops)
						paint.PaintOp{}.Add(gtx.Ops)
						return layout.Dimensions{Size: image.Pt(w, h)}
					}
				}
			}
			lbl := material.Body2(th.Material, s.text)
			lbl.Color = ColorText
			if sels != nil && mySelIdx >= 0 && mySelIdx < len(*sels) {
				lbl.State = &(*sels)[mySelIdx]
			}
			switch s.style {
			case styleBold:
				lbl.Font.Weight = font.Bold
			case styleItalic:
				lbl.Font.Style = font.Italic
			case styleLink:
				lbl.Color = ColorAccent
			case styleEmoji:
				lbl.Color = ColorAccent
			case styleMention:
				lbl.Color = ColorAccent
				lbl.Font.Weight = font.Bold
			}
			return lbl.Layout(gtx)
		}))
	}
	return layout.Flex{}.Layout(gtx, items...)
}

func layoutCodeBlockSeg(gtx layout.Context, th *Theme, code, lang string, sels *[]widget.Selectable, si int) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				sz := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(4)
				paint.FillShape(gtx.Ops, ColorInput, clip.RRect{Rect: sz, NE: rr, NW: rr, SE: rr, SW: rr}.Op(gtx.Ops))
				return layout.Dimensions{Size: sz.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					// Syntax highlighting — pokud máme language tag
					if lang != "" {
						tokens := tokenizeCode(code, lang)
						if len(tokens) > 0 {
							return layoutHighlightedTokens(gtx, th, tokens, sels, si)
						}
					}
					// Fallback: mono font bez highlightingu
					lbl := material.Body2(th.Material, code)
					lbl.Color = ColorText
					lbl.Font.Typeface = "Go Mono"
					if sels != nil && si >= 0 && si < len(*sels) {
						lbl.State = &(*sels)[si]
					}
					return lbl.Layout(gtx)
				})
			},
		)
	})
}

func layoutInlineCodeSeg(gtx layout.Context, th *Theme, code string, sels *[]widget.Selectable, si int) layout.Dimensions {
	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			sz := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
			rr := gtx.Dp(3)
			paint.FillShape(gtx.Ops, ColorInput, clip.RRect{Rect: sz, NE: rr, NW: rr, SE: rr, SW: rr}.Op(gtx.Ops))
			return layout.Dimensions{Size: sz.Max}
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(4), Right: unit.Dp(4), Top: unit.Dp(1), Bottom: unit.Dp(1)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Body2(th.Material, code)
				lbl.Color = ColorText
				lbl.Font.Typeface = "Go Mono"
				if sels != nil && si >= 0 && si < len(*sels) {
					lbl.State = &(*sels)[si]
				}
				return lbl.Layout(gtx)
			})
		},
	)
}

// DisplayNameOf returns the display name of a user, falling back to username.
func DisplayNameOf(u *api.User) string {
	if u == nil {
		return "?"
	}
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Username
}

// openURL opens a URL in the system browser.
// Validuje schéma — povoluje pouze http, https.
func openURL(rawURL string) {
	u, err := neturl.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return
	}
	switch runtime.GOOS {
	case "linux":
		exec.Command("xdg-open", u.String()).Start()
	case "windows":
		exec.Command("cmd", "/c", "start", u.String()).Start()
	case "darwin":
		exec.Command("open", u.String()).Start()
	}
}

// copyToClipboard copies text to the system clipboard.
func copyToClipboard(text string) {
	switch runtime.GOOS {
	case "linux":
		cmd := exec.Command("xclip", "-selection", "clipboard")
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err != nil {
			cmd2 := exec.Command("xsel", "--clipboard", "--input")
			cmd2.Stdin = strings.NewReader(text)
			cmd2.Run()
		}
	case "windows":
		cmd := exec.Command("clip")
		cmd.Stdin = strings.NewReader(text)
		cmd.Run()
	case "darwin":
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(text)
		cmd.Run()
	}
}

// layoutAvatar renders a user avatar — either the actual image (if avatarURL is set and cached)
// or a colored circle with the user's initial as fallback.
func layoutAvatar(gtx layout.Context, app *App, username, avatarURL string, sizeDp unit.Dp) layout.Dimensions {
	size := gtx.Dp(sizeDp)
	rr := size / 2

	// Try to render actual avatar image
	if avatarURL != "" {
		conn := app.Conn()
		if conn != nil {
			fullURL := conn.URL + avatarURL
			ci := app.Images.Get(fullURL, func() { app.Window.Invalidate() })
			if ci != nil && ci.ok {
				defer clip.RRect{
					Rect: image.Rect(0, 0, size, size),
					NE:   rr, NW: rr, SE: rr, SW: rr,
				}.Push(gtx.Ops).Pop()

				imgBounds := ci.img.Bounds()
				imgW := float32(imgBounds.Dx())
				imgH := float32(imgBounds.Dy())
				scaleX := float32(size) / imgW
				scaleY := float32(size) / imgH

				defer op.Affine(f32.Affine2D{}.Scale(f32.Pt(0, 0), f32.Pt(scaleX, scaleY))).Push(gtx.Ops).Pop()

				ci.op.Add(gtx.Ops)
				paint.PaintOp{}.Add(gtx.Ops)

				return layout.Dimensions{Size: image.Pt(size, size)}
			}
		}
	}

	// Fallback: colored circle with initial
	clr := UserColor(username)
	paint.FillShape(gtx.Ops, clr, clip.RRect{
		Rect: image.Rect(0, 0, size, size),
		NE:   rr, NW: rr, SE: rr, SW: rr,
	}.Op(gtx.Ops))
	return layout.Stack{Alignment: layout.Center}.Layout(gtx,
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Dimensions{Size: image.Pt(size, size)}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			initial := "?"
			if len(username) > 0 {
				initial = string([]rune(username)[0])
			}
			var lbl material.LabelStyle
			if sizeDp <= 32 {
				lbl = material.Caption(app.Theme.Material, initial)
			} else {
				lbl = material.Body2(app.Theme.Material, initial)
			}
			lbl.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
			return lbl.Layout(gtx)
		}),
	)
}

// --- Syntax highlighting (chroma + One Dark barvy) ---

// coloredToken je token s barvou pro rendering.
type coloredToken struct {
	text  string
	color color.NRGBA
}

// One Dark barvy
var (
	colorKeyword  = color.NRGBA{R: 198, G: 120, B: 221, A: 255} // #c678dd
	colorString   = color.NRGBA{R: 152, G: 195, B: 121, A: 255} // #98c379
	colorComment  = color.NRGBA{R: 128, G: 128, B: 128, A: 255} // #808080
	colorNumber   = color.NRGBA{R: 209, G: 154, B: 102, A: 255} // #d19a66
	colorFunction = color.NRGBA{R: 97, G: 175, B: 239, A: 255}  // #61afef
	colorClass    = color.NRGBA{R: 229, G: 192, B: 123, A: 255} // #e5c07b
	colorOperator = color.NRGBA{R: 171, G: 178, B: 191, A: 255} // #abb2bf
)

// tokenColor vrátí barvu pro chroma token type.
func tokenColor(tt chroma.TokenType) color.NRGBA {
	switch {
	case tt == chroma.Comment || tt == chroma.CommentSingle || tt == chroma.CommentMultiline ||
		tt == chroma.CommentSpecial || tt == chroma.CommentPreproc || tt == chroma.CommentPreprocFile:
		return colorComment
	case tt == chroma.Keyword || tt == chroma.KeywordConstant || tt == chroma.KeywordDeclaration ||
		tt == chroma.KeywordNamespace || tt == chroma.KeywordPseudo || tt == chroma.KeywordReserved ||
		tt == chroma.KeywordType:
		return colorKeyword
	case tt == chroma.LiteralString || tt == chroma.LiteralStringAffix || tt == chroma.LiteralStringBacktick ||
		tt == chroma.LiteralStringChar || tt == chroma.LiteralStringDelimiter || tt == chroma.LiteralStringDoc ||
		tt == chroma.LiteralStringDouble || tt == chroma.LiteralStringEscape || tt == chroma.LiteralStringHeredoc ||
		tt == chroma.LiteralStringInterpol || tt == chroma.LiteralStringOther || tt == chroma.LiteralStringRegex ||
		tt == chroma.LiteralStringSingle || tt == chroma.LiteralStringSymbol:
		return colorString
	case tt == chroma.LiteralNumber || tt == chroma.LiteralNumberBin || tt == chroma.LiteralNumberFloat ||
		tt == chroma.LiteralNumberHex || tt == chroma.LiteralNumberInteger || tt == chroma.LiteralNumberIntegerLong ||
		tt == chroma.LiteralNumberOct:
		return colorNumber
	case tt == chroma.NameFunction || tt == chroma.NameFunctionMagic:
		return colorFunction
	case tt == chroma.NameClass || tt == chroma.NameBuiltin || tt == chroma.NameBuiltinPseudo:
		return colorClass
	case tt == chroma.Operator || tt == chroma.OperatorWord:
		return colorOperator
	case tt == chroma.NameDecorator || tt == chroma.NameAttribute:
		return colorFunction
	default:
		return ColorText
	}
}

// tokenizeCode tokenizuje kód pomocí chroma lexeru.
func tokenizeCode(code, lang string) []coloredToken {
	lexer := lexers.Get(lang)
	if lexer == nil {
		return nil
	}
	iter, err := lexer.Tokenise(nil, code)
	if err != nil {
		return nil
	}
	var tokens []coloredToken
	for _, tok := range iter.Tokens() {
		if tok.Value == "" {
			continue
		}
		tokens = append(tokens, coloredToken{
			text:  tok.Value,
			color: tokenColor(tok.Type),
		})
	}
	return tokens
}

// layoutHighlightedTokens renderuje tokenizovaný kód s barvami a mono fontem.
// Tokeny se renderují po řádcích (vertikální flex), každý řádek je horizontální flex tokenů.
func layoutHighlightedTokens(gtx layout.Context, th *Theme, tokens []coloredToken, sels *[]widget.Selectable, si int) layout.Dimensions {
	// Rozdělit tokeny na řádky
	type line struct {
		tokens []coloredToken
	}
	var lines []line
	current := line{}
	for _, tok := range tokens {
		// Token může obsahovat newliny — rozdělit
		parts := strings.Split(tok.text, "\n")
		for i, part := range parts {
			if i > 0 {
				lines = append(lines, current)
				current = line{}
			}
			if part != "" {
				current.tokens = append(current.tokens, coloredToken{text: part, color: tok.color})
			}
		}
	}
	lines = append(lines, current)

	var rows []layout.FlexChild
	for _, ln := range lines {
		lineToks := ln.tokens
		rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if len(lineToks) == 0 {
				// Prázdný řádek — malá výška
				lbl := material.Body2(th.Material, " ")
				lbl.Font.Typeface = "Go Mono"
				return lbl.Layout(gtx)
			}
			var items []layout.FlexChild
			for _, t := range lineToks {
				tok := t
				items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Body2(th.Material, tok.text)
					lbl.Color = tok.color
					lbl.Font.Typeface = "Go Mono"
					return lbl.Layout(gtx)
				}))
			}
			return layout.Flex{}.Layout(gtx, items...)
		}))
	}

	// Selectable pro celý blok (fallback — kopírování celého kódu)
	if sels != nil && si >= 0 && si < len(*sels) {
		// Přidáme selectable overlay na celý blok
		return layout.Stack{}.Layout(gtx,
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
			}),
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				// Neviditelný selectable text pro kopírování
				var fullText string
				for _, tok := range tokens {
					fullText += tok.text
				}
				lbl := material.Body2(th.Material, fullText)
				lbl.Color = color.NRGBA{A: 0} // průhledný
				lbl.State = &(*sels)[si]
				return lbl.Layout(gtx)
			}),
		)
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
}

// ParseHexColor parsuje hex barvu (#rrggbb nebo #rgb) na color.NRGBA.
// Vrací ok=false pokud vstup není platný hex color.
func ParseHexColor(hex string) (color.NRGBA, bool) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) == 3 {
		// #rgb → #rrggbb
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	if len(hex) != 6 {
		return color.NRGBA{}, false
	}
	var r, g, b uint8
	for i, ptr := range []*uint8{&r, &g, &b} {
		hi := hexVal(hex[i*2])
		lo := hexVal(hex[i*2+1])
		if hi < 0 || lo < 0 {
			return color.NRGBA{}, false
		}
		*ptr = uint8(hi<<4 | lo)
	}
	return color.NRGBA{R: r, G: g, B: b, A: 255}, true
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// GetUserRoleColor vrátí barvu role s nejnižší position (= nejvyšší rank) pro daného uživatele.
// Pokud žádná role nemá barvu, vrátí fallback UserColor(username).
func (a *App) GetUserRoleColor(conn *ServerConnection, userID, username string) color.NRGBA {
	if conn == nil {
		return UserColor(username)
	}

	// Najít role IDs pro uživatele — projít conn.Members a conn.Roles
	// Klient nemá přímo mapování user→roles, tak projdeme všechny role
	// a podíváme se jestli user má přiřazené role (přes UserRolesMap).
	roleMap := conn.UserRolesMap
	if roleMap == nil {
		return UserColor(username)
	}

	userRoleIDs, ok := roleMap[userID]
	if !ok || len(userRoleIDs) == 0 {
		return UserColor(username)
	}

	// Najít roli s nejnižší position (= nejvyšší rank) která má color
	bestPos := 1<<31 - 1
	bestColor := color.NRGBA{}
	found := false
	for _, role := range conn.Roles {
		if !userRoleIDs[role.ID] {
			continue
		}
		if role.Color == "" {
			continue
		}
		if role.Position < bestPos {
			c, ok := ParseHexColor(role.Color)
			if ok {
				bestPos = role.Position
				bestColor = c
				found = true
			}
		}
	}
	if found {
		return bestColor
	}
	return UserColor(username)
}

// layoutCentered — centrovaný text.
func layoutCentered(gtx layout.Context, th *Theme, text string, c color.NRGBA) layout.Dimensions {
	paint.FillShape(gtx.Ops, ColorBg, clip.Rect{Max: gtx.Constraints.Max}.Op())
	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		lbl := material.Body1(th.Material, text)
		lbl.Color = c
		return lbl.Layout(gtx)
	})
}
