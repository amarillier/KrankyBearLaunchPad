package main

import (
	"encoding/json"
	"fmt"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	// appName    = "KrankyBear LaunchPad"
	appVersion = "0.1.0" // see FyneApp.toml
	appAuthor  = "Allan Marillier"
)

var appName = "KrankyBear LaunchPad"
var appCopyright = "Copyright (c) Allan Marillier, 2025-" + strconv.Itoa(time.Now().Year())

var (
	myApp          fyne.App
	mainWindow     fyne.Window
	config         *Config
	configPath     string
	discoveredApps []App
	tabContainer   *container.AppTabs
	appGrids       map[string]fyne.CanvasObject // Maps tab ID to app grid (scroll container)
	currentTabID   string
	childWindows   []fyne.Window          // Track child windows for auto-close
	toolbar        *fyne.Container        // Reference to toolbar for easy updates
	menuBar        *fyne.Container        // Reference to menuBar for easy updates
	openDialogs    map[string]fyne.Window // Track open dialogs by title to prevent duplicates
	updateWindow   fyne.Window            // Update check window
)

func main() {
	myApp = app.NewWithID("com.github.amarillier.KrankyBearLaunchPad")

	// Load saved theme preference
	savedTheme := myApp.Preferences().StringWithFallback("theme", "default")
	if savedTheme == "light" {
		myApp.Settings().SetTheme(&appTheme{Theme: theme.LightTheme()})
	} else if savedTheme == "dark" {
		myApp.Settings().SetTheme(&appTheme{Theme: theme.DarkTheme()})
	} else {
		myApp.Settings().SetTheme(&appTheme{Theme: theme.DefaultTheme()})
	}

	configPath = GetConfigPath()
	var err error
	config, err = LoadConfig(configPath)
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		config = &Config{
			Tabs: []Tab{{ID: "home", Name: "Home", AppIDs: []string{}}},
			Apps: []App{},
		}
	}

	// Discover installed applications
	discoveredApps, err = DiscoverApps()
	if err != nil {
		fmt.Printf("Error discovering apps: %v\n", err)
		discoveredApps = []App{}
	}

	// Merge discovered apps with saved apps (avoid duplicates)
	mergeDiscoveredApps()

	// Auto-add all discovered apps to Home tab if not already in any tab
	autoAddAppsToHomeTab()

	mainWindow = myApp.NewWindow("KrankyBear LaunchPad")
	mainWindow.Resize(fyne.NewSize(1000, 700))
	mainWindow.CenterOnScreen()
	// Ensure window is resizable (default, but make it explicit)
	mainWindow.SetFixedSize(false)

	// Close all child windows when main window closes
	mainWindow.SetCloseIntercept(func() {
		closeAllChildWindows()
		// Clean up system tray if it exists
		if desk, ok := myApp.(desktop.App); ok {
			desk.SetSystemTrayMenu(nil)
		}
		mainWindow.Close()
		myApp.Quit()
	})

	appGrids = make(map[string]fyne.CanvasObject)
	currentTabID = "home"
	childWindows = []fyne.Window{}
	openDialogs = make(map[string]fyne.Window)

	setupUI()
	mainWindow.ShowAndRun()
}

func setupUI() {
	// Create menu bar
	menuBar = createMenuBar()

	// Create tab container with initial tabs
	tabItems := []*container.TabItem{}
	appGrids = make(map[string]fyne.CanvasObject)

	// Create tabs from config
	for _, tab := range config.Tabs {
		grid := createAppGrid(tab.ID)
		appGrids[tab.ID] = grid
		tabItems = append(tabItems, container.NewTabItem(tab.Name, grid))
	}

	tabContainer = container.NewAppTabs(tabItems...)

	// Set current tab
	tabContainer.OnSelected = func(tab *container.TabItem) {
		// Find tab ID by name
		for _, t := range config.Tabs {
			if t.Name == tab.Text {
				currentTabID = t.ID
				break
			}
		}
	}

	// Add button to create new tab
	addTabBtn := widget.NewButton("+ New Tab", func() {
		showCreateTabDialog()
	})

	deleteTabBtn := widget.NewButton("Delete Tab", func() {
		showDeleteTabDialog()
	})

	sortTabsBtn := widget.NewButton("Sort Tabs", func() {
		showSortTabsDialog()
	})

	// Top toolbar - Manage Apps first, then Delete Tab, Edit Tab, Sort Tabs alphabetically
	toolbar = container.NewHBox(
		widget.NewLabel("KrankyBear LaunchPad"),
		widget.NewSeparator(),
		widget.NewButton("Manage Apps", showManageAppsDialog),
		addTabBtn,
		deleteTabBtn,
		widget.NewButton("Edit Tab", showEditTabDialog),
		sortTabsBtn,
	)

	content := container.NewBorder(
		toolbar,
		nil,
		nil,
		nil,
		tabContainer,
	)

	mainWindow.SetContent(container.NewBorder(
		menuBar,
		nil,
		nil,
		nil,
		content,
	))
}

func createMenuBar() *fyne.Container {
	// Create menu items that will be reused in system tray
	menuItems := createMenuItems()

	operationsMenu := fyne.NewMenu("Operations",
		menuItems["Show"],
		menuItems["Hide"],
		fyne.NewMenuItemSeparator(),
		menuItems["Manage Apps"],
		menuItems["Refresh Apps"],
		fyne.NewMenuItemSeparator(),
		menuItems["New Tab"],
		menuItems["Delete Tab"],
		menuItems["Edit Tab"],
		menuItems["Sort Tabs"],
		fyne.NewMenuItemSeparator(),
		menuItems["Quit"],
	)

	settingsMenu := fyne.NewMenu("Settings",
		menuItems["Theme Settings"],
	)

	helpMenu := fyne.NewMenu("Help",
		menuItems["Check for Update"],
		fyne.NewMenuItemSeparator(),
		menuItems["About"],
	)

	mainMenu := fyne.NewMainMenu(operationsMenu, settingsMenu, helpMenu)
	mainWindow.SetMainMenu(mainMenu)

	// Set up system tray menu if desktop driver supports it
	setupSystemTrayMenu(menuItems)

	return container.NewVBox() // Placeholder, menu is set via SetMainMenu
}

// createMenuItems creates reusable menu items for both main menu and system tray
func createMenuItems() map[string]*fyne.MenuItem {
	return map[string]*fyne.MenuItem{
		"Show": fyne.NewMenuItem("Show", func() {
			if mainWindow != nil {
				mainWindow.Show()
				mainWindow.RequestFocus()
			}
		}),
		"Hide": fyne.NewMenuItem("Hide", func() {
			if mainWindow != nil {
				mainWindow.Hide()
			}
		}),
		"Manage Apps": fyne.NewMenuItem("Manage Apps", func() {
			showManageAppsDialog()
		}),
		"Refresh Apps": fyne.NewMenuItem("Refresh Apps", func() {
			refreshApps()
		}),
		"New Tab": fyne.NewMenuItem("New Tab", func() {
			showCreateTabDialog()
		}),
		"Delete Tab": fyne.NewMenuItem("Delete Tab", func() {
			showDeleteTabDialog()
		}),
		"Edit Tab": fyne.NewMenuItem("Edit Tab", func() {
			showEditTabDialog()
		}),
		"Sort Tabs": fyne.NewMenuItem("Sort Tabs", func() {
			showSortTabsDialog()
		}),
		"Check for Update": fyne.NewMenuItem("Check for Update", func() {
			checkForUpdate()
		}),
		"About": fyne.NewMenuItem("About", func() {
			showAboutDialog()
		}),
		"Theme Settings": fyne.NewMenuItem("Theme Settings", func() {
			showThemeSettingsDialog()
		}),
		"Quit": fyne.NewMenuItem("Quit", func() {
			// Clean up system tray
			if desk, ok := myApp.(desktop.App); ok {
				desk.SetSystemTrayMenu(nil)
			}
			closeAllChildWindows()
			myApp.Quit()
		}),
	}
}

// setupSystemTrayMenu sets up the system tray menu if supported
func setupSystemTrayMenu(menuItems map[string]*fyne.MenuItem) {
	if desk, ok := myApp.(desktop.App); ok {
		trayMenu := fyne.NewMenu("KrankyBear LaunchPad",
			menuItems["Show"],
			menuItems["Hide"],
			fyne.NewMenuItemSeparator(),
			menuItems["Manage Apps"],
			menuItems["Refresh Apps"],
			fyne.NewMenuItemSeparator(),
			menuItems["New Tab"],
			menuItems["Delete Tab"],
			menuItems["Edit Tab"],
			menuItems["Sort Tabs"],
			fyne.NewMenuItemSeparator(),
			menuItems["Theme Settings"],
			fyne.NewMenuItemSeparator(),
			menuItems["Check for Update"],
			menuItems["About"],
			fyne.NewMenuItemSeparator(),
			menuItems["Quit"],
		)
		desk.SetSystemTrayMenu(trayMenu)
		desk.SetSystemTrayIcon(resourceKrankyBearTrapperRedPlaidPng)
	}
}

// GitHubRelease represents a GitHub release
type GitHubRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	Body    string `json:"body"`
	URL     string `json:"html_url"`
}

// checkForUpdate checks for updates from GitHub releases
func checkForUpdate() {
	// Check if update window is already open
	if updateWindow != nil {
		updateWindow.RequestFocus()
		return
	}

	// Show checking dialog
	checkingDialog := dialog.NewInformation("Checking for Updates", "Checking for updates...", mainWindow)
	checkingDialog.Show()

	// Run update check in goroutine to avoid blocking UI
	go func() {
		defer checkingDialog.Hide()

		// GitHub API URL for releases (using public API, no auth needed)
		apiURL := "https://api.github.com/repos/amarillier/KrankyBearLaunchPad/releases/latest"

		client := &http.Client{
			Timeout: 10 * time.Second,
		}

		resp, err := client.Get(apiURL)
		if err != nil {
			dialog.ShowError(fmt.Errorf("Failed to check for updates: %v", err), mainWindow)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			dialog.ShowError(fmt.Errorf("Failed to check for updates: HTTP %d", resp.StatusCode), mainWindow)
			return
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			dialog.ShowError(fmt.Errorf("Failed to read update information: %v", err), mainWindow)
			return
		}

		var release GitHubRelease
		if err := json.Unmarshal(body, &release); err != nil {
			dialog.ShowError(fmt.Errorf("Failed to parse update information: %v", err), mainWindow)
			return
		}

		// Compare versions (remove 'v' prefix if present)
		latestVersion := strings.TrimPrefix(release.TagName, "v")
		currentVersion := strings.TrimPrefix(appVersion, "v")

		// Format message similar to KrankyBearClock
		var message string
		if latestVersion != currentVersion {
			message = fmt.Sprintf("A newer version is available!\n\nCurrent version: %s\nLatest version: %s\n\n%s",
				currentVersion, latestVersion, release.Body)
		} else {
			message = fmt.Sprintf("You are running the latest version.\n\nCurrent version: %s\nLatest version: %s",
				currentVersion, latestVersion)
		}

		// Show update alert window
		showUpdateAlert(message, release.URL, latestVersion != currentVersion)
	}()
}

// showUpdateAlert displays the update check results in a persistent window
func showUpdateAlert(updtmsg string, releaseURL string, updateAvailable bool) {
	// Parse release link
	releaselink, rerr := url.Parse(releaseURL)
	if rerr != nil {
		fyne.LogError("Could not parse URL", rerr)
		releaselink, _ = url.Parse("https://github.com/amarillier/KrankyBearLaunchPad/releases/latest")
	}
	myreleaselink := widget.NewHyperlink(releaseURL, releaselink)
	myreleaselink.Alignment = fyne.TextAlignLeading

	// Create content
	text := widget.NewLabel(updtmsg)
	text.Wrapping = fyne.TextWrapWord

	openBtn := widget.NewButton("Open Release Page", func() {
		// Open browser to release page
		cmd := exec.Command("open", releaseURL) // macOS
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd", "/c", "start", releaseURL)
		} else if runtime.GOOS == "linux" {
			cmd = exec.Command("xdg-open", releaseURL)
		}
		cmd.Run()
	})

	content := container.NewVBox(
		text,
		myreleaselink,
		openBtn,
	)

	// Create or update window
	if updateWindow == nil {
		updateWindow = myApp.NewWindow(appName + ": Update Check")
		updateWindow.SetIcon(theme.FolderIcon())
		updateWindow.Resize(fyne.NewSize(500, 300))
		updateWindow.SetCloseIntercept(func() {
			updateWindow.Close()
			updateWindow = nil
		})
		registerChildWindow(updateWindow)
	}

	updateWindow.SetContent(content)
	updateWindow.Show()
	updateWindow.RequestFocus()
	centerDialogOnMainWindow(updateWindow)
}

func createAppGrid(tabID string) fyne.CanvasObject {
	grid := container.NewGridWithColumns(6) // 6 columns

	// Get apps for this tab
	tab := getTabByID(tabID)
	if tab != nil {
		// Collect apps and sort them alphabetically by name
		apps := []App{}
		for _, appID := range tab.AppIDs {
			app := getAppByID(appID)
			if app != nil {
				apps = append(apps, *app)
			}
		}

		// Sort apps alphabetically by name (case-insensitive)
		sort.Slice(apps, func(i, j int) bool {
			return strings.ToLower(apps[i].Name) < strings.ToLower(apps[j].Name)
		})

		// Add sorted apps to grid
		for _, app := range apps {
			card := createAppCard(app, tabID)
			grid.Add(card)
		}
	}

	// Wrap grid in scroll container so all apps are visible
	scrollContainer := container.NewScroll(grid)
	return scrollContainer
}

// getGridFromContainer extracts the actual grid from a scroll container
func getGridFromContainer(scrollContainer fyne.CanvasObject) *fyne.Container {
	// If container is a scroll container, get its content
	if scroll, ok := scrollContainer.(*container.Scroll); ok {
		if content := scroll.Content; content != nil {
			if grid, ok := content.(*fyne.Container); ok {
				return grid
			}
		}
	}
	// If it's already a grid, return it
	if grid, ok := scrollContainer.(*fyne.Container); ok {
		return grid
	}
	return nil
}

// refreshGridWithSortedApps refreshes a grid with apps sorted alphabetically
func refreshGridWithSortedApps(tabID string) {
	if scrollContainer, ok := appGrids[tabID]; ok {
		grid := getGridFromContainer(scrollContainer)
		if grid != nil {
			grid.RemoveAll()
			tab := getTabByID(tabID)
			if tab != nil {
				// Collect apps and sort them alphabetically by name
				apps := []App{}
				for _, appID := range tab.AppIDs {
					app := getAppByID(appID)
					if app != nil {
						apps = append(apps, *app)
					}
				}

				// Sort apps alphabetically by name (case-insensitive)
				sort.Slice(apps, func(i, j int) bool {
					return strings.ToLower(apps[i].Name) < strings.ToLower(apps[j].Name)
				})

				// Add sorted apps to grid
				for _, app := range apps {
					card := createAppCard(app, tabID)
					grid.Add(card)
				}
			}
			grid.Refresh()
			scrollContainer.Refresh()
		}
	}
}

// AppCardWidget is a custom widget that supports right-click context menu
type AppCardWidget struct {
	widget.BaseWidget
	app   App
	tabID string
	icon  *canvas.Image
	label *widget.Label
}

// NewAppCardWidget creates a new app card widget
func NewAppCardWidget(app App, tabID string) *AppCardWidget {
	card := &AppCardWidget{
		app:   app,
		tabID: tabID,
	}
	card.ExtendBaseWidget(card)
	return card
}

// CreateRenderer creates the renderer for the widget
func (a *AppCardWidget) CreateRenderer() fyne.WidgetRenderer {
	// Load and resize icon to 64x64
	icon := a.loadAppIcon()

	// Create label
	label := widget.NewLabel(a.app.Name)
	label.Wrapping = fyne.TextWrapWord
	label.Alignment = fyne.TextAlignCenter

	// Create card content with icon and label
	content := container.NewVBox(
		container.NewCenter(icon),
		label,
	)

	a.icon = icon
	a.label = label

	return &appCardRenderer{
		widget:  a,
		content: content,
		objects: []fyne.CanvasObject{content},
	}
}

// Tapped handles single click (launch app)
func (a *AppCardWidget) Tapped(*fyne.PointEvent) {
	if err := LaunchApp(a.app); err != nil {
		dialog.ShowError(fmt.Errorf("Failed to launch %s: %v", a.app.Name, err), mainWindow)
	}
}

// TappedSecondary handles right-click (show context menu)
func (a *AppCardWidget) TappedSecondary(pe *fyne.PointEvent) {
	if mainWindow == nil {
		return
	}

	menu := fyne.NewMenu(a.app.Name,
		fyne.NewMenuItem("Launch", func() {
			if err := LaunchApp(a.app); err != nil {
				dialog.ShowError(fmt.Errorf("Failed to launch %s: %v", a.app.Name, err), mainWindow)
			}
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Copy to Tab", func() {
			showCopyToTabDialog(a.app, a.tabID)
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Edit Properties", func() {
			showEditAppPropertiesDialog(a.app, a.tabID)
		}),
		fyne.NewMenuItem("Change Icon", func() {
			showChangeIconDialog(a.app, a.tabID)
		}),
		fyne.NewMenuItem("Remove from Tab", func() {
			removeAppFromTab(a.app.ID, a.tabID)
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("View Details", func() {
			showAppDetailsDialog(a.app)
		}),
	)

	canvas := mainWindow.Canvas()
	if canvas == nil {
		return
	}

	// Get position safely
	driver := fyne.CurrentApp().Driver()
	if driver == nil {
		return
	}

	pos := driver.AbsolutePositionForObject(a)
	popupPos := pos.Add(pe.Position)
	widget.ShowPopUpMenuAtPosition(menu, canvas, popupPos)
}

// MouseIn is required for desktop mouse events
func (a *AppCardWidget) MouseIn(*desktop.MouseEvent)    {}
func (a *AppCardWidget) MouseOut()                      {}
func (a *AppCardWidget) MouseMoved(*desktop.MouseEvent) {}

// loadAppIcon loads and resizes the app icon to 64x64
func (a *AppCardWidget) loadAppIcon() *canvas.Image {
	var iconResource fyne.Resource

	// Try to load icon from saved path first
	if a.app.Icon != "" && fileExists(a.app.Icon) {
		// Skip .icns files - Fyne can't load them directly
		if !strings.HasSuffix(strings.ToLower(a.app.Icon), ".icns") {
			resource, err := fyne.LoadResourceFromPath(a.app.Icon)
			if err == nil {
				iconResource = resource
			}
		}
	}

	// If no icon found, try to detect from executable path
	if iconResource == nil {
		iconResource = detectIconFromExecutable(a.app.Executable)
		// If we detected an icon, save the path to config (but only if it's not .icns)
		if iconResource != nil && a.app.Icon == "" {
			// Find the icon path that was detected
			if detectedIconPath := findIconPathFromExecutable(a.app.Executable); detectedIconPath != "" {
				// Only save if it's a format Fyne can load (skip .icns)
				if !strings.HasSuffix(strings.ToLower(detectedIconPath), ".icns") {
					// Update app icon in config and refresh widget
					for i := range config.Apps {
						if config.Apps[i].ID == a.app.ID {
							config.Apps[i].Icon = detectedIconPath
							a.app.Icon = detectedIconPath // Update local copy
							// Save config (async to avoid blocking UI)
							go func() {
								SaveConfig(config, configPath)
							}()
							// Reload icon now that we have the path
							if resource, err := fyne.LoadResourceFromPath(detectedIconPath); err == nil {
								iconResource = resource
							}
							break
						}
					}
				}
			}
		}
	}

	// Fallback to default icon
	if iconResource == nil {
		iconResource = theme.FolderIcon()
	}

	// Create image and resize to 64x64
	img := canvas.NewImageFromResource(iconResource)
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(fyne.NewSize(64, 64))
	img.Resize(fyne.NewSize(64, 64))

	return img
}

// findIconPathFromExecutable finds the icon file path (not resource) for saving to config
// Prioritizes formats that Fyne can load (PNG, JPG, etc.) over .icns
func findIconPathFromExecutable(executable string) string {
	// For macOS .app bundles, look for icon in Resources
	if strings.HasSuffix(executable, ".app") || strings.Contains(executable, ".app/Contents") {
		appPath := executable
		if !strings.HasSuffix(appPath, ".app") {
			parts := strings.Split(appPath, ".app")
			if len(parts) > 0 {
				appPath = parts[0] + ".app"
			}
		}

		resourcesDir := filepath.Join(appPath, "Contents", "Resources")
		if entries, err := os.ReadDir(resourcesDir); err == nil {
			// First pass: prioritize PNG and other standard formats
			for _, entry := range entries {
				name := strings.ToLower(entry.Name())
				if strings.HasSuffix(name, ".png") ||
					strings.HasSuffix(name, ".jpg") ||
					strings.HasSuffix(name, ".jpeg") ||
					strings.HasSuffix(name, ".bmp") ||
					strings.HasSuffix(name, ".ico") {
					return filepath.Join(resourcesDir, entry.Name())
				}
			}
			// Second pass: fall back to .icns if nothing else found
			// Note: .icns files may not work with Fyne, but we'll save the path anyway
			for _, entry := range entries {
				name := strings.ToLower(entry.Name())
				if strings.HasSuffix(name, ".icns") {
					return filepath.Join(resourcesDir, entry.Name())
				}
			}
		}
	}

	// For Windows, try icon/image files in same directory
	if strings.HasSuffix(strings.ToLower(executable), ".exe") {
		dir := filepath.Dir(executable)
		if entries, err := os.ReadDir(dir); err == nil {
			exeName := strings.TrimSuffix(filepath.Base(executable), ".exe")
			for _, entry := range entries {
				name := strings.ToLower(entry.Name())
				if (strings.HasSuffix(name, ".ico") ||
					strings.HasSuffix(name, ".png") ||
					strings.HasSuffix(name, ".jpg") ||
					strings.HasSuffix(name, ".jpeg") ||
					strings.HasSuffix(name, ".bmp")) &&
					strings.Contains(name, strings.ToLower(exeName)) {
					return filepath.Join(dir, entry.Name())
				}
			}
		}
	}

	// For Linux, try common image formats in same directory
	dir := filepath.Dir(executable)
	if entries, err := os.ReadDir(dir); err == nil {
		exeName := filepath.Base(executable)
		for _, entry := range entries {
			name := strings.ToLower(entry.Name())
			if strings.HasSuffix(name, ".png") ||
				strings.HasSuffix(name, ".jpg") ||
				strings.HasSuffix(name, ".jpeg") ||
				strings.HasSuffix(name, ".bmp") ||
				strings.HasSuffix(name, ".ico") ||
				strings.HasSuffix(name, ".svg") {
				baseName := strings.TrimSuffix(strings.ToLower(exeName), filepath.Ext(exeName))
				iconBaseName := strings.TrimSuffix(strings.ToLower(entry.Name()), filepath.Ext(entry.Name()))
				if strings.Contains(iconBaseName, baseName) || strings.Contains(baseName, iconBaseName) {
					return filepath.Join(dir, entry.Name())
				}
			}
		}
	}

	return ""
}

// loadIconResource loads the icon resource (used for button icon)
func (a *AppCardWidget) loadIconResource() fyne.Resource {
	var iconResource fyne.Resource

	// Try to load icon from various sources
	if a.app.Icon != "" && fileExists(a.app.Icon) {
		resource, err := fyne.LoadResourceFromPath(a.app.Icon)
		if err == nil {
			iconResource = resource
		}
	}

	// If no icon found, try to detect from executable path
	if iconResource == nil {
		iconResource = detectIconFromExecutable(a.app.Executable)
	}

	// Fallback to default icon
	if iconResource == nil {
		iconResource = theme.FolderIcon()
	}

	return iconResource
}

// detectIconFromExecutable tries to find an icon based on the executable path
func detectIconFromExecutable(executable string) fyne.Resource {
	// For macOS .app bundles, look for icon in Resources
	if strings.HasSuffix(executable, ".app") || strings.Contains(executable, ".app/Contents") {
		// Extract .app path
		appPath := executable
		if !strings.HasSuffix(appPath, ".app") {
			// Find .app in path
			parts := strings.Split(appPath, ".app")
			if len(parts) > 0 {
				appPath = parts[0] + ".app"
			}
		}

		resourcesDir := filepath.Join(appPath, "Contents", "Resources")
		if entries, err := os.ReadDir(resourcesDir); err == nil {
			// First pass: try PNG and other standard formats (Fyne supports these)
			for _, entry := range entries {
				name := strings.ToLower(entry.Name())
				// Prioritize PNG and other standard formats that Fyne can load
				if strings.HasSuffix(name, ".png") ||
					strings.HasSuffix(name, ".jpg") ||
					strings.HasSuffix(name, ".jpeg") ||
					strings.HasSuffix(name, ".bmp") ||
					strings.HasSuffix(name, ".ico") {
					iconPath := filepath.Join(resourcesDir, entry.Name())
					if resource, err := fyne.LoadResourceFromPath(iconPath); err == nil {
						return resource
					}
				}
			}
			// Second pass: try .icns files (Fyne may not support these, but try anyway)
			// Note: .icns files often fail to load, so we try them last
			for _, entry := range entries {
				name := strings.ToLower(entry.Name())
				if strings.HasSuffix(name, ".icns") {
					iconPath := filepath.Join(resourcesDir, entry.Name())
					if resource, err := fyne.LoadResourceFromPath(iconPath); err == nil {
						return resource
					}
					// If .icns fails, don't save it to config - it won't work later either
				}
			}
		}
	}

	// For Windows, try icon/image files in same directory
	if strings.HasSuffix(strings.ToLower(executable), ".exe") {
		dir := filepath.Dir(executable)
		if entries, err := os.ReadDir(dir); err == nil {
			exeName := strings.TrimSuffix(filepath.Base(executable), ".exe")
			for _, entry := range entries {
				name := strings.ToLower(entry.Name())
				// Support multiple image formats
				if (strings.HasSuffix(name, ".ico") ||
					strings.HasSuffix(name, ".png") ||
					strings.HasSuffix(name, ".jpg") ||
					strings.HasSuffix(name, ".jpeg") ||
					strings.HasSuffix(name, ".bmp")) &&
					strings.Contains(name, strings.ToLower(exeName)) {
					iconPath := filepath.Join(dir, entry.Name())
					if resource, err := fyne.LoadResourceFromPath(iconPath); err == nil {
						return resource
					}
				}
			}
		}
	}

	// For Linux, try common image formats in same directory
	dir := filepath.Dir(executable)
	if entries, err := os.ReadDir(dir); err == nil {
		exeName := filepath.Base(executable)
		for _, entry := range entries {
			name := strings.ToLower(entry.Name())
			// Support multiple image formats
			if strings.HasSuffix(name, ".png") ||
				strings.HasSuffix(name, ".jpg") ||
				strings.HasSuffix(name, ".jpeg") ||
				strings.HasSuffix(name, ".bmp") ||
				strings.HasSuffix(name, ".ico") ||
				strings.HasSuffix(name, ".svg") {
				// Try to match by name
				baseName := strings.TrimSuffix(strings.ToLower(exeName), filepath.Ext(exeName))
				iconBaseName := strings.TrimSuffix(strings.ToLower(entry.Name()), filepath.Ext(entry.Name()))
				if strings.Contains(iconBaseName, baseName) || strings.Contains(baseName, iconBaseName) {
					iconPath := filepath.Join(dir, entry.Name())
					if resource, err := fyne.LoadResourceFromPath(iconPath); err == nil {
						return resource
					}
				}
			}
		}
	}

	return nil
}

// appCardRenderer renders the app card
type appCardRenderer struct {
	widget  *AppCardWidget
	content *fyne.Container
	objects []fyne.CanvasObject
}

func (r *appCardRenderer) Layout(size fyne.Size) {
	r.content.Resize(size)
	r.content.Move(fyne.NewPos(0, 0))
}

func (r *appCardRenderer) MinSize() fyne.Size {
	return r.content.MinSize()
}

func (r *appCardRenderer) Refresh() {
	// Reload icon if needed
	if r.widget.icon != nil {
		newIcon := r.widget.loadAppIcon()
		if newIcon != nil {
			r.widget.icon = newIcon
			// Update the content
			label := r.widget.label
			if label == nil {
				label = widget.NewLabel(r.widget.app.Name)
				label.Wrapping = fyne.TextWrapWord
				label.Alignment = fyne.TextAlignCenter
				r.widget.label = label
			}
			r.content.Objects = []fyne.CanvasObject{
				container.NewVBox(
					container.NewCenter(newIcon),
					label,
				),
			}
		}
	}
	r.content.Refresh()
}

func (r *appCardRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *appCardRenderer) Destroy() {}

// Update createAppCard to use the new widget
func createAppCard(app App, tabID string) fyne.CanvasObject {
	card := NewAppCardWidget(app, tabID)
	return container.NewPadded(card)
}

func showCreateTabDialog() {
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("Enter tab name (e.g., Productivity)")
	nameEntry.Wrapping = fyne.TextWrapOff

	// Create a custom dialog with wider entry field (32 characters width)
	formContent := container.NewVBox(
		widget.NewLabel("Tab Name:"),
		container.NewPadded(nameEntry),
	)

	// Check if dialog is already open
	if existingWindow := showOrFocusDialog("Create New Tab"); existingWindow != nil {
		return
	}

	// Create dialog window with custom size (wider for 32 characters)
	dialogWindow := myApp.NewWindow("Create New Tab")
	dialogWindow.Resize(fyne.NewSize(500, 150))
	registerDialog(dialogWindow)

	createBtn := widget.NewButton("Create", func() {
		name := strings.TrimSpace(nameEntry.Text)
		if name == "" {
			dialog.ShowError(fmt.Errorf("Tab name cannot be empty"), dialogWindow)
			return
		}

		// Check if tab name already exists
		for _, tab := range config.Tabs {
			if tab.Name == name {
				dialog.ShowError(fmt.Errorf("Tab '%s' already exists", name), dialogWindow)
				return
			}
		}

		// Create new tab
		newTabID := fmt.Sprintf("tab_%d", len(config.Tabs)+1)
		newTab := Tab{
			ID:     newTabID,
			Name:   name,
			AppIDs: []string{},
		}

		config.Tabs = append(config.Tabs, newTab)

		// Create grid for new tab
		grid := createAppGrid(newTabID)
		appGrids[newTabID] = grid

		// Refresh tabs UI
		refreshTabsUI()

		// Save config
		if err := SaveConfig(config, configPath); err != nil {
			dialog.ShowError(fmt.Errorf("Failed to save config: %v", err), dialogWindow)
		} else {
			dialogWindow.Close()
		}
	})

	cancelBtn := widget.NewButton("Cancel", func() {
		dialogWindow.Close()
	})

	dialogWindow.SetContent(container.NewBorder(
		nil,
		container.NewHBox(createBtn, cancelBtn),
		nil,
		nil,
		formContent,
	))
	centerDialogOnMainWindow(dialogWindow)
}

func showEditTabDialog() {
	if currentTabID == "home" {
		dialog.ShowInformation("Cannot Edit", "The Home tab cannot be edited.", mainWindow)
		return
	}

	tab := getTabByID(currentTabID)
	if tab == nil {
		return
	}

	nameEntry := widget.NewEntry()
	nameEntry.SetText(tab.Name)
	nameEntry.Wrapping = fyne.TextWrapOff

	// Create a custom dialog with wider entry field (32 characters width)
	formContent := container.NewVBox(
		widget.NewLabel("Tab Name:"),
		container.NewPadded(nameEntry),
	)

	// Check if dialog is already open
	if existingWindow := showOrFocusDialog("Edit Tab"); existingWindow != nil {
		return
	}

	// Create dialog window with custom size (wider for 32 characters)
	dialogWindow := myApp.NewWindow("Edit Tab")
	dialogWindow.Resize(fyne.NewSize(500, 150))
	registerDialog(dialogWindow)

	saveBtn := widget.NewButton("Save", func() {
		newName := strings.TrimSpace(nameEntry.Text)
		if newName == "" {
			dialog.ShowError(fmt.Errorf("Tab name cannot be empty"), dialogWindow)
			return
		}

		// Update tab name
		for i := range config.Tabs {
			if config.Tabs[i].ID == currentTabID {
				config.Tabs[i].Name = newName
				break
			}
		}

		// Refresh tabs UI
		refreshTabsUI()

		// Save config
		if err := SaveConfig(config, configPath); err != nil {
			dialog.ShowError(fmt.Errorf("Failed to save config: %v", err), dialogWindow)
		} else {
			dialogWindow.Close()
		}
	})

	cancelBtn := widget.NewButton("Cancel", func() {
		dialogWindow.Close()
	})

	dialogWindow.SetContent(container.NewBorder(
		nil,
		container.NewHBox(saveBtn, cancelBtn),
		nil,
		nil,
		formContent,
	))
	centerDialogOnMainWindow(dialogWindow)
}

func showDeleteTabDialog() {
	if currentTabID == "home" {
		dialog.ShowInformation("Cannot Delete", "The Home tab cannot be deleted.", mainWindow)
		return
	}

	tab := getTabByID(currentTabID)
	if tab == nil {
		return
	}

	dialog.ShowConfirm("Delete Tab",
		fmt.Sprintf("Are you sure you want to delete the '%s' tab? This will remove all apps from this tab.", tab.Name),
		func(confirmed bool) {
			if confirmed {
				// Remove tab from config
				newTabs := []Tab{}
				for _, t := range config.Tabs {
					if t.ID != currentTabID {
						newTabs = append(newTabs, t)
					}
				}
				config.Tabs = newTabs

				// Delete grid
				delete(appGrids, currentTabID)

				// Switch to home tab
				currentTabID = "home"

				// Refresh tabs UI
				refreshTabsUI()

				// Save config
				if err := SaveConfig(config, configPath); err != nil {
					dialog.ShowError(fmt.Errorf("Failed to save config: %v", err), mainWindow)
				}
			}
		}, mainWindow)
}

func showDeleteCustomAppDialog(app App) {
	if !app.IsCustom {
		dialog.ShowInformation("Cannot Delete", "Only custom applications can be deleted.", mainWindow)
		return
	}

	// Find the Manage Applications dialog window if it exists
	var manageAppsWindow fyne.Window
	if w, exists := openDialogs["Manage Applications"]; exists && w != nil {
		manageAppsWindow = w
	}

	// Use Manage Applications window as parent if it exists, otherwise use main window
	parentWindow := mainWindow
	if manageAppsWindow != nil {
		parentWindow = manageAppsWindow
	}

	dialog.ShowConfirm("Delete Application",
		fmt.Sprintf("Are you sure you want to delete '%s'? This will remove it from all tabs.", app.Name),
		func(confirmed bool) {
			if confirmed {
				// Remove app from all tabs
				for i := range config.Tabs {
					newAppIDs := []string{}
					for _, appID := range config.Tabs[i].AppIDs {
						if appID != app.ID {
							newAppIDs = append(newAppIDs, appID)
						}
					}
					config.Tabs[i].AppIDs = newAppIDs
				}

				// Remove app from config
				newApps := []App{}
				for _, a := range config.Apps {
					if a.ID != app.ID {
						newApps = append(newApps, a)
					}
				}
				config.Apps = newApps

				// Refresh all tab grids (apps will be sorted alphabetically)
				for tabID := range appGrids {
					refreshGridWithSortedApps(tabID)
				}

				// Save config
				if err := SaveConfig(config, configPath); err != nil {
					dialog.ShowError(fmt.Errorf("Failed to save config: %v", err), mainWindow)
				} else {
					dialog.ShowInformation("Success", fmt.Sprintf("Deleted '%s'", app.Name), parentWindow)
				}
			}
		}, parentWindow)
}

func showManageAppsDialog() {
	// Create a sorted copy of apps for display
	sortedApps := make([]App, len(config.Apps))
	copy(sortedApps, config.Apps)

	// Sort apps alphabetically by name (case-insensitive)
	sort.Slice(sortedApps, func(i, j int) bool {
		return strings.ToLower(sortedApps[i].Name) < strings.ToLower(sortedApps[j].Name)
	})

	// Filtered apps list (starts with all apps)
	filteredApps := make([]App, len(sortedApps))
	copy(filteredApps, sortedApps)

	// Filter entry field
	filterEntry := widget.NewEntry()
	filterEntry.SetPlaceHolder("Filter applications by name...")
	filterEntry.Wrapping = fyne.TextWrapOff

	// List of apps (using filtered list)
	var appList *widget.List
	appList = widget.NewList(
		func() int {
			return len(filteredApps)
		},
		func() fyne.CanvasObject {
			return container.NewHBox(
				widget.NewLabel(""),
				widget.NewButton("Add to Tab", nil),
				widget.NewButton("Remove from Current Tab", nil),
				widget.NewButton("", nil), // Delete button for custom apps
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id >= len(filteredApps) {
				return
			}
			app := filteredApps[id]
			container := obj.(*fyne.Container)
			label := container.Objects[0].(*widget.Label)
			addBtn := container.Objects[1].(*widget.Button)
			removeBtn := container.Objects[2].(*widget.Button)
			deleteBtn := container.Objects[3].(*widget.Button)

			labelText := app.Name
			if app.IsCustom {
				labelText += " (Custom)"
			}
			label.SetText(labelText)

			addBtn.SetText("Add to Tab")
			addBtn.OnTapped = func() {
				// If current tab is not "home", add directly to current tab
				if currentTabID != "home" {
					// Check if app is already in tab
					tab := getTabByID(currentTabID)
					if tab != nil {
						alreadyAdded := false
						for _, appID := range tab.AppIDs {
							if appID == app.ID {
								alreadyAdded = true
								break
							}
						}

						if !alreadyAdded {
							// Add app to current tab
							for i := range config.Tabs {
								if config.Tabs[i].ID == currentTabID {
									config.Tabs[i].AppIDs = append(config.Tabs[i].AppIDs, app.ID)
									// Refresh the grid (apps will be sorted alphabetically)
									refreshGridWithSortedApps(currentTabID)

									// Save config
									if err := SaveConfig(config, configPath); err != nil {
										dialog.ShowError(fmt.Errorf("Failed to save config: %v", err), mainWindow)
									}
									break
								}
							}
						} else {
							dialog.ShowInformation("Already Added", fmt.Sprintf("'%s' is already in the current tab", app.Name), mainWindow)
						}
					}
				} else {
					// Current tab is "home", show dialog to select which tab
					showAddAppToTabDialog(app)
				}
			}

			// Remove from current tab button (works for all apps)
			removeBtn.SetText("Remove from Current Tab")
			removeBtn.OnTapped = func() {
				removeAppFromTab(app.ID, currentTabID)
			}

			// Delete button (only for custom apps)
			if app.IsCustom {
				deleteBtn.SetText("Delete Application")
				deleteBtn.Show()
				deleteBtn.OnTapped = func() {
					showDeleteCustomAppDialog(app)
				}
			} else {
				deleteBtn.Hide()
			}
		},
	)

	// Filter function
	applyFilter := func(filterText string) {
		filterText = strings.ToLower(strings.TrimSpace(filterText))
		if filterText == "" {
			// Show all apps
			filteredApps = make([]App, len(sortedApps))
			copy(filteredApps, sortedApps)
		} else {
			// Filter apps by name
			filteredApps = []App{}
			for _, app := range sortedApps {
				if strings.Contains(strings.ToLower(app.Name), filterText) {
					filteredApps = append(filteredApps, app)
				}
			}
		}
		appList.Refresh()
	}

	// Update filter when text changes
	filterEntry.OnChanged = func(text string) {
		applyFilter(text)
	}

	// Add custom app button
	addCustomBtn := widget.NewButton("Add Custom Application", func() {
		showAddCustomAppDialog()
	})

	scrollContainer := container.NewScroll(appList)
	scrollContainer.SetMinSize(fyne.NewSize(600, 400)) // Wider and taller

	// Create content with filter entry, label and scrollable list
	contentVBox := container.NewVBox(
		widget.NewLabel("Filter:"),
		container.NewPadded(filterEntry),
		widget.NewLabel(fmt.Sprintf("All Applications (sorted alphabetically) - Showing %d of %d", len(filteredApps), len(sortedApps))),
		scrollContainer,
	)

	// Update count label when filter changes
	originalOnChanged := filterEntry.OnChanged
	filterEntry.OnChanged = func(text string) {
		originalOnChanged(text)
		// Update the count label
		countLabel := contentVBox.Objects[2].(*widget.Label)
		countLabel.SetText(fmt.Sprintf("All Applications (sorted alphabetically) - Showing %d of %d", len(filteredApps), len(sortedApps)))
	}

	// Check if dialog is already open
	if existingWindow := showOrFocusDialog("Manage Applications"); existingWindow != nil {
		return
	}

	// Create a custom dialog window with larger size
	dialogWindow := myApp.NewWindow("Manage Applications")
	registerDialog(dialogWindow)

	// Create bottom buttons
	bottomButtons := container.NewHBox(
		addCustomBtn,
		widget.NewButton("Close", func() {
			// Explicitly remove from tracking before closing
			title := dialogWindow.Title()
			delete(openDialogs, title)
			// Remove from child windows
			for i, w := range childWindows {
				if w == dialogWindow {
					childWindows = append(childWindows[:i], childWindows[i+1:]...)
					break
				}
			}
			dialogWindow.Close()
		}),
	)
	dialogWindow.Resize(fyne.NewSize(700, 500))
	dialogWindow.SetContent(container.NewBorder(
		nil,
		bottomButtons,
		nil,
		nil,
		contentVBox,
	))
	centerDialogOnMainWindow(dialogWindow)
}

// refreshManageAppsDialog refreshes the Manage Apps dialog if it's open
func refreshManageAppsDialog() {
	if manageAppsWindow, exists := openDialogs["Manage Applications"]; exists && manageAppsWindow != nil {
		// Check if window is still visible
		if manageAppsWindow.Content() != nil && manageAppsWindow.Content().Visible() {
			// Explicitly remove from tracking before closing
			delete(openDialogs, "Manage Applications")
			// Remove from child windows
			for i, w := range childWindows {
				if w == manageAppsWindow {
					childWindows = append(childWindows[:i], childWindows[i+1:]...)
					break
				}
			}
			// Close the window
			manageAppsWindow.Close()
			// Small delay to ensure window is closed before reopening
			go func() {
				time.Sleep(100 * time.Millisecond)
				showManageAppsDialog()
			}()
		} else {
			// Window is not visible, clean it up
			delete(openDialogs, "Manage Applications")
		}
	}
}

func removeAppFromTab(appID, tabID string) {
	tab := getTabByID(tabID)
	if tab == nil {
		return
	}

	// Remove app from tab
	newAppIDs := []string{}
	for _, id := range tab.AppIDs {
		if id != appID {
			newAppIDs = append(newAppIDs, id)
		}
	}

	// Update tab
	for i := range config.Tabs {
		if config.Tabs[i].ID == tabID {
			config.Tabs[i].AppIDs = newAppIDs
			break
		}
	}

	// Refresh the grid (apps will be sorted alphabetically)
	refreshGridWithSortedApps(tabID)

	// Save config
	if err := SaveConfig(config, configPath); err != nil {
		dialog.ShowError(fmt.Errorf("Failed to save config: %v", err), mainWindow)
	}
}

func showCopyToTabDialog(app App, currentTabID string) {
	// Create a dialog to select which tab to copy the app to
	selectedTabID := ""

	// Filter out the current tab from the list
	availableTabs := []Tab{}
	for _, tab := range config.Tabs {
		if tab.ID != currentTabID {
			availableTabs = append(availableTabs, tab)
		}
	}

	if len(availableTabs) == 0 {
		dialog.ShowInformation("No Other Tabs", "There are no other tabs to copy this app to.", mainWindow)
		return
	}

	var tabList *widget.List
	tabList = widget.NewList(
		func() int {
			return len(availableTabs)
		},
		func() fyne.CanvasObject {
			// Create checkbox with initial handler
			check := widget.NewCheck("", nil)
			button := widget.NewButton("", nil)
			return container.NewHBox(check, button)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			tab := availableTabs[id]
			container := obj.(*fyne.Container)
			check := container.Objects[0].(*widget.Check)
			button := container.Objects[1].(*widget.Button)

			labelText := tab.Name

			// Check if app is already in this tab
			alreadyAdded := false
			for _, appID := range tab.AppIDs {
				if appID == app.ID {
					alreadyAdded = true
					break
				}
			}

			// Set initial state
			if alreadyAdded {
				labelText += " (already added)"
				check.SetChecked(true)
				check.Disable()
				button.Disable()
			} else {
				// Check if this tab is currently selected
				isSelected := (selectedTabID == tab.ID)
				check.SetChecked(isSelected)
				check.Enable()
				button.Enable()
			}
			button.SetText(labelText)

			// Set checkbox handler - clear previous handler first
			check.OnChanged = nil
			check.OnChanged = func(checked bool) {
				if checked && !alreadyAdded {
					selectedTabID = tab.ID
					// Refresh to uncheck other items
					tabList.Refresh()
				} else if !checked {
					if selectedTabID == tab.ID {
						selectedTabID = ""
					}
				}
			}

			// Set button handler
			button.OnTapped = nil
			button.OnTapped = func() {
				if !alreadyAdded {
					check.SetChecked(!check.Checked)
				}
			}
		},
	)

	scrollContainer := container.NewScroll(tabList)
	scrollContainer.SetMinSize(fyne.NewSize(400, 300))

	// Check if dialog is already open
	if existingWindow := showOrFocusDialog("Copy to Tab"); existingWindow != nil {
		return
	}

	dialogWindow := myApp.NewWindow("Copy to Tab")
	registerDialog(dialogWindow)
	dialogWindow.Resize(fyne.NewSize(500, 400))

	copyButton := widget.NewButton("Copy", func() {
		if selectedTabID != "" {
			// Add app to selected tab
			for i := range config.Tabs {
				if config.Tabs[i].ID == selectedTabID {
					// Check if app is already in tab
					alreadyAdded := false
					for _, existingID := range config.Tabs[i].AppIDs {
						if existingID == app.ID {
							alreadyAdded = true
							break
						}
					}

					if !alreadyAdded {
						config.Tabs[i].AppIDs = append(config.Tabs[i].AppIDs, app.ID)
						// Refresh the grid (apps will be sorted alphabetically)
						refreshGridWithSortedApps(selectedTabID)

						// Save config
						if err := SaveConfig(config, configPath); err != nil {
							dialog.ShowError(fmt.Errorf("Failed to save config: %v", err), mainWindow)
						} else {
							dialogWindow.Close()
							dialog.ShowInformation("Success", fmt.Sprintf("Copied '%s' to '%s' tab", app.Name, config.Tabs[i].Name), mainWindow)
						}
					} else {
						dialog.ShowInformation("Already Added", fmt.Sprintf("'%s' is already in the '%s' tab", app.Name, config.Tabs[i].Name), mainWindow)
					}
					break
				}
			}
		} else {
			dialog.ShowInformation("No Selection", "Please select a tab to copy the app to.", dialogWindow)
		}
	})

	content := container.NewVBox(
		widget.NewLabel(fmt.Sprintf("Copy '%s' to which tab?", app.Name)),
		scrollContainer,
	)

	cancelButton := widget.NewButton("Cancel", func() {
		dialogWindow.Close()
	})

	bottomButtons := container.NewHBox(copyButton, cancelButton)
	dialogWindow.SetContent(container.NewBorder(
		nil,
		bottomButtons,
		nil,
		nil,
		content,
	))
	centerDialogOnMainWindow(dialogWindow)
}

func showAddAppToTabDialog(app App) {
	// Create a dialog to select which tab to add the app to
	selectedTabID := ""

	tabList := widget.NewList(
		func() int {
			return len(config.Tabs)
		},
		func() fyne.CanvasObject {
			return container.NewHBox(
				widget.NewLabel(""),
				widget.NewCheck("", nil),
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			tab := config.Tabs[id]
			container := obj.(*fyne.Container)
			label := container.Objects[0].(*widget.Label)
			check := container.Objects[1].(*widget.Check)

			label.SetText(tab.Name)

			// Check if app is already in this tab
			alreadyAdded := false
			for _, appID := range tab.AppIDs {
				if appID == app.ID {
					alreadyAdded = true
					break
				}
			}
			check.SetChecked(alreadyAdded)

			check.OnChanged = func(checked bool) {
				if checked {
					selectedTabID = tab.ID
				} else {
					selectedTabID = ""
				}
			}
		},
	)

	addButton := widget.NewButton("Add", func() {
		if selectedTabID != "" {
			// Add app to selected tab
			for i := range config.Tabs {
				if config.Tabs[i].ID == selectedTabID {
					// Check if app is already in tab
					alreadyAdded := false
					for _, existingID := range config.Tabs[i].AppIDs {
						if existingID == app.ID {
							alreadyAdded = true
							break
						}
					}

					if !alreadyAdded {
						config.Tabs[i].AppIDs = append(config.Tabs[i].AppIDs, app.ID)
						// Refresh the grid (apps will be sorted alphabetically)
						refreshGridWithSortedApps(selectedTabID)
					}
					break
				}
			}

			// Save config
			if err := SaveConfig(config, configPath); err != nil {
				dialog.ShowError(fmt.Errorf("Failed to save config: %v", err), mainWindow)
			}
		}
	})

	content := container.NewVBox(
		widget.NewLabel(fmt.Sprintf("Add '%s' to which tab?", app.Name)),
		tabList,
		container.NewHBox(addButton, widget.NewButton("Cancel", func() {})),
	)

	dialog.ShowCustom("Select Tab", "Close", content, mainWindow)
}

func showAddCustomAppDialog() {
	// Create a custom dialog window so we can control size and positioning
	// Check if dialog is already open
	if existingWindow := showOrFocusDialog("Add Custom Application"); existingWindow != nil {
		return
	}

	dialogWindow := myApp.NewWindow("Add Custom Application")
	registerDialog(dialogWindow)
	dialogWindow.Resize(fyne.NewSize(600, 300))

	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("Application Name")
	nameEntry.Wrapping = fyne.TextWrapOff

	execEntry := widget.NewEntry()
	execEntry.SetPlaceHolder("/path/to/executable or ~/path/to/executable")
	execEntry.Wrapping = fyne.TextWrapOff

	iconEntry := widget.NewEntry()
	iconEntry.SetPlaceHolder("/path/to/icon (optional) or ~/path/to/icon")
	iconEntry.Wrapping = fyne.TextWrapOff

	// Browse button for executable
	execBrowseBtn := widget.NewButton("Browse...", func() {
		dialog.ShowFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil {
				return
			}
			if reader == nil {
				return
			}
			defer reader.Close()
			execPath := reader.URI().Path()
			execEntry.SetText(execPath)
		}, dialogWindow)
	})

	// Browse button for icon
	iconBrowseBtn := widget.NewButton("Browse...", func() {
		dialog.ShowFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil {
				return
			}
			if reader == nil {
				return
			}
			defer reader.Close()
			iconPath := reader.URI().Path()
			iconEntry.SetText(iconPath)
		}, dialogWindow)
	})

	execEntryContainer := container.NewBorder(nil, nil, nil, execBrowseBtn, execEntry)
	iconEntryContainer := container.NewBorder(nil, nil, nil, iconBrowseBtn, iconEntry)

	formContent := container.NewVBox(
		widget.NewLabel("Application Name:"),
		container.NewPadded(nameEntry),
		widget.NewLabel("Executable Path:"),
		container.NewPadded(execEntryContainer),
		widget.NewLabel("Icon Path (optional):"),
		container.NewPadded(iconEntryContainer),
	)

	addBtn := widget.NewButton("Add", func() {
		name := strings.TrimSpace(nameEntry.Text)
		executable := strings.TrimSpace(execEntry.Text)
		icon := strings.TrimSpace(iconEntry.Text)

		if name == "" || executable == "" {
			dialog.ShowError(fmt.Errorf("Name and executable path are required"), dialogWindow)
			return
		}

		// Expand home directory shortcuts
		executable = expandPath(executable)
		if icon != "" {
			icon = expandPath(icon)
		}

		// Check if executable exists
		if _, err := os.Stat(executable); err != nil {
			dialog.ShowError(fmt.Errorf("Executable not found: %v", err), dialogWindow)
			return
		}

		// Check if icon exists (if provided)
		if icon != "" {
			if _, err := os.Stat(icon); err != nil {
				dialog.ShowError(fmt.Errorf("Icon file not found: %v", err), dialogWindow)
				return
			}
		}

		// Create custom app
		customApp := App{
			ID:         fmt.Sprintf("custom_%d", len(config.Apps)+1),
			Name:       name,
			Executable: executable,
			Icon:       icon,
			IsCustom:   true,
		}

		config.Apps = append(config.Apps, customApp)

		// Save config
		if err := SaveConfig(config, configPath); err != nil {
			dialog.ShowError(fmt.Errorf("Failed to save config: %v", err), dialogWindow)
		} else {
			dialogWindow.Close()
			// Refresh Manage Apps dialog if it's open
			refreshManageAppsDialog()
			dialog.ShowInformation("Success", fmt.Sprintf("Added '%s' to applications", name), mainWindow)
		}
	})

	cancelBtn := widget.NewButton("Cancel", func() {
		dialogWindow.Close()
	})

	bottomButtons := container.NewHBox(addBtn, cancelBtn)
	dialogWindow.SetContent(container.NewBorder(
		nil,
		bottomButtons,
		nil,
		nil,
		formContent,
	))

	// Show dialog in front of Manage Applications dialog
	dialogWindow.RequestFocus()
	centerDialogOnMainWindow(dialogWindow)
}

func showEditAppPropertiesDialog(app App, tabID string) {
	nameEntry := widget.NewEntry()
	nameEntry.SetText(app.Name)
	nameEntry.Wrapping = fyne.TextWrapOff

	execEntry := widget.NewEntry()
	execEntry.SetText(app.Executable)
	execEntry.Wrapping = fyne.TextWrapOff

	iconEntry := widget.NewEntry()
	iconEntry.SetText(app.Icon)
	iconEntry.Wrapping = fyne.TextWrapOff

	descEntry := widget.NewMultiLineEntry()
	descEntry.SetText(app.Description)

	// Check if dialog is already open
	if existingWindow := showOrFocusDialog("Edit Application Properties"); existingWindow != nil {
		return
	}

	dialogWindow := myApp.NewWindow("Edit Application Properties")
	registerDialog(dialogWindow)
	dialogWindow.Resize(fyne.NewSize(600, 400))

	// Browse button for executable
	execBrowseBtn := widget.NewButton("Browse...", func() {
		dialog.ShowFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil {
				return
			}
			if reader == nil {
				return
			}
			defer reader.Close()
			execPath := reader.URI().Path()
			execEntry.SetText(execPath)
		}, dialogWindow)
	})

	// Browse button for icon
	iconBrowseBtn := widget.NewButton("Browse...", func() {
		dialog.ShowFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil {
				return
			}
			if reader == nil {
				return
			}
			defer reader.Close()
			iconPath := reader.URI().Path()
			iconEntry.SetText(iconPath)
		}, dialogWindow)
	})

	execEntryContainer := container.NewBorder(nil, nil, nil, execBrowseBtn, execEntry)
	iconEntryContainer := container.NewBorder(nil, nil, nil, iconBrowseBtn, iconEntry)

	formContent := container.NewVBox(
		widget.NewLabel("Application Name:"),
		container.NewPadded(nameEntry),
		widget.NewLabel("Executable Path:"),
		container.NewPadded(execEntryContainer),
		widget.NewLabel("Icon Path (optional):"),
		container.NewPadded(iconEntryContainer),
		widget.NewLabel("Description (optional):"),
		container.NewPadded(descEntry),
	)

	scrollForm := container.NewScroll(formContent)

	saveBtn := widget.NewButton("Save", func() {
		newName := strings.TrimSpace(nameEntry.Text)
		newExec := strings.TrimSpace(execEntry.Text)
		newIcon := strings.TrimSpace(iconEntry.Text)
		newDesc := strings.TrimSpace(descEntry.Text)

		if newName == "" || newExec == "" {
			dialog.ShowError(fmt.Errorf("Name and executable path are required"), dialogWindow)
			return
		}

		// Update app in config
		for i := range config.Apps {
			if config.Apps[i].ID == app.ID {
				config.Apps[i].Name = newName
				config.Apps[i].Executable = newExec
				config.Apps[i].Icon = newIcon
				config.Apps[i].Description = newDesc
				break
			}
		}

		// Refresh the grid (apps will be sorted alphabetically)
		refreshGridWithSortedApps(tabID)

		// Save config
		if err := SaveConfig(config, configPath); err != nil {
			dialog.ShowError(fmt.Errorf("Failed to save config: %v", err), dialogWindow)
		} else {
			dialogWindow.Close()
		}
	})

	cancelBtn := widget.NewButton("Cancel", func() {
		dialogWindow.Close()
	})

	bottomButtons := container.NewHBox(saveBtn, cancelBtn)
	dialogWindow.SetContent(container.NewBorder(
		nil,
		bottomButtons,
		nil,
		nil,
		scrollForm,
	))
	centerDialogOnMainWindow(dialogWindow)
}

func showChangeIconDialog(app App, tabID string) {
	// Check if dialog is already open
	if existingWindow := showOrFocusDialog("Change Icon"); existingWindow != nil {
		return
	}

	dialogWindow := myApp.NewWindow("Change Icon")
	dialogWindow.Resize(fyne.NewSize(500, 200))
	registerDialog(dialogWindow)

	iconEntry := widget.NewEntry()
	iconEntry.SetText(app.Icon)
	iconEntry.Wrapping = fyne.TextWrapOff

	// Icon preview
	iconPreview := canvas.NewImageFromResource(theme.FolderIcon())
	iconPreview.FillMode = canvas.ImageFillContain
	iconPreview.SetMinSize(fyne.NewSize(64, 64))
	iconPreview.Resize(fyne.NewSize(64, 64))

	// Function to update icon preview and apply immediately
	updateIcon := func(iconPath string) {
		iconEntry.SetText(iconPath)

		// Update preview
		if iconPath != "" && fileExists(iconPath) {
			if resource, err := fyne.LoadResourceFromPath(iconPath); err == nil {
				iconPreview.Resource = resource
				iconPreview.Refresh()
			}
		} else {
			// Try to detect icon from executable
			if detectedIcon := detectIconFromExecutable(app.Executable); detectedIcon != nil {
				iconPreview.Resource = detectedIcon
				iconPreview.Refresh()
			} else {
				iconPreview.Resource = theme.FolderIcon()
				iconPreview.Refresh()
			}
		}

		// Apply immediately
		newIcon := strings.TrimSpace(iconPath)
		if newIcon != "" {
			// Update app icon in config
			for i := range config.Apps {
				if config.Apps[i].ID == app.ID {
					config.Apps[i].Icon = newIcon
					break
				}
			}

			// Refresh the grid (apps will be sorted alphabetically)
			refreshGridWithSortedApps(tabID)

			// Save config
			if err := SaveConfig(config, configPath); err != nil {
				dialog.ShowError(fmt.Errorf("Failed to save config: %v", err), dialogWindow)
			}
		}
	}

	// Initialize preview
	if app.Icon != "" && fileExists(app.Icon) {
		if resource, err := fyne.LoadResourceFromPath(app.Icon); err == nil {
			iconPreview.Resource = resource
		}
	} else if detectedIcon := detectIconFromExecutable(app.Executable); detectedIcon != nil {
		iconPreview.Resource = detectedIcon
	}

	browseBtn := widget.NewButton("Browse...", func() {
		// Use dialogWindow as parent so file dialog appears on top
		dialog.ShowFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil {
				return
			}
			if reader == nil {
				return
			}
			defer reader.Close()
			iconPath := reader.URI().Path()
			updateIcon(iconPath)
			// Close dialog after selection
			dialogWindow.Close()
		}, dialogWindow)
	})

	iconEntryContainer := container.NewBorder(nil, nil, nil, browseBtn, iconEntry)

	formContent := container.NewVBox(
		container.NewHBox(
			container.NewCenter(iconPreview),
			container.NewVBox(
				widget.NewLabel("Icon Path:"),
				container.NewPadded(iconEntryContainer),
				widget.NewLabel("Supported formats: PNG, JPG, JPEG, BMP, ICO, ICNS"),
			),
		),
	)

	closeBtn := widget.NewButton("Close", func() {
		dialogWindow.Close()
	})

	bottomButtons := container.NewHBox(closeBtn)
	dialogWindow.SetContent(container.NewBorder(
		nil,
		bottomButtons,
		nil,
		nil,
		formContent,
	))
	centerDialogOnMainWindow(dialogWindow)
}

func showAppDetailsDialog(app App) {
	details := fmt.Sprintf("Application Details\n\n")
	details += fmt.Sprintf("Name: %s\n", app.Name)
	details += fmt.Sprintf("Executable: %s\n", app.Executable)
	if app.Icon != "" {
		details += fmt.Sprintf("Icon: %s\n", app.Icon)
	}
	if app.Description != "" {
		details += fmt.Sprintf("Description: %s\n", app.Description)
	}
	if app.Category != "" {
		details += fmt.Sprintf("Category: %s\n", app.Category)
	}
	details += fmt.Sprintf("Type: %s\n", map[bool]string{true: "Custom", false: "Discovered"}[app.IsCustom])

	dialog.ShowInformation("Application Details", details, mainWindow)
}

func showAboutDialog() {
	// Check if dialog is already open
	if existingWindow := showOrFocusDialog("About"); existingWindow != nil {
		return
	}

	aboutText := appName + " v " + appVersion
	aboutText += "\n" + appCopyright
	aboutText += "\n\nCreated by " + appAuthor + ", using Go and fyne GUI"
	aboutText += "\n\nNo obligation, it's rewarding to hear if you use this app."
	aboutText += "\n\nAnd looking about about and help or settings too much might expose an easter egg!"

	kb := canvas.NewImageFromResource(resourceKrankyBearTrapperRedPlaidPng)
	kb.FillMode = canvas.ImageFillOriginal
	text := widget.NewLabel(aboutText)
	content := container.NewHBox(kb, text)

	dialogWindow := myApp.NewWindow(appName + ": About")
	dialogWindow.SetIcon(resourceKrankyBearTrapperRedPlaidPng)
	dialogWindow.Resize(fyne.NewSize(500, 200))
	dialogWindow.SetContent(content)

	registerDialog(dialogWindow)
	centerDialogOnMainWindow(dialogWindow)
}

// showThemeSettingsDialog displays a dialog to switch between light and dark themes
func showThemeSettingsDialog() {
	// Check if dialog is already open
	if existingWindow := showOrFocusDialog("Theme Settings"); existingWindow != nil {
		return
	}

	dialogWindow := myApp.NewWindow("Theme Settings")
	dialogWindow.Resize(fyne.NewSize(400, 200))

	// Register dialog (this will set up proper cleanup on close)
	registerDialog(dialogWindow)

	// Get current theme variant
	currentVariant := myApp.Settings().ThemeVariant()
	var currentThemeText string
	// Check theme variant - VariantDark is 1, VariantLight is 0
	if currentVariant == theme.VariantDark {
		currentThemeText = "Dark"
	} else {
		currentThemeText = "Light"
	}

	currentLabel := widget.NewLabel(fmt.Sprintf("Current theme: %s", currentThemeText))
	currentLabel.Alignment = fyne.TextAlignCenter

	lightBtn := widget.NewButton("Light Theme", func() {
		// Set light theme wrapped in our custom theme
		myApp.Settings().SetTheme(&appTheme{Theme: theme.LightTheme()})
		// Save preference
		myApp.Preferences().SetString("theme", "light")
		currentLabel.SetText("Current theme: Light")
		// Refresh all windows to apply theme change
		if mainWindow != nil {
			mainWindow.Content().Refresh()
		}
		for _, w := range childWindows {
			if w != nil {
				w.Content().Refresh()
			}
		}
	})

	darkBtn := widget.NewButton("Dark Theme", func() {
		// Set dark theme wrapped in our custom theme
		myApp.Settings().SetTheme(&appTheme{Theme: theme.DarkTheme()})
		// Save preference
		myApp.Preferences().SetString("theme", "dark")
		currentLabel.SetText("Current theme: Dark")
		// Refresh all windows to apply theme change
		if mainWindow != nil {
			mainWindow.Content().Refresh()
		}
		for _, w := range childWindows {
			if w != nil {
				w.Content().Refresh()
			}
		}
	})

	closeBtn := widget.NewButton("Close", func() {
		// Explicitly remove from tracking before closing
		title := dialogWindow.Title()
		delete(openDialogs, title)
		// Remove from child windows
		for i, w := range childWindows {
			if w == dialogWindow {
				childWindows = append(childWindows[:i], childWindows[i+1:]...)
				break
			}
		}
		dialogWindow.Close()
	})

	content := container.NewVBox(
		widget.NewLabel("Select Theme"),
		currentLabel,
		container.NewHBox(lightBtn, darkBtn),
		widget.NewLabel("Note: Theme changes apply to all Fyne applications."),
	)

	bottomButtons := container.NewHBox(closeBtn)
	dialogWindow.SetContent(container.NewBorder(
		nil,
		bottomButtons,
		nil,
		nil,
		content,
	))
	centerDialogOnMainWindow(dialogWindow)
}

func showSortTabsDialog() {
	// Create a working copy of tabs for reordering
	tabsCopy := make([]Tab, len(config.Tabs))
	copy(tabsCopy, config.Tabs)

	// Create list widget for tabs
	var tabList *widget.List
	tabList = widget.NewList(
		func() int {
			return len(tabsCopy)
		},
		func() fyne.CanvasObject {
			return container.NewHBox(
				widget.NewButton("↑", nil),
				widget.NewButton("↓", nil),
				widget.NewLabel(""),
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			tab := tabsCopy[id]
			container := obj.(*fyne.Container)
			upBtn := container.Objects[0].(*widget.Button)
			downBtn := container.Objects[1].(*widget.Button)
			label := container.Objects[2].(*widget.Label)

			labelText := tab.Name
			if tab.ID == "home" {
				labelText += " (Home - cannot be moved)"
			}
			label.SetText(labelText)

			// Up button
			upBtn.SetText("↑")
			upBtn.OnTapped = func() {
				if id > 0 && tab.ID != "home" {
					// Swap with previous tab (unless it's home)
					if tabsCopy[id-1].ID != "home" {
						tabsCopy[id], tabsCopy[id-1] = tabsCopy[id-1], tabsCopy[id]
						tabList.Refresh()
					}
				}
			}

			// Down button
			downBtn.SetText("↓")
			downBtn.OnTapped = func() {
				if id < len(tabsCopy)-1 && tab.ID != "home" {
					tabsCopy[id], tabsCopy[id+1] = tabsCopy[id+1], tabsCopy[id]
					tabList.Refresh()
				}
			}

			// Disable buttons for Home tab
			if tab.ID == "home" {
				upBtn.Disable()
				downBtn.Disable()
			} else {
				upBtn.Enable()
				downBtn.Enable()
			}
		},
	)

	scrollContainer := container.NewScroll(tabList)
	scrollContainer.SetMinSize(fyne.NewSize(400, 300))

	// Check if dialog is already open
	if existingWindow := showOrFocusDialog("Sort Tabs"); existingWindow != nil {
		return
	}

	dialogWindow := myApp.NewWindow("Sort Tabs")
	registerDialog(dialogWindow)
	dialogWindow.Resize(fyne.NewSize(500, 400))

	saveBtn := widget.NewButton("Save", func() {
		// Update config with new tab order
		config.Tabs = tabsCopy

		// Save config
		if err := SaveConfig(config, configPath); err != nil {
			dialog.ShowError(fmt.Errorf("Failed to save config: %v", err), dialogWindow)
		} else {
			// Refresh tabs UI to reflect new order
			refreshTabsUI()
			dialogWindow.Close()
		}
	})

	cancelBtn := widget.NewButton("Cancel", func() {
		dialogWindow.Close()
	})

	content := container.NewVBox(
		widget.NewLabel("Drag tabs up or down to reorder them. Home tab cannot be moved."),
		scrollContainer,
	)

	bottomButtons := container.NewHBox(saveBtn, cancelBtn)
	dialogWindow.SetContent(container.NewBorder(
		nil,
		bottomButtons,
		nil,
		nil,
		content,
	))
	centerDialogOnMainWindow(dialogWindow)
}

func refreshApps() {
	// Re-discover apps
	_, err := DiscoverApps()
	if err != nil {
		dialog.ShowError(fmt.Errorf("Failed to refresh apps: %v", err), mainWindow)
		return
	}

	// Merge with existing apps
	mergeDiscoveredApps()

	// Save config
	if err := SaveConfig(config, configPath); err != nil {
		dialog.ShowError(fmt.Errorf("Failed to save config: %v", err), mainWindow)
	} else {
		dialog.ShowInformation("Success", "Applications refreshed", mainWindow)
	}
}

func mergeDiscoveredApps() {
	// Create a map of existing app executables
	existingApps := make(map[string]bool)
	for _, app := range config.Apps {
		existingApps[app.Executable] = true
	}

	// Add discovered apps that don't already exist
	for _, discoveredApp := range discoveredApps {
		if !existingApps[discoveredApp.Executable] {
			discoveredApp.ID = fmt.Sprintf("discovered_%d", len(config.Apps)+1)
			// Auto-detect and save icon path if not set
			if discoveredApp.Icon == "" {
				if iconPath := findIconPathFromExecutable(discoveredApp.Executable); iconPath != "" {
					discoveredApp.Icon = iconPath
				}
			}
			config.Apps = append(config.Apps, discoveredApp)
			existingApps[discoveredApp.Executable] = true
		} else {
			// Update icon path if app exists but icon is missing
			for i := range config.Apps {
				if config.Apps[i].Executable == discoveredApp.Executable && config.Apps[i].Icon == "" {
					if iconPath := findIconPathFromExecutable(discoveredApp.Executable); iconPath != "" {
						config.Apps[i].Icon = iconPath
					}
				}
			}
		}
	}
}

// autoAddAppsToHomeTab adds all discovered apps to the Home tab if they're not already in any tab
func autoAddAppsToHomeTab() {
	// Find Home tab
	homeTab := getTabByID("home")
	if homeTab == nil {
		// Create Home tab if it doesn't exist
		homeTab = &Tab{
			ID:     "home",
			Name:   "Home",
			AppIDs: []string{},
		}
		config.Tabs = append([]Tab{*homeTab}, config.Tabs...)
	}

	// Create a map of app IDs already in any tab
	appsInTabs := make(map[string]bool)
	for _, tab := range config.Tabs {
		for _, appID := range tab.AppIDs {
			appsInTabs[appID] = true
		}
	}

	// Add all apps that aren't in any tab to Home tab
	for _, app := range config.Apps {
		if !appsInTabs[app.ID] {
			homeTab.AppIDs = append(homeTab.AppIDs, app.ID)
			appsInTabs[app.ID] = true
		}
	}

	// Update Home tab in config
	for i := range config.Tabs {
		if config.Tabs[i].ID == "home" {
			config.Tabs[i] = *homeTab
			break
		}
	}
}

func getTabByID(tabID string) *Tab {
	for i := range config.Tabs {
		if config.Tabs[i].ID == tabID {
			return &config.Tabs[i]
		}
	}
	return nil
}

func getAppByID(appID string) *App {
	for i := range config.Apps {
		if config.Apps[i].ID == appID {
			return &config.Apps[i]
		}
	}
	return nil
}

func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil
}

// expandPath expands home directory shortcuts like ~ and $HOME
func expandPath(path string) string {
	if path == "" {
		return path
	}

	// Expand ~ to home directory
	if strings.HasPrefix(path, "~") {
		homeDir, err := os.UserHomeDir()
		if err == nil {
			if path == "~" {
				return homeDir
			}
			if strings.HasPrefix(path, "~/") {
				return filepath.Join(homeDir, path[2:])
			}
		}
	}

	// Expand environment variables like $HOME
	path = os.ExpandEnv(path)

	return path
}

// centerDialogOnMainWindow positions a dialog window relative to the main window
// to ensure it appears on the same display. Fyne's CenterOnScreen() centers on
// the monitor where the window is currently positioned, so we show the dialog
// first to ensure it's on the same display as the main window, then center it.
func centerDialogOnMainWindow(dialogWindow fyne.Window) {
	if mainWindow == nil {
		dialogWindow.CenterOnScreen()
		return
	}

	// Show the dialog first (it will appear on the same display as the main window)
	// then center it on that display
	dialogWindow.Show()
	dialogWindow.CenterOnScreen()
}

// registerChildWindow adds a window to the tracking list
func registerChildWindow(window fyne.Window) {
	childWindows = append(childWindows, window)
	// Store title before setting close intercept (in case window.Title() becomes unavailable after close)
	title := window.Title()

	// Set close intercept to remove from tracking list when window closes
	window.SetCloseIntercept(func() {
		// Remove from tracking list
		for i, w := range childWindows {
			if w == window {
				childWindows = append(childWindows[:i], childWindows[i+1:]...)
				break
			}
		}
		// Remove from open dialogs map if it's tracked there
		// Use stored title since window.Title() might not work after window is closed
		if _, exists := openDialogs[title]; exists {
			delete(openDialogs, title)
		}
		window.Close()
	})
}

// showOrFocusDialog checks if a dialog with the given title is already open.
// If it exists, brings it to front. Otherwise, returns nil.
func showOrFocusDialog(title string) fyne.Window {
	if existingWindow, exists := openDialogs[title]; exists {
		// Check if window is still valid and visible
		if existingWindow != nil {
			// Check if window content is visible (window is still open)
			if existingWindow.Content() != nil && existingWindow.Content().Visible() {
				existingWindow.Show()
				existingWindow.RequestFocus()
				return existingWindow
			} else {
				// Window was closed but not removed from map, clean it up
				delete(openDialogs, title)
			}
		} else {
			// Window reference is nil, clean it up
			delete(openDialogs, title)
		}
	}
	return nil
}

// registerDialog registers a dialog window by its title to prevent duplicates
func registerDialog(window fyne.Window) {
	title := window.Title()
	openDialogs[title] = window
	registerChildWindow(window)
}

// closeAllChildWindows closes all tracked child windows
func closeAllChildWindows() {
	for _, window := range childWindows {
		if window != nil {
			window.Close()
		}
	}
	childWindows = []fyne.Window{}
}

func refreshTabsUI() {
	// Recreate tabs with current config
	tabItems := []*container.TabItem{}
	for _, t := range config.Tabs {
		if g, ok := appGrids[t.ID]; ok {
			tabItems = append(tabItems, container.NewTabItem(t.Name, g))
		} else {
			// Create grid if it doesn't exist
			grid := createAppGrid(t.ID)
			appGrids[t.ID] = grid
			tabItems = append(tabItems, container.NewTabItem(t.Name, grid))
		}
	}

	// Get current selected tab name
	currentTabName := ""
	if tab := getTabByID(currentTabID); tab != nil {
		currentTabName = tab.Name
	}

	// Recreate tab container
	newTabContainer := container.NewAppTabs(tabItems...)
	newTabContainer.OnSelected = func(tab *container.TabItem) {
		// Find tab ID by name
		for _, t := range config.Tabs {
			if t.Name == tab.Text {
				currentTabID = t.ID
				break
			}
		}
	}

	// Select the same tab by name
	for i, tabItem := range tabItems {
		if tabItem.Text == currentTabName {
			newTabContainer.SelectTabIndex(i)
			break
		}
	}

	tabContainer = newTabContainer

	// Update the content area by recreating the content border
	if toolbar != nil && menuBar != nil {
		// Recreate the content border with new tab container
		content := container.NewBorder(
			toolbar,
			nil,
			nil,
			nil,
			newTabContainer,
		)

		// Update the main window content
		mainWindow.SetContent(container.NewBorder(
			menuBar,
			nil,
			nil,
			nil,
			content,
		))
	} else {
		// Fallback: recreate the UI if references are lost
		setupUI()
	}
}
