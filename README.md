# KrankyBear LaunchPad

A cross-platform application launcher similar to macOS LaunchPad, built with Go and Fyne GUI.

## Features

- **Cross-platform**: Works on macOS, Linux, and Windows
- **Application Discovery**: Automatically discovers installed applications
- **Package Manager Support**: Detects apps from:
  - macOS: Homebrew (Casks)
  - Windows: Chocolatey, Winget, Scoop
  - Linux: APT, RPM/YUM/DNF, Zypper, Snap
- **Tab Organization**: Create custom tabs to organize applications
- **Custom Apps**: Add manually installed applications

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
