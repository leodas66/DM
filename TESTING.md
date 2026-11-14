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
| Fresh clone, no `npm install` | `make check` green with no asset build step | pass — the daemon no longer embeds a web bundle, so the check runs without Node (D035) | 2026-11-15 |

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

## Phase 4: extension and native host

| Check | Expected | Result | Date |
|---|---|---|---|
| Native Messaging framing | round trip, EOF, empty and oversize frames rejected | pass (automated: `TestMessageRoundTrip`, `TestReadMessageEOF`, `TestReadMessageRejectsEmpty`, `TestReadMessageRejectsOversize`, `TestReadMessageRejectsShortBody`) | 2026-11-15 |
| Host forwards a download | `add` posts to `/api/downloads` with the bearer token, referrer, headers and cookies | pass (automated: `TestAddForwardsCredentialsAndPayload`) | 2026-11-15 |
| Host input validation | `file://`, `ftp://`, `javascript:`, empty and hostless URLs never reach the daemon | pass (automated: `TestAddRejectsNonHTTPURLs`) | 2026-11-15 |
| Host sanitizes headers | CR/LF header names or values and empty names are dropped | pass (automated: `TestAddStripsDangerousHeaders`) | 2026-11-15 |
| Host error reporting | daemon 4xx/5xx, unreadable body, missing endpoint and missing token produce a message the popup can show | pass (automated: `TestAddSurfacesDaemonError`, `TestAddFallsBackToStatusWhenBodyIsNotJSON`, `TestAddReportsDaemonUnavailable`, `TestAddReportsMissingToken`) | 2026-11-15 |
| Framed end-to-end host run | `ping`, `add`, a rejected `file://` URL and an unknown command over real stdin/stdout | pass — frames only on stdout, exit 0, downloaded file matched the fixture SHA-256 | 2026-11-15 |
| Host manifests | `-manifest chrome -extension-id …` and `-manifest firefox` produce the expected allowlists; unknown browsers and a missing ID fail | pass — Chrome origin `chrome-extension://<id>/`, Firefox `dm@vishwa.github.io` | 2026-11-15 |
| Capture rules | size, allow and deny filters, referrer and filename payload | pass (automated: 17 tests in `extension/src/types.test.ts`) | 2026-11-15 |
| Extension build | one bundle produces loadable `dist/chrome` and `dist/firefox` | pass — both manifests and assets present; `npm run lint` clean | 2026-11-15 |
| Cookie-protected download, Chrome | daemon gets file, browser download canceled | not run — no Chrome in this environment; see deferred | — |
| Cookie-protected download, Firefox | same | not run — no Firefox in this environment; see deferred | — |
| Daemon stopped | browser download proceeds normally | not run — same | — |
| Below minimum-size filter | browser handles it, daemon untouched | not run — same | — |

## Phase 5: UI

The original Svelte web UI was removed under D035. Results below are kept as a
historical record; the GTK4 client is covered in its own section.

| Check | Expected | Result | Date |
|---|---|---|---|
| Bundle size | under ~100 KB gzipped | pass — 12.3 KB gzipped total (10.4 KB JS, 1.6 KB CSS, 0.3 KB HTML) | 2026-11-14 |
| Live progress with tab open | updates without reload | superseded by the GTK4 client | — |
| Tab closed | daemon RSS still under 20 MB idle | superseded by the GTK4 client | — |
| Dark/light | both readable, no hardcoded colors | superseded by the GTK4 client | — |

## Phase 6: packaging

| Check | Expected | Result | Date |
|---|---|---|---|
| `makepkg` build | produces `dm-0.2.0-1-x86_64.pkg.tar.zst` with all three binaries, the unit, entry, icon, Firefox manifest and license | pass — package built from the published `v0.2.0` tag; `bsdtar -tf` lists every expected path | 2026-11-15 |
| Client build duration | first GTK4 build completes instead of hanging | pass — 510 s for `dm-desktop` on 8 cores with a cold cache; 12 s for the whole package once warm, so a silent pause is compilation | 2026-11-15 |
| `makepkg -si` install | installs, systemd user unit starts | not run — needs root/pacman transaction in this environment; the build half is verified above | — |
| Login on a fresh user | daemon running, token created 0600 | not run — needs a fresh user session | — |
| Uninstall | no orphan config or data | not run — same | — |

## GTK4 desktop client

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
| Preferences dialog | opens from the app menu, shows stored values, rejects an empty folder inline, saving closes it and updates the daemon | pass — menu action opened it; save applied dir/concurrency/connections/limits and preserved `minSegmentSize`/`saveInterval` | 2026-11-14 |
| Settings persistence | settings survive a daemon restart | pass — `PUT` then `SIGTERM`/restart returned the same record | 2026-11-14 |
| Category management | create with a default folder, list it, delete it; sidebar refreshes | pass — created and deleted through the dialog against the live daemon, sidebar rows 2 → 3 → 2 | 2026-11-14 |
| History after restart | completed/failed/canceled downloads still listed | pass — 9 downloads before and after a daemon restart | 2026-11-14 |
| Clipboard prefill | Add dialog fills an empty URL from a copied link, shows where it came from, and never overwrites typing | pass — prefilled `https://example.com/from-clipboard.iso` with the hint visible and Add enabled; typing another value kept it and hid the hint | 2026-11-15 |
| Retry after restart | a failed download survives a daemon restart and completes when retried | pass (automated: `TestRetryFailedDownloadAfterRestart`) | 2026-11-15 |
| Retry control | failed and canceled rows show a refresh control in the state slot | pass — live failed row showed the refresh button with tooltip "Retry broken.bin"; the retry produced `queued → queued → failed` status events for a still-missing URL | 2026-11-15 |
| Remove history after restart | removing a finished download deletes the store record and its file | pass (automated: `TestRemoveFinishedDownloadAfterRestart`) | 2026-11-15 |
| Zero start time | an unscheduled download round-trips as zero instead of a wrapped timestamp | pass (automated: `TestStartAtRoundTrip`) | 2026-11-15 |

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
| 6 | Desktop app launch and shutdown | The Wails wrapper is gone (D035); the GTK4 client is covered by `make check-desktop` and the GTK4 section above |
| GTK4 | Orca and GTK Inspector accessibility pass | Needs a full screen-reader session; labels and roles are implemented but not audited with Orca |
| GTK4 | High-contrast and large-text appearance | Needs a separate GTK theme/settings session; no clipping was observed under the default font |
| GTK4 | Light appearance | Only Breeze dark was available during the live check |
| GTK4 | Confirm delete end-to-end | Destructive check on seeded files; the confirmation flow was rendered but not confirmed |
| 6 | `makepkg -si` installs daemon, client, unit, desktop entry, icon | `makepkg`/`pacman` are unavailable here; `bash -n PKGBUILD`, `desktop-file-validate`, and the source tag were checked instead |
| 6 | `dm --serve` opens no browser and prints no token | needs a fresh terminal run after installing the package |
| 4 | Extension loaded in Chrome | No Chrome available; the build output and host protocol are tested, the browser UI is not |
| 4 | Extension loaded in Firefox | No Firefox available; same |
| 4 | Chrome per-user host manifest in place | The extension ID only exists after loading the extension, so this is a manual install step |
