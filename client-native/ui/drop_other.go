//go:build !windows && !linux

package ui

import "gioui.org/app"

// SetupFileDrop is a no-op on unsupported platforms.
func (a *App) SetupFileDrop(e app.ViewEvent) {}
