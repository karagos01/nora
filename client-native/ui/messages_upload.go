package ui

import (
	"fmt"
	"image"
	"image/color"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"nora-client/api"
)

type pendingUpload struct {
	filename  string
	size      int64
	sent      int64
	status    int // 0=uploading, 1=done, 2=error
	err       string
	result    *api.UploadResult
	removed   bool
	removeBtn widget.Clickable
	startTime time.Time
}

func (v *MessageView) pickFiles() {
	paths := openMultiFileDialog()
	if len(paths) == 0 {
		return
	}

	conn := v.app.Conn()
	if conn == nil {
		return
	}

	// Nabídnout ZIP / P2P dialog
	if conn.P2P != nil {
		v.app.ZipUploadDlg.Show(paths, v.startUploads, v.startP2PSend)
	} else if len(paths) >= 2 {
		v.app.ZipUploadDlg.Show(paths, v.startUploads)
	} else {
		v.startUploads(paths)
		return
	}
	v.app.Window.Invalidate()
}

func (v *MessageView) startUploads(paths []string) {
	if len(paths) == 0 {
		return
	}

	conn := v.app.Conn()
	if conn == nil {
		return
	}

	// Zachytit channelID pro auto-send
	v.app.mu.RLock()
	chID := conn.ActiveChannelID
	v.app.mu.RUnlock()

	v.uploadMu.Lock()
	v.uploadChannelID = chID
	v.uploadMu.Unlock()

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		name := filepath.Base(path)
		u := &pendingUpload{
			filename:  name,
			size:      int64(len(data)),
			status:    0,
			startTime: time.Now(),
		}

		v.uploadMu.Lock()
		v.pendingUploads = append(v.pendingUploads, u)
		v.uploadMu.Unlock()
		v.app.Window.Invalidate()

		go func(pu *pendingUpload, fname string, fileData []byte, origPath string) {
			result, err := conn.Client.UploadFileChunked(fname, fileData, func(sent, total int64) {
				if !pu.removed {
					pu.sent = sent
					v.app.Window.Invalidate()
				}
			})

			if pu.removed {
				return
			}

			v.uploadMu.Lock()
			if err != nil {
				pu.status = 2
				pu.err = err.Error()

				// P2P fallback: nabídnout přímý přenos pokud chyba vypadá jako size limit
				errStr := err.Error()
				if conn.P2P != nil && (strings.Contains(errStr, "413") || strings.Contains(errStr, "too large") || strings.Contains(errStr, "file size")) {
					v.uploadMu.Unlock()
					v.app.ConfirmDlg.Show("File too large", "File exceeds server limit. Share directly via P2P?", func() {
						v.startP2PSend([]string{origPath})
					})
					v.app.Window.Invalidate()
					return
				}
			} else {
				pu.status = 1
				pu.result = result
				pu.sent = pu.size
			}

			// Auto-send: zkontrolovat jestli jsou všechny hotové
			if v.autoSend {
				allDone := true
				for _, other := range v.pendingUploads {
					if !other.removed && other.status == 0 {
						allDone = false
						break
					}
				}
				if allDone {
					v.autoSendPending = true
				}
			}
			v.uploadMu.Unlock()
			v.app.Window.Invalidate()
		}(u, name, data, path)
	}

	// Po přidání souborů otevřít popup
	v.app.UploadDlg.Show()
	v.app.Window.Invalidate()
}

// startP2PSend registruje soubory v P2P manageru a pošle zprávu s P2P linkem do kanálu.
// Formát zprávy: [P2P:transferID:senderUserID:fileName:fileSize]
func (v *MessageView) startP2PSend(paths []string) {
	conn := v.app.Conn()
	if conn == nil || conn.P2P == nil {
		return
	}

	v.app.mu.RLock()
	chID := conn.ActiveChannelID
	userID := conn.UserID
	v.app.mu.RUnlock()

	if chID == "" {
		return
	}

	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}

		fileName := info.Name()
		fileSize := info.Size()

		// Registrovat soubor — zůstane dostupný pro stahování
		isTemp := strings.HasPrefix(path, os.TempDir())
		var transferID string
		if isTemp {
			transferID = conn.P2P.RegisterTempFile(path, fileName, fileSize)
		} else {
			transferID = conn.P2P.RegisterFile(path, fileName, fileSize)
		}

		// Poslat zprávu s P2P odkazem do kanálu
		msgText := fmt.Sprintf("[P2P:%s:%s:%s:%d]", transferID, userID, fileName, fileSize)
		go func(text, channelID string) {
			_, err := conn.Client.SendMessage(channelID, text, "")
			if err != nil {
				log.Printf("p2p: send message: %v", err)
			}
		}(msgText, chID)
	}
}

func (v *MessageView) layoutUploadPanel(gtx layout.Context) layout.Dimensions {
	v.uploadMu.Lock()
	n := len(v.pendingUploads)
	if n == 0 {
		v.uploadMu.Unlock()
		return layout.Dimensions{}
	}

	// Spočítat celkový progress
	var totalSize, totalSent int64
	var earliest time.Time
	allDone := true
	hasError := false
	for _, u := range v.pendingUploads {
		totalSize += u.size
		totalSent += u.sent
		if u.status == 0 {
			allDone = false
			if earliest.IsZero() || u.startTime.Before(earliest) {
				earliest = u.startTime
			}
		}
		if u.status == 2 {
			hasError = true
		}
	}
	v.uploadMu.Unlock()

	// Status text
	var statusText string
	var statusColor color.NRGBA
	if hasError {
		statusText = fmt.Sprintf("%d file(s) — error", n)
		statusColor = ColorDanger
	} else if allDone {
		statusText = fmt.Sprintf("%d file(s) ready", n)
		statusColor = ColorSuccess
	} else {
		pct := 0
		if totalSize > 0 {
			pct = int(totalSent * 100 / totalSize)
		}
		statusText = fmt.Sprintf("Uploading %d file(s)... %d%%", n, pct)
		// Přidat speed + ETA
		elapsed := time.Since(earliest).Seconds()
		if elapsed > 0.5 && totalSent > 0 {
			speed := float64(totalSent) / elapsed
			statusText += fmt.Sprintf(" · %s/s", FormatBytes(int64(speed)))
			remaining := float64(totalSize-totalSent) / speed
			if remaining > 0 && remaining < 3600 {
				if remaining < 60 {
					statusText += fmt.Sprintf(" · ~%ds", int(remaining))
				} else {
					statusText += fmt.Sprintf(" · ~%dm%ds", int(remaining)/60, int(remaining)%60)
				}
			}
		}
		statusColor = ColorAccent
	}

	return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return v.uploadBarBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				// Bar background + text
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					bg := ColorSidebar
					if v.uploadBarBtn.Hovered() {
						bg = ColorHover
					}
					return layout.Background{}.Layout(gtx,
						func(gtx layout.Context) layout.Dimensions {
							bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
							rr := gtx.Dp(6)
							paint.FillShape(gtx.Ops, bg, clip.RRect{
								Rect: bounds,
								NE:   rr, NW: rr, SE: rr, SW: rr,
							}.Op(gtx.Ops))
							return layout.Dimensions{Size: bounds.Max}
						},
						func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Caption(v.app.Theme.Material, statusText)
								lbl.Color = statusColor
								return lbl.Layout(gtx)
							})
						},
					)
				}),
				// Progress bar (celková)
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if allDone {
						return layout.Dimensions{}
					}
					maxW := gtx.Constraints.Max.X
					h := gtx.Dp(3)
					paint.FillShape(gtx.Ops, ColorBg, clip.Rect(image.Rect(0, 0, maxW, h)).Op())
					pct := float32(0)
					if totalSize > 0 {
						pct = float32(totalSent) / float32(totalSize)
					}
					barW := int(pct * float32(maxW))
					if barW > 0 {
						paint.FillShape(gtx.Ops, ColorAccent, clip.Rect(image.Rect(0, 0, barW, h)).Op())
					}
					return layout.Dimensions{Size: image.Pt(maxW, h)}
				}),
			)
		})
	})
}

func (v *MessageView) pasteClipboardImage() {
	data := readClipboardImage()
	if data == nil {
		return
	}

	// Uložit do temp souboru
	tmpFile, err := os.CreateTemp("", "nora-paste-*.png")
	if err != nil {
		log.Printf("Clipboard paste temp file error: %v", err)
		return
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return
	}
	tmpFile.Close()

	// Spustit upload
	v.autoSend = true
	v.startUploads([]string{tmpPath})

	// Cleanup temp souboru po 30s
	go func() {
		time.Sleep(30 * time.Second)
		os.Remove(tmpPath)
	}()
}

// readClipboardImage přečte PNG obrázek z clipboard.
// Vrátí nil pokud clipboard neobsahuje obrázek.
func readClipboardImage() []byte {
	switch runtime.GOOS {
	case "linux":
		// xclip -selection clipboard -t image/png -o
		cmd := exec.Command("xclip", "-selection", "clipboard", "-t", "image/png", "-o")
		out, err := cmd.Output()
		if err == nil && len(out) > 8 {
			return out
		}
		// Fallback: xsel
		cmd2 := exec.Command("xsel", "--clipboard", "--output")
		out2, err := cmd2.Output()
		if err == nil && len(out2) > 8 && isPNG(out2) {
			return out2
		}
		return nil

	case "windows":
		// PowerShell: Get-Clipboard -Format Image, Save as PNG
		script := `
$img = Get-Clipboard -Format Image
if ($img -ne $null) {
    $ms = New-Object System.IO.MemoryStream
    $img.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png)
    [Console]::OpenStandardOutput().Write($ms.ToArray(), 0, $ms.Length)
}`
		cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
		out, err := cmd.Output()
		if err == nil && len(out) > 8 {
			return out
		}
		return nil

	default:
		return nil
	}
}

// isPNG kontroluje PNG magic bytes.
func isPNG(data []byte) bool {
	return len(data) > 8 && data[0] == 0x89 && data[1] == 'P' && data[2] == 'N' && data[3] == 'G'
}
