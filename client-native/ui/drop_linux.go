//go:build linux

package ui

/*
#cgo pkg-config: x11 xext
#include <X11/Xlib.h>
#include <X11/Xatom.h>
#include <X11/Xutil.h>
#include <X11/extensions/shape.h>
#include <stdlib.h>

static long cmDataL(XClientMessageEvent *ev, int i) { return ev->data.l[i]; }
static void setCMDataL(XClientMessageEvent *ev, int i, long v) { ev->data.l[i] = v; }

// Prázdný input shape = click-through pro myš, bounding shape zůstane
// (XDND traversal používá bounding/geometrii, ne input shape)
static void setClickThrough(Display *dpy, Window win) {
	XShapeCombineRectangles(dpy, win, ShapeInput, 0, 0, NULL, 0, ShapeSet, Unsorted);
}

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
		a.useTransferDrop = false
		dpy := (*C.Display)(v.Display)
		win := C.Window(v.Window)
		go setupXDNDOverlay(dpy, win, a.DroppedFiles, a.Window)
	case app.WaylandViewEvent:
		// Wayland: použijeme Gio transfer.DataEvent v App.Layout.
		a.fileDropInitialized = true
		a.useTransferDrop = true
	}
}

func setupXDNDOverlay(gioDpy *C.Display, gioWin C.Window, ch chan<- []string, gioWindow *app.Window) {
	runtime.LockOSThread()

	// Počkat na WM reparenting
	time.Sleep(300 * time.Millisecond)

	// Separátní X11 spojení — eventy pro naše okno přijdou sem, ne do Gio
	displayStr := C.GoString(C.XDisplayString(gioDpy))
	cstr := C.CString(displayStr)
	dpy := C.XOpenDisplay(cstr)
	C.free(unsafe.Pointer(cstr))
	if dpy == nil {
		log.Println("drop: nelze otevřít X11 display")
		return
	}

	root := C.XDefaultRootWindow(dpy)

	// Najít top-level WM frame
	topLevel := findTopLevel(dpy, gioWin, root)

	// Velikost klientského okna
	var rr C.Window
	var gx, gy C.int
	var gw, gh, gbw, gd C.uint
	C.XGetGeometry(dpy, C.Drawable(gioWin), &rr, &gx, &gy, &gw, &gh, &gbw, &gd)

	// Vytvořit InputOnly overlay jako child gioWin
	// InputOnly = neviditelný (žádný visual, žádné renderování), ale přijímá XDND eventy
	var swa C.XSetWindowAttributes
	swa.override_redirect = C.True
	swa.event_mask = C.StructureNotifyMask | C.PropertyChangeMask
	overlay := C.XCreateWindow(dpy, gioWin, 0, 0, gw, gh, 0,
		0, C.InputOnly, nil,
		C.CWOverrideRedirect|C.CWEventMask, &swa)

	// Prázdný input shape = click-through (myš prochází do Gio okna pod tím)
	C.setClickThrough(dpy, overlay)

	// XDND atomy
	xdndAware := internAtom(dpy, "XdndAware")
	xdndProxy := internAtom(dpy, "XdndProxy")
	xdndEnter := internAtom(dpy, "XdndEnter")
	xdndPosition := internAtom(dpy, "XdndPosition")
	xdndDrop := internAtom(dpy, "XdndDrop")
	xdndLeave := internAtom(dpy, "XdndLeave")
	xdndFinished := internAtom(dpy, "XdndFinished")
	xdndStatus := internAtom(dpy, "XdndStatus")
	xdndSelection := internAtom(dpy, "XdndSelection")
	xdndActionCopy := internAtom(dpy, "XdndActionCopy")
	textURIList := internAtom(dpy, "text/uri-list")

	ver := C.long(5)
	overlayID := C.long(overlay)

	// Frame: XdndAware + XdndProxy → overlay
	C.XChangeProperty(dpy, topLevel, xdndAware, C.XA_ATOM, 32,
		C.PropModeReplace, (*C.uchar)(unsafe.Pointer(&ver)), 1)
	C.XChangeProperty(dpy, topLevel, xdndProxy, C.XA_WINDOW, 32,
		C.PropModeReplace, (*C.uchar)(unsafe.Pointer(&overlayID)), 1)

	// Klientské okno: XdndAware + XdndProxy → overlay (pro sources co traversují do childrenu)
	if topLevel != gioWin {
		C.XChangeProperty(dpy, gioWin, xdndAware, C.XA_ATOM, 32,
			C.PropModeReplace, (*C.uchar)(unsafe.Pointer(&ver)), 1)
		C.XChangeProperty(dpy, gioWin, xdndProxy, C.XA_WINDOW, 32,
			C.PropModeReplace, (*C.uchar)(unsafe.Pointer(&overlayID)), 1)
	}

	// Overlay: XdndAware + self-proxy (XDND spec: proxy musí mít XdndProxy na sebe)
	C.XChangeProperty(dpy, overlay, xdndAware, C.XA_ATOM, 32,
		C.PropModeReplace, (*C.uchar)(unsafe.Pointer(&ver)), 1)
	C.XChangeProperty(dpy, overlay, xdndProxy, C.XA_WINDOW, 32,
		C.PropModeReplace, (*C.uchar)(unsafe.Pointer(&overlayID)), 1)

	// Zobrazit overlay + sledovat resize rodiče
	C.XMapRaised(dpy, overlay)
	C.XSelectInput(dpy, gioWin, C.StructureNotifyMask)
	C.XFlush(dpy)

	log.Printf("drop: overlay=0x%x gioWin=0x%x frame=0x%x (%dx%d) InputOnly",
		overlay, gioWin, topLevel, gw, gh)

	// Event loop na separátním spojení
	var sourceWin C.Window
	var ev C.XEvent

	for {
		C.XNextEvent(dpy, &ev)
		evType := *(*C.int)(unsafe.Pointer(&ev))

		switch evType {
		case C.ClientMessage:
			cm := (*C.XClientMessageEvent)(unsafe.Pointer(&ev))

			switch cm.message_type {
			case xdndEnter:
				sourceWin = C.Window(C.cmDataL(cm, 0))
				log.Printf("drop: XdndEnter source=0x%x", sourceWin)

			case xdndLeave:
				log.Printf("drop: XdndLeave source=0x%x", C.Window(C.cmDataL(cm, 0)))
				sourceWin = 0

			case xdndPosition:
				sourceWin = C.Window(C.cmDataL(cm, 0))
				suggestedAction := C.cmDataL(cm, 4)

				// XdndStatus — data.l[0] = frame per XDND proxy spec (ne overlay!)
				var reply C.XClientMessageEvent
				reply._type = C.ClientMessage
				reply.window = sourceWin
				reply.message_type = xdndStatus
				reply.format = 32
				C.setCMDataL(&reply, 0, C.long(topLevel))
				C.setCMDataL(&reply, 1, 1)                  // accept
				C.setCMDataL(&reply, 2, 0)                  // rectangle origin
				C.setCMDataL(&reply, 3, C.long(0x7FFF7FFF)) // celý rectangle
				C.setCMDataL(&reply, 4, suggestedAction)
				C.XSendEvent(dpy, sourceWin, C.False, C.NoEventMask,
					(*C.XEvent)(unsafe.Pointer(&reply)))
				C.XFlush(dpy)
				log.Printf("drop: XdndPosition → Status (target=0x%x)", topLevel)

			case xdndDrop:
				sourceWin = C.Window(C.cmDataL(cm, 0))
				dropTS := C.ulong(C.cmDataL(cm, 2))
				log.Printf("drop: XdndDrop source=0x%x ts=%d", sourceWin, dropTS)
				C.XConvertSelection(dpy, xdndSelection, textURIList,
					xdndSelection, overlay, dropTS)
				C.XFlush(dpy)
			}

		case C.SelectionNotify:
			sn := (*C.XSelectionEvent)(unsafe.Pointer(&ev))
			var paths []string
			if sn.property != C.None {
				paths = readSelectionProp(dpy, overlay, sn.property)
			}
			log.Printf("drop: %d files: %v", len(paths), paths)

			// XdndFinished — data.l[0] = frame
			if sourceWin != 0 {
				var fin C.XClientMessageEvent
				fin._type = C.ClientMessage
				fin.window = sourceWin
				fin.message_type = xdndFinished
				fin.format = 32
				C.setCMDataL(&fin, 0, C.long(topLevel))
				if len(paths) > 0 {
					C.setCMDataL(&fin, 1, 1)
					C.setCMDataL(&fin, 2, C.long(xdndActionCopy))
				}
				C.XSendEvent(dpy, sourceWin, C.False, C.NoEventMask,
					(*C.XEvent)(unsafe.Pointer(&fin)))
				C.XFlush(dpy)
			}
			sourceWin = 0

			if len(paths) > 0 {
				queued := append([]string(nil), paths...)
				select {
				case ch <- queued:
				default:
					go func(ps []string) { ch <- ps }(queued)
					log.Println("drop: channel full, queueing asynchronously")
				}
				gioWindow.Invalidate()
			}

		case C.ConfigureNotify:
			cn := (*C.XConfigureEvent)(unsafe.Pointer(&ev))
			if cn.window == gioWin {
				C.XResizeWindow(dpy, overlay, C.uint(cn.width), C.uint(cn.height))
				C.setClickThrough(dpy, overlay)
				C.XRaiseWindow(dpy, overlay)
				C.XFlush(dpy)
			}
		}
	}
}

func internAtom(dpy *C.Display, name string) C.Atom {
	cstr := C.CString(name)
	defer C.free(unsafe.Pointer(cstr))
	return C.XInternAtom(dpy, cstr, C.False)
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

func readSelectionProp(dpy *C.Display, win C.Window, prop C.Atom) []string {
	var actualType C.Atom
	var actualFormat C.int
	var nitems, bytesAfter C.ulong
	var data *C.uchar

	C.XGetWindowProperty(dpy, win, prop, 0, 65536, C.True, C.AnyPropertyType,
		&actualType, &actualFormat, &nitems, &bytesAfter, &data)

	if data == nil || nitems == 0 {
		if data != nil {
			C.XFree(unsafe.Pointer(data))
		}
		return nil
	}

	raw := C.GoStringN((*C.char)(unsafe.Pointer(data)), C.int(nitems))
	C.XFree(unsafe.Pointer(data))

	return parseDroppedURIList(raw)
}
