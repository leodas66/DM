# Phase R: decisions

What we take from the five reference projects, and what we deliberately leave.
Each decision names the note it comes from; the notes carry `path:function`
citations, and the ones marked ✔ were spot-checked against the clones on
2026-11-01. Nothing here is copied code — these are design choices, not ports.

Clones (read-only, `/tmp/opencode/research`):

| Note | Commit | Vintage |
|---|---|---|
| `aria2.md` | `9e72735` | C++ engine, RPC only, no GUI |
| `gopeed.md` | `fc0a1e0` | Go engine embedded in Flutter app |
| `xdm.md` | `805ded0` | XDM 9 rewrite, Kotlin engine + MV3 extensions |
| `ab-download-manager.md` | `14a947b` | Kotlin/Compose, JSON-per-download store |
| `ketch.md` | `c683496` | Kotlin engine + SQLDelight, SSE, no daemon |

## R1. Segmentation and work stealing

- Split into **8 segments by default**, **1 MiB minimum** per segment. Matches
  PLAN, and sits between the references (AB DM 512 KiB, Gopeed 512 KiB steal
  floor, XDM 256 KiB, Ketch 1 MiB ✔).
- **Work stealing is mediated by the Manager.** A worker that finishes reports
  `done`; the Manager halves the largest remaining segment if both halves stay
  ≥ 1 MiB and hands one half to the idle worker. No peer-to-peer stealing
  (Gopeed `helpOtherConnection`, XDM `takeOverChunk`), no shared lane ledger
  (Ketch `SegmentLedger`) — both would break our "workers own nothing" rule.
  Phase 1 ships fixed segments; the halving hand-off lands with the Manager in
  Phase 2, because until then nothing owns the segment table between workers.
- No mid-flight rebalancing of an *running* segment, no per-connection bitfield.
  aria2's model (`BitfieldMan.getSparseMissingUnusedIndex`) is elegant but it
  exists to serve BT; HTTP-only does not need it.
- **Avoid:** Ketch splitting a 5-byte file into 8 segments, XDM's 256 KiB
  splits across a 256-request pool, AB DM's `maxRequestsPerHost =
  Int.MAX_VALUE`.

## R2. Resume validation

- Persist per download: `FinalURL`, `Size`, **strong** `ETag`, `LastModified`,
  and the segment table (`start`, `end`, `downloaded`) plus bytes actually
  `fsync`ed.
- On restart: re-probe, then **restart from zero** if size changed, strong ETag
  changed, or (no ETag) Last-Modified changed. Gopeed, XDM and Ketch all gate on
  a validator; aria2 validates length only and silently corrupts ✔.
- Send `If-Range` with the strong ETag, else Last-Modified. A `200` to a range
  request means "file changed" → restart. Weak `W/` ETags are skipped (Ketch).
- Persist **only fsynced bytes** (XDM `syncData`), saved on a ~1 s tick, on
  pause, on shutdown, and **before the first byte** (Ketch).
- File layout: `<filename>.part` in the destination dir, preallocated, atomically
  renamed on completion (Ketch `PartialFiles`, XDM publish-by-rename). Rejected:
  preallocating the final name (Gopeed) — an interrupted download looks complete
  to every other program on the system.
- Startup: `queued`/`running` records re-enter the queue and revalidate;
  `paused` stays paused (do not force-pause unfinished tasks like
  `downloader.go:Setup`).

## R3. Retries

- Exponential backoff, base 1 s, doubling, cap 60 s, **equal jitter** (Ketch
  `RetryPolicy`). PLAN requires backoff + jitter; none of aria2, Gopeed, XDM or
  AB DM has both.
- Per-segment attempt limit **5** (aria2 5, XDM 5, Ketch 3, AB DM 10).
- Retryable: network/DNS/timeout, 408, 425, 429, 500, 502, 503, 504.
  Fatal: 400, 401, 403, 404, 410, 416, disk-full, checksum mismatch.
- **429 and 503 honour `Retry-After`** (delta-seconds or HTTP-date), capped at
  60 s. XDM does 429 only, Ketch does 429 only, aria2/Gopeed/AB DM ignore it
  entirely ✔. This is a deliberate improvement, not a port.
- A failure after real progress **resets** the attempt counter (Ketch, AB DM,
  XDM all do this; it stops killing merely flaky downloads).
- A 416 on a range triggers one full re-probe; if the size is still wrong, fatal.

## R4. Filenames and redirects

- Name order: RFC 6266 `filename*` → `filename` → percent-decoded last URL path
  segment → `download`. (Gopeed, XDM, Ketch, AB DM all agree.)
- Sanitize: last path element only; strip separators, control and bidi
  characters; trim trailing dots/spaces; neutralise Windows device names; cap
  255 UTF-8 bytes preserving the extension (Ketch `FileNames`).
- Collisions: auto-rename `name (1).ext`; never overwrite by default (Gopeed,
  XDM, AB DM). aria2's `.1` form is not worth copying.
- Redirects: follow at most **20** (aria2), refuse HTTPS→HTTP and non-http(s);
  on cross-scheme/host/port hops drop `Cookie`/`Authorization` and reduce
  `Referer` to the origin (Ketch `RedirectCache`). Never replay a session cookie
  to a different host.
- **Cookies, auth headers and referrer are never persisted** — AB DM writes them
  in cleartext to `<id>.json`, XDM to `task-<id>.info`. Our `model.Credentials`
  is memory-only for exactly this reason.

## R5. Extension ↔ daemon protocol

- **Loopback HTTP + bearer token + Origin allowlist** (our spec; XDM's
  `BrowserIntegration.kt` is the closest precedent). Not native messaging:
  Gopeed and AB DM each need a host binary, two manifests, a `4-byte length`
  framing protocol and a wakeup URI — three moving parts per browser for no
  benefit when the daemon is already listening.
- Extension sends `url`, `referrer`, `Cookie` header, `User-Agent`, extra
  headers, suggested filename, and the tab URL.
- **Cancel the browser's download only after the daemon returns 2xx.** XDM
  cancels first and admits it in its own error copy ("browser download was
  already cancelled") — the single worst behaviour in the reference set.
- Capture filters live in the extension: minimum size, MIME allow/deny,
  extension allow/deny, and never capture requests the daemon itself makes.
- Settings come from `GET /api/settings`; no long-poll needed — `/api/ws`
  already exists.

## R6. Queue, concurrency, speed limits

- Single FIFO queue. **3 concurrent downloads** by default (AB DM 2, XDM 3,
  Ketch 4, aria2 5, Gopeed 5); configurable.
- **8 connections per download, 8 per host across all downloads.** Only Ketch
  has a per-host cap; XDM, Gopeed and AB DM are unbounded, which is how they
  saturate a router or trip bot protection.
- Speed limiting: **two independent token buckets** — global and per-download —
  both consulted before every read (AB DM's stacked wrappers). Rejected: XDM's
  per-download limiter reading the global number, which hands out N× the limit.
- Pause = cancel workers, fsync, persist, set `paused`. Resume = revalidate then
  continue. Both are Manager operations; workers only stop when their context
  is cancelled.

## R7. Persistence

- SQLite, **two tables** (`downloads`, `segments`) written in one transaction,
  WAL mode, numbered migrations recorded in a `schema_version` table (PLAN
  Phase 2). Segment rows are what make per-segment resume cheap.
- Rejected: one JSON file per download (AB DM — every add reads every file, every
  second rewrites each one) and one row with `segments_json` blobs (Ketch — fine
  for them, but PLAN asks for a segment schema and we want partial updates).
- Save cadence: ~1 s, on pause, on shutdown, before the first byte. Never store
  a known `Size` with an empty segment list (Ketch: resumes as a zero-filled
  file).
- Manager remains the only writer; `store` exposes no mutable cache.

## R8. API and events

- REST per PLAN §5 plus `GET /api/ws` (WebSocket), not SSE (Ketch) — one
  connection, bidirectional, works with the token in a header.
- Progress events are **coarse and throttled** (~4/s per download, only when the
  byte count moved). Full segment detail on `GET /api/downloads/{id}`. This
  avoids AB DM's whole-list rebuild every 500 ms and Gopeed's 350 ms full JSON
  rewrite.
- Errors are structured: `{code, message, detail, retryable}`. aria2 returns one
  generic RPC code with a prose message, forcing every UI to parse English.
- Token compared in constant time (aria2 `validateToken`), sent as a bearer
  header (PLAN), not as a first RPC parameter.

## R9. UI surface

- Columns: name, size, progress, speed, ETA, status. Segment detail only in an
  expanded row.
- Failures are a triple — short title, cause, action — keyed by error code, as
  AB DM does in `en_US.properties`; wording lives client-side, codes on the wire.
- Show `retrying in Ns` and `preparing file` as sub-states of `running`; they
  are not new `model.Status` values.

## R10. Not adopted, on purpose

| Reference idea | Why not |
|---|---|
| aria2 piece bitfield / `PieceStorage` | Serves BT and Metalink; HTTP range segments are simpler and resumable from rows |
| Native messaging (Gopeed, AB DM) | Host binary + manifests per browser when a loopback port already exists |
| SSE stream with client-owned subscriptions (Ketch) | Extra protocol; WebSocket is in the spec and bidirectional |
| Engine embedded in GUI process (Gopeed, Ketch) | Defeats the always-on daemon and the 20 MB idle target |
| Final-name preallocation (aria2, Gopeed) | Interrupted files look complete; `.part` + rename is safer |
| Per-download JSON store (AB DM) | O(n) reads per add, full rewrite per second |
| Unbounded per-host connections (XDM, Gopeed, AB DM) | Hammers origins, breaks many CDNs |
| Ignoring `Retry-After` (aria2, Gopeed, AB DM) | 429/503 without it either spins or abandons the download |

## Open questions for the author

1. **License** — still undecided. These notes are reimplementations, not ports;
   nothing GPL has been copied, so the choice is open.
2. **Completed downloads stay in the list as history, or auto-remove?** Affects
   the schema and the UI's empty state.
3. **Confirm defaults:** 3 concurrent downloads, 8 connections per download,
   8 per host, 1 MiB minimum segment. All are setting-overridable.
