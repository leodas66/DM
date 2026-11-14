// Command dm is the download manager daemon.
//
// Usage:
//
//	dm <url>              one-shot download to disk (Phase 1 behavior)
//	dm --serve            run as daemon: API + web UI on 127.0.0.1
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/vishwa/dm/internal/api"
	"github.com/vishwa/dm/internal/download"
	"github.com/vishwa/dm/internal/manager"
	"github.com/vishwa/dm/internal/probe"
	"github.com/vishwa/dm/internal/runtime"
	"github.com/vishwa/dm/internal/store"
	"github.com/vishwa/dm/internal/web"
)

const version = "0.1.0-dev"

func main() {
	fs := flag.NewFlagSet("dm", flag.ExitOnError)
	out := fs.String("o", "", "output file name (default: name from the server)")
	dir := fs.String("d", ".", "destination directory")
	segments := fs.Int("n", 8, "number of segments")
	tries := fs.Int("tries", 5, "attempts per segment")
	timeout := fs.Duration("timeout", 0, "overall timeout (0 = none)")
	showVersion := fs.Bool("version", false, "print version and exit")
	serveMode := fs.Bool("serve", false, "run as daemon with API and web UI")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: dm [flags] <url>\n\n")
		fs.PrintDefaults()
	}
	_ = fs.Parse(os.Args[1:])

	if *showVersion {
		fmt.Printf("dm %s\n", version)
		return
	}

	if *serveMode {
		if err := runServe(); err != nil {
			fmt.Fprintf(os.Stderr, "dm: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(2)
	}
	if err := run(fs.Arg(0), *dir, *out, *segments, *tries, *timeout); err != nil {
		fmt.Fprintf(os.Stderr, "dm: %v\n", err)
		os.Exit(1)
	}
}

// runServe starts the daemon: SQLite store, Manager, API server, and the
// embedded web UI, all on a single 127.0.0.1 listener. It runs until
// SIGINT/SIGTERM, then shuts down gracefully.
func runServe() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Open the SQLite store under the per-user config dir.
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	dmDir := filepath.Join(cfgDir, "dm")
	if err := os.MkdirAll(dmDir, 0o700); err != nil {
		return err
	}
	st, err := store.Open(filepath.Join(dmDir, "dm.db"))
	if err != nil {
		return err
	}
	defer st.Close()

	// Create and start the Manager.
	mgr := manager.New(st, manager.Settings{}, nil)
	if err := mgr.Start(ctx); err != nil {
		return err
	}

	// Load or create the bearer token (mode 0600).
	token, err := api.LoadOrCreateToken()
	if err != nil {
		return err
	}

	// Create the API server and wrap it with the embedded web UI. This path
	// serves with its own http.Server, so it must start event broadcasting
	// itself; ListenAndServe is not called here.
	apiSrv := api.NewServer(mgr, token)
	apiSrv.Broadcast(ctx)
	handler := web.Handler(apiSrv)

	// Listen on loopback only.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	addr := ln.Addr().String()

	// Publish the loopback address so local clients (GTK app, native host)
	// can find the daemon without being told a port.
	endpointPath, err := runtime.Publish(addr)
	if err != nil {
		ln.Close()
		return err
	}
	defer runtime.Remove()

	uiURL := fmt.Sprintf("http://%s/?token=%s", addr, token)
	fmt.Printf("dm daemon listening on http://%s\n", addr)
	fmt.Printf("Endpoint metadata: %s\n", endpointPath)
	fmt.Printf("Bearer token: %s\n", token)
	fmt.Printf("Opening UI at %s\n", uiURL)

	srv := &http.Server{Handler: handler}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "error", err)
		}
	}()

	// Open the UI in a browser window so dm feels like a desktop app.
	go openBrowser(uiURL)

	// Wait for shutdown signal.
	<-ctx.Done()
	srv.Close()
	return mgr.Shutdown(context.Background())
}

// openBrowser tries to open url in an app-style browser window. It never
// fails the daemon: chromium --app is preferred (windowed, no chrome), then
// xdg-open as a fallback, and any failure is silently ignored.
func openBrowser(url string) {
	browsers := [][]string{
		{"chromium", "--app=" + url},
		{"chromium-browser", "--app=" + url},
		{"google-chrome", "--app=" + url},
		{"google-chrome-stable", "--app=" + url},
	}
	for _, b := range browsers {
		if err := exec.Command(b[0], b[1:]...).Start(); err == nil {
			return
		}
	}
	// Fallback to the desktop's default handler.
	_ = exec.Command("xdg-open", url).Start()
}

func run(rawURL, dir, outName string, segments, tries int, timeout time.Duration) error {
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	client := probe.NewClient()
	fmt.Fprintf(os.Stderr, "probing %s\n", rawURL)
	info, err := probe.Probe(ctx, client, rawURL, nil)
	if err != nil {
		return err
	}

	name := info.Filename
	if outName != "" {
		name = probe.SanitizeFilename(outName)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	dest := filepath.Join(dir, name)
	// Defense in depth: name is sanitized, but never let a joined path escape.
	if absDir, err := filepath.Abs(dir); err == nil {
		if absDest, err := filepath.Abs(dest); err != nil || !strings.HasPrefix(absDest, absDir+string(os.PathSeparator)) {
			return fmt.Errorf("refusing to write outside %s", absDir)
		}
	}

	kind := "single stream"
	if info.AcceptsRanges && info.Size > 0 {
		kind = fmt.Sprintf("%d segments", segments)
	}
	size := "unknown size"
	if info.Size > 0 {
		size = humanBytes(info.Size)
	}
	fmt.Fprintf(os.Stderr, "%s, %s -> %s\n", kind, size, dest)

	events := make(chan download.Event, 16)
	go printProgress(events)

	cfg := download.Config{
		URL:           info.URL,
		FinalURL:      info.FinalURL,
		Size:          info.Size,
		AcceptsRanges: info.AcceptsRanges,
		ETag:          info.ETag,
		LastModified:  info.LastModified,
	}
	err = download.Run(ctx, cfg, dest, download.Options{
		Segments: segments,
		MaxTries: tries,
		Client:   client,
	}, events)
	close(events)
	fmt.Fprintf(os.Stderr, "\n")
	if err != nil {
		return err
	}
	fmt.Println(dest)
	return nil
}

// printProgress renders one updating line on stderr. It is deliberately dumb:
// the daemon will own live progress later, this is for the CLI only.
func printProgress(events <-chan download.Event) {
	var lastBytes int64
	var lastAt = time.Now()
	var speed float64

	for ev := range events {
		now := time.Now()
		if dt := now.Sub(lastAt).Seconds(); dt > 0 {
			inst := float64(ev.Downloaded-lastBytes) / dt
			if speed == 0 {
				speed = inst
			} else {
				speed = 0.7*speed + 0.3*inst
			}
		}
		lastBytes, lastAt = ev.Downloaded, now

		if ev.Type == download.EventDone {
			fmt.Fprintf(os.Stderr, "done: %s\n", humanBytes(ev.Downloaded))
			continue
		}
		if ev.Total > 0 {
			pct := float64(ev.Downloaded) / float64(ev.Total) * 100
			eta := "?"
			if speed > 1024 {
				eta = (time.Duration(float64(ev.Total-ev.Downloaded)/speed) * time.Second).Round(time.Second).String()
			}
			fmt.Fprintf(os.Stderr, "\r%5.1f%%  %s / %s  %s/s  eta %s",
				pct, humanBytes(ev.Downloaded), humanBytes(ev.Total), humanBytes(int64(speed)), eta)
			continue
		}
		fmt.Fprintf(os.Stderr, "\r%s  %s/s", humanBytes(ev.Downloaded), humanBytes(int64(speed)))
	}
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
