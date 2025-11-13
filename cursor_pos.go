// +build !darwin,!windows,!linux

package main

import "fyne.io/fyne/v2"

// getCursorPosition returns the current mouse cursor position in screen coordinates
// Default implementation returns (0, 0) - platform-specific implementations override this
func getCursorPosition() (x, y int) {
	return 0, 0
}

// positionWindowOnDisplayImpl attempts to position the window on the display containing the cursor
// Default implementation returns false - platform-specific implementations override this
func positionWindowOnDisplayImpl(window fyne.Window, cursorX, cursorY int) bool {
	return false
}

