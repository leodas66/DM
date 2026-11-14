# DM architecture

## Product shape

DM is a lightweight download service with independent clients. The Go daemon is
the only process that owns download lifecycle state, queue scheduling, network
workers and persistence. Closing a client must never stop active downloads.

The supported first-party clients are:
- GTK4 desktop application (native Linux widgets, Go).
- Browser extension (Chrome/Firefox) connected through Native Messaging.
- CLI.

There is no long-term web UI, embedded browser UI or Wails frontend. The Svelte
and Wails code is transitional and must be removed after GTK feature parity.

## Target architecture

```text
CLIENTS
  GTK4 desktop ----------- HTTP REST + WebSocket -----------+
  CLI -------------------- HTTP REST / daemon commands -----+--> GO DAEMON
  Browser extension                                        |
       | Native Messaging                                  |
       v                                                   |
  dm-native-host ---------------- authenticated HTTP ------+
                                                            |
GO DAEMON                                                    |
  API layer                                                  |
       |                                                     |
  Application services                                       |
       |                                                     |
  Download Manager (single owner of state)                  |
       |                                                     |
  Download engine                                            |
       |                                                     |
  SQLite persistence <---------------------------------------+
```

## Daemon layers

### API

The API owns transport concerns only: HTTP routing, request decoding,
authentication, origin policy, response encoding and WebSocket event delivery.
It must not contain use-case orchestration or directly manipulate persistence.

### Application services

Add `internal/app` as the use-case boundary between API and Manager.

- `DownloadService`: add/list/get/pause/resume/cancel/remove downloads.
- `QueueService`: categories, queue operations and scheduling.
- `SettingsService`: read/update validated runtime settings.

Services may validate and coordinate use cases, but must not own a second copy
of download state. The Manager remains the only state owner and only store
writer. Services depend on narrow Manager-facing interfaces where practical;
API handlers depend on service interfaces, not the concrete Manager.

### Manager and engine

Preserve the Manager actor model. Only its event loop mutates the item map and
only Manager writes to SQLite. Workers report through channels. The engine
remains independent of GTK, browser APIs and HTTP transport.

## GTK4 desktop client

Use `github.com/diamondburned/gotk4/pkg` v0.4.1, importing generated GTK4
bindings from `github.com/diamondburned/gotk4/pkg/gtk/v4` and related packages.

Keep the desktop application in a separate Go module at `desktop/`. This is
intentional:
- GTK4 and cgo development libraries are not required to build/test the daemon.
- The daemon stays small and usable headlessly.
- Desktop code cannot reach Manager internals; it consumes the same public API
  as other clients.
- CI can run daemon tests everywhere and GTK compilation in a Linux job with
  GTK4 development packages installed.

The desktop app is a client only. It discovers the daemon endpoint and token,
connects to REST and WebSocket, and presents native GTK widgets. It must not
start its own Manager or open SQLite.

System build prerequisites on Arch Linux:
`gtk4`, `gobject-introspection`, `pkgconf` (plus a C compiler).

The gotk4 generated package module is MPL-2.0. The upstream repository also
contains an AGPL-3.0 code generator, but DM depends only on the generated
`pkg` module. Do not add the generator module as a dependency.

## Browser integration: Native Messaging

The browser extension communicates only with the Native Messaging host; it
does not call localhost HTTP directly.

1. The extension sends a framed JSON request to `dm-native-host` over stdin.
2. The host validates the message, resolves the running daemon's address,
   loads the existing 0600 bearer token and calls the authenticated REST API.
3. The host returns a framed JSON success/error response to the extension.
4. The extension cancels the browser's original download only after the host
   confirms that the daemon accepted it.

The host is a small Go executable (`cmd/dm-native-host`) with no GTK
dependency. Chrome and Firefox each need a Native Messaging manifest that
points to the installed host executable and allowlists the extension ID.
The host uses the Native Messaging 32-bit little-endian length prefix and
bounded JSON messages; stdout is reserved exclusively for protocol frames,
with diagnostics sent to stderr.

Daemon discovery is via a small runtime metadata file under
`$XDG_RUNTIME_DIR/dm/` containing the loopback listen address. The host reads
the bearer token from the existing config file; it does not duplicate or
persist credentials. The daemon writes metadata atomically with restrictive
permissions and removes it on clean shutdown. If the daemon is unavailable,
the host returns a structured error and the extension leaves the browser
download untouched.

The API remains loopback-only and bearer-authenticated. It remains useful to
the GTK client, CLI and native host; Native Messaging replaces only the
browser extension's direct HTTP transport.

## Migration order

1. Introduce and test application services; route API handlers through them.
2. Add daemon runtime endpoint metadata and test lifecycle/permissions.
3. Implement and test `dm-native-host` framing, dispatch and REST forwarding.
4. Migrate the extension to Native Messaging. Keep accept-before-cancel
   semantics and cookie/header handling.
5. Build GTK4 desktop client in `desktop/`, including downloads, add, controls,
   categories, settings and live progress.
6. Update Arch packaging, systemd integration and CI for daemon, host and GTK
   client.
7. Verify parity, then delete `ui/`, `internal/web/`, Wails sources and
   Svelte/Vite dependencies. Remove obsolete frontend build workflows and docs.

Status 2026-11-14: step 2 is done (`internal/runtime`, published by
`cmd/dm --serve`). Step 3 is done for the main surface: the GTK client in
`desktop/` covers the main window, filters, list, add, delete confirmation, live
progress, preferences, and category management. Retry is still missing because
the API has no endpoint for it. Steps 1, 2, 4, 6, and 7 are not started; step 4
(removing the legacy web UI) stays blocked until parity is complete.

Do not delete the old UI until step 5 and the packaging checks are green.
