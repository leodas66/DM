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
	want := clientPrefs{
		Theme:       themeLight,
		Density:     densityCompact,
		ColorScheme: colorSchemeKanagawa,
		AccentColor: "#D27E99",
	}
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
	cases := []struct {
		name string
		in   clientPrefs
		want clientPrefs
	}{
		{
			name: "invalid legacy values",
			in:   clientPrefs{Theme: "sepia", Density: "giant", ColorScheme: "unknown", AccentColor: "not-a-color"},
			want: defaultClientPrefs(),
		},
		{
			name: "legacy preferences receive new defaults",
			in:   clientPrefs{Theme: themeLight, Density: densityCompact},
			want: clientPrefs{Theme: themeLight, Density: densityCompact, ColorScheme: colorSchemeDM},
		},
		{
			name: "accent normalizes",
			in:   clientPrefs{ColorScheme: colorSchemeNord, AccentColor: " #aBc123 "},
			want: clientPrefs{Theme: themeSystem, Density: densityComfortable, ColorScheme: colorSchemeNord, AccentColor: "#ABC123"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeClientPrefs(tc.in); got != tc.want {
				t.Fatalf("normalizeClientPrefs = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestNormalizeHexColor(t *testing.T) {
	cases := []struct {
		input string
		want  string
		ok    bool
	}{
		{"#D97757", "#D97757", true},
		{"d97757", "#D97757", true},
		{"  #aBc123  ", "#ABC123", true},
		{"#123", "", false},
		{"#GG0000", "", false},
		{"red", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		got, ok := normalizeHexColor(tc.input)
		if got != tc.want || ok != tc.ok {
			t.Errorf("normalizeHexColor(%q) = (%q, %v), want (%q, %v)", tc.input, got, ok, tc.want, tc.ok)
		}
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
