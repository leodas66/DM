package ui

import (
	"regexp"
	"testing"
)

func TestResolvePalette(t *testing.T) {
	cases := []struct {
		name         string
		prefs        clientPrefs
		systemDark   bool
		highContrast bool
		want         string
	}{
		{"system light", clientPrefs{Theme: themeSystem}, false, false, "light"},
		{"system dark", clientPrefs{Theme: themeSystem}, true, false, "dark"},
		{"explicit light", clientPrefs{Theme: themeLight}, true, false, "light"},
		{"explicit dark", clientPrefs{Theme: themeDark}, false, false, "dark"},
		{"dark high contrast", clientPrefs{Theme: themeSystem}, true, true, "dark-hc"},
		{"light high contrast", clientPrefs{Theme: themeSystem}, false, true, "light-hc"},
		{"explicit light high contrast", clientPrefs{Theme: themeLight}, true, true, "light-hc"},
		{"explicit dark high contrast", clientPrefs{Theme: themeDark}, false, true, "dark-hc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolvePalette(tc.prefs, tc.systemDark, tc.highContrast)
			if got != tc.want {
				t.Fatalf("resolvePalette = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildStylesheetDefinesEveryUsedColor(t *testing.T) {
	definedRE := regexp.MustCompile(`@define-color\s+(dm_[a-z0-9_]+)`)
	usedRE := regexp.MustCompile(`@(dm_[a-z0-9_]+)`)
	schemes := []colorScheme{
		colorSchemeDM,
		colorSchemeCatppuccin,
		colorSchemeNord,
		colorSchemeGruvbox,
		colorSchemeKanagawa,
	}
	palettes := []string{"dark", "light", "dark-hc", "light-hc"}
	for _, scheme := range schemes {
		for _, palette := range palettes {
			t.Run(string(scheme)+"/"+palette, func(t *testing.T) {
				css, err := buildStylesheetForPrefs(clientPrefs{ColorScheme: scheme}, palette)
				if err != nil {
					t.Fatal(err)
				}
				defined := map[string]bool{}
				for _, match := range definedRE.FindAllStringSubmatch(css, -1) {
					defined[match[1]] = true
				}
				for _, match := range usedRE.FindAllStringSubmatch(css, -1) {
					if !defined[match[1]] {
						t.Errorf("undefined color @%s", match[1])
					}
				}
			})
		}
	}
}

func TestBuildStylesheetRejectsUnknownPalette(t *testing.T) {
	if _, err := buildStylesheet("sepia"); err == nil {
		t.Fatal("expected an error for unknown palette")
	}
}

func TestBuildStylesheetRejectsUnknownColorScheme(t *testing.T) {
	if _, err := buildStylesheetForPrefs(clientPrefs{ColorScheme: "unknown"}, "dark"); err == nil {
		t.Fatal("expected an error for unknown color scheme")
	}
}

func TestBuildStylesheetAppliesCustomAccent(t *testing.T) {
	css, err := buildStylesheetForPrefs(clientPrefs{
		ColorScheme: colorSchemeNord,
		AccentColor: "#777777",
	}, "light")
	if err != nil {
		t.Fatal(err)
	}
	accent := definedColor(t, css, "dm_accent")
	ink := definedColor(t, css, "dm_accent_ink")
	background := definedColor(t, css, "dm_bg")
	if got := contrastRatio(accent, background); got < 3 {
		t.Errorf("accent/background contrast = %.2f, want at least 3:1", got)
	}
	if got := contrastRatio(ink, accent); got < 4.5 {
		t.Errorf("accent ink contrast = %.2f, want at least 4.5:1", got)
	}
	for _, name := range []string{"dm_accent_soft", "dm_accent_hover"} {
		if got := definedColor(t, css, name); got == "" {
			t.Errorf("%s was not defined", name)
		}
	}
}

func TestBuildStylesheetHighContrastKeepsSelectedColorScheme(t *testing.T) {
	css, err := buildStylesheetForPrefs(clientPrefs{
		ColorScheme: colorSchemeCatppuccin,
		AccentColor: "#89B4FA",
	}, "dark-hc")
	if err != nil {
		t.Fatal(err)
	}
	if got := definedColor(t, css, "dm_bg"); got != "#1e1e2e" {
		t.Errorf("high-contrast Catppuccin background = %q, want selected theme background", got)
	}
	if got := definedColor(t, css, "dm_text"); got != "#ffffff" {
		t.Errorf("high-contrast text = %q, want #ffffff", got)
	}
}

func definedColor(t *testing.T, css, name string) string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^@define-color\s+` + regexp.QuoteMeta(name) + `\s+(.+);$`)
	match := re.FindStringSubmatch(css)
	if len(match) != 2 {
		t.Fatalf("could not find @define-color %s", name)
	}
	return match[1]
}
