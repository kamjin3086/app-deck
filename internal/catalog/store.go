package catalog

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

func DefaultConfigDir() (string, error) {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "appdeck"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "appdeck"), nil
}

func DefaultAppsRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/home/kamjin/apps"
	}
	return filepath.Join(home, "apps")
}

func DefaultPreferences() Preferences {
	return Preferences{
		Version:  1,
		Title:    "AppDeck",
		AppsRoot: DefaultAppsRoot(),
		Categories: []Category{
			{ID: "music", Name: "音乐与媒体"},
			{ID: "ai", Name: "AI 与创作"},
			{ID: "search", Name: "搜索与采集"},
			{ID: "ops", Name: "运维与工具"},
			{ID: "dev", Name: "开发与自动化"},
			{ID: "pending", Name: "待补充"},
		},
		Overrides:  map[string]AppOverride{},
		ManualApps: []App{},
	}
}

func LoadPreferences(path string) (Preferences, error) {
	prefs := DefaultPreferences()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return prefs, nil
		}
		return prefs, err
	}
	if err := json.Unmarshal(data, &prefs); err != nil {
		return prefs, err
	}
	normalizePreferences(&prefs)
	return prefs, nil
}

func SavePreferences(path string, prefs Preferences) error {
	normalizePreferences(&prefs)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func normalizePreferences(prefs *Preferences) {
	if prefs.Version == 0 {
		prefs.Version = 1
	}
	if prefs.Title == "" {
		prefs.Title = "AppDeck"
	}
	if prefs.AppsRoot == "" {
		prefs.AppsRoot = DefaultAppsRoot()
	}
	if len(prefs.Categories) == 0 {
		prefs.Categories = DefaultPreferences().Categories
	}
	if prefs.Overrides == nil {
		prefs.Overrides = map[string]AppOverride{}
	}
	if prefs.ManualApps == nil {
		prefs.ManualApps = []App{}
	}
}
