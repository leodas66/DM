// Package download transfers a file in segments. Workers write with WriteAt
// into a preallocated .part file and report progress over channels; they never
// share mutable state with each other or with the caller.
package download
