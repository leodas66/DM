# DM

A lightweight download manager: a small always-on Go daemon that does the
downloading, and a native GTK4 desktop client.

- Segmented HTTP/HTTPS downloads with pause, resume, and crash recovery.
- SQLite state, so a restart resumes partial files instead of starting over.
- Local-only REST + WebSocket API, bound to `127.0.0.1` and protected by a
  bearer token.
- GTK4 client that follows the system theme, with filters, categories,
  preferences, and live progress.
- Small daemon: no browser engine and no web UI to build or ship.

## Install (Arch)

```sh
git clone https://github.com/leodas66/DM.git
cd dm
makepkg -si
```

This installs:

| Path | Purpose |
|---|---|
| `/usr/bin/dm` | daemon and one-shot CLI |
| `/usr/bin/dm-desktop` | GTK4 desktop client |
| `/usr/bin/dm-native-host` | Native Messaging host for the browser extension |
| `/usr/lib/systemd/user/dm.service` | per-user daemon unit |
| `/usr/share/applications/io.github.vishwa.DM.desktop` | application entry |
| `/usr/share/icons/hicolor/scalable/apps/io.github.vishwa.DM.svg` | icon |
| `/usr/lib/mozilla/native-messaging-hosts/io.github.vishwa.dm.json` | system-wide Firefox host manifest (allowlists `dm@vishwa.github.io`) |

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
make build                       # bin/dm and bin/dm-native-host
make desktop                     # bin/dm-desktop (GTK4)
```

Run the daemon in a terminal with `./bin/dm --serve`, then `./bin/dm-desktop`.

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

## Browser integration

The extension in `extension/` sends browser downloads to the daemon through
`dm-native-host`, which speaks Native Messaging on stdin/stdout and forwards
`add`/`ping` to the authenticated loopback API. The extension holds no port and
no token.

```sh
cd extension && npm install && npm run build   # dist/chrome and dist/firefox
# then load dist/chrome unpacked, or dist/firefox as a temporary add-on
```

Chrome needs a per-user host manifest because it allowlists the extension ID,
which is only known after the extension is loaded:

```sh
dm-native-host -manifest chrome -extension-id <id> \
  > ~/.config/google-chrome/NativeMessagingHosts/io.github.vishwa.dm.json
```

Firefox is already covered by the packaged system manifest. The host refuses
non-`http(s)` URLs and strips CR/LF from forwarded headers and cookie names.
See `extension/README.md` for loading, troubleshooting, and settings.

## Layout

- `cmd/dm`: daemon and CLI.
- `cmd/dm-native-host`: Native Messaging host for the browser extension.
- `internal/nativehost`: framing and API forwarding for that host.
- `internal/`: probe, segmented downloader, manager, SQLite store, API, runtime
  endpoint discovery.
- `desktop/`: separate Go module with the GTK4 client.
- `docs/`: architecture, decisions, GTK4 design contract, research notes.
- `packaging/`: desktop entry and icon used by `PKGBUILD`.

## License

MIT. See `LICENSE`.
