package catalog

type Status string

const (
	StatusRunning  Status = "running"
	StatusStopped  Status = "stopped"
	StatusUnknown  Status = "unknown"
	StatusConflict Status = "conflict"
)

type Source string

const (
	SourcePath    Source = "path"
	SourceDocker  Source = "docker"
	SourceSystemd Source = "systemd"
	SourceManual  Source = "manual"
)

type App struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	URL         string   `json:"url,omitempty"`
	Note        string   `json:"note,omitempty"`
	Path        string   `json:"path,omitempty"`
	CategoryID  string   `json:"categoryId"`
	Status      Status   `json:"status"`
	Source      Source   `json:"source"`
	Ports       []string `json:"ports,omitempty"`
	Image       string   `json:"image,omitempty"`
	Service     string   `json:"service,omitempty"`
	Project     string   `json:"project,omitempty"`
	ComposeFile string   `json:"composeFile,omitempty"`
	Order       int      `json:"order"`
	Hidden      bool     `json:"hidden,omitempty"`
}

type Category struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ScanIssue struct {
	Source  string `json:"source"`
	Message string `json:"message"`
}

type Catalog struct {
	Title      string      `json:"title"`
	AppsRoot   string      `json:"appsRoot"`
	Categories []Category  `json:"categories"`
	Apps       []App       `json:"apps"`
	Issues     []ScanIssue `json:"issues,omitempty"`
}

type Preferences struct {
	Version    int                    `json:"version"`
	Title      string                 `json:"title"`
	AppsRoot   string                 `json:"appsRoot"`
	Categories []Category             `json:"categories"`
	Overrides  map[string]AppOverride `json:"overrides"`
	ManualApps []App                  `json:"manualApps"`
}

type AppOverride struct {
	Name       *string `json:"name,omitempty"`
	URL        *string `json:"url,omitempty"`
	Note       *string `json:"note,omitempty"`
	Path       *string `json:"path,omitempty"`
	CategoryID *string `json:"categoryId,omitempty"`
	Order      *int    `json:"order,omitempty"`
	Hidden     *bool   `json:"hidden,omitempty"`
}

type ScanResult struct {
	Apps   []App
	Issues []ScanIssue
}
