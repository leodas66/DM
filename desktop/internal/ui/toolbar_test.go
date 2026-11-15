package ui

import (
	"reflect"
	"testing"
	"time"

	"github.com/vishwa/dm/internal/model"
)

func TestMatchesDownloadQuery(t *testing.T) {
	d := model.Download{
		Filename: "Ubuntu-Desktop.iso",
		URL:      "https://mirror.example/releases/ubuntu.iso",
		FinalURL: "https://cdn.example/files/ubuntu.iso",
		Dir:      "/home/vishwa/Downloads",
	}
	tests := []struct {
		query string
		want  bool
	}{
		{query: "ubuntu", want: true},
		{query: "MIRROR.EXAMPLE", want: true},
		{query: "cdn.example", want: true},
		{query: "downloads", want: true},
		{query: "not-found", want: false},
		{query: "   ", want: true},
	}
	for _, tt := range tests {
		if got := matchesDownloadQuery(d, tt.query); got != tt.want {
			t.Errorf("matchesDownloadQuery(%q) = %v, want %v", tt.query, got, tt.want)
		}
	}
}

func TestSortDownloadIDs(t *testing.T) {
	now := time.Date(2026, time.November, 15, 12, 0, 0, 0, time.UTC)
	store := newViewStore()
	store.replaceAll([]model.Download{
		{ID: "a", Filename: "Alpha.iso", Size: 100, Segments: []model.Segment{{Downloaded: 80}}, CreatedAt: now},
		{ID: "b", Filename: "beta.zip", Size: 200, Segments: []model.Segment{{Downloaded: 50}}, CreatedAt: now.Add(-time.Minute)},
		{ID: "c", Filename: "gamma.tar", Size: 0, CreatedAt: now.Add(time.Minute)},
		{ID: "d", Filename: "alpha-old.iso", Size: 100, Segments: []model.Segment{{Downloaded: 20}}, CreatedAt: now.Add(-2 * time.Minute)},
	})

	tests := []struct {
		mode sortMode
		want []string
	}{
		{mode: sortNewest, want: []string{"c", "a", "b", "d"}},
		{mode: sortName, want: []string{"d", "a", "b", "c"}},
		{mode: sortSize, want: []string{"b", "a", "d", "c"}},
		{mode: sortProgress, want: []string{"a", "b", "d", "c"}},
	}
	for _, tt := range tests {
		ids := []string{"a", "b", "c", "d"}
		got := sortDownloadIDs(store, ids, tt.mode)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("sortDownloadIDs(mode %d) = %v, want %v", tt.mode, got, tt.want)
		}
	}
}
