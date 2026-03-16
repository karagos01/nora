//go:build linux

package ui

/*
#cgo pkg-config: x11
#include <X11/Xlib.h>
#include <X11/Xatom.h>
#include <stdlib.h>

*/
import "C"

import (
	"log"
	"runtime"
	"time"
	"unsafe"

	"gioui.org/app"
)

func (a *App) SetupFileDrop(e app.ViewEvent) {
	if !e.Valid() || a.fileDropInitialized {
		return
	}
	switch v := e.(type) {
	case app.X11ViewEvent:
		a.fileDropInitialized = true
		a.useTransferDrop = true
		// Set XdndAware on WM frame so XDND traversal reaches gioWin.
		// Gio's patched os_x11.go already sets XdndAware on gioWin and handles events.
		dpy := (*C.Display)(v.Display)
		win := C.Window(v.Window)
		go setFrameXdndAware(dpy, win)
	case app.WaylandViewEvent:
		a.fileDropInitialized = true
		a.useTransferDrop = true
	}
}

// FinishFileDropSetup is a no-op on Linux (setup done in SetupFileDrop).
func (a *App) FinishFileDropSetup() {}

func setFrameXdndAware(gioDpy *C.Display, gioWin C.Window) {
	runtime.LockOSThread()

	// Wait for WM reparenting
	time.Sleep(300 * time.Millisecond)

	// Use separate connection for property changes on frame
	displayStr := C.GoString(C.XDisplayString(gioDpy))
	cstr := C.CString(displayStr)
	dpy := C.XOpenDisplay(cstr)
	C.free(unsafe.Pointer(cstr))
	if dpy == nil {
		log.Println("drop: cannot open X11 display")
		return
	}

	root := C.XDefaultRootWindow(dpy)
	topLevel := findTopLevel(dpy, gioWin, root)

	if topLevel != gioWin {
		xdndAware := C.XInternAtom(dpy, C.CString("XdndAware"), C.False)
		ver := C.long(5)
		C.XChangeProperty(dpy, topLevel, xdndAware, C.XA_ATOM, 32,
			C.PropModeReplace, (*C.uchar)(unsafe.Pointer(&ver)), 1)
		C.XFlush(dpy)
		log.Printf("drop: set XdndAware on frame=0x%x (gioWin=0x%x)", topLevel, gioWin)
	} else {
		log.Printf("drop: frame==gioWin=0x%x, no extra XdndAware needed", gioWin)
	}

	C.XCloseDisplay(dpy)
}

func findTopLevel(dpy *C.Display, win, root C.Window) C.Window {
	current := win
	for {
		var rootRet, parentRet C.Window
		var children *C.Window
		var nchildren C.uint
		C.XQueryTree(dpy, current, &rootRet, &parentRet, &children, &nchildren)
		if children != nil {
			C.XFree(unsafe.Pointer(children))
		}
		if parentRet == root || parentRet == 0 {
			return current
		}
		current = parentRet
	}
}
