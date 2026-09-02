package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tpabla/treework/internal/config"
	"github.com/tpabla/treework/internal/gitx"
)

// memConfigs is an in-memory config.Repository.
type memConfigs struct {
	global   config.Global
	projects map[string]*config.Project
	written  map[string]config.Project
}

func newMemConfigs(g config.Global) *memConfigs {
	return &memConfigs{global: g, projects: map[string]*config.Project{}, written: map[string]config.Project{}}
}

func (m *memConfigs) LoadGlobal() (config.Global, error) { return m.global, nil }
func (m *memConfigs) LoadProject(dir string) (*config.Project, error) {
	return m.projects[dir], nil
}
func (m *memConfigs) WriteProject(dir string, p config.Project) error {
	m.written[dir] = p
	m.projects[dir] = &p
	return nil
}

func testGlobal(t *testing.T) config.Global {
	return config.Global{
		ReposDir:    filepath.Join(t.TempDir(), "repos"),
		ProjectsDir: filepath.Join(t.TempDir(), "projects"),
		Username:    "taran",
	}
}

func newTestEngine(t *testing.T, g config.Global, fake *gitx.Fake) (*Engine, *memConfigs, *[]string) {
	t.Helper()
	cfgs := newMemConfigs(g)
	var hooks []string
	hook := func(ctx context.Context, dir, command string) error {
		hooks = append(hooks, dir+": "+command)
		return nil
	}
	return New(fake, g, cfgs, hook), cfgs, &hooks
}

func hasLine(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}

func TestBuildPlanDefaults(t *testing.T) {
	g := testGlobal(t)
	fake := gitx.NewFake()
	fake.Responses["remote"] = gitx.FakeResponse{Out: "origin"}
	fake.Responses["symbolic-ref"] = gitx.FakeResponse{Out: "origin/main"}
	e, _, _ := newTestEngine(t, g, fake)

	plan, err := e.BuildPlan(context.Background(), "feat", []string{"repo1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.ProjectDir != filepath.Join(g.ProjectsDir, "feat") {
		t.Errorf("ProjectDir = %q", plan.ProjectDir)
	}
	rp := plan.Repos[0]
	if rp.Branch != "taran/feat" {
		t.Errorf("Branch = %q", rp.Branch)
	}
	if rp.BaseBranch != "main" {
		t.Errorf("BaseBranch = %q, want main from origin/HEAD", rp.BaseBranch)
	}
	if !rp.HasRemote || !rp.Fetch {
		t.Errorf("HasRemote/Fetch = %v/%v, want true/true", rp.HasRemote, rp.Fetch)
	}
	if rp.BranchExists {
		t.Error("BranchExists should be false when branch --list is empty")
	}
	if rp.SourceDir != filepath.Join(g.ReposDir, "repo1") {
		t.Errorf("SourceDir = %q", rp.SourceDir)
	}
	if rp.WorktreePath != filepath.Join(g.ProjectsDir, "feat", "repo1") {
		t.Errorf("WorktreePath = %q", rp.WorktreePath)
	}
}

func TestBuildPlanNoRemoteAndExistingBranch(t *testing.T) {
	g := testGlobal(t)
	g.DefaultBaseBranch = "main"
	fake := gitx.NewFake()
	fake.Responses["remote"] = gitx.FakeResponse{Out: ""}
	fake.Responses["branch --list taran/feat"] = gitx.FakeResponse{Out: "  taran/feat"}
	e, _, _ := newTestEngine(t, g, fake)

	plan, err := e.BuildPlan(context.Background(), "feat", []string{"repo1"})
	if err != nil {
		t.Fatal(err)
	}
	rp := plan.Repos[0]
	if rp.HasRemote || rp.Fetch {
		t.Errorf("no remote: HasRemote/Fetch = %v/%v", rp.HasRemote, rp.Fetch)
	}
	if !rp.BranchExists {
		t.Error("BranchExists should be true")
	}
}

func TestBuildPlanUsesProjectOverrides(t *testing.T) {
	g := testGlobal(t)
	fake := gitx.NewFake()
	fake.Responses["remote"] = gitx.FakeResponse{Out: "origin"}
	fake.Responses["symbolic-ref"] = gitx.FakeResponse{Out: "origin/main"}
	e, cfgs, _ := newTestEngine(t, g, fake)
	projDir := filepath.Join(g.ProjectsDir, "feat")
	cfgs.projects[projDir] = &config.Project{
		Repos: map[string]config.RepoOverride{
			"repo1": {BaseBranch: "develop", PostCreateHook: "make setup"},
		},
	}

	plan, err := e.BuildPlan(context.Background(), "feat", []string{"repo1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Repos[0].BaseBranch != "develop" {
		t.Errorf("BaseBranch = %q, want project override", plan.Repos[0].BaseBranch)
	}
	if plan.Repos[0].Hook != "make setup" {
		t.Errorf("Hook = %q", plan.Repos[0].Hook)
	}
}

func TestExecuteRunsExpectedGitCommands(t *testing.T) {
	g := testGlobal(t)
	fake := gitx.NewFake()
	e, cfgs, hooks := newTestEngine(t, g, fake)
	src := filepath.Join(g.ReposDir, "repo1")
	dst := filepath.Join(g.ProjectsDir, "feat", "repo1")
	plan := Plan{
		Project:    "feat",
		ProjectDir: filepath.Join(g.ProjectsDir, "feat"),
		Repos: []RepoPlan{{
			Repo: "repo1", SourceDir: src, WorktreePath: dst,
			Branch: "taran/feat", BaseBranch: "main",
			HasRemote: true, Fetch: true, Hook: "echo hi",
		}},
	}

	results := e.Execute(context.Background(), plan, nil)
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("results = %+v", results)
	}
	lines := fake.CommandLines()
	if !hasLine(lines, src+": git fetch origin") {
		t.Errorf("missing fetch, got %v", lines)
	}
	if !hasLine(lines, src+": git worktree add "+dst+" -b taran/feat origin/main") {
		t.Errorf("missing worktree add, got %v", lines)
	}
	if st, err := os.Stat(plan.ProjectDir); err != nil || !st.IsDir() {
		t.Error("project dir not created")
	}
	if len(*hooks) != 1 || !strings.HasPrefix((*hooks)[0], dst+": echo hi") {
		t.Errorf("hooks = %v", *hooks)
	}
	if _, ok := cfgs.written[plan.ProjectDir]; !ok {
		t.Error("project .treework.toml not written on creation")
	}
}

func TestExecuteReusesExistingBranchAndLocalBase(t *testing.T) {
	g := testGlobal(t)
	fake := gitx.NewFake()
	e, _, _ := newTestEngine(t, g, fake)
	src := filepath.Join(g.ReposDir, "repo1")
	dst := filepath.Join(g.ProjectsDir, "feat", "repo1")

	e.Execute(context.Background(), Plan{
		Project: "feat", ProjectDir: filepath.Join(g.ProjectsDir, "feat"),
		Repos: []RepoPlan{{
			Repo: "repo1", SourceDir: src, WorktreePath: dst,
			Branch: "taran/feat", BaseBranch: "main", BranchExists: true,
		}},
	}, nil)
	lines := fake.CommandLines()
	if !hasLine(lines, src+": git worktree add "+dst+" taran/feat") {
		t.Errorf("existing branch should be reused, got %v", lines)
	}
	for _, l := range lines {
		if strings.Contains(l, "fetch") {
			t.Errorf("no fetch expected: %v", lines)
		}
	}
}

func TestExecuteIsolatesFailures(t *testing.T) {
	g := testGlobal(t)
	fake := gitx.NewFake()
	src1 := filepath.Join(g.ReposDir, "repo1")
	fake.Responses[""] = gitx.FakeResponse{} // default ok
	fake.Responses["worktree add "+filepath.Join(g.ProjectsDir, "feat", "repo1")] =
		gitx.FakeResponse{Err: errors.New("boom")}
	e, _, _ := newTestEngine(t, g, fake)

	mk := func(repo string) RepoPlan {
		return RepoPlan{
			Repo: repo, SourceDir: filepath.Join(g.ReposDir, repo),
			WorktreePath: filepath.Join(g.ProjectsDir, "feat", repo),
			Branch:       "taran/feat", BaseBranch: "main",
		}
	}
	var seen []string
	results := e.Execute(context.Background(), Plan{
		Project: "feat", ProjectDir: filepath.Join(g.ProjectsDir, "feat"),
		Repos:   []RepoPlan{mk("repo1"), mk("repo2")},
	}, func(r RepoResult) { seen = append(seen, r.Repo) })

	if results[0].Err == nil {
		t.Error("repo1 should fail")
	}
	if results[1].Err != nil {
		t.Errorf("repo2 should succeed despite repo1: %v", results[1].Err)
	}
	if len(seen) != 2 {
		t.Errorf("progress callback: %v", seen)
	}
	_ = src1
}

func TestRemoveRepoDirtyProtection(t *testing.T) {
	g := testGlobal(t)
	fake := gitx.NewFake()
	fake.Responses["status --porcelain"] = gitx.FakeResponse{Out: " M file.go"}
	e, _, _ := newTestEngine(t, g, fake)

	err := e.RemoveRepo(context.Background(), "feat", "repo1", false)
	if !errors.Is(err, ErrDirty) {
		t.Fatalf("err = %v, want ErrDirty", err)
	}

	if err := e.RemoveRepo(context.Background(), "feat", "repo1", true); err != nil {
		t.Fatalf("forced remove: %v", err)
	}
	wt := filepath.Join(g.ProjectsDir, "feat", "repo1")
	src := filepath.Join(g.ReposDir, "repo1")
	if !hasLine(fake.CommandLines(), src+": git worktree remove --force "+wt) {
		t.Errorf("commands = %v", fake.CommandLines())
	}
}

func TestRemoveProject(t *testing.T) {
	g := testGlobal(t)
	fake := gitx.NewFake()
	e, _, _ := newTestEngine(t, g, fake)
	projDir := filepath.Join(g.ProjectsDir, "feat")
	os.MkdirAll(filepath.Join(projDir, "repo1"), 0o755)
	os.WriteFile(filepath.Join(projDir, "repo1", ".git"), []byte("gitdir: x"), 0o644)
	os.WriteFile(filepath.Join(projDir, ".treework.toml"), []byte(""), 0o644)

	if err := e.RemoveProject(context.Background(), "feat", false); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(g.ReposDir, "repo1")
	if !hasLine(fake.CommandLines(), src+": git worktree remove "+filepath.Join(projDir, "repo1")) {
		t.Errorf("commands = %v", fake.CommandLines())
	}
	if _, err := os.Stat(projDir); !os.IsNotExist(err) {
		t.Error("project dir should be removed")
	}
}

func TestProjectDir(t *testing.T) {
	g := testGlobal(t)
	e, _, _ := newTestEngine(t, g, gitx.NewFake())
	if got := e.ProjectDir("feat"); got != filepath.Join(g.ProjectsDir, "feat") {
		t.Errorf("ProjectDir = %q", got)
	}
}
