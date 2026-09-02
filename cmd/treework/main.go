// Command treework creates and manages git worktrees for cross-repo
// feature development. With no arguments it launches the TUI; with a
// subcommand (add, rm, list, open) it runs non-interactively.
package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/tpabla/treework/internal/cli"
	"github.com/tpabla/treework/internal/config"
	"github.com/tpabla/treework/internal/engine"
	"github.com/tpabla/treework/internal/gitx"
	"github.com/tpabla/treework/internal/scan"
	"github.com/tpabla/treework/internal/tui"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "treework:", err)
		os.Exit(1)
	}
}

// scanner adapts the scan package to the cli/tui Scanner interfaces.
type scanner struct{ global config.Global }

func (s scanner) Projects() ([]string, error) { return scan.Projects(s.global.ProjectsDir) }
func (s scanner) Repos() ([]string, error)    { return scan.Repos(s.global.ReposDir) }
func (s scanner) AttachedRepos(project string) ([]string, error) {
	return scan.AttachedRepos(filepath.Join(s.global.ProjectsDir, project))
}

func run(args []string) error {
	// Answered before loading config so it works on a fresh machine.
	if len(args) == 1 && (args[0] == "--version" || args[0] == "-v" || args[0] == "version") {
		fmt.Println("treework", version)
		return nil
	}
	configs := config.NewFileRepository()
	global, err := configs.LoadGlobal()
	if errors.Is(err, fs.ErrNotExist) {
		if len(args) > 0 {
			return fmt.Errorf("no config found; run `treework` once to complete setup (%w)", err)
		}
		done, serr := tui.RunSetup(configs.WriteGlobal)
		if serr != nil {
			return serr
		}
		if !done {
			return errors.New("setup aborted")
		}
		global, err = configs.LoadGlobal()
	}
	if err != nil {
		return err
	}
	logw, _ := openAuditLog()
	if logw != nil {
		defer logw.Close()
	}
	hook := func(ctx context.Context, dir, command string) error {
		cmd := exec.CommandContext(ctx, "sh", "-c", command)
		cmd.Dir = dir
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	eng := engine.New(&gitx.ExecRunner{Log: logw}, global, configs, hook)
	sc := scanner{global: global}

	if len(args) == 0 {
		final, err := tui.Run(eng, sc)
		if err != nil {
			return err
		}
		if dir := final.ProjectDir(); dir != "" {
			fmt.Println(dir)
		}
		return nil
	}

	app := &cli.App{
		Engine:  eng,
		Scanner: sc,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		CwdProject: func() string {
			cwd, err := os.Getwd()
			if err != nil {
				return ""
			}
			return engine.InferProject(cwd, global.ProjectsDir)
		},
		Editor: func(dir string) error {
			editor := os.Getenv("EDITOR")
			if editor == "" {
				return fmt.Errorf("$EDITOR is not set")
			}
			cmd := exec.Command(editor, ".")
			cmd.Dir = dir
			cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
			return cmd.Run()
		},
	}
	return app.Run(context.Background(), args)
}

func openAuditLog() (*os.File, error) {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(home, ".local", "state")
	}
	dir = filepath.Join(dir, "treework")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(dir, "log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
}
