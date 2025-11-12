#!/bin/bash
# Sync KrankyBearLaunchPad to Linux Mint, compile, and retrieve binary

# set -e  # Exit on error (disabled to allow helpful messages)

# UPDATE THESE FOR YOUR LINUX MINT SYSTEM:
MINT_USER="allan"
MINT_HOST="192.168.1.190"  # Your Mint machine IP
MINT_PATH="/home/${MINT_USER}/KrankyBearLaunchPad"

echo "=== Syncing to Linux Mint ==="
echo "Target: ${MINT_USER}@${MINT_HOST}:${MINT_PATH}"

# Sync all source files, excluding build artifacts and OS files
cp ReleaseNotes.txt Resources
rsync -av \
  --exclude='bin/*.exe' \
  --exclude='bin/*-macos-*' \
  --exclude='bin/*-windows-*' \
  --exclude='.git/' \
  --exclude='.DS_Store' \
  . ${MINT_USER}@${MINT_HOST}:${MINT_PATH}/

echo ""
echo "=== Compiling on Linux Mint ==="
ssh ${MINT_USER}@${MINT_HOST} "cd ${MINT_PATH} && chmod +x compile-linux.sh && ./compile-linux.sh"

echo ""
echo "=== Retrieving compiled binary ==="
# Ensure local directory exists
mkdir -p ./bin/
scp ${MINT_USER}@${MINT_HOST}:${MINT_PATH}/bin/launchpad-linux ./bin/

echo ""
echo "[SUCCESS] Sync complete! Binary at: ./bin/launchpad-linux"
ls -lh ./bin/launchpad-linux

# Prerequisites for Linux Mint (Ubuntu-based):
# sudo apt update && sudo apt install -y build-essential pkg-config libasound2-dev libgl1-mesa-dev libx11-dev libxcursor-dev libxinerama-dev libxi-dev libxrandr-dev libxss-dev libxxf86vm-dev

# "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
