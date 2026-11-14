# DM

A lightweight download manager: a small always-on Go daemon that does the
downloading, and a native GTK4 desktop client.

- Segmented HTTP/HTTPS downloads with pause, resume, and crash recovery.
- SQLite state, so a restart resumes partial files instead of starting over.
- Local-only REST + WebSocket API, bound to `127.0.0.1` and protected by a
  bearer token.
- GTK4 client that follows the system theme, with filters, categories,
  preferences, and live progress.
- Small daemon: no browser engine, no web UI required at runtime.

The embedded web UI from earlier phases still ships in the daemon but is legacy
and will be removed once the native client has full parity.

## Install (Arch)

```sh
git clone https://github.com/vishwa/dm.git
cd dm
makepkg -si
```

This installs:

| Path | Purpose |
|---|---|
| `/usr/bin/dm` | daemon and one-shot CLI |
| `/usr/bin/dm-desktop` | GTK4 desktop client |
| `/usr/lib/systemd/user/dm.service` | per-user daemon unit |
| `/usr/share/applications/io.github.vishwa.DM.desktop` | application entry |
| `/usr/share/icons/hicolor/scalable/apps/io.github.vishwa.DM.svg` | icon |

Then start the daemon for your user session:

```sh
systemctl --user enable --now dm.service
```

and launch **DM** from the application menu. The client finds the daemon through
its runtime endpoint file, so no address or port is configured by hand.

## First run

- New downloads go to `~/Downloads` until you change the folder in
  **Preferences** (menu → Preferences, or Ctrl+,).
- Settings live in the daemon's database and survive restarts and reboots.
- The bearer token is created on first daemon start at
  `~/.config/dm/token` (mode 0600). The database is
  `~/.config/dm/dm.db`.
- The daemon listens on an ephemeral loopback port and advertises it in
  `$XDG_RUNTIME_DIR/dm/endpoint`.

## Build from source

Requirements: Go (see `go.mod`), and for the client the GTK4 development files
(Arch: `gtk4`; Debian/Ubuntu: `libgtk-4-dev`).

```sh
make build                       # bin/dm
cd desktop && go build -o ../bin/dm-desktop .
```

Run the daemon in a terminal with `./bin/dm --serve`, then `./bin/dm-desktop`.
Add `--open-ui` to `--serve` only if you want the legacy browser UI.

## Use the client

- **Add Download** (or Ctrl+N) takes an `http://` or `https://` URL, an optional
  filename, and an optional folder.
- Pause and resume with the row button; cancel from the row menu.
- Completed rows offer **Open File**; finished rows can be removed from the list
  or deleted from disk with confirmation.
- Categories group downloads and can carry their own default folder.
- Failed rows show a short reason; the raw daemon error is in the tooltip.

## Development

```sh
make check            # daemon module: gofmt, vet, race tests, build
make check-desktop    # desktop module: same gate, needs the GTK4 headers
```

`make check` deliberately does not enter `desktop/` because that module needs
cgo and GTK. CI runs both jobs.

The browser extension in `extension/` still talks to the daemon over HTTP and
is not part of this release; the approved design routes it through a Native
Messaging host (`cmd/dm-native-host`), which is not implemented yet. Downloads
added by hand work without it.

## Layout

- `cmd/dm`: daemon and CLI.
- `internal/`: probe, segmented downloader, manager, SQLite store, API, runtime
  endpoint discovery.
- `desktop/`: separate Go module with the GTK4 client.
- `docs/`: architecture, decisions, GTK4 design contract, research notes.
- `packaging/`: desktop entry and icon used by `PKGBUILD`.

## License

MIT. See `LICENSE`.
