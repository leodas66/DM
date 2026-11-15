package ui

import (
	"embed"
	"fmt"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

//go:embed style.css palette-dark.css palette-light.css palette-dark-hc.css palette-light-hc.css
var appearanceFiles embed.FS

func resolvePalette(prefs clientPrefs, systemDark, highContrast bool) string {
	dark := systemDark
	switch normalizeClientPrefs(prefs).Theme {
	case themeDark:
		dark = true
	case themeLight:
		dark = false
	}
	if highContrast {
		if dark {
			return "dark-hc"
		}
		return "light-hc"
	}
	if dark {
		return "dark"
	}
	return "light"
}

func paletteIsDark(name string) bool {
	return name == "dark" || name == "dark-hc"
}

func buildStylesheet(palette string) (string, error) {
	var paletteFile string
	switch palette {
	case "dark":
		paletteFile = "palette-dark.css"
	case "light":
		paletteFile = "palette-light.css"
	case "dark-hc":
		paletteFile = "palette-dark-hc.css"
	case "light-hc":
		paletteFile = "palette-light-hc.css"
	default:
		return "", fmt.Errorf("unknown DM palette %q", palette)
	}

	colors, err := appearanceFiles.ReadFile(paletteFile)
	if err != nil {
		return "", fmt.Errorf("read DM palette %q: %w", palette, err)
	}
	rules, err := appearanceFiles.ReadFile("style.css")
	if err != nil {
		return "", fmt.Errorf("read shared DM stylesheet: %w", err)
	}
	return string(colors) + "\n" + string(rules), nil
}

func (w *window) loadClientPrefs() {
	w.prefs = defaultClientPrefs()
	path, err := clientPrefsPath()
	if err != nil {
		return
	}
	w.prefsPath = path
	prefs, err := loadClientPrefs(path)
	if err != nil {
		w.prefsWarning = err.Error()
		return
	}
	w.prefs = prefs
}

func (w *window) systemAppearance() (dark, highContrast bool) {
	settings := gtk.SettingsGetDefault()
	if settings == nil {
		return false, false
	}
	theme, _ := settings.ObjectProperty("gtk-theme-name").(string)
	preferDark, _ := settings.ObjectProperty("gtk-application-prefer-dark-theme").(bool)
	name := strings.ToLower(theme)
	dark = preferDark || strings.Contains(name, "dark")
	highContrast = strings.Contains(name, "highcontrast") || strings.Contains(name, "high-contrast")
	return dark, highContrast
}

func (w *window) currentPalette() string {
	dark, highContrast := w.systemAppearance()
	return resolvePalette(w.prefs, dark, highContrast)
}

func (w *window) installAppearance() {
	if w.win == nil || w.win.Window.Display() == nil {
		return
	}
	w.css = gtk.NewCSSProvider()
	gtk.StyleContextAddProviderForDisplay(
		w.win.Window.Display(),
		w.css,
		gtk.STYLE_PROVIDER_PRIORITY_APPLICATION,
	)
	w.applyAppearance()
}

func (w *window) applyAppearance() {
	if w.win == nil {
		return
	}
	css, err := buildStylesheet(w.currentPalette())
	if err != nil {
		w.toast("Could not load DM appearance: " + err.Error())
		return
	}
	if w.css == nil {
		if w.win.Window.Display() == nil {
			return
		}
		w.css = gtk.NewCSSProvider()
		gtk.StyleContextAddProviderForDisplay(
			w.win.Window.Display(),
			w.css,
			gtk.STYLE_PROVIDER_PRIORITY_APPLICATION,
		)
	}
	w.css.LoadFromString(css)

	if w.prefs.Density == densityCompact {
		w.win.AddCSSClass("dm-compact")
	} else {
		w.win.RemoveCSSClass("dm-compact")
	}

	if w.themeButton != nil {
		iconName, label := "weather-clear-night-symbolic", "Switch to dark appearance"
		if paletteIsDark(w.currentPalette()) {
			iconName, label = "weather-clear-symbolic", "Switch to light appearance"
		}
		icon := gtk.NewImageFromIconName(iconName)
		icon.SetPixelSize(16)
		w.themeButton.SetChild(icon)
		w.themeButton.SetTooltipText(label)
		setAccessibleLabel(w.themeButton.Accessible, label)
	}
}

func (w *window) setClientPrefs(prefs clientPrefs) error {
	w.prefs = normalizeClientPrefs(prefs)
	w.applyAppearance()
	if w.prefsPath == "" {
		path, err := clientPrefsPath()
		if err != nil {
			return err
		}
		w.prefsPath = path
	}
	return saveClientPrefs(w.prefsPath, w.prefs)
}

func (w *window) toggleAppearance() {
	next := themeDark
	if paletteIsDark(w.currentPalette()) {
		next = themeLight
	}
	prefs := w.prefs
	prefs.Theme = next
	if err := w.setClientPrefs(prefs); err != nil {
		w.toast("Appearance changed but could not be saved: " + err.Error())
	}
}
