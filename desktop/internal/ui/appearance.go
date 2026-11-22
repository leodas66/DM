package ui

import (
	"embed"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

//go:embed style.css palette-*.css
var appearanceFiles embed.FS

type rgbColor struct {
	r float64
	g float64
	b float64
}

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
	return buildStylesheetForPrefs(defaultClientPrefs(), palette)
}

func buildStylesheetForPrefs(prefs clientPrefs, palette string) (string, error) {
	if palette != "dark" && palette != "light" && palette != "dark-hc" && palette != "light-hc" {
		return "", fmt.Errorf("unknown DM palette %q", palette)
	}
	scheme := prefs.ColorScheme
	if scheme == "" {
		scheme = colorSchemeDM
	}
	switch scheme {
	case colorSchemeDM, colorSchemeCatppuccin, colorSchemeNord, colorSchemeGruvbox, colorSchemeKanagawa:
	default:
		return "", fmt.Errorf("unknown DM color scheme %q", scheme)
	}

	paletteFile := ""
	if scheme == colorSchemeDM {
		paletteFile = "palette-" + palette + ".css"
	} else {
		mode := "light"
		if paletteIsDark(palette) {
			mode = "dark"
		}
		paletteFile = fmt.Sprintf("palette-%s-%s.css", scheme, mode)
	}
	colors, err := appearanceFiles.ReadFile(paletteFile)
	if err != nil {
		return "", fmt.Errorf("read DM palette %q: %w", paletteFile, err)
	}
	colorCSS := string(colors)

	highContrast := strings.HasSuffix(palette, "-hc")
	if highContrast && scheme != colorSchemeDM {
		colorCSS = setDefineColors(colorCSS, highContrastColors(paletteIsDark(palette)))
	}
	if prefs.AccentColor != "" {
		accent, ok := normalizeHexColor(prefs.AccentColor)
		if !ok {
			return "", fmt.Errorf("invalid accent color %q: expected #RRGGBB", prefs.AccentColor)
		}
		background := definedColorValue(colorCSS, "dm_bg")
		overrides, err := accentColorDefinitions(accent, background, highContrast)
		if err != nil {
			return "", err
		}
		colorCSS = setDefineColors(colorCSS, overrides)
	}

	rules, err := appearanceFiles.ReadFile("style.css")
	if err != nil {
		return "", fmt.Errorf("read shared DM stylesheet: %w", err)
	}
	return colorCSS + "\n" + string(rules), nil
}

func highContrastColors(dark bool) map[string]string {
	if dark {
		return map[string]string{
			"dm_text": "#FFFFFF", "dm_muted": "#E0E4E8", "dm_subtle": "#C3CBD3",
			"dm_line": "#747D88", "dm_line_soft": "#555D67", "dm_track": "#555D67",
			"dm_hover": "rgba(255,255,255,0.10)",
		}
	}
	return map[string]string{
		"dm_text": "#111111", "dm_muted": "#262626", "dm_subtle": "#444444",
		"dm_line": "#555555", "dm_line_soft": "#777777", "dm_track": "#777777",
		"dm_hover": "rgba(0,0,0,0.10)",
	}
}

func normalizeHexColor(value string) (string, bool) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "#")
	if len(value) != 6 {
		return "", false
	}
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return "", false
		}
	}
	return "#" + strings.ToUpper(value), true
}

func parseHexColor(value string) (rgbColor, bool) {
	normalized, ok := normalizeHexColor(value)
	if !ok {
		return rgbColor{}, false
	}
	raw := strings.TrimPrefix(normalized, "#")
	red, errR := strconv.ParseUint(raw[0:2], 16, 8)
	green, errG := strconv.ParseUint(raw[2:4], 16, 8)
	blue, errB := strconv.ParseUint(raw[4:6], 16, 8)
	if errR != nil || errG != nil || errB != nil {
		return rgbColor{}, false
	}
	return rgbColor{r: float64(red) / 255, g: float64(green) / 255, b: float64(blue) / 255}, true
}

func clampColor(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func colorHex(c rgbColor) string {
	return fmt.Sprintf("#%02X%02X%02X",
		uint8(math.Round(clampColor(c.r)*255)),
		uint8(math.Round(clampColor(c.g)*255)),
		uint8(math.Round(clampColor(c.b)*255)),
	)
}

func linearChannel(value float64) float64 {
	if value <= 0.04045 {
		return value / 12.92
	}
	return math.Pow((value+0.055)/1.055, 2.4)
}

func relativeLuminance(c rgbColor) float64 {
	return 0.2126*linearChannel(c.r) + 0.7152*linearChannel(c.g) + 0.0722*linearChannel(c.b)
}

func contrastRatio(foreground, background string) float64 {
	fg, okFG := parseHexColor(foreground)
	bg, okBG := parseHexColor(background)
	if !okFG || !okBG {
		return 0
	}
	l1, l2 := relativeLuminance(fg), relativeLuminance(bg)
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}

func blendColor(from, to rgbColor, amount float64) rgbColor {
	return rgbColor{
		r: from.r + (to.r-from.r)*amount,
		g: from.g + (to.g-from.g)*amount,
		b: from.b + (to.b-from.b)*amount,
	}
}

func ensureContrast(foreground, background string, minimum float64) string {
	if contrastRatio(foreground, background) >= minimum {
		return foreground
	}
	fg, okFG := parseHexColor(foreground)
	bg, okBG := parseHexColor(background)
	if !okFG || !okBG {
		return foreground
	}
	black, white := rgbColor{}, rgbColor{r: 1, g: 1, b: 1}
	for step := 1; step <= 100; step++ {
		amount := float64(step) / 100
		darker := colorHex(blendColor(fg, black, amount))
		lighter := colorHex(blendColor(fg, white, amount))
		darkRatio := contrastRatio(darker, colorHex(bg))
		lightRatio := contrastRatio(lighter, colorHex(bg))
		if darkRatio >= minimum && lightRatio >= minimum {
			if darkRatio >= lightRatio {
				return darker
			}
			return lighter
		}
		if darkRatio >= minimum {
			return darker
		}
		if lightRatio >= minimum {
			return lighter
		}
	}
	if contrastRatio("#000000", colorHex(bg)) >= contrastRatio("#FFFFFF", colorHex(bg)) {
		return "#000000"
	}
	return "#FFFFFF"
}

func accentColorDefinitions(accent, background string, highContrast bool) (map[string]string, error) {
	if _, ok := parseHexColor(background); !ok {
		return nil, fmt.Errorf("DM palette has an invalid background color %q", background)
	}
	minimum, alpha := 3.0, 0.14
	if highContrast {
		minimum, alpha = 4.5, 0.22
	}
	accent = ensureContrast(accent, background, minimum)
	accentRGB, _ := parseHexColor(accent)
	ink := "#000000"
	if contrastRatio("#FFFFFF", accent) >= contrastRatio("#000000", accent) {
		ink = "#FFFFFF"
	}
	hoverTarget := rgbColor{}
	if relativeLuminance(accentRGB) < 0.5 {
		hoverTarget = rgbColor{r: 1, g: 1, b: 1}
	}
	hover := colorHex(blendColor(accentRGB, hoverTarget, 0.10))
	soft := fmt.Sprintf("rgba(%d,%d,%d,%.2f)",
		int(math.Round(accentRGB.r*255)), int(math.Round(accentRGB.g*255)),
		int(math.Round(accentRGB.b*255)), alpha,
	)
	return map[string]string{
		"dm_accent": accent, "dm_accent_ink": ink,
		"dm_accent_hover": hover, "dm_accent_soft": soft,
	}, nil
}

func definedColorValue(css, name string) string {
	for _, line := range strings.Split(css, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 3 && fields[0] == "@define-color" && fields[1] == name {
			return strings.TrimSuffix(fields[2], ";")
		}
	}
	return ""
}

func setDefineColors(css string, overrides map[string]string) string {
	lines := strings.Split(css, "\n")
	for i, line := range lines {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 3 || fields[0] != "@define-color" {
			continue
		}
		if value, ok := overrides[fields[1]]; ok {
			lines[i] = fmt.Sprintf("@define-color %s %s;", fields[1], value)
		}
	}
	return strings.Join(lines, "\n")
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
	if w.win == nil || gdk.DisplayGetDefault() == nil {
		return
	}
	w.css = gtk.NewCSSProvider()
	gtk.StyleContextAddProviderForDisplay(
		gdk.DisplayGetDefault(), w.css, gtk.STYLE_PROVIDER_PRIORITY_APPLICATION,
	)
	w.applyAppearance()
}

func (w *window) applyAppearance() {
	if w.win == nil {
		return
	}
	css, err := buildStylesheetForPrefs(w.prefs, w.currentPalette())
	if err != nil {
		w.toast("Could not load DM appearance: " + err.Error())
		return
	}
	if w.css == nil {
		if gdk.DisplayGetDefault() == nil {
			return
		}
		w.css = gtk.NewCSSProvider()
		gtk.StyleContextAddProviderForDisplay(
			gdk.DisplayGetDefault(), w.css, gtk.STYLE_PROVIDER_PRIORITY_APPLICATION,
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
