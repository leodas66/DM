package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type themeChoice string

const (
	themeSystem themeChoice = "system"
	themeDark   themeChoice = "dark"
	themeLight  themeChoice = "light"
)

type densityChoice string

const (
	densityComfortable densityChoice = "comfortable"
	densityCompact     densityChoice = "compact"
)

type colorScheme string

const (
	colorSchemeDM         colorScheme = "dm"
	colorSchemeCatppuccin colorScheme = "catppuccin"
	colorSchemeNord       colorScheme = "nord"
	colorSchemeGruvbox    colorScheme = "gruvbox"
	colorSchemeKanagawa   colorScheme = "kanagawa"
)

var colorSchemeOptions = []string{
	"DM · Amber", "Catppuccin", "Nord", "Gruvbox", "Kanagawa",
}

type clientPrefs struct {
	Theme       themeChoice   `json:"theme"`
	Density     densityChoice `json:"density"`
	ColorScheme colorScheme   `json:"color_scheme"`
	AccentColor string        `json:"accent_color,omitempty"`
}

func defaultClientPrefs() clientPrefs {
	return clientPrefs{Theme: themeSystem, Density: densityComfortable, ColorScheme: colorSchemeDM}
}

func normalizeClientPrefs(p clientPrefs) clientPrefs {
	switch p.Theme {
	case themeSystem, themeDark, themeLight:
	default:
		p.Theme = themeSystem
	}
	switch p.Density {
	case densityComfortable, densityCompact:
	default:
		p.Density = densityComfortable
	}
	switch p.ColorScheme {
	case colorSchemeDM, colorSchemeCatppuccin, colorSchemeNord, colorSchemeGruvbox, colorSchemeKanagawa:
	default:
		p.ColorScheme = colorSchemeDM
	}
	if p.AccentColor != "" {
		if normalized, ok := normalizeHexColor(p.AccentColor); ok {
			p.AccentColor = normalized
		} else {
			p.AccentColor = ""
		}
	}
	return p
}

func colorSchemeChoiceIndex(choice colorScheme) uint {
	switch choice {
	case colorSchemeCatppuccin:
		return 1
	case colorSchemeNord:
		return 2
	case colorSchemeGruvbox:
		return 3
	case colorSchemeKanagawa:
		return 4
	default:
		return 0
	}
}

func colorSchemeFromIndex(index uint) colorScheme {
	switch index {
	case 1:
		return colorSchemeCatppuccin
	case 2:
		return colorSchemeNord
	case 3:
		return colorSchemeGruvbox
	case 4:
		return colorSchemeKanagawa
	default:
		return colorSchemeDM
	}
}

// clientPrefsPath keeps desktop-only preferences beside the user's DM config,
// without touching daemon settings or authentication material.
func clientPrefsPath() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		var err error
		base, err = os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("locate user config directory: %w", err)
		}
	}
	if base == "" {
		return "", fmt.Errorf("user config directory is empty")
	}
	return filepath.Join(base, "dm", "desktop.json"), nil
}

func loadClientPrefs(path string) (clientPrefs, error) {
	prefs := defaultClientPrefs()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return prefs, nil
	}
	if err != nil {
		return prefs, fmt.Errorf("read desktop preferences: %w", err)
	}
	if err := json.Unmarshal(data, &prefs); err != nil {
		return defaultClientPrefs(), fmt.Errorf("parse desktop preferences: %w", err)
	}
	return normalizeClientPrefs(prefs), nil
}

// saveClientPrefs writes atomically with owner-only permissions. The file
// contains appearance settings only, but stays private and safe by default.
func saveClientPrefs(path string, prefs clientPrefs) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create desktop config directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".desktop-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary preferences file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("secure temporary preferences file: %w", err)
	}
	if err := json.NewEncoder(tmp).Encode(normalizeClientPrefs(prefs)); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("encode desktop preferences: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync desktop preferences: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close desktop preferences: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace desktop preferences: %w", err)
	}
	return nil
}
