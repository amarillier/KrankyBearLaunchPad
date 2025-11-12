#!/bin/bash
# Sync KrankyBearLaunchPad / launchpad-windows to Windows via mounted share

set -e  # Exit on error

usage() {
  cat <<EOF
Usage: ./sync2windows.sh [sync-back]

Arguments:
  sync-back    Only sync back compiled binaries from Windows (bin/*.exe and installers/*.exe)
               If not specified, syncs source files to Windows and then syncs back changes

Examples:
  ./sync2windows.sh              # Full sync: source to Windows, then changes back
  ./sync2windows.sh sync-back    # Only sync back compiled binaries
EOF
}

# Check for help flag
if [[ "${1:-}" == "-h" || "${1:-}" == "--help" || "${1:-}" == "-?" ]]
then
  usage
  exit 0
fi

SYNC_BACK_ONLY="${1:-}"

# mount the Windows share if not already mounted
if ! mount | grep "AllanMWin"
then
    echo "Not mounted, mounting"
    mount -t smbfs //allanm@allanm.marillier.local/AllanMWin ~/AllanMWin
fi

# Configuration - ADJUST THESE PATHS
WINDOWS_SHARE="$HOME/AllanMWin/Allan/Source/go/KrankyBearLaunchPad"  # Mounted Windows share path
WINDOWS_HOST="192.168.1.9"  # Your Windows machine IP (for PowerShell remoting)
WINDOWS_USER="allan"   # Windows username

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Check if Windows share is mounted
if [ ! -d "$WINDOWS_SHARE" ]
then
    echo "ERROR: Windows share not mounted at: $WINDOWS_SHARE"
    echo "Please mount your Windows share first, or update WINDOWS_SHARE path in this script"
    exit 1
fi

# Create directory if it doesn't exist
mkdir -p "$WINDOWS_SHARE"

# If sync-back only, skip the forward sync
if [[ "$SYNC_BACK_ONLY" == "sync-back" ]]
then
    echo -e "${BLUE}=== Syncing back compiled binaries from Windows ===${NC}"
    
    # Sync back only the compiled binaries
    mkdir -p ./bin ./installers
    
    if [ -f "$WINDOWS_SHARE/bin/launchpad-windows.exe" ]
    then
        echo "Syncing bin/launchpad-windows.exe..."
        rsync -av --progress "$WINDOWS_SHARE/bin/launchpad-windows.exe" ./bin/
    fi
    
    # Sync back any bin and installer executables
    if [ -d "$WINDOWS_SHARE/bin" ]
    then
        echo "Syncing bin/*.exe..."
        rsync -av --progress --include='*.exe' --exclude='*' "$WINDOWS_SHARE/bin/" ./bin/
    fi
    if [ -d "$WINDOWS_SHARE/installers" ]
    then
        echo "Syncing installers/*.exe..."
        rsync -av --progress --include='*.exe' --exclude='*' "$WINDOWS_SHARE/installers/" ./installers/
    fi
    
    echo -e "${GREEN}✓ Sync-back complete${NC}"
    
    # Show what was synced
    if [ -f "./bin/launchpad-windows.exe" ]
    then
        echo -e "${GREEN}✓ Windows binary: ./bin/launchpad-windows.exe${NC}"
        ls -lh ./bin/launchpad-windows.exe
    fi
    
    if ls ./installers/*.exe 1> /dev/null 2>&1
    then
        echo -e "${GREEN}✓ Windows installer(s):${NC}"
        ls -lh ./installers/*.exe
    fi
    
    exit 0
fi

# Full sync mode
echo -e "${BLUE}=== Syncing to Windows ===${NC}"

# Sync files to Windows share (do not delete Windows-built artifacts)
# Exclude vendor directory and other large/unnecessary directories to speed up sync
# Note: vendor/ is excluded because Go will download dependencies on Windows anyway
cp ReleaseNotes.txt Resources 2>/dev/null || true
echo "Copying files to Windows share (excluding build outputs and vendor directory)..."
echo "Note: vendor/ directory excluded - Go will download dependencies on Windows"
rsync -av \
  --exclude='bin/**' \
  --exclude='installers/**' \
  --exclude='launchpad-windows.exe' \
  --exclude='vendor/**' \
  --exclude='.git/' \
  --exclude='.DS_Store' \
  --exclude='*.swp' \
  --exclude='*.swo' \
  --exclude='*~' \
  . "$WINDOWS_SHARE/"

echo -e "${GREEN}✓ Files synced to: $WINDOWS_SHARE${NC}"
echo ""

# Compilation instructions
echo -e "${BLUE}=== Next: Compile on Windows ===${NC}"
echo "To compile the Windows binary, open PowerShell on Windows and run:"
echo ""
echo "  cd C:\\Allan\\Source\\go\\KrankyBearLaunchPad"
echo "  go build -ldflags=\"-s -w -H windowsgui\" -trimpath -o bin\\launchpad-windows.exe"
echo ""
echo "Or use the compile script:"
echo "  .\\compile-windows.ps1"
echo ""
echo "After compiling, run this script again to sync back the binary."
echo "  Or use: ./sync2windows.sh sync-back  (faster - only syncs binaries)"

echo ""
echo -e "${GREEN}=== Sync Complete ===${NC}"
echo ""
echo "Files are at: $WINDOWS_SHARE"
echo ""

# Sync back any changed files from Windows to Mac
echo -e "${BLUE}=== Syncing changes back from Windows ===${NC}"

# Sync back source files that may have been edited on Windows (but skip vendor)
rsync -av --update \
  --include='*.go' \
  --include='*.mod' \
  --include='*.sum' \
  --include='*.md' \
  --include='*.sh' \
  --include='*.ps1' \
  --include='*.bat' \
  --include='*.yml' \
  --include='*.yaml' \
  --include='bin/' \
  --include='bin/launchpad-windows.exe' \
  --include='installers/' \
  --include='installers/*.exe' \
  --include='launchpad-windows.exe' \
  --exclude='bin/*-linux-*' \
  --exclude='bin/*-macos-*' \
  --exclude='vendor/**' \
  --exclude='*' \
  "$WINDOWS_SHARE/" .

echo -e "${GREEN}✓ Files synced back from Windows${NC}"
echo ""

# Check if binary exists and show info
if [ -f "./bin/launchpad-windows.exe" ]
then
    echo -e "${GREEN}✓ Windows binary found: ./bin/launchpad-windows.exe${NC}"
    ls -lh ./bin/launchpad-windows.exe
    if [ -f "./installers/launchpad-windows.exe" ]
    then
        echo -e "${GREEN}✓ Windows installer found: ./installers/launchpad-windows.exe${NC}"
        ls -lh ./installers/launchpad-windows.exe
    else
        echo "Note: Windows installer not found (needs to be compiled on Windows)"
        echo ""
        echo "Next steps on Windows:"
        echo "1. Open PowerShell"
        echo "2. cd to C:\\Allan\\Source\\go\\KrankyBearLaunchPad"
        echo "3. Run: .\\compile-windows.ps1 -Windows -Package"
        echo ""
        echo "Then run: ./sync2windows.sh sync-back  (to copy the installer back)"
    fi
else
    echo "Note: Windows binary not found (needs to be compiled on Windows)"
    echo ""
    echo "Next steps on Windows:"
    echo "1. Open PowerShell"
    echo "2. cd to C:\\Allan\\Source\\go\\KrankyBearLaunchPad"
    echo "3. Run: .\\compile-windows.ps1 -Windows -Package"
    echo "  OR: Run: go build -ldflags=\"-s -w -H windowsgui\" -trimpath -o bin\\launchpad-windows.exe"
    echo ""
    echo "Then run: ./sync2windows.sh sync-back  (to copy the binary back)"
fi

# "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
