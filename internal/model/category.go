// Package model defines the core types shared across the daemon: Download,
// Segment, Status, Event, Credentials, and Category.
package model

import "time"

// Category is a named grouping of downloads with a default destination
// directory. Users pick a category when adding a download; the category's
// DefaultDir is prefilled but can be overridden per download.
type Category struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	DefaultDir string    `json:"defaultDir"`
	CreatedAt  time.Time `json:"createdAt"`
}
