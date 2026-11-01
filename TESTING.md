# TESTING.md

Manual checks that automated tests can't cover honestly. Automated coverage
lives in `_test.go` files; this file is for real servers, real interrupts, and
real filesystems.

Rule from `PROCESS.md`: a phase that changes runtime behaviour is not done
until its rows here are filled in or explicitly deferred with a reason.

## How to run the mock server

`scripts/mockserver` serves a directory with Range support and two failure modes:

```sh
go run ./scripts/mockserver -dir /tmp/fixtures -addr 127.0.0.1:8099
go run ./scripts/mockserver -dir /tmp/fixtures -no-range          # single-stream path
go run ./scripts/mockserver -dir /tmp/fixtures -flaky-every 3     # sever every 3rd GET after 64 KiB
```

Then `./bin/dm -d /tmp/out -n 8 http://127.0.0.1:8099/big.bin`.

## Phase 0: scaffold

| Check | Expected | Result | Date |
|---|---|---|---|
| `make check` | gofmt clean, vet clean, race tests pass, build ok | pass | 2026-11-01 |
| `./bin/dm -version` | prints `dm 0.1.0-dev` | pass | 2026-11-01 |
## Phase 1: probe + segmented downloader

| Check | Expected | Result | Date |
|---|---|---|---|
| 1 GB file, ranges supported | SHA-256 matches server copy | pass — 8 segments, `e3b73148…de0acdf` both sides | 2026-11-01 |
| Server without `Accept-Ranges` | falls back to single stream, file correct | pass — 1 GiB, same checksum | 2026-11-01 |
| Unknown `Content-Length` (chunked) | single stream, correct bytes | pass (automated: `TestRunUnknownLength`) | 2026-11-01 |
| Server closes connection mid-body | retries, final file correct | pass — `mockserver -flaky-every 3`, 1 GiB, same checksum | 2026-11-01 |
| Redirect chain (3 hops) | `FinalURL` recorded, file correct | pass (automated: `TestProbeFollowsRedirectsAndRecordsFinalURL`) | 2026-11-01 |
| `Content-Disposition: attachment; filename*=UTF-8''…` | decoded, sanitized filename | pass (automated: `TestProbeReadsMetadataFromRangeResponse`) | 2026-11-01 |
| Filename collision in target dir | no silent overwrite | deferred to Phase 2 (auto-rename lands with the Manager's add flow) | — |
| Path-traversal filename (`../../etc/passwd`) | rejected or sanitized, nothing written outside dir | pass (automated `TestSanitizeFilename`; CLI also refuses a joined path outside `-d`) | 2026-11-01 |
| Disk full mid-write | fails cleanly, error surfaced, no corrupt final file | pass — hit accidentally on tmpfs: `write … disk quota exceeded`, old file untouched, `.part` kept | 2026-11-01 |

## Phase 2: persistence, resume, queue

| Check | Expected | Result | Date |
|---|---|---|---|
| `kill -9` mid-download, restart | resumes, checksum correct | | |
| Pause/resume 20 times, then finish | checksum correct | | |
| File changed on server between pause and resume | detects ETag/Last-Modified change, restarts from zero | | |
| Max-concurrency limit | never exceeds configured running downloads | | |
| Speed limit (global and per download) | measured rate within ~10% of cap | | |
| Disk full mid-write | fails cleanly, error surfaced, no corrupt final file | | |
| Destination directory removed mid-download | fails cleanly with a useful message | | |

## Phase 3: API

| Check | Expected | Result | Date |
|---|---|---|---|
| Request without token | 401 | | |
| Request with wrong token | 401 | | |
| Request with foreign `Origin` | rejected | | |
| `curl` add/list/pause/resume/cancel/delete | all behave per PLAN §5 | | |
| WebSocket client receives added/progress/status/removed | in order, valid JSON | | |
| SIGTERM during download | state persisted, process exits cleanly | | |

## Phase 4: extension

| Check | Expected | Result | Date |
|---|---|---|---|
| Cookie-protected download, Chrome | daemon gets file, browser download canceled | | |
| Cookie-protected download, Firefox | same | | |
| Daemon stopped | browser download proceeds normally | | |
| Below minimum-size filter | browser handles it, daemon untouched | | |

## Phase 5: UI

| Check | Expected | Result | Date |
|---|---|---|---|
| Bundle size | under ~100 KB gzipped | | |
| Live progress with tab open | updates without reload | | |
| Tab closed | daemon RSS still under 20 MB idle | | |
| Dark/light | both readable, no hardcoded colors | | |

## Phase 6: packaging

| Check | Expected | Result | Date |
|---|---|---|---|
| `makepkg -si` | installs, systemd user unit starts | | |
| Login on a fresh user | daemon running, token created 0600 | | |
| Uninstall | no orphan config or data | | |

## Deferred checks

Record any check skipped, with the reason and the phase it was moved to.

| Phase | Check | Reason |
|---|---|---|
| 4 | Cookie-protected download, Chrome | No Chrome available in this environment; verify manually with a loaded unpacked extension |
| 4 | Cookie-protected download, Firefox | No Firefox available in this environment; verify manually with a temporary add-on |
| 4 | Daemon stopped → browser proceeds | Same; manual browser check |
| 4 | Below minimum-size filter | Same; manual browser check |
