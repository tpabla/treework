# TreeWork - Technical Design

Status: draft — tooling decision pending (see decision matrix).

## Architecture overview

Regardless of tooling choice, the structure is the same three layers:

```
+--------------------+     +--------------------+
|   TUI frontend     |     |   CLI frontend     |
|  (screens, keys)   |     |  (flags, output)   |
+---------+----------+     +----------+---------+
          |                           |
          v                           v
+--------------------------------------------+
|                Core engine                  |
|  ProjectService / RepoService (pure logic)  |
+---------------------+----------------------+
                      |
        +-------------+--------------+
        v                            v
+---------------+          +------------------+
| GitRunner     |          | FileSystem /     |
| (shells out   |          | ConfigRepository |
|  to git CLI)  |          |                  |
+---------------+          +------------------+
```

- **Core engine**: pure, testable logic — plan what worktrees/branches to create, validate names, diff desired vs. actual state. No direct I/O; depends on injected interfaces.
- **GitRunner**: the only place that executes `git`. Interface-shaped so tests inject a fake; integration tests run against real temp repos.
- **ConfigRepository / FileSystem**: config load/validate/write and directory scanning behind interfaces (repository pattern; core logic never touches disk directly).
- **Frontends**: TUI and CLI are both thin adapters over the engine, so every interactive flow has a scriptable equivalent (spec req. 26-27).

### Config layering

Two config sources merged at plan time, most specific wins:

```
per-repo table in <project>/.treework.toml
  > project-wide keys in <project>/.treework.toml
    > ~/.config/treework/config.toml
      > built-in defaults
```

ConfigRepository loads both files into one `ResolvedConfig` per (project, repo) pair; the engine only ever sees resolved values, so layering logic lives in exactly one place and is unit-testable without disk. When a project is created, the engine writes a minimal `.treework.toml` recording the resolved settings so the project is self-describing and later `add`s inherit the same overrides.

Key design rules:

1. Shell out to the system `git` binary — never embed a git library (libgit2/go-git/gitoxide). Credentials, hooks, and config then behave exactly as manual usage, and behavior is trivially auditable (log the exact commands).
2. Plan-then-execute: build a full plan (fetches, worktree adds, hooks) and show it on the confirmation screen; execution just walks the plan. This makes dry-run and the confirmation UI free.
3. Per-repo isolation during execution: each repo's steps run independently; failures collect into a summary rather than aborting the batch.

## Data / state

- No database. State is derived from the filesystem + git each launch:
  - Projects = subdirectories of `projects_dir`.
  - Attached repos = subdirectories of a project that are valid worktrees (`git -C <dir> rev-parse --is-inside-work-tree` + `git worktree list` from the source repo).
- Config at `~/.config/treework/config.toml`, plus per-project `.treework.toml` in each project directory (see Config layering); command log at `~/.local/state/treework/log`.

## Testing strategy

- Unit tests on the core engine with a fake GitRunner/FileSystem (fast, no git needed).
- Integration tests that create real temp repos with fixtures (no remote, non-main default branch, existing branch, dirty worktree) and run the CLI frontend against them.
- TUI tested via the framework's test harness where available (Bubble Tea's `teatest`, Textual's `Pilot`, Ratatui's `TestBackend`).

## Tooling decision matrix

The real decision is **language + TUI framework** as a pair; distribution story follows from the language.

Scoring 1-5 (5 best), weighted by what the spec cares about:

| Criterion (weight) | Go + Bubble Tea | Rust + Ratatui | TypeScript + Ink | Python + Textual | Shell + fzf/gum |
|---|---|---|---|---|---|
| Portability / single-binary distribution (x3) | 5 | 5 | 2 | 2 | 3 |
| TUI ergonomics: lists, fuzzy search, vim keys (x3) | 5 | 4 | 3 | 5 | 4 |
| Dev velocity / iteration speed (x2) | 4 | 2 | 4 | 5 | 5 |
| Testability (DI, fakes, TUI harness) (x2) | 5 | 4 | 3 | 4 | 1 |
| Startup time / perceived snappiness (x1) | 5 | 5 | 3 | 2 | 5 |
| Ecosystem fit for this exact tool (x1) | 5 | 4 | 3 | 4 | 3 |
| Long-term maintenance burden (x1) | 4 | 4 | 3 | 3 | 2 |
| **Weighted total (/65)** | **61** | **52** | **39** | **48** | **43** |

### Option details

**Go + Bubble Tea (charmbracelet)** — recommended
- Bubble Tea + Bubbles gives list-with-fuzzy-filter, multi-select, and text input out of the box; Lip Gloss for styling; `teatest` for TUI tests. This is the ecosystem lazygit/gh-dash-style tools live in.
- `go build` produces static binaries for darwin/linux both arches; goreleaser + Homebrew tap is a solved one-afternoon problem.
- Interfaces make the GitRunner/repository DI pattern natural.
- Cons: Elm-architecture boilerplate for simple screens; error handling verbosity.

**Rust + Ratatui**
- Best runtime characteristics and strong typing; `ratatui` is mature, `tui-input`/`nucleo` cover input and fuzzy matching.
- Cons: slowest to iterate; more assembly required (Ratatui is a rendering library, not a component kit — you build list state, event loop, and focus handling yourself). Worth it if you want the Rust practice; overkill for the tool's needs.

**TypeScript + Ink (React for CLIs)**
- Familiar React model, fast iteration, easy testing with ink-testing-library.
- Cons: needs Node on the target machine or bundling (`bun build --compile` / pkg produce 60-90MB binaries); slower startup; weakest "portable script" story of the compiled options.

**Python + Textual**
- The nicest TUI DX of the lot (CSS-like styling, built-in widgets, `Pilot` test harness, live dev reload); great if you value polish and speed of building screens.
- Cons: distribution — needs Python + venv, or PyInstaller/shiv artifacts that are large and slower to start; "pip install" tools rot with system Python changes. Fine if this is personal-only; weak if you want teammates to `brew install` it.

**Shell (bash) + fzf + gum**
- Closest to "simple portable script"; fzf natively gives fuzzy search and multi-select with `t`-style tagging; could be working in an evening.
- Cons: no real structure for the engine/DI/testing goals; multi-screen flows, confirmation UIs, and per-repo progress get painful; error handling in bash across N repos is where these scripts go to die. Best as a throwaway v0 to validate the UX, not the v1.

### Secondary decisions (mostly settled by spec, listed for completeness)

| Decision | Choice | Alternatives considered |
|---|---|---|
| Config format | TOML | YAML (whitespace footguns), JSON (no comments) |
| Git access | Shell out to system `git` | go-git/libgit2 (diverges from user's git config/credentials) |
| Fuzzy matching | Framework-native (Bubbles filter / nucleo / fzf) | Custom implementation |
| Distribution | goreleaser → GitHub releases + Homebrew tap (if Go/Rust) | curl-pipe installer, cargo/pip install |
| State storage | Derive from filesystem + git each run | Manifest file (drifts from reality), SQLite (overkill) |

## Recommendation

Go + Bubble Tea. It's the only option scoring high on both of the spec's two hard requirements — portable single-artifact distribution and a rich list-driven TUI — while keeping the DI/repository testing story clean. Rust is the runner-up if you want the learning value; bash+fzf is a good weekend v0 to validate keybindings and flow before committing.

## Proposed module layout (Go, for reference)

```
cmd/treework/          main, flag parsing, wiring (composition root)
internal/config/       ConfigRepository: load/validate/write TOML
internal/gitx/         GitRunner interface + exec implementation + fake
internal/engine/       ProjectService, RepoService, Plan/Execute
internal/scan/         repo & project directory scanning
internal/tui/          Bubble Tea models: project list, repo select, confirm, progress
internal/cli/          non-interactive subcommands (add/rm/list)
```

## Milestones

1. **v0 skeleton**: config load (global + project `.treework.toml` layering) + `list`/`add`/`open` CLI (no TUI), integration-tested against temp repos.
2. **v1 TUI**: project + repo selection screens, confirmation, progress, removal flows.
3. **v1.1**: shell integration (`cd` helper), post-create hooks, prune/stale handling.
