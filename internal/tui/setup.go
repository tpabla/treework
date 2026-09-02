package tui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/tpabla/treework/internal/config"
)

type setupField struct {
	label    string
	input    textinput.Model
	required bool
}

// SetupModel is the first-run wizard: prompts for repos_dir,
// projects_dir, and username, then writes the global config.
type SetupModel struct {
	fields  []setupField
	current int
	write   func(config.Global) error
	done    bool
	aborted bool
	errMsg  string
}

// NewSetupModel pre-fills defaults (~/Projects, ~/worktrees/projects, $USER).
func NewSetupModel(write func(config.Global) error) SetupModel {
	home, _ := os.UserHomeDir()
	mk := func(label, def string, required bool) setupField {
		ti := textinput.New()
		ti.SetValue(def)
		return setupField{label: label, input: ti, required: required}
	}
	m := SetupModel{
		write: write,
		fields: []setupField{
			mk("repos directory", filepath.Join(home, "Projects"), true),
			mk("projects directory", filepath.Join(home, "worktrees", "projects"), true),
			mk("username (branch prefix)", os.Getenv("USER"), false),
		},
	}
	m.fields[0].input.Focus()
	return m
}

func (m SetupModel) Init() tea.Cmd { return textinput.Blink }

// Done reports whether the config was written successfully.
func (m SetupModel) Done() bool { return m.done }

// Aborted reports whether the user quit without finishing.
func (m SetupModel) Aborted() bool { return m.aborted }

func (m SetupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok || m.done {
		return m, nil
	}
	switch key.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.aborted = true
		return m, tea.Quit
	case tea.KeyEnter:
		field := &m.fields[m.current]
		val := strings.TrimSpace(field.input.Value())
		if field.required && val == "" {
			m.errMsg = field.label + " is required"
			return m, nil
		}
		m.errMsg = ""
		if m.current < len(m.fields)-1 {
			field.input.Blur()
			m.current++
			m.fields[m.current].input.Focus()
			return m, nil
		}
		return m.finish()
	}
	var cmd tea.Cmd
	m.fields[m.current].input, cmd = m.fields[m.current].input.Update(msg)
	return m, cmd
}

func (m SetupModel) finish() (tea.Model, tea.Cmd) {
	g := config.Global{
		ReposDir:    strings.TrimSpace(m.fields[0].input.Value()),
		ProjectsDir: strings.TrimSpace(m.fields[1].input.Value()),
		Username:    strings.TrimSpace(m.fields[2].input.Value()),
	}
	if err := m.write(g); err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	m.done = true
	return m, tea.Quit
}

func (m SetupModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("treework first-run setup") + "\n\n")
	for i, f := range m.fields {
		marker := "  "
		if i == m.current && !m.done {
			marker = "> "
		}
		b.WriteString(marker + f.label + ": " + f.input.View() + "\n")
	}
	if m.errMsg != "" {
		b.WriteString(errStyle.Render(m.errMsg) + "\n")
	}
	if m.done {
		b.WriteString("config written\n")
	}
	b.WriteString(dimStyle.Render("enter accept · esc abort"))
	return b.String()
}

// RunSetup runs the wizard; returns false if the user aborted.
func RunSetup(write func(config.Global) error) (bool, error) {
	p := tea.NewProgram(NewSetupModel(write))
	final, err := p.Run()
	if err != nil {
		return false, err
	}
	return final.(SetupModel).Done(), nil
}
