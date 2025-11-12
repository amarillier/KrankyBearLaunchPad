#!/bin/bash

echo "KrankyBear LaunchPad - Linux Compile Script"
echo "========================================="
echo ""

# Create bin directory if it doesn't exist
mkdir -p bin

# cleanup any existing binaries
rm -f bin/tailer*

# Check if Go is installed
if ! command -v go &> /dev/null
then
    echo "Error: Go is not installed. Please install Go 1.21 or later."
    exit 1
fi

echo "Building for Linux (native)..."
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -trimpath -o bin/tailer-linux
if [ $? -eq 0 ]
then
    echo "✓ Linux build successful"
else
    echo "✗ Linux build failed"
    exit 1
fi

echo ""
echo "Done. Binary at: bin/tailer-linux"

# Note about runtime audio dependency on Linux (ALSA via libasound)
if command -v ldd >/dev/null 2>&1
then
    echo ""
    echo "Checking runtime dependencies (ldd)..."
    ldd bin/tailer-linux | grep -E "libasound|libGL|libX11" || true
fi

echo ""
echo "Runtime requirements (not bundled):"
echo "- ALSA library (libasound2) for sound playback"
echo "- OpenGL/X11 libs for Fyne (typically present on desktop distros)"
echo ""
echo "Install on Ubuntu/Debian: sudo apt-get install -y libasound2 libgl1-mesa-glx libx11-6"
echo "Install on Ubuntu/Debian: sudo apt-get install -y build-essential pkg-config libasound2-dev libgl1-mesa-dev libx11-dev"
echo "Install on Fedora/RHEL:   sudo dnf install -y alsa-lib mesa-libGL libX11"

# "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
