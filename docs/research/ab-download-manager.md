# AB Download Manager — design notes

AB DM is a Kotlin/Compose downloader: a platform-independent engine in `downloader/core` driven by `DownloadManager` (jobs) and `QueueManager` (queues), with a UI that renders a 500 ms snapshot of engine state. It is close to our target — but stores one JSON file per download, keeps every item resident, and has no per-host connection control.

## 1. Segmentation

`utils/SplitToRange.kt:splitToRange` splits once up front: `actualPartCount = min(threadCount, ceil(size/minPartSize))`, ranges inclusive. `DownloadSettings.kt` defaults: 8 threads, 512 KiB minimum part, dynamic part creation on.

Stealing is zone-based: `part/PartSplitSupport.kt` gives each part a 1 MiB read-ahead safe zone (`SAFE_ZONE_SIZE = 128*8192`), and `splitPart()` halves the region beyond it, so no byte is ever re-requested. `HttpDownloadJob.beginDownloadParts` starts idle parts first, then splits the part with the largest `remainingLength`, refusing when the half would fall under `minPartSize` (`canSplit`). No per-host cap: `Di.kt:BaseOKHttpClientQualifier` sets `maxRequestsPerHost = Int.MAX_VALUE`.

## 2. Resume

JSON, not SQL: `db/DownloadListFileStorage.kt` writes `<downloads>/<id>.json`, `PartListFileStorage.kt` writes `<parts>/<id>.json` holding `RangedParts` (`from`, `to` nullable = "blind", `current`). Item fields (`HttpDownloadItem.kt`): link, headers, credentials, userAgent, `serverETag`, contentLength, status, speedLimit, timestamps. `DownloadJob.kt:startAutoSaver` rewrites both every second per active job.

`HttpDownloadJob.fetchDownloadInfoAndValidate`: the first probe is a 256-byte range (`HttpDownloaderClient.test`), giving `resumeSupport = isPartial && contentLength != null && contentRange.fullSize != null` (`HttpResponseInfo.kt`). On resume it compares length and ETag (`LengthChangedException`, `ETagChangedException`) and throws `ServerResumeSupportChangeException` if concurrency was previously supported; Last-Modified only stamps local mtime. The file must also exist at exactly `contentLength` bytes (`SimpleDownloadDestination.isDownloadedPartsIsValid`), else all parts reset. Preallocation (`EmptyFileCreator.kt:prepareFile`) checks free space, then sparse file (`SparseFile`) or a zero-fill loop — hence `PreparingFile(percent)`.

## 3. Retries

Two fixed-delay layers, no backoff, no jitter. Per part (`part/PartDownloader.kt`): `PART_MAX_TRIES = 10`, `RetryDelay = 1000L`; only a critical `DownloadValidationException` escalates to "stop the job". Per job (`HttpDownloadJob.downloadFailedRetryOrPause`): `delayForEachRetry = 3_000`, and the counter resets whenever `getDownloadedSize()` grew — the one good idea here. `getMaxAllowedRetries()` reads `settings.maxDownloadRetryCount`, default `0`, so job retries are off out of the box.

No `Retry-After`: a search for `retry-after`/`429`/`503` hits only `HttpStatusDownloadErrorMapper.kt`, which formats messages. 429/503 are ordinary retryable failures; fatal are critical validation errors and network/response errors on the first resume attempt.

## 4. Filenames and redirects

`HttpResponseInfo.fileName`: `Content-Disposition` → URL → host, with `.html` forced for HTML. `headers/fileNameExtractor.kt` parses `filename*=UTF-8''…` and RFC 2047 words; `HttpUrlUtils.extractNameFromLink` tries a `filename` query param, then the last path segment percent-decoded.

`DownloadManager.addDownload` treats same folder+name as duplicate (`DuplicateFilterByPath`), then `FileNameUtil.numberedIfExists` yields `name_1.ext`. `FilenameFixer.fix` maps per-OS illegal chars (Windows also trims trailing dots/spaces); `FileNameValidator.isValidFileName` requires the canonical name to be unchanged, subsuming `..` and separators.

Redirects are unconfigured: `OkHttpHttpDownloaderClient` uses the shared client (OkHttp defaults, 20 hops) and takes `requestUrl` from `response.request.url`, so naming uses the final URL.

## 5. Browser integration

Two transports, no WebSocket. Native messaging: `nativemessaging/host/NativeMessagingHost.kt` speaks stdio framing (`StdioProtocol.kt`: 4-byte length prefix, 4 MB cap) carrying `NativeMessagingMessage` — id, optional `action`, `isError`, string `payload`; only `add` and `ping` exist (`DefinedHandlers.kt`), and `add` forwards over single-instance IPC, waking the app. HTTP: `Endpoints.kt:setupRouting` on `127.0.0.1:15151`, optional `X-Api-Key`, with `/add`, `/queues`, `/ping`, `/start-headless-download`.

Payloads (`DownloadCredentialsFromIntegration.kt`) carry `link`, `headers: Map<String,String>`, `downloadPage`, `suggestedName` — no cookie/referrer/UA field; the extension puts them in `headers`. Duplicates use `OnDuplicateStrategy.default() = AddNumbered`, and `add` returns a boolean so the caller can cancel the browser's copy. Extension logic for cancelling and capture filters (size/MIME/extension) is a separate repo — **not verified in this clone**.

## 6. Queue / scheduler / speed limiter

Multiple queues (`queue/QueueManager.kt`), default `Main` (id 0). `QueueModel`: `maxConcurrent` default 2, ordered `queueItems`, `scheduledTimes`. Ordering is positional FIFO with explicit move/swap (`DownloadQueue.move`); no priority field. `actualShake` reconciles active items against `maxConcurrent` — `extend()` starts the first eligible item, `trim()` stops the last; shakes debounce 500 ms to survive concurrent cancels. Non-queued downloads are capped separately (`ManualDownloadQueue.kt`, fed by `maxConcurrentDownloads`, default 3). Scheduling is per queue: day-of-week plus start/end window (`ScheduleTimes.kt`, default 02:30–07:30, off), re-armed daily; state is `QueueModel` JSON, persisted on change.

Limits are layered token buckets: `OkioCustomizedThrottler.kt` resets `allocatedUntil` on change so new limits apply immediately; each part chains global and per-job limiters as `Source` wrappers. `pause` cancels the scope, stops all parts, saves, and records the throwable in `DownloadJobStatus.Canceled(e)`; resume continues from `RangedPart.current`.

## 7. UI

Columns (`DownloadList.kt:DownloadListCells`): `#`, Name, Size, Status, Speed, Time Left, Date Added, Date Started, Date Finished, Location — last three hidden by default. Visible statuses derive from `DownloadJobStatus` (adds `Retrying(seconds)`, `PreparingFile(percent)`) into Added/Paused/Downloading/Resuming/CreatingFile/Error; persisted `DownloadStatus` is only Error/Added/Paused/Downloading/Completed. Dialogs under `pages/`: add (single/multiple), enter URL, edit, checksum, error details, new queue, queue info, per-host settings, settings, confirm-exit.

Failures are always a triple — short title, causal description, actionable suggestion: "Too Many Requests" / "…too many downloads were started in a short period of time." / "Wait before retrying or reduce the number of parallel downloads and threads." (`en_US.properties`).

Cost is state propagation, not painting. `DownloadMonitor.startUpdateActiveDownloadList` merges job events, a speed flow and a 500 ms interval, then rebuilds the whole list of immutable `ProcessingDownloadItemState` objects, each holding a `List<UiPart>` of every part. For a web UI: row-keyed deltas over one WebSocket, one SQLite row per download, per-part detail only when a pane is open.

## Ideas worth adopting

1. Zone-based splitting with a 1 MiB safe zone: a worker hands off its tail without re-requesting bytes.
2. `minPartSize` gate plus split-the-largest-part on demand: small files stay single-stream.
3. Probe resume support with a 256-byte range before committing to segments.
4. "Progress since last failure resets the retry counter" — stops killing merely flaky downloads.
5. Error triples (title / why / what to do) keyed by HTTP status.
6. Global and per-download limiters as stacked reader wrappers, re-tunable live.

## Mistakes to avoid

1. **Credentials on disk.** `HttpDownloadItem` is `@Serializable` with `headers`, username/password and `userAgent`, written to `<id>.json` in cleartext.
2. **One JSON file per item.** `addDownload` calls `dlListDb.getAll()` (reads every file) per add, and `startAutoSaver` rewrites per item every second.
3. **Fixed retry delays, no `Retry-After`**, with `maxDownloadRetryCount` default 0: 429s either spin or give up instantly.
4. **Whole-list rebuild every 500 ms**, with a `List<UiPart>` per item — GC pressure and full recomposition.
5. **Unbounded fan-out and residency.** `maxRequestsPerHost = Int.MAX_VALUE` plus a live job per non-completed download scale with library size, not with what is on screen.

Sources: ab-download-manager @ 14a947b
