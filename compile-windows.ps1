# Windows compile script for KrankyBearLaunchPad (Windows build + optional packaging)

param(
    [switch]$Windows,
    [switch]$Package,
    [string]$InnoPath = "C:\Program Files (x86)\Inno Setup 6\ISCC.exe"
)

$ErrorActionPreference = "Continue"

# Get script directory and change to it
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
$binDir = "bin"
if (-not (Test-Path $binDir)) {
    New-Item -ItemType Directory -Path $binDir -Force | Out-Null
}

# Cleanup previous binaries
Remove-Item -Path (Join-Path bin 'launchpad') -Force -ErrorAction SilentlyContinue

# Remove ALL syso files before generating new ones
Get-ChildItem -Path $PSScriptRoot -Filter "*.syso" -Recurse -ErrorAction SilentlyContinue | Remove-Item -Force -ErrorAction SilentlyContinue
Write-Host "Cleaned up all existing syso files (if any existed)" -ForegroundColor Gray

# Check if Go is installed
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "Error: Go is not installed. Please install Go 1.21 or later." -ForegroundColor Red
    exit 1
}

# Check for vendor dependencies
if (-not (Test-Path "vendor/modules.txt")) {
    Write-Host "Error: vendor/modules.txt not found. Running prepare-deps.ps1 first..." -ForegroundColor Red
    & "$PSScriptRoot\prepare-deps.ps1"
    if ($LASTEXITCODE -ne 0) {
        Write-Host "prepare-deps.ps1 failed" -ForegroundColor Red
        exit 1
    }
    Write-Host "prepare-deps.ps1 completed successfully" -ForegroundColor Green
}

# Display Go version
$goVersion = go version
Write-Host "Using: $goVersion" -ForegroundColor Green
Write-Host ""

# Update fyne and dependencies
Write-Host "Updating dependencies..." -ForegroundColor Yellow
go get fyne.io/fyne/v2@latest
go mod tidy
go mod vendor

# Install and run go-winres
Write-Host "Installing go-winres..." -ForegroundColor Yellow
go install github.com/tc-hib/go-winres@latest

Write-Host "Generating Windows resources from winres/winres.json..." -ForegroundColor Cyan
go-winres make -arch amd64
if ($LASTEXITCODE -ne 0) {
    Write-Host "WARNING: go-winres failed. Icon may not be embedded." -ForegroundColor Yellow
} else {
    Write-Host "Windows resources generated successfully" -ForegroundColor Green
    $sysoFiles = Get-ChildItem -Path $PSScriptRoot -Filter "*.syso" -ErrorAction SilentlyContinue
    if ($sysoFiles) {
        Write-Host "Created syso files:" -ForegroundColor Gray
        $sysoFiles | ForEach-Object { Write-Host "  $($_.Name) ($([math]::Round($_.Length/1KB, 2)) KB)" -ForegroundColor Gray }
    }
}

# Set build environment
$env:GOOS = "windows"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "1"
if (Get-Command x86_64-w64-mingw32-gcc -ErrorAction SilentlyContinue) {
    $env:CC = "x86_64-w64-mingw32-gcc"
}
Get-ChildItem -Path $PSScriptRoot -Filter "*386.syso" -Recurse -ErrorAction SilentlyContinue | Remove-Item -Force -ErrorAction SilentlyContinue

$buildFailed = $false

# Build Windows binary
Write-Host "Building for Windows..." -ForegroundColor Yellow
$ldflags = "-s -w -H windowsgui"
go build -ldflags="$ldflags" -trimpath -o bin/launchpad-windows.exe
if ($LASTEXITCODE -eq 0) {
    Write-Host "Windows build successful" -ForegroundColor Green
    
    if ($Package) {
        Write-Host "Packaging Windows installer with Inno Setup..." -ForegroundColor Yellow
        try {
            Copy-Item -Path (Join-Path $PSScriptRoot "bin/launchpad-windows.exe") -Destination (Join-Path $PSScriptRoot "launchpad-windows.exe") -Force
        } catch {
            Write-Host "Failed to copy Windows binary for packaging: $_" -ForegroundColor Red
            $buildFailed = $true
        }

        if (Test-Path $InnoPath) {
            & "$InnoPath" "Inno/KrankyBearLaunchPad.iss"
            if ($LASTEXITCODE -eq 0) {
                Write-Host "Inno Setup packaging complete (see installers/ folder)" -ForegroundColor Green
            } else {
                Write-Host "Inno Setup packaging failed (exit $LASTEXITCODE)" -ForegroundColor Red
                $buildFailed = $true
            }
        } else {
            Write-Host "Inno Setup not found at: $InnoPath" -ForegroundColor Red
            $buildFailed = $true
        }
    }
} else {
    Write-Host "Windows build failed" -ForegroundColor Red
    $buildFailed = $true
}
Write-Host ""

Write-Host "================================================" -ForegroundColor Cyan
if ($buildFailed) {
    Write-Host "One or more build steps failed." -ForegroundColor Red
    exit 1
} else {
    Write-Host "Compile complete! Binaries are in the bin/ directory." -ForegroundColor Green
    Get-ChildItem -Path bin -Filter "*launchpad*" | Format-Table Name, Length -AutoSize
    
    Write-Host ""
    Write-Host "Note: If the executable shows the old icon, Windows may be caching it." -ForegroundColor Yellow
    Write-Host "To clear the icon cache, run: ie4uinit.exe -ClearIconCache" -ForegroundColor Cyan
}

# "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
