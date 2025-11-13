package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
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
	appVersion = "0.1.1" // see FyneApp.toml
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
	helpWindow     fyne.Window            // Help window
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

	_, month, _ := time.Now().Date()

	// Check if we have existing apps - if so, show UI immediately and discover in background
	hasExistingApps := len(config.Apps) > 0
	var loadingWindow fyne.Window

	if !hasExistingApps {
		// No existing apps - show loading window while discovering
		loadingWindow = myApp.NewWindow("KrankyBear LaunchPad")
		if month == time.December {
			loadingWindow.SetIcon(resourceKrankyBearChristmasGrinchPng)
		} else {
			loadingWindow.SetIcon(resourceKrankyBearTrapperRedPlaidPng)
		}
		loadingWindow.Resize(fyne.NewSize(400, 200))
		loadingWindow.CenterOnScreen()
		loadingWindow.SetFixedSize(true)

		// Create loading content
		loadingIcon := canvas.NewImageFromResource(resourceKrankyBearTrapperRedPlaidPng)
		if month == time.December {
			loadingIcon = canvas.NewImageFromResource(resourceKrankyBearChristmasGrinchPng)
		}
		loadingIcon.FillMode = canvas.ImageFillContain
		loadingIcon.SetMinSize(fyne.NewSize(64, 64))

		loadingLabel := widget.NewLabel("Discovering applications... this could take a few seconds\nPlease wait.")
		loadingLabel.Alignment = fyne.TextAlignCenter
		loadingLabel.Wrapping = fyne.TextWrapWord

		loadingContent := container.NewVBox(
			container.NewCenter(loadingIcon),
			loadingLabel,
		)
		loadingWindow.SetContent(container.NewPadded(loadingContent))
		loadingWindow.Show()
		loadingWindow.Canvas().Refresh(loadingContent) // Ensure window is rendered

		// Discover installed applications synchronously (first time)
		discoveredApps, err = DiscoverApps()
		if err != nil {
			fmt.Printf("Error discovering apps: %v\n", err)
			discoveredApps = []App{}
		}

		// Close loading window
		loadingWindow.Close()

		// Merge discovered apps with saved apps (avoid duplicates)
		mergeDiscoveredApps()

		// Auto-add all discovered apps to Home tab if not already in any tab
		autoAddAppsToHomeTab()
	} else {
		// We have existing apps - show UI immediately and discover in background
		// Start background discovery (will update UI when complete)
		go discoverAppsInBackground()
	}

	mainWindow = myApp.NewWindow("KrankyBear LaunchPad")

	// Set icon based on month - Christmas Grinch in December, otherwise Trapper Red Plaid
	if month == time.December {
		mainWindow.SetIcon(resourceKrankyBearChristmasGrinchPng)
	} else {
		mainWindow.SetIcon(resourceKrankyBearTrapperRedPlaidPng)
	}

	// Restore saved window size, or use defaults
	savedWidth := myApp.Preferences().FloatWithFallback("windowWidth", 1000)
	savedHeight := myApp.Preferences().FloatWithFallback("windowHeight", 700)
	mainWindow.Resize(fyne.NewSize(float32(savedWidth), float32(savedHeight)))
	// Note: Fyne does not natively support window position saving/restoration.
	// The Window interface doesn't expose GetPosition() or SetPosition() methods.
	// Window position is always centered on screen at startup.
	mainWindow.CenterOnScreen()
	// Ensure window is resizable (default, but make it explicit)
	mainWindow.SetFixedSize(false)

	// Save window size when it's resized
	// Note: We'll save on close as well, but this helps capture size changes during use
	mainWindow.SetOnClosed(func() {
		// Save window size before closing
		if mainWindow != nil && mainWindow.Canvas() != nil {
			size := mainWindow.Canvas().Size()
			myApp.Preferences().SetFloat("windowWidth", float64(size.Width))
			myApp.Preferences().SetFloat("windowHeight", float64(size.Height))
		}
	})

	// Close all child windows when main window closes
	mainWindow.SetCloseIntercept(func() {
		// Save window size before closing
		if mainWindow != nil && mainWindow.Canvas() != nil {
			size := mainWindow.Canvas().Size()
			myApp.Preferences().SetFloat("windowWidth", float64(size.Width))
			myApp.Preferences().SetFloat("windowHeight", float64(size.Height))
		}

		closeAllChildWindows()
		// Note: System tray cleanup is handled by the Quit menu item.
		// We don't clean it up here to avoid crashes during window close.
		mainWindow.Close()
		myApp.Quit()
	})

	appGrids = make(map[string]fyne.CanvasObject)
	// Restore saved tab, or default to "home"
	currentTabID = myApp.Preferences().StringWithFallback("currentTabID", "home")
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
		tabItems = append(tabItems, createColoredTabItem(tab, grid))
	}

	tabContainer = container.NewAppTabs(tabItems...)

	// Set current tab
	tabContainer.OnSelected = func(tab *container.TabItem) {
		// Find tab ID by name
		for _, t := range config.Tabs {
			if t.Name == tab.Text {
				currentTabID = t.ID
				// Save selected tab to preferences
				myApp.Preferences().SetString("currentTabID", currentTabID)
				break
			}
		}
	}

	// Restore saved tab selection
	if savedTabID := myApp.Preferences().StringWithFallback("currentTabID", ""); savedTabID != "" {
		// Find the tab by ID and select it
		for i, tab := range config.Tabs {
			if tab.ID == savedTabID {
				// Find the corresponding tab item in tabContainer
				if i < len(tabContainer.Items) {
					tabContainer.SelectTabIndex(i)
					currentTabID = savedTabID
				}
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
		menuItems["Help"],
		fyne.NewMenuItemSeparator(),
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
		"Help": fyne.NewMenuItem("Help", func() {
			showHelpDialog()
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
			menuItems["Help"],
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

// compareVersions compares two version strings (e.g., "0.1.0", "0.1.1")
// Returns: -1 if v1 < v2, 0 if v1 == v2, 1 if v1 > v2
func compareVersions(v1, v2 string) int {
	// Remove 'v' prefix if present
	v1 = strings.TrimPrefix(v1, "v")
	v2 = strings.TrimPrefix(v2, "v")

	// Split versions into parts
	parts1 := strings.Split(v1, ".")
	parts2 := strings.Split(v2, ".")

	// Get maximum length
	maxLen := len(parts1)
	if len(parts2) > maxLen {
		maxLen = len(parts2)
	}

	// Compare each part
	for i := 0; i < maxLen; i++ {
		var part1, part2 int
		if i < len(parts1) {
			part1, _ = strconv.Atoi(parts1[i])
		}
		if i < len(parts2) {
			part2, _ = strconv.Atoi(parts2[i])
		}

		if part1 < part2 {
			return -1
		}
		if part1 > part2 {
			return 1
		}
	}

	return 0
}

// checkForUpdate checks for updates from GitHub releases
func checkForUpdate() {
	// Check if update window is already open using dialog tracking system
	updateWindowTitle := appName + ": Update Check"
	if existingWindow := showOrFocusDialog(updateWindowTitle); existingWindow != nil {
		// Update updateWindow reference in case it was cleaned up
		updateWindow = existingWindow
		return
	}

	// Show checking dialog
	checkingDialog := dialog.NewInformation("Checking for Updates", "Checking for updates...", mainWindow)
	checkingDialog.Show()

	// Run update check in goroutine to avoid blocking UI
	go func() {
		// GitHub API URL for releases (using public API, no auth needed)
		apiURL := "https://api.github.com/repos/amarillier/KrankyBearLaunchPad/releases/latest"

		client := &http.Client{
			Timeout: 10 * time.Second,
		}

		resp, err := client.Get(apiURL)
		if err != nil {
			fyne.Do(func() {
				checkingDialog.Hide()
				dialog.ShowError(fmt.Errorf("Failed to check for updates: %v", err), mainWindow)
			})
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			fyne.Do(func() {
				checkingDialog.Hide()
				dialog.ShowError(fmt.Errorf("Failed to check for updates: HTTP %d", resp.StatusCode), mainWindow)
			})
			return
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			fyne.Do(func() {
				checkingDialog.Hide()
				dialog.ShowError(fmt.Errorf("Failed to read update information: %v", err), mainWindow)
			})
			return
		}

		var release GitHubRelease
		if err := json.Unmarshal(body, &release); err != nil {
			fyne.Do(func() {
				checkingDialog.Hide()
				dialog.ShowError(fmt.Errorf("Failed to parse update information: %v", err), mainWindow)
			})
			return
		}

		// Compare versions (remove 'v' prefix if present)
		latestVersion := strings.TrimPrefix(release.TagName, "v")
		currentVersion := strings.TrimPrefix(appVersion, "v")

		// Compare versions to determine which is newer
		comparison := compareVersions(currentVersion, latestVersion)

		// Format message similar to KrankyBearClock
		var message string
		var updateAvailable bool

		if comparison > 0 {
			// Current version is newer than released version
			message = fmt.Sprintf("You are running a newer version of %s.\n\nCurrent version: %s\nLatest released version: %s",
				appName, currentVersion, latestVersion)
			updateAvailable = false
		} else if comparison < 0 {
			// Current version is older than released version
			message = fmt.Sprintf("A newer version is available!\n\nCurrent version: %s\nLatest version: %s\n\n%s",
				currentVersion, latestVersion, release.Body)
			updateAvailable = true
		} else {
			// Versions are the same
			message = fmt.Sprintf("You are running the latest version.\n\nCurrent version: %s\nLatest version: %s",
				currentVersion, latestVersion)
			updateAvailable = false
		}

		// Hide checking dialog and show update alert window on main thread
		fyne.Do(func() {
			checkingDialog.Hide()
			showUpdateAlert(message, release.URL, updateAvailable)
		})
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

	releasenoteslink, rnerr := url.Parse("https://github.com/amarillier/KrankyBearLaunchPad/blob/allanm/ReleaseNotes.txt")
	if rnerr != nil {
		fyne.LogError("Could not parse URL", rnerr)
	}
	myreleasenoteslink := widget.NewHyperlink("https://github.com/amarillier/KrankyBearLaunchPad/blob/allanm/ReleaseNotes.txt", releasenoteslink)
	myreleasenoteslink.Alignment = fyne.TextAlignLeading

	// Create image - use HardHat if running newer version, Christmas Grinch in December, otherwise Trapper Red Plaid
	var kbimg *canvas.Image
	_, month, _ := time.Now().Date()
	if strings.Contains(updtmsg, "running a newer version") {
		kbimg = canvas.NewImageFromResource(resourceKrankyBearHardHatPng)
	} else if month == time.December {
		kbimg = canvas.NewImageFromResource(resourceKrankyBearChristmasGrinchPng)
	} else {
		kbimg = canvas.NewImageFromResource(resourceKrankyBearTrapperRedPlaidPng)
	}
	kbimg.FillMode = canvas.ImageFillOriginal

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
		kbimg,
		text,
		myreleaselink,
		myreleasenoteslink,
		openBtn,
	)

	// Create or update window
	updateWindowTitle := appName + ": Update Check"
	if updateWindow == nil {
		updateWindow = myApp.NewWindow(updateWindowTitle)
		// Set icon - use HardHat if running newer version, Christmas Grinch in December, otherwise Trapper Red Plaid
		if strings.Contains(updtmsg, "running a newer version") {
			updateWindow.SetIcon(resourceKrankyBearHardHatPng)
		} else if month == time.December {
			updateWindow.SetIcon(resourceKrankyBearChristmasGrinchPng)
		} else {
			updateWindow.SetIcon(resourceKrankyBearTrapperRedPlaidPng)
		}
		updateWindow.Resize(fyne.NewSize(500, 300))

		// Register dialog for tracking
		registerDialog(updateWindow)

		// Override close intercept to ensure proper cleanup
		// This needs to be after registerDialog because it sets up its own intercept
		updateWindow.SetCloseIntercept(func() {
			// Store reference and title before cleanup
			windowToClose := updateWindow
			title := windowToClose.Title()

			// Remove from tracking list
			for i, w := range childWindows {
				if w == windowToClose {
					childWindows = append(childWindows[:i], childWindows[i+1:]...)
					break
				}
			}
			// Remove from open dialogs map
			if _, exists := openDialogs[title]; exists {
				delete(openDialogs, title)
			}
			// Clear updateWindow variable
			updateWindow = nil
			// Close the window
			windowToClose.Close()
		})
	} else {
		// Update icon if window already exists
		if strings.Contains(updtmsg, "running a newer version") {
			updateWindow.SetIcon(resourceKrankyBearHardHatPng)
		} else if month == time.December {
			updateWindow.SetIcon(resourceKrankyBearChristmasGrinchPng)
		} else {
			updateWindow.SetIcon(resourceKrankyBearTrapperRedPlaidPng)
		}
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
		// Only try formats that Fyne supports
		if isFyneSupportedImageFormat(a.app.Icon) {
			resource, err := fyne.LoadResourceFromPath(a.app.Icon)
			if err == nil {
				iconResource = resource
			}
			// Silently skip if load fails - file might be corrupted or invalid
		}
	}

	// If no icon found, try to detect from executable path
	if iconResource == nil {
		iconResource = detectIconFromExecutable(a.app.Executable)
		// If we detected an icon, save the path to config (but only if it's not .icns)
		if iconResource != nil && a.app.Icon == "" {
			// Find the icon path that was detected
			if detectedIconPath := findIconPathFromExecutable(a.app.Executable); detectedIconPath != "" {
				// Only save if it's a format Fyne can load
				if isFyneSupportedImageFormat(detectedIconPath) {
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

	// Fallback to generic application icon if we couldn't load one
	// Users can customize this icon using the "Change Icon" option in the context menu
	if iconResource == nil {
		// Use DocumentIcon as a generic application placeholder (more appropriate than FolderIcon)
		iconResource = theme.DocumentIcon()
		if iconResource == nil {
			// Fallback to FileIcon if DocumentIcon not available
			iconResource = theme.FileIcon()
			if iconResource == nil {
				// Final fallback to FolderIcon
				iconResource = theme.FolderIcon()
			}
		}
	}

	// Create image and resize to 64x64
	img := canvas.NewImageFromResource(iconResource)
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(fyne.NewSize(64, 64))
	img.Resize(fyne.NewSize(64, 64))

	return img
}

// findIconPathFromExecutable finds the icon file path (not resource) for saving to config
// Only returns paths for formats that Fyne can load (PNG, JPG, BMP)
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
			// Only return formats that Fyne supports
			for _, entry := range entries {
				name := entry.Name()
				if isFyneSupportedImageFormat(name) {
					return filepath.Join(resourcesDir, name)
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
				name := entry.Name()
				// Only return formats that Fyne supports
				if isFyneSupportedImageFormat(name) &&
					strings.Contains(strings.ToLower(name), strings.ToLower(exeName)) {
					return filepath.Join(dir, name)
				}
			}
		}
	}

	// For Linux, try common image formats in same directory
	dir := filepath.Dir(executable)
	if entries, err := os.ReadDir(dir); err == nil {
		exeName := filepath.Base(executable)
		for _, entry := range entries {
			name := entry.Name()
			// Only return formats that Fyne supports
			if isFyneSupportedImageFormat(name) {
				// Try to match by name
				baseName := strings.TrimSuffix(strings.ToLower(exeName), filepath.Ext(exeName))
				iconBaseName := strings.TrimSuffix(strings.ToLower(name), filepath.Ext(name))
				if strings.Contains(iconBaseName, baseName) || strings.Contains(baseName, iconBaseName) {
					return filepath.Join(dir, name)
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
		// Only try formats that Fyne supports
		if isFyneSupportedImageFormat(a.app.Icon) {
			resource, err := fyne.LoadResourceFromPath(a.app.Icon)
			if err == nil {
				iconResource = resource
			}
			// Silently skip if load fails
		}
	}

	// If no icon found, try to detect from executable path
	if iconResource == nil {
		iconResource = detectIconFromExecutable(a.app.Executable)
	}

	// Fallback to generic application icon if we couldn't load one
	// Users can customize this icon using the "Change Icon" option in the context menu
	if iconResource == nil {
		iconResource = getGenericAppIcon()
	}

	return iconResource
}

// getGenericAppIcon returns a generic application icon as a fallback
// Users can customize icons using the "Change Icon" option in the context menu
func getGenericAppIcon() fyne.Resource {
	// Try DocumentIcon first (most appropriate for applications)
	if icon := theme.DocumentIcon(); icon != nil {
		return icon
	}
	// Fallback to FileIcon
	if icon := theme.FileIcon(); icon != nil {
		return icon
	}
	// Final fallback to FolderIcon
	return theme.FolderIcon()
}

// isFyneSupportedImageFormat checks if a file format is supported by Fyne
func isFyneSupportedImageFormat(filename string) bool {
	name := strings.ToLower(filename)
	// Fyne supports: PNG, JPG/JPEG, BMP
	// Fyne does NOT support: ICO, ICNS, SVG (on most platforms)
	return strings.HasSuffix(name, ".png") ||
		strings.HasSuffix(name, ".jpg") ||
		strings.HasSuffix(name, ".jpeg") ||
		strings.HasSuffix(name, ".bmp")
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
			// Only try formats that Fyne supports
			for _, entry := range entries {
				name := entry.Name()
				if isFyneSupportedImageFormat(name) {
					iconPath := filepath.Join(resourcesDir, name)
					if resource, err := fyne.LoadResourceFromPath(iconPath); err == nil {
						return resource
					}
					// Silently skip if load fails - file might be corrupted or invalid
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
				name := entry.Name()
				// Only try formats that Fyne supports
				if isFyneSupportedImageFormat(name) &&
					strings.Contains(strings.ToLower(name), strings.ToLower(exeName)) {
					iconPath := filepath.Join(dir, name)
					if resource, err := fyne.LoadResourceFromPath(iconPath); err == nil {
						return resource
					}
					// Silently skip if load fails
				}
			}
		}
	}

	// For Linux, try common image formats in same directory
	dir := filepath.Dir(executable)
	if entries, err := os.ReadDir(dir); err == nil {
		exeName := filepath.Base(executable)
		for _, entry := range entries {
			name := entry.Name()
			// Only try formats that Fyne supports (skip SVG and ICO)
			if isFyneSupportedImageFormat(name) {
				// Try to match by name
				baseName := strings.TrimSuffix(strings.ToLower(exeName), filepath.Ext(exeName))
				iconBaseName := strings.TrimSuffix(strings.ToLower(name), filepath.Ext(name))
				if strings.Contains(iconBaseName, baseName) || strings.Contains(baseName, iconBaseName) {
					iconPath := filepath.Join(dir, name)
					if resource, err := fyne.LoadResourceFromPath(iconPath); err == nil {
						return resource
					}
					// Silently skip if load fails
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

// createColoredTabItem creates a TabItem with an optional colored icon indicator
func createColoredTabItem(tab Tab, content fyne.CanvasObject) *container.TabItem {
	if tab.Color == "" {
		// No color specified, create regular tab
		return container.NewTabItem(tab.Name, content)
	}

	// Parse hex color
	tabColor, err := parseHexColor(tab.Color)
	if err != nil {
		fyne.LogError("Failed to parse tab color", err)
		return container.NewTabItem(tab.Name, content)
	}

	// Create a colored icon resource
	coloredIcon := createColoredIconResource(tabColor)
	if coloredIcon == nil {
		// If icon creation fails, fall back to regular tab
		fyne.LogError("Failed to create colored icon, using regular tab", nil)
		return container.NewTabItem(tab.Name, content)
	}

	return container.NewTabItemWithIcon(tab.Name, coloredIcon, content)
}

// parseHexColor parses a hex color string (e.g., "#FF0000" or "#FF0000FF")
func parseHexColor(hex string) (color.Color, error) {
	hex = strings.TrimPrefix(hex, "#")

	var r, g, b, a uint8 = 0, 0, 0, 255

	if len(hex) == 6 {
		// RGB format
		val, err := strconv.ParseUint(hex, 16, 32)
		if err != nil {
			return nil, err
		}
		r = uint8((val >> 16) & 0xFF)
		g = uint8((val >> 8) & 0xFF)
		b = uint8(val & 0xFF)
	} else if len(hex) == 8 {
		// RGBA format
		val, err := strconv.ParseUint(hex, 16, 32)
		if err != nil {
			return nil, err
		}
		r = uint8((val >> 24) & 0xFF)
		g = uint8((val >> 16) & 0xFF)
		b = uint8((val >> 8) & 0xFF)
		a = uint8(val & 0xFF)
	} else {
		return nil, fmt.Errorf("invalid hex color format: %s", hex)
	}

	return color.NRGBA{R: r, G: g, B: b, A: a}, nil
}

// colorToHex converts a color.Color to hex string format
func colorToHex(c color.Color) string {
	nrgba := color.NRGBAModel.Convert(c).(color.NRGBA)
	return fmt.Sprintf("#%02X%02X%02X", nrgba.R, nrgba.G, nrgba.B)
}

// createColoredIconResource creates a simple colored icon resource
func createColoredIconResource(c color.Color) fyne.Resource {
	// Create a 16x16 colored PNG image
	imgData := createColoredImage(c, 16, 16)
	if imgData == nil || len(imgData) == 0 {
		fyne.LogError("Failed to create colored image", nil)
		return nil
	}

	// Generate a unique resource name from the color hex value
	// Include .png extension so Fyne recognizes it as a PNG image
	nrgba := color.NRGBAModel.Convert(c).(color.NRGBA)
	resourceName := fmt.Sprintf("tab_color_%02X%02X%02X%02X.png", nrgba.R, nrgba.G, nrgba.B, nrgba.A)

	return fyne.NewStaticResource(resourceName, imgData)
}

// createColoredImage creates PNG image data for a solid color
func createColoredImage(c color.Color, width, height int) []byte {
	// Convert color to NRGBA
	nrgba := color.NRGBAModel.Convert(c).(color.NRGBA)

	// Create a new NRGBA image
	img := image.NewNRGBA(image.Rect(0, 0, width, height))

	// Fill the image with the color
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, nrgba)
		}
	}

	// Encode as PNG
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		fyne.LogError("Failed to encode PNG", err)
		return nil
	}

	pngData := buf.Bytes()
	if len(pngData) == 0 {
		fyne.LogError("PNG encoding produced empty data", nil)
		return nil
	}

	// Verify it's valid PNG by checking the signature
	if len(pngData) < 8 || string(pngData[0:8]) != "\x89PNG\r\n\x1a\n" {
		fyne.LogError("Invalid PNG signature", nil)
		return nil
	}

	return pngData
}

func showCreateTabDialog() {
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("Enter tab name (e.g., Productivity)")
	nameEntry.Wrapping = fyne.TextWrapOff

	// Color selection
	var selectedColorHex string = ""
	colorPreview := canvas.NewRectangle(color.Transparent)
	colorPreview.SetMinSize(fyne.NewSize(40, 30))
	colorPreview.StrokeColor = theme.ForegroundColor()
	colorPreview.StrokeWidth = 1

	// Check if dialog is already open
	if existingWindow := showOrFocusDialog("Create New Tab"); existingWindow != nil {
		return
	}

	// Create dialog window with custom size (wider for 32 characters)
	dialogWindow := myApp.NewWindow("Create New Tab")
	dialogWindow.Resize(fyne.NewSize(500, 200))
	registerDialog(dialogWindow)

	colorBtn := widget.NewButton("Choose Color", func() {
		dialog.ShowColorPicker("Select Tab Color", "Choose a color for this tab", func(c color.Color) {
			selectedColorHex = colorToHex(c)
			colorPreview.FillColor = c
			colorPreview.Refresh()
		}, dialogWindow)
	})

	clearColorBtn := widget.NewButton("Clear", func() {
		selectedColorHex = ""
		colorPreview.FillColor = color.Transparent
		colorPreview.Refresh()
	})

	colorRow := container.NewHBox(
		widget.NewLabel("Tab Color:"),
		colorPreview,
		colorBtn,
		clearColorBtn,
	)

	// Create a custom dialog with wider entry field (32 characters width)
	formContent := container.NewVBox(
		widget.NewLabel("Tab Name:"),
		container.NewPadded(nameEntry),
		colorRow,
	)

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
			Color:  selectedColorHex,
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
		}
	})

	cancelBtn := widget.NewButton("Cancel", func() {
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

	// Color selection
	var selectedColor color.Color = nil
	var selectedColorHex string = tab.Color

	// Parse existing color if present
	if tab.Color != "" {
		if c, err := parseHexColor(tab.Color); err == nil {
			selectedColor = c
		}
	}

	colorPreview := canvas.NewRectangle(color.Transparent)
	if selectedColor != nil {
		colorPreview.FillColor = selectedColor
	}
	colorPreview.SetMinSize(fyne.NewSize(40, 30))
	colorPreview.StrokeColor = theme.ForegroundColor()
	colorPreview.StrokeWidth = 1

	// Check if dialog is already open
	if existingWindow := showOrFocusDialog("Edit Tab"); existingWindow != nil {
		return
	}

	// Create dialog window with custom size (wider for 32 characters)
	dialogWindow := myApp.NewWindow("Edit Tab")
	dialogWindow.Resize(fyne.NewSize(500, 200))
	registerDialog(dialogWindow)

	colorBtn := widget.NewButton("Choose Color", func() {
		initialColor := selectedColor
		if initialColor == nil {
			initialColor = theme.PrimaryColor()
		}
		colorPicker := dialog.NewColorPicker("Select Tab Color", "Choose a color for this tab", func(c color.Color) {
			selectedColor = c
			selectedColorHex = colorToHex(c)
			colorPreview.FillColor = c
			colorPreview.Refresh()
		}, dialogWindow)
		if initialColor != nil {
			colorPicker.SetColor(initialColor)
		}
		colorPicker.Show()
	})

	clearColorBtn := widget.NewButton("Clear", func() {
		selectedColor = nil
		selectedColorHex = ""
		colorPreview.FillColor = color.Transparent
		colorPreview.Refresh()
	})

	colorRow := container.NewHBox(
		widget.NewLabel("Tab Color:"),
		colorPreview,
		colorBtn,
		clearColorBtn,
	)

	// Create a custom dialog with wider entry field (32 characters width)
	formContent := container.NewVBox(
		widget.NewLabel("Tab Name:"),
		container.NewPadded(nameEntry),
		colorRow,
	)

	saveBtn := widget.NewButton("Save", func() {
		newName := strings.TrimSpace(nameEntry.Text)
		if newName == "" {
			dialog.ShowError(fmt.Errorf("Tab name cannot be empty"), dialogWindow)
			return
		}

		// Update tab name and color
		for i := range config.Tabs {
			if config.Tabs[i].ID == currentTabID {
				config.Tabs[i].Name = newName
				config.Tabs[i].Color = selectedColorHex
				break
			}
		}

		// Refresh tabs UI
		refreshTabsUI()

		// Save config
		if err := SaveConfig(config, configPath); err != nil {
			dialog.ShowError(fmt.Errorf("Failed to save config: %v", err), dialogWindow)
		} else {
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
		}
	})

	cancelBtn := widget.NewButton("Cancel", func() {
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
					// Refresh Manage Apps dialog if it's open to update the filter
					refreshManageAppsDialog()
					// Show success dialog that auto-closes after 5 seconds
					showAutoCloseSuccessDialog(fmt.Sprintf("Deleted '%s'", app.Name), parentWindow)
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
				widget.NewButton("Add to Tab", nil),
				widget.NewButton("Remove from Tab", nil),
				widget.NewButton("", nil), // Delete button for custom apps
				widget.NewLabel(""),       // Application name on the right
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id >= len(filteredApps) {
				return
			}
			app := filteredApps[id]
			container := obj.(*fyne.Container)
			addBtn := container.Objects[0].(*widget.Button)
			removeBtn := container.Objects[1].(*widget.Button)
			deleteBtn := container.Objects[2].(*widget.Button)
			label := container.Objects[3].(*widget.Label)

			labelText := app.Name
			if app.IsCustom {
				labelText += " (Custom)"
			}
			label.SetText(labelText)

			addBtn.SetText("Add to Tab")
			addBtn.OnTapped = func() {
				// If current tab is not "home", add directly to current tab
				if currentTabID != "home" {
					// Check if app is already in tab - check directly against config.Tabs
					alreadyAdded := false
					for i := range config.Tabs {
						if config.Tabs[i].ID == currentTabID {
							for _, appID := range config.Tabs[i].AppIDs {
								// Only consider it already added if the ID matches AND the app actually exists
								if appID == app.ID {
									// Verify the app actually exists in config.Apps and matches this app
									existingApp := getAppByID(appID)
									if existingApp != nil && existingApp.ID == app.ID {
										alreadyAdded = true
										break
									}
									// If app doesn't exist or doesn't match, it's a stale ID - we can ignore it
								}
							}
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
				} else {
					// Current tab is "home", show dialog to select which tab
					showAddAppToTabDialog(app)
				}
			}

			// Remove from current tab button (works for all apps)
			removeBtn.SetText("Remove from Tab")
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
			// Reopen the dialog immediately (on the main thread, since we're called from a button handler)
			showManageAppsDialog()
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
				// Only consider it already added if the ID matches AND the app actually exists
				if appID == app.ID {
					// Verify the app actually exists in config.Apps
					if getAppByID(appID) != nil {
						alreadyAdded = true
						break
					}
					// If app doesn't exist, it's a stale ID - we can ignore it
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
					// Check if app is already in tab - verify app exists and matches
					alreadyAdded := false
					for _, existingID := range config.Tabs[i].AppIDs {
						if existingID == app.ID {
							// Verify the app actually exists in config.Apps and matches this app
							existingApp := getAppByID(existingID)
							if existingApp != nil && existingApp.ID == app.ID {
								alreadyAdded = true
								break
							}
							// If app doesn't exist or doesn't match, it's a stale ID - we can ignore it
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
				// Only consider it already added if the ID matches AND the app actually exists
				if appID == app.ID {
					// Verify the app actually exists in config.Apps
					if getAppByID(appID) != nil {
						alreadyAdded = true
						break
					}
					// If app doesn't exist, it's a stale ID - we can ignore it
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
					// Check if app is already in tab - verify app exists and matches
					alreadyAdded := false
					for _, existingID := range config.Tabs[i].AppIDs {
						if existingID == app.ID {
							// Verify the app actually exists in config.Apps and matches this app
							existingApp := getAppByID(existingID)
							if existingApp != nil && existingApp.ID == app.ID {
								alreadyAdded = true
								break
							}
							// If app doesn't exist or doesn't match, it's a stale ID - we can ignore it
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

		// Add custom app to Home tab if not already there
		homeTab := getTabByID("home")
		if homeTab != nil {
			// Check if app is already in home tab
			alreadyInHome := false
			for _, appID := range homeTab.AppIDs {
				if appID == customApp.ID {
					alreadyInHome = true
					break
				}
			}
			if !alreadyInHome {
				// Add to home tab
				for i := range config.Tabs {
					if config.Tabs[i].ID == "home" {
						config.Tabs[i].AppIDs = append(config.Tabs[i].AppIDs, customApp.ID)
						// Refresh the home tab grid
						refreshGridWithSortedApps("home")
						break
					}
				}
			}
		}

		// Save config
		if err := SaveConfig(config, configPath); err != nil {
			dialog.ShowError(fmt.Errorf("Failed to save config: %v", err), dialogWindow)
		} else {
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
			// Refresh Manage Apps dialog if it's open
			refreshManageAppsDialog()
			// Show success dialog that auto-closes after 5 seconds
			showAutoCloseSuccessDialog(fmt.Sprintf("Added '%s' to applications", name), mainWindow)
		}
	})

	cancelBtn := widget.NewButton("Cancel", func() {
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

	// Override close intercept to ensure proper cleanup
	// This needs to be after registerDialog because it sets up its own intercept
	dialogWindow.SetCloseIntercept(func() {
		// Store reference and title before cleanup
		windowToClose := dialogWindow
		title := windowToClose.Title()

		// Remove from tracking list
		for i, w := range childWindows {
			if w == windowToClose {
				childWindows = append(childWindows[:i], childWindows[i+1:]...)
				break
			}
		}
		// Remove from open dialogs map
		if _, exists := openDialogs[title]; exists {
			delete(openDialogs, title)
		}
		// Close the window
		windowToClose.Close()
	})

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
			// Explicitly clean up tracking before closing
			title := dialogWindow.Title()
			// Remove from child windows
			for i, w := range childWindows {
				if w == dialogWindow {
					childWindows = append(childWindows[:i], childWindows[i+1:]...)
					break
				}
			}
			// Remove from open dialogs map
			if _, exists := openDialogs[title]; exists {
				delete(openDialogs, title)
			}
			dialogWindow.Close()
		}
	})

	cancelBtn := widget.NewButton("Cancel", func() {
		// Explicitly clean up tracking before closing
		title := dialogWindow.Title()
		// Remove from child windows
		for i, w := range childWindows {
			if w == dialogWindow {
				childWindows = append(childWindows[:i], childWindows[i+1:]...)
				break
			}
		}
		// Remove from open dialogs map
		if _, exists := openDialogs[title]; exists {
			delete(openDialogs, title)
		}
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
	iconPreview := canvas.NewImageFromResource(getGenericAppIcon())
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
				iconPreview.Resource = getGenericAppIcon()
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

	// Initialize preview with fallback to generic icon
	if app.Icon != "" && fileExists(app.Icon) {
		if isFyneSupportedImageFormat(app.Icon) {
			if resource, err := fyne.LoadResourceFromPath(app.Icon); err == nil {
				iconPreview.Resource = resource
			} else {
				// Fallback to generic icon if load fails
				iconPreview.Resource = getGenericAppIcon()
			}
		} else {
			// Unsupported format, use generic icon
			iconPreview.Resource = getGenericAppIcon()
		}
	} else if detectedIcon := detectIconFromExecutable(app.Executable); detectedIcon != nil {
		iconPreview.Resource = detectedIcon
	} else {
		// No icon found, use generic placeholder
		iconPreview.Resource = getGenericAppIcon()
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

// showAutoCloseSuccessDialog shows a success dialog that automatically closes after 5 seconds
func showAutoCloseSuccessDialog(message string, parent fyne.Window) {
	successWindow := myApp.NewWindow("Success")
	successWindow.Resize(fyne.NewSize(400, 150))

	label := widget.NewLabel(message)
	label.Alignment = fyne.TextAlignCenter
	label.Wrapping = fyne.TextWrapWord

	okBtn := widget.NewButton("OK", func() {
		successWindow.Close()
	})

	content := container.NewVBox(
		label,
		container.NewCenter(okBtn),
	)

	successWindow.SetContent(content)
	// Use centerDialogOnMainWindow to ensure it appears on the same display as the main window
	centerDialogOnMainWindow(successWindow)

	// Auto-close after 5 seconds
	go func() {
		time.Sleep(5 * time.Second)
		// Check if window is still open before closing
		if successWindow.Content() != nil && successWindow.Content().Visible() {
			successWindow.Close()
		}
	}()
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

func showHelpDialog() {
	// Check if help window is already open using the dialog tracking system
	if existingWindow := showOrFocusDialog(appName + ": Help"); existingWindow != nil {
		// Update helpWindow reference in case it was cleaned up
		helpWindow = existingWindow
		return
	}

	helpWindow = myApp.NewWindow(appName + ": Help")

	// Set icon based on month - Christmas Grinch in December, otherwise Trapper Red Plaid
	_, month, _ := time.Now().Date()
	if month == time.December {
		helpWindow.SetIcon(resourceKrankyBearChristmasGrinchPng)
	} else {
		helpWindow.SetIcon(resourceKrankyBearTrapperRedPlaidPng)
	}

	hlpText := `KrankyBear LaunchPad is a cross-platform application launcher similar to macOS LaunchPad.

FEATURES:

- Cross-platform support: Works on macOS, Linux, and Windows
- Automatic application discovery from package managers:
  • macOS: Homebrew (Casks)
  • Windows: Chocolatey, Winget, Scoop
  • Linux: APT, RPM/YUM/DNF, Zypper, Snap
- Tab organization: Create custom tabs to organize applications
  • Create, edit, delete, and sort tabs
  • Home tab automatically includes all discovered apps
  • Tab selection is remembered between sessions
- Custom applications: Add manually installed applications with custom icons
- Theme support: Light and dark themes with preference persistence
- System tray integration: Run in background with system tray menu
- Update checker: Check for updates from GitHub releases
- Window state memory: Remembers window size and selected tab between sessions
- App management:
  • Add apps to multiple tabs
  • Edit app properties (name, executable, icon)
  • Remove apps from tabs
  • Filter and search applications

USAGE:

- Click on any app icon to launch the application
- Use "Manage Apps" to add, edit, or remove applications
- Create new tabs using "+ New Tab" button
- Use "Edit Tab" to rename tabs or manage apps within tabs
- Use "Sort Tabs" to reorder tabs (Home tab cannot be moved)
- Theme can be changed in Settings > Theme Settings
- Check for updates in Help > Check for Update

CONFIGURATION:

Configuration is stored in ~/.krankybear-launchpad/config.json and includes:
- List of discovered and custom applications
- Tab definitions and app assignments
- Preferences (theme, window size, selected tab) are stored separately

Default settings will be created on first run if they don't exist.
`

	hlpText += "\n" + appName + " v " + appVersion
	hlpText += "\n" + appCopyright
	hlpText += "\n\n" + appAuthor + ", using Go and fyne GUI"

	plnText := `PLANNED UPDATES:

- Window position saving/restoration (currently limited by Fyne framework)
- Additional package manager support
- App icon auto-detection improvements
- Keyboard shortcuts for common operations
- Drag and drop app organization
- Export/import configuration
- App categories/tags
- Recent apps tracking
- Favorites/starred apps
`

	bugText := `KNOWN ISSUES:

- Window position cannot be saved/restored (Fyne framework limitation)
  • Window always opens centered on screen
  • Window size is remembered correctly
- Some discovered apps may not have icons detected automatically
  • Custom icons can be added manually via "Manage Apps"
- On some Linux distributions, app discovery may be slower
- System tray menu may not be available on all platforms
`

	settingsText := `SETTINGS INFORMATION:

Preferences are stored automatically and include:

- Theme preference (light/dark/default)
- Window size (width and height)
- Selected tab ID
- Application configuration (stored in config.json)

THEME SETTINGS:
- Light Theme: Light background with dark text
- Dark Theme: Dark background with light text
- Default Theme: Uses system theme

WINDOW SETTINGS:
- Window size is automatically saved when the window is closed
- Window position cannot be saved (Fyne framework limitation)
- Selected tab is saved when you switch tabs

APPLICATION CONFIGURATION:
- Stored in ~/.krankybear-launchpad/config.json
- Contains all tabs, apps, and their relationships
- Can be manually edited if needed (backup recommended)
- Default configuration created on first run
`

	licText := `KrankyBear LaunchPad is FREE Software as defined in the license agreement below.

This application is "FREE Software".

This application is intended for any use by any individual, in any organization.

This application provides no guarantees as to stability of operations or suitability 
for any purpose, but every attempt has been made to make this application reliable.

This application may not be sold, no money may be asked by anyone for provision of, or any services related to this application.

Using this application (and reading this text) is considered acceptance of
the terms of the License Agreement, and acknowledgement that this is FREE
Software and the additional terms above.

See https://github.com/amarillier/KrankyBearLaunchPad/
`

	licenseLink, err := url.Parse("https://github.com/amarillier/KrankyBearLaunchPad/blob/main/LICENSE")
	if err != nil {
		fyne.LogError("Could not parse URL", err)
	}
	hyperlink := widget.NewHyperlink("https://github.com/amarillier/KrankyBearLaunchPad/blob/main/LICENSE", licenseLink)
	hyperlink.Alignment = fyne.TextAlignLeading

	helpLabel := widget.NewLabel(hlpText)
	helpLabel.Wrapping = fyne.TextWrapWord

	plannedLabel := widget.NewLabel(plnText)
	plannedLabel.Wrapping = fyne.TextWrapWord

	bugsLabel := widget.NewLabel(bugText)
	bugsLabel.Wrapping = fyne.TextWrapWord

	settingsLabel := widget.NewLabel(settingsText)
	settingsLabel.Wrapping = fyne.TextWrapWord

	licLabel := widget.NewLabel(licText)
	licLabel.Wrapping = fyne.TextWrapWord

	tabs := container.NewDocTabs(
		container.NewTabItem("Help", container.NewScroll(helpLabel)),
		container.NewTabItem("Known Issues", container.NewScroll(bugsLabel)),
		container.NewTabItem("Planned Updates", container.NewScroll(plannedLabel)),
		container.NewTabItem("Settings Info", container.NewScroll(settingsLabel)),
		container.NewTabItem("License", container.NewVBox(
			container.NewScroll(licLabel),
			hyperlink,
		)),
	)
	tabs.SetTabLocation(container.TabLocationTop)

	helpWindow.Resize(fyne.NewSize(800, 500))
	helpWindow.SetContent(tabs)
	registerDialog(helpWindow)

	// Set up close intercept to also clear helpWindow variable
	// This needs to be after registerDialog because it will override the one set up by registerChildWindow
	helpWindow.SetCloseIntercept(func() {
		// Store reference to window before cleanup
		windowToClose := helpWindow
		// Get title before cleanup
		title := windowToClose.Title()

		// Remove from tracking list
		for i, w := range childWindows {
			if w == windowToClose {
				childWindows = append(childWindows[:i], childWindows[i+1:]...)
				break
			}
		}
		// Remove from open dialogs map
		if _, exists := openDialogs[title]; exists {
			delete(openDialogs, title)
		}
		// Clear helpWindow variable
		helpWindow = nil
		// Close the window
		windowToClose.Close()
	})

	centerDialogOnMainWindow(helpWindow)
	helpWindow.Show()
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

// discoverAppsInBackground discovers apps in the background and updates the UI when complete
func discoverAppsInBackground() {
	apps, err := DiscoverApps()
	if err != nil {
		fmt.Printf("Error discovering apps: %v\n", err)
		return
	}

	// Update discoveredApps and merge (safe to do from goroutine - no UI calls)
	discoveredApps = apps
	mergeDiscoveredApps()

	// Auto-add newly discovered apps to Home tab (safe - no UI calls)
	autoAddAppsToHomeTab()

	// Refresh UI on main thread using fyne.Do
	if mainWindow != nil {
		fyne.Do(func() {
			refreshTabsUI()
			// Refresh the main window content to show new apps
			if mainWindow.Content() != nil {
				mainWindow.Content().Refresh()
			}
		})
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
			tabItems = append(tabItems, createColoredTabItem(t, g))
		} else {
			// Create grid if it doesn't exist
			grid := createAppGrid(t.ID)
			appGrids[t.ID] = grid
			tabItems = append(tabItems, createColoredTabItem(t, grid))
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
				// Save selected tab to preferences
				myApp.Preferences().SetString("currentTabID", currentTabID)
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
