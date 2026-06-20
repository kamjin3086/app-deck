package catalog

import (
	"sort"
	"strings"
)

func Merge(prefs Preferences, scan ScanResult) Catalog {
	normalizePreferences(&prefs)
	seen := map[string]bool{}
	apps := make([]App, 0, len(scan.Apps)+len(prefs.ManualApps))
	for _, app := range scan.Apps {
		applyDefaults(&app)
		if override, ok := prefs.Overrides[app.ID]; ok {
			applyOverride(&app, override)
		}
		if !app.Hidden {
			apps = append(apps, app)
		}
		seen[app.ID] = true
	}
	for _, app := range prefs.ManualApps {
		if app.ID == "" {
			continue
		}
		app.Source = SourceManual
		app.Status = StatusUnknown
		applyDefaults(&app)
		if override, ok := prefs.Overrides[app.ID]; ok {
			applyOverride(&app, override)
		}
		if !app.Hidden {
			apps = append(apps, app)
		}
		seen[app.ID] = true
	}
	sort.SliceStable(apps, func(i, j int) bool {
		if apps[i].CategoryID != apps[j].CategoryID {
			return categoryIndex(prefs.Categories, apps[i].CategoryID) < categoryIndex(prefs.Categories, apps[j].CategoryID)
		}
		if apps[i].Order != apps[j].Order {
			return apps[i].Order < apps[j].Order
		}
		return strings.ToLower(apps[i].Name) < strings.ToLower(apps[j].Name)
	})
	markConflicts(apps)
	return Catalog{
		Title:      prefs.Title,
		AppsRoot:   prefs.AppsRoot,
		Categories: prefs.Categories,
		Apps:       apps,
		Issues:     scan.Issues,
	}
}

func markConflicts(apps []App) {
	urlCounts := map[string]int{}
	for _, app := range apps {
		if app.URL != "" {
			urlCounts[app.URL]++
		}
	}
	for i := range apps {
		if apps[i].URL != "" && urlCounts[apps[i].URL] > 1 && apps[i].Status == StatusUnknown {
			apps[i].Status = StatusConflict
		}
	}
}

func applyDefaults(app *App) {
	if app.CategoryID == "" {
		app.CategoryID = Classify(*app)
	}
	if app.Status == "" {
		app.Status = StatusUnknown
	}
}

func applyOverride(app *App, override AppOverride) {
	if override.Name != nil {
		app.Name = *override.Name
	}
	if override.URL != nil {
		app.URL = *override.URL
	}
	if override.Note != nil {
		app.Note = *override.Note
	}
	if override.Path != nil {
		app.Path = *override.Path
	}
	if override.CategoryID != nil {
		app.CategoryID = *override.CategoryID
	}
	if override.Order != nil {
		app.Order = *override.Order
	}
	if override.Hidden != nil {
		app.Hidden = *override.Hidden
	}
}

func categoryIndex(categories []Category, id string) int {
	for i, cat := range categories {
		if cat.ID == id {
			return i
		}
	}
	return len(categories) + 1
}

func Classify(app App) string {
	hay := strings.ToLower(strings.Join([]string{app.Name, app.ID, app.Note, app.Image, app.Service, app.Project, app.Path}, " "))
	switch {
	case containsAny(hay, "navidrome", "subwave", "sub-wave", "music", "go-music-dl"):
		if strings.Contains(hay, "tts") {
			return "ai"
		}
		return "music"
	case containsAny(hay, "portainer", "prowlarr", "arr-stack", "jackett", "caddy", "redis"):
		return "ops"
	case containsAny(hay, "openwebui", "open-webui", "llm", "qwen", "draw", "stable", "comfy", "sillytavern"):
		return "ai"
	case containsAny(hay, "crawl", "search", "searx", "pansou", "firecrawl", "deep-searcher"):
		return "search"
	case containsAny(hay, "skyvern", "opencode", "github", "automation"):
		return "dev"
	default:
		return "pending"
	}
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
