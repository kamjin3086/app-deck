package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	iconMaxHTMLBytes  = 512 * 1024
	iconMaxImageBytes = 1024 * 1024
)

type cachedIcon struct {
	Data      []byte
	MimeType  string
	ExpiresAt time.Time
}

type iconCandidate struct {
	URL   string
	Rank  int
	Score int
}

var (
	linkTagRe = regexp.MustCompile(`(?is)<link\s+[^>]*>`)
	attrRe    = regexp.MustCompile(`(?is)([a-z0-9:-]+)\s*=\s*("([^"]*)"|'([^']*)'|([^\s>]+))`)
)

func (s *Server) handleIcon(w http.ResponseWriter, r *http.Request) {
	rawURL := strings.TrimSpace(r.URL.Query().Get("url"))
	if rawURL == "" {
		http.NotFound(w, r)
		return
	}
	data, mimeType, err := s.iconForURL(rawURL)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Content-Type", mimeType)
	_, _ = w.Write(data)
}

func (s *Server) iconForURL(rawURL string) ([]byte, string, error) {
	now := time.Now()
	s.mu.Lock()
	if cached, ok := s.iconCache[rawURL]; ok && cached.ExpiresAt.After(now) {
		s.mu.Unlock()
		return cached.Data, cached.MimeType, nil
	}
	s.mu.Unlock()

	baseURL, err := parseHTTPURL(rawURL)
	if err != nil {
		return nil, "", err
	}
	client := &http.Client{Timeout: 1800 * time.Millisecond}
	candidates := discoverIconCandidates(client, baseURL)
	for _, candidate := range candidates {
		data, mimeType, err := fetchIcon(client, candidate.URL)
		if err == nil {
			s.mu.Lock()
			s.iconCache[rawURL] = cachedIcon{Data: data, MimeType: mimeType, ExpiresAt: now.Add(time.Hour)}
			s.mu.Unlock()
			return data, mimeType, nil
		}
	}
	return nil, "", errors.New("no icon found")
}

func discoverIconCandidates(client *http.Client, baseURL *url.URL) []iconCandidate {
	candidates := []iconCandidate{}
	html, pageURL, err := fetchPageHTML(client, baseURL.String())
	if err == nil {
		links := extractLinks(html, pageURL)
		for _, manifestURL := range links["manifest"] {
			candidates = append(candidates, manifestIconCandidates(client, manifestURL)...)
		}
		for _, iconURL := range links["apple-touch-icon"] {
			candidates = append(candidates, iconCandidate{URL: iconURL, Rank: 20, Score: 180})
		}
		for _, iconURL := range links["icon"] {
			candidates = append(candidates, iconCandidate{URL: iconURL, Rank: 30, Score: scoreIconURL(iconURL)})
		}
		for _, iconURL := range links["mask-icon"] {
			candidates = append(candidates, iconCandidate{URL: iconURL, Rank: 40, Score: 64})
		}
	}
	candidates = append(candidates,
		iconCandidate{URL: resolveURL(baseURL, "/apple-touch-icon.png"), Rank: 80, Score: 180},
		iconCandidate{URL: resolveURL(baseURL, "/favicon.svg"), Rank: 90, Score: 64},
		iconCandidate{URL: resolveURL(baseURL, "/favicon.png"), Rank: 91, Score: 48},
		iconCandidate{URL: resolveURL(baseURL, "/favicon.ico"), Rank: 92, Score: 32},
	)
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Rank != candidates[j].Rank {
			return candidates[i].Rank < candidates[j].Rank
		}
		return candidates[i].Score > candidates[j].Score
	})
	return dedupeCandidates(candidates)
}

func fetchPageHTML(client *http.Client, rawURL string) (string, *url.URL, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("User-Agent", "AppDeck/1.0")
	res, err := client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 400 {
		return "", nil, errors.New("page request failed")
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, iconMaxHTMLBytes))
	if err != nil {
		return "", nil, err
	}
	return string(body), res.Request.URL, nil
}

func extractLinks(html string, pageURL *url.URL) map[string][]string {
	links := map[string][]string{}
	for _, tag := range linkTagRe.FindAllString(html, -1) {
		attrs := parseTagAttrs(tag)
		rel := strings.ToLower(attrs["rel"])
		href := attrs["href"]
		if rel == "" || href == "" {
			continue
		}
		resolved := resolveURL(pageURL, href)
		for _, part := range strings.Fields(rel) {
			switch part {
			case "icon", "apple-touch-icon", "mask-icon", "manifest":
				links[part] = append(links[part], resolved)
			}
		}
		if strings.Contains(rel, "shortcut") && strings.Contains(rel, "icon") {
			links["icon"] = append(links["icon"], resolved)
		}
	}
	return links
}

func parseTagAttrs(tag string) map[string]string {
	attrs := map[string]string{}
	for _, match := range attrRe.FindAllStringSubmatch(tag, -1) {
		value := match[3]
		if value == "" {
			value = match[4]
		}
		if value == "" {
			value = match[5]
		}
		attrs[strings.ToLower(match[1])] = strings.TrimSpace(value)
	}
	return attrs
}

func manifestIconCandidates(client *http.Client, manifestURL string) []iconCandidate {
	req, err := http.NewRequest(http.MethodGet, manifestURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "AppDeck/1.0")
	res, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 400 {
		return nil
	}
	var manifest struct {
		Icons []struct {
			Src   string `json:"src"`
			Sizes string `json:"sizes"`
			Type  string `json:"type"`
		} `json:"icons"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, iconMaxHTMLBytes)).Decode(&manifest); err != nil {
		return nil
	}
	base := res.Request.URL
	candidates := []iconCandidate{}
	for _, icon := range manifest.Icons {
		if icon.Src == "" {
			continue
		}
		candidates = append(candidates, iconCandidate{
			URL:   resolveURL(base, icon.Src),
			Rank:  10,
			Score: maxManifestSize(icon.Sizes),
		})
	}
	return candidates
}

func fetchIcon(client *http.Client, rawURL string) ([]byte, string, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "AppDeck/1.0")
	res, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 400 {
		return nil, "", errors.New("icon request failed")
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, iconMaxImageBytes))
	if err != nil {
		return nil, "", err
	}
	mimeType := res.Header.Get("Content-Type")
	if idx := strings.Index(mimeType, ";"); idx >= 0 {
		mimeType = mimeType[:idx]
	}
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = http.DetectContentType(data)
	}
	if !isImageMime(mimeType, rawURL, data) {
		return nil, "", errors.New("not an image")
	}
	if mimeType == "image/x-icon" || mimeType == "image/vnd.microsoft.icon" {
		mimeType = "image/x-icon"
	}
	return data, mimeType, nil
}

func parseHTTPURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme == "" {
		parsed.Scheme = "http"
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("unsupported icon URL")
	}
	if parsed.Host == "" {
		return nil, errors.New("missing host")
	}
	return parsed, nil
}

func resolveURL(base *url.URL, raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return base.ResolveReference(parsed).String()
}

func maxManifestSize(sizes string) int {
	best := 0
	for _, size := range strings.Fields(sizes) {
		parts := strings.Split(strings.ToLower(size), "x")
		if len(parts) != 2 {
			continue
		}
		width, _ := strconv.Atoi(parts[0])
		height, _ := strconv.Atoi(parts[1])
		if width > 0 && height > 0 && width*height > best {
			best = width * height
		}
	}
	if best == 0 {
		return 1
	}
	return best
}

func scoreIconURL(rawURL string) int {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return 1
	}
	ext := strings.ToLower(path.Ext(parsed.Path))
	switch ext {
	case ".svg":
		return 512
	case ".png", ".webp":
		return 256
	case ".ico":
		return 32
	default:
		return 64
	}
}

func dedupeCandidates(candidates []iconCandidate) []iconCandidate {
	seen := map[string]bool{}
	out := make([]iconCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.URL == "" || seen[candidate.URL] {
			continue
		}
		seen[candidate.URL] = true
		out = append(out, candidate)
	}
	return out
}

func isImageMime(mimeType, rawURL string, data []byte) bool {
	if strings.HasPrefix(mimeType, "image/") {
		return true
	}
	ext := strings.ToLower(path.Ext(rawURL))
	if mime.TypeByExtension(ext) != "" && strings.HasPrefix(mime.TypeByExtension(ext), "image/") {
		return true
	}
	return bytes.HasPrefix(bytes.TrimSpace(data), []byte("<svg"))
}
