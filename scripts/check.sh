#!/bin/sh
# Verification gate: the only accepted evidence that a phase is green.
# Runs the same commands locally, in the pre-commit hook, and in CI.
set -eu

cd "$(dirname "$0")/.."

unformatted=$(gofmt -l .)
if [ -n "$unformatted" ]; then
	echo "gofmt: these files need formatting:" >&2
	echo "$unformatted" >&2
	echo "fix with: gofmt -w ." >&2
	exit 1
fi

echo "==> go vet"
go vet ./...

echo "==> go test -race"
go test -race ./...

echo "==> go build"
go build ./...

echo "check: green"
