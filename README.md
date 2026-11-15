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
cd DM
makepkg -si
```

The PKGBUILD downloads the prebuilt daemon, Native Messaging host and GTK4
desktop client from the latest `continuous` GitHub prerelease for your
architecture. It verifies the bundle against the release's `SHA256SUMS` file
and installs the binaries plus the systemd unit, desktop entry, icon and
Firefox Native Messaging manifest. The package version follows the current
main commit, and the downloaded bundle must match that commit's short SHA.
A local Go/GTK development toolchain is not needed to install the package.

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

## Automated builds and releases

Pull requests run the Go and GTK4 test/build checks in the `ci` workflow.
Full release bundles are built for Linux x86-64 and ARM64 only after pushes to
`main`, then published to the rolling `continuous` prerelease at
https://github.com/leodas66/DM/releases/tag/continuous. The release workflow
can also be started manually from GitHub Actions. The Arch PKGBUILD tracks
that channel and derives its package version from metadata embedded in the
downloaded bundle.

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

The extension in `extension/` sends browser download requests through
`dm-native-host` to the GTK desktop review dialog. DM shows the original URL,
source page, file details and destination folder; the request reaches the
daemon only after the user confirms **Add Download**. If the UI cannot accept
the request, the browser's own download continues normally.

```sh
cd extension && npm install && npm run build   # dist/chrome and dist/firefox
```

For Firefox, the Arch package already installs the system Native Messaging
manifest. Load `extension/dist/firefox/manifest.json` through
`about:debugging#/runtime/this-firefox`. Chrome needs a per-user host manifest
because it allowlists the extension ID; see `extension/README.md`.

The Native Messaging host starts `dm-desktop` if necessary and uses a 0600
per-user Unix socket for the transient browser request. Cookies and headers
remain in memory and are sent to the daemon only after confirmation. See
`extension/README.md` for settings and troubleshooting.

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
