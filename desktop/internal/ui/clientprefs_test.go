package ui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadClientPrefsMissingFileUsesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desktop.json")
	got, err := loadClientPrefs(path)
	if err != nil {
		t.Fatalf("loadClientPrefs: %v", err)
	}
	if want := defaultClientPrefs(); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestClientPrefsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "desktop.json")
	want := clientPrefs{Theme: themeLight, Density: densityCompact}
	if err := saveClientPrefs(path, want); err != nil {
		t.Fatalf("saveClientPrefs: %v", err)
	}
	got, err := loadClientPrefs(path)
	if err != nil {
		t.Fatalf("loadClientPrefs: %v", err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("permissions = %o, want 600", mode)
	}
}

func TestLoadClientPrefsInvalidJSONUsesDefaultsAndReportsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desktop.json")
	if err := os.WriteFile(path, []byte("{invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := loadClientPrefs(path)
	if err == nil {
		t.Fatal("expected invalid JSON error")
	}
	if want := defaultClientPrefs(); got != want {
		t.Fatalf("got %+v, want defaults %+v", got, want)
	}
}

func TestNormalizeClientPrefs(t *testing.T) {
	got := normalizeClientPrefs(clientPrefs{Theme: "sepia", Density: "giant"})
	if got != defaultClientPrefs() {
		t.Fatalf("got %+v, want defaults %+v", got, defaultClientPrefs())
	}
}

func TestClientPrefsPathUsesXDGConfigHome(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	got, err := clientPrefsPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(base, "dm", "desktop.json")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
