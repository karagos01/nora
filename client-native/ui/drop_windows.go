//go:build windows

package ui

import (
	"gioui.org/app"
)

// SetupFileDrop is disabled on Windows (subclassing window proc from Go goroutine
// causes Win32 message pump to hang).
func (a *App) SetupFileDrop(e app.ViewEvent) {}

// FinishFileDropSetup is a no-op on Windows.
func (a *App) FinishFileDropSetup() {}
