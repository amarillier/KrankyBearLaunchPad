package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// App represents an application that can be launched
type App struct {
	ID          string `json:"id"`          // Unique identifier
	Name        string `json:"name"`        // Display name
	Executable  string `json:"executable"`  // Path to executable
	Icon        string `json:"icon"`        // Path to icon (optional)
	Description string `json:"description"` // Description (optional)
	Category    string `json:"category"`    // Category (optional)
	IsCustom    bool   `json:"is_custom"`   // True if manually added by user
}

// Tab represents a tab/category in the launchpad
type Tab struct {
	ID     string   `json:"id"`      // Unique identifier
	Name   string   `json:"name"`    // Display name
	AppIDs []string `json:"app_ids"` // List of app IDs in this tab
	Color  string   `json:"color"`   // Tab color in hex format (e.g., "#FF0000" or empty for default)
}

// Config represents the application configuration
type Config struct {
	Tabs []Tab `json:"tabs"`
	Apps []App `json:"apps"`
}

// LoadConfig loads the configuration from a JSON file
func LoadConfig(configPath string) (*Config, error) {
	config := &Config{
		Tabs: []Tab{
			{ID: "home", Name: "Home", AppIDs: []string{}},
		},
		Apps: []App{},
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Return default config if file doesn't exist
			return config, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, config); err != nil {
		return nil, err
	}

	// Ensure home tab exists
	hasHome := false
	for _, tab := range config.Tabs {
		if tab.ID == "home" {
			hasHome = true
			break
		}
	}
	if !hasHome {
		config.Tabs = append([]Tab{{ID: "home", Name: "Home", AppIDs: []string{}}}, config.Tabs...)
	}

	return config, nil
}

// SaveConfig saves the configuration to a JSON file
func SaveConfig(config *Config, configPath string) error {
	// Ensure directory exists
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0644)
}

// GetConfigPath returns the path to the configuration file
func GetConfigPath() string {
	homeDir, _ := os.UserHomeDir()
	configDir := filepath.Join(homeDir, ".krankybear-launchpad")
	return filepath.Join(configDir, "config.json")
}

