# TreeWork - a worktree based approach to cross repo feature development.

## Problem statement

My current workflow requires a lot of typing and annoying cd'ing into directories where I need to create worktrees for multiple repos and point them a folder for a feature, investigation, or project i am working on.

For instance given this setup.
My Repos are all stored in `~/Projects/`
My project/feature/investigation dev is in `~/worktrees/projects/`
Feature i am working on is called `test-feature`

Lets say i want to work on `repo1` and `repo2`
1. I have to navigate to `~/Projects/repo1`
2. I make sure i have the latest main so i `git pull`
3. I have to make the project/feature i am working on if it doesn't exist `mkdir ~/worktrees/projects/test-feature`
4. i have to make a worktree for the feature i'm working on so `git worktree add ~/worktrees/projects/test-feature/repo1 -b username/test-feature`
5. repeat 1-4 for the next repo.

## Goals

- Reduce the "start working on a feature across N repos" flow to a single command and a few keystrokes.
- Keep the tool portable: a single binary or script with minimal runtime dependencies, installable via a one-liner.
- Never surprise the user: all git operations are the same ones they would run by hand, and destructive actions require confirmation.

## Non-goals

- Not a git GUI or a replacement for lazygit/tig. TreeWork only manages worktree lifecycle.
- No remote/PR management (no pushing, no PR creation) in v1.
- No repo discovery beyond the configured repos directory (no GitHub org scanning).
- No Windows support in v1 (macOS and Linux only).

## Terminology

- **Repo**: a git repository living under the configured repos directory (e.g. `~/Projects/repo1`).
- **Project**: a named unit of work (feature, investigation, spike). Materialized as a directory under the projects directory (e.g. `~/worktrees/projects/test-feature/`).
- **Worktree**: a git worktree of a repo, checked out inside a project directory (e.g. `~/worktrees/projects/test-feature/repo1`).

## Functional Requirements

### Configuration
1. Config lives at `~/.config/treework/config.toml` (XDG-compliant; respects `$XDG_CONFIG_HOME`).
2. Config fields:
   - `repos_dir` (required): directory containing the user's clones, e.g. `~/Projects`.
   - `projects_dir` (required): directory where project folders are created, e.g. `~/worktrees/projects`.
   - `username` (optional): used as the branch prefix; defaults to the OS user (`$USER`).
   - `branch_template` (optional): defaults to `{username}/{project}`. Supported tokens: `{username}`, `{project}`, `{repo}`.
   - `default_base_branch` (optional): defaults to the repo's default branch (detected via `origin/HEAD`, falling back to `main` then `master`).
   - `pull_before_worktree` (optional, default `true`): fetch/pull the base branch before creating a worktree.
   - `post_create_hook` (optional): shell command run inside each new worktree (e.g. `npm install`, `direnv allow`).
3. First run with no config launches a short interactive setup that writes the config file.
4. Invalid or missing required config produces a clear error naming the file and field, never a stack trace.

### Project-level configuration
5. Each project directory may contain a `.treework.toml` at its root (`<projects_dir>/<project>/.treework.toml`) with project-scoped settings and per-repo overrides:

   ```toml
   # <projects_dir>/test-feature/.treework.toml
   branch_template = "{username}/test-feature-{repo}"  # project-wide override
   post_create_hook = "direnv allow"                    # project-wide override

   [repos.repo1]
   base_branch = "develop"
   post_create_hook = "npm install"

   [repos.repo2]
   branch = "username/existing-branch"  # exact branch name, skips template
   ```
6. Resolution order (most specific wins): per-repo table in `.treework.toml` > project-wide keys in `.treework.toml` > global `~/.config/treework/config.toml` > built-in defaults.
7. TreeWork writes a minimal `.treework.toml` when creating a project (recording resolved settings so the project is self-describing) and re-reads it when repos are later attached, so overrides apply to future additions too.
8. An invalid `.treework.toml` fails with a clear error naming the file and field; it never silently falls back to global config.

### Project selection screen
9. On launch, the TUI shows a list of existing projects (subdirectories of `projects_dir`) plus a "New project" action.
10. Vim keybindings: `j`/`k` (and arrows) to move, `gg`/`G` for top/bottom, `/` to fuzzy-search/filter the list, `Enter` to select, `q`/`Esc` to quit.
11. Selecting "New project" prompts for a project name. Names are validated (non-empty, filesystem- and branch-safe characters).
12. Each project row shows the project name and a summary of repos already attached (e.g. `test-feature  [repo1, repo2]`).

### Repo selection screen
13. After choosing a project, the TUI lists all repos found in `repos_dir` (a repo = a directory containing `.git`).
14. Repos already attached to the project are visually marked and cannot be double-added.
15. `t` (or `Space`) toggles a tag on a repo for multi-select; `Enter` on an untagged repo selects just that one; `Enter` with tags confirms all tagged repos.
16. `/` fuzzy-search works the same as the project screen.
17. Before executing, a confirmation screen shows exactly what will happen: per repo, the base branch, the new branch name, and the worktree path. `y`/`Enter` confirms, `n`/`Esc` goes back.

### Worktree creation
18. For each selected repo, in order:
    a. `git fetch origin` in the source repo (skipped if `pull_before_worktree` is false or the repo has no remote).
    b. Create the project directory if it doesn't exist.
    c. `git worktree add <projects_dir>/<project>/<repo> -b <branch>` based off the base branch (`origin/<base>` when a remote exists, local base otherwise).
    d. Run `post_create_hook` if configured.
19. If the branch already exists (e.g. re-attaching a repo whose worktree was removed), reuse it instead of failing: `git worktree add <path> <branch>`.
20. Progress is shown per repo (pending / running / done / failed). One repo failing does not abort the others; failures are summarized at the end with the underlying git error.
21. Never touches the user's checked-out branch in the source repo; all base-branch updates go through `fetch` + worktree-from-`origin/<base>`, not `checkout`/`pull` on the user's working copy.

### Project management
22. From the project list, `d` on a project opens a removal flow: removes each worktree (`git worktree remove`), then the project directory. Refuses (with override prompt) if any worktree has uncommitted changes. Local branches are left intact by default; a follow-up prompt offers local branch deletion. Remote branches are never touched.
23. From the repo list within a project, `d` on an attached repo removes just that repo's worktree, with the same dirty-state protection.
24. On exit after creating worktrees, print the project path to stdout so shell integration can `cd` into it (`cd $(treework ...)` or a provided shell wrapper function).
25. `treework open <project>` prints the project path, or launches `$EDITOR` in it with `--edit`. Works with the shell wrapper for `two() { cd $(treework open $1) }`-style usage.

### Non-interactive mode
26. All flows are scriptable via flags for automation and testing:
    - `treework add --project test-feature --repos repo1,repo2`
    - `treework rm --project test-feature [--repos repo1]`
    - `treework list [--project test-feature]`
    - `treework open <project> [--edit]`
27. Non-interactive mode uses the same core logic as the TUI (thin CLI and TUI layers over one engine).

## Non-functional requirements

- **Portability**: macOS (arm64/x86_64) and Linux. Single artifact install; no language runtime required on the target machine (or, if scripted, only ubiquitous dependencies).
- **Performance**: launch to interactive < 200ms with ~100 repos/projects; directory scans are shallow (one level).
- **Safety**: no destructive git operations without confirmation; all git commands logged to `~/.local/state/treework/log` for auditability.
- **Respect the environment**: honors `$GIT_*` env vars; uses the system `git` binary (>= 2.30) rather than a bundled git implementation, so credentials, hooks, and config behave identically to manual usage.

## Edge cases to handle

- Repo has no remote (local-only repo): skip fetch, branch from local base.
- Repo's default branch is not `main` (detect via `origin/HEAD`).
- Branch name already exists locally or a worktree for it already exists elsewhere.
- Project directory exists but is empty, or contains non-worktree files.
- Stale worktree entries (`git worktree prune` candidates) — offer to prune.
- Bare repos in `repos_dir`.
- `repos_dir`/`projects_dir` on different filesystems or containing symlinks.
- Repo directory name differs from remote repo name (use directory name everywhere).

## Resolved questions

- Per-repo base branch overrides: **yes** — via project-level `.treework.toml` (per-repo tables for base branch, exact branch name, post-create hooks). See "Project-level configuration".
- Remote branch deletion on project removal: **no** — remote branches are never touched.
- `treework open <project>`: **yes**, in scope for v1 (print path, `--edit` for `$EDITOR`).
