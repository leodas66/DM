# Gopeed: HTTP engine, persistence, and browser handoff

Our closest stack match: a Go core (HTTP/FTP/BT/HLS), a bbolt store, a REST
server, a Flutter client. The HTTP fetcher is well-factored around a per-task
state machine and a chunk list; the weak points are distribution (the core links
into a GUI as a C library, not a daemon), dependency weight, and no bandwidth
control at all.

## 1. Segmentation

The unit of work is `chunk{Begin int64, End *int64, Downloaded int64}`, inclusive
end, nil meaning "extent unknown" (`internal/protocol/http/fetcher.go:chunk`).
No chunk grid, no central allocator: a fresh download is one connection, and
`slowStartController` grows the count in doubling batches (1, 2, 4, 16, … capped
at max) via `commitBatch`, each new connection halving the largest remaining
chunk (`fetcher.go:expandConnections`). A connection that *finishes* calls
`helpOtherConnection`, taking the latter half of the peer with the largest
remaining time (only if > 3 s, `stealThresholdSeconds`), floor 512 KiB
(`stealMinChunkSize`). Finish-driven stealing, not a queue. The cap is per task
only — `OptsExtra.Connections`, else `config.Connections`, default 16
(`fetcher_manager.go:DefaultConfig`); no per-host limiter. Each connection holds
a 64 KiB buffer (`connReadBufferSize`): 256 connections cost 16 MiB.

## 2. Resume

bbolt (`go.etcd.io/bbolt`), one `gopeed.db`, JSON values, buckets `task`,
`save`, `task_stats` (`pkg/download/storage.go:NewBoltStorage`, const block in
`pkg/download/downloader.go`, written by `downloader.go:saveTask`). The `save`
bucket holds `fetcherData`: the connection array (ID, Role, State, Downloaded,
Completed, chunk Begin/End/Downloaded) plus `RedirectURL`, `IfRange`, and the
pinned range-mode flags (`RangeReprobeEligible`, `RangeValidatorPinned`,
`SequentialSizeUnknown`, `SizeKnown`, `Range`, `ResourceSize`, `FileSize`) —
`fetcher_manager.go:Store`. Checkpoints land on any progress tick where bytes
moved — 350 ms by default (`downloader.go:shouldSaveTaskCheckpoint`).

No `.part` file: bytes go in place into the final path via `WriteAt`, and the
file is preallocated with `os.Truncate`
(`internal/controller/controller.go:DefaultFileController.Touch`). Validation is
per request: the saved validator (strong ETag, else Last-Modified only when ≥1 s
older than `Date`) is sent as `If-Range`
(`internal/protocol/http/helper.go:extractIfRangeValidator`); a 200 restarts that
connection from zero (`fetcher.go:restartSequentialDownload`); `Content-Range`
start/end/total must echo the request and recorded size or the connection dies
terminally (`fetcher.go:validateRangeResponse`). Nothing stat-checks the partial
file's size, and on restart every unfinished task is forced to `pause`
(`downloader.go:Setup`).

## 3. Retries

Per-connection `retryTimes`, linear delay of n seconds capped at 5 s, no jitter,
no `Retry-After` parsing anywhere (`fetcher.go:runConnection`). Classification is
the useful part: 5xx, 429, 408, 440 and 499 are "failure exempt" and never
consume a retry, so they retry forever; other 4xx consume the budget and die at
3; 403 means per-server connection limit and fails immediately
(`helper.go:isFailureExemptHTTPCode`, `fetcher.go:runConnection`). Timeouts adapt: the fast-fail
client uses 1.5× the observed connect time, 3 s floor
(`helper.go:buildFastFailClient`).

## 4. Filenames and redirects

`helper.go:parseFilename` tries RFC 5987 `filename*=`, then `mime.ParseMediaType`,
then a manual scan; `decodeFilenameParam` unwinds HTML entities, RFC 2047
encoded-words, percent-escapes and GBK. `appendFilenameExtension` fills a
MIME extension when the URL segment is opaque. Sanitizing is
`pkg/util/path.go:SafeFilename` (invalid chars to `_`, 100-byte truncation),
applied in `downloader.go` task preparation;
collisions use `util.CheckDuplicateAndRename` ("name (1).ext") when
`fetcherManager.AutoRename()` is true, as for HTTP. Redirects are not
customized — Go's 10-hop default — but the resolved URL is persisted for chunk
requests, falling back to the original when it expires via
`helper.go:tryFallbackToOriginalURL`.

## 5. Browser integration

Not verified in this clone: the extension lives in a separate repo
(`GopeedLab/browser-extension`, per `README.md`). The host side is here:
`cmd/host/main.go` is a Chrome **native messaging** host — 4-byte native-endian
length prefix plus JSON `{method, meta, params}` on stdin/stdout, methods
`ping`, `wakeup`, `create`, `forward`. It boots the GUI via the `gopeed:` URI
scheme, then POSTs to Flutter's loopback RPC (`/create`, `/forward`) with metadata
in `X-Gopeed-Host-Meta`. The app also installs the manifest
(`browser_extension_host.dart:installHost`).
A headless path exists: `POST /api/v1/resolve` then `POST /api/v1/tasks`, with
cookies, referrer and UA as plain `ReqExtra.Header` entries
(`pkg/protocol/http/model.go:ReqExtra`). Capture filtering and the "cancel the browser
download only after acceptance" rule are extension-side; the daemon just
returns a task id or an error.

## 6. Queue, scheduler, speed limiter

One FIFO slice `waitTasks`; `downloader.go:notifyRunning` pops exactly one entry
when a slot frees. Concurrency is `MaxRunning`, default 5
(`pkg/base/model.go:DownloaderStoreConfig.Init`). `Continue` is intrusive: if the
request would exceed `MaxRunning` it pauses running tasks and requeues them as
`wait` (`downloader.go:Continue`). Pause bumps `runGeneration`, calls
`fetcher.Pause()` (drains goroutines, materializes any prefetch prefix), then
`saveTask` (`fetcher.go:Pause`). There is **no bandwidth limiter** at this
commit — no max-speed field; speed is measured for display only.

## 7. API and UI surface

Routes are one table in `pkg/api/service.go:NewService` (`info`, `resolve`,
`tasks`, `tasks/{id}/pause|continue|status|stats`, `config`, `extensions/*`),
mounted in `pkg/rest/server.go:buildServer`; the envelope is `Result{code,msg,data}`
(`pkg/rest/model/result.go`). Task JSON carries `id`, `protocol`, `meta`,
`status` (`ready|running|pause|wait|error|done`, `pkg/base/constants.go`),
`progress{used,speed,downloaded,uploadSpeed,…,extractStatus}` and timestamps;
`/tasks/{id}/status` returns a deliberately small `TaskRuntimeStatus`
(`pkg/download/model.go`), `/stats` per-connection `{downloaded,total,
completed,failed,retryTimes}` (`pkg/protocol/http/model.go:StatsConnection`).

Events are **not** WebSocket. `download.Downloader` calls one `Listener`;
`pkg/api/events.go:emitTaskEvent` converts it to
`TaskEvent{type:"task.<key>", taskId, name, error}` handed to
`SubscribeTaskEvents(mask, callback)`, which Flutter receives over FFI
(`bind/desktop/main.go:SubscribeTaskEvents`). Rendered fields live in
`ui/flutter/lib/features/tasks/domain/task_record.dart`: name, status,
downloaded/total, speed, remaining, error, file list.

## Ideas worth adopting

1. `If-Range` resume behind a strong-ETag-else-old-Last-Modified helper: ~30
   lines, kills a class of silent corruption.
2. Finish-driven work stealing (512 KiB floor, ETA gate) over a central
   allocator — no dispatcher goroutine, no shared queue behind a mutex.
3. Store the chunk table with the fields that justify it (validator, range mode,
   size) so offsets cannot pair with a stale validator.
4. A slow-start ramp gated on successful responses: polite, and the natural home
   for a 429 cooldown.
5. Two-tier retries (non-counting 5xx/429/408, counted 4xx, fail-fast 403) plus
   `Content-Range` echo validation.
6. A small status DTO and typed event struct instead of shipping the whole task
   every tick.

## Mistakes to avoid

1. Core reachable only via GUI plumbing (`bind/desktop` C callbacks, `cmd/host`
   native messaging, `gopeed:` wakeup); a daemon owns its own event stream.
2. Dependency weight: `anacrolix/torrent`, `goja`, `wazero`, `mholt/archives`,
   `go-git`, FTP, ED2K, an MCP SDK in one binary — incompatible with ~20 MB idle.
3. Rewriting task plus fetcher snapshot as JSON every ~350 ms tick.
4. No bandwidth limiting, no `Retry-After`, and `slowStartController.paused` is
   declared but never set: the promised 429 cooldown does not exist.
5. Preallocating the final path with no `.part` sidecar: an interrupted download
   looks complete to other programs, and resume never re-validates on-disk size.

Sources: gopeed @ fc0a1e0
