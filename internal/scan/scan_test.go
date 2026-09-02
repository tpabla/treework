package scan

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func mkdir(t *testing.T, parts ...string) string {
	t.Helper()
	p := filepath.Join(parts...)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRepos(t *testing.T) {
	dir := t.TempDir()
	mkdir(t, dir, "repo1", ".git")
	mkdir(t, dir, "repo2", ".git")
	mkdir(t, dir, "not-a-repo")
	mkdir(t, dir, "bare.git") // bare repo: no .git inside, skipped
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("x"), 0o644)

	got, err := Repos(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"repo1", "repo2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Repos = %v, want %v", got, want)
	}
}

func TestReposWorktreeGitFile(t *testing.T) {
	dir := t.TempDir()
	mkdir(t, dir, "wt")
	os.WriteFile(filepath.Join(dir, "wt", ".git"), []byte("gitdir: /elsewhere"), 0o644)
	got, err := Repos(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"wt"}) {
		t.Errorf("Repos = %v, want [wt] (.git file counts)", got)
	}
}

func TestProjects(t *testing.T) {
	dir := t.TempDir()
	mkdir(t, dir, "proj-b")
	mkdir(t, dir, "proj-a")
	os.WriteFile(filepath.Join(dir, "stray.txt"), []byte("x"), 0o644)

	got, err := Projects(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"proj-a", "proj-b"} // sorted
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Projects = %v, want %v", got, want)
	}
}

func TestProjectsMissingDirIsEmpty(t *testing.T) {
	got, err := Projects(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("missing projects dir should not error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

func TestAttachedRepos(t *testing.T) {
	dir := t.TempDir()
	proj := mkdir(t, dir, "feat")
	mkdir(t, proj, "repo1")
	os.WriteFile(filepath.Join(proj, "repo1", ".git"), []byte("gitdir: x"), 0o644)
	mkdir(t, proj, "notes") // plain dir, not a worktree
	os.WriteFile(filepath.Join(proj, ".treework.toml"), []byte(""), 0o644)

	got, err := AttachedRepos(proj)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"repo1"}) {
		t.Errorf("AttachedRepos = %v, want [repo1]", got)
	}
}
