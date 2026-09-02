package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/taranpabla/treework/internal/config"
	"github.com/taranpabla/treework/internal/gitx"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return string(out)
}

// makeRepo creates a real repo with an initial commit on branch.
func makeRepo(t *testing.T, reposDir, name, branch string) string {
	t.Helper()
	dir := filepath.Join(reposDir, name)
	os.MkdirAll(dir, 0o755)
	git(t, dir, "init", "-b", branch)
	git(t, dir, "config", "user.email", "t@example.com")
	git(t, dir, "config", "user.name", "t")
	os.WriteFile(filepath.Join(dir, "README.md"), []byte(name), 0o644)
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "init")
	return dir
}

func integrationEngine(t *testing.T) (*Engine, config.Global) {
	t.Helper()
	root := t.TempDir()
	g := config.Global{
		ReposDir:    filepath.Join(root, "repos"),
		ProjectsDir: filepath.Join(root, "projects"),
		Username:    "taran",
	}
	os.MkdirAll(g.ReposDir, 0o755)
	hook := func(ctx context.Context, dir, command string) error {
		cmd := exec.CommandContext(ctx, "sh", "-c", command)
		cmd.Dir = dir
		return cmd.Run()
	}
	e := New(&gitx.ExecRunner{}, g, config.NewFileRepository(), hook)
	return e, g
}

func TestIntegrationAddCreatesWorktrees(t *testing.T) {
	e, g := integrationEngine(t)
	makeRepo(t, g.ReposDir, "repo1", "main")
	makeRepo(t, g.ReposDir, "repo2", "master") // non-main default

	ctx := context.Background()
	plan, err := e.BuildPlan(ctx, "feat", []string{"repo1", "repo2"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Repos[1].BaseBranch != "master" {
		t.Errorf("repo2 base = %q, want master", plan.Repos[1].BaseBranch)
	}
	for _, res := range e.Execute(ctx, plan, nil) {
		if res.Err != nil {
			t.Fatalf("%s: %v", res.Repo, res.Err)
		}
	}
	for _, repo := range []string{"repo1", "repo2"} {
		wt := filepath.Join(g.ProjectsDir, "feat", repo)
		out := git(t, wt, "rev-parse", "--abbrev-ref", "HEAD")
		if got := string(out); got != "taran/feat\n" {
			t.Errorf("%s HEAD = %q", repo, got)
		}
	}
	if _, err := os.Stat(filepath.Join(g.ProjectsDir, "feat", ".treework.toml")); err != nil {
		t.Error(".treework.toml not written")
	}
}

func TestIntegrationReattachReusesBranch(t *testing.T) {
	e, g := integrationEngine(t)
	makeRepo(t, g.ReposDir, "repo1", "main")
	ctx := context.Background()

	plan, _ := e.BuildPlan(ctx, "feat", []string{"repo1"})
	if res := e.Execute(ctx, plan, nil); res[0].Err != nil {
		t.Fatal(res[0].Err)
	}
	if err := e.RemoveRepo(ctx, "feat", "repo1", false); err != nil {
		t.Fatal(err)
	}

	plan2, err := e.BuildPlan(ctx, "feat", []string{"repo1"})
	if err != nil {
		t.Fatal(err)
	}
	if !plan2.Repos[0].BranchExists {
		t.Error("second plan should detect existing branch")
	}
	if res := e.Execute(ctx, plan2, nil); res[0].Err != nil {
		t.Fatalf("re-attach: %v", res[0].Err)
	}
}

func TestIntegrationDirtyRemoveRefused(t *testing.T) {
	e, g := integrationEngine(t)
	makeRepo(t, g.ReposDir, "repo1", "main")
	ctx := context.Background()
	plan, _ := e.BuildPlan(ctx, "feat", []string{"repo1"})
	e.Execute(ctx, plan, nil)

	wt := filepath.Join(g.ProjectsDir, "feat", "repo1")
	os.WriteFile(filepath.Join(wt, "dirty.txt"), []byte("x"), 0o644)

	if err := e.RemoveRepo(ctx, "feat", "repo1", false); err == nil {
		t.Fatal("want dirty refusal")
	}
	if err := e.RemoveRepo(ctx, "feat", "repo1", true); err != nil {
		t.Fatalf("forced: %v", err)
	}
}

func TestIntegrationProjectOverridesAndHook(t *testing.T) {
	e, g := integrationEngine(t)
	dir := makeRepo(t, g.ReposDir, "repo1", "main")
	git(t, dir, "branch", "develop")

	projDir := filepath.Join(g.ProjectsDir, "feat")
	os.MkdirAll(projDir, 0o755)
	os.WriteFile(filepath.Join(projDir, ".treework.toml"), []byte(`
[repos.repo1]
base_branch = "develop"
post_create_hook = "touch hooked"
`), 0o644)

	ctx := context.Background()
	plan, err := e.BuildPlan(ctx, "feat", []string{"repo1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Repos[0].BaseBranch != "develop" {
		t.Fatalf("base = %q", plan.Repos[0].BaseBranch)
	}
	if res := e.Execute(ctx, plan, nil); res[0].Err != nil {
		t.Fatal(res[0].Err)
	}
	if _, err := os.Stat(filepath.Join(projDir, "repo1", "hooked")); err != nil {
		t.Error("post-create hook did not run")
	}
}

func TestIntegrationRemoveProjectKeepsBranches(t *testing.T) {
	e, g := integrationEngine(t)
	src := makeRepo(t, g.ReposDir, "repo1", "main")
	ctx := context.Background()
	plan, _ := e.BuildPlan(ctx, "feat", []string{"repo1"})
	e.Execute(ctx, plan, nil)

	if err := e.RemoveProject(ctx, "feat", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(g.ProjectsDir, "feat")); !os.IsNotExist(err) {
		t.Error("project dir remains")
	}
	out := git(t, src, "branch", "--list", "taran/feat")
	if out == "" {
		t.Error("local branch should be kept after project removal")
	}
}
