package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/vishwa/dm/internal/api"
	"github.com/vishwa/dm/internal/manager"
	"github.com/vishwa/dm/internal/model"
	"github.com/vishwa/dm/internal/store"
)

// App struct
type App struct {
	ctx    context.Context
	mgr    *manager.Manager
	store  *store.Store
	apiSrv *http.Server
	token  string
	addr   string
}

type DownloadRow struct {
	ID         string `json:"id"`
	URL        string `json:"url"`
	Filename   string `json:"filename"`
	Dir        string `json:"dir"`
	CategoryID string `json:"categoryId,omitempty"`
	Size       int64  `json:"size"`
	Status     string `json:"status"`
	Progress   int    `json:"progress"`
	Error      string `json:"error,omitempty"`
	CreatedAt  string `json:"createdAt"`
}

type CategoryRow struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	DefaultDir string `json:"defaultDir"`
}

type SettingsRow struct {
	Dir              string `json:"dir"`
	MaxConcurrent    int    `json:"maxConcurrent"`
	Connections      int    `json:"connections"`
	MinSegmentSize   int64  `json:"minSegmentSize"`
	GlobalLimit      int64  `json:"globalLimit"`
	PerDownloadLimit int64  `json:"perDownloadLimit"`
	SaveInterval     string `json:"saveInterval"`
}

type AddDownloadRequest struct {
	URL        string `json:"url"`
	Filename   string `json:"filename,omitempty"`
	Dir        string `json:"dir,omitempty"`
	CategoryID string `json:"categoryId,omitempty"`
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// startup is called when the app starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// Open the SQLite store under the per-user config dir.
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		slog.Error("gui: failed to get config dir", "error", err)
		return
	}
	dmDir := filepath.Join(cfgDir, "dm")
	if err := os.MkdirAll(dmDir, 0o700); err != nil {
		slog.Error("gui: failed to create config dir", "error", err)
		return
	}
	a.store, err = store.Open(filepath.Join(dmDir, "dm.db"))
	if err != nil {
		slog.Error("gui: failed to open store", "error", err)
		return
	}

	// Create and start the Manager.
	a.mgr = manager.New(a.store, manager.Settings{}, nil)
	if err := a.mgr.Start(ctx); err != nil {
		slog.Error("gui: failed to start manager", "error", err)
		return
	}

	// Load or create the bearer token.
	a.token, err = api.LoadOrCreateToken()
	if err != nil {
		slog.Error("gui: failed to load/create token", "error", err)
		return
	}

	// Create the API server on localhost (for the web UI fallback).
	apiSrv := api.NewServer(a.mgr, a.token)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		slog.Error("gui: failed to listen", "error", err)
		return
	}
	a.addr = ln.Addr().String()
	a.apiSrv = &http.Server{Handler: apiSrv}
	go func() {
		if err := a.apiSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("gui: api server error", "error", err)
		}
	}()

	slog.Info("gui: desktop app started", "addr", a.addr)
}

// shutdown is called when the app closes.
func (a *App) shutdown(ctx context.Context) {
	if a.apiSrv != nil {
		_ = a.apiSrv.Close()
	}
	if a.mgr != nil {
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.mgr.Shutdown(sctx)
	}
	if a.store != nil {
		_ = a.store.Close()
	}
}

// ---- Download bindings ----

func (a *App) ListDownloads() (string, error) {
	list, err := a.mgr.List(a.ctx)
	if err != nil {
		return "", err
	}
	rows := make([]DownloadRow, 0, len(list))
	for _, d := range list {
		progress := 0
		if d.Size > 0 {
			var total int64
			for _, s := range d.Segments {
				total += s.Downloaded
			}
			progress = int(float64(total) / float64(d.Size) * 100)
		}
		rows = append(rows, DownloadRow{
			ID:         d.ID,
			URL:        d.URL,
			Filename:   d.Filename,
			Dir:        d.Dir,
			CategoryID: d.CategoryID,
			Size:       d.Size,
			Status:     string(d.Status),
			Progress:   progress,
			Error:      d.Error,
			CreatedAt:  d.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	b, _ := json.Marshal(rows)
	return string(b), nil
}

func (a *App) AddDownload(req AddDownloadRequest) (string, error) {
	d, err := a.mgr.Add(a.ctx, manager.AddRequest{
		URL:        req.URL,
		Filename:   req.Filename,
		Dir:        req.Dir,
		CategoryID: req.CategoryID,
	})
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(d)
	return string(b), nil
}

func (a *App) PauseDownload(id string) (string, error) {
	d, err := a.mgr.Pause(a.ctx, id)
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(d)
	return string(b), nil
}

func (a *App) ResumeDownload(id string) (string, error) {
	d, err := a.mgr.Resume(a.ctx, id)
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(d)
	return string(b), nil
}

func (a *App) CancelDownload(id string) (string, error) {
	d, err := a.mgr.Cancel(a.ctx, id)
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(d)
	return string(b), nil
}

func (a *App) RemoveDownload(id string, deleteFile bool) error {
	return a.mgr.Remove(a.ctx, id, deleteFile)
}

// ---- Settings bindings ----

func (a *App) GetSettings() (string, error) {
	s := a.mgr.Settings()
	row := SettingsRow{
		Dir:              s.Dir,
		MaxConcurrent:    s.MaxConcurrent,
		Connections:      s.Connections,
		MinSegmentSize:   s.MinSegmentSize,
		GlobalLimit:      s.GlobalLimit,
		PerDownloadLimit: s.PerDownloadLimit,
		SaveInterval:     s.SaveInterval.String(),
	}
	b, _ := json.Marshal(row)
	return string(b), nil
}

func (a *App) UpdateSettings(jsonStr string) (string, error) {
	var s SettingsRow
	if err := json.Unmarshal([]byte(jsonStr), &s); err != nil {
		return "", err
	}
	settings := manager.Settings{
		Dir:              s.Dir,
		MaxConcurrent:    s.MaxConcurrent,
		Connections:      s.Connections,
		MinSegmentSize:   s.MinSegmentSize,
		GlobalLimit:      s.GlobalLimit,
		PerDownloadLimit: s.PerDownloadLimit,
	}
	if d, err := time.ParseDuration(s.SaveInterval); err == nil {
		settings.SaveInterval = d
	}
	a.mgr.UpdateSettings(settings)
	b, _ := json.Marshal(settings)
	return string(b), nil
}

// ---- Category bindings ----

func (a *App) ListCategories() (string, error) {
	list, err := a.mgr.ListCategories(a.ctx)
	if err != nil {
		return "", err
	}
	rows := make([]CategoryRow, 0, len(list))
	for _, c := range list {
		rows = append(rows, CategoryRow{
			ID:         c.ID,
			Name:       c.Name,
			DefaultDir: c.DefaultDir,
		})
	}
	b, _ := json.Marshal(rows)
	return string(b), nil
}

func (a *App) AddCategory(name, defaultDir string) (string, error) {
	c, err := a.mgr.AddCategory(a.ctx, model.Category{
		ID:         manager.NewID(),
		Name:       name,
		DefaultDir: defaultDir,
		CreatedAt:  time.Now().UTC(),
	})
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(c)
	return string(b), nil
}

func (a *App) DeleteCategory(id string) error {
	return a.mgr.DeleteCategory(a.ctx, id)
}
