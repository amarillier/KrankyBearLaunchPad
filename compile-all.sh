#!/usr/bin/env bash

set -euo pipefail

echo "KrankyBearLaunchPad - Linux, Mac, Windows Sync, Compile. Package and Sync Back (bash)"
echo "================================================"
echo

# Create bin directory if it doesn't exist
mkdir -p bin

# Cleanup previous binaries and installers only (keep other artifacts)
rm -f tailer-windows.exe
rm -f bin/tailer*
rm -f installers/*tailer*
rm -f installers/KrankyBearLaunchPadSetup*

# mount the Windows share if not already mounted
if ! mount | grep "AllanMWin"
then
    echo "Not mounted, mounting"
    mount -t smbfs //allanm@allanm.marillier.local/AllanMWin ~/AllanMWin
fi
rm -f ~/AllanMWin/Allan/Source/go/KrankyBearLaunchPad/bin/*.exe
rm -f ~/AllanMWin/Allan/Source/go/KrankyBearLaunchPad/installers/*

echo "MacOS"
./compile-mac.sh
./package.sh mac
ARCH=amd64 ./package.sh mac

echo "Linux"
./sync2ubuntu18.sh
./package.sh linux

echo "Windows"
echo "syncing to windows"
./sync2windows.sh

echo "compiling on windows"
./compile-windows-ssh.sh -windows -package

echo "syncing back"
./sync2windows.sh sync-back
rm -f tailer-windows.exe

echo "Not setting icons"
# ./setIcon.sh Resources/Images/KrankyBearHogwartsSorting.png bin/tailer-windows.exe

ls -al bin
ls -al installers

# "Now this is not even the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
