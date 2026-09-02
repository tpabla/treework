package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/taranpabla/treework/internal/engine"
)

type fakeEngine struct {
	built    []string
	executed []string
	removed  []string
}

func (f *fakeEngine) BuildPlan(ctx context.Context, project string, repos []string) (engine.Plan, error) {
	f.built = append(f.built, project+":"+strings.Join(repos, ","))
	var rps []engine.RepoPlan
	for _, r := range repos {
		rps = append(rps, engine.RepoPlan{Repo: r, Branch: "u/" + project, BaseBranch: "main",
			WorktreePath: "/p/" + project + "/" + r})
	}
	return engine.Plan{Project: project, ProjectDir: "/p/" + project, Repos: rps}, nil
}

func (f *fakeEngine) Execute(ctx context.Context, plan engine.Plan, progress func(engine.RepoResult)) []engine.RepoResult {
	var out []engine.RepoResult
	for _, r := range plan.Repos {
		f.executed = append(f.executed, r.Repo)
		res := engine.RepoResult{Repo: r.Repo}
		if progress != nil {
			progress(res)
		}
		out = append(out, res)
	}
	return out
}

func (f *fakeEngine) RemoveRepo(ctx context.Context, project, repo string, force bool) error {
	f.removed = append(f.removed, project+"/"+repo)
	return nil
}

func (f *fakeEngine) RemoveProject(ctx context.Context, project string, force bool) error {
	f.removed = append(f.removed, project)
	return nil
}

func (f *fakeEngine) ProjectDir(project string) string { return "/p/" + project }

type fakeScanner struct {
	projects []string
	repos    []string
	attached map[string][]string
}

func (f *fakeScanner) Projects() ([]string, error) { return f.projects, nil }
func (f *fakeScanner) Repos() ([]string, error)    { return f.repos, nil }
func (f *fakeScanner) AttachedRepos(p string) ([]string, error) {
	return f.attached[p], nil
}

func newTestModel(t *testing.T) (Model, *fakeEngine) {
	t.Helper()
	eng := &fakeEngine{}
	sc := &fakeScanner{
		projects: []string{"feat-a", "feat-b"},
		repos:    []string{"repo1", "repo2", "repo3"},
		attached: map[string][]string{"feat-a": {"repo1"}},
	}
	m, err := NewModel(eng, sc)
	if err != nil {
		t.Fatal(err)
	}
	// size the embedded lists so they accept input
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return sized.(Model), eng
}

func key(m Model, k string) Model {
	var msg tea.Msg
	switch k {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	next, _ := m.Update(msg)
	return next.(Model)
}

func typeString(m Model, s string) Model {
	for _, r := range s {
		m = key(m, string(r))
	}
	return m
}

func TestInitialStateListsProjects(t *testing.T) {
	m, _ := newTestModel(t)
	if m.State() != StateProjects {
		t.Fatalf("state = %v", m.State())
	}
	v := m.View()
	for _, want := range []string{"feat-a", "feat-b", "new project"} {
		if !strings.Contains(strings.ToLower(v), want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
}

func TestSelectProjectShowsRepos(t *testing.T) {
	m, _ := newTestModel(t)
	m = key(m, "j")     // move from "new project" to feat-a
	m = key(m, "enter") // select feat-a
	if m.State() != StateRepos {
		t.Fatalf("state = %v, want StateRepos", m.State())
	}
	v := m.View()
	if !strings.Contains(v, "repo2") {
		t.Errorf("view missing repos:\n%s", v)
	}
	if !strings.Contains(v, "attached") {
		t.Errorf("attached repo1 should be marked:\n%s", v)
	}
}

func TestNewProjectFlow(t *testing.T) {
	m, _ := newTestModel(t)
	m = key(m, "enter") // "new project" is first
	if m.State() != StateNewName {
		t.Fatalf("state = %v, want StateNewName", m.State())
	}
	m = typeString(m, "my-feat")
	m = key(m, "enter")
	if m.State() != StateRepos {
		t.Fatalf("state = %v, want StateRepos after naming", m.State())
	}
}

func TestNewProjectRejectsInvalidName(t *testing.T) {
	m, _ := newTestModel(t)
	m = key(m, "enter")
	m = typeString(m, "bad name")
	m = key(m, "enter")
	if m.State() != StateNewName {
		t.Fatalf("invalid name should stay on input, state = %v", m.State())
	}
	if !strings.Contains(strings.ToLower(m.View()), "invalid") {
		t.Errorf("view should show validation error:\n%s", m.View())
	}
}

func TestTagAndConfirmFlow(t *testing.T) {
	m, eng := newTestModel(t)
	m = key(m, "j")
	m = key(m, "enter") // into feat-a repos; repo1 attached
	m = key(m, "j")     // repo2 (repo1 first? order repo1,repo2,repo3; cursor starts repo1)
	m = key(m, "t")     // tag repo2
	m = key(m, "j")
	m = key(m, "t") // tag repo3
	m = key(m, "enter")
	if m.State() != StateConfirm {
		t.Fatalf("state = %v, want StateConfirm", m.State())
	}
	if len(eng.built) != 1 || eng.built[0] != "feat-a:repo2,repo3" {
		t.Fatalf("built = %v", eng.built)
	}
	v := m.View()
	if !strings.Contains(v, "u/feat-a") || !strings.Contains(v, "main") {
		t.Errorf("confirm view should show branch and base:\n%s", v)
	}
}

func TestEnterOnUntaggedSelectsSingle(t *testing.T) {
	m, eng := newTestModel(t)
	m = key(m, "j")
	m = key(m, "enter") // feat-a
	m = key(m, "j")     // repo2
	m = key(m, "enter")
	if m.State() != StateConfirm {
		t.Fatalf("state = %v", m.State())
	}
	if eng.built[0] != "feat-a:repo2" {
		t.Fatalf("built = %v", eng.built)
	}
}

func TestConfirmYesExecutes(t *testing.T) {
	m, eng := newTestModel(t)
	m = key(m, "j")
	m = key(m, "enter")
	m = key(m, "j")
	m = key(m, "enter") // confirm screen for repo2
	m = key(m, "y")
	// drive the returned command to completion
	if m.State() != StateRunning && m.State() != StateDone {
		t.Fatalf("state = %v", m.State())
	}
	next, _ := m.Update(m.runPlanMsg())
	m = next.(Model)
	if m.State() != StateDone {
		t.Fatalf("state = %v, want StateDone", m.State())
	}
	if len(eng.executed) != 1 || eng.executed[0] != "repo2" {
		t.Fatalf("executed = %v", eng.executed)
	}
	if m.ProjectDir() != "/p/feat-a" {
		t.Errorf("ProjectDir = %q", m.ProjectDir())
	}
}

func TestConfirmNoGoesBack(t *testing.T) {
	m, _ := newTestModel(t)
	m = key(m, "j")
	m = key(m, "enter")
	m = key(m, "enter") // repo1 attached — cannot add; use repo2
	// enter on attached repo should do nothing
	if m.State() != StateRepos {
		t.Fatalf("attached repo should not be selectable, state = %v", m.State())
	}
	m = key(m, "j")
	m = key(m, "enter")
	m = key(m, "n")
	if m.State() != StateRepos {
		t.Fatalf("n should return to repos, state = %v", m.State())
	}
}
