// Package app contains application services that coordinate domain operations
// for delivery layers such as the HTTP API and desktop client.
package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/vishwa/dm/internal/manager"
	"github.com/vishwa/dm/internal/model"
	"github.com/vishwa/dm/internal/store"
)

// ErrNotFound is returned when an entity does not exist.
var ErrNotFound = store.ErrNotFound

// ErrInvalid marks invalid application input.
var ErrInvalid = errors.New("app: invalid input")

// DownloadBackend is the manager capability used by DownloadService.
type DownloadBackend interface {
	Add(context.Context, manager.AddRequest) (model.Download, error)
	List(context.Context) ([]model.Download, error)
	Get(context.Context, string) (model.Download, error)
	Pause(context.Context, string) (model.Download, error)
	Resume(context.Context, string) (model.Download, error)
	Cancel(context.Context, string) (model.Download, error)
	Remove(context.Context, string, bool) error
}

// DownloadService validates and dispatches download use cases.
type DownloadService struct {
	backend DownloadBackend
}

// NewDownloadService creates a DownloadService over backend.
func NewDownloadService(backend DownloadBackend) *DownloadService {
	return &DownloadService{backend: backend}
}

// Add validates and accepts a download. Credentials are passed through to the
// Manager and remain transient; they are never copied into the public model.
func (s *DownloadService) Add(ctx context.Context, req manager.AddRequest) (model.Download, error) {
	rawURL := strings.TrimSpace(req.URL)
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return model.Download{}, fmt.Errorf("%w: URL must be an absolute HTTP or HTTPS URL", ErrInvalid)
	}
	req.URL = rawURL
	return s.backend.Add(ctx, req)
}

// List returns downloads in Manager order.
func (s *DownloadService) List(ctx context.Context) ([]model.Download, error) {
	return s.backend.List(ctx)
}

// Get returns one download by id.
func (s *DownloadService) Get(ctx context.Context, id string) (model.Download, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return model.Download{}, fmt.Errorf("%w: download id is required", ErrInvalid)
	}
	return s.backend.Get(ctx, id)
}

// Pause pauses a download without deleting its partial data.
func (s *DownloadService) Pause(ctx context.Context, id string) (model.Download, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return model.Download{}, fmt.Errorf("%w: download id is required", ErrInvalid)
	}
	return s.backend.Pause(ctx, id)
}

// Resume re-queues a paused, failed or canceled download.
func (s *DownloadService) Resume(ctx context.Context, id string) (model.Download, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return model.Download{}, fmt.Errorf("%w: download id is required", ErrInvalid)
	}
	return s.backend.Resume(ctx, id)
}

// Cancel cancels a download while retaining partial data.
func (s *DownloadService) Cancel(ctx context.Context, id string) (model.Download, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return model.Download{}, fmt.Errorf("%w: download id is required", ErrInvalid)
	}
	return s.backend.Cancel(ctx, id)
}

// Remove deletes a download record and optionally its files.
func (s *DownloadService) Remove(ctx context.Context, id string, deleteFile bool) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("%w: download id is required", ErrInvalid)
	}
	return s.backend.Remove(ctx, id, deleteFile)
}
