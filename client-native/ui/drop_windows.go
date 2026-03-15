//go:build windows

package ui

import (
	"log"
	"syscall"
	"unsafe"

	"gioui.org/app"
)

const (
	_WM_DROPFILES = 0x0233
	_GWLP_WNDPROC = ^uintptr(3) // -4
)

var (
	shell32           = syscall.NewLazyDLL("shell32.dll")
	user32            = syscall.NewLazyDLL("user32.dll")
	pDragAcceptFiles  = shell32.NewProc("DragAcceptFiles")
	pDragQueryFileW   = shell32.NewProc("DragQueryFileW")
	pDragFinish       = shell32.NewProc("DragFinish")
	pSetWindowLongPtr = user32.NewProc("SetWindowLongPtrW")
	pGetWindowLongPtr = user32.NewProc("GetWindowLongPtrW")
	pCallWindowProc   = user32.NewProc("CallWindowProcW")
)

// SetupFileDrop inicializuje OS file drop pro Windows (WM_DROPFILES).
func (a *App) SetupFileDrop(e app.ViewEvent) {
	v, ok := e.(app.Win32ViewEvent)
	if !ok || !e.Valid() || a.fileDropInitialized {
		return
	}
	a.fileDropInitialized = true
	a.useTransferDrop = false
	initFileDropWin32(v.HWND, a.DroppedFiles, a.Window)
}

func initFileDropWin32(hwnd uintptr, ch chan<- []string, win *app.Window) {
	// Povolit příjem WM_DROPFILES
	pDragAcceptFiles.Call(hwnd, 1)

	// Uložit originální window procedure
	origWndProc, _, _ := pGetWindowLongPtr.Call(hwnd, _GWLP_WNDPROC)

	// Subclass — zachytit WM_DROPFILES, ostatní předat dál
	newProc := syscall.NewCallback(func(hWnd, msg, wParam, lParam uintptr) uintptr {
		if msg == _WM_DROPFILES {
			hDrop := wParam

			count, _, _ := pDragQueryFileW.Call(hDrop, 0xFFFFFFFF, 0, 0)
			paths := make([]string, 0, count)

			for i := uintptr(0); i < count; i++ {
				length, _, _ := pDragQueryFileW.Call(hDrop, i, 0, 0)
				buf := make([]uint16, length+1)
				pDragQueryFileW.Call(hDrop, i, uintptr(unsafe.Pointer(&buf[0])), length+1)
				paths = append(paths, syscall.UTF16ToString(buf))
			}

			pDragFinish.Call(hDrop)

			if len(paths) > 0 {
				queued := append([]string(nil), paths...)
				select {
				case ch <- queued:
				default:
					go func(ps []string) { ch <- ps }(queued)
					log.Println("drop: channel full, queueing asynchronously")
				}
				win.Invalidate()
			}
			return 0
		}

		ret, _, _ := pCallWindowProc.Call(origWndProc, hWnd, msg, wParam, lParam)
		return ret
	})

	pSetWindowLongPtr.Call(hwnd, _GWLP_WNDPROC, newProc)
}
