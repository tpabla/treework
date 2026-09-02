// Package gitx runs git commands. It is the only package allowed to
// execute git; everything else depends on the Runner interface.
package gitx

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Call records one git invocation.
type Call struct {
	Dir  string
	Args []string
}

// Runner executes git in a directory and returns trimmed stdout.
type Runner interface {
	Run(ctx context.Context, dir string, args ...string) (string, error)
}

// ExecRunner shells out to the system git binary, logging each command.
type ExecRunner struct {
	Log io.Writer // optional audit log
}

func (r *ExecRunner) Run(ctx context.Context, dir string, args ...string) (string, error) {
	if r.Log != nil {
		fmt.Fprintf(r.Log, "%s: git %s\n", dir, strings.Join(args, " "))
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// git spawns ssh, which can outlive it holding the output pipes open.
	// Without a delay Wait blocks on those pipes even after cancellation.
	cmd.WaitDelay = 2 * time.Second
	// Never let git or ssh prompt: a prompt on /dev/tty is invisible under
	// the alt-screen TUI and would block forever. An explicit
	// GIT_SSH_COMMAND is left alone so custom ssh setups keep working.
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if os.Getenv("GIT_SSH_COMMAND") == "" {
		env = append(env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	}
	cmd.Env = env
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
