# PROCESS.md

How work happens in this repo. Agents follow it; the author can override it at
any time. `PLAN.md` says *what* to build, this file says *how the work runs*.

## Autonomy policy

**Decide everything reversible. Stop for everything else.**

| Decision | Who decides | Recorded in |
|---|---|---|
| Module path, package layout, file names | Agent | `docs/DECISIONS.md` |
| Type shape, field names, JSON tags | Agent | `docs/DECISIONS.md` |
| Test strategy, fixtures, mocks | Agent | commit message |
| Commit boundaries and messages | Agent | `git log` |
| Which phase starts next / skipping Phase R | Agent | `docs/DECISIONS.md` |
| Adding a dependency | **Author** | `docs/DECISIONS.md` |
| Project license | **Author** | — |
| Deleting files, rewriting history, force-push | **Author** | — |
| `internal/model` types (PLAN §8.2) | **Author reviews** | — |
| Manager state machine (PLAN §8.2) | **Author reviews** | — |

If the author is unreachable and a blocked decision has an obvious default,
take the default, mark it `provisional` in the decision log, and say so in the
next report. Never silently take a permanent decision.

## Stop conditions

Stop and ask only when the answer changes something irreversible or
product-level:

1. A dependency outside the pre-approved list (`modernc.org/sqlite` at Phase 2,
   Svelte/Vite in `ui/`, esbuild in `extension/`).
2. The project license, or vendoring anything copyleft.
3. Destroying data: deleting user files, rewriting history, dropping the DB
   schema without a migration.
4. The two review gates PLAN §8 assigns to the author (model types, Manager
   state machine).

Everything else: decide, log, continue.

When asking, use this shape — one message, no open-ended "what do you think":

> **Blocked on:** <decision>
> **Why it blocks:** <what can't be written until it's answered>
> **Default if you say nothing:** <the call I'll make, and its cost if wrong>

## The phase loop

One phase per plan section, run in this order:

1. **Read** the phase's section of `PLAN.md` plus the relevant
   `docs/research/*.md`. Do not re-derive design the notes already settle.
2. **Write tests first** for the behaviour the phase adds
   (`superpowers:test-driven-development`). Types-only phases get wire-format
   tests instead.
3. **Implement** to green. Stay inside the phase: no work from the next phase,
   no opportunistic refactors.
4. **Verify** with `make check` (gofmt + `go vet` + `go test -race` + build).
   The command's real output is the only acceptable evidence of success.
5. **Record** manual checks in `TESTING.md` when the phase changes runtime
   behaviour.
6. **Log** every non-obvious call in `docs/DECISIONS.md`.
7. **Commit** one commit per green phase: `phase N: <what changed>`.
8. **Report** to the author: what works, the evidence, the decisions taken,
   what's next. Keep it short; paste failures verbatim when they happen.

A phase is done when its PLAN "Done when" line is true *and* `make check` is
green *and* the commit exists.

## Evidence rules

- Never claim "tests pass" without pasting the command and its output.
- A red check is reported as red, with the failing output, before any fix is
  attempted.
- A test that passes for the wrong reason (skipped, no assertions, mocked
  everything) does not count; say so if that's the state.
- Timing-sensitive or network tests that flake get quarantined with a skipped
  subtest and a `TESTING.md` entry, not silently deleted.

## Chat responses

The report is the product of the work, not a transcript of it. Default shape:

```
phase 2: resume survives kill -9, checksum matches

Changed: internal/store/{store.go,ddl.go}, internal/manager/manager.go
Verified: make check -> green (gofmt, vet, race tests, build)
Decisions: D012 snapshot before pause; D013 fsync every 1s
Next: Phase 3 API
```

Rules:

- First line is the outcome, the answer, or the blocker. No preamble, no "let
  me", no restating the request.
- `Verified` is a command plus its result. Paste failures verbatim; for passes
  quote the meaningful line, not one line per package with no tests.
- List only decisions that are new or surprising. Never re-summarise the plan.
- One caveat, one line, starting with what was *not* checked.
- Length: routine report ≤ 12 lines, blocker ≤ 6, a direct question answered in
  prose with no headings.
- No emoji, no bold-for-emphasis, no headings in replies under ~15 lines, no
  closing "let me know if…" unless there is a real choice to make.
- Never compress away: failing output, unverified claims, irreversible
  choices, or the next step.
- Commands and output in code blocks, reasoning in prose. Tables only for three
  or more comparable options.

## Debugging protocol

Use `superpowers:systematic-debugging` for any failure that isn't obviously a
typo. If three fix attempts fail, stop and report the evidence instead of
trying a fourth theory. Never weaken a test to make it pass; if the test is
wrong, fix the test in its own commit and say why.

## Review gates

Two artifacts need the author's eyes before anything is built on them:

- **`internal/model`** — after Phase 0 and after any field change. Present the
  type sketch and the reasoning for each non-obvious tag.
- **Manager state machine** — before Phase 3. Present the transition table and
  what happens on each edge (pause during probe, cancel during write, restart
  from partial state).

Review requests are one message: the artifact, the decisions embedded in it,
and the specific questions. Not a link and "PTAL".

## Research policy

Phase R notes live in `docs/research/<project>.md`, one per reference, each
claim carrying a file path and function name. `docs/research/decisions.md`
holds the adopted/rejected calls. See `AGENTS.md` for the licensing rules —
they are not negotiable: read GPL code for design, never copy or translate it.

## Commits

- One commit per phase, message `phase N: <summary>`, body listing decisions
  and any deviations from `PLAN.md`.
- **Review before committing.** After `make check` is green and before `git
  commit`, re-read every changed file end-to-end and confirm: each hunk does
  what it claims, no leftover debugging, no test weakened to pass, and all
  non-obvious decisions are logged in `docs/DECISIONS.md`. If anything looks
  wrong, fix it and re-run `make check` before committing.
- Never commit `bin/`, `node_modules/`, `dist/`, `*.db`, or anything a
  `.gitignore` covers. If something new needs ignoring, add the rule.
- Pre-commit runs `scripts/check.sh` via `.githooks/pre-commit`
  (enable once with `make hooks`). Do not bypass with `--no-verify`; if the
  hook is wrong, fix the hook.
