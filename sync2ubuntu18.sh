#!/bin/bash
# Sync KrankyBearLaunchPad to Ubuntu 18.04, compile, and retrieve binary
# Ubuntu 18.04 has glibc 2.27 which provides excellent compatibility

# set -e  # Exit on error

# UPDATE THESE FOR YOUR UBUNTU 18.04 SYSTEM:
UBUNTU_USER="allan"
UBUNTU_HOST="192.168.1.18"  # Your Ubuntu 18.04 IP address
UBUNTU_PATH="/home/${UBUNTU_USER}/KrankyBearLaunchPad"

echo "=== Syncing to Ubuntu 18.04 ==="
echo "Target: ${UBUNTU_USER}@${UBUNTU_HOST}:${UBUNTU_PATH}"
# Sync all source files, excluding build artifacts and OS files
cp ReleaseNotes.txt Resources
rsync -av --exclude='bin/*.exe' --exclude='bin/*-macos-*' --exclude='bin/*-windows-*' --exclude='.git/' --exclude='.DS_Store' \
  . ${UBUNTU_USER}@${UBUNTU_HOST}:${UBUNTU_PATH}/

echo ""
echo "=== Compiling on Ubuntu 18.04 ==="
# Run the project's Linux compile script on the remote host
ssh ${UBUNTU_USER}@${UBUNTU_HOST} "cd ${UBUNTU_PATH} && chmod +x compile-linux.sh && ./compile-linux.sh"

echo ""
echo "=== Retrieving compiled binary ==="
# Ensure local directory exists
mkdir -p ./bin/
scp ${UBUNTU_USER}@${UBUNTU_HOST}:${UBUNTU_PATH}/bin/launchpad-linux ./bin/

echo ""
echo "[SUCCESS] Sync complete! Binary at: ./bin/tailer-linux"
ls -lh ./bin/tailer-linux

# Prerequisites for Ubuntu 18.04:
# sudo apt update && sudo apt install -y gcc pkg-config libgl1-mesa-dev libxcursor-dev libxrandr-dev libxinerama-dev libxi-dev libxxf86vm-dev libx11-dev libxcursor-dev libxss-dev
# 
# Install Go 1.21+ on Ubuntu 18.04:
# wget https://go.dev/dl/go1.21.0.linux-amd64.tar.gz
# sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.21.0.linux-amd64.tar.gz
# echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
# source ~/.bashrc
#
# For KrankyBearLaunchPad, you'll also need (if not already installed):
# sudo apt install -y libgl1-mesa-dev libx11-dev libxcursor-dev libxinerama-dev libxi-dev libxrandr-dev libxss-dev libxxf86vm-dev


# "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
