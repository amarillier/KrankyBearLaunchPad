# KrankyBear LaunchPad

A cross-platform application launcher similar to macOS LaunchPad, built with Go and Fyne GUI. Why a new application just like MacOS Launchpad? Because with MacOS 26, Launchpad as we knew it is not the same. It is simply named Apps, looks, and behaves quite differently. If you like that, if you like using Spotlight - great, nothing wrong with them. But Launchpad was convenient and allowed customization. This is similar, but not the same, and allows you to use an identical application across all of Windows, Linux and MacOS.

Design philosophy aligns with Fyne (GUI package I use), ease of use, functionality and bug fixes, performance. Some areas are a little slow, acknowledged and attempts to make it all faster are continuous

## Features

- **Cross-platform**: Works on macOS, Linux, and Windows
- **Application Discovery**: Automatically discovers installed applications
- **Package Manager Support**: Detects apps from:
  - macOS: Homebrew (Casks)
  - Windows: Chocolatey, Winget, Scoop
  - Linux: APT, RPM/YUM/DNF, Zypper, Snap
- **Tab Organization**: Create custom tabs to organize applications
  - Create, edit, delete, and sort tabs
  - Home tab automatically includes all discovered apps
  - Tab selection is remembered between sessions
- **Custom Apps**: Add manually installed applications with custom icons
- **Theme Support**: System (follows OS), Light, and Dark themes with preference persistence
- **System Tray Integration**: Run in background with system tray menu
- **Update Checker**: Check for updates from GitHub releases
- **Window State Memory**: Remembers window size and selected tab between sessions
- **Performance Optimizations**: 
  - Icon caching at display size (64x64) for fast loading
  - Progressive icon loading for responsive startup
  - Background app discovery doesn't block UI
- **App Management**: 
  - Add apps to multiple tabs
  - Edit app properties (name, executable, icon)
  - Remove apps from tabs
  - Filter and search applications with clear button
  - Search for icons online via Google Images
- **View Modes**: Toggle between grid/icon view and compact list view
- **Multi-Monitor Support**: Window opens on the display where your cursor is located
- **Right-Click Context**: Right-click on empty tab space to quickly edit tab settings

## Quick Start

### First-Time Setup

Before building for the first time, prepare all dependencies:

**macOS/Linux:**
```bash

./prepare-deps.sh
```

**Windows (PowerShell):**
```powershell
.\prepare-deps.ps1
```

**Or using Make:**
```bash
make prepare-deps
# or
make vendor
```

This will:
1. Download all required Go packages
2. Tidy the module dependencies
3. Verify module integrity
4. Create a vendor directory for offline builds

### Building

After preparing dependencies:

```bash
# Build for current platform
make build

# Or build directly with vendor
go build -mod=vendor -o launchpad
```

## Development

### Dependency Management

The project uses Go modules. To update dependencies:

```bash
# Download and tidy dependencies
make deps

# Prepare dependencies with vendor (recommended for first-time setup)
make prepare-deps
```

### Project Structure

- `main.go` - Main application entry point and UI
- `models.go` - Data models and JSON persistence
- `discovery.go` - Platform-specific application discovery
- `theme.go` - Custom Fyne theme
- `prepare-deps.sh` - Dependency preparation script (Unix/macOS/Linux)
- `prepare-deps.ps1` - Dependency preparation script (Windows)

## Configuration

Configuration is stored in `~/.krankybear-launchpad/config.json` and includes:
- List of discovered and custom applications
- Tab definitions and app assignments

## Building for Multiple Platforms

See the `Makefile` for platform-specific build targets:
- `make build-linux` - Build for Linux
- `make build-darwin` - Build for macOS
- `make build-windows` - Build for Windows

## License

See LICENSE file for details.
