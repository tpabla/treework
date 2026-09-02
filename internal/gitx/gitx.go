// Package gitx runs git commands. It is the only package allowed to
// execute git; everything else depends on the Runner interface.
package gitx

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
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
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
