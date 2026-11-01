# Ketch: engine, task store and event stream

Ketch (linroid/Ketch, Apache-2.0) keeps one engine, `KetchApi`, and clients around it. Read: `library/sqlite` (SQLDelight store), `library/core`, `library/api`, `library/server`, `library/endpoints/stream`, `library/remote`, `library/ktor`. **No separate daemon by default**: the engine is embedded in the app process, and `app/desktop/.../LocalApiServer.kt:LocalApiServer` optionally serves it on loopback HTTP at a system-picked port with a per-run token for extension and CLI; `RemoteKetch` serves remote devices.

## 1. Store

SQLDelight, not Room, deliberately flat: one table. `library/sqlite/.../TaskRecords.sq` declares `task_records` — `task_id` (PK), `request_json`, `output_path`, `state`, `total_bytes`, `source_type`, `source_resume_state_json`, `segments_json`, `created_at`, `updated_at`, `error_json`, `download_time_ms`, `completed_at`, `control_json`, `partial_path`, `resolved_source_json`. `state` is the coarse `core/task/TaskState` enum (`SCHEDULED/QUEUED/DOWNLOADING/PAUSED/COMPLETED/FAILED/CANCELED`); progress lives in segments, each `api/Segment.kt` being `index, start, end, downloadedBytes`.

`SqliteTaskStore.save` encodes the blobs, then `insertOrIgnore` + `update` in one `transaction{}` under a `Mutex`. Cadence is the engine's: `engine/DownloadExecution.kt` loops on `config.saveIntervalMs` (default 5000, `api/DownloadConfig.kt`), snapshots segments and `source.stored(ctx)`; it saves `sourceState` before the first byte and again in `finally` under `NonCancellable`. Transitions save via `task/AtomicSaver.kt:update`, store-then-swap under a mutex. Migrations are numbered `.sqm` (1–2 rebuild, 3–7 add a column). Startup: `core/Ketch.kt:loadTasks` → `loadAll`, which skips and logs undecodable rows.

## 2. Resume validation

Resume state is per source: migration 2 dropped `etag`, `last_modified` and `accept_ranges` for `source_type` + `source_resume_state_json`, decoded by `engine/SourceState.kt:decodeState`, which only accepts a type this source wrote or once wrote (`previousTypes`), so foreign state cannot validate. HTTP's is `HttpSourceState(etag, lastModified, totalBytes)`.

`engine/HttpDownloadSource.kt:continueFrom` re-probes (`RangeSupportDetector`) and compares: ETag or Last-Modified mismatch → `KetchError.FileChanged`; no range support with incomplete segments → one segment from zero. `validateLocalFile` then checks disk: smaller than claimed progress or the total, it preallocates and zeroes `downloadedBytes`. `If-Range` carries a strong ETag only (weak `W/` skipped, else Last-Modified). Unfinished files are `name.ext.ketch` (`file/PartialFiles.kt:partialFileName`), atomically moved on completion.

## 3. Segmentation and stealing

`segment/SegmentCalculator.kt:calculateSegments` splits evenly and gives the first `remainder` segments one extra byte; `end` is inclusive. No minimum chunk: a 5-byte file at 8 connections yields 5 segments. `resegment` keeps completed work, cuts partials at `currentOffset`.

`segment/SegmentLane.kt:SegmentLedger` gives each incomplete segment a lane, starts at most `connections`, marks the rest waiting. A connection that finishes takes another's tail (`steal`), lowering the victim's `limit`; since sources call `admit` before writing and `commit` after, the victim stops mid-chunk. Splits under `DEFAULT_MIN_SPLIT_BYTES = 1 MiB` are refused, weighed against `setupTime` (0.5 s) over a 1 s rate window. A failed lane is `released`; on retry it `rejoin`s and reclaims its bytes. Per-host caps key on host alone (`DownloadQueue.extractHost`).

## 4. Retries

`engine/RetryPolicy.kt`: `retryDelayMs` (1 s) doubled per consecutive attempt, capped at `maxRetryDelayMs` (60 s), then equal jitter. `retryCount` defaults to 3, or `unlimitedRetries`. One classifier, `api/KetchError.kt:isRetryable`: `Network`, 5xx, 429 retryable; everything else fatal. `DownloadExecution.downloadWithRetry` resets the counter whenever progress happened since the last failure. On 429 it uses `Retry-After` (or `RateLimit-Reset`) verbatim and cuts connections to `RateLimit-Remaining` or halves them. 503 gets no special treatment.

## 5. API and events

Progress is **SSE, not WebSocket**: `GET /api/stream`, then `PUT /api/stream/{id}/subscription` replacing `StreamSubscription(revision, progressIntervalMs, segments, interests)`. Events: `hello`, `subscribed`, `tasks`, then `task.added/removed/state/progress/segments`, `interest*` and `error` (`endpoints/stream/StreamEvents.kt`). Intervals are per stream and per interest, snapped to a 200 ms–60 s ladder. Caps: 32 streams, 64 interests, 1024 task IDs; each interest is sequenced, so a reconnect cannot confuse replayed values. REST: `/api/tasks`, `/api/tasks/{id}/{pause,resume,cancel,files}`, `/api/status`, `/api/config`. Bearer token, ten wrong tokens a minute → `429`, loopback bind by default, Host validation against DNS rebinding, CORS allowlist; `/api/health` answers `503` until the store has loaded.

## 6. Filenames, redirects and the queue

`file/DefaultFileNameResolver.kt` tries `Content-Disposition` (`filename*`, quoted, then bare), then the last non-empty URL path segment (percent-decoded, query and fragment stripped), then `download`; each candidate passes `file/FileNames.kt:sanitizeFileName` — last segment only, control/bidi dropped, Windows-reserved names neutralised, trailing dots and spaces trimmed, 255 UTF-8 bytes keeping the extension — and falls through when nothing usable is left. Redirects are followed by the engine, not the client: at most 20, refusing HTTPS→HTTP and non-HTTP(S); across scheme/host/port it keeps only `User-Agent`, `Accept*` and an origin-only `Referer`, so cookies, `Authorization` and URL user info never travel. Redirect hops are remembered, so segment requests skip them.

Queue state is in memory (`DownloadQueue.kt`): active map, queued list, host counts. Ordering is priority descending then `createdAt` ascending. Defaults 4 concurrent, 16 per host, `0` unlimited; raising a limit starts queued work at once, lowering one never stops a running task. `URGENT` preempts the lowest-priority non-terminal active task, which becomes `Paused(Preempted)`, still queued and self-resuming. Restore (`Ketch.kt:mapRecordState`): `SCHEDULED` reschedules, `QUEUED`/`DOWNLOADING` re-enqueue with `preferResume`, `PAUSED` returns as `PauseReason.User` or `AwaitingFileSelection`, terminals stay terminal.

## 7. UI and API surface

`app/` findings hold and match the server: `Queued/Scheduled/Downloading/Paused(reason)/Completed/Failed/Canceled`, structured `KetchError` with `Http(code, statusMessage, retryAfterSeconds)`, and interest subscriptions. A light web UI should take `task.progress` coarsely, ask for `task.segments` only on the expanded row, keep wording client-side, codes on the wire.

## Ideas worth adopting

1. One `tasks` row per download, JSON columns for request, segments, error, resume state.
2. Save on a timer, before the first byte, and on exit — not every tick.
3. Never persist an empty segment list when the total is known: it resumes as a zero-filled file.
4. Source-scoped resume state (`sourceType` + `previousTypes`), so foreign state cannot validate.
5. SSE with a client-owned subscription (revision, per-interest intervals, quantised) instead of one firehose.
6. Rate-driven lane stealing via `admit`/`commit` with a 1 MiB floor, and 429 → `Retry-After` + fewer connections.

## Mistakes to avoid

1. Trusting `downloadedBytes` against file length only; a same-size wrong partial resumes silently.
2. Upserting with two statements; one `ON CONFLICT DO UPDATE` halves writes.
3. Losing up to `saveIntervalMs` on a hard kill; a byte threshold bounds it.
4. Reading `Retry-After` only on 429; 503 backpressure falls to jitter.
5. Two event APIs: `/api/events` already deprecated beside `/api/stream`.

Sources: Ketch @ c683496
