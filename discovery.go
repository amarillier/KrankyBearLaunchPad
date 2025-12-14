package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// DiscoverApps discovers installed applications based on the platform
func DiscoverApps() ([]App, error) {
	switch runtime.GOOS {
	case "darwin":
		return discoverMacOSApps()
	case "windows":
		return discoverWindowsApps()
	case "linux":
		return discoverLinuxApps()
	default:
		return []App{}, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// discoverMacOSApps discovers applications on macOS
func discoverMacOSApps() ([]App, error) {
	var apps []App
	appIDCounter := 1
	seenApps := make(map[string]bool) // Track by executable path to avoid duplicates

	// Standard application directories
	appDirs := []string{
		"/Applications",
		filepath.Join(os.Getenv("HOME"), "Applications"),
		"/System/Applications",
	}

	for _, appDir := range appDirs {
		// Use WalkDir to search recursively for .app bundles
		filepath.WalkDir(appDir, func(appPath string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // Skip directories we can't read
			}

			// Only process .app directories
			if !d.IsDir() || !strings.HasSuffix(d.Name(), ".app") {
				return nil
			}

			appName := strings.TrimSuffix(d.Name(), ".app")

			// Try to get the actual app name from Info.plist
			infoPlist := filepath.Join(appPath, "Contents", "Info.plist")
			if data, err := os.ReadFile(infoPlist); err == nil {
				// Simple extraction of CFBundleName or CFBundleDisplayName
				content := string(data)
				if name := extractPlistValue(content, "CFBundleDisplayName"); name != "" {
					appName = name
				} else if name := extractPlistValue(content, "CFBundleName"); name != "" {
					appName = name
				}
			}

			// Find executable
			executable := filepath.Join(appPath, "Contents", "MacOS", appName)
			if _, err := os.Stat(executable); err != nil {
				// Try to find any executable in MacOS directory
				macOSDir := filepath.Join(appPath, "Contents", "MacOS")
				if entries, err := os.ReadDir(macOSDir); err == nil && len(entries) > 0 {
					executable = filepath.Join(macOSDir, entries[0].Name())
				} else {
					executable = appPath // Fallback to .app bundle (use open command)
				}
			}

			// Skip if already seen
			if seenApps[executable] {
				return filepath.SkipDir // Skip contents of this .app
			}
			seenApps[executable] = true

			// Find icon
			iconPath := ""
			resourcesDir := filepath.Join(appPath, "Contents", "Resources")
			// Look for .icns file
			if entries, err := os.ReadDir(resourcesDir); err == nil {
				for _, resEntry := range entries {
					if strings.HasSuffix(strings.ToLower(resEntry.Name()), ".icns") {
						iconPath = filepath.Join(resourcesDir, resEntry.Name())
						break
					}
				}
			}

			apps = append(apps, App{
				ID:         fmt.Sprintf("macos_%d", appIDCounter),
				Name:       appName,
				Executable: executable,
				Icon:       iconPath,
				IsCustom:   false,
			})
			appIDCounter++

			// Skip walking into the .app bundle's contents
			return filepath.SkipDir
		})
	}

	// Discover Homebrew applications
	homebrewApps, err := discoverHomebrewApps(&appIDCounter, seenApps)
	if err == nil {
		apps = append(apps, homebrewApps...)
	}

	return apps, nil
}

// discoverWindowsApps discovers applications on Windows
func discoverWindowsApps() ([]App, error) {
	var apps []App
	appIDCounter := 1
	seenApps := make(map[string]bool) // Track by executable path to avoid duplicates

	// Common Windows application locations
	appDirs := []string{
		`C:\Program Files`,
		`C:\Program Files (x86)`,
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs"),
		filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs"),
	}

	// Also check Start Menu
	startMenu := filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs")
	if entries, err := os.ReadDir(startMenu); err == nil {
		for _, entry := range entries {
			entryPath := filepath.Join(startMenu, entry.Name())
			if entry.IsDir() {
				// Recursively search for .lnk files
				filepath.WalkDir(entryPath, func(path string, d os.DirEntry, err error) error {
					if err != nil {
						return nil
					}
					if strings.HasSuffix(strings.ToLower(path), ".lnk") {
						if seenApps[path] {
							return nil
						}
						seenApps[path] = true
						appName := strings.TrimSuffix(filepath.Base(path), ".lnk")
						apps = append(apps, App{
							ID:         fmt.Sprintf("windows_%d", appIDCounter),
							Name:       appName,
							Executable: path,
							IsCustom:   false,
						})
						appIDCounter++
					}
					return nil
				})
			} else if strings.HasSuffix(strings.ToLower(entry.Name()), ".lnk") {
				if seenApps[entryPath] {
					continue
				}
				seenApps[entryPath] = true
				appName := strings.TrimSuffix(entry.Name(), ".lnk")
				apps = append(apps, App{
					ID:         fmt.Sprintf("windows_%d", appIDCounter),
					Name:       appName,
					Executable: entryPath,
					IsCustom:   false,
				})
				appIDCounter++
			}
		}
	}

	// Search Program Files for executables
	for _, appDir := range appDirs[:2] { // Only Program Files directories
		filepath.WalkDir(appDir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if !d.IsDir() && strings.HasSuffix(strings.ToLower(path), ".exe") {
				if seenApps[path] {
					return nil
				}
				// Skip system executables and common non-app executables
				baseName := strings.ToLower(filepath.Base(path))
				if strings.Contains(baseName, "uninstall") ||
					strings.Contains(baseName, "setup") ||
					strings.Contains(baseName, "installer") {
					return nil
				}

				seenApps[path] = true
				appName := strings.TrimSuffix(filepath.Base(path), ".exe")
				apps = append(apps, App{
					ID:         fmt.Sprintf("windows_%d", appIDCounter),
					Name:       appName,
					Executable: path,
					IsCustom:   false,
				})
				appIDCounter++
			}
			return nil
		})
	}

	// Discover package manager applications
	chocoApps, err := discoverChocolateyApps(&appIDCounter, seenApps)
	if err == nil {
		apps = append(apps, chocoApps...)
	}

	wingetApps, err := discoverWingetApps(&appIDCounter, seenApps)
	if err == nil {
		apps = append(apps, wingetApps...)
	}

	scoopApps, err := discoverScoopApps(&appIDCounter, seenApps)
	if err == nil {
		apps = append(apps, scoopApps...)
	}

	return apps, nil
}

// discoverLinuxApps discovers applications on Linux using .desktop files
func discoverLinuxApps() ([]App, error) {
	var apps []App
	appIDCounter := 1
	seenApps := make(map[string]bool) // Track by executable path

	// Standard .desktop file locations (covers APT, RPM, etc.)
	desktopDirs := []string{
		"/usr/share/applications",
		filepath.Join(os.Getenv("HOME"), ".local/share/applications"),
		"/usr/local/share/applications",
		"/var/lib/snapd/desktop/applications", // Snap applications
	}

	for _, desktopDir := range desktopDirs {
		entries, err := os.ReadDir(desktopDir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".desktop") {
				continue
			}

			desktopPath := filepath.Join(desktopDir, entry.Name())
			desktopFile, err := os.ReadFile(desktopPath)
			if err != nil {
				continue
			}

			app := parseDesktopFile(string(desktopFile), desktopPath)
			if app.Name == "" || app.Executable == "" {
				continue
			}

			// Skip if NoDisplay=true or Hidden=true
			if strings.Contains(string(desktopFile), "NoDisplay=true") ||
				strings.Contains(string(desktopFile), "Hidden=true") {
				continue
			}

			// Track by executable path to avoid duplicates
			if seenApps[app.Executable] {
				continue
			}
			seenApps[app.Executable] = true

			app.ID = fmt.Sprintf("linux_%d", appIDCounter)
			app.IsCustom = false
			apps = append(apps, app)
			appIDCounter++
		}
	}

	// Discover Snap applications (they may have their own .desktop files)
	snapApps, err := discoverSnapApps(&appIDCounter, seenApps)
	if err == nil {
		apps = append(apps, snapApps...)
	}

	return apps, nil
}

// parseDesktopFile parses a .desktop file and extracts app information
func parseDesktopFile(content, filePath string) App {
	app := App{}
	lines := strings.Split(content, "\n")
	var currentSection string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentSection = line
			continue
		}

		if currentSection != "[Desktop Entry]" {
			continue
		}

		if strings.HasPrefix(line, "Name=") {
			app.Name = strings.TrimPrefix(line, "Name=")
		} else if strings.HasPrefix(line, "Exec=") {
			execLine := strings.TrimPrefix(line, "Exec=")
			// Remove %u, %f, %F, %U, etc. parameters
			execLine = strings.Fields(execLine)[0]
			app.Executable = execLine
		} else if strings.HasPrefix(line, "Comment=") {
			app.Description = strings.TrimPrefix(line, "Comment=")
		} else if strings.HasPrefix(line, "Categories=") {
			categories := strings.TrimPrefix(line, "Categories=")
			cats := strings.Split(categories, ";")
			if len(cats) > 0 {
				app.Category = cats[0]
			}
		} else if strings.HasPrefix(line, "Icon=") {
			app.Icon = strings.TrimPrefix(line, "Icon=")
		}
	}

	return app
}

// extractPlistValue extracts a value from a plist file (simple string extraction)
func extractPlistValue(content, key string) string {
	keyPattern := fmt.Sprintf("<key>%s</key>", key)
	idx := strings.Index(content, keyPattern)
	if idx == -1 {
		return ""
	}

	// Find the next <string> tag after the key
	stringStart := strings.Index(content[idx:], "<string>")
	if stringStart == -1 {
		return ""
	}

	start := idx + stringStart + len("<string>")
	stringEnd := strings.Index(content[start:], "</string>")
	if stringEnd == -1 {
		return ""
	}

	return content[start : start+stringEnd]
}

// discoverHomebrewApps discovers applications installed via Homebrew
func discoverHomebrewApps(appIDCounter *int, seenApps map[string]bool) ([]App, error) {
	var apps []App

	// Check if Homebrew is installed
	cmd := exec.Command("which", "brew")
	if err := cmd.Run(); err != nil {
		return apps, nil // Homebrew not installed
	}

	// Get Homebrew prefix
	cmd = exec.Command("brew", "--prefix")
	prefixOutput, err := cmd.Output()
	if err != nil {
		return apps, nil
	}
	brewPrefix := strings.TrimSpace(string(prefixOutput))

	// Check for casks (GUI applications) - they install to /Applications
	caskDir := filepath.Join(brewPrefix, "Caskroom")
	if entries, err := os.ReadDir(caskDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				caskName := entry.Name()
				// Look for .app bundles in the cask directory
				caskPath := filepath.Join(caskDir, caskName)
				filepath.WalkDir(caskPath, func(path string, d os.DirEntry, err error) error {
					if err != nil {
						return nil
					}
					if d.IsDir() && strings.HasSuffix(path, ".app") {
						// Found a .app bundle
						appPath := path
						appName := strings.TrimSuffix(filepath.Base(path), ".app")
						executable := appPath // Use .app bundle path

						if seenApps[executable] {
							return nil
						}
						seenApps[executable] = true

						apps = append(apps, App{
							ID:         fmt.Sprintf("homebrew_%d", *appIDCounter),
							Name:       appName,
							Executable: executable,
							Category:   "Homebrew (Cask)",
							IsCustom:   false,
						})
						*appIDCounter++
					}
					return nil
				})
			}
		}
	}

	// Also check /opt/homebrew/Caskroom (Apple Silicon) and /usr/local/Caskroom (Intel)
	altCaskDirs := []string{
		"/opt/homebrew/Caskroom",
		"/usr/local/Caskroom",
	}
	for _, caskDir := range altCaskDirs {
		if entries, err := os.ReadDir(caskDir); err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					caskName := entry.Name()
					caskPath := filepath.Join(caskDir, caskName)
					filepath.WalkDir(caskPath, func(path string, d os.DirEntry, err error) error {
						if err != nil {
							return nil
						}
						if d.IsDir() && strings.HasSuffix(path, ".app") {
							appPath := path
							appName := strings.TrimSuffix(filepath.Base(path), ".app")
							executable := appPath

							if seenApps[executable] {
								return nil
							}
							seenApps[executable] = true

							apps = append(apps, App{
								ID:         fmt.Sprintf("homebrew_%d", *appIDCounter),
								Name:       appName,
								Executable: executable,
								Category:   "Homebrew (Cask)",
								IsCustom:   false,
							})
							*appIDCounter++
						}
						return nil
					})
				}
			}
		}
	}

	return apps, nil
}

// discoverChocolateyApps discovers applications installed via Chocolatey
func discoverChocolateyApps(appIDCounter *int, seenApps map[string]bool) ([]App, error) {
	var apps []App

	// Check if Chocolatey is installed
	cmd := exec.Command("choco", "--version")
	if err := cmd.Run(); err != nil {
		return apps, nil // Chocolatey not installed
	}

	// Get Chocolatey install location
	chocoLib := os.Getenv("ChocolateyInstall")
	if chocoLib == "" {
		chocoLib = `C:\ProgramData\chocolatey`
	}

	// List installed packages and find their executables
	cmd = exec.Command("choco", "list", "--local-only", "--limit-output")
	output, err := cmd.Output()
	if err != nil {
		return apps, nil
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Format: package-name|version
		parts := strings.Split(line, "|")
		if len(parts) < 1 {
			continue
		}
		packageName := parts[0]

		// Try to find executable in common locations
		// Chocolatey packages typically install to Program Files or their own lib directory
		executable := findWindowsExecutable(packageName, chocoLib)
		if executable == "" {
			continue // Skip if no executable found
		}

		if seenApps[executable] {
			continue
		}
		seenApps[executable] = true

		appName := packageName
		// Try to get a better name from Start Menu shortcut
		if shortcut := findStartMenuShortcut(packageName); shortcut != "" {
			executable = shortcut
			appName = strings.TrimSuffix(filepath.Base(shortcut), ".lnk")
		}

		apps = append(apps, App{
			ID:         fmt.Sprintf("choco_%d", *appIDCounter),
			Name:       appName,
			Executable: executable,
			Category:   "Chocolatey",
			IsCustom:   false,
		})
		*appIDCounter++
	}

	return apps, nil
}

// discoverWingetApps discovers applications installed via Winget
func discoverWingetApps(appIDCounter *int, seenApps map[string]bool) ([]App, error) {
	var apps []App

	// Check if winget is available
	cmd := exec.Command("winget", "--version")
	if err := cmd.Run(); err != nil {
		return apps, nil // winget not available
	}

	// List installed packages
	cmd = exec.Command("winget", "list", "--accept-source-agreements", "--accept-package-agreements")
	output, err := cmd.Output()
	if err != nil {
		return apps, nil
	}

	lines := strings.Split(string(output), "\n")
	for i, line := range lines {
		if i < 2 {
			continue // Skip header lines
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Format: Name      Id                          Version      Available    Source
		fields := strings.Fields(line)
		if len(fields) < 1 {
			continue
		}

		packageName := fields[0]
		// Try to find executable
		executable := findWindowsExecutable(packageName, "")
		if executable == "" {
			// Try Start Menu shortcut
			executable = findStartMenuShortcut(packageName)
		}
		if executable == "" {
			continue
		}

		if seenApps[executable] {
			continue
		}
		seenApps[executable] = true

		appName := packageName
		if strings.HasSuffix(strings.ToLower(executable), ".lnk") {
			appName = strings.TrimSuffix(filepath.Base(executable), ".lnk")
		}

		apps = append(apps, App{
			ID:         fmt.Sprintf("winget_%d", *appIDCounter),
			Name:       appName,
			Executable: executable,
			Category:   "Winget",
			IsCustom:   false,
		})
		*appIDCounter++
	}

	return apps, nil
}

// discoverScoopApps discovers applications installed via Scoop
func discoverScoopApps(appIDCounter *int, seenApps map[string]bool) ([]App, error) {
	var apps []App

	// Check if scoop is available
	cmd := exec.Command("scoop", "--version")
	if err := cmd.Run(); err != nil {
		return apps, nil // scoop not available
	}

	// Get Scoop install location
	scoopRoot := os.Getenv("SCOOP")
	if scoopRoot == "" {
		scoopRoot = filepath.Join(os.Getenv("USERPROFILE"), "scoop")
	}

	// List installed packages
	cmd = exec.Command("scoop", "list")
	output, err := cmd.Output()
	if err != nil {
		return apps, nil
	}

	lines := strings.Split(string(output), "\n")
	for i, line := range lines {
		if i == 0 {
			continue // Skip header line
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Format: app-name version
		fields := strings.Fields(line)
		if len(fields) < 1 {
			continue
		}

		packageName := fields[0]
		appDir := filepath.Join(scoopRoot, "apps", packageName)

		// Look for executable in current or versioned directory
		executable := findWindowsExecutable(packageName, appDir)
		if executable == "" {
			continue
		}

		if seenApps[executable] {
			continue
		}
		seenApps[executable] = true

		apps = append(apps, App{
			ID:         fmt.Sprintf("scoop_%d", *appIDCounter),
			Name:       packageName,
			Executable: executable,
			Category:   "Scoop",
			IsCustom:   false,
		})
		*appIDCounter++
	}

	return apps, nil
}

// discoverSnapApps discovers applications installed via Snap
func discoverSnapApps(appIDCounter *int, seenApps map[string]bool) ([]App, error) {
	var apps []App

	// Check if snap is available
	cmd := exec.Command("which", "snap")
	if err := cmd.Run(); err != nil {
		return apps, nil // snap not available
	}

	// List installed snaps
	cmd = exec.Command("snap", "list")
	output, err := cmd.Output()
	if err != nil {
		return apps, nil
	}

	lines := strings.Split(string(output), "\n")
	for i, line := range lines {
		if i == 0 {
			continue // Skip header
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Format: name version rev tracking publisher notes
		fields := strings.Fields(line)
		if len(fields) < 1 {
			continue
		}

		snapName := fields[0]

		// Look for .desktop file in snap directory
		desktopPath := filepath.Join("/var/lib/snapd/desktop/applications", snapName+"_*.desktop")
		// Use glob to find desktop files
		matches, err := filepath.Glob(desktopPath)
		if err == nil && len(matches) > 0 {
			// Parse the desktop file
			for _, desktopFile := range matches {
				if data, err := os.ReadFile(desktopFile); err == nil {
					app := parseDesktopFile(string(data), desktopFile)
					if app.Name != "" && app.Executable != "" {
						if seenApps[app.Executable] {
							continue
						}
						seenApps[app.Executable] = true

						app.ID = fmt.Sprintf("snap_%d", *appIDCounter)
						app.Category = "Snap"
						app.IsCustom = false
						apps = append(apps, app)
						*appIDCounter++
					}
				}
			}
		} else {
			// Fallback: try to find executable directly
			executable := filepath.Join("/snap", snapName, "current", "bin", snapName)
			if _, err := os.Stat(executable); err == nil {
				if seenApps[executable] {
					continue
				}
				seenApps[executable] = true

				apps = append(apps, App{
					ID:         fmt.Sprintf("snap_%d", *appIDCounter),
					Name:       snapName,
					Executable: executable,
					Category:   "Snap",
					IsCustom:   false,
				})
				*appIDCounter++
			}
		}
	}

	return apps, nil
}

// Helper functions for Windows

func findWindowsExecutable(packageName, searchDir string) string {
	// Common executable names to search for
	execNames := []string{
		packageName + ".exe",
		strings.Title(packageName) + ".exe",
	}

	searchDirs := []string{
		`C:\Program Files`,
		`C:\Program Files (x86)`,
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs"),
	}

	if searchDir != "" {
		searchDirs = append([]string{searchDir}, searchDirs...)
	}

	var foundPath string
	for _, dir := range searchDirs {
		for _, execName := range execNames {
			// Search recursively
			filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				if !d.IsDir() && strings.EqualFold(filepath.Base(path), execName) {
					// Found it
					foundPath = path
					return filepath.SkipAll // Stop searching
				}
				return nil
			})
			if foundPath != "" {
				return foundPath
			}
		}
	}

	return ""
}

func findStartMenuShortcut(packageName string) string {
	startMenu := filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs")
	searchPattern := filepath.Join(startMenu, "**", "*"+packageName+"*.lnk")

	matches, err := filepath.Glob(searchPattern)
	if err == nil && len(matches) > 0 {
		return matches[0]
	}

	return ""
}

// LaunchApp launches an application
func LaunchApp(app App) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		// On macOS, if it's a .app bundle, use open command
		if strings.HasSuffix(app.Executable, ".app") {
			cmd = exec.Command("open", app.Executable)
		} else {
			cmd = exec.Command(app.Executable)
		}
	case "windows":
		// On Windows, use cmd /c start for .lnk files, or direct execution
		if strings.HasSuffix(strings.ToLower(app.Executable), ".lnk") {
			cmd = exec.Command("cmd", "/c", "start", "", app.Executable)
		} else {
			cmd = exec.Command(app.Executable)
		}
	case "linux":
		// On Linux, execute directly
		cmd = exec.Command(app.Executable)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	return cmd.Start()
}


