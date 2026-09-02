package gitx

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"
)

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
		{"commit", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestExecRunnerSuccess(t *testing.T) {
	dir := initRepo(t)
	var log bytes.Buffer
	r := &ExecRunner{Log: &log}
	out, err := r.Run(context.Background(), dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "main" {
		t.Errorf("out = %q, want main (trimmed)", out)
	}
	if !strings.Contains(log.String(), "rev-parse") {
		t.Errorf("audit log missing command: %q", log.String())
	}
}

func TestExecRunnerErrorIncludesStderr(t *testing.T) {
	dir := initRepo(t)
	r := &ExecRunner{}
	_, err := r.Run(context.Background(), dir, "checkout", "no-such-branch")
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "no-such-branch") {
		t.Errorf("error should include git stderr, got: %v", err)
	}
}

func TestFakeRecordsAndMatches(t *testing.T) {
	f := NewFake()
	f.Responses["rev-parse"] = FakeResponse{Out: "main"}
	out, err := f.Run(context.Background(), "/x", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || out != "main" {
		t.Fatalf("got %q, %v", out, err)
	}
	if len(f.Calls) != 1 || f.Calls[0].Dir != "/x" {
		t.Fatalf("calls = %+v", f.Calls)
	}
}
