package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/vishwa/dm/internal/model"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "dm.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func sample(id string) model.Download {
	return model.Download{
		ID:            id,
		URL:           "https://example.com/f.bin",
		FinalURL:      "https://cdn.example.com/f.bin",
		Filename:      "f.bin",
		Dir:           "/tmp",
		Size:          1000,
		AcceptsRanges: true,
		ETag:          `"v1"`,
		LastModified:  "Wed, 21 Oct 2015 07:28:00 GMT",
		Status:        model.StatusPaused,
		Segments: []model.Segment{
			{Index: 0, Start: 0, End: 499, Downloaded: 123, Status: model.SegmentRunning},
			{Index: 1, Start: 500, End: 999, Status: model.SegmentPending},
		},
		CreatedAt: time.Date(2026, 11, 1, 12, 0, 0, 123456789, time.UTC),
		Error:     "boom",
	}
}

func TestSaveAndGetRoundTrip(t *testing.T) {
	s := openTemp(t)
	want := sample("d1")
	if err := s.Save(context.Background(), want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.Get(context.Background(), "d1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != want.ID || got.URL != want.URL || got.FinalURL != want.FinalURL ||
		got.Filename != want.Filename || got.Dir != want.Dir || got.Size != want.Size ||
		got.AcceptsRanges != want.AcceptsRanges || got.ETag != want.ETag ||
		got.LastModified != want.LastModified || got.Status != want.Status || got.Error != want.Error {
		t.Fatalf("mismatch:\n got %+v\nwant %+v", got, want)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, want.CreatedAt)
	}
	if len(got.Segments) != 2 || got.Segments[0].Downloaded != 123 || got.Segments[1].End != 999 {
		t.Errorf("segments = %+v", got.Segments)
	}
}

func TestSaveNeverPersistsCredentials(t *testing.T) {
	s := openTemp(t)
	d := sample("d1")
	d.Headers = map[string]string{"Authorization": "Bearer s3cret"}
	d.Referrer = "https://example.com/private"

	if err := s.Save(context.Background(), d); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.Get(context.Background(), "d1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Headers != nil || got.Referrer != "" {
		t.Fatalf("credentials came back from the store: %+v", got)
	}
}

func TestSaveReplacesSegmentsInsteadOfAppending(t *testing.T) {
	s := openTemp(t)
	d := sample("d1")
	if err := s.Save(context.Background(), d); err != nil {
		t.Fatal(err)
	}

	d.Segments = []model.Segment{{Index: 0, Start: 0, End: 999, Downloaded: 500, Status: model.SegmentRunning}}
	if err := s.Save(context.Background(), d); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(context.Background(), "d1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Segments) != 1 || got.Segments[0].Downloaded != 500 {
		t.Fatalf("segments = %+v, want the single updated row", got.Segments)
	}
}

func TestListIsOrderedByCreationTime(t *testing.T) {
	s := openTemp(t)
	later := sample("late")
	later.CreatedAt = later.CreatedAt.Add(time.Hour)
	earlier := sample("early")

	for _, d := range []model.Download{later, earlier} {
		if err := s.Save(context.Background(), d); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "early" || got[1].ID != "late" {
		t.Fatalf("order = %v", []string{got[0].ID, got[1].ID})
	}
}

func TestUnfinishedExcludesTerminalStates(t *testing.T) {
	s := openTemp(t)
	all := map[string]model.Status{
		"q": model.StatusQueued,
		"r": model.StatusRunning,
		"p": model.StatusPaused,
		"c": model.StatusCompleted,
		"f": model.StatusFailed,
		"x": model.StatusCanceled,
	}
	for id, st := range all {
		d := sample(id)
		d.Status = st
		if err := s.Save(context.Background(), d); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.Unfinished(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, d := range got {
		seen[d.ID] = true
	}
	for _, id := range []string{"q", "r", "p"} {
		if !seen[id] {
			t.Errorf("%s missing from unfinished", id)
		}
	}
	for _, id := range []string{"c", "f", "x"} {
		if seen[id] {
			t.Errorf("%s should be excluded (terminal)", id)
		}
	}
}

func TestDeleteRemovesRows(t *testing.T) {
	s := openTemp(t)
	if err := s.Save(context.Background(), sample("d1")); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(context.Background(), "d1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(context.Background(), "d1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after delete = %v, want ErrNotFound", err)
	}
	if _, err := s.Get(context.Background(), "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get missing = %v, want ErrNotFound", err)
	}
}

func TestReopenKeepsDataAndSkipsAppliedMigrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dm.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(context.Background(), sample("d1")); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	got, err := s2.Get(context.Background(), "d1")
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if got.Status != model.StatusPaused {
		t.Errorf("status = %s", got.Status)
	}
}

func sampleCategory(id string) model.Category {
	return model.Category{
		ID:         id,
		Name:       "Videos",
		DefaultDir: "/home/user/downloads/videos",
		CreatedAt:  time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC),
	}
}

func TestCategorySaveAndGetRoundTrip(t *testing.T) {
	s := openTemp(t)
	want := sampleCategory("c1")
	if err := s.SaveCategory(context.Background(), want); err != nil {
		t.Fatalf("SaveCategory: %v", err)
	}

	got, err := s.GetCategory(context.Background(), "c1")
	if err != nil {
		t.Fatalf("GetCategory: %v", err)
	}
	if got.ID != want.ID || got.Name != want.Name || got.DefaultDir != want.DefaultDir {
		t.Fatalf("mismatch:\n got %+v\nwant %+v", got, want)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, want.CreatedAt)
	}
}

func TestCategoryGetMissing(t *testing.T) {
	s := openTemp(t)
	if _, err := s.GetCategory(context.Background(), "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetCategory missing = %v, want ErrNotFound", err)
	}
}

func TestCategoryList(t *testing.T) {
	s := openTemp(t)
	a := sampleCategory("c-a")
	a.CreatedAt = a.CreatedAt.Add(time.Hour)
	b := sampleCategory("c-b")
	for _, c := range []model.Category{a, b} {
		if err := s.SaveCategory(context.Background(), c); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.ListCategories(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "c-b" || got[1].ID != "c-a" {
		t.Fatalf("order = %v", []string{got[0].ID, got[1].ID})
	}
}

func TestCategoryDelete(t *testing.T) {
	s := openTemp(t)
	if err := s.SaveCategory(context.Background(), sampleCategory("c1")); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteCategory(context.Background(), "c1"); err != nil {
		t.Fatalf("DeleteCategory: %v", err)
	}
	if _, err := s.GetCategory(context.Background(), "c1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetCategory after delete = %v, want ErrNotFound", err)
	}
}

func TestDownloadCategoryIDRoundTrip(t *testing.T) {
	s := openTemp(t)
	d := sample("d1")
	d.CategoryID = "c1"
	if err := s.Save(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(context.Background(), "d1")
	if err != nil {
		t.Fatal(err)
	}
	if got.CategoryID != "c1" {
		t.Fatalf("CategoryID = %q, want %q", got.CategoryID, "c1")
	}
}

func TestDownloadStartAtRoundTrip(t *testing.T) {
	s := openTemp(t)
	d := sample("d1")
	d.StartAt = time.Date(2026, 11, 1, 15, 0, 0, 0, time.UTC)
	if err := s.Save(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(context.Background(), "d1")
	if err != nil {
		t.Fatal(err)
	}
	if !got.StartAt.Equal(d.StartAt) {
		t.Fatalf("StartAt = %v, want %v", got.StartAt, d.StartAt)
	}
	if got.Status != model.StatusPaused {
		t.Fatalf("Status = %s, want %s", got.Status, model.StatusPaused)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)

	if _, err := s.GetSetting(ctx, "settings"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetSetting before write = %v, want ErrNotFound", err)
	}
	if err := s.PutSetting(ctx, "settings", `{"dir":"/a"}`); err != nil {
		t.Fatalf("PutSetting: %v", err)
	}
	got, err := s.GetSetting(ctx, "settings")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if got != `{"dir":"/a"}` {
		t.Fatalf("GetSetting = %q", got)
	}
	if err := s.PutSetting(ctx, "settings", `{"dir":"/b"}`); err != nil {
		t.Fatalf("PutSetting overwrite: %v", err)
	}
	if got, _ := s.GetSetting(ctx, "settings"); got != `{"dir":"/b"}` {
		t.Fatalf("GetSetting after overwrite = %q", got)
	}
}
