#!/usr/bin/env bash

set -euo pipefail
PATH="/opt/homebrew/bin:$PATH"

# fpm-based packager for LaunchPad
# - macOS: Packages bin/launchpad-macos-{arch} as .pkg installer to /Applications
# - Linux: Packages bin/launchpad-linux as .deb and .rpm installers
# - macOS: Installs as KrankyBearLaunchPad.app bundle structure
# - Linux: Includes bin/Images -> /opt/local/bin/Resources/Images
# - Linux: Includes bin/Sounds -> /opt/local/bin/Resources/Sounds
# - Outputs to ./installers (configurable)

usage() {
  cat <<EOF
Usage: ./package.sh [linux|mac|all] [ENV_VARS]

Arguments:
  linux       Build Linux packages (.deb and .rpm)
  mac         Build macOS package (.pkg)
  all         Build both Linux and macOS packages

Environment variables (optional):
  VERSION     Package version (default: 0.1.0)
  ITERATION   Package iteration/release (default: 1)
  ARCH        Target arch: amd64|arm64 (default: native for mac, amd64 for linux)
  OUTDIR      Output directory (default: ./installers)
  MAINTAINER  Maintainer (default: amarillier@gmail.com)
  VENDOR      Vendor (default: KrankyBear)
  URL         Project URL (default: https://github.com/amarillier/KrankyBearLaunchPad)
  LICENSE     License (default: GNU GPL v3)

Examples:
  # Build macOS package
  ./package.sh mac
  ./package.sh mac VERSION=1.2.3 ARCH=amd64
  
  # Build Linux packages
  ./package.sh linux
  ./package.sh linux VERSION=1.2.3 ARCH=amd64
  
  # Build both
  ./package.sh all VERSION=1.2.3
EOF
}

# Parse command-line arguments
if [[ $# -eq 0 ]]
then
  usage
  exit 0
fi

TYPE_ARG="${1:-}"
shift  # Remove first argument, keep rest for env vars

# Validate TYPE argument
case "$TYPE_ARG" in
  linux|mac|all)
    # Valid argument
    ;;
  -h|--help|-?)
    usage
    exit 0
    ;;
  *)
    echo "Error: Invalid argument '$TYPE_ARG'. Must be 'linux', 'mac', or 'all'." >&2
    echo ""
    usage
    exit 1
    ;;
esac

# Process remaining arguments as environment variable assignments
# This allows: ./package.sh linux VERSION=1.2.3 ARCH=amd64
for arg in "$@"
do
  if [[ "$arg" == *"="* ]]
  then
    export "$arg"
  fi
done

# Configurable via env vars
NAME=${NAME:-KrankyBearLaunchPad}
VERSION=${VERSION:-0.1.3}
ITERATION=${ITERATION:-1}
OUTDIR=${OUTDIR:-./installers}
MAINTAINER=${MAINTAINER:-"amarillier@gmail.com"}
VENDOR=${VENDOR:-"KrankyBear"}
URL=${URL:-"https://github.com/amarillier/KrankyBearLaunchPad"}
LICENSE=${LICENSE:-"GNU GPL v3"}

# Function to build packages for a specific type
build_package() {
  local TYPE="$1"
  
  # Architecture handling
  local ARCH="${ARCH:-}"
  if [[ -z "$ARCH" ]]
  then
    if [[ "$TYPE" == "mac" ]]
    then
      # Default to native macOS arch
      ARCH=$(uname -m)
      [[ "$ARCH" == "x86_64" ]] && ARCH=amd64
    else
      ARCH=amd64
    fi
  fi

  case "$ARCH" in
    amd64|x86_64)
      DEB_ARCH=amd64
      RPM_ARCH=x86_64
      PKG_ARCH=amd64
      ;;
    arm64|aarch64)
      DEB_ARCH=arm64
      RPM_ARCH=aarch64
      PKG_ARCH=arm64
      ;;
    *)
      # Fall back to using the same string
      DEB_ARCH="$ARCH"
      RPM_ARCH="$ARCH"
      PKG_ARCH="$ARCH"
      ;;
  esac

  # Source assets - depends on TYPE
  # Determine binary name early for macOS staging
  BIN_NAME="KrankyBearLaunchPad"
  
  if [[ "$TYPE" == "mac" ]]
  then
    SRC_BIN="bin/launchpad-macos-${PKG_ARCH}"
    SRC_RESOURCES="Resources"
    SRC_INFO_PLIST="Info-plist.txt"
    SRC_README_PLIST="Readme-plist.txt"
    # These will be set from staging directory later
    SRC_IMAGES=""
    SRC_SOUNDS=""
  else
    SRC_BIN="bin/launchpad-linux"
    SRC_IMAGES="Resources/Images"
    SRC_SOUNDS="Resources/Sounds"
  fi

  # Validate sources
  if [[ ! -f "$SRC_BIN" ]]
  then
    if [[ "$TYPE" == "mac" ]]
    then
      echo "Error: Missing $SRC_BIN (build your macOS binary first with ./compile-mac.sh)." >&2
    else
      echo "Error: Missing $SRC_BIN (build your Linux binary first)." >&2
    fi
    exit 1
  fi
  if [[ "$TYPE" == "mac" ]]
  then
    if [[ ! -d "$SRC_RESOURCES" ]]
    then
      echo "Error: Missing directory $SRC_RESOURCES" >&2
      exit 1
    fi
    if [[ ! -d "$SRC_RESOURCES/Images" ]]
    then
      echo "Error: Missing directory $SRC_RESOURCES/Images" >&2
      exit 1
    fi
    if [[ ! -d "$SRC_RESOURCES/Sounds" ]]
    then
      echo "Error: Missing directory $SRC_RESOURCES/Sounds" >&2
      exit 1
    fi
    if [[ ! -f "$SRC_INFO_PLIST" ]]
    then
      echo "Error: Missing file $SRC_INFO_PLIST" >&2
      exit 1
    fi
  else
    if [[ ! -d "$SRC_IMAGES" ]]
    then
      echo "Error: Missing directory $SRC_IMAGES" >&2
      exit 1
    fi
    if [[ ! -d "$SRC_SOUNDS" ]]
    then
      echo "Error: Missing directory $SRC_SOUNDS" >&2
      exit 1
    fi
  fi

  mkdir -p "$OUTDIR"

  # For macOS: Build complete .app bundle, then use it as source for fpm
  # For Linux packages built on macOS, create staging directory with files
  # stripped of extended attributes to avoid tar header compatibility issues
  STAGING_DIR=""
  APP_BUNDLE=""
  
  if [[ "$TYPE" == "mac" ]]
  then
    # Build the complete .app bundle structure
    APP_BUNDLE="KrankyBearLaunchPad.app"
    echo "Building .app bundle: $APP_BUNDLE..."
    
    # Remove existing bundle if present
    rm -rf "$APP_BUNDLE"
    
    # Create app bundle structure
    mkdir -p "$APP_BUNDLE/Contents/MacOS/Resources"
    
    # Copy binary to Contents/MacOS
    cp "$SRC_BIN" "$APP_BUNDLE/Contents/MacOS/$BIN_NAME"
    
    # Create symlink: launchpad -> KrankyBearLaunchPad in Contents/MacOS
    ln -s "$BIN_NAME" "$APP_BUNDLE/Contents/MacOS/launchpad"
    
    # Copy Resources subdirectories to Contents/MacOS/Resources
    cp -R "$SRC_RESOURCES/Images" "$APP_BUNDLE/Contents/MacOS/Resources/Images"
    cp -R "$SRC_RESOURCES/Sounds" "$APP_BUNDLE/Contents/MacOS/Resources/Sounds"
    
    # Set proper permissions on Resources directories (755 = rwxr-xr-x)
    chmod -R 755 "$APP_BUNDLE/Contents/MacOS/Resources"
    
    # Copy Info-plist.txt to Contents
    cp "$SRC_INFO_PLIST" "$APP_BUNDLE/Contents/Info-plist.txt"
    cp "$SRC_README_PLIST" "$APP_BUNDLE/Contents/Readme-plist.txt"
    
    # Verify files were copied correctly
    if [[ ! -d "$APP_BUNDLE/Contents/MacOS/Resources/Images" ]] || [[ -z "$(ls -A "$APP_BUNDLE/Contents/MacOS/Resources/Images" 2>/dev/null)" ]]
    then
      echo "Error: Images directory is empty or missing in app bundle" >&2
      exit 1
    fi
    if [[ ! -d "$APP_BUNDLE/Contents/MacOS/Resources/Sounds" ]] || [[ -z "$(ls -A "$APP_BUNDLE/Contents/MacOS/Resources/Sounds" 2>/dev/null)" ]]
    then
      echo "Error: Sounds directory is empty or missing in app bundle" >&2
      exit 1
    fi
    
    echo "App bundle created successfully."
    
  elif [[ "$TYPE" == "linux" && "$(uname -s)" == "Darwin" ]]
  then
    echo "Creating staging directory without macOS extended attributes..."
    STAGING_DIR=$(mktemp -d -t fpm-staging.XXXXXX)
    cleanup_staging() { rm -rf "$STAGING_DIR"; }
    trap cleanup_staging EXIT INT TERM
    
    # Copy files to staging directory using cp -X which explicitly excludes
    # extended attributes (xattr) that cause issues with Ubuntu's dpkg
    cp -X "$SRC_BIN" "$STAGING_DIR/krankybearlaunchpad"
    cp -XR "$SRC_IMAGES" "$STAGING_DIR/Images"
    cp -XR "$SRC_SOUNDS" "$STAGING_DIR/Sounds"
    
    # Create symlink: launchpad -> krankybearlaunchpad (similar to macOS)
    ln -s "krankybearlaunchpad" "$STAGING_DIR/launchpad"
    
    # Aggressively strip any remaining extended attributes from staging directory
    # This is critical for Ubuntu dpkg compatibility
    if command -v xattr >/dev/null 2>&1
    then
      xattr -cr "$STAGING_DIR" 2>/dev/null || true
      # Also remove any AppleDouble files (._*)
      find "$STAGING_DIR" -name '._*' -delete 2>/dev/null || true
    fi
    
    # Update source paths to point to staging directory
    SRC_BIN="$STAGING_DIR/krankybearlaunchpad"
    SRC_SYMLINK_LAUNCHPAD="$STAGING_DIR/launchpad"
    SRC_IMAGES="$STAGING_DIR/Images"
    SRC_SOUNDS="$STAGING_DIR/Sounds"
    
    # Also set environment variable as additional safeguard
    export COPYFILE_DISABLE=1
  fi

  COMMON_ARGS=(
    -s dir
    -n "$NAME"
    -v "$VERSION"
    --iteration "$ITERATION"
    --maintainer "$MAINTAINER"
    --vendor "$VENDOR"
    --url "$URL"
    --license "$LICENSE"
    --description "KrankyBear LaunchPad - A cross-platform GUI application launcher"
    -f
  )

  if [[ "$TYPE" == "mac" ]]
  then
    # macOS .pkg installer - installs to /Applications as .app bundle
    PKG_OUTFILE="$OUTDIR/krankybearlaunchpad_${VERSION}-${ITERATION}_${PKG_ARCH}.pkg"
    echo "Building macOS .pkg ($PKG_ARCH) -> $PKG_OUTFILE..."
    
    # macOS .app bundle structure:
    # /Applications/KrankyBearLaunchPad.app/Contents/Info-plist.txt (sample Info.plist)
    # /Applications/KrankyBearLaunchPad.app/Contents/MacOS/KrankyBearLaunchPad (executable)
    # /Applications/KrankyBearLaunchPad.app/Contents/MacOS/Resources/Images (resources)
    # /Applications/KrankyBearLaunchPad.app/Contents/MacOS/Resources/Sounds (sounds)
    # Note: App looks for Resources/Images and Resources/Sounds relative to executable
    APP_NAME="KrankyBearLaunchPad.app"
    APP_DIR="/Applications/$APP_NAME"
    CONTENTS_DIR="$APP_DIR/Contents"
    MACOS_DIR="$CONTENTS_DIR/MacOS"
    RESOURCES_DIR="$MACOS_DIR/Resources"
    
    # Note: BIN_NAME is already defined earlier
    # Map files individually from the app bundle to avoid directory nesting
    # Build fpm file list
    FPM_FILES=(
      "$APP_BUNDLE/Contents/MacOS/$BIN_NAME=$MACOS_DIR/$BIN_NAME"
      "$APP_BUNDLE/Contents/MacOS/launchpad=$MACOS_DIR/launchpad"
      "$APP_BUNDLE/Contents/Info-plist.txt=$CONTENTS_DIR/Info-plist.txt"
      "$APP_BUNDLE/Contents/Readme-plist.txt=$CONTENTS_DIR/Readme-plist.txt"
    )
    
    # Map Images files
    while IFS= read -r -d '' file; do
      rel_path="${file#$APP_BUNDLE/Contents/MacOS/Resources/Images/}"
      FPM_FILES+=("$file=$RESOURCES_DIR/Images/$rel_path")
    done < <(find "$APP_BUNDLE/Contents/MacOS/Resources/Images" -type f -print0)
    
    # Map Sounds files
    while IFS= read -r -d '' file; do
      rel_path="${file#$APP_BUNDLE/Contents/MacOS/Resources/Sounds/}"
      FPM_FILES+=("$file=$RESOURCES_DIR/Sounds/$rel_path")
    done < <(find "$APP_BUNDLE/Contents/MacOS/Resources/Sounds" -type f -print0)
    
    fpm \
      "${COMMON_ARGS[@]}" \
      -t osxpkg \
      -a "$PKG_ARCH" \
      --directories "$APP_DIR" \
      --directories "$CONTENTS_DIR" \
      --directories "$MACOS_DIR" \
      --directories "$RESOURCES_DIR" \
      --package "$PKG_OUTFILE" \
      "${FPM_FILES[@]}"
    
    ./setIcon.sh Resources/Images/KrankyBearTrapperRedPlaid.png "$PKG_OUTFILE"
    echo ""
    echo "Done. Package created:"
    echo "  $PKG_OUTFILE"
    
  else
    # Linux .deb and .rpm installers
    DEB_OUTFILE="$OUTDIR/krankybearlaunchpad_${VERSION}-${ITERATION}_${DEB_ARCH}.deb"
    RPM_OUTFILE="$OUTDIR/krankybearlaunchpad_${VERSION}-${ITERATION}_${RPM_ARCH}.rpm"
    
    echo "Building .deb ($DEB_ARCH) -> $DEB_OUTFILE..."
    fpm \
      "${COMMON_ARGS[@]}" \
      -t deb \
      -a "$DEB_ARCH" \
      --deb-no-default-config-files \
      --directories /opt/local/bin \
      --directories /opt/local/bin/Resources \
      --package "$DEB_OUTFILE" \
      "$SRC_BIN=/opt/local/bin/krankybearlaunchpad" \
      "$SRC_SYMLINK_LAUNCHPAD=/opt/local/bin/launchpad" \
      "$SRC_IMAGES=/opt/local/bin/Resources/Images" \
      "$SRC_SOUNDS=/opt/local/bin/Resources/Sounds"
    
    echo ""
    echo "Building .rpm ($RPM_ARCH) -> $RPM_OUTFILE..."
    fpm \
      "${COMMON_ARGS[@]}" \
      -t rpm \
      -a "$RPM_ARCH" \
      --rpm-os linux \
      --directories /opt/local/bin \
      --directories /opt/local/bin/Resources \
      --package "$RPM_OUTFILE" \
      "$SRC_BIN=/opt/local/bin/krankybearlaunchpad" \
      "$SRC_SYMLINK_LAUNCHPAD=/opt/local/bin/launchpad" \
      "$SRC_IMAGES=/opt/local/bin/Resources/Images" \
      "$SRC_SOUNDS=/opt/local/bin/Resources/Sounds"
    
    echo ""
    echo "Done. Packages created:"
    echo "  $DEB_OUTFILE"
    echo "  $RPM_OUTFILE"
  fi
}

# Check for fpm before starting
if ! command -v fpm >/dev/null 2>&1
then
  echo "Error: fpm not found. Install with: gem install fpm" >&2
  exit 1
fi

# Build packages based on TYPE_ARG
case "$TYPE_ARG" in
  linux)
    build_package "linux"
    ;;
  mac)
    build_package "mac"
    ;;
  all)
    echo "Building all packages..."
    echo ""
    build_package "linux"
    echo ""
    build_package "mac"
    ;;
esac

# "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
