package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/tpabla/treework/internal/gitx"
)

// attach fakes an attached repo: a project subdir with a .git file.
func attach(t *testing.T, projectsDir, project, repo string) {
	t.Helper()
	dir := filepath.Join(projectsDir, project, repo)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: x"), 0o644)
}

func TestSyncRebasesEachWorktree(t *testing.T) {
	g := testGlobal(t)
	fake := gitx.NewFake()
	fake.Responses["remote"] = gitx.FakeResponse{Out: "origin"}
	fake.Responses["symbolic-ref"] = gitx.FakeResponse{Out: "origin/main"}
	e, _, _ := newTestEngine(t, g, fake)
	attach(t, g.ProjectsDir, "feat", "repo1")
	attach(t, g.ProjectsDir, "feat", "repo2")

	results, err := e.Sync(context.Background(), "feat", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v", results)
	}
	lines := fake.CommandLines()
	for _, repo := range []string{"repo1", "repo2"} {
		src := filepath.Join(g.ReposDir, repo)
		wt := filepath.Join(g.ProjectsDir, "feat", repo)
		if !hasLine(lines, src+": git fetch origin") {
			t.Errorf("%s: missing fetch, got %v", repo, lines)
		}
		if !hasLine(lines, wt+": git rebase --autostash origin/main") {
			t.Errorf("%s: missing rebase, got %v", repo, lines)
		}
	}
}

func TestSyncNoRemoteUsesLocalBase(t *testing.T) {
	g := testGlobal(t)
	g.DefaultBaseBranch = "master"
	fake := gitx.NewFake()
	fake.Responses["remote"] = gitx.FakeResponse{Out: ""}
	e, _, _ := newTestEngine(t, g, fake)
	attach(t, g.ProjectsDir, "feat", "repo1")

	if _, err := e.Sync(context.Background(), "feat", nil); err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(g.ProjectsDir, "feat", "repo1")
	lines := fake.CommandLines()
	if !hasLine(lines, wt+": git rebase --autostash master") {
		t.Errorf("want local-base rebase, got %v", lines)
	}
	for _, l := range lines {
		if l == filepath.Join(g.ReposDir, "repo1")+": git fetch origin" {
			t.Errorf("no fetch expected without remote: %v", lines)
		}
	}
}

func TestSyncIsolatesFailures(t *testing.T) {
	g := testGlobal(t)
	fake := gitx.NewFake()
	fake.Responses["remote"] = gitx.FakeResponse{Out: "origin"}
	fake.Responses["symbolic-ref"] = gitx.FakeResponse{Out: "origin/main"}
	fake.Responses["rebase --autostash origin/main"] = gitx.FakeResponse{Err: os.ErrPermission}
	e, _, _ := newTestEngine(t, g, fake)
	attach(t, g.ProjectsDir, "feat", "repo1")
	attach(t, g.ProjectsDir, "feat", "repo2")

	var seen []string
	results, err := e.Sync(context.Background(), "feat", func(r RepoResult) { seen = append(seen, r.Repo) })
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Err == nil || results[1].Err == nil {
		t.Errorf("both should fail here: %+v", results)
	}
	if len(seen) != 2 {
		t.Errorf("progress = %v", seen)
	}
}

func TestSyncUnknownProject(t *testing.T) {
	g := testGlobal(t)
	e, _, _ := newTestEngine(t, g, gitx.NewFake())
	results, err := e.Sync(context.Background(), "nope", nil)
	if err == nil && len(results) != 0 {
		t.Errorf("empty project should yield no results/an error, got %v, %v", results, err)
	}
}

func TestInferProject(t *testing.T) {
	cases := []struct{ cwd, want string }{
		{"/p/feat", "feat"},
		{"/p/feat/repo1", "feat"},
		{"/p/feat/repo1/src/deep", "feat"},
		{"/p", ""},
		{"/elsewhere/feat", ""},
		{"/pother/feat", ""}, // prefix but not a child of /p
	}
	for _, c := range cases {
		if got := InferProject(c.cwd, "/p"); got != c.want {
			t.Errorf("InferProject(%q) = %q, want %q", c.cwd, got, c.want)
		}
	}
}

func TestIntegrationSync(t *testing.T) {
	e, g := integrationEngine(t)
	src := makeRepo(t, g.ReposDir, "repo1", "main")
	ctx := context.Background()
	plan, _ := e.BuildPlan(ctx, "feat", []string{"repo1"})
	if res := e.Execute(ctx, plan, nil); res[0].Err != nil {
		t.Fatal(res[0].Err)
	}
	// advance main after the worktree branched
	os.WriteFile(filepath.Join(src, "new.txt"), []byte("x"), 0o644)
	git(t, src, "add", ".")
	git(t, src, "commit", "-m", "advance main")

	results, err := e.Sync(ctx, "feat", nil)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Err != nil {
		t.Fatalf("sync: %v", results[0].Err)
	}
	wt := filepath.Join(g.ProjectsDir, "feat", "repo1")
	if _, err := os.Stat(filepath.Join(wt, "new.txt")); err != nil {
		t.Error("worktree not rebased onto advanced main")
	}
}
