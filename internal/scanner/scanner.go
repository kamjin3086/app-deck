package scanner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/kamjin/app-deck/internal/catalog"
)

func ScanAll(appsRoot string) catalog.ScanResult {
	var result catalog.ScanResult
	add := func(part catalog.ScanResult) {
		result.Apps = append(result.Apps, part.Apps...)
		result.Issues = append(result.Issues, part.Issues...)
	}
	add(ScanAppsRoot(appsRoot))
	add(ScanDocker())
	add(ScanSystemd())
	result.Apps = reducePathDuplicates(result.Apps)
	return result
}

func ScanAppsRoot(appsRoot string) catalog.ScanResult {
	entries, err := os.ReadDir(appsRoot)
	if err != nil {
		return catalog.ScanResult{Issues: []catalog.ScanIssue{{Source: "apps", Message: err.Error()}}}
	}
	apps := []catalog.App{}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		path := filepath.Join(appsRoot, entry.Name())
		app := catalog.App{
			ID:     "path:" + entry.Name(),
			Name:   titleize(entry.Name()),
			Path:   path,
			Source: catalog.SourcePath,
			Status: catalog.StatusUnknown,
			Note:   inferNote(path),
			URL:    inferURLFromCompose(path),
		}
		app.CategoryID = catalog.Classify(app)
		apps = append(apps, app)
	}
	sort.Slice(apps, func(i, j int) bool { return strings.ToLower(apps[i].Name) < strings.ToLower(apps[j].Name) })
	return catalog.ScanResult{Apps: apps}
}

type dockerRow struct {
	Names  string `json:"Names"`
	Image  string `json:"Image"`
	Ports  string `json:"Ports"`
	Status string `json:"Status"`
	State  string `json:"State"`
	Labels string `json:"Labels"`
}

func ScanDocker() catalog.ScanResult {
	out, err := run(4*time.Second, "docker", "ps", "-a", "--format", "{{json .}}")
	if err != nil {
		return catalog.ScanResult{Issues: []catalog.ScanIssue{{Source: "docker", Message: err.Error()}}}
	}
	apps := []catalog.App{}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var row dockerRow
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		labels := parseLabels(row.Labels)
		project := firstNonEmpty(labels["com.docker.compose.project"], row.Names)
		service := firstNonEmpty(labels["com.docker.compose.service"], row.Names)
		workdir := labels["com.docker.compose.project.working_dir"]
		configFile := labels["com.docker.compose.project.config_files"]
		ports, urlValue := parsePublishedPorts(row.Ports)
		if urlValue == "" && workdir != "" {
			urlValue = inferURLFromCompose(workdir)
		}
		if urlValue == "" && len(ports) == 0 {
			continue
		}
		name := titleize(firstNonEmpty(project, row.Names))
		if project != "" && service != "" && service != project && service != "web" && service != "app" && service != "caddy" {
			name = name + " / " + titleize(service)
		}
		if strings.EqualFold(project, "subwave") {
			name = "SUB/WAVE"
		}
		app := catalog.App{
			ID:          fmt.Sprintf("docker:%s:%s", safeID(project), safeID(service)),
			Name:        name,
			URL:         urlValue,
			Note:        "Docker container: " + row.Names,
			Path:        workdir,
			CategoryID:  "",
			Status:      dockerStatus(row.State, row.Status),
			Source:      catalog.SourceDocker,
			Ports:       ports,
			Image:       row.Image,
			Service:     service,
			Project:     project,
			ComposeFile: configFile,
		}
		app.CategoryID = catalog.Classify(app)
		apps = append(apps, app)
	}
	if err := scanner.Err(); err != nil {
		return catalog.ScanResult{Apps: apps, Issues: []catalog.ScanIssue{{Source: "docker", Message: err.Error()}}}
	}
	return catalog.ScanResult{Apps: apps}
}

func ScanSystemd() catalog.ScanResult {
	out, err := run(4*time.Second, "systemctl", "--user", "list-units", "--type=service", "--all", "--no-legend", "--no-pager")
	if err != nil {
		return catalog.ScanResult{Issues: []catalog.ScanIssue{{Source: "systemd", Message: err.Error()}}}
	}
	apps := []catalog.App{}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 || !strings.HasSuffix(fields[0], ".service") {
			continue
		}
		unit := fields[0]
		if !looksUserApp(unit) {
			continue
		}
		show, _ := run(2*time.Second, "systemctl", "--user", "show", unit, "--property=Description,ExecStart,FragmentPath,ActiveState")
		props := parseSystemdProps(show)
		name := strings.TrimSpace(props["Description"])
		if name == "" {
			name = strings.TrimSuffix(unit, ".service")
		}
		app := catalog.App{
			ID:      "systemd:" + unit,
			Name:    name,
			Note:    "systemd user service",
			Path:    props["FragmentPath"],
			Status:  systemdStatus(firstNonEmpty(props["ActiveState"], fields[2])),
			Source:  catalog.SourceSystemd,
			Service: unit,
		}
		if strings.Contains(unit, "go-music-dl") {
			app.URL = "http://localhost:8081/music/"
		}
		app.CategoryID = catalog.Classify(app)
		apps = append(apps, app)
	}
	return catalog.ScanResult{Apps: apps}
}

func reducePathDuplicates(apps []catalog.App) []catalog.App {
	dockerPaths := map[string]bool{}
	for _, app := range apps {
		if app.Source == catalog.SourceDocker && app.Path != "" && app.URL != "" {
			dockerPaths[filepath.Clean(app.Path)] = true
		}
	}
	out := apps[:0]
	for _, app := range apps {
		if app.Source == catalog.SourcePath && dockerPaths[filepath.Clean(app.Path)] {
			continue
		}
		out = append(out, app)
	}
	return out
}

func inferNote(path string) string {
	if data, err := os.ReadFile(filepath.Join(path, "package.json")); err == nil {
		var pkg struct {
			Description string `json:"description"`
		}
		if json.Unmarshal(data, &pkg) == nil && pkg.Description != "" {
			return pkg.Description
		}
	}
	if data, err := os.ReadFile(filepath.Join(path, "README.md")); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(strings.Trim(line, "# "))
			if line != "" && len([]rune(line)) < 120 {
				return line
			}
		}
	}
	if hasCompose(path) {
		return "Compose app"
	}
	return ""
}

func inferURLFromCompose(path string) string {
	files := []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"}
	re := regexp.MustCompile(`["']?([0-9]{2,5}):([0-9]{2,5})["']?`)
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(path, name))
		if err != nil {
			continue
		}
		matches := re.FindAllStringSubmatch(string(data), -1)
		for _, match := range matches {
			port := match[1]
			if skipPort(port) {
				continue
			}
			scheme := "http"
			if port == "443" || port == "9443" {
				scheme = "https"
			}
			return scheme + "://localhost:" + port
		}
	}
	return ""
}

func hasCompose(path string) bool {
	for _, name := range []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"} {
		if _, err := os.Stat(filepath.Join(path, name)); err == nil {
			return true
		}
	}
	return false
}

func parsePublishedPorts(raw string) ([]string, string) {
	parts := strings.Split(raw, ",")
	ports := []string{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if !strings.Contains(part, "->") {
			continue
		}
		host := part
		if idx := strings.Index(host, "->"); idx >= 0 {
			host = host[:idx]
		}
		host = strings.Trim(host, "[]")
		port := host[strings.LastIndex(host, ":")+1:]
		if port == "" || skipPort(port) {
			continue
		}
		if !contains(ports, port) {
			ports = append(ports, port)
		}
	}
	sort.Strings(ports)
	if len(ports) == 0 {
		return ports, ""
	}
	chosen := ports[0]
	if contains(ports, "9443") {
		chosen = "9443"
	}
	scheme := "http"
	if chosen == "443" || chosen == "9443" {
		scheme = "https"
	}
	return ports, scheme + "://localhost:" + chosen
}

func parseLabels(raw string) map[string]string {
	labels := map[string]string{}
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key, value, ok := strings.Cut(item, "=")
		if ok {
			labels[key] = value
		}
	}
	return labels
}

func parseSystemdProps(raw []byte) map[string]string {
	props := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			props[key] = value
		}
	}
	return props
}

func run(timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("%s timed out", name)
	}
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return out, fmt.Errorf("%s: %s", name, strings.TrimSpace(string(exit.Stderr)))
		}
		return out, err
	}
	return out, nil
}

func dockerStatus(state, status string) catalog.Status {
	value := strings.ToLower(state + " " + status)
	if strings.Contains(value, "running") || strings.Contains(value, "up ") {
		return catalog.StatusRunning
	}
	if strings.Contains(value, "exited") || strings.Contains(value, "created") {
		return catalog.StatusStopped
	}
	return catalog.StatusUnknown
}

func systemdStatus(value string) catalog.Status {
	switch strings.ToLower(value) {
	case "active", "running":
		return catalog.StatusRunning
	case "inactive", "failed", "dead":
		return catalog.StatusStopped
	default:
		return catalog.StatusUnknown
	}
}

func looksUserApp(unit string) bool {
	keep := []string{"appdeck", "go-music-dl", "llama", "opencode", "hermes", "aria2"}
	for _, token := range keep {
		if strings.Contains(strings.ToLower(unit), token) {
			return true
		}
	}
	return false
}

func titleize(value string) string {
	brand := map[string]string{
		"appdeck":     "AppDeck",
		"crawl4ai":    "Crawl4AI",
		"go-music-dl": "Go Music DL",
		"navidrome":   "Navidrome",
		"openwebui":   "Open WebUI",
		"open-webui":  "Open WebUI",
		"pansou":      "PanSou",
		"portainer":   "Portainer",
		"qwen3-tts":   "Qwen3 TTS",
		"searxng":     "SearXNG",
		"sillytavern": "SillyTavern",
		"subwave":     "SUB/WAVE",
	}
	if name, ok := brand[strings.ToLower(value)]; ok {
		return name
	}
	value = strings.TrimSuffix(value, ".service")
	value = strings.ReplaceAll(value, "_", " ")
	value = strings.ReplaceAll(value, "-", " ")
	words := strings.Fields(value)
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(word[:1]) + word[1:]
		}
	}
	if len(words) == 0 {
		return value
	}
	return strings.Join(words, " ")
}

func safeID(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "unknown"
	}
	value = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '-'
	}, value)
	return strings.Trim(value, "-")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func skipPort(port string) bool {
	switch port {
	case "22", "53", "631", "6379", "2019", "3389", "3390", "5432", "9222":
		return true
	default:
		return false
	}
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func normalizeURL(raw string) string {
	if raw == "" {
		return ""
	}
	if _, err := url.Parse(raw); err != nil {
		return ""
	}
	return raw
}

var _ = normalizeURL
