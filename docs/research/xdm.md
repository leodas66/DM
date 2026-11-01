# XDM (subhra74/xdm) — browser integration and segmented HTTP engine

This is not the 2010s Java/Swing XDM and contains no C#: it is the in-progress **XDM 9 rewrite**, a Maven multi-module Kotlin project (`pom.xml`). `xdm-core` is the engine (Kotlin on OkHttp, Java 8 bytecode); `xdm-app` is a Kotlin Swing/FlatLaf desktop app on a Java 25 toolchain; `browser-extension` holds hand-written MV3 Chrome and Firefox extensions that post to a loopback HTTP server, not a native-messaging host. Torrent is declared (`Models.kt:DownloadType.Torrent`) but `DownloadManager.kt:createTask` returns null for it. Docs the code cites (`docs/batch-downloader.md`, `docs/site-video-detection.md`) are absent. Headers claim Apache-2.0, the root `LICENSE` is GPL-3.0 — treat as GPL, read only.

## 1. Segmentation

A download opens as one `Chunk` at offset 0 (`xdm-core/.../http/HttpDownloader.kt:HttpDownloaderTask.start`); the first response supplies the length, then `splitChuck` halves the largest remaining range (`findMaxChunk`). Splitting is dynamic — on every chunk connect/finish, on a 1 s timer (`updateBytesDownloaded`), and a finishing chunk *steals* the adjacent unstarted chunk via `takeOverChunk` (only `downloaded == 0`, contiguous, outside a 5 s cooldown). `MIN_SPLIT_BYTES = 256 KiB`; smaller tails are raced by `TailRacer.kt` (off under a speed limit). Concurrency is per download: `maxChunk = task.maxPiece ?: config.maxSegments` (default 8), capped by `CoreConfig.MAX_SEGMENTS = 256`, which sizes the OkHttp pool (`DownloadManager.kt:newHttpClient`). There is **no per-host cap** beyond `maxRequestsPerHost`.

## 2. Resume

Metadata is `<configDir>/<id>.state` (`StateSaver.kt:writeContext`) beside `task-<id>.info` (`TaskInfoDB.kt:saveHttpTask`): chunk offset/length/**synced**/status, `totalSize`, `init`, `tempFileCreated`, `completed`, `diskError`, `validator` (Last-Modified), `noRangeSupport`. The part file's path is derived from task and id, not stored (`Models.kt:partFileName`). Only fsynced bytes count — `syncData` forces the file every 30 s, then copies `downloaded` into `synced`. Validation is structural: `mustStartOver` restarts when the first response had no length or ranges were ignored; `piecesMakeUpFile` requires the part file to equal `totalSize` and the chunks to tile `0..size`; `normalizeRestoredChunks` repairs lost finish updates. Freshness uses `If-Range: <Last-Modified>` (`HttpChunkRetriever.kt:rangeHeaders`), so a changed file answers 200 and is refused as `NoResume` — no ETag check, because mirrors differ.

## 3. Retries

Per chunk, in `HttpChunkRetriever.retrieveChunkInternal`: `retryCount` counts only attempts that delivered no bytes (progress resets it), limited by `config.maxRetries` (default 5) in `onRetry`. Delay is a flat 5 s — no backoff. Only 429 is special: `HeaderUtils.kt:getRetryDelay` parses `Retry-After` capped at 60 s; **503's Retry-After is ignored**. `isFatalStatus` fails instantly outside {200, 206, 413, 408, 429, 500, 502, 503, 504}, so 404/416 are fatal. Disk errors are never retried (`onChunkFailed` ends it as `DiskSpaceError`). 403/410, or a 401 with a non-login challenge, is `LinkExpired`; `InvalidResponse` with progress and more than two chunks becomes `SessionExpired`.

## 4. Filenames and redirects

`HeaderUtils.kt:getFileName` prefers RFC 6266 `filename*`, then `filename`, then the URL's last segment; URL names are percent-decoded with `+` kept (`FileUtils.kt:decodeFileName`), stripped of path separators and query/fragment, then `sanitizeFileName` replaces control characters, trims trailing dots/spaces, prefixes Windows device names and caps length. Missing extensions come from Content-Type. Collisions resolve at publish in `DownloadManager.kt:resolveConflict` — overwrite, else auto-rename, else fail `OutputWriteError` — and `publishInFolder` retries the same-directory rename. Redirects use OkHttp defaults (no explicit cap, so 20); `finalUrl` is cached per session, and when a signed target refuses, `connect` clears it and retries through the original link.

## 5. Browser integration

Loopback HTTP on `127.0.0.1:8597` (`BrowserIntegration.kt:PORT`), no native messaging. `RequestFilter` requires a loopback `Host`, rejects non-extension `Origin`s and allows POST-only state changes; the port is also the single-instance lock (`acquire`). The extension pushes `url, cookie, requestHeaders, responseHeaders, filename, fileSize, mimeType, referer, tabUrl, tabId` (`app.js:sendDownload`), then holds a long poll returning a versioned config snapshot (`EventChannel`). Requests are observed, not re-fetched (`request-watcher.js:onSendHeadersEvent`); the app strips hop-by-hop, conditional, `Range` and `Cookie` before replay (`removeBlockedHeaders`), while `Authorization` stays off disk via `SensitiveHeaders`. **The guard is cancel-first**: `app.js:decideDownload` calls `suggest()` then immediately `chrome.downloads.cancel` + `erase`, before XDM accepts anything — admitted in `MSG_BROWSER_DOWNLOAD_FAILED` ("… browser download was already cancelled …", `AppInstance.kt:showHandoffFailed`). Capture is an extension allow-list plus host block-list (`shouldTakeOver`); video matches `videoExtensions`, MIME prefixes and URL regexes (`isMatchingRequest`). Logged-in downloads reuse the browser's Cookie header; the context menu instead calls `chrome.cookies.getAll`.

## 6. Queue, scheduler, speed limiter

One FIFO `ArrayDeque` (`DownloadManager.kt:enqueue`), drained by `pumpQueue` while `activeSessions.size < config.maxParallelDownloads` (default 3); failures leave records `ERROR`, unqueued. `DownloadScheduler.kt:tick` fires minute-granular one-time/weekly entries with optional stop times (`schedule.dat`). Named queues are stubs (`Queues.kt:QueueManager.attachToQueue` is empty). Throttling is a sleep loop over cumulative bytes (`SpeedLimiter.kt:throttleIfNeeded`), but each download owns a limiter reading the same global number, so N downloads each get the full limit. Only `maxPiece` can be overridden per download.

## 7. UI

No columns: a two-line row — icon tile, name, status/size/speed/ETA/date, progress bar, inline open/pause/resume/delete (`ui/components/DownloadRowPanel.kt`). States (`DownloadsDB.kt:RecordStatus`): DOWNLOADING, READY ("Waiting"), PAUSED, ERROR ("Failed"), ASSEMBLING, MOVING, FINISHED. Dialogs: New download (address, file, segments, subtitles), Progress (segment bars, speed, ETA), Download complete (checksum), Properties, Refresh link, Change folder, Schedule, Settings. Failures map from `DownloadError` in `ProgressWindow.kt:errorText`: "Session expired.", "The download link has expired.", "Network error.", "Server sent invalid response.", "Server does not support resume.", "Disk error.", "TLS connection error.", "Unable to save the file."

## Ideas worth adopting

- Validate resume with `If-Range: Last-Modified`, not ETag, so mirror hops still resume.
- Persist only fsynced byte counts, so crash resume never trusts page cache.
- Steal the adjacent unstarted chunk on finish instead of leaving a slow tail.
- Keep the part file id-named and hidden in the destination folder; publish by rename.
- Version the config snapshot with an instance id so stale replies are dropped.
- Make handoff failure loud when the browser copy is already gone.

## Mistakes to avoid

- Cancelling the browser download before the app accepts the task.
- A per-download limiter reading the global value, yielding N× the limit.
- Flat 5 s retries, no backoff, 503's `Retry-After` discarded but 429's honoured.
- No per-host cap; segments split to 256 KiB across a 256-request pool.
- Cookies in cleartext in `task-<id>.info` while only `Authorization` is sensitive.

Sources: xdm @ 805ded0
