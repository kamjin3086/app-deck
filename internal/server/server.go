package server

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/kamjin/app-deck/internal/catalog"
	"github.com/kamjin/app-deck/internal/scanner"
)

type Server struct {
	configPath string
	webFS      fs.FS
	mu         sync.Mutex
	prefs      catalog.Preferences
	scan       catalog.ScanResult
	iconCache  map[string]cachedIcon
}

func New(configPath string, webFS fs.FS) (*Server, error) {
	prefs, err := catalog.LoadPreferences(configPath)
	if err != nil {
		return nil, err
	}
	s := &Server{configPath: configPath, webFS: webFS, prefs: prefs, iconCache: map[string]cachedIcon{}}
	s.scan = scanner.ScanAll(prefs.AppsRoot)
	return s, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/apps", s.handleApps)
	mux.HandleFunc("POST /api/preferences", s.handlePreferences)
	mux.HandleFunc("POST /api/rescan", s.handleRescan)
	mux.HandleFunc("GET /api/export", s.handleExport)
	mux.HandleFunc("POST /api/import", s.handleImport)
	mux.HandleFunc("GET /api/icon", s.handleIcon)
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "app": "appdeck"})
	})
	mux.Handle("/", http.FileServer(http.FS(s.webFS)))
	return requestLog(mux)
}

func (s *Server) handleApps(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, http.StatusOK, catalog.Merge(s.prefs, s.scan))
}

func (s *Server) handlePreferences(w http.ResponseWriter, r *http.Request) {
	var prefs catalog.Preferences
	if err := json.NewDecoder(r.Body).Decode(&prefs); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid preferences JSON")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := catalog.SavePreferences(s.configPath, prefs); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.prefs = prefs
	writeJSON(w, http.StatusOK, catalog.Merge(s.prefs, s.scan))
}

func (s *Server) handleRescan(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scan = scanner.ScanAll(s.prefs.AppsRoot)
	writeJSON(w, http.StatusOK, catalog.Merge(s.prefs, s.scan))
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Disposition", `attachment; filename="appdeck.json"`)
	writeJSON(w, http.StatusOK, s.prefs)
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	var prefs catalog.Preferences
	if err := json.NewDecoder(r.Body).Decode(&prefs); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid import JSON")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := catalog.SavePreferences(s.configPath, prefs); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.prefs = prefs
	s.scan = scanner.ScanAll(s.prefs.AppsRoot)
	writeJSON(w, http.StatusOK, catalog.Merge(s.prefs, s.scan))
}

func EnsureInitialConfig(configPath string) error {
	if _, err := os.Stat(configPath); err == nil {
		return nil
	}
	prefs := catalog.DefaultPreferences()
	if migrated, ok := migrateLegacyNav(); ok {
		prefs = migrated
	}
	return catalog.SavePreferences(configPath, prefs)
}

func migrateLegacyNav() (catalog.Preferences, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return catalog.Preferences{}, false
	}
	path := filepath.Join(home, "apps", "apps-nav.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return catalog.Preferences{}, false
	}
	var legacy struct {
		Title      string `json:"title"`
		Categories []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Items []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
				URL  string `json:"url"`
				Note string `json:"note"`
				Path string `json:"path"`
			} `json:"items"`
		} `json:"categories"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return catalog.Preferences{}, false
	}
	prefs := catalog.DefaultPreferences()
	if legacy.Title != "" {
		prefs.Title = legacy.Title
	}
	if len(legacy.Categories) > 0 {
		prefs.Categories = nil
		for _, cat := range legacy.Categories {
			prefs.Categories = append(prefs.Categories, catalog.Category{ID: cat.ID, Name: cat.Name})
		}
	}
	order := 0
	for _, cat := range legacy.Categories {
		for _, item := range cat.Items {
			id := legacyItemID(item.ID, item.Path)
			name, urlValue, note, itemPath, category, itemOrder := item.Name, item.URL, item.Note, item.Path, cat.ID, order
			prefs.Overrides[id] = catalog.AppOverride{
				Name:       &name,
				URL:        &urlValue,
				Note:       &note,
				Path:       &itemPath,
				CategoryID: &category,
				Order:      &itemOrder,
			}
			if strings.HasPrefix(id, "manual:") {
				prefs.ManualApps = append(prefs.ManualApps, catalog.App{
					ID:         id,
					Name:       item.Name,
					URL:        item.URL,
					Note:       item.Note,
					Path:       item.Path,
					CategoryID: cat.ID,
					Source:     catalog.SourceManual,
				})
			}
			order++
		}
	}
	return prefs, true
}

func legacyItemID(oldID, itemPath string) string {
	home, _ := os.UserHomeDir()
	appsRoot := filepath.Join(home, "apps")
	if itemPath != "" {
		if rel, err := filepath.Rel(appsRoot, itemPath); err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
			return "path:" + strings.Split(rel, string(filepath.Separator))[0]
		}
	}
	switch oldID {
	case "go-music-dl":
		return "systemd:go-music-dl.service"
	default:
		if oldID != "" {
			return "manual:" + oldID
		}
		return "manual:legacy"
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}

func ListenAddr(host, port string) string {
	if host == "" {
		host = "127.0.0.1"
	}
	if port == "" {
		port = "8788"
	}
	return fmt.Sprintf("%s:%s", host, port)
}
