// Package runtime publishes where the running daemon listens on loopback, so
// local clients (the GTK desktop app, dm-native-host) can find it without
// being told a port. The API still binds an ephemeral port and keeps its own
// bearer token; this package only advertises the address.
//
// The metadata file lives next to other per-session state: under
// $XDG_RUNTIME_DIR/dm/endpoint, falling back to $TMPDIR/dm-<uid>/endpoint when
// XDG_RUNTIME_DIR is unset. It is written atomically with mode 0600 inside a
// 0700 directory and removed on clean shutdown. A stale file can survive a
// crash, so readers must treat a failed connection as "daemon not running"
// rather than as proof that the address is live.
package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// Endpoint is the metadata a daemon publishes about its API listener.
type Endpoint struct {
	// Addr is the loopback listen address, e.g. "127.0.0.1:41234".
	Addr string `json:"addr"`

	// PID is the daemon process that published the address. Clients can use
	// it to explain a stale file, but must not signal the process.
	PID int `json:"pid"`
}

// Dir returns the directory that holds runtime metadata for this user:
// $XDG_RUNTIME_DIR/dm when set, otherwise $TMPDIR/dm-<uid>.
func Dir() (string, error) {
	if base := os.Getenv("XDG_RUNTIME_DIR"); base != "" {
		return filepath.Join(base, "dm"), nil
	}
	tmp := os.TempDir()
	if tmp == "" {
		return "", errors.New("runtime: no XDG_RUNTIME_DIR or temp directory")
	}
	return filepath.Join(tmp, "dm-"+strconv.Itoa(os.Getuid())), nil
}

// Path returns the full path of the endpoint metadata file.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "endpoint"), nil
}

// Publish records addr as the running daemon's API address and returns the
// path it wrote. The write is atomic: readers see either the previous file or
// the complete new one, never a partial line.
func Publish(addr string) (string, error) {
	if addr == "" {
		return "", errors.New("runtime: empty listen address")
	}
	path, err := Path()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("runtime: create %s: %w", dir, err)
	}

	data, err := json.Marshal(Endpoint{Addr: addr, PID: os.Getpid()})
	if err != nil {
		return "", fmt.Errorf("runtime: encode endpoint: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, ".endpoint-*")
	if err != nil {
		return "", fmt.Errorf("runtime: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	// Best effort: remove the temp file if any step below fails.
	defer func() {
		if tmpName != "" {
			os.Remove(tmpName)
		}
	}()

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return "", fmt.Errorf("runtime: set mode: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", fmt.Errorf("runtime: write endpoint: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return "", fmt.Errorf("runtime: sync endpoint: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("runtime: close endpoint: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return "", fmt.Errorf("runtime: publish %s: %w", path, err)
	}
	tmpName = ""
	return path, nil
}

// Read loads the published endpoint. It returns an error wrapping
// os.ErrNotExist when no daemon has published one.
func Read() (Endpoint, error) {
	path, err := Path()
	if err != nil {
		return Endpoint{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Endpoint{}, err
	}
	var ep Endpoint
	if err := json.Unmarshal(b, &ep); err != nil {
		return Endpoint{}, fmt.Errorf("runtime: decode %s: %w", path, err)
	}
	if ep.Addr == "" {
		return Endpoint{}, fmt.Errorf("runtime: %s has no addr", path)
	}
	return ep, nil
}

// Remove deletes the endpoint metadata. It is safe to call when nothing was
// published, so shutdown paths can defer it unconditionally.
func Remove() error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("runtime: remove %s: %w", path, err)
	}
	return nil
}
