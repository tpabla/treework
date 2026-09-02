// Package cli implements the non-interactive subcommands. It is a thin
// adapter over the engine; the TUI is the other adapter.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/tpabla/treework/internal/engine"
)

// Engine is the subset of engine.Engine the CLI needs (injected for tests).
type Engine interface {
	BuildPlan(ctx context.Context, project string, repos []string) (engine.Plan, error)
	Execute(ctx context.Context, plan engine.Plan, progress func(engine.RepoResult)) []engine.RepoResult
	RemoveRepo(ctx context.Context, project, repo string, force bool) error
	RemoveProject(ctx context.Context, project string, force bool) error
	Sync(ctx context.Context, project string, progress func(engine.RepoResult)) ([]engine.RepoResult, error)
	ProjectDir(project string) string
}

// Scanner lists projects and attached repos (injected for tests).
type Scanner interface {
	Projects() ([]string, error)
	AttachedRepos(project string) ([]string, error)
}

// App wires the CLI to its dependencies.
type App struct {
	Engine  Engine
	Scanner Scanner
	Stdout  io.Writer
	Stderr  io.Writer
	Editor  func(dir string) error // launches $EDITOR for open --edit
	// CwdProject returns the project inferred from the working
	// directory, or "" when not inside a project.
	CwdProject func() string
}

// Run dispatches: add, rm, list, open. Returns an error for unknown
// commands, bad flags, or any failed repo.
func (a *App) Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: treework <add|rm|list|open> [flags]")
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "add":
		return a.add(ctx, rest)
	case "rm":
		return a.rm(ctx, rest)
	case "list":
		return a.list(rest)
	case "open":
		return a.open(rest)
	case "sync":
		return a.sync(ctx, rest)
	default:
		return fmt.Errorf("unknown command %q (want add, rm, list, open, or sync)", cmd)
	}
}

func (a *App) add(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	project := fs.String("project", "", "project name")
	repos := fs.String("repos", "", "comma-separated repo names")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *project == "" || *repos == "" {
		return errors.New("add requires --project and --repos")
	}
	plan, err := a.Engine.BuildPlan(ctx, *project, splitList(*repos))
	if err != nil {
		return err
	}
	var failed []string
	a.Engine.Execute(ctx, plan, func(r engine.RepoResult) {
		if r.Err != nil {
			failed = append(failed, r.Repo)
			fmt.Fprintf(a.Stderr, "%s: FAILED: %v\n", r.Repo, r.Err)
		} else {
			fmt.Fprintf(a.Stdout, "%s: created\n", r.Repo)
		}
	})
	fmt.Fprintln(a.Stdout, plan.ProjectDir)
	if len(failed) > 0 {
		return fmt.Errorf("failed repos: %s", strings.Join(failed, ", "))
	}
	return nil
}

func (a *App) rm(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("rm", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	project := fs.String("project", "", "project name")
	repos := fs.String("repos", "", "comma-separated repo names (omit to remove whole project)")
	force := fs.Bool("force", false, "remove even with uncommitted changes")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *project == "" {
		return errors.New("rm requires --project")
	}
	if *repos == "" {
		return a.Engine.RemoveProject(ctx, *project, *force)
	}
	for _, repo := range splitList(*repos) {
		if err := a.Engine.RemoveRepo(ctx, *project, repo, *force); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) list(args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	project := fs.String("project", "", "show repos attached to one project")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *project != "" {
		repos, err := a.Scanner.AttachedRepos(*project)
		if err != nil {
			return err
		}
		for _, r := range repos {
			fmt.Fprintln(a.Stdout, r)
		}
		return nil
	}
	projects, err := a.Scanner.Projects()
	if err != nil {
		return err
	}
	for _, p := range projects {
		fmt.Fprintln(a.Stdout, p)
	}
	return nil
}

func (a *App) open(args []string) error {
	fs := flag.NewFlagSet("open", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	edit := fs.Bool("edit", false, "launch $EDITOR in the project directory")
	// accept both "open <project> --edit" and "open --edit <project>"
	var positional []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return err
		}
		if fs.NArg() == 0 {
			break
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
	if len(positional) != 1 {
		return errors.New("usage: treework open <project> [--edit]")
	}
	project := positional[0]
	projects, err := a.Scanner.Projects()
	if err != nil {
		return err
	}
	found := false
	for _, p := range projects {
		if p == project {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("unknown project %q", project)
	}
	dir := a.Engine.ProjectDir(project)
	if *edit {
		if a.Editor == nil {
			return errors.New("no editor configured")
		}
		return a.Editor(dir)
	}
	fmt.Fprintln(a.Stdout, dir)
	return nil
}

func (a *App) sync(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	project := fs.String("project", "", "project name (default: inferred from cwd)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	name := *project
	if name == "" && a.CwdProject != nil {
		name = a.CwdProject()
	}
	if name == "" {
		return errors.New("sync requires --project or being inside a project directory")
	}
	var failed []string
	results, err := a.Engine.Sync(ctx, name, func(r engine.RepoResult) {
		if r.Err != nil {
			failed = append(failed, r.Repo)
			fmt.Fprintf(a.Stderr, "%s: FAILED: %v\n", r.Repo, r.Err)
		} else {
			fmt.Fprintf(a.Stdout, "%s: rebased\n", r.Repo)
		}
	})
	if err != nil {
		return err
	}
	if len(results) == 0 {
		return fmt.Errorf("project %q has no attached repos", name)
	}
	if len(failed) > 0 {
		return fmt.Errorf("failed repos: %s", strings.Join(failed, ", "))
	}
	return nil
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
