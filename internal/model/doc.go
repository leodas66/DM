// Package model defines the core data types shared across the daemon.
//
// This package holds types only: no behaviour, no I/O, no imports beyond the
// standard library. Every other package depends on it, so a change here is a
// change to the whole system's vocabulary.
package model
