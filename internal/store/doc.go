// Package store persists downloads and segment progress in SQLite
// (modernc.org/sqlite, pure Go) and runs schema migrations on startup. Only
// the Manager writes through it. Implemented in Phase 2.
package store
