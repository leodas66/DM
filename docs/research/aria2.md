# aria2: engine, HTTP and RPC design notes

aria2 is a single-process C++ engine that turns a URI/option pair into a `RequestGroup`, then into `Command` objects driven by one `DownloadEngine` loop. A download is not a byte stream but a fixed-piece bitfield shared by all connections; resume, RPC progress and file naming derive from it. That model buys cheap progress reporting and restartability without cross-worker mutable state.

## 1. Segmentation

`src/download_helper.cc:createRequestGroup` sets the `DownloadContext` piece length from `--piece-length` (1M) and `createRequestGroupForUri` sets workers via `setNumConcurrentCommand(split)`, capped at `min(numPieces, split)` (`src/RequestGroup.cc:createNextCommandWithAdj`). Defaults: `--split` 5, `--max-connection-per-server` 1 (max 16), `--min-split-size` 20M.

Connections get no precomputed range. Each asks `SegmentMan::getSegment` (`src/SegmentMan.cc`) → `DefaultPieceStorage::getMissingPiece` → `DefaultStreamPieceSelector::select` → `BitfieldMan::getSparseMissingUnusedIndex` (`src/BitfieldMan.cc`). That selector takes the largest free run and, when its start abuts an in-flight piece, splits at `Range::getMidIndex` — a *new* connection steals the second half of a busy region, never the reverse. Nothing rebalances running connections and there is no slowest-segment heuristic: a finishing worker simply asks for another piece. Two softer corrections: `SegmentMan::getCleanSegmentIfOwnerIsIdle` reclaims a zero-progress segment from an idle peer; `FileEntry::findFasterRequest` re-points a worker at a faster mirror within the per-host cap.

## 2. Resume

Progress lives in a control file, final name + `.aria2` (`src/DefaultBtProgressInfoFile.cc`): a version-1 binary record (network byte order) of infoHash (BT), pieceLength, totalLength, uploadLength, the piece bitfield and in-flight piece bitfields. Written to `name__temp` then renamed, skipped when a SHA-1 is unchanged, validated by length: totalLength must equal the re-probed length, bitfield length must equal `ceil(total/piece)/8`, indexes must be in range, infoHash must match for BT. **No ETag or Last-Modified check exists for resume**; those headers appear only under `--conditional-get`.

Without a control file, `RequestGroup::loadAndOpenFile` opens the file under `--continue` and calls `markPiecesDone(size)`; an unsatisfiable range (`src/HttpResponse.cc:validateResponse`) raises `CANNOT_RESUME`, and `src/AbstractCommand.cc:onAbort` restarts from zero only with `--always-resume=false` and `--max-resume-failure-tries` reached (default true: abort). No `.part` files — data lands in the final name, so preallocation is the default (`--file-allocation=prealloc`, also `none`/`trunc`/`falloc`), skipped below `--no-file-allocation-limit` (5M), flushed every `--auto-save-interval` (60s) and on stop.

## 3. Retries

`src/AbstractCommand.cc` catches `DlRetryEx`, bumps a per-URI try counter on the `Request`, aborts at `--max-tries` (5; 0 = unlimited), then drops that URI and tries the next. `--retry-wait` defaults to 0, and the only delay is `src/HttpSkipResponseCommand.cc:processResponse`: 502/503 retry *only* if `--retry-wait > 0`, waking after exactly that many seconds; 504 always retries; 404 retries only if `--max-file-not-found > 0`. Other statuses ≥400 are fatal `HTTP_PROTOCOL_ERROR`, including **429**, which is not special-cased. 401 retries once if `--http-auth-challenge` supplies credentials. No exponential backoff, no Retry-After parser in this clone. Timeouts, connect and DNS errors retry; disk, checksum and auth failures do not.

## 4. Filenames and redirects

`src/HttpResponse.cc:determineFilename` prefers Content-Disposition, else the percent-decoded last path segment, else a default name. `util::getContentDispositionFilename` decodes quoted/extended forms, converts ISO-8859-1 unless `--content-disposition-default-utf8`, and rejects names containing `/`, `\` or traversal. Collisions go to `RequestGroup::tryAutoFileRenaming`: `name.1.ext` … `name.9999.ext`, reusing an existing file when its control file also exists; `--auto-file-renaming` (true) gates it, else error 13. Redirects: `isRedirect` covers 300/301/302/303/307/308 with Location; `Request::redirectUri` resolves absolute, network-path and relative references; `Request::MAX_REDIRECT` = 20, beyond which error 23 is fatal.

## 5. Browser integration

No browser hook exists — no extension, native-messaging host or interceptor here. Browsers drive aria2 over RPC: JSON-RPC 2.0 at `/jsonrpc`, XML-RPC at `/rpc`, JSON-RPC over WebSocket for notifications (`src/WebSocketSessionMan.cc`; port 6800, `--rpc-listen-all` false). Auth is a first parameter `token:<secret>`, stripped and checked against an HMAC of `--rpc-secret` (`src/RpcMethod.cc:authorize`, `src/DownloadEngine.cc:validateToken`). An extension sends `addUri` with the URL plus `referer`, `header` (Cookie), `user-agent`, `out`, `dir`. No double download arises because aria2 never competes for the browser's request; the extension cancels the browser transfer and hands over the resolved URL (client-side, not verified here).

## 6. Queue, scheduler, speed limiter

`RequestGroupMan` keeps `reservedGroups_` as a FIFO deque; `--max-concurrent-downloads` (5) bounds active groups and `fillRequestGroupFromReserver` (`src/RequestGroupMan.cc`) promotes from the front, skipping paused and unresolved-dependency groups; RPC `position` uses `insertReservedGroup`. Speed limiting is not a token bucket: `src/SpeedCalc.cc` is a sliding window of (timestamp, bytes) slots, and `DownloadCommand::executeInternal` detaches socket read checks while `doesOverallDownloadSpeedExceed()` or `doesDownloadSpeedExceed()` is true, re-checking next tick. `--max-overall-download-limit` and `--max-download-limit` both default 0.

## 7. UI surface

No GUI ships; AriaNg and Persepolis are pure RPC clients. `tellStatus` (`src/RpcMethodImpl.cc:gatherProgressCommon`, `gatherStoppedDownload`) exposes gid, status, totalLength, completedLength, downloadSpeed, connections, hex bitfield, pieceLength, numPieces, dir, files[] (index, path, length, completedLength, selected, uris[]), followedBy/following/belongsTo, and errorCode/errorMessage once stopped. Statuses are active, waiting, paused, error, complete, removed; errorCode is the decimal `error_code` enum from `src/error_code.h` (0 finished … 32 checksum). JSON-RPC failures are always `{"code":1,"message":"…"}` (`src/RpcMethod.cc:createErrorResponse`), so clients match message text, "Unauthorized" included.

## Ideas worth adopting

- Piece bitfield as the single source of progress truth: one blob per task, hex in the API, resume validated by bits.
- Sparse-midpoint claiming: workers pull from a shared manifest, never talk to each other — our "no shared mutable structs" rule.
- Control file as an atomically renamed journal with content-hash skip, mapping onto SQLite checkpoints.
- Per-URI try counters plus an explicit retryable/fatal table.
- Token as first RPC parameter, constant-time compare, secret filtered from option dumps.
- Separate `--split` (workers) from a per-host cap enforced by counting in-flight hosts.

## Mistakes to avoid

- No Retry-After, no backoff, 429 fatal: hammers or abandons real CDNs.
- Resume trusts length only; a changed remote file corrupts quietly. Store ETag/Last-Modified and restart on mismatch.
- Prealloc by default plus 1M pieces: needless I/O and large bitfields for a 20MB daemon.
- One generic RPC error code forces UIs to parse English strings.
- BT, Metalink and DHT welded into one command model, with results only in a 1000-entry in-memory list.

Sources: aria2 @ 9e72735
