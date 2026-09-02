# TreeWork

Worktree-based cross-repo feature development. One command creates git
worktrees for several repos under a single project directory, on
consistently named branches.

## Install

```sh
go build -o ~/bin/treework ./cmd/treework
```

## Configure

`~/.config/treework/config.toml`:

```toml
repos_dir = "~/Projects"
projects_dir = "~/worktrees/projects"
username = "taran"                       # optional, defaults to $USER
# branch_template = "{username}/{project}"
# default_base_branch = "main"           # optional, auto-detected per repo
# pull_before_worktree = true
# post_create_hook = "direnv allow"
```

Per-project overrides live in `<project>/.treework.toml`:

```toml
branch_template = "{username}/{project}-{repo}"

[repos.repo1]
base_branch = "develop"
post_create_hook = "npm install"
```

## Use

```sh
treework                                   # TUI: pick/create project, tag repos with t, confirm
treework add --project feat --repos r1,r2  # non-interactive
treework list [--project feat]
treework open feat [--edit]
treework rm --project feat [--repos r1] [--force]
treework sync [--project feat]            # rebase all worktrees onto their base branch;
                                          # --project optional inside a project directory
```

`treework` prints the project path on exit, so a shell wrapper enables
jumping into it:

```sh
tw() { cd "$(treework open "$1")"; }
```

Keys in the TUI: `j`/`k`/`gg`/`G` move, `/` filters, `t` tags repos for
multi-select, `Enter` confirms, `Esc` goes back, `q` quits.

All git commands are logged to `~/.local/state/treework/log`.

## Development

```sh
go test ./...
```

Docs: `docs/spec/initial_design.md`, `docs/design/technical_design.md`.
