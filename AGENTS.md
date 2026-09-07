# hcm-next Agent Instructions

## Scope

These instructions apply to the whole repository. `CLAUDE.md` at the root is a thin pointer to this file so the rules live in one place. The coding discipline is informed by the vendored Karpathy guidelines under `.claude/skills/karpathy-guidelines/`; where the two disagree, this file wins.

## Who decides what

| Question                                           | Authority                                                               |
| -------------------------------------------------- | ----------------------------------------------------------------------- |
| What gets built, in which phase, behind which gate | `planning/plan.md`, `planning/execution-plan.md`, `planning/specs/*`    |
| The unit of work and its proof                     | `planning/todos.md` (RED, GREEN, TEST and TEST MATRIX are the contract) |
| How the work is done                               | this file                                                               |
| What already exists                                | source, tests, migrations, `definitions/`                               |

Where they disagree about implemented behaviour, the source wins. Where they disagree about intent, the plan wins. Do not invent packages, tables or capabilities because the plan anticipates them.

## Tracked agent configuration

| Path                                  | What it is                                                                                                                              |
| ------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| `.claude/settings.json`               | Permissions. Gates and read-only git are pre-allowed; bypassing the hook, stashing, hard resets, amends, rebases and pushes are denied. |
| `.claude/agents/gate-runner.md`       | Runs the gates and returns a triaged failure list instead of raw logs.                                                                  |
| `.claude/skills/karpathy-guidelines/` | Vendored verbatim from `multica-ai/andrej-karpathy-skills` at `2c60614`, MIT by its own frontmatter.                                    |

## Quality gates

Four gates hold the project on track. All of them run in the pre-commit hook (`.husky/pre-commit`) and in CI (`.github/workflows/tests.yml`); a commit that fails one does not land.

| Gate           | Command                                                        | Rule                                                                                |
| -------------- | -------------------------------------------------------------- | ----------------------------------------------------------------------------------- |
| Format         | `npm run format:check`, `gofmt -l`                             | Every tracked file is formatted. `gofmt -w` only files you created or edited.       |
| Lint           | `npm run lint`, `npm run check:code-style`, `go vet`           | No lint findings, no vet findings, house code-style rules hold.                     |
| Unit tests     | `npm run check:coverage:staged`, `npm run test`                | Every package holding a staged Go file passes `go test`; the vitest suites pass.    |
| Coverage floor | `npm run check:coverage:staged` (CI: `npm run check:coverage`) | Every gated package covers at least 70% of statements; no package is without tests. |

The coverage policy is `definitions/toolchain/coverage-gate.yaml`. A package below the floor either gets tests or an exception naming its exact path, kind, owner, reason and expiry; prefixes and wildcards are refused, and an expired exception waives nothing. The gate judges by `go test` result lines, so the Windows "unlinkat ... Access is denied" exit is not a failure.

The hook also runs the drift, API, substrate-coverage, engine-coverage and race-policy gates, the nested-module tests and the build. Never bypass it: no `--no-verify`, ever. If the hook is red because of another session's half-written file, wait for that file to compile; do not edit it.

Root `go test ./...` is not a gate on the development machine: every data package starts an embedded PostgreSQL and the full run takes hours. Test one package at a time with `go test -count=1 ./<pkg>/`; CI runs the whole module with the race detector.

## Task lifecycle

1. Read this file, then the todo entries you are closing in `planning/todos.md`, then every package their Refs name.
2. Inspect the source, tests, migrations and git state actually in scope. Another session may be working in the same checkout; `git status` is not all yours.
3. Run the narrowest relevant test before changing anything.
4. Implement the smallest sufficient change. No speculative abstraction, no adjacent cleanup.
5. Rerun the targeted package, then `gofmt -l`, `go vet ./<pkg>/` and `npm run check:coverage:staged`.
6. Inspect the final diff and `git status`. Nothing outside the change, no scratch directories, no credentials.
7. Report what you verified and what remains partial. A finished change under an unticked todo looks like work nobody started; a ticked todo without passing evidence is a lie. Neither is acceptable.

## Go engineering rules

- Module path is `github.com/monstercameron/hcm-next`. The typo `monstercamarin` recurs; check it.
- Every hand-written `.go` file has a test in its package that exercises it. Generated code (`gen/go/...`), `testdata/` fixtures and thin `cmd` wrappers whose every call is covered by their library are the only exclusions, and each is named in `planning/test_coverage_root.md`.
- A test must be able to fail: assert on outputs, errors (`errors.Is` against sentinels) and state after the call. A test that only calls a function, repeats its fixture or aliases another test is not a test.
- Test names come from the todo's TEST and TEST MATRIX fields (`TestTodo_<ID>`, `TestTodo_<ID>_<Kind>`). A matrix label must prove what it says: a Race test runs goroutines, an Integration test reaches a real store, a Golden test pins bytes.
- Kernel packages are pure: no database, no clock, no network, no new dependencies. Data packages use `internal/data/pgtest` (`pgtest.New(t)`, `TestMain` with `pgtest.RunMain`); if pgtest reports an archive or startup lock, wait sixty seconds and retry.
- Every tenant-scoped table has `tenant_isolation` RLS and every append-only table a `forbid_mutation` trigger; every new table gets a `definitions/storage/storage-disposition.yaml` row. plpgsql blocks in migrations need `-- +goose StatementBegin/End`.
- Transport is a thin generated boundary (ARCH-GO-023): handlers call ports, never business logic. Commands reach the capability registry through `internal/application`, never by importing `internal/capability`.
- Package-level mutable registries are refused by the composition-root rule; state lives on the value that owns it.
- Never edit `go.mod` or `go.sum` inside a lane; report the dependency instead.

## Ownership and lanes

Work is delivered by coding lanes (Codex "Luna" subagents) coordinated by an orchestrator session. The orchestrator owns `planning/`, `definitions/`, `go.mod`, `go.sum`, git, registry ticks, migration numbering and commits. A lane owns only the file roots its brief names, writes additive files, never runs git, never edits planning or definitions, and reports the registry rows, manifest entries and policy edits it needs verbatim.

Front-end surfaces (`WEB-`, `UX-`, `UXFLOW-`, `A11Y-` todos; `internal/humanwork`, `tools/uxqual`, `cmd/frontenddev`) belong to a separate session. Leave those files alone.

## Git discipline

- Commit only when asked, in groups by area (data, trust, domain, operations, transport, policy, docs), each through the full hook.
- Never `git stash`, `git reset --hard`, `git commit --amend`, `git rebase` or `git push`. Never `--no-verify`.
- Every commit ends with `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`.
- Update `CHANGELOG.md` and the current `planning/devlog/` entry with the commits, the defects found and the decisions taken. Devlog updates say what was verified and what was left partial, in plain prose.

## Documents

Do not create a new Markdown file unless the user explicitly asked for that file. Existing planning documents may be edited when a todo or the user requires it. `planning/todos.md` ticks carry an evidence line naming the tests and the exact `go test` command in backticks.

## Environment notes

- Windows 11 on arm64, Go 1.26, no Docker, no race detector locally.
- Leaked scratch directories at the repo root (`.gotmp*`, `.codex-*`, `.tmp-*`, `.lane-gotmp-*`, `tmp/`) trip `gofmt`; they are ignored by git and safe to delete.
- Stale embedded PostgreSQL servers and `go-build` temp directories accumulate under `%TEMP%`; sweep them when disk or CPU is short.
