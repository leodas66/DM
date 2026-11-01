# Lightweight Download Manager: Spec

Project name: TBD

## 1. Goals
- Tiny always-on daemon (target: under 20 MB RSS idle).
- Fast segmented downloads with reliable pause, resume and crash recovery.
- Browser extension (Chrome + Firefox) that captures downloads.
- Native GTK4 desktop UI; no embedded browser or standalone web frontend.
- Single binary, easy to package on Arch (PKGBUILD + systemd user service).

## 2. Non-goals (v1)
Torrents, FTP, HLS/DASH stream merging, cloud sync, multi-user.

## 3. Stack
- Engine: Go, standard `net/http`, goroutine per segment
- State: SQLite via `modernc.org/sqlite` (pure Go, no CGO)
- API: REST (stdlib mux or chi) + WebSocket for live progress
- Desktop UI: GTK4 with Go bindings (`github.com/diamondburned/gotk4/pkg`), isolated in `desktop/` as a separate Go module
- Extension: TypeScript WebExtension (Manifest V3, Chrome + Firefox) using Native Messaging to a Go host
- Logging: `log/slog`; CLI/config: flags or cobra
- Packaging: PKGBUILD + systemd user service

## 4. Architecture
```
Browser extension --\
                     >-- HTTP/WS (127.0.0.1 + token) --> Daemon
Svelte UI (browser) -/                                    |- API layer
                                                          |- Manager (queue, state owner)
                                                          |- Downloader (probe, segments, writer)
                                                          |- Store (SQLite)
```

Key rule: the Manager owns all download state. Segment workers never touch shared state; they send progress/events over channels to the Manager. No shared mutable structs behind mutexes across goroutines.

### Suggested layout
```
cmd/dm/main.go
internal/model/      core types
internal/probe/      HEAD/range probing, filename detection
internal/download/   segment workers, writer, retry
internal/manager/    queue, state machine, event bus
internal/store/      SQLite persistence
internal/api/        REST + WebSocket + auth
internal/app/        application services between API and Manager
internal/web/        legacy embedded UI server (remove after migration)
extension/           TypeScript WebExtension + Native Messaging client
cmd/dm-native-host/  browser native messaging host
 desktop/            isolated GTK4 desktop client Go module
```

### Core types (define and review first)
- `Download`: ID, URL, FinalURL, Filename, Dir, Size, AcceptsRanges, ETag, LastModified, Status, Segments, Headers, Cookies, Referrer, CreatedAt, Error
- `Segment`: Index, Start, End, Downloaded, Status
- `Status`: queued, probing, running, paused, completed, failed, canceled
- `Event`: DownloadAdded, Progress, StatusChanged, Removed

## 5. API outline
All endpoints bind to 127.0.0.1 only and require `Authorization: Bearer <token>`. Reject requests whose `Origin` is not the UI origin or an allowed extension.

- `POST /api/downloads` add (url, headers, cookies, referrer, dir, filename)
- `GET /api/downloads` list
- `GET /api/downloads/{id}`
- `POST /api/downloads/{id}/pause | resume | cancel`
- `DELETE /api/downloads/{id}` (query: deleteFile=true|false)
- `GET/PUT /api/settings`
- `GET /api/ws` live events

## 6. Security notes
- Localhost bind + token; token generated on first run, stored in config dir with 0600.
- Sanitize filenames (strip path separators, `..`, control chars). Never write outside the chosen directory.
- Validate URL schemes (http/https only).
- Cookies/headers from the extension are stored only for the lifetime of the download.

## 6A. Reference implementations (study, don't copy)

| Project | Language | License | Use it for |
|---|---|---|---|
| Gopeed | Go + Flutter | GPLv3 | Closest to our stack: Go engine, REST API, browser extension protocol |
| aria2 (and Persepolis on top of it) | C++ / Python | GPL (check each repo) | Segmenting, retry, resume, file allocation, option names. The reference engine |
| XDM (subhra74/xdm) | Java (7.x), C# (newer) | GPL (repo analytics show GPL-2.0, check LICENSE) | Browser integration (downloads API + native messaging), video/stream detection |
| AB Download Manager | Kotlin | Apache-2.0 | Queue/scheduler, settings, UI flows, extension-to-app protocol. Note: one review measured ~637 MB RSS, our goal is the opposite |
| Ketch (linroid) | Kotlin | Apache-2.0 | Daemon + REST + persisted task store architecture |
| FDM | C++ | Proprietary freeware from v5.0 (open source only v2.5 to 3.9.7) | Behavior reference only, see below |

### Licensing rules
1. Reading GPL code to understand a design is fine. Copying or translating it into our code would make ours GPL. Decide our own license first.
2. Apache-2.0 projects allow reuse with attribution, but we still reimplement rather than port, so the design fits Go.
3. FDM 5.0+ has no public source. Don't decompile it (e.g. with Ghidra) to lift its logic; that's legally murky and unnecessary. Study behavior from the outside instead: what the UI offers, and the network traffic it produces (browser devtools, mitmproxy, Wireshark). The old 3.9.x source (GPL, C++) is the only FDM code you can read.
4. If you install FDM just to observe it, use a VM or container. Its official site was compromised in the past to serve Linux malware, so download only from the official source and verify.

### Study questions (answer from each reference, with file/function citations)
1. Segmentation: how is the file split? When a segment finishes early, does it split the slowest remaining one (work stealing)? Min segment size, max connections per host?
2. Resume: what metadata is stored, and how is it validated (size, ETag, Last-Modified)? Preallocation vs `.part` files?
3. Retries: backoff, handling of 429/503 and `Retry-After`, what counts as fatal.
4. Filenames and redirects: Content-Disposition, URL decoding, name collisions, sanitizing.
5. Browser integration: native messaging vs localhost HTTP; what the extension sends (cookies, referrer, UA); how it avoids a double download; what it decides to capture (size, MIME, extension).
6. Queue, scheduler, speed limiter design.
7. UI: which columns, dialogs and states users actually see; what the failure messages look like.

### Where each reference helps
| Phase | Study |
|---|---|
| 1 Engine | aria2, Gopeed engine, XDM segment logic |
| 2 Persistence/queue | aria2 session file, Ketch task store, AB DM queue |
| 3 API | Gopeed REST API, Ketch REST + events |
| 4 Extension | XDM integration module, Gopeed extension, AB DM extension |
| 5 UI | AB DM, Gopeed, FDM (behavior only) |

### Phase R: Research (before Phase 1)
Deliverable: `docs/research/<project>.md` per reference (aria2, Gopeed, XDM, AB DM, Ketch) plus `docs/research/decisions.md` listing what we adopt, e.g. work stealing yes/no, native messaging vs localhost HTTP, resume validation rules.
Done when: every study question above has an answer with file/function citations, and decisions.md is reviewed by you.

Prompt (run once per project, read-only clone):
```
Clone <repo URL> into a scratch directory outside our repo. Do NOT copy any code into our project. Read the download engine / browser integration / queue code and write a design note at docs/research/<name>.md that answers these questions: [paste the Study questions list from SPEC.md]. For each answer cite the file path and function name. End with a short "ideas worth adopting" and "mistakes to avoid" list. Keep it under 2 pages.
```

## 7. Phases

### Phase 0: Scaffold and types
Deliverable: repo layout, `go.mod`, core types, empty packages, CI-style script running `go vet` and `go test -race ./...`.
Done when: builds and tests pass, types reviewed by you.

Prompt:
```
Create a Go module for a download manager with this layout: cmd/dm, internal/{model,probe,download,manager,store,api,web}. In internal/model define Download, Segment, Status and Event types as described below. No logic yet, only types, JSON tags and a doc comment per type. Add a Makefile with targets: build, vet, test (go test -race ./...). Run go vet and go test and fix anything that fails.
[paste the Core types list from SPEC.md]
```

### Phase 1: Probe + segmented downloader (CLI)
Requirements:
- Probe: HEAD, fall back to GET with `Range: bytes=0-0`. Detect size, range support, ETag, Last-Modified, filename (Content-Disposition, then URL path), redirects.
- Split into N segments (default 8, configurable, minimum segment size e.g. 1 MiB).
- Preallocate file, each worker writes with `WriteAt`.
- Fallback to single stream if no range support or unknown size.
- Retry with exponential backoff and jitter; per-segment retry limit.
- Progress events via channel.
Done when: downloads a 1 GB test file correctly (checksum matches), including against a mock server that drops connections mid-transfer.

Prompt:
```
Implement internal/probe and internal/download in Go. Probe: HEAD request, fall back to GET with Range bytes=0-0; return size, accepts-ranges, ETag, Last-Modified, final URL after redirects, and a sanitized filename. Download: split into N segments, preallocate the file, one goroutine per segment writing with WriteAt, report progress over a channel, retry with exponential backoff and jitter, fall back to a single stream if ranges are unsupported. Segment workers must not share mutable state; they only send events on a channel. Write tests using httptest with a server that supports Range requests and one that randomly closes connections mid-body. Verify SHA-256 of the output. Run go vet and go test -race ./... until clean.
```

### Phase 2: Persistence, resume, queue
Requirements:
- SQLite schema for downloads and segments; save segment progress about once per second and on pause.
- Resume: on restart, reload unfinished downloads; verify ETag/Last-Modified before reusing partial data; if changed, restart cleanly.
- Manager: state machine, max concurrent downloads, global and per-download speed limit.
Done when: kill -9 during a download, restart, and it resumes to a correct file.

Prompt:
```
Implement internal/store (SQLite via modernc.org/sqlite, migrations on startup) and internal/manager. The Manager owns all download state and is the only writer to the store. It receives events from segment workers over channels, persists progress about once per second, enforces a max-concurrent limit and a token-bucket speed limit (global and per download). On startup it reloads unfinished downloads; before resuming it re-probes and compares ETag/Last-Modified, restarting from zero if they changed. Add tests including a simulated crash (cancel context abruptly, rebuild the manager from the DB, finish the download, verify checksum). Run go vet and go test -race ./... until clean.
```

### Phase 3: HTTP API + live events
Requirements: endpoints from section 5, WebSocket event stream, token auth, Origin checks, graceful shutdown.
Done when: all actions work via curl and a WebSocket client; unauthenticated requests are rejected.

Prompt:
```
Implement internal/api exposing the REST endpoints and WebSocket event stream from SPEC.md section 5. Bind to 127.0.0.1 only. Require a bearer token (generate on first run, store in the user config dir with mode 0600). Check the Origin header against an allowlist. Broadcast Manager events to all WebSocket clients. Add graceful shutdown that pauses running downloads and persists state. Write handler tests with httptest, including auth failures. Run go vet and go test -race ./... until clean.
```

### Phase 4: Browser extension (legacy transport; migration planned)
Requirements:
- Intercept downloads (`downloads` API), send URL, referrer, headers, cookies to the daemon.
- Cancel the browser's own download only after the daemon accepts it.
- Context menu "Download with DM"; popup to paste URL.
- If the daemon is unreachable, let the browser download normally.
- Settings: daemon port, token, minimum file size to capture, extension allow/deny list.
Done when: a logged-in download (cookie-protected) works on both Chrome and Firefox.

Prompt:
```
Create a TypeScript WebExtension (Manifest V3, works on Chrome and Firefox) in extension/. It listens to downloads.onCreated, cancels the browser download only after POST /api/downloads to the local daemon succeeds (sending url, referrer, cookies for the URL, and relevant headers), and falls back to the browser's own download if the daemon is unreachable. Add a context menu "Download with DM", an options page for port and token, and a minimum-size filter. Use a small build setup (esbuild). Include a README with load-unpacked steps for both browsers.
```

### Phase 5: Legacy Svelte UI (implemented; scheduled for removal)
The Svelte UI was an intermediate client. It will be replaced by the native GTK4
client described in `docs/ARCHITECTURE.md`; do not add new features to `ui/` or
the Wails frontend. Delete the legacy web assets only after GTK parity and
packaging checks pass.

Prompt:
```
Create a Svelte + Vite app (no SvelteKit, no component library) in ui/. Features: download list with progress bar, speed, ETA and status; add-URL dialog; pause/resume/cancel/remove actions; settings page; live updates via the WebSocket at /api/ws; token read from the URL fragment on first load and kept in memory/localStorage. Plain CSS with variables for dark/light. Output static files that internal/web embeds using go:embed and serves at /. Keep the gzipped bundle small and report its size.
```

### Phase 6: Extras
- Categories with default folders, scheduler, clipboard link detection, yt-dlp integration for video sites.
- Tray integration and PKGBUILD + systemd user service.

## 10. Approved architecture migration
See `docs/ARCHITECTURE.md` for the authoritative target architecture and staged
migration. The approved work is:
1. Add application services between API handlers and Manager.
2. Add a Go Native Messaging host; the browser extension must not call the
   loopback API directly. The host forwards accepted requests to the existing
   authenticated daemon API.
3. Build a native GTK4 desktop client in the separate `desktop/` Go module.
   Keep GTK/cgo dependencies out of the daemon module and its normal CI gate.
4. After native feature parity, remove `ui/`, `internal/web/`, Wails and all
   Svelte/Vite artifacts. Keep REST/WebSocket for native desktop, native host
   and future clients.

## 8. Rules for working with the AI tool
1. One phase per session; paste the relevant section and prompt only.
2. Review core types and the Manager's state machine yourself.
3. After every change: `go vet`, `go test -race ./...`; paste failures back until green.
4. Never let it add dependencies without asking; prefer stdlib.
5. Keep a `TESTING.md` of manual checks (huge file, flaky Wi-Fi, server without ranges, expired links).
6. Commit after each green phase.
7. Run Phase R first and give the AI the relevant `docs/research/*.md` notes plus `decisions.md` as context for each phase, instead of letting it guess.
8. Never paste code from GPL references into prompts or the repo. Paste your own design notes.

## 9. Test matrix (manual, before calling a phase done)
- Server with ranges, without ranges, with unknown Content-Length
- Redirect chains and Content-Disposition filenames
- Mid-download disconnect, server error 5xx, 429 with Retry-After
- Pause/resume 20 times in a row, then verify checksum
- kill -9 and restart
- Disk full, destination folder removed
- File changed on server between pause and resume