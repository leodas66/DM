package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/vishwa/dm/internal/api"
	"github.com/vishwa/dm/internal/manager"
	"github.com/vishwa/dm/internal/store"
	"github.com/vishwa/dm/internal/web"
)

// App struct
type App struct {
	ctx    context.Context
	mgr    *manager.Manager
	store  *store.Store
	apiSrv *http.Server
	addr   string
	token  string
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

	// Create the API server wrapped with the embedded web UI.
	// All routes (/api/* for REST, / for SPA) served by one listener.
	apiSrv := api.NewServer(a.mgr, a.token)
	handler := web.Handler(apiSrv)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		slog.Error("gui: failed to listen", "error", err)
		return
	}
	a.addr = "http://" + ln.Addr().String()
	a.apiSrv = &http.Server{Handler: handler}
	go func() {
		if err := a.apiSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("gui: server error", "error", err)
		}
	}()

	slog.Info("gui: desktop app started", "addr", a.addr)
}

// GetAPIURL returns the base URL of the local API server.
// Called by the frontend on startup to know where to fetch from.
func (a *App) GetAPIURL() string {
	return a.addr
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
