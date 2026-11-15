package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vishwa/dm/internal/manager"
	"github.com/vishwa/dm/internal/model"
)

type downloadBackendStub struct {
	added manager.AddRequest
}

func (b *downloadBackendStub) Add(_ context.Context, req manager.AddRequest) (model.Download, error) {
	b.added = req
	return model.Download{URL: req.URL}, nil
}
func (*downloadBackendStub) List(context.Context) ([]model.Download, error) { return nil, nil }
func (*downloadBackendStub) Get(context.Context, string) (model.Download, error) {
	return model.Download{}, nil
}
func (*downloadBackendStub) Pause(context.Context, string) (model.Download, error) {
	return model.Download{}, nil
}
func (*downloadBackendStub) Resume(context.Context, string) (model.Download, error) {
	return model.Download{}, nil
}
func (*downloadBackendStub) Cancel(context.Context, string) (model.Download, error) {
	return model.Download{}, nil
}
func (*downloadBackendStub) Remove(context.Context, string, bool) error { return nil }

func TestDownloadServiceValidatesAndPreservesTransientCredentials(t *testing.T) {
	backend := &downloadBackendStub{}
	service := NewDownloadService(backend)
	headers := map[string]string{"Authorization": "Bearer secret"}
	referrer := "https://example.test/page"

	got, err := service.Add(context.Background(), manager.AddRequest{
		URL:      "  https://example.test/file  ",
		Headers: headers,
		Referrer: referrer,
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if got.URL != "https://example.test/file" || backend.added.URL != got.URL {
		t.Fatalf("URL was not normalized: got %q, request %q", got.URL, backend.added.URL)
	}
	if backend.added.Headers["Authorization"] != "Bearer secret" || backend.added.Referrer != referrer {
		t.Fatal("transient credentials were not forwarded to the backend")
	}
}

func TestDownloadServiceRejectsNonHTTPURL(t *testing.T) {
	service := NewDownloadService(&downloadBackendStub{})
	_, err := service.Add(context.Background(), manager.AddRequest{URL: "ftp://example.test/file"})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("Add() error = %v, want ErrInvalid", err)
	}
}

type categoryBackendStub struct {
	category model.Category
}

func (b *categoryBackendStub) AddCategory(_ context.Context, c model.Category) (model.Category, error) {
	b.category = c
	return c, nil
}
func (*categoryBackendStub) ListCategories(context.Context) ([]model.Category, error) {
	return nil, nil
}
func (*categoryBackendStub) GetCategory(context.Context, string) (model.Category, error) {
	return model.Category{}, nil
}
func (*categoryBackendStub) DeleteCategory(context.Context, string) error { return nil }

func TestQueueServiceNormalizesCategory(t *testing.T) {
	backend := &categoryBackendStub{}
	service := NewQueueService(backend)
	got, err := service.Add(context.Background(), "  Movies  ", "  /downloads/movies  ")
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if got.Name != "Movies" || got.DefaultDir != "/downloads/movies" {
		t.Fatalf("category fields not normalized: %+v", got)
	}
	if got.ID == "" || got.CreatedAt.IsZero() || got.CreatedAt.Location() != time.UTC {
		t.Fatalf("category metadata not initialized: %+v", got)
	}
	if backend.category.ID != got.ID {
		t.Fatal("category was not passed to backend")
	}
}

func TestQueueServiceRejectsEmptyCategoryName(t *testing.T) {
	service := NewQueueService(&categoryBackendStub{})
	_, err := service.Add(context.Background(), "  ", "")
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("Add() error = %v, want ErrInvalid", err)
	}
}
