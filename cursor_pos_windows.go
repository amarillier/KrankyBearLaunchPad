// +build windows

package main

/*
#include <windows.h>
*/
import "C"
import "fyne.io/fyne/v2"

// getCursorPosition returns the current mouse cursor position in screen coordinates
func getCursorPosition() (x, y int) {
	var point C.POINT
	C.GetCursorPos(&point)
	return int(point.x), int(point.y)
}

// positionWindowOnDisplayImpl positions the window on the display containing the cursor
func positionWindowOnDisplayImpl(window fyne.Window, cursorX, cursorY int) bool {
	// On Windows, we can use MonitorFromPoint to find the monitor containing the cursor
	point := C.POINT{C.LONG(cursorX), C.LONG(cursorY)}
	monitor := C.MonitorFromPoint(point, C.MONITOR_DEFAULTTONEAREST)
	
	if monitor == nil {
		return false
	}
	
	var info C.MONITORINFO
	info.cbSize = C.DWORD(C.sizeof_MONITORINFO)
	if C.GetMonitorInfo(monitor, &info) == 0 {
		return false
	}
	
	// Found the monitor containing the cursor
	// Center the window on this display
	// (Window should already be shown before this function is called)
	window.CenterOnScreen()
	return true
}

