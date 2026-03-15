package ui

import (
	"crypto/tls"
	"fmt"
	"image"
	"image/color"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"nora-client/api"
)

type pendingDownload struct {
	filename  string
	size      int64 // z Content-Length, -1 pokud neznámý
	received  int64
	status    int // 0=downloading, 1=done, 2=error
	err       string
	startTime time.Time
	// Batch download (více souborů)
	totalFiles  int
	currentFile int
	currentName string
}

// startDownload zahájí stahování souboru s progress trackingem.
func (v *MessageView) startDownload(fileURL, savePath, filename, token string) {
	isZip := strings.HasSuffix(strings.ToLower(savePath), ".zip")

	// Zobrazit extract dialog hned při zahájení stahování .zip
	if isZip {
		v.app.ZipExtractDlg.Show(savePath)
		v.app.Window.Invalidate()
	}

	dl := &pendingDownload{
		filename:  filename,
		size:      -1,
		startTime: time.Now(),
	}

	v.downloadMu.Lock()
	v.pendingDownloads = append(v.pendingDownloads, dl)
	v.downloadMu.Unlock()

	go func() {
		// Při chybě zrušit zip dialog
		var downloadOK bool
		if isZip {
			defer func() {
				if !downloadOK {
					v.app.ZipExtractDlg.Cancel()
				}
			}()
		}

		req, err := http.NewRequest("GET", fileURL, nil)
		if err != nil {
			dl.status = 2
			dl.err = err.Error()
			v.app.Window.Invalidate()
			return
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			dl.status = 2
			dl.err = err.Error()
			v.app.Window.Invalidate()
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			dl.status = 2
			dl.err = fmt.Sprintf("HTTP %d", resp.StatusCode)
			v.app.Window.Invalidate()
			return
		}

		dl.size = resp.ContentLength

		f, err := os.Create(savePath)
		if err != nil {
			dl.status = 2
			dl.err = err.Error()
			v.app.Window.Invalidate()
			return
		}
		defer f.Close()

		buf := make([]byte, 32*1024)
		for {
			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				if _, writeErr := f.Write(buf[:n]); writeErr != nil {
					dl.status = 2
					dl.err = writeErr.Error()
					v.app.Window.Invalidate()
					return
				}
				dl.received += int64(n)
				v.app.Window.Invalidate()
			}
			if readErr != nil {
				if readErr == io.EOF {
					break
				}
				dl.status = 2
				dl.err = readErr.Error()
				v.app.Window.Invalidate()
				return
			}
		}

		dl.status = 1
		downloadOK = true
		v.app.Window.Invalidate()

		// Oznámit dialogu, že stahování je hotové
		if isZip {
			v.app.ZipExtractDlg.MarkDownloaded()
		}

		// Po stažení .zip aplikovat volbu uživatele
		if isZip {
			choice := v.app.ZipExtractDlg.WaitChoice()
			dir := filepath.Dir(savePath)
			if choice == zipChoiceExtractDelete || choice == zipChoiceExtractOnly {
				doExtract := true
				conflicts := zipConflicts(savePath, dir)
				if len(conflicts) > 0 {
					ch := make(chan bool, 1)
					v.app.ConfirmDlg.ShowWithCancel(
						"Overwrite files",
						formatConflictMessage(conflicts),
						"Overwrite",
						func() { ch <- true },
						func() { ch <- false },
					)
					v.app.Window.Invalidate()
					doExtract = <-ch
				}
				if doExtract {
					extractZip(savePath, dir)
					if choice == zipChoiceExtractDelete {
						removeWithRetry(savePath)
					}
				}
			}
		}

		// Auto-cleanup po 3 sekundách
		time.AfterFunc(3*time.Second, func() {
			v.downloadMu.Lock()
			filtered := v.pendingDownloads[:0]
			for _, d := range v.pendingDownloads {
				if d != dl {
					filtered = append(filtered, d)
				}
			}
			v.pendingDownloads = filtered
			v.downloadMu.Unlock()
			v.app.Window.Invalidate()
		})
	}()
}

func (v *MessageView) layoutDownloadPanel(gtx layout.Context) layout.Dimensions {
	v.downloadMu.Lock()
	n := len(v.pendingDownloads)
	if n == 0 {
		v.downloadMu.Unlock()
		return layout.Dimensions{}
	}

	// Spočítat celkový progress
	var totalSize, totalReceived int64
	var earliest time.Time
	allDone := true
	hasError := false
	sizeKnown := true
	for _, dl := range v.pendingDownloads {
		if dl.size > 0 {
			totalSize += dl.size
		} else {
			sizeKnown = false
		}
		totalReceived += dl.received
		if dl.status == 0 {
			allDone = false
			if earliest.IsZero() || dl.startTime.Before(earliest) {
				earliest = dl.startTime
			}
		}
		if dl.status == 2 {
			hasError = true
		}
	}
	v.downloadMu.Unlock()

	// Batch download info (pokud existuje)
	var batchFile, batchName string
	var batchTotal int
	for _, dl := range v.pendingDownloads {
		if dl.totalFiles > 0 {
			batchTotal = dl.totalFiles
			batchFile = fmt.Sprintf("%d/%d", dl.currentFile, dl.totalFiles)
			batchName = dl.currentName
			break
		}
	}

	// Status text
	var statusText string
	var statusColor color.NRGBA
	if hasError {
		statusText = fmt.Sprintf("Download error (%d file(s))", n)
		statusColor = ColorDanger
	} else if allDone {
		if batchTotal > 0 {
			statusText = fmt.Sprintf("%d files downloaded", batchTotal)
		} else {
			statusText = fmt.Sprintf("%d file(s) downloaded", n)
		}
		statusColor = ColorSuccess
	} else {
		if batchTotal > 0 {
			statusText = fmt.Sprintf("Downloading file %s: %s", batchFile, batchName)
		} else if sizeKnown && totalSize > 0 {
			pct := int(totalReceived * 100 / totalSize)
			statusText = fmt.Sprintf("Downloading %d file(s)... %d%%", n, pct)
		} else {
			statusText = fmt.Sprintf("Downloading %d file(s)... %s", n, FormatBytes(totalReceived))
		}
		elapsed := time.Since(earliest).Seconds()
		if elapsed > 0.5 && totalReceived > 0 {
			speed := float64(totalReceived) / elapsed
			statusText += fmt.Sprintf(" · %s/s", FormatBytes(int64(speed)))
			if sizeKnown && totalSize > 0 {
				remaining := float64(totalSize-totalReceived) / speed
				if remaining > 0 && remaining < 3600 {
					if remaining < 60 {
						statusText += fmt.Sprintf(" · ~%ds", int(remaining))
					} else {
						statusText += fmt.Sprintf(" · ~%dm%ds", int(remaining)/60, int(remaining)%60)
					}
				}
			}
		}
		statusColor = ColorAccent
	}

	return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			// Bar background + text
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Background{}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions {
						bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
						rr := gtx.Dp(6)
						paint.FillShape(gtx.Ops, ColorSidebar, clip.RRect{
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
			// Progress bar
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if allDone || !sizeKnown || totalSize <= 0 {
					return layout.Dimensions{}
				}
				maxW := gtx.Constraints.Max.X
				h := gtx.Dp(3)
				paint.FillShape(gtx.Ops, ColorBg, clip.Rect(image.Rect(0, 0, maxW, h)).Op())
				pct := float32(totalReceived) / float32(totalSize)
				barW := int(pct * float32(maxW))
				if barW > 0 {
					paint.FillShape(gtx.Ops, ColorAccent, clip.Rect(image.Rect(0, 0, barW, h)).Op())
				}
				return layout.Dimensions{Size: image.Pt(maxW, h)}
			}),
		)
	})
}

// startDirectoryDownload sekvenčně stáhne attachmenty do zvoleného adresáře
func (v *MessageView) startDirectoryDownload(attachments []api.Attachment, baseURL, token, dir string) {
	total := len(attachments)
	if total == 0 {
		return
	}

	pd := &pendingDownload{
		filename:    filepath.Base(dir),
		size:        0,
		status:      0,
		startTime:   time.Now(),
		totalFiles:  total,
		currentFile: 0,
	}
	v.downloadMu.Lock()
	v.pendingDownloads = append(v.pendingDownloads, pd)
	v.downloadMu.Unlock()
	v.app.Window.Invalidate()

	client := &http.Client{
		Timeout: 5 * time.Minute,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}

	for i, att := range attachments {
		pd.currentFile = i + 1
		pd.currentName = att.Filename
		v.app.Window.Invalidate()

		// Vytvořit podadresáře pokud relPath obsahuje /
		// Path traversal ochrana — filename nesmí uniknout z cílového adresáře
		cleanName := filepath.Clean(att.Filename)
		if strings.HasPrefix(cleanName, "..") || filepath.IsAbs(cleanName) {
			pd.status = 2
			pd.err = fmt.Sprintf("unsafe filename: %s", att.Filename)
			v.app.Window.Invalidate()
			return
		}
		localPath := filepath.Join(dir, cleanName)
		absDir, _ := filepath.Abs(dir)
		absLocal, _ := filepath.Abs(localPath)
		if !strings.HasPrefix(absLocal, absDir+string(filepath.Separator)) && absLocal != absDir {
			pd.status = 2
			pd.err = fmt.Sprintf("path traversal: %s", att.Filename)
			v.app.Window.Invalidate()
			return
		}
		if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
			pd.status = 2
			pd.err = fmt.Sprintf("mkdir: %v", err)
			v.app.Window.Invalidate()
			return
		}

		url := baseURL + att.URL
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			pd.status = 2
			pd.err = fmt.Sprintf("request: %v", err)
			v.app.Window.Invalidate()
			return
		}
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := client.Do(req)
		if err != nil {
			pd.status = 2
			pd.err = fmt.Sprintf("download: %v", err)
			v.app.Window.Invalidate()
			return
		}

		f, err := os.Create(localPath)
		if err != nil {
			resp.Body.Close()
			pd.status = 2
			pd.err = fmt.Sprintf("create: %v", err)
			v.app.Window.Invalidate()
			return
		}

		written, err := io.Copy(f, resp.Body)
		f.Close()
		resp.Body.Close()
		if err != nil {
			pd.status = 2
			pd.err = fmt.Sprintf("write: %v", err)
			v.app.Window.Invalidate()
			return
		}

		pd.received += written
		v.app.Window.Invalidate()
	}

	pd.status = 1
	v.app.Window.Invalidate()
}

// hasGameServerAttachments vrátí true pokud zpráva má >1 attachment s game server URL
func hasGameServerAttachments(attachments []api.Attachment) bool {
	count := 0
	for _, a := range attachments {
		if strings.Contains(a.URL, "/api/gameservers/") {
			count++
		}
	}
	return count > 1
}
