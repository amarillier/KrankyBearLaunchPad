#!/bin/bash

echo "KrankyBear LaunchPad - Linux Compile Script"
echo "========================================="
echo ""

# Create bin directory if it doesn't exist
if [ ! -d "bin" ]
then
    mkdir -p bin
fi

# cleanup any existing binaries
rm -f bin/launchpad*

# Check if Go is installed
if ! command -v go &> /dev/null
then
    echo "Error: Go is not installed. Please install Go 1.21 or later."
    exit 1
fi

# fast update fyne before compile
go get fyne.io/fyne/v2@latest # or a specific version like @v2.4.0
go mod tidy
go mod vendor

echo "Building for Linux (native)..."
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -trimpath -o bin/launchpad-linux
if [ $? -eq 0 ]
then
    echo "✓ Linux build successful"
else
    echo "✗ Linux build failed"
    exit 1
fi

echo ""
echo "Done. Binary at: bin/launchpad-linux"

# "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
