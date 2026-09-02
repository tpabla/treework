package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/taranpabla/treework/internal/config"
)

func setupKey(m SetupModel, k string) SetupModel {
	var msg tea.Msg
	switch k {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "backspace":
		msg = tea.KeyMsg{Type: tea.KeyBackspace}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	next, _ := m.Update(msg)
	return next.(SetupModel)
}

func setupType(m SetupModel, s string) SetupModel {
	for _, r := range s {
		m = setupKey(m, string(r))
	}
	return m
}

func clearField(m SetupModel) SetupModel {
	for i := 0; i < 100; i++ {
		m = setupKey(m, "backspace")
	}
	return m
}

func TestSetupShowsDefaults(t *testing.T) {
	m := NewSetupModel(func(config.Global) error { return nil })
	v := m.View()
	for _, want := range []string{"repos", "projects"} {
		if !strings.Contains(strings.ToLower(v), want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
}

func TestSetupWritesConfig(t *testing.T) {
	var written *config.Global
	m := NewSetupModel(func(g config.Global) error { written = &g; return nil })

	m = clearField(m)
	m = setupType(m, "/tmp/repos")
	m = setupKey(m, "enter")
	m = clearField(m)
	m = setupType(m, "/tmp/projects")
	m = setupKey(m, "enter")
	m = clearField(m)
	m = setupType(m, "someone")
	m = setupKey(m, "enter")

	if !m.Done() {
		t.Fatalf("wizard not done after three fields:\n%s", m.View())
	}
	if written == nil {
		t.Fatal("config not written")
	}
	if written.ReposDir != "/tmp/repos" || written.ProjectsDir != "/tmp/projects" || written.Username != "someone" {
		t.Errorf("written = %+v", *written)
	}
}

func TestSetupRejectsEmptyRequiredField(t *testing.T) {
	m := NewSetupModel(func(config.Global) error { return nil })
	m = clearField(m)
	m = setupKey(m, "enter") // empty repos_dir
	if m.Done() {
		t.Fatal("should not finish with empty repos_dir")
	}
	if !strings.Contains(strings.ToLower(m.View()), "required") {
		t.Errorf("view should flag required field:\n%s", m.View())
	}
}

func TestSetupEnterAcceptsDefaults(t *testing.T) {
	var written *config.Global
	m := NewSetupModel(func(g config.Global) error { written = &g; return nil })
	m = setupKey(m, "enter")
	m = setupKey(m, "enter")
	m = setupKey(m, "enter")
	if !m.Done() || written == nil {
		t.Fatal("accepting defaults should complete setup")
	}
	if written.ReposDir == "" || written.ProjectsDir == "" {
		t.Errorf("defaults not applied: %+v", *written)
	}
}

func TestWriteGlobalRoundTrip(t *testing.T) {
	path := t.TempDir() + "/config.toml"
	r := &config.FileRepository{GlobalPath: path}
	in := config.Global{ReposDir: "/r", ProjectsDir: "/p", Username: "u"}
	if err := r.WriteGlobal(in); err != nil {
		t.Fatal(err)
	}
	out, err := r.LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if out.ReposDir != "/r" || out.ProjectsDir != "/p" || out.Username != "u" {
		t.Errorf("round trip: %+v", out)
	}
}
