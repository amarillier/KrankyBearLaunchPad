// +build linux

package main

/*
#cgo pkg-config: x11
#include <X11/Xlib.h>
#include <stdlib.h>
*/
import "C"
import (
	"fyne.io/fyne/v2"
)

// getCursorPosition returns the current mouse cursor position in screen coordinates
func getCursorPosition() (x, y int) {
	display := C.XOpenDisplay(nil)
	if display == nil {
		return 0, 0
	}
	defer C.XCloseDisplay(display)

	var root C.Window
	var child C.Window
	var rootX, rootY C.int
	var winX, winY C.int
	var mask C.uint

	root = C.XDefaultRootWindow(display)
	C.XQueryPointer(display, root, &root, &child, &rootX, &rootY, &winX, &winY, &mask)

	return int(rootX), int(rootY)
}

// positionWindowOnDisplayImpl positions the window on the display containing the cursor
func positionWindowOnDisplayImpl(window fyne.Window, cursorX, cursorY int) bool {
	// On Linux/X11, we can use XGetGeometry and XRRGetMonitors to find the monitor
	// For now, we'll use a simple approach: center the window
	// (Window should already be shown before this function is called)
	window.CenterOnScreen()
	return true
}

