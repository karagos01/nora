package main

import (
	"log"
	"os"
	"strings"

	"gioui.org/app"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"nora-client/ui"
	"nora-client/update"
)

// Set via ldflags: go build -ldflags "-X main.version=0.1.0"
var version = "dev"

func main() {
	go func() {
		w := new(app.Window)
		w.Option(
			app.Title("NORA"),
			app.Size(unit.Dp(1100), unit.Dp(700)),
			app.MinSize(unit.Dp(800), unit.Dp(500)),
		)

		a := ui.NewApp(w, version)
		defer a.Destroy()

		// Deep link from command line arguments
		if len(os.Args) > 1 && strings.HasPrefix(os.Args[1], "nora://") {
			a.PendingDeepLink = os.Args[1]
		}

		// Background update check
		go update.CheckAndPrompt(version, func(res update.Result) {
			a.UpdateBar.SetAvailable(res.NewVersion, res.DownloadURL, res.SHA256)
			w.Invalidate()
		})

		var ops op.Ops
		for {
			switch e := w.Event().(type) {
			case app.DestroyEvent:
				a.Destroy()
				if e.Err != nil {
					log.Fatal(e.Err)
				}
				os.Exit(0)
			case app.ViewEvent:
				log.Printf("main: ViewEvent received: %T valid=%v", e, e.Valid())
				a.SetupFileDrop(e)
			case app.FrameEvent:
				// Process all dragged files from OS that are waiting in the queue.
				for {
					select {
					case paths := <-a.DroppedFiles:
						a.HandleDroppedFiles(paths)
					default:
						goto dropsDone
					}
				}
			dropsDone:
				gtx := app.NewContext(&ops, e)
				paint.FillShape(gtx.Ops, ui.ColorBg, clip.Rect{Max: gtx.Constraints.Max}.Op())
				a.Layout(layout.Context(gtx))
				e.Frame(gtx.Ops)
			}
		}
	}()
	app.Main()
}
