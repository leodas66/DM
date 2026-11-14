# AGENTS.md

## What this repo is
A download manager daemon in Go, built phase by phase from `PLAN.md` (the
spec — source of truth). Phases 0, R (research) and 1 (probe + segmented
downloader + CLI) are done. Phase 2 (SQLite store, Manager, queue, resume) is
next. `internal/api` and `internal/web` are still doc-only placeholders.

Read `PROCESS.md` before starting work. It defines how much you decide alone,
when you must stop, and what counts as done. `docs/DECISIONS.md` records every
non-obvious call already made — check it before re-litigating a choice.

## How work runs (details in PROCESS.md §The phase loop)
- **One phase per session.** Implement only the requested phase; no work from
  the next phase, no opportunistic refactors.
- Tests first (`superpowers:test-driven-development`); systematic debugging
  after three failed fix attempts, not a fourth guess.
- `make check` is the only accepted evidence a phase is green: gofmt, `go vet
  ./...`, `go test -race ./...`, `go build ./...`. Paste its real output.
- Commit per green phase: `phase N: <what changed>`. Pre-commit runs the same
  gate — enable once per clone with `make hooks`. Never `--no-verify`.
- Stop only for: a new dependency, the license, destructive actions, and the
  two author review gates (model types, Manager state machine).
- Replies follow the template in PROCESS.md §Chat responses: outcome on line
  one, `Changed`/`Verified`/`Decisions`/`Next`, failures verbatim, no preamble
  and no restating the request.

## Dependencies
- Never add one without asking. Stdlib first.
- Pre-approved: `modernc.org/sqlite` (Phase 2 only), Svelte + Vite in `ui/`,
  esbuild in `extension/`.
- `chi` and `cobra` are *not* needed — stdlib mux, `flag`, `log/slog`.
- Module path is `github.com/vishwa/dm` (provisional, D001 in the decision
  log). Binary is `cmd/dm` → `dm`.

## Architecture invariants (the mistakes that are easy to make here)
- The Manager owns all download state and is the **only** writer to the store.
  Segment workers share nothing; they report progress over channels. No shared
  mutable structs behind mutexes across goroutines — this is the spec's central
  rule, not a style preference.
- API binds `127.0.0.1` only, requires `Authorization: Bearer <token>`
  (generated first run, stored 0600), rejects unknown `Origin`s.
- Only `http`/`https` URLs. Sanitize filenames (separators, `..`, control
  chars) and never write outside the chosen directory.
- Cookies/headers/referrer are per-download and memory-only — they live in
  `model.Credentials` and are `json:"-"` everywhere. Never put them in a struct
  that `store` writes or that the API returns.
- Resume re-probes and compares ETag/Last-Modified before reusing a partial
  file; on mismatch, restart from zero.
- `Segment.End` is inclusive (HTTP byte-range semantics). Off-by-one here
  corrupts files.

## Reference projects / licensing (PLAN §6A)
- Clone aria2, Gopeed, XDM, AB DM, Ketch **outside** this repo, read-only.
  Design notes go to `docs/research/<name>.md`; every claim needs a file path
  and function name.
- Never copy or translate code from GPL sources (aria2, Gopeed, XDM, old FDM)
  into this repo — that would relicense the project.
- Do not decompile FDM 5.0+; observe behaviour only.
- This project's license is undecided; ask before adding one or vendoring
  anything copyleft.

## Layout (PLAN §4)
`cmd/dm`, `internal/{model,probe,download,manager,store,api,web}`, `ui/`
(Svelte), `extension/` (TypeScript). The built UI is embedded via
`go:embed` in `internal/web` and served at `/`. `scripts/mockserver` is a dev
tool for manual checks (`-no-range`, `-flaky-every N`), see `TESTING.md`.

## Known local quirks
- Git has no configured `user.name`/`user.email`; commits so far used a
  placeholder identity (D009). Fix before the author takes ownership of
  history: `git config user.name … && git config user.email … && git commit
  --amend --reset-author --no-edit`.
- Go toolchain is `go1.27.3` (custom build); `go.mod` pins it.

## Code search
`codebase-memory-mcp` is installed and indexed for this repo. Use it for
structural code exploration instead of `grep`/`Glob`/`Read` when the question
is about *relationships* — who calls what, what a function affects, where a
route is wired, what changed.

| Use this tool | When |
|---|---|
| `search_graph` | Find functions/classes/routes by name pattern (case-insensitive) |
| `trace_call_path` | inbound/outbound call chains, risk-classified |
| `get_architecture` | languages, packages, entry points, routes, hotspots |
| `detect_changes` | git diff → affected symbols + blast radius |
| `query_graph` | Cypher-like graph queries |
| `search_code` | Text search inside indexed files (grep-like) |

Prefer graph tools over file-by-file exploration: a single `search_graph` call
returns what would take dozens of `grep`/Read calls, at ~500 tokens vs ~80K.

The MCP server is registered in `kilo.json`, `.mcp.json`, and
`~/.config/opencode/opencode.json` — all agents share one SQLite graph at
`~/.cache/codebase-memory-mcp/codebase-memory.db`. It auto-syncs on file
changes; reindex manually with:

```
codebase-memory-mcp cli index_repository '{"repo_path": "/home/vishwa/Projects/project1"}'
```

## GTK4 UI skills
- Project-local skills are in `.agents/skills/`; start with
  `dm-ui-router` for GTK4 desktop tasks.
- Load only the smallest relevant set (normally one, never more than three).
- `docs/GTK4-DESIGN-SPEC.md` is authoritative over skill recommendations.
- These skills apply only to the GTK4 desktop client. They do not change the
  current daemon/CLI phase or authorize GTK, Qt, Libadwaita, or other new
  dependencies. See `docs/UI-SKILLS.md` for provenance and update policy.
