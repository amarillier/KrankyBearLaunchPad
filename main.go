package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/jackmordaunt/icns/v3"
)

const (
	// appName    = "KrankyBear LaunchPad"
	appVersion = "0.1.3" // see FyneApp.toml
	appAuthor  = "Allan Marillier"
)

var appName = "KrankyBear LaunchPad"
var appCopyright = "Copyright (c) Allan Marillier, 2025-" + strconv.Itoa(time.Now().Year())

var debugMode = false

var (
	myApp                 fyne.App
	mainWindow            fyne.Window
	config                *Config
	configPath            string
	discoveredApps        []App
	tabContainer          *container.AppTabs
	appGrids              map[string]fyne.CanvasObject // Maps tab ID to app grid (scroll container)
	currentTabID          string
	childWindows          []fyne.Window          // Track child windows for auto-close
	toolbar               *fyne.Container        // Reference to toolbar for easy updates
	menuBar               *fyne.Container        // Reference to menuBar for easy updates
	contentWrapper        *fyne.Container        // Stable wrapper for main content (avoids SetContent calls)
	openDialogs           map[string]fyne.Window // Track open dialogs by title to prevent duplicates
	updateWindow          fyne.Window            // Update check window
	helpWindow            fyne.Window            // Help window
	aboutWindow           fyne.Window            // About window
	searchFilter          string                 // Current search/filter text
	searchEntry           *widget.Entry          // Search entry widget for filtering apps
	listViewMode          bool                   // List view mode (true) vs grid/icon view (false)
	viewToggleBtn         *widget.Button         // Toggle button for list/grid view
	manageAppsRefreshFunc func()                 // Function to refresh manage apps dialog list

	// Performance caches with mutex protection for concurrent access
	iconCache         = make(map[string]fyne.Resource)  // Cache loaded icon resources by path
	iconCacheMu       sync.RWMutex                      // Mutex for iconCache
	validImageCache   = make(map[string]bool)           // Cache image validation results
	validImageCacheMu sync.RWMutex                      // Mutex for validImageCache
	appCardCache      = make(map[string]*AppCardWidget) // Cache app cards by app ID + tab ID
	appCardCacheMu    sync.RWMutex                      // Mutex for appCardCache
)

func main() {
	myApp = app.NewWithID("com.github.amarillier.KrankyBearLaunchPad")

	// Load saved theme preference
	// "system" follows OS theme, "light" and "dark" are explicit overrides
	savedTheme := myApp.Preferences().StringWithFallback("theme", "system")
	if savedTheme == "light" {
		myApp.Settings().SetTheme(&appTheme{Theme: theme.LightTheme()})
	} else if savedTheme == "dark" {
		myApp.Settings().SetTheme(&appTheme{Theme: theme.DarkTheme()})
	} else {
		// "system" or any other value - use default theme which follows OS
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

	// Start progressive icon loading in background after UI is shown
	go loadIconsProgressively()

	// Center on cursor display after a brief delay to let GLFW fully initialize
	go func() {
		time.Sleep(100 * time.Millisecond)
		fyne.Do(func() {
			centerWindowOnCursorDisplay(mainWindow)
		})
	}()

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

	// Create search entry for filtering apps with minimum width
	// Use debouncing to avoid excessive grid rebuilds while typing
	var filterTimer *time.Timer
	searchEntry = widget.NewEntry()
	searchEntry.SetPlaceHolder("Search apps...")
	searchEntry.OnChanged = func(text string) {
		// Cancel previous timer if still pending
		if filterTimer != nil {
			filterTimer.Stop()
		}
		// Debounce: wait 200ms after last keystroke before filtering
		filterTimer = time.AfterFunc(200*time.Millisecond, func() {
			newFilter := strings.ToLower(strings.TrimSpace(text))
			if newFilter != searchFilter {
				searchFilter = newFilter
				fyne.Do(func() {
					refreshAllGrids()
				})
			}
		})
	}

	// Clear search button - immediately clear filter
	clearSearchBtn := widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		searchEntry.SetText("")
		// Immediately clear filter without waiting for debounce
		if searchFilter != "" {
			searchFilter = ""
			refreshAllGrids()
		}
	})

	// Wrap search entry in a container with minimum width (approx 20 characters = ~200px)
	searchEntryWithMinWidth := container.New(&minWidthLayout{minWidth: 200}, searchEntry)

	// Search container with entry and clear button
	searchContainer := container.NewBorder(nil, nil, nil, clearSearchBtn, searchEntryWithMinWidth)

	// View toggle button - switches between grid (icons) and list view
	viewToggleBtn = widget.NewButtonWithIcon("List", theme.ListIcon(), func() {
		listViewMode = !listViewMode
		if listViewMode {
			viewToggleBtn.SetText("Grid")
			viewToggleBtn.SetIcon(theme.GridIcon())
		} else {
			viewToggleBtn.SetText("List")
			viewToggleBtn.SetIcon(theme.ListIcon())
		}
		refreshAllGrids()
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
		widget.NewSeparator(),
		viewToggleBtn,
		widget.NewSeparator(),
		searchContainer,
	)

	content := container.NewBorder(
		toolbar,
		nil,
		nil,
		nil,
		tabContainer,
	)

	fullContent := container.NewBorder(
		menuBar,
		nil,
		nil,
		nil,
		content,
	)

	// Create or update the content wrapper (only call SetContent once during initial setup)
	if contentWrapper == nil {
		// Use a VBox with single item that we can update later
		contentWrapper = container.NewStack(fullContent)
		mainWindow.SetContent(contentWrapper)
	} else {
		// Update existing wrapper by replacing its single child
		contentWrapper.Objects = []fyne.CanvasObject{fullContent}
		contentWrapper.Refresh()
	}
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
			// Close all child windows and quit
			// Note: Don't call SetSystemTrayMenu(nil) - Fyne doesn't handle nil menus
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

// minWidthLayout is a custom layout that enforces a minimum width
type minWidthLayout struct {
	minWidth float32
}

func (m *minWidthLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 {
		return fyne.NewSize(m.minWidth, 0)
	}
	childMin := objects[0].MinSize()
	return fyne.NewSize(fyne.Max(m.minWidth, childMin.Width), childMin.Height)
}

func (m *minWidthLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, obj := range objects {
		obj.Resize(size)
		obj.Move(fyne.NewPos(0, 0))
	}
}

// DynamicGridWidget is a custom widget that creates a grid with dynamic column count
type DynamicGridWidget struct {
	widget.BaseWidget
	tabID        string
	grid         *fyne.Container
	gridWrapper  *fyne.Container // Wrapper to constrain grid height
	scroll       *container.Scroll
	lastWidth    float32
	lastFilter   string // Track last filter to detect changes
	lastViewMode bool   // Track last view mode (true = list, false = grid)
}

// NewDynamicGridWidget creates a new dynamic grid widget
func NewDynamicGridWidget(tabID string) *DynamicGridWidget {
	w := &DynamicGridWidget{
		tabID:     tabID,
		lastWidth: 0,
	}
	w.ExtendBaseWidget(w)
	return w
}

// CreateRenderer creates the renderer for the dynamic grid widget
func (d *DynamicGridWidget) CreateRenderer() fyne.WidgetRenderer {
	// Create initial grid with estimated columns
	d.updateGrid(800) // Initial estimate

	// Don't wrap in VBox - use scroll directly to allow proper resizing
	// The scroll container will handle its own sizing and scrolling

	return &dynamicGridRenderer{
		widget:  d,
		objects: []fyne.CanvasObject{d.scroll},
	}
}

// updateGrid updates the grid with the appropriate number of columns based on width
func (d *DynamicGridWidget) updateGrid(availableWidth float32) {
	// Check if view mode changed
	viewModeChanged := d.lastViewMode != listViewMode

	// Check if filter changed
	filterChanged := d.lastFilter != searchFilter

	// For grid view: calculate columns based on available width
	cardWidth := float32(120)
	currentColumns := int(availableWidth / cardWidth)
	if currentColumns < 1 {
		currentColumns = 1
	}
	lastColumns := int(d.lastWidth / cardWidth)
	if lastColumns < 1 && d.lastWidth > 0 {
		lastColumns = 1
	}

	// Determine if we need to recreate the container
	needsRecreate := d.grid == nil || d.lastWidth == 0 || viewModeChanged || filterChanged
	if !listViewMode && currentColumns != lastColumns {
		needsRecreate = true
	}

	if needsRecreate {
		d.lastWidth = availableWidth
		d.lastFilter = searchFilter
		d.lastViewMode = listViewMode

		// Get apps for this tab
		apps := []App{}
		tab := getTabByID(d.tabID)
		if tab != nil {
			for _, appID := range tab.AppIDs {
				app := getAppByID(appID)
				if app != nil {
					// Apply search filter if set
					if searchFilter != "" {
						appNameLower := strings.ToLower(app.Name)
						if !strings.Contains(appNameLower, searchFilter) {
							continue
						}
					}
					apps = append(apps, *app)
				}
			}
		}

		// Sort apps alphabetically by name (case-insensitive)
		sort.Slice(apps, func(i, j int) bool {
			return strings.ToLower(apps[i].Name) < strings.ToLower(apps[j].Name)
		})

		// Create container based on view mode
		if listViewMode {
			// List view: simple VBox with clickable labels
			d.grid = container.NewVBox()
			for _, app := range apps {
				listItem := createListItem(app, d.tabID)
				d.grid.Add(listItem)
			}
		} else {
			// Grid view: cards with icons
			d.grid = container.NewGridWithColumns(currentColumns)
			for _, app := range apps {
				card := getCachedAppCard(app, d.tabID)
				d.grid.Add(card)
			}
		}

		// Update the grid wrapper with the new grid
		if d.gridWrapper == nil {
			d.gridWrapper = container.NewWithoutLayout()
		}
		d.gridWrapper.Objects = []fyne.CanvasObject{d.grid}

		// Create or update scroll container with wrapped grid
		if d.scroll == nil {
			d.scroll = container.NewScroll(d.gridWrapper)
		}

		// Force layout update on the grid and wrapper
		if availableWidth > 0 {
			gridMinSize := d.grid.MinSize()
			d.grid.Resize(fyne.NewSize(availableWidth, gridMinSize.Height))
			d.grid.Move(fyne.NewPos(0, 0))
			d.gridWrapper.Resize(fyne.NewSize(availableWidth, gridMinSize.Height))
			d.gridWrapper.Move(fyne.NewPos(0, 0))
		}

		// Refresh grid, wrapper, and scroll to show changes
		d.grid.Refresh()
		if d.gridWrapper != nil {
			d.gridWrapper.Refresh()
		}
		if d.scroll != nil {
			d.scroll.Refresh()
		}
	}
}

// createListItem creates a simple clickable list item for list view mode
func createListItem(app App, tabID string) fyne.CanvasObject {
	// Capture app for closure
	appCopy := app

	// Create a button-style label that launches the app
	btn := widget.NewButton(app.Name, func() {
		if err := LaunchApp(appCopy); err != nil {
			dialog.ShowError(fmt.Errorf("Failed to launch %s: %v", appCopy.Name, err), mainWindow)
		}
	})
	btn.Importance = widget.LowImportance     // Less prominent styling
	btn.Alignment = widget.ButtonAlignLeading // Left-align text

	return btn
}

// ForceRefresh forces a complete refresh of the grid (e.g., when filter changes)
func (d *DynamicGridWidget) ForceRefresh() {
	// Get current size from the widget itself
	currentSize := d.Size()
	currentWidth := currentSize.Width
	if currentWidth <= 0 {
		currentWidth = d.lastWidth
	}
	if currentWidth <= 0 {
		currentWidth = 800 // Default fallback
	}

	// Reset filter tracking to force update (keep width to avoid column recalculation)
	d.lastFilter = "___force_refresh___"

	// Update the grid
	d.updateGrid(currentWidth)

	// Refresh all components in the hierarchy
	if d.grid != nil {
		d.grid.Refresh()
	}
	if d.gridWrapper != nil {
		d.gridWrapper.Refresh()
	}
	if d.scroll != nil {
		d.scroll.Refresh()
	}
	d.Refresh()
}

// Resize handles widget resize to recalculate columns
func (d *DynamicGridWidget) Resize(size fyne.Size) {
	d.BaseWidget.Resize(size)
	// Recalculate columns based on new width
	d.updateGrid(size.Width)
	if d.scroll != nil {
		d.scroll.Resize(size)
	}
}

// refreshAllGrids refreshes all app grids (used when search filter changes)
func refreshAllGrids() {
	// Only refresh the currently visible tab for better performance
	if currentTabID != "" {
		if gridObj, ok := appGrids[currentTabID]; ok {
			if tabContent, ok := gridObj.(*TabContentWidget); ok {
				if dynamicGrid, ok := tabContent.content.(*DynamicGridWidget); ok {
					dynamicGrid.ForceRefresh()
				}
			} else if dynamicGrid, ok := gridObj.(*DynamicGridWidget); ok {
				dynamicGrid.ForceRefresh()
			}
		}
	}
}

// dynamicGridRenderer renders the dynamic grid widget
type dynamicGridRenderer struct {
	widget  *DynamicGridWidget
	objects []fyne.CanvasObject
}

func (r *dynamicGridRenderer) Layout(size fyne.Size) {
	if r.widget.scroll != nil && r.widget.grid != nil {
		r.widget.updateGrid(size.Width)

		// Get the grid's natural minimum size (compact, no stretching)
		gridMinSize := r.widget.grid.MinSize()

		// Set grid wrapper to grid's minimum height to prevent vertical stretching
		// Width uses full available width
		if r.widget.gridWrapper != nil {
			r.widget.gridWrapper.Resize(fyne.NewSize(size.Width, gridMinSize.Height))
			r.widget.gridWrapper.Move(fyne.NewPos(0, 0))

			// Position grid within wrapper at its natural size (no vertical stretching)
			if len(r.widget.gridWrapper.Objects) > 0 {
				r.widget.gridWrapper.Objects[0].Resize(fyne.NewSize(size.Width, gridMinSize.Height))
				r.widget.gridWrapper.Objects[0].Move(fyne.NewPos(0, 0))
			}
		}

		// Resize scroll to full available size - it will handle scrolling internally
		// This allows the window to be resized vertically
		r.widget.scroll.Resize(size)
		r.widget.scroll.Move(fyne.NewPos(0, 0))
	}
}

func (r *dynamicGridRenderer) MinSize() fyne.Size {
	if r.widget.scroll != nil {
		// Return scroll's minimum size, not grid's - allows window to resize vertically
		return r.widget.scroll.MinSize()
	}
	return fyne.NewSize(100, 100)
}

func (r *dynamicGridRenderer) Refresh() {
	if r.widget.scroll != nil {
		r.widget.scroll.Refresh()
	}
}

func (r *dynamicGridRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *dynamicGridRenderer) Destroy() {}

// TabContentWidget wraps the grid and handles right-clicks on empty space
type TabContentWidget struct {
	widget.BaseWidget
	tabID   string
	content fyne.CanvasObject
}

// NewTabContentWidget creates a new tab content widget
func NewTabContentWidget(tabID string, content fyne.CanvasObject) *TabContentWidget {
	w := &TabContentWidget{
		tabID:   tabID,
		content: content,
	}
	w.ExtendBaseWidget(w)
	return w
}

// CreateRenderer creates the renderer for the tab content widget
func (t *TabContentWidget) CreateRenderer() fyne.WidgetRenderer {
	return &tabContentRenderer{
		widget:  t,
		content: t.content,
		objects: []fyne.CanvasObject{t.content},
	}
}

// TappedSecondary handles right-click on empty space
func (t *TabContentWidget) TappedSecondary(pe *fyne.PointEvent) {
	if mainWindow == nil {
		return
	}

	// Set current tab ID to this tab's ID so edit dialog edits the correct tab
	currentTabID = t.tabID

	// Show edit tab dialog
	showEditTabDialog()
}

// MouseIn is required for desktop mouse events
func (t *TabContentWidget) MouseIn(*desktop.MouseEvent)    {}
func (t *TabContentWidget) MouseOut()                      {}
func (t *TabContentWidget) MouseMoved(*desktop.MouseEvent) {}

// tabContentRenderer renders the tab content widget
type tabContentRenderer struct {
	widget  *TabContentWidget
	content fyne.CanvasObject
	objects []fyne.CanvasObject
}

func (r *tabContentRenderer) Layout(size fyne.Size) {
	r.content.Resize(size)
	r.content.Move(fyne.NewPos(0, 0))
}

func (r *tabContentRenderer) MinSize() fyne.Size {
	// Return content's minimum size - allows window to resize freely
	return r.content.MinSize()
}

func (r *tabContentRenderer) Refresh() {
	r.content.Refresh()
}

func (r *tabContentRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *tabContentRenderer) Destroy() {}

func createAppGrid(tabID string) fyne.CanvasObject {
	// Use dynamic grid widget that adjusts columns based on width
	dynamicGrid := NewDynamicGridWidget(tabID)
	// Wrap in TabContentWidget to handle right-clicks on empty space
	return NewTabContentWidget(tabID, dynamicGrid)
}

// getGridFromContainer extracts the actual grid from a scroll container or TabContentWidget
func getGridFromContainer(scrollContainer fyne.CanvasObject) *fyne.Container {
	// Check if it's a TabContentWidget - unwrap it
	if tabContent, ok := scrollContainer.(*TabContentWidget); ok {
		// Get the content (which should be DynamicGridWidget)
		if dynamicGrid, ok := tabContent.content.(*DynamicGridWidget); ok {
			return dynamicGrid.grid
		}
		// Fallback: try to get grid from content
		scrollContainer = tabContent.content
	}

	// Check if it's a DynamicGridWidget
	if dynamicGrid, ok := scrollContainer.(*DynamicGridWidget); ok {
		return dynamicGrid.grid
	}

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
// This function uses fyne.Do to ensure UI updates happen on the main thread
func refreshGridWithSortedApps(tabID string) {
	fyne.Do(func() {
		refreshGridWithSortedAppsImpl(tabID)
	})
}

// refreshGridWithSortedAppsImpl is the actual implementation (must be called on main thread)
// Note: This function no longer calls refreshTabsUIImpl - callers should call refreshTabsUI() if needed
func refreshGridWithSortedAppsImpl(tabID string) {
	if scrollContainer, ok := appGrids[tabID]; ok {
		// Check if it's a TabContentWidget - just force refresh
		if tabContent, ok := scrollContainer.(*TabContentWidget); ok {
			if dynamicGrid, ok := tabContent.content.(*DynamicGridWidget); ok {
				dynamicGrid.ForceRefresh()
			}
			return
		}
		// Check if it's a DynamicGridWidget - just force refresh
		if dynamicGrid, ok := scrollContainer.(*DynamicGridWidget); ok {
			dynamicGrid.ForceRefresh()
			return
		}

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
	// Try to get cached icon first (synchronous - fast if cached)
	var iconResource fyne.Resource
	if a.app.Icon != "" {
		iconCacheMu.RLock()
		if cached, found := iconCache[a.app.Icon]; found && cached != nil {
			iconResource = cached
		}
		iconCacheMu.RUnlock()
	}

	// Use cached icon or placeholder
	var icon *canvas.Image
	if iconResource != nil {
		icon = canvas.NewImageFromResource(iconResource)
	} else {
		icon = canvas.NewImageFromResource(getGenericAppIcon())
		// Only start async load if we don't have a cached icon
		go a.loadIconAsync()
	}
	icon.FillMode = canvas.ImageFillContain
	icon.SetMinSize(fyne.NewSize(64, 64))
	icon.Resize(fyne.NewSize(64, 64))

	// Create label
	label := widget.NewLabel(a.app.Name)
	label.Wrapping = fyne.TextWrapWord
	label.Alignment = fyne.TextAlignCenter

	// Create card content with icon and label - use minimal spacing
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

// loadIconAsync loads the icon in the background and updates the widget
func (a *AppCardWidget) loadIconAsync() {
	// Try to get cached icon first (fast path)
	var iconResource fyne.Resource

	// Check icon cache first (with read lock)
	if a.app.Icon != "" {
		iconCacheMu.RLock()
		if cached, found := iconCache[a.app.Icon]; found && cached != nil {
			iconResource = cached
		}
		iconCacheMu.RUnlock()
	}

	// If not cached, load it (slow path)
	if iconResource == nil {
		iconResource = a.loadIconResourceFast()
	}

	// Update UI on main thread
	if iconResource != nil && a.icon != nil {
		fyne.Do(func() {
			a.icon.Resource = iconResource
			a.icon.Refresh()
		})
	}
}

// loadIconResourceFast loads icon with validation to prevent "unknown format" errors
func (a *AppCardWidget) loadIconResourceFast() fyne.Resource {
	// Try saved icon path first
	if a.app.Icon != "" {
		// Check cache (read lock)
		iconCacheMu.RLock()
		if cached, found := iconCache[a.app.Icon]; found {
			iconCacheMu.RUnlock()
			return cached
		}
		iconCacheMu.RUnlock()

		// Validate and load for supported formats
		if isValidImageFile(a.app.Icon) {
			if resource, err := fyne.LoadResourceFromPath(a.app.Icon); err == nil {
				iconCacheMu.Lock()
				iconCache[a.app.Icon] = resource
				iconCacheMu.Unlock()
				return resource
			}
		}

		// Try .icns/.ico extraction
		if strings.HasSuffix(strings.ToLower(a.app.Icon), ".icns") || strings.HasSuffix(strings.ToLower(a.app.Icon), ".ico") {
			if extractedPath := extractIconFromIcnsOrIco(a.app.Icon); extractedPath != "" {
				if isValidImageFile(extractedPath) {
					if resource, err := fyne.LoadResourceFromPath(extractedPath); err == nil {
						iconCacheMu.Lock()
						iconCache[a.app.Icon] = resource
						iconCacheMu.Unlock()
						return resource
					}
				}
			}
		}
	}

	// Try to detect from executable
	if resource := detectIconFromExecutableFast(a.app.Executable); resource != nil {
		return resource
	}

	return nil
}

// detectIconFromExecutableFast finds icon with validation to prevent errors
func detectIconFromExecutableFast(executable string) fyne.Resource {
	// For macOS .app bundles
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
			// Try PNG/JPG files first (with validation)
			for _, entry := range entries {
				name := entry.Name()
				if isFyneSupportedImageFormat(name) {
					iconPath := filepath.Join(resourcesDir, name)
					// Check cache (read lock)
					iconCacheMu.RLock()
					if cached, found := iconCache[iconPath]; found {
						iconCacheMu.RUnlock()
						return cached
					}
					iconCacheMu.RUnlock()

					// Validate before loading
					if isValidImageFile(iconPath) {
						if resource, err := fyne.LoadResourceFromPath(iconPath); err == nil {
							iconCacheMu.Lock()
							iconCache[iconPath] = resource
							iconCacheMu.Unlock()
							return resource
						}
					}
				}
			}
			// Try .icns files
			for _, entry := range entries {
				name := entry.Name()
				if strings.HasSuffix(strings.ToLower(name), ".icns") {
					iconPath := filepath.Join(resourcesDir, name)
					// Check cache (read lock)
					iconCacheMu.RLock()
					if cached, found := iconCache[iconPath]; found {
						iconCacheMu.RUnlock()
						return cached
					}
					iconCacheMu.RUnlock()

					if extractedPath := extractIconFromIcnsOrIco(iconPath); extractedPath != "" {
						if isValidImageFile(extractedPath) {
							if resource, err := fyne.LoadResourceFromPath(extractedPath); err == nil {
								iconCacheMu.Lock()
								iconCache[iconPath] = resource
								iconCacheMu.Unlock()
								return resource
							}
						}
					}
				}
			}
		}
	}

	return nil
}

// Tapped handles single click (launch app)
// Only launches if the click is actually on the widget, not on whitespace
func (a *AppCardWidget) Tapped(pe *fyne.PointEvent) {
	// Verify the click is within the widget bounds
	size := a.Size()
	if pe.Position.X < 0 || pe.Position.X > size.Width ||
		pe.Position.Y < 0 || pe.Position.Y > size.Height {
		return // Click outside widget bounds, ignore
	}

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
		fyne.NewMenuItem("Refresh Icon", func() {
			// Clear cache and reload icon
			refreshIconCache(a.app, a.tabID)
		}),
		fyne.NewMenuItemSeparator(),
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

	// Create popup menu and show it - NewPopUpMenu properly handles keyboard focus
	// allowing Escape to close the menu
	popup := widget.NewPopUpMenu(menu, canvas)
	popup.ShowAtPosition(popupPos)

	// Request focus for the popup so Escape key works
	canvas.Focus(popup)
}

// MouseIn is required for desktop mouse events
func (a *AppCardWidget) MouseIn(*desktop.MouseEvent)    {}
func (a *AppCardWidget) MouseOut()                      {}
func (a *AppCardWidget) MouseMoved(*desktop.MouseEvent) {}

// loadAppIcon loads and resizes the app icon to 64x64
// Uses caching to improve performance
func (a *AppCardWidget) loadAppIcon() *canvas.Image {
	var iconResource fyne.Resource

	// Try to load icon from saved path first (uses cache)
	if a.app.Icon != "" {
		iconResource = loadCachedIconResource(a.app.Icon)
	}

	// If no icon found, try to detect from executable path
	if iconResource == nil {
		iconResource = detectIconFromExecutable(a.app.Executable)
		// If we detected an icon, save the path to config
		if iconResource != nil && a.app.Icon == "" {
			if detectedIconPath := findIconPathFromExecutable(a.app.Executable); detectedIconPath != "" {
				if isValidImageFile(detectedIconPath) {
					// Update app icon in config
					for i := range config.Apps {
						if config.Apps[i].ID == a.app.ID {
							config.Apps[i].Icon = detectedIconPath
							a.app.Icon = detectedIconPath
							// Save config async
							go func() {
								SaveConfig(config, configPath)
							}()
							break
						}
					}
				}
			}
		}
	}

	// Fallback to generic application icon
	if iconResource == nil {
		iconResource = getGenericAppIcon()
	}

	// Create image and resize to 64x64
	img := canvas.NewImageFromResource(iconResource)
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(fyne.NewSize(64, 64))
	img.Resize(fyne.NewSize(64, 64))

	return img
}

// findIconPathFromExecutable finds the icon file path (not resource) for saving to config
// Only returns paths for formats that Fyne can load (PNG, JPG, BMP) and validates the content
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
			// Only return valid image files
			for _, entry := range entries {
				iconPath := filepath.Join(resourcesDir, entry.Name())
				if isValidImageFile(iconPath) {
					return iconPath
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
				iconPath := filepath.Join(dir, name)
				// Only return valid image files that match name
				if isValidImageFile(iconPath) &&
					strings.Contains(strings.ToLower(name), strings.ToLower(exeName)) {
					return iconPath
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
			iconPath := filepath.Join(dir, name)
			// Only return valid image files
			if isValidImageFile(iconPath) {
				// Try to match by name
				baseName := strings.TrimSuffix(strings.ToLower(exeName), filepath.Ext(exeName))
				iconBaseName := strings.TrimSuffix(strings.ToLower(name), filepath.Ext(name))
				if strings.Contains(iconBaseName, baseName) || strings.Contains(baseName, iconBaseName) {
					return iconPath
				}
			}
		}
	}

	return ""
}

// loadIconResource loads the icon resource (used for button icon)
// Uses caching to improve performance
func (a *AppCardWidget) loadIconResource() fyne.Resource {
	var iconResource fyne.Resource

	// Try to load icon from saved path first (uses cache)
	if a.app.Icon != "" {
		iconResource = loadCachedIconResource(a.app.Icon)
	}

	// If no icon found, try to detect from executable path
	if iconResource == nil {
		iconResource = detectIconFromExecutable(a.app.Executable)
	}

	// Fallback to generic application icon
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

// loadCachedIconResource loads an icon resource from cache or disk
// Returns nil if the icon cannot be loaded
// iconDisplaySize is the size icons are displayed at in the app
const iconDisplaySize = 64

func loadCachedIconResource(iconPath string) fyne.Resource {
	if iconPath == "" {
		return nil
	}

	// Check cache first (read lock)
	iconCacheMu.RLock()
	if resource, found := iconCache[iconPath]; found {
		iconCacheMu.RUnlock()
		return resource
	}
	iconCacheMu.RUnlock()

	var resource fyne.Resource
	var err error

	// Get resized icon path (64x64 cached version)
	resizedPath := getResizedIconPath(iconPath)

	// Check if we have a resized version already
	if resizedPath != "" && fileExists(resizedPath) && isValidImageFile(resizedPath) {
		resource, err = fyne.LoadResourceFromPath(resizedPath)
		if err == nil && resource != nil {
			// Cache and return
			iconCacheMu.Lock()
			iconCache[iconPath] = resource
			iconCacheMu.Unlock()
			return resource
		}
	}

	// Try to load and resize based on file type
	if isValidImageFile(iconPath) {
		// Resize to 64x64 and cache
		if resizedPath := resizeAndCacheIcon(iconPath); resizedPath != "" {
			resource, err = fyne.LoadResourceFromPath(resizedPath)
			if err != nil {
				resource = nil
			}
		}
	} else if strings.HasSuffix(strings.ToLower(iconPath), ".icns") || strings.HasSuffix(strings.ToLower(iconPath), ".ico") {
		// Try to extract from .icns or .ico file (already resizes to 64x64)
		if extractedPath := extractIconFromIcnsOrIco(iconPath); extractedPath != "" {
			if isValidImageFile(extractedPath) {
				resource, err = fyne.LoadResourceFromPath(extractedPath)
				if err != nil {
					resource = nil
				}
			}
		}
	}

	// Cache the result (write lock)
	iconCacheMu.Lock()
	iconCache[iconPath] = resource
	iconCacheMu.Unlock()
	return resource
}

// getResizedIconPath returns the path where a resized 64x64 icon would be cached
func getResizedIconPath(originalPath string) string {
	if originalPath == "" {
		return ""
	}
	iconCacheDir := filepath.Join(filepath.Dir(configPath), "icon_cache")
	hash := sha256.Sum256([]byte(originalPath))
	return filepath.Join(iconCacheDir, "resized_"+hex.EncodeToString(hash[:8])+".png")
}

// resizeAndCacheIcon resizes an icon to 64x64 and caches it
func resizeAndCacheIcon(iconPath string) string {
	if iconPath == "" || !fileExists(iconPath) {
		return ""
	}

	iconCacheDir := filepath.Join(filepath.Dir(configPath), "icon_cache")
	os.MkdirAll(iconCacheDir, 0755)

	resizedPath := getResizedIconPath(iconPath)

	// Check if already resized
	if fileExists(resizedPath) {
		return resizedPath
	}

	// Use sips on macOS to resize (fast and reliable)
	if runtime.GOOS == "darwin" {
		cmd := exec.Command("sips",
			"-s", "format", "png",
			"-Z", fmt.Sprintf("%d", iconDisplaySize), // Resize to fit in 64x64 box
			iconPath,
			"--out", resizedPath)
		if err := cmd.Run(); err == nil {
			return resizedPath
		}
	}

	// Use ImageMagick convert on Linux
	if runtime.GOOS == "linux" {
		cmd := exec.Command("convert",
			iconPath,
			"-resize", fmt.Sprintf("%dx%d", iconDisplaySize, iconDisplaySize),
			resizedPath)
		if err := cmd.Run(); err == nil {
			return resizedPath
		}
	}

	// On Windows or if resize fails, just copy the original
	// (Fyne will handle scaling, but at least we have a cache)
	if data, err := os.ReadFile(iconPath); err == nil {
		if err := os.WriteFile(resizedPath, data, 0644); err == nil {
			return resizedPath
		}
	}

	// If all else fails, return original path
	return iconPath
}

// clearIconCache removes an icon from both memory and disk cache
// Returns true if anything was cleared
func clearIconCache(iconPath string) bool {
	cleared := false

	if iconPath == "" {
		return false
	}

	// Clear from memory cache
	iconCacheMu.Lock()
	if _, exists := iconCache[iconPath]; exists {
		delete(iconCache, iconPath)
		cleared = true
	}
	iconCacheMu.Unlock()

	// Clear from validation cache
	validImageCacheMu.Lock()
	delete(validImageCache, iconPath)
	validImageCacheMu.Unlock()

	// Clear resized icon file from disk
	resizedPath := getResizedIconPath(iconPath)
	if resizedPath != "" && fileExists(resizedPath) {
		if err := os.Remove(resizedPath); err == nil {
			cleared = true
			if debugMode {
				fmt.Printf("Removed cached icon: %s\n", resizedPath)
			}
		}
	}

	return cleared
}

// refreshIconCache clears and reloads an icon for an app
// Updates the app card with the fresh icon
func refreshIconCache(app App, tabID string) {
	// Clear existing cache entries
	if app.Icon != "" {
		clearIconCache(app.Icon)
	}

	// Also try to clear any detected icon path
	if detectedPath := findIconPathFromExecutable(app.Executable); detectedPath != "" {
		clearIconCache(detectedPath)
	}

	// Clear app card cache for this app
	appCardCacheMu.Lock()
	cacheKey := app.ID + "_" + tabID
	delete(appCardCache, cacheKey)
	appCardCacheMu.Unlock()

	// Refresh the UI to reload the icon
	fyne.Do(func() {
		refreshTabsUI()
	})
}

// isValidImageFile checks if a file is a valid image that can be decoded by Go's image package
// This prevents "unknown format" errors from corrupted or unsupported image files
// Results are cached to avoid repeated file I/O
func isValidImageFile(path string) bool {
	if path == "" {
		return false
	}

	// Check cache first (read lock)
	validImageCacheMu.RLock()
	if result, found := validImageCache[path]; found {
		validImageCacheMu.RUnlock()
		return result
	}
	validImageCacheMu.RUnlock()

	// Check extension first (fast check)
	if !isFyneSupportedImageFormat(path) {
		validImageCacheMu.Lock()
		validImageCache[path] = false
		validImageCacheMu.Unlock()
		return false
	}

	// Check file exists
	if !fileExists(path) {
		validImageCacheMu.Lock()
		validImageCache[path] = false
		validImageCacheMu.Unlock()
		return false
	}

	// Try to actually decode the image to validate it
	file, err := os.Open(path)
	if err != nil {
		validImageCacheMu.Lock()
		validImageCache[path] = false
		validImageCacheMu.Unlock()
		return false
	}
	defer file.Close()

	// Try to decode the image header to validate format
	_, _, err = image.DecodeConfig(file)
	result := err == nil
	validImageCacheMu.Lock()
	validImageCache[path] = result
	validImageCacheMu.Unlock()
	return result
}

// extractIconFromIcnsOrIco extracts an icon from .icns or .ico file and converts it to PNG
// Returns the path to the converted PNG file, or empty string if extraction fails
func extractIconFromIcnsOrIco(iconPath string) string {
	if iconPath == "" || !fileExists(iconPath) {
		return ""
	}

	name := strings.ToLower(iconPath)
	isIcns := strings.HasSuffix(name, ".icns")
	isIco := strings.HasSuffix(name, ".ico")

	if !isIcns && !isIco {
		return ""
	}

	// Create cache directory for extracted icons
	cacheDir := filepath.Join(filepath.Dir(configPath), "icon_cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return ""
	}

	// Generate cache filename based on icon path hash
	hashBytes := sha256.Sum256([]byte(iconPath))
	hash := hex.EncodeToString(hashBytes[:])[:16] // Use first 16 chars of hash
	cacheFile := filepath.Join(cacheDir, hash+".png")

	// Check if we already have a cached version
	if fileExists(cacheFile) {
		return cacheFile
	}

	// Extract and resize icon to 64x64 using system tools
	if runtime.GOOS == "darwin" {
		// Use sips on macOS to extract and resize in one step
		// -Z resizes to fit within 64x64 box while maintaining aspect ratio
		cmd := exec.Command("sips",
			"-s", "format", "png",
			"-Z", fmt.Sprintf("%d", iconDisplaySize),
			iconPath,
			"--out", cacheFile)
		if err := cmd.Run(); err == nil && fileExists(cacheFile) {
			return cacheFile
		}
	} else if runtime.GOOS == "linux" {
		// Linux - try ImageMagick with resize
		cmd := exec.Command("convert",
			iconPath,
			"-resize", fmt.Sprintf("%dx%d", iconDisplaySize, iconDisplaySize),
			cacheFile)
		if err := cmd.Run(); err == nil && fileExists(cacheFile) {
			return cacheFile
		}
	}
	// Windows - not supported for icns/ico extraction currently

	return ""
}

// detectIconPathFromExecutable tries to find an icon path based on the executable
// Returns the path to an icon file (may be extracted/cached) or empty string if not found
// This is used when adding custom apps to store the icon path in config
func detectIconPathFromExecutable(executable string) string {
	if executable == "" {
		return ""
	}

	dir := filepath.Dir(executable)
	exeName := filepath.Base(executable)
	baseName := strings.TrimSuffix(exeName, filepath.Ext(exeName))

	// Try exact name match with common icon extensions
	iconExtensions := []string{".png", ".jpg", ".jpeg", ".icns", ".ico", ".bmp", ".gif"}
	for _, ext := range iconExtensions {
		iconPath := filepath.Join(dir, baseName+ext)
		if fileExists(iconPath) {
			// For icns/ico, extract to png and return the extracted path
			if ext == ".icns" || ext == ".ico" {
				if extracted := extractIconFromIcnsOrIco(iconPath); extracted != "" {
					return extracted
				}
			}
			return iconPath
		}
		// Also try lowercase
		iconPath = filepath.Join(dir, strings.ToLower(baseName)+ext)
		if fileExists(iconPath) {
			if ext == ".icns" || ext == ".ico" {
				if extracted := extractIconFromIcnsOrIco(iconPath); extracted != "" {
					return extracted
				}
			}
			return iconPath
		}
	}

	// Try to find any matching icon in same directory
	if entries, err := os.ReadDir(dir); err == nil {
		for _, entry := range entries {
			name := entry.Name()
			nameLower := strings.ToLower(name)
			if strings.HasSuffix(nameLower, ".png") || strings.HasSuffix(nameLower, ".jpg") ||
				strings.HasSuffix(nameLower, ".jpeg") || strings.HasSuffix(nameLower, ".icns") ||
				strings.HasSuffix(nameLower, ".ico") {
				// Try to match by name
				iconBaseName := strings.TrimSuffix(nameLower, filepath.Ext(nameLower))
				if strings.Contains(iconBaseName, strings.ToLower(baseName)) ||
					strings.Contains(strings.ToLower(baseName), iconBaseName) {
					iconPath := filepath.Join(dir, name)
					if strings.HasSuffix(nameLower, ".icns") || strings.HasSuffix(nameLower, ".ico") {
						if extracted := extractIconFromIcnsOrIco(iconPath); extracted != "" {
							return extracted
						}
					}
					return iconPath
				}
			}
		}
	}

	// On macOS, try to extract custom icon from file (set via Finder's Get Info)
	if runtime.GOOS == "darwin" {
		if iconPath := extractMacOSCustomIconPath(executable); iconPath != "" {
			return iconPath
		}
	}

	return ""
}

// extractMacOSCustomIconPath extracts custom Finder icon and returns the cached path
func extractMacOSCustomIconPath(filePath string) string {
	iconCacheDir := filepath.Join(filepath.Dir(configPath), "icon_cache")
	os.MkdirAll(iconCacheDir, 0755)

	// Create a unique cache filename
	hash := sha256.Sum256([]byte(filePath))
	hashStr := hex.EncodeToString(hash[:8])
	cacheFile := filepath.Join(iconCacheDir, "custom_"+hashStr+".png")

	// Check if we already have a cached PNG version
	if fileExists(cacheFile) {
		if isValidImageFile(cacheFile) {
			return cacheFile
		}
	}

	// Try reading the resource fork directly (pure Go)
	rsrcPath := filePath + "/..namedfork/rsrc"
	icnsData := extractIcnsFromResourceFork(rsrcPath)
	if icnsData != nil {
		// Decode icns to PNG using pure Go
		if pngData := decodeIcnsToPNG(icnsData); pngData != nil {
			if err := os.WriteFile(cacheFile, pngData, 0644); err == nil {
				return cacheFile
			}
		}

		// Fallback: try using sips if pure Go decoding failed
		if runtime.GOOS == "darwin" {
			icnsFile := filepath.Join(iconCacheDir, "custom_"+hashStr+".icns")
			if err := os.WriteFile(icnsFile, icnsData, 0644); err == nil {
				defer os.Remove(icnsFile)
				sipsCmd := exec.Command("sips",
					"-s", "format", "png",
					"-Z", fmt.Sprintf("%d", iconDisplaySize),
					icnsFile,
					"--out", cacheFile)
				if err := sipsCmd.Run(); err == nil {
					if fileExists(cacheFile) {
						return cacheFile
					}
				}
			}
		}
	}

	return ""
}

// detectIconFromExecutable tries to find an icon based on the executable path
// Uses caching to improve performance
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
			// First try formats that Fyne supports directly (with caching)
			for _, entry := range entries {
				name := entry.Name()
				if isFyneSupportedImageFormat(name) {
					iconPath := filepath.Join(resourcesDir, name)
					if resource := loadCachedIconResource(iconPath); resource != nil {
						return resource
					}
				}
			}
			// Then try .icns files (extract to PNG, with caching)
			for _, entry := range entries {
				name := entry.Name()
				if strings.HasSuffix(strings.ToLower(name), ".icns") {
					iconPath := filepath.Join(resourcesDir, name)
					if resource := loadCachedIconResource(iconPath); resource != nil {
						return resource
					}
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
				if isFyneSupportedImageFormat(name) &&
					strings.Contains(strings.ToLower(name), strings.ToLower(exeName)) {
					iconPath := filepath.Join(dir, name)
					if resource := loadCachedIconResource(iconPath); resource != nil {
						return resource
					}
				}
			}
		}
	}

	// For any standalone executable, look for adjacent icon files with matching name
	dir := filepath.Dir(executable)
	exeName := filepath.Base(executable)
	baseName := strings.TrimSuffix(exeName, filepath.Ext(exeName))

	// Try exact name match with common icon extensions
	iconExtensions := []string{".png", ".jpg", ".jpeg", ".icns", ".ico", ".bmp", ".gif"}
	for _, ext := range iconExtensions {
		iconPath := filepath.Join(dir, baseName+ext)
		if fileExists(iconPath) {
			if resource := loadCachedIconResource(iconPath); resource != nil {
				return resource
			}
		}
		// Also try lowercase
		iconPath = filepath.Join(dir, strings.ToLower(baseName)+ext)
		if fileExists(iconPath) {
			if resource := loadCachedIconResource(iconPath); resource != nil {
				return resource
			}
		}
	}

	// Try to find any matching icon in same directory
	if entries, err := os.ReadDir(dir); err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if isFyneSupportedImageFormat(name) || strings.HasSuffix(strings.ToLower(name), ".icns") || strings.HasSuffix(strings.ToLower(name), ".ico") {
				// Try to match by name
				iconBaseName := strings.TrimSuffix(strings.ToLower(name), filepath.Ext(name))
				if strings.Contains(iconBaseName, strings.ToLower(baseName)) || strings.Contains(strings.ToLower(baseName), iconBaseName) {
					iconPath := filepath.Join(dir, name)
					if resource := loadCachedIconResource(iconPath); resource != nil {
						return resource
					}
				}
			}
		}
	}

	// Look in common icon directories (XDG standard locations)
	homeDir := os.Getenv("HOME")
	iconDirs := []string{
		filepath.Join(homeDir, ".local", "share", "icons"),
		filepath.Join(homeDir, ".icons"),
		"/usr/share/icons",
		"/usr/share/pixmaps",
		"/usr/local/share/icons",
	}

	for _, iconDir := range iconDirs {
		for _, ext := range iconExtensions {
			iconPath := filepath.Join(iconDir, baseName+ext)
			if fileExists(iconPath) {
				if resource := loadCachedIconResource(iconPath); resource != nil {
					return resource
				}
			}
			// Try in hicolor theme
			for _, size := range []string{"256x256", "128x128", "64x64", "48x48", "32x32"} {
				iconPath = filepath.Join(iconDir, "hicolor", size, "apps", baseName+ext)
				if fileExists(iconPath) {
					if resource := loadCachedIconResource(iconPath); resource != nil {
						return resource
					}
				}
			}
		}
	}

	// On macOS, try to extract custom icon from file (set via Finder's Get Info)
	if runtime.GOOS == "darwin" {
		if resource := extractMacOSCustomIcon(executable); resource != nil {
			return resource
		}
	}

	return nil
}

// extractMacOSCustomIcon extracts custom Finder icon from a file on macOS
// Uses pure Go for resource fork reading and icns decoding (no external tools needed)
func extractMacOSCustomIcon(filePath string) fyne.Resource {
	iconCacheDir := filepath.Join(filepath.Dir(configPath), "icon_cache")
	os.MkdirAll(iconCacheDir, 0755)

	// Create a unique cache filename
	hash := sha256.Sum256([]byte(filePath))
	hashStr := hex.EncodeToString(hash[:8])
	cacheFile := filepath.Join(iconCacheDir, "custom_"+hashStr+".png")

	// Check if we already have a cached PNG version
	if fileExists(cacheFile) {
		if isValidImageFile(cacheFile) {
			if resource, err := fyne.LoadResourceFromPath(cacheFile); err == nil {
				return resource
			}
		}
	}

	// Try reading the resource fork directly (pure Go, no external tools)
	// macOS stores custom icons in the resource fork at path/..namedfork/rsrc
	rsrcPath := filePath + "/..namedfork/rsrc"
	icnsData := extractIcnsFromResourceFork(rsrcPath)
	if icnsData != nil {
		// Decode icns to PNG using pure Go
		if pngData := decodeIcnsToPNG(icnsData); pngData != nil {
			if err := os.WriteFile(cacheFile, pngData, 0644); err == nil {
				if resource, err := fyne.LoadResourceFromPath(cacheFile); err == nil {
					return resource
				}
			}
		}

		// Fallback: try using sips if pure Go decoding failed
		if runtime.GOOS == "darwin" {
			icnsFile := filepath.Join(iconCacheDir, "custom_"+hashStr+".icns")
			if err := os.WriteFile(icnsFile, icnsData, 0644); err == nil {
				defer os.Remove(icnsFile)
				sipsCmd := exec.Command("sips",
					"-s", "format", "png",
					"-Z", fmt.Sprintf("%d", iconDisplaySize),
					icnsFile,
					"--out", cacheFile)
				if err := sipsCmd.Run(); err == nil {
					if resource, err := fyne.LoadResourceFromPath(cacheFile); err == nil {
						return resource
					}
				}
			}
		}
	}

	return nil
}

// extractIcnsFromResourceFork reads the resource fork and extracts icns data
func extractIcnsFromResourceFork(rsrcPath string) []byte {
	data, err := os.ReadFile(rsrcPath)
	if err != nil || len(data) < 268 {
		return nil
	}

	// Search for 'icns' magic bytes in the resource fork
	for i := 0; i < len(data)-8; i++ {
		if data[i] == 'i' && data[i+1] == 'c' && data[i+2] == 'n' && data[i+3] == 's' {
			// Found icns header, read the length (4 bytes, big-endian)
			if i+8 > len(data) {
				continue
			}
			length := int(data[i+4])<<24 | int(data[i+5])<<16 | int(data[i+6])<<8 | int(data[i+7])
			// Sanity check: length should be reasonable
			if length < 8 || length > len(data)-i || length > 10*1024*1024 {
				continue
			}
			// Extract and return the icns data
			if length >= 8 {
				icnsData := data[i : i+length]
				// Verify it starts with icns magic
				if len(icnsData) >= 4 && icnsData[0] == 'i' && icnsData[1] == 'c' && icnsData[2] == 'n' && icnsData[3] == 's' {
					return icnsData
				}
			}
		}
	}

	return nil
}

// decodeIcnsToPNG decodes icns data to PNG bytes using the jackmordaunt/icns package
// This provides full support for all ICNS formats including legacy icon types
func decodeIcnsToPNG(icnsData []byte) []byte {
	if len(icnsData) < 8 {
		return nil
	}

	// Verify icns magic
	if string(icnsData[:4]) != "icns" {
		return nil
	}

	// Use the icns package to decode
	reader := bytes.NewReader(icnsData)
	img, err := icns.Decode(reader)
	if err != nil {
		// Fallback: try manual PNG extraction for embedded PNG chunks
		return extractEmbeddedPNG(icnsData)
	}

	// Encode the decoded image to PNG
	return encodeToPNG(img)
}

// extractEmbeddedPNG extracts embedded PNG data from icns chunks (fallback method)
func extractEmbeddedPNG(icnsData []byte) []byte {
	if len(icnsData) < 8 {
		return nil
	}

	// Chunk types that contain PNG data (prefer larger sizes)
	pngTypes := []string{"ic10", "ic14", "ic13", "ic09", "ic08", "ic07", "icp6", "icp5", "icp4"}

	for _, pngType := range pngTypes {
		if pngData := extractIcnsChunk(icnsData, pngType); pngData != nil {
			// Check if it's actually PNG data (starts with PNG magic)
			if len(pngData) > 8 && pngData[0] == 0x89 && pngData[1] == 'P' && pngData[2] == 'N' && pngData[3] == 'G' {
				return pngData
			}
		}
	}

	// Look for any chunk with PNG magic
	offset := 8 // Skip header
	for offset+8 <= len(icnsData) {
		chunkLen := int(icnsData[offset+4])<<24 | int(icnsData[offset+5])<<16 | int(icnsData[offset+6])<<8 | int(icnsData[offset+7])

		if chunkLen < 8 || offset+chunkLen > len(icnsData) {
			break
		}

		chunkData := icnsData[offset+8 : offset+chunkLen]

		// Check for PNG magic in chunk data
		if len(chunkData) > 8 && chunkData[0] == 0x89 && chunkData[1] == 'P' && chunkData[2] == 'N' && chunkData[3] == 'G' {
			return chunkData
		}

		offset += chunkLen
	}

	return nil
}

// extractIcnsChunk extracts data for a specific chunk type from icns
func extractIcnsChunk(icnsData []byte, chunkType string) []byte {
	if len(icnsData) < 8 {
		return nil
	}

	offset := 8 // Skip icns header
	for offset+8 <= len(icnsData) {
		cType := string(icnsData[offset : offset+4])
		cLen := int(icnsData[offset+4])<<24 | int(icnsData[offset+5])<<16 | int(icnsData[offset+6])<<8 | int(icnsData[offset+7])

		if cLen < 8 || offset+cLen > len(icnsData) {
			break
		}

		if cType == chunkType {
			return icnsData[offset+8 : offset+cLen]
		}

		offset += cLen
	}

	return nil
}

// encodeToPNG encodes an image to PNG bytes
func encodeToPNG(img image.Image) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	return buf.Bytes()
}

// parseRsrcToIcns parses DeRez output and extracts raw icns data
func parseRsrcToIcns(rsrcData []byte) []byte {
	// DeRez output format is like:
	// data 'icns' (128) {
	//     $"0000 0001 ..."
	// };
	content := string(rsrcData)

	// Find hex data between $" and "
	var hexData strings.Builder
	inHex := false
	for i := 0; i < len(content); i++ {
		if i+1 < len(content) && content[i] == '$' && content[i+1] == '"' {
			inHex = true
			i++ // Skip the quote
			continue
		}
		if inHex && content[i] == '"' {
			inHex = false
			continue
		}
		if inHex {
			c := content[i]
			// Only keep hex characters (0-9, A-F, a-f)
			if (c >= '0' && c <= '9') || (c >= 'A' && c <= 'F') || (c >= 'a' && c <= 'f') {
				hexData.WriteByte(c)
			}
		}
	}

	hexStr := hexData.String()
	if len(hexStr) == 0 {
		return nil
	}

	// Decode hex to bytes
	data, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil
	}

	return data
}

// appCardRenderer renders the app card
type appCardRenderer struct {
	widget  *AppCardWidget
	content *fyne.Container
	objects []fyne.CanvasObject
}

func (r *appCardRenderer) Layout(size fyne.Size) {
	// Set content to match widget size exactly - no extra space
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

// getCachedAppCard returns a cached app card or creates a new one
// This significantly improves performance when filtering or refreshing grids
func getCachedAppCard(app App, tabID string) fyne.CanvasObject {
	cacheKey := app.ID + "_" + tabID

	// Check cache (read lock)
	appCardCacheMu.RLock()
	if card, found := appCardCache[cacheKey]; found {
		appCardCacheMu.RUnlock()
		// Update the app data in case it changed
		card.app = app
		return card
	}
	appCardCacheMu.RUnlock()

	// Create new card and cache it (write lock)
	card := NewAppCardWidget(app, tabID)
	appCardCacheMu.Lock()
	appCardCache[cacheKey] = card
	appCardCacheMu.Unlock()
	return card
}

// Update createAppCard to use the new widget
func createAppCard(app App, tabID string) fyne.CanvasObject {
	card := NewAppCardWidget(app, tabID)
	// Return card directly without extra padding - grid will handle spacing
	// This makes cards fit closer together, matching home tab behavior
	return card
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
			safeCloseDialog(dialogWindow)
		}
	})

	cancelBtn := widget.NewButton("Cancel", func() {
		safeCloseDialog(dialogWindow)
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
			safeCloseDialog(dialogWindow)
		}
	})

	cancelBtn := widget.NewButton("Cancel", func() {
		safeCloseDialog(dialogWindow)
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

				// Clear app card cache for the deleted app
				appCardCacheMu.Lock()
				for key := range appCardCache {
					if strings.HasPrefix(key, app.ID+"_") {
						delete(appCardCache, key)
					}
				}
				appCardCacheMu.Unlock()

				// Save config first
				if err := SaveConfig(config, configPath); err != nil {
					dialog.ShowError(fmt.Errorf("Failed to save config: %v", err), mainWindow)
				} else {
					if debugMode {
						fmt.Printf("Deleted '%s'\n", app.Name)
					}
					// Refresh the manage apps dialog list after a delay
					refreshManageAppsDialog()
					// Also refresh the main grid
					refreshTabsUI()
				}
			}
		}, parentWindow)
}

func showManageAppsDialog() {
	// Check if dialog is already open
	if existingWindow := showOrFocusDialog("Manage Applications"); existingWindow != nil {
		return
	}

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

	// Create a refresh function that can be called after add/delete
	var currentFilter string
	refreshList := func() {
		// Reload apps from config
		sortedApps = make([]App, len(config.Apps))
		copy(sortedApps, config.Apps)
		sort.Slice(sortedApps, func(i, j int) bool {
			return strings.ToLower(sortedApps[i].Name) < strings.ToLower(sortedApps[j].Name)
		})

		// Apply current filter
		if currentFilter == "" {
			filteredApps = make([]App, len(sortedApps))
			copy(filteredApps, sortedApps)
		} else {
			filteredApps = []App{}
			for _, app := range sortedApps {
				if strings.Contains(strings.ToLower(app.Name), currentFilter) {
					filteredApps = append(filteredApps, app)
				}
			}
		}

		// Refresh the list widget
		if appList != nil {
			appList.Refresh()
		}
	}

	// Store refresh function globally so it can be called from add/delete operations
	manageAppsRefreshFunc = refreshList

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
		currentFilter = strings.ToLower(strings.TrimSpace(text))
		applyFilter(text)
	}

	// Add custom app button
	addCustomBtn := widget.NewButton("Add Custom Application", func() {
		showAddCustomAppDialog()
	})

	scrollContainer := container.NewScroll(appList)
	scrollContainer.SetMinSize(fyne.NewSize(600, 400)) // Wider and taller

	// Count label (created early so clear button can update it)
	countLabel := widget.NewLabel(fmt.Sprintf("All Applications (sorted alphabetically) - Showing %d of %d", len(filteredApps), len(sortedApps)))

	// Clear filter button
	clearFilterBtn := widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		filterEntry.SetText("")
		currentFilter = ""
		// Reset to show all apps
		filteredApps = make([]App, len(sortedApps))
		copy(filteredApps, sortedApps)
		appList.Refresh()
		countLabel.SetText(fmt.Sprintf("All Applications (sorted alphabetically) - Showing %d of %d", len(filteredApps), len(sortedApps)))
	})

	// Filter entry with clear button
	filterContainer := container.NewBorder(nil, nil, nil, clearFilterBtn, filterEntry)

	// Create content with filter entry, label and scrollable list
	contentVBox := container.NewVBox(
		widget.NewLabel("Filter:"),
		container.NewPadded(filterContainer),
		countLabel,
		scrollContainer,
	)

	// Update count label when filter changes
	originalOnChanged := filterEntry.OnChanged
	filterEntry.OnChanged = func(text string) {
		originalOnChanged(text)
		// Update the count label
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
			// Clear the refresh function when dialog closes
			manageAppsRefreshFunc = nil
			safeCloseDialog(dialogWindow)
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

// refreshManageAppsDialog refreshes the Manage Applications list if the dialog is open
// Uses a delay to ensure config changes have been saved
func refreshManageAppsDialog() {
	if manageAppsRefreshFunc == nil {
		return // Dialog not open
	}

	// Delay refresh to ensure config is saved and UI is stable
	go func() {
		time.Sleep(500 * time.Millisecond)
		fyne.Do(func() {
			if manageAppsRefreshFunc != nil {
				manageAppsRefreshFunc()
			}
		})
	}()
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
							safeCloseDialog(dialogWindow)
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
		safeCloseDialog(dialogWindow)
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
	dialogWindow.Resize(fyne.NewSize(700, 350))

	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("Application Name")
	nameEntry.Wrapping = fyne.TextWrapOff

	execEntry := widget.NewEntry()
	execEntry.SetPlaceHolder("/path/to/executable or ~/path/to/executable")
	execEntry.Wrapping = fyne.TextWrapOff

	iconEntry := widget.NewEntry()
	iconEntry.SetPlaceHolder("/path/to/icon (optional) or ~/path/to/icon")
	iconEntry.Wrapping = fyne.TextWrapOff

	// Browse button for executable - larger dialog
	execBrowseBtn := widget.NewButton("Browse...", func() {
		fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
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

		// Make file dialog larger
		fileDialog.Resize(fyne.NewSize(1000, 700))

		// Start in home directory or common locations
		homeDir, _ := os.UserHomeDir()
		if homeDir != "" {
			if uri, err := storage.ListerForURI(storage.NewFileURI(homeDir)); err == nil {
				fileDialog.SetLocation(uri)
			}
		}

		fileDialog.Show()
	})

	// Browse button for icon - larger dialog with image filter
	iconBrowseBtn := widget.NewButton("Browse...", func() {
		fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
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

		// Make file dialog larger
		fileDialog.Resize(fyne.NewSize(1000, 700))

		// Filter to image files
		fileDialog.SetFilter(storage.NewExtensionFileFilter([]string{
			".png", ".jpg", ".jpeg", ".bmp", ".ico", ".icns",
			".PNG", ".JPG", ".JPEG", ".BMP", ".ICO", ".ICNS",
		}))

		// Start in executable directory if set, otherwise home
		startDir := ""
		if execEntry.Text != "" {
			startDir = filepath.Dir(expandPath(execEntry.Text))
		}
		if startDir == "" || startDir == "." {
			startDir, _ = os.UserHomeDir()
		}
		if startDir != "" {
			if uri, err := storage.ListerForURI(storage.NewFileURI(startDir)); err == nil {
				fileDialog.SetLocation(uri)
			}
		}

		fileDialog.Show()
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
		} else {
			// Try to detect icon from executable if none provided
			// This will check for adjacent icon files, embedded icons, etc.
			if detectedIcon := detectIconPathFromExecutable(executable); detectedIcon != "" {
				icon = detectedIcon
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
						break
					}
				}
			}
		}

		// Save config
		if err := SaveConfig(config, configPath); err != nil {
			dialog.ShowError(fmt.Errorf("Failed to save config: %v", err), dialogWindow)
		} else {
			appName := name // Capture for closure
			if debugMode {
				fmt.Printf("Added '%s' to applications\n", appName)
			}
			// Refresh the manage apps dialog list after a delay
			refreshManageAppsDialog()
			// Also refresh the main grid
			refreshTabsUI()
			// Close the add dialog
			safeCloseDialog(dialogWindow)
		}
	})

	cancelBtn := widget.NewButton("Cancel", func() {
		// Use safe close to prevent GLFW crashes
		safeCloseDialog(dialogWindow)
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
			safeCloseDialog(dialogWindow)
		}
	})

	cancelBtn := widget.NewButton("Cancel", func() {
		safeCloseDialog(dialogWindow)
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

	dialogWindow := myApp.NewWindow("Change Icon - " + app.Name)
	// Make dialog larger for easier browsing (resizable by default)
	dialogWindow.Resize(fyne.NewSize(900, 650))
	registerDialog(dialogWindow)

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

	iconEntry := widget.NewEntry()
	iconEntry.SetText(app.Icon)
	iconEntry.Wrapping = fyne.TextWrapOff

	// Icon preview - make it larger for better visibility
	iconPreview := canvas.NewImageFromResource(getGenericAppIcon())
	iconPreview.FillMode = canvas.ImageFillContain
	iconPreview.SetMinSize(fyne.NewSize(128, 128))
	iconPreview.Resize(fyne.NewSize(128, 128))

	// Function to update icon preview and apply immediately
	updateIcon := func(iconPath string) {
		iconEntry.SetText(iconPath)

		// Update preview
		if iconPath != "" && fileExists(iconPath) {
			var resource fyne.Resource
			var err error
			// Validate and try direct load first
			if isValidImageFile(iconPath) {
				resource, err = fyne.LoadResourceFromPath(iconPath)
			} else if strings.HasSuffix(strings.ToLower(iconPath), ".icns") || strings.HasSuffix(strings.ToLower(iconPath), ".ico") {
				// Try to extract from .icns or .ico file
				if extractedPath := extractIconFromIcnsOrIco(iconPath); extractedPath != "" {
					if isValidImageFile(extractedPath) {
						resource, err = fyne.LoadResourceFromPath(extractedPath)
					}
				}
			}
			if err == nil && resource != nil {
				iconPreview.Resource = resource
				iconPreview.Refresh()
			} else {
				// Show generic icon if validation fails
				iconPreview.Resource = getGenericAppIcon()
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
		var resource fyne.Resource
		var err error
		if isValidImageFile(app.Icon) {
			resource, err = fyne.LoadResourceFromPath(app.Icon)
		} else if strings.HasSuffix(strings.ToLower(app.Icon), ".icns") || strings.HasSuffix(strings.ToLower(app.Icon), ".ico") {
			// Try to extract from .icns or .ico file
			if extractedPath := extractIconFromIcnsOrIco(app.Icon); extractedPath != "" {
				if isValidImageFile(extractedPath) {
					resource, err = fyne.LoadResourceFromPath(extractedPath)
				}
			}
		}
		if err == nil && resource != nil {
			iconPreview.Resource = resource
		} else {
			// Fallback to generic icon if load fails
			iconPreview.Resource = getGenericAppIcon()
		}
	} else if detectedIcon := detectIconFromExecutable(app.Executable); detectedIcon != nil {
		iconPreview.Resource = detectedIcon
	} else {
		// No icon found, use generic placeholder
		iconPreview.Resource = getGenericAppIcon()
	}

	browseBtn := widget.NewButton("Browse...", func() {
		// Create a larger, resizable file open dialog
		fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
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
			safeCloseDialog(dialogWindow)
		}, dialogWindow)

		// Make the file dialog larger (default is too small)
		fileDialog.Resize(fyne.NewSize(1000, 700))

		// Set a starting location based on executable path if possible
		execDir := filepath.Dir(app.Executable)
		if uri, err := storage.ListerForURI(storage.NewFileURI(execDir)); err == nil {
			fileDialog.SetLocation(uri)
		}

		// Filter to image files
		fileDialog.SetFilter(storage.NewExtensionFileFilter([]string{".png", ".jpg", ".jpeg", ".bmp", ".ico", ".icns", ".PNG", ".JPG", ".JPEG", ".BMP", ".ICO", ".ICNS"}))

		fileDialog.Show()
	})

	searchOnlineBtn := widget.NewButton("Search Online", func() {
		// Construct Google Images search URL for app icon
		searchQuery := url.QueryEscape(app.Name + " icon png")
		searchURL := fmt.Sprintf("https://www.google.com/search?q=%s&tbm=isch", searchQuery)

		// Open browser with search URL
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd", "/c", "start", searchURL)
		} else if runtime.GOOS == "linux" {
			cmd = exec.Command("xdg-open", searchURL)
		} else {
			// macOS
			cmd = exec.Command("open", searchURL)
		}

		if err := cmd.Run(); err != nil {
			dialog.ShowError(fmt.Errorf("Failed to open browser: %v", err), dialogWindow)
		}
	})

	// Create a close button
	closeBtn := widget.NewButton("Close", func() {
		safeCloseDialog(dialogWindow)
	})

	// Help text
	formatLabel := widget.NewLabel("Supported formats: PNG, JPG, JPEG, BMP, ICO, ICNS")
	tipLabel := widget.NewLabel("Tip: Use 'Search Online' to find icons, then 'Browse' to select the downloaded file.")
	tipLabel.Wrapping = fyne.TextWrapWord

	// App info for reference (helps user locate icons near the executable)
	appInfoLabel := widget.NewLabel("Application: " + app.Name)
	appInfoLabel.TextStyle = fyne.TextStyle{Bold: true}
	execPathEntry := widget.NewEntry()
	execPathEntry.SetText(app.Executable)
	execPathEntry.Disable() // Read-only
	execPathEntry.Wrapping = fyne.TextWrapOff

	// Create a multiline entry for the icon path that expands both horizontally and vertically
	iconEntry.MultiLine = true
	iconEntry.Wrapping = fyne.TextWrapBreak

	// Top section: Icon preview and buttons
	topSection := container.NewHBox(
		container.NewPadded(container.NewCenter(iconPreview)),
		widget.NewSeparator(),
		container.NewVBox(
			widget.NewLabel("Icon Preview"),
			widget.NewSeparator(),
			browseBtn,
			searchOnlineBtn,
		),
	)

	// App info section for reference
	appInfoSection := container.NewVBox(
		appInfoLabel,
		container.NewBorder(nil, nil, widget.NewLabel("Executable:"), nil, execPathEntry),
	)

	// Bottom info section
	bottomInfo := container.NewVBox(
		widget.NewSeparator(),
		formatLabel,
		tipLabel,
	)

	// Main form with icon path entry that expands to fill available space
	// Use Border layout: top has preview/buttons + app info, bottom has info, center has expanding entry
	formContent := container.NewBorder(
		container.NewVBox(
			appInfoSection,
			widget.NewSeparator(),
			topSection,
			widget.NewSeparator(),
			widget.NewLabel("Icon Path (paste path or use Browse):"),
		),
		bottomInfo,
		nil,
		nil,
		container.NewScroll(iconEntry), // Entry in scroll container expands to fill
	)

	// Bottom buttons centered
	bottomButtons := container.NewHBox(layout.NewSpacer(), closeBtn, layout.NewSpacer())

	// Main dialog content with proper expansion
	dialogWindow.SetContent(container.NewBorder(
		nil,
		container.NewPadded(bottomButtons),
		nil,
		nil,
		container.NewPadded(formContent),
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
		safeCloseDialog(successWindow)
	})

	content := container.NewVBox(
		label,
		container.NewCenter(okBtn),
	)

	successWindow.SetContent(content)
	registerDialog(successWindow)
	// Use centerDialogOnMainWindow to ensure it appears on the same display as the main window
	centerDialogOnMainWindow(successWindow)

	// Auto-close after 5 seconds
	go func() {
		time.Sleep(5 * time.Second)
		fyne.Do(func() {
			// Check if window is still tracked before closing
			if _, exists := openDialogs["Success"]; exists {
				safeCloseDialog(successWindow)
			}
		})
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
	// Check if about window is already open - bring to front
	if aboutWindow != nil {
		aboutWindow.Show()
		aboutWindow.RequestFocus()
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

	aboutWindow = myApp.NewWindow(appName + ": About")
	aboutWindow.SetIcon(resourceKrankyBearTrapperRedPlaidPng)
	aboutWindow.Resize(fyne.NewSize(500, 200))
	aboutWindow.SetContent(content)

	// Set close intercept to clear the tracking variable
	aboutWindow.SetCloseIntercept(func() {
		aboutWindow.Close()
		aboutWindow = nil
	})

	childWindows = append(childWindows, aboutWindow)
	aboutWindow.Show()
	centerDialogOnMainWindow(aboutWindow)
}

// showThemeSettingsDialog displays a dialog to switch between light and dark themes
func showThemeSettingsDialog() {
	// Check if dialog is already open
	if existingWindow := showOrFocusDialog("Theme Settings"); existingWindow != nil {
		return
	}

	dialogWindow := myApp.NewWindow("Theme Settings")
	dialogWindow.Resize(fyne.NewSize(450, 250))

	// Register dialog (this will set up proper cleanup on close)
	registerDialog(dialogWindow)

	// Get current saved theme preference
	savedTheme := myApp.Preferences().StringWithFallback("theme", "system")
	var currentThemeText string
	switch savedTheme {
	case "light":
		currentThemeText = "Light"
	case "dark":
		currentThemeText = "Dark"
	default:
		currentThemeText = "System (follows OS)"
	}

	currentLabel := widget.NewLabel(fmt.Sprintf("Current theme: %s", currentThemeText))
	currentLabel.Alignment = fyne.TextAlignCenter

	// Helper to refresh all windows after theme change
	refreshAllThemes := func() {
		if mainWindow != nil {
			mainWindow.Content().Refresh()
		}
		for _, w := range childWindows {
			if w != nil {
				w.Content().Refresh()
			}
		}
	}

	systemBtn := widget.NewButton("System Theme", func() {
		// Use default theme which follows OS appearance
		myApp.Settings().SetTheme(&appTheme{Theme: theme.DefaultTheme()})
		myApp.Preferences().SetString("theme", "system")
		currentLabel.SetText("Current theme: System (follows OS)")
		refreshAllThemes()
	})

	lightBtn := widget.NewButton("Light Theme", func() {
		myApp.Settings().SetTheme(&appTheme{Theme: theme.LightTheme()})
		myApp.Preferences().SetString("theme", "light")
		currentLabel.SetText("Current theme: Light")
		refreshAllThemes()
	})

	darkBtn := widget.NewButton("Dark Theme", func() {
		myApp.Settings().SetTheme(&appTheme{Theme: theme.DarkTheme()})
		myApp.Preferences().SetString("theme", "dark")
		currentLabel.SetText("Current theme: Dark")
		refreshAllThemes()
	})

	closeBtn := widget.NewButton("Close", func() {
		safeCloseDialog(dialogWindow)
	})

	content := container.NewVBox(
		widget.NewLabel("Select Theme"),
		currentLabel,
		container.NewHBox(systemBtn, lightBtn, darkBtn),
		widget.NewSeparator(),
		widget.NewLabel("System theme follows your OS appearance settings."),
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
			safeCloseDialog(dialogWindow)
		}
	})

	cancelBtn := widget.NewButton("Cancel", func() {
		safeCloseDialog(dialogWindow)
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

// loadIconsProgressively loads icons in the background after the UI is shown
// This prevents the UI from blocking during initial icon loading
func loadIconsProgressively() {
	// Small delay to let the UI fully render first
	time.Sleep(100 * time.Millisecond)

	// Get all apps that need icons loaded
	type appIconInfo struct {
		app    App
		tabIDs []string
	}
	appsToLoad := []appIconInfo{}

	for _, app := range config.Apps {
		// Check if icon is already cached
		iconCacheMu.RLock()
		_, found := iconCache[app.Icon]
		iconCacheMu.RUnlock()

		if !found && app.Icon != "" {
			// Find which tabs this app is in
			tabIDs := []string{}
			for _, tab := range config.Tabs {
				for _, appID := range tab.AppIDs {
					if appID == app.ID {
						tabIDs = append(tabIDs, tab.ID)
						break
					}
				}
			}
			appsToLoad = append(appsToLoad, appIconInfo{app: app, tabIDs: tabIDs})
		}
	}

	// Load icons in small batches to avoid blocking
	batchSize := 5
	for i := 0; i < len(appsToLoad); i += batchSize {
		end := i + batchSize
		if end > len(appsToLoad) {
			end = len(appsToLoad)
		}

		// Load this batch
		for _, info := range appsToLoad[i:end] {
			// Load and cache the icon
			resource := loadCachedIconResource(info.app.Icon)
			if resource != nil {
				// Update any cached cards that use this icon
				for _, tabID := range info.tabIDs {
					cacheKey := info.app.ID + "_" + tabID
					appCardCacheMu.RLock()
					card, found := appCardCache[cacheKey]
					appCardCacheMu.RUnlock()
					if found && card != nil && card.icon != nil {
						fyne.Do(func() {
							card.icon.Resource = resource
							card.icon.Refresh()
						})
					}
				}
			}
		}

		// Small delay between batches to keep UI responsive
		time.Sleep(50 * time.Millisecond)
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

// positionWindowOnDisplay attempts to position the window on the display containing the cursor
// Returns true if positioning was successful, false otherwise
// This is implemented in platform-specific files
func positionWindowOnDisplay(window fyne.Window, cursorX, cursorY int) bool {
	return positionWindowOnDisplayImpl(window, cursorX, cursorY)
}

// centerWindowOnCursorDisplay attempts to center the window on the display containing the mouse cursor
// Falls back to CenterOnScreen() if cursor position cannot be determined or if positioning fails
func centerWindowOnCursorDisplay(window fyne.Window) {
	cursorX, cursorY := getCursorPosition()

	// If we couldn't get cursor position, fall back to default behavior
	if cursorX == 0 && cursorY == 0 {
		window.CenterOnScreen()
		return
	}

	// Try to position window on the display containing the cursor
	if !positionWindowOnDisplay(window, cursorX, cursorY) {
		// Fallback: center on screen (will use primary display)
		window.CenterOnScreen()
	}
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

// safeCloseDialog safely closes a dialog window by deferring everything
// to a background goroutine, preventing GLFW crashes from closing during events
func safeCloseDialog(window fyne.Window) {
	if window == nil {
		return
	}
	// Get title before any operations
	title := window.Title()

	// Remove from tracking immediately (prevents reopening the same dialog)
	delete(openDialogs, title)
	for i, w := range childWindows {
		if w == window {
			childWindows = append(childWindows[:i], childWindows[i+1:]...)
			break
		}
	}

	// Just hide the window - don't close it
	// Closing causes OpenGL texture deletion issues
	// Hidden windows use minimal resources and will be cleaned up on app exit
	window.Hide()
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
	fyne.Do(func() {
		refreshTabsUIImpl()
	})
}

func refreshTabsUIImpl() {
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

	// Update the content area without calling SetContent (to avoid GLFW crashes)
	if toolbar != nil && menuBar != nil && contentWrapper != nil {
		// Recreate the content border with new tab container
		content := container.NewBorder(
			toolbar,
			nil,
			nil,
			nil,
			newTabContainer,
		)

		fullContent := container.NewBorder(
			menuBar,
			nil,
			nil,
			nil,
			content,
		)

		// Update the wrapper's child instead of calling SetContent
		// This is safer because it doesn't invalidate the GLFW window state
		contentWrapper.Objects = []fyne.CanvasObject{fullContent}
		contentWrapper.Refresh()
	} else {
		// Fallback: recreate the UI if references are lost
		setupUI()
	}
}
