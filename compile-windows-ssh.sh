#!/bin/bash
# Helper script to compile on Windows via SSH
# Usage: ./compile-windows-ssh.sh [options]
# Options: -Windows, -Package, -All, etc.

WINDOWS_HOST="${WINDOWS_HOST:-192.168.1.9}"
WINDOWS_USER="${WINDOWS_USER:-allanm}"
SCRIPT_DIR="C:\\Allan\\Source\\go\\KrankyBearLaunchPad"
BATCH_FILE="${SCRIPT_DIR}\\compile-windows.bat"

# Build parameters string from arguments
PARAMS=""
for arg in "$@"; do
    PARAMS="$PARAMS $arg"
done

echo "Connecting to Windows host: $WINDOWS_USER@$WINDOWS_HOST"
echo "Using batch wrapper: $BATCH_FILE"
echo "Parameters: $PARAMS"
echo ""

# Use the batch file wrapper - it handles line endings and execution context better
# The batch file will invoke PowerShell with proper settings
ssh "$WINDOWS_USER@$WINDOWS_HOST" "\"$BATCH_FILE\"$PARAMS"

# "Now this is not even the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
