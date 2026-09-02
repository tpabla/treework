package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadGlobal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	writeFile(t, path, `
repos_dir = "~/Projects"
projects_dir = "~/worktrees/projects"
username = "taran"
pull_before_worktree = false
`)
	r := &FileRepository{GlobalPath: path}
	g, err := r.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	home, _ := os.UserHomeDir()
	if g.ReposDir != filepath.Join(home, "Projects") {
		t.Errorf("ReposDir = %q, want ~ expanded", g.ReposDir)
	}
	if g.Username != "taran" {
		t.Errorf("Username = %q", g.Username)
	}
	if g.PullBeforeWorktree == nil || *g.PullBeforeWorktree != false {
		t.Errorf("PullBeforeWorktree = %v, want false", g.PullBeforeWorktree)
	}
}

func TestLoadGlobalMissingRequired(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	writeFile(t, path, `repos_dir = "~/Projects"`)
	r := &FileRepository{GlobalPath: path}
	if _, err := r.LoadGlobal(); err == nil {
		t.Fatal("want error for missing projects_dir")
	}
}

func TestLoadGlobalMissingFile(t *testing.T) {
	r := &FileRepository{GlobalPath: filepath.Join(t.TempDir(), "nope.toml")}
	if _, err := r.LoadGlobal(); err == nil {
		t.Fatal("want error for missing config file")
	}
}

func TestLoadProjectAbsentIsNil(t *testing.T) {
	r := NewFileRepository()
	p, err := r.LoadProject(t.TempDir())
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if p != nil {
		t.Fatalf("want nil for absent .treework.toml, got %+v", p)
	}
}

func TestLoadProjectInvalidTOMLErrors(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".treework.toml"), `branch_template = [broken`)
	r := NewFileRepository()
	if _, err := r.LoadProject(dir); err == nil {
		t.Fatal("want error for invalid TOML")
	}
}

func TestWriteProjectRoundTrip(t *testing.T) {
	dir := t.TempDir()
	r := NewFileRepository()
	in := Project{
		BranchTemplate: "{username}/{project}-{repo}",
		Repos: map[string]RepoOverride{
			"repo1": {BaseBranch: "develop", PostCreateHook: "npm install"},
		},
	}
	if err := r.WriteProject(dir, in); err != nil {
		t.Fatalf("WriteProject: %v", err)
	}
	out, err := r.LoadProject(dir)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if out == nil || out.BranchTemplate != in.BranchTemplate {
		t.Fatalf("round trip mismatch: %+v", out)
	}
	if out.Repos["repo1"].BaseBranch != "develop" {
		t.Errorf("repo override lost: %+v", out.Repos)
	}
}

func globalFixture() Global {
	return Global{
		ReposDir:    "/repos",
		ProjectsDir: "/projects",
		Username:    "taran",
	}
}

func TestResolveDefaults(t *testing.T) {
	res, err := Resolve(globalFixture(), nil, "feat", "repo1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Branch != "taran/feat" {
		t.Errorf("Branch = %q, want taran/feat (default template)", res.Branch)
	}
	if !res.PullBeforeWorktree {
		t.Error("PullBeforeWorktree should default true")
	}
	if res.BaseBranch != "" {
		t.Errorf("BaseBranch = %q, want empty (detect)", res.BaseBranch)
	}
}

func TestResolveLayering(t *testing.T) {
	g := globalFixture()
	g.PostCreateHook = "global-hook"
	g.DefaultBaseBranch = "main"
	p := &Project{
		BranchTemplate: "{username}/{project}-{repo}",
		PostCreateHook: "project-hook",
		Repos: map[string]RepoOverride{
			"repo1": {BaseBranch: "develop", PostCreateHook: "repo-hook"},
			"repo2": {Branch: "exact-branch"},
		},
	}

	r1, err := Resolve(g, p, "feat", "repo1")
	if err != nil {
		t.Fatal(err)
	}
	if r1.Branch != "taran/feat-repo1" {
		t.Errorf("repo1 Branch = %q", r1.Branch)
	}
	if r1.BaseBranch != "develop" {
		t.Errorf("repo1 BaseBranch = %q, want repo override", r1.BaseBranch)
	}
	if r1.PostCreateHook != "repo-hook" {
		t.Errorf("repo1 hook = %q, want repo override", r1.PostCreateHook)
	}

	r2, _ := Resolve(g, p, "feat", "repo2")
	if r2.Branch != "exact-branch" {
		t.Errorf("repo2 Branch = %q, want exact branch to skip template", r2.Branch)
	}
	if r2.BaseBranch != "main" {
		t.Errorf("repo2 BaseBranch = %q, want global default", r2.BaseBranch)
	}
	if r2.PostCreateHook != "project-hook" {
		t.Errorf("repo2 hook = %q, want project-wide", r2.PostCreateHook)
	}

	r3, _ := Resolve(g, nil, "feat", "repo3")
	if r3.PostCreateHook != "global-hook" {
		t.Errorf("repo3 hook = %q, want global", r3.PostCreateHook)
	}
}

func TestRenderBranch(t *testing.T) {
	got := RenderBranch("{username}/{project}-{repo}", "u", "p", "r")
	if got != "u/p-r" {
		t.Errorf("RenderBranch = %q", got)
	}
}

func TestValidateProjectName(t *testing.T) {
	valid := []string{"feat", "test-feature", "fix_123", "a.b"}
	for _, v := range valid {
		if err := ValidateProjectName(v); err != nil {
			t.Errorf("ValidateProjectName(%q) = %v, want nil", v, err)
		}
	}
	invalid := []string{"", " ", "a b", "a/b", "a..b", "-lead", ".lead", "a\tb", "a~b", "a:b"}
	for _, v := range invalid {
		if err := ValidateProjectName(v); err == nil {
			t.Errorf("ValidateProjectName(%q) = nil, want error", v)
		}
	}
}
