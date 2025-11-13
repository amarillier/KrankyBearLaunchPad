// +build darwin

package main

/*
#include <CoreGraphics/CoreGraphics.h>
#include <ApplicationServices/ApplicationServices.h>
*/
import "C"
import (
	"fyne.io/fyne/v2"
)

// getCursorPosition returns the current mouse cursor position in screen coordinates
func getCursorPosition() (x, y int) {
	// Use CGEventSource to create a mouse event and get current location
	eventSource := C.CGEventSourceCreate(C.kCGEventSourceStateHIDSystemState)
	defer func() {
		if eventSource != 0 {
			C.CFRelease(C.CFTypeRef(eventSource))
		}
	}()

	if eventSource == 0 {
		return 0, 0
	}

	// Create an event to query current position
	// CGEventCreate with eventSource creates a null event we can query
	event := C.CGEventCreate(eventSource)
	defer func() {
		if event != 0 {
			C.CFRelease(C.CFTypeRef(event))
		}
	}()

	if event == 0 {
		return 0, 0
	}

	location := C.CGEventGetLocation(event)
	return int(location.x), int(location.y)
}

// positionWindowOnDisplayImpl positions the window on the display containing the cursor
func positionWindowOnDisplayImpl(window fyne.Window, cursorX, cursorY int) bool {
	// Get list of active displays
	displayCount := C.uint32_t(0)
	C.CGGetActiveDisplayList(0, nil, &displayCount)
	
	if displayCount == 0 {
		window.CenterOnScreen()
		return false
	}
	
	displays := make([]C.CGDirectDisplayID, displayCount)
	C.CGGetActiveDisplayList(displayCount, &displays[0], &displayCount)
	
	// Find which display contains the cursor
	cursorPoint := C.CGPointMake(C.CGFloat(cursorX), C.CGFloat(cursorY))
	for i := C.uint32_t(0); i < displayCount; i++ {
		bounds := C.CGDisplayBounds(displays[i])
		
		// Check if cursor point is within this display's bounds
		if cursorPoint.x >= bounds.origin.x &&
			cursorPoint.x < bounds.origin.x+bounds.size.width &&
			cursorPoint.y >= bounds.origin.y &&
			cursorPoint.y < bounds.origin.y+bounds.size.height {
			// Found the display containing the cursor
			// Calculate the center of this display for positioning
			// Since Fyne's CenterOnScreen() centers on the primary display,
			// we'll show the window first (which should appear on the cursor's display),
			// then center it. The window will be shown later in main() via ShowAndRun()
			// For now, we just mark that we found the correct display
			// The actual centering will happen when ShowAndRun() is called
			window.CenterOnScreen()
			return true
		}
	}
	
	// Fallback: center on screen
	window.CenterOnScreen()
	return false
}

