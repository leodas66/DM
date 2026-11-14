#!/bin/sh
# Verification gate for the GTK4 desktop client, which is a separate Go module.
# The root gate (scripts/check.sh) deliberately does not enter desktop/ because
# it needs cgo and the GTK4 development headers.
set -eu

cd "$(dirname "$0")/../desktop"

if ! pkg-config --exists gtk4 2>/dev/null; then
	echo "check-desktop: GTK4 development files not found (pkg-config gtk4)" >&2
	echo "on Arch: pacman -S gtk4; on Debian/Ubuntu: apt install libgtk-4-dev" >&2
	exit 1
fi

unformatted=$(gofmt -l .)
if [ -n "$unformatted" ]; then
	echo "gofmt: these files need formatting:" >&2
	echo "$unformatted" >&2
	echo "fix with: gofmt -w ." >&2
	exit 1
fi

echo "==> go vet (desktop)"
go vet ./...

echo "==> go test -race (desktop)"
go test -race ./...

echo "==> go build (desktop)"
go build ./...

echo "check-desktop: green"
