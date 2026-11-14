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
| Fresh clone, no `npm install` | `make check` green; embedded UI placeholders satisfy both `go:embed` directives | pass — build + vet + tests green in a clone with only `internal/web/dist/.gitkeep` and `internal/gui/wails/frontend/dist/.gitkeep`; `TestHandlerServesAssets` skips with "run `npm run build` in ui/" | 2026-11-14 |
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
| `kill -9` mid-download, restart | resumes, checksum correct | pass (automated: `TestKillNineAndResume` — SIGKILLs a child process mid-download, reopens the DB, finishes, compares SHA-256) | 2026-11-14 |
| Pause/resume 20 times, then finish | checksum correct | not run — one pause/resume cycle is covered by `TestPauseResumesFromPartialFile`; the 20-cycle soak is manual | — |
| File changed on server between pause and resume | detects ETag/Last-Modified change, restarts from zero | pass (automated: `TestChangedValidatorRestartsFromZero`) | 2026-11-14 |
| Max-concurrency limit | never exceeds configured running downloads | pass (automated: `TestMaxConcurrentIsEnforced` — samples the Manager's running count and also checks the server saw two GETs at once) | 2026-11-14 |
| Speed limit (global and per download) | measured rate within ~10% of cap | partial — `TestGlobalSpeedLimitSlowsTransfer` and `TestBucketDelaysLargeRequests` prove the cap is applied; the ~10% accuracy figure is not measured | 2026-11-14 |
| Disk full mid-write | fails cleanly, error surfaced, no corrupt final file | not run under the Manager; the Phase 1 CLI hit the same path (`disk quota exceeded`, `.part` kept, existing file untouched) | — |
| Destination directory removed mid-download | fails cleanly with a useful message | not run | — |

## Phase 3: API

| Check | Expected | Result | Date |
|---|---|---|---|
| Request without token | 401 | pass (automated: `TestRESTRequiresAuth`) | 2026-11-14 |
| Request with wrong token | 401 | pass (same test) | 2026-11-14 |
| Request with foreign `Origin` | rejected | pass (automated: `TestOriginRejected`, `TestWSRejectsBadOrigin`) | 2026-11-14 |
| `curl` add/list/pause/resume/cancel/delete | all behave per PLAN §5 | pass over httptest (automated: `TestRESTAddListGet`, `TestRESTPauseResumeCancel`, `TestRESTDelete`, `TestRESTSettings`); a hand-run curl pass was not repeated | 2026-11-14 |
| WebSocket client receives added/progress/status/removed | in order, valid JSON | pass (automated: `TestWSReceivesEvents`; auth by `TestWSRequiresAuth`) | 2026-11-14 |
| SIGTERM during download | state persisted, process exits cleanly | not run — needs a real daemon process; graceful shutdown is only covered indirectly by `Manager.Shutdown` | — |

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
| Bundle size | under ~100 KB gzipped | pass — 12.3 KB gzipped total (10.4 KB JS, 1.6 KB CSS, 0.3 KB HTML) | 2026-11-14 |
| Live progress with tab open | updates without reload | | |
| Tab closed | daemon RSS still under 20 MB idle | | |
| Dark/light | both readable, no hardcoded colors | | |

## Phase 6: packaging

| Check | Expected | Result | Date |
|---|---|---|---|
| `makepkg -si` | installs, systemd user unit starts | not run — requires an Arch host (`makepkg`/`pacman` unavailable here) | — |
| Login on a fresh user | daemon running, token created 0600 | not run — same | — |
| Uninstall | no orphan config or data | not run — same | — |

## GTK4 desktop client (first slice)

Run against `scripts/mockserver` on `127.0.0.1:8099` with a real `dm --serve`
daemon; the client used the published `$XDG_RUNTIME_DIR/dm/endpoint` and the
existing 0600 token. Screenshots were taken of each state.

| Check | Expected | Result | Date |
|---|---|---|---|
| `make check-desktop` | gofmt, vet, race tests, build green in `desktop/` | pass | 2026-11-14 |
| Launch with daemon running | window opens, all downloads listed | pass — 7 seeded downloads with type icons, state icons, and counts | 2026-11-14 |
| Endpoint discovery | client finds the ephemeral port without configuration | pass — daemon printed the endpoint; client connected on first try | 2026-11-14 |
| Live WebSocket updates | progress, summary, and counts update without reload | pass — a running row advanced 23% → 57% and the header summary changed with it | 2026-11-14 |
| Filters and counts | each of All/Active/Queued/Paused/Completed/Failed shows only matching rows | pass — verified per filter; All includes the canceled row, so it exceeds the sum of the other counts | 2026-11-14 |
| Add dialog | URL, folder, optional name; submit adds a row | pass — submitted from the dialog, new row appeared in the list | 2026-11-14 |
| Delete confirmation | modal names the file and destination; Cancel is default | pass — rendered for a failed row; Escape closes it | 2026-11-14 |
| Offline state | clear message and a reconnect action | pass — "Cannot reach the DM daemon" with Retry when no endpoint exists | 2026-11-14 |
| Empty collection | explanation plus one Add Download action | pass — "No downloads yet" with Add Download against a fresh daemon | 2026-11-14 |
| Narrow window | sidebar collapses below 820 px, restores when widened | pass — 720 px hid it, 1024 px restored it, manual toggle overrides | 2026-11-14 |
| Failed row | human-readable reason, raw error available | partial — short reason and error icon verified; the tooltip was not opened | 2026-11-14 |
| Keyboard | Ctrl+N opens Add Download, Escape dismisses dialogs | pass | 2026-11-14 |
| Light and dark appearance | readable in both without app colors | partial — verified under Breeze dark only | 2026-11-14 |
| Confirm delete actually removes | file gone, row gone, daemon state updated | not run — the confirmation was opened but not confirmed | — |

## Deferred checks

Record any check skipped, with the reason and the phase it was moved to.

| Phase | Check | Reason |
|---|---|---|
| 4 | Cookie-protected download, Chrome | No Chrome available in this environment; verify manually with a loaded unpacked extension |
| 4 | Cookie-protected download, Firefox | No Firefox available in this environment; verify manually with a temporary add-on |
| 4 | Daemon stopped → browser proceeds | Same; manual browser check |
| 4 | Below minimum-size filter | Same; manual browser check |
| 5 | Live progress with tab open | Requires a running browser; verify manually |
| 5 | Tab closed → daemon RSS under 20 MB idle | Requires a running browser; verify manually |
| 5 | Dark/light both readable | Requires a running browser; verify manually |
| 2 | Pause/resume 20 cycles | Long soak; automated coverage is one cycle (`TestPauseResumesFromPartialFile`) |
| 2 | Disk full under the Manager | Damaging to the host; the Phase 1 CLI exercised the same error path |
| 2 | Destination directory removed mid-download | Manual filesystem check; not automated |
| 3 | SIGTERM during download | Needs a real daemon process; `Manager.Shutdown` is covered indirectly |
| 6 | `makepkg -si`, fresh-login, uninstall | Requires an Arch host; `makepkg`/`pacman` are unavailable in this environment |
| 6 | Desktop app (Wails) launch and shutdown | Not in PLAN; needs a desktop session plus the Wails toolchain, so it is not part of `make check` |
| GTK4 | Orca and GTK Inspector accessibility pass | Needs a full screen-reader session; labels and roles are implemented but not audited with Orca |
| GTK4 | High-contrast and large-text appearance | Needs a separate GTK theme/settings session; no clipping was observed under the default font |
| GTK4 | Light appearance | Only Breeze dark was available during the live check |
| GTK4 | Confirm delete end-to-end | Destructive check on seeded files; the confirmation flow was rendered but not confirmed |
