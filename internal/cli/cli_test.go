package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/taranpabla/treework/internal/engine"
)

type fakeEngine struct {
	plans    []string // "project:repo1,repo2"
	removed  []string
	execErr  error
	rmErr    error
	synced   []string
	syncErr  error
	projects map[string]string // project -> dir
}

func (f *fakeEngine) BuildPlan(ctx context.Context, project string, repos []string) (engine.Plan, error) {
	f.plans = append(f.plans, project+":"+strings.Join(repos, ","))
	var rps []engine.RepoPlan
	for _, r := range repos {
		rps = append(rps, engine.RepoPlan{Repo: r, Branch: "u/" + project})
	}
	return engine.Plan{Project: project, ProjectDir: "/p/" + project, Repos: rps}, nil
}

func (f *fakeEngine) Execute(ctx context.Context, plan engine.Plan, progress func(engine.RepoResult)) []engine.RepoResult {
	var out []engine.RepoResult
	for _, r := range plan.Repos {
		res := engine.RepoResult{Repo: r.Repo, Err: f.execErr}
		if progress != nil {
			progress(res)
		}
		out = append(out, res)
	}
	return out
}

func (f *fakeEngine) RemoveRepo(ctx context.Context, project, repo string, force bool) error {
	f.removed = append(f.removed, project+"/"+repo)
	return f.rmErr
}

func (f *fakeEngine) RemoveProject(ctx context.Context, project string, force bool) error {
	f.removed = append(f.removed, project)
	return f.rmErr
}

func (f *fakeEngine) ProjectDir(project string) string { return "/p/" + project }

func (f *fakeEngine) Sync(ctx context.Context, project string, progress func(engine.RepoResult)) ([]engine.RepoResult, error) {
	f.synced = append(f.synced, project)
	res := engine.RepoResult{Repo: "repo1", Err: f.syncErr}
	if progress != nil {
		progress(res)
	}
	return []engine.RepoResult{res}, nil
}

type fakeScanner struct {
	projects []string
	attached map[string][]string
}

func (f *fakeScanner) Projects() ([]string, error) { return f.projects, nil }
func (f *fakeScanner) AttachedRepos(p string) ([]string, error) {
	return f.attached[p], nil
}

func newApp() (*App, *fakeEngine, *bytes.Buffer) {
	eng := &fakeEngine{}
	out := &bytes.Buffer{}
	app := &App{
		Engine: eng,
		Scanner: &fakeScanner{
			projects: []string{"feat-a", "feat-b"},
			attached: map[string][]string{"feat-a": {"repo1"}},
		},
		Stdout: out,
		Stderr: &bytes.Buffer{},
	}
	return app, eng, out
}

func TestAdd(t *testing.T) {
	app, eng, _ := newApp()
	err := app.Run(context.Background(), []string{"add", "--project", "feat", "--repos", "repo1,repo2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(eng.plans) != 1 || eng.plans[0] != "feat:repo1,repo2" {
		t.Errorf("plans = %v", eng.plans)
	}
}

func TestAddFailurePropagates(t *testing.T) {
	app, eng, _ := newApp()
	eng.execErr = errors.New("boom")
	err := app.Run(context.Background(), []string{"add", "--project", "feat", "--repos", "repo1"})
	if err == nil {
		t.Fatal("want error when a repo fails")
	}
}

func TestAddRequiresFlags(t *testing.T) {
	app, _, _ := newApp()
	if err := app.Run(context.Background(), []string{"add", "--project", "feat"}); err == nil {
		t.Error("want error when --repos missing")
	}
	if err := app.Run(context.Background(), []string{"add", "--repos", "r"}); err == nil {
		t.Error("want error when --project missing")
	}
}

func TestRmProjectAndRepo(t *testing.T) {
	app, eng, _ := newApp()
	if err := app.Run(context.Background(), []string{"rm", "--project", "feat"}); err != nil {
		t.Fatal(err)
	}
	if err := app.Run(context.Background(), []string{"rm", "--project", "feat", "--repos", "repo1"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"feat", "feat/repo1"}
	if strings.Join(eng.removed, " ") != strings.Join(want, " ") {
		t.Errorf("removed = %v, want %v", eng.removed, want)
	}
}

func TestList(t *testing.T) {
	app, _, out := newApp()
	if err := app.Run(context.Background(), []string{"list"}); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "feat-a") || !strings.Contains(s, "feat-b") {
		t.Errorf("list output: %q", s)
	}
}

func TestListProjectShowsRepos(t *testing.T) {
	app, _, out := newApp()
	if err := app.Run(context.Background(), []string{"list", "--project", "feat-a"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "repo1") {
		t.Errorf("output: %q", out.String())
	}
}

func TestOpenPrintsPath(t *testing.T) {
	app, _, out := newApp()
	if err := app.Run(context.Background(), []string{"open", "feat-a"}); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "/p/feat-a" {
		t.Errorf("output: %q", out.String())
	}
}

func TestOpenEdit(t *testing.T) {
	app, _, _ := newApp()
	var opened string
	app.Editor = func(dir string) error { opened = dir; return nil }
	if err := app.Run(context.Background(), []string{"open", "feat-a", "--edit"}); err != nil {
		t.Fatal(err)
	}
	if opened != "/p/feat-a" {
		t.Errorf("opened = %q", opened)
	}
}

func TestOpenUnknownProjectErrors(t *testing.T) {
	app, _, _ := newApp()
	if err := app.Run(context.Background(), []string{"open", "nope"}); err == nil {
		t.Fatal("want error for unknown project")
	}
}

func TestSyncWithProjectFlag(t *testing.T) {
	app, eng, out := newApp()
	if err := app.Run(context.Background(), []string{"sync", "--project", "feat-a"}); err != nil {
		t.Fatal(err)
	}
	if len(eng.synced) != 1 || eng.synced[0] != "feat-a" {
		t.Errorf("synced = %v", eng.synced)
	}
	if !strings.Contains(out.String(), "rebased") {
		t.Errorf("output: %q", out.String())
	}
}

func TestSyncInfersProjectFromCwd(t *testing.T) {
	app, eng, _ := newApp()
	app.CwdProject = func() string { return "feat-b" }
	if err := app.Run(context.Background(), []string{"sync"}); err != nil {
		t.Fatal(err)
	}
	if len(eng.synced) != 1 || eng.synced[0] != "feat-b" {
		t.Errorf("synced = %v", eng.synced)
	}
}

func TestSyncOutsideProjectWithoutFlagErrors(t *testing.T) {
	app, eng, _ := newApp()
	app.CwdProject = func() string { return "" }
	if err := app.Run(context.Background(), []string{"sync"}); err == nil {
		t.Fatal("want error")
	}
	if len(eng.synced) != 0 {
		t.Errorf("should not sync: %v", eng.synced)
	}
}

func TestSyncFailurePropagates(t *testing.T) {
	app, eng, _ := newApp()
	eng.syncErr = errors.New("conflict")
	if err := app.Run(context.Background(), []string{"sync", "--project", "feat-a"}); err == nil {
		t.Fatal("want error when a repo fails")
	}
}

func TestUnknownCommand(t *testing.T) {
	app, _, _ := newApp()
	if err := app.Run(context.Background(), []string{"wat"}); err == nil {
		t.Fatal("want error")
	}
}
