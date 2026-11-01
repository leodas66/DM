# Decision log

Every non-obvious call made under the autonomy policy in `PROCESS.md`.
Newest last. Mark `provisional` when the author hasn't confirmed it.

| # | Date | Decision | Alternatives considered | Why | Status |
|---|---|---|---|---|---|
| D001 | 2026-11-01 | Module path `github.com/vishwa/dm`; binary `dm` | Product-name TBD | Name was blocking Phase 0; rename is one `go mod edit -module` plus import rewrite | provisional |
| D002 | 2026-11-01 | `go 1.27.3` directive matching the installed toolchain | Lower pin for wider compatibility | Only toolchain available here; raise later if packaging needs it | provisional |
| D003 | 2026-11-01 | `Download` has no `Cookies` field; secrets live in `model.Credentials` | Cookies on `Download` as PLAN §4 lists | PLAN §6 says cookies live only for the download's lifetime, so they must never reach SQLite | — |
| D004 | 2026-11-01 | `Download.Headers` and `.Referrer` are `json:"-"` | Expose to UI | They carry bearer tokens and session-bearing URLs; the UI has no use for them | — |
| D005 | 2026-11-01 | `Event` is one flat struct, `Download` nil on `removed` | Interface with concrete event types | It is the WebSocket wire format; clients unmarshal without a type registry | — |
| D006 | 2026-11-01 | `SegmentStatus` is separate from `Status` | Reuse `Status` | A segment is never `paused`, `completed` or `probing` | — |
| D007 | 2026-11-01 | `Segment.End` is inclusive (HTTP byte-range semantics) | Exclusive end | Matches `Range: bytes=a-b`; avoids off-by-one file corruption | — |
| D008 | 2026-11-01 | Autonomy model A + C: decide reversible, stop for irreversible, plus automated gates | Full autonomy (B) | Keeps speed without handing over license/dependency/destructive calls | — |
| D009 | 2026-11-01 | Phase 0 committed with a placeholder git identity (`vishwa <vishwa@localhost>`) | Blocking until identity configured | No `user.name`/`user.email` set; fix with `git commit --amend --reset-author` | provisional |
| D010 | 2026-11-01 | Phase R complete; R1–R10 in `docs/research/decisions.md` are binding design inputs for Phases 1–5 | Implementing from first principles without notes | Five notes with `path:function` citations; citations spot-checked against the clones | — |
| D011 | 2026-11-01 | Defaults: 3 concurrent downloads, 8 connections/download, 8 connections/host, 1 MiB minimum segment, 5 tries/segment | Reference defaults (2–5 concurrent, 3–10 tries) | Mid-range of the references; all settings-overridable | provisional |
| D012 | 2026-11-01 | Write to `<name>.part` and rename on completion, rather than preallocating the final name | Preallocate final name (PLAN §7 wording, aria2/Gopeed) | Avoids "looks complete" files and lets resume validate size; PLAN's intent (preallocate + WriteAt) is preserved | — |
| D013 | 2026-11-01 | License: MIT | Apache-2.0, none, GPL | Personal tool; every dependency is permissive (BSD/MIT); GPL references were read, never copied | — |
| D014 | 2026-11-01 | Completed downloads stay in history; no auto-prune in v1 | Auto-delete on completion, retention window | Rows are tiny; history keeps retry/open-folder useful; user can clear manually | — |
| D015 | 2026-11-01 | Phase 1 ships fixed segments; Manager-mediated work stealing lands in Phase 2 | Implementing stealing now inside a one-shot `Run` | Nothing owns the segment table between workers until the Manager exists; `Run` is throwaway plumbing | — |
| D016 | 2026-11-01 | Phase 4: loopback HTTP (not native messaging) for extension ↔ daemon; MV3 extension in `extension/` built with esbuild; `sendToDaemon` prefixes `/api`; capture filters (min size, allow/deny hosts) live in the extension; daemon's own URL never captured; `<all_urls>` host permission for the configurable localhost port | Native messaging (host binary + per-browser manifests) | Reuses the existing loopback API server; no host binary or wakeup URI needed | provisional |
| D017 | 2026-11-01 | Phase 5: Svelte + Vite UI in `ui/`, built with esbuild via vite, output to `internal/web/dist/` and embedded via `go:embed`; `web.Handler(api)` wraps the API mux with SPA static serving; token passed via URL fragment/query and stored in localStorage; no UI component library; ~9.5 KB gzipped bundle | Server-side rendered HTML or a separate frontend server | Keeps the daemon a single binary; zero runtime frontend build step; simple SPA routing via index.html fallback | provisional |
