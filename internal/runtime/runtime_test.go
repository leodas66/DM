package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// isolate points the runtime dir at a temporary XDG_RUNTIME_DIR so tests never
// touch the real user session.
func isolate(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", base)
	return base
}

func TestPublishReadRoundTrip(t *testing.T) {
	base := isolate(t)

	path, err := Publish("127.0.0.1:43210")
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	want := filepath.Join(base, "dm", "endpoint")
	if path != want {
		t.Fatalf("Publish path = %q, want %q", path, want)
	}

	ep, err := Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if ep.Addr != "127.0.0.1:43210" {
		t.Errorf("Addr = %q, want %q", ep.Addr, "127.0.0.1:43210")
	}
	if ep.PID != os.Getpid() {
		t.Errorf("PID = %d, want %d", ep.PID, os.Getpid())
	}
}

func TestPublishPermissions(t *testing.T) {
	isolate(t)

	path, err := Publish("127.0.0.1:1")
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat file: %v", err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Errorf("file mode = %04o, want 0600", got)
	}

	di, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	if got := di.Mode().Perm(); got != 0o700 {
		t.Errorf("dir mode = %04o, want 0700", got)
	}
}

func TestPublishReplacesAtomically(t *testing.T) {
	isolate(t)

	if _, err := Publish("127.0.0.1:1111"); err != nil {
		t.Fatalf("first Publish: %v", err)
	}
	if _, err := Publish("127.0.0.1:2222"); err != nil {
		t.Fatalf("second Publish: %v", err)
	}

	ep, err := Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if ep.Addr != "127.0.0.1:2222" {
		t.Errorf("Addr = %q, want the second address", ep.Addr)
	}

	entries, err := os.ReadDir(filepath.Dir(mustPath(t)))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "endpoint" {
			t.Errorf("leftover file %q after publish", e.Name())
		}
	}
}

func TestPublishRejectsEmptyAddr(t *testing.T) {
	isolate(t)
	if _, err := Publish(""); err == nil {
		t.Fatal("Publish(\"\") succeeded, want error")
	}
}

func TestReadMissing(t *testing.T) {
	isolate(t)
	if _, err := Read(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Read error = %v, want os.ErrNotExist", err)
	}
}

func TestReadRejectsMalformed(t *testing.T) {
	isolate(t)
	path, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	for name, body := range map[string]string{
		"not json": "{",
		"no addr":  `{"pid":1}`,
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("%s: WriteFile: %v", name, err)
		}
		if _, err := Read(); err == nil {
			t.Errorf("%s: Read succeeded, want error", name)
		}
	}
}

func TestRemove(t *testing.T) {
	isolate(t)

	if _, err := Publish("127.0.0.1:1"); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if err := Remove(); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := Read(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Read after Remove = %v, want os.ErrNotExist", err)
	}
	// Removing twice is not an error: shutdown paths call it unconditionally.
	if err := Remove(); err != nil {
		t.Fatalf("second Remove: %v", err)
	}
}

func TestDirFallbackWithoutXdgRuntimeDir(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")

	dir, err := Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	want := filepath.Join(os.TempDir(), "dm-"+strconv.Itoa(os.Getuid()))
	if dir != want {
		t.Errorf("Dir = %q, want %q", dir, want)
	}
}

func mustPath(t *testing.T) string {
	t.Helper()
	p, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	return p
}
