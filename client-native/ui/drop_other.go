//go:build !windows && !linux

package ui

import "gioui.org/app"

// SetupFileDrop je no-op na nepodporovaných platformách.
func (a *App) SetupFileDrop(e app.ViewEvent) {}
