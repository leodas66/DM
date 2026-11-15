package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/vishwa/dm/internal/manager"
	"github.com/vishwa/dm/internal/model"
)

// CategoryBackend is the Manager capability used by QueueService.
type CategoryBackend interface {
	AddCategory(context.Context, model.Category) (model.Category, error)
	ListCategories(context.Context) ([]model.Category, error)
	GetCategory(context.Context, string) (model.Category, error)
	DeleteCategory(context.Context, string) error
}

// QueueService contains category and queue-organization use cases.
type QueueService struct {
	backend CategoryBackend
}

// NewQueueService creates a QueueService over backend.
func NewQueueService(backend CategoryBackend) *QueueService {
	return &QueueService{backend: backend}
}

// Add creates a category after normalizing user-supplied fields.
func (s *QueueService) Add(ctx context.Context, name, defaultDir string) (model.Category, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return model.Category{}, fmt.Errorf("%w: category name is required", ErrInvalid)
	}
	category := model.Category{
		ID:         manager.NewID(),
		Name:       name,
		DefaultDir: strings.TrimSpace(defaultDir),
		CreatedAt:  time.Now().UTC(),
	}
	return s.backend.AddCategory(ctx, category)
}

// List returns all categories.
func (s *QueueService) List(ctx context.Context) ([]model.Category, error) {
	return s.backend.ListCategories(ctx)
}

// Get returns a category by id.
func (s *QueueService) Get(ctx context.Context, id string) (model.Category, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return model.Category{}, fmt.Errorf("%w: category id is required", ErrInvalid)
	}
	return s.backend.GetCategory(ctx, id)
}

// Delete removes a category by id.
func (s *QueueService) Delete(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("%w: category id is required", ErrInvalid)
	}
	return s.backend.DeleteCategory(ctx, id)
}
