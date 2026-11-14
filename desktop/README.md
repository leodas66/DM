# DM desktop client (GTK4)

The native client for the `dm` daemon. It is a client only: the daemon owns
downloads, scheduling, and persistence. The client finds the daemon through its
runtime endpoint file, authenticates with the existing bearer token, and uses
REST plus WebSocket events. Closing the window does not stop the daemon.

## Requirements

- Go 1.27.3 with cgo enabled (default).
- GTK4 development files (Arch: `gtk4`; Debian/Ubuntu: `libgtk-4-dev`).
- A running daemon: `make build && ./bin/dm --serve`.

## Build and run

```sh
# from the repository root
make build            # daemon
(cd desktop && go build -o ../bin/dm-desktop .)
./bin/dm-desktop
```

The client reads the daemon address from `$XDG_RUNTIME_DIR/dm/endpoint`
(fallback `$TMPDIR/dm-<uid>/endpoint`) and the token from `$DM_CONFIG_DIR/token`
or `$XDG_CONFIG_HOME/dm/token` (default `~/.config/dm/token`). Set the same
`XDG_RUNTIME_DIR` and `XDG_CONFIG_HOME` for both processes when testing.

## Check

```sh
make check-desktop
```

This runs gofmt, `go vet`, `go test -race`, and `go build` in this module. The
root `make check` deliberately does not enter `desktop/` because it needs cgo
and the GTK4 headers.

## Layout

- `internal/dmapi`: endpoint discovery, token loading, REST calls, and a
  minimal WebSocket client for the event stream.
- `internal/format`: byte, rate, percentage, and ETA formatting.
- `internal/ui`: GTK widgets and the pure view logic (`store.go`,
  `rowtext.go`) that the widget code renders.

## Design contract

`docs/GTK4-DESIGN-SPEC.md` is authoritative. In particular: standard GTK
widgets only, no app-owned palette or CSS skin, the user's GTK theme and system
appearance decide colors, icon-only controls carry accessible names and
tooltips, and unknown sizes never produce invented progress.
