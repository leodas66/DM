// Package manager owns all download state. It is the only writer to the
// store, enforces the queue and concurrency limits, applies speed limits, and
// publishes events. Implemented in Phase 2.
package manager
