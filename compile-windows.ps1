# Windows compile script for KrankyBearLaunchPad / launchpad-windows (Windows build + optional packaging)
# Note: Remove Unix shebang for Windows execution via SSH

param(
    [switch]$Windows,
    [switch]$Linux,
    [switch]$All,
    [switch]$Current,
    [switch]$Package,              # After Windows build, run Inno Setup to create installer
    [string]$InnoPath = "C:\Program Files (x86)\Inno Setup 6\ISCC.exe"  # Path to ISCC.exe
)

# PowerShell execution policy and error handling (after param block)
$ErrorActionPreference = "Continue"  # Continue on errors so we can report them

# Get script directory and change to it (important for SSH execution)
$PSScriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
if ($PSScriptRoot) {
    Set-Location $PSScriptRoot
    Write-Host "Changed to script directory: $PSScriptRoot" -ForegroundColor Gray
} else {
    $PSScriptRoot = $PWD.Path
}

Write-Host "KrankyBear LaunchPad - Windows Compile Script" -ForegroundColor Cyan
Write-Host "================================================" -ForegroundColor Cyan
Write-Host "Working directory: $PWD" -ForegroundColor Gray
Write-Host "" 

# Create bin directory if it doesn't exist
New-Item -ItemType Directory -Force -Path bin | Out-Null

# Cleanup previous Windows/Linux binaries only (keep Resources/ and other assets)
Remove-Item -Path (Join-Path bin 'launchpad') -Force -ErrorAction SilentlyContinue

# Ensure no conflicting 32-bit resource objects linger
Remove-Item -Path "rsrc_windows_386.syso" -Force -ErrorAction SilentlyContinue

# Check if Go is installed
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "Error: Go is not installed. Please install Go 1.21 or later." -ForegroundColor Red
    exit 1
}

# Verify winres make is installed and run
go install github.com/tc-hib/go-winres@latest
go-winres make -arch amd64

# Ensure 64-bit build environment
$env:GOOS = "windows"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "1"
# Prefer a 64-bit MinGW if available
if (Get-Command x86_64-w64-mingw32-gcc -ErrorAction SilentlyContinue) {
    $env:CC = "x86_64-w64-mingw32-gcc"
}
# Clear any stale 32-bit cache artifacts - only clean cache if needed, or it slows builds
# go clean -cache -testcache -i | Out-Null
# Remove any stray 32-bit resource objects in the tree
Get-ChildItem -Path $PSScriptRoot -Filter "*386.syso" -Recurse -ErrorAction SilentlyContinue | Remove-Item -Force -ErrorAction SilentlyContinue

$buildFailed = $false

if ($All -or $Windows -or (-not $Linux -and -not $Current)) {
    Write-Host "Building for Windows..." -ForegroundColor Yellow
    $env:GOOS = "windows"
    $env:GOARCH = "amd64"
    # Always build as GUI app (no console window)
    $ldflags = "-s -w -H windowsgui"
    go build -ldflags="$ldflags" -trimpath -o bin/launchpad-windows.exe
    if ($LASTEXITCODE -eq 0) {
        Write-Host "✓ Windows build successful" -ForegroundColor Green
        if ($Package) {
            Write-Host "Packaging Windows installer with Inno Setup..." -ForegroundColor Yellow
            # Copy/rename to match Inno script expectation
            try {
                Copy-Item -Path (Join-Path $PSScriptRoot "bin/launchpad-windows.exe") -Destination (Join-Path $PSScriptRoot "launchpad-windows.exe") -Force
            } catch {
                Write-Host "Failed to copy Windows binary for packaging: $_" -ForegroundColor Red
                $buildFailed = $true
            }

            if (Test-Path $InnoPath) {
                & "$InnoPath" "Inno/KrankyBearLaunchPad.iss"
                if ($LASTEXITCODE -eq 0) {
                    Write-Host "✓ Inno Setup packaging complete (see installers/ folder)" -ForegroundColor Green
                } else {
                    Write-Host "✗ Inno Setup packaging failed (exit $LASTEXITCODE)" -ForegroundColor Red
                    $buildFailed = $true
                }
            } else {
                Write-Host "Inno Setup not found at: $InnoPath" -ForegroundColor Red
                Write-Host "Install Inno Setup 6 and/or pass -InnoPath to this script." -ForegroundColor Yellow
                $buildFailed = $true
            }
        }
    } else {
        Write-Host "✗ Windows build failed" -ForegroundColor Red
        $buildFailed = $true
    }
    Write-Host ""
}

if ($All -or $Linux) {
    if ($env:OS -eq 'Windows_NT') {
        Write-Host "Skipping Linux build on Windows (CGO/OpenGL/ALSA cross-compile unsupported)." -ForegroundColor Yellow
        Write-Host "Build Linux on a Linux host using ./compile-linux.sh or sync2ubuntu18.sh" -ForegroundColor Yellow
    } else {
        Write-Host "Building for Linux..." -ForegroundColor Yellow
        $env:GOOS = "linux"
        $env:GOARCH = "amd64"
        go build -ldflags="-s -w" -trimpath -o bin/launchpad-linux
        if ($LASTEXITCODE -eq 0) {
            Write-Host "✓ Linux build successful" -ForegroundColor Green
        } else {
            Write-Host "✗ Linux build failed" -ForegroundColor Red
            $buildFailed = $true
        }
        Write-Host ""
    }
}

if ($Current) {
    Write-Host "Building for current platform..." -ForegroundColor Yellow
    go build -ldflags="-s -w" -trimpath -o bin/launchpad
    if ($LASTEXITCODE -eq 0) {
        Write-Host "✓ Current platform build successful" -ForegroundColor Green
    } else {
        Write-Host "✗ Current platform build failed" -ForegroundColor Red
        $buildFailed = $true
    }
    Write-Host ""
}

Write-Host "================================================" -ForegroundColor Cyan
if ($buildFailed) {
    Write-Host "One or more build steps failed." -ForegroundColor Red
    exit 1
} else {
    Write-Host "Compile complete! Binaries are in the bin/ directory." -ForegroundColor Green
    Get-ChildItem -Path bin -Filter "*launchpad*" | Format-Table Name, Length -AutoSize
}

# "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
