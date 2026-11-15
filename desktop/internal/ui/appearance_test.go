package ui

import (
	"regexp"
	"testing"
)

func TestResolvePalette(t *testing.T) {
	cases := []struct {
		name string
		prefs clientPrefs
		systemDark bool
		highContrast bool
		want string
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
	for _, palette := range []string{"dark", "light", "dark-hc", "light-hc"} {
		t.Run(palette, func(t *testing.T) {
			css, err := buildStylesheet(palette)
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

func TestBuildStylesheetRejectsUnknownPalette(t *testing.T) {
	if _, err := buildStylesheet("sepia"); err == nil {
		t.Fatal("expected an error for unknown palette")
	}
}
