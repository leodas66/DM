package app

import (
	"context"

	"github.com/vishwa/dm/internal/model"
)

// Lifecycle abstracts the event subscription and graceful shutdown boundary.
type Lifecycle interface {
	Events() (<-chan model.Event, func())
	Shutdown(context.Context) error
}
