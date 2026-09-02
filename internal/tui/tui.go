// Package tui is the interactive frontend: project selection, repo
// multi-select, confirmation, and execution progress. A thin adapter
// over the engine, like the CLI.
package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tpabla/treework/internal/config"
	"github.com/tpabla/treework/internal/engine"
)

// Engine is the subset of engine.Engine the TUI needs.
type Engine interface {
	BuildPlan(ctx context.Context, project string, repos []string) (engine.Plan, error)
	Execute(ctx context.Context, plan engine.Plan, progress func(engine.RepoResult)) []engine.RepoResult
	RemoveRepo(ctx context.Context, project, repo string, force bool) error
	RemoveProject(ctx context.Context, project string, force bool) error
	ProjectDir(project string) string
}

// Scanner lists filesystem state for the screens.
type Scanner interface {
	Projects() ([]string, error)
	Repos() ([]string, error)
	AttachedRepos(project string) ([]string, error)
}

// State identifies the active screen.
type State int

const (
	StateProjects State = iota
	StateNewName
	StateRepos
	StateConfirm
	StateRunning
	StateDone
)

const newProjectLabel = "+ new project"

var (
	cursorStyle = lipgloss.NewStyle().Bold(true)
	dimStyle    = lipgloss.NewStyle().Faint(true)
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	titleStyle  = lipgloss.NewStyle().Bold(true).Underline(true)
)

type resultsMsg []engine.RepoResult

// Model is the Bubble Tea model for the whole app.
type Model struct {
	eng Engine
	sc  Scanner

	state    State
	projects listView
	repos    listView

	nameInput textinput.Model
	inputErr  string

	project    string
	attached   map[string]bool
	repoNames  []string
	tagged     map[string]bool
	plan       engine.Plan
	results    []engine.RepoResult
	projectDir string
	err        error
}

// NewModel loads the project list and returns the initial model.
func NewModel(eng Engine, sc Scanner) (Model, error) {
	projects, err := sc.Projects()
	if err != nil {
		return Model{}, err
	}
	items := append([]string{newProjectLabel}, projects...)
	ti := textinput.New()
	ti.Placeholder = "project name"
	ti.Focus()
	return Model{
		eng:       eng,
		sc:        sc,
		projects:  newListView(items),
		nameInput: ti,
		tagged:    map[string]bool{},
		attached:  map[string]bool{},
	}, nil
}

func (m Model) Init() tea.Cmd { return nil }

// State returns the active screen (exported for tests).
func (m Model) State() State { return m.state }

// ProjectDir returns the selected project's directory once work is done,
// so main can print it for shell integration.
func (m Model) ProjectDir() string { return m.projectDir }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.projects.height = msg.Height - 4
		m.repos.height = msg.Height - 4
		return m, nil
	case resultsMsg:
		m.results = msg
		m.projectDir = m.plan.ProjectDir
		m.state = StateDone
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.state {
	case StateProjects:
		return m.updateProjects(msg)
	case StateNewName:
		return m.updateNewName(msg)
	case StateRepos:
		return m.updateRepos(msg)
	case StateConfirm:
		return m.updateConfirm(msg)
	case StateDone:
		if msg.String() == "q" || msg.Type == tea.KeyEnter || msg.Type == tea.KeyEsc {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m Model) updateProjects(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.projects.handleKey(msg) {
		return m, nil
	}
	switch {
	case msg.Type == tea.KeyEnter:
		idx := m.projects.selected()
		if idx < 0 {
			return m, nil
		}
		if m.projects.items[idx] == newProjectLabel {
			m.state = StateNewName
			m.inputErr = ""
			m.nameInput.SetValue("")
			return m, textinput.Blink
		}
		return m.enterProject(m.projects.items[idx])
	case msg.String() == "q" || msg.Type == tea.KeyEsc:
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) updateNewName(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		name := strings.TrimSpace(m.nameInput.Value())
		if err := config.ValidateProjectName(name); err != nil {
			m.inputErr = err.Error()
			return m, nil
		}
		return m.enterProject(name)
	case tea.KeyEsc:
		m.state = StateProjects
		return m, nil
	}
	var cmd tea.Cmd
	m.nameInput, cmd = m.nameInput.Update(msg)
	return m, cmd
}

func (m Model) enterProject(name string) (tea.Model, tea.Cmd) {
	m.project = name
	repos, err := m.sc.Repos()
	if err != nil {
		m.err = err
		m.state = StateDone
		return m, nil
	}
	attached, err := m.sc.AttachedRepos(name)
	if err != nil {
		m.err = err
		m.state = StateDone
		return m, nil
	}
	m.repoNames = repos
	m.attached = map[string]bool{}
	for _, r := range attached {
		m.attached[r] = true
	}
	m.tagged = map[string]bool{}
	m.repos = newListView(repos)
	m.state = StateRepos
	return m, nil
}

func (m Model) updateRepos(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.repos.handleKey(msg) {
		return m, nil
	}
	switch {
	case msg.String() == "t" || msg.Type == tea.KeySpace:
		if idx := m.repos.selected(); idx >= 0 {
			name := m.repoNames[idx]
			if !m.attached[name] {
				m.tagged[name] = !m.tagged[name]
			}
		}
		return m, nil
	case msg.Type == tea.KeyEnter:
		selected := m.taggedList()
		if len(selected) == 0 {
			idx := m.repos.selected()
			if idx < 0 || m.attached[m.repoNames[idx]] {
				return m, nil
			}
			selected = []string{m.repoNames[idx]}
		}
		plan, err := m.eng.BuildPlan(context.Background(), m.project, selected)
		if err != nil {
			m.err = err
			m.state = StateDone
			return m, nil
		}
		m.plan = plan
		m.state = StateConfirm
		return m, nil
	case msg.Type == tea.KeyEsc:
		m.state = StateProjects
		return m, nil
	case msg.String() == "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.String() == "y" || msg.Type == tea.KeyEnter:
		m.state = StateRunning
		return m, func() tea.Msg { return m.runPlanMsg() }
	case msg.String() == "n" || msg.Type == tea.KeyEsc:
		m.state = StateRepos
		return m, nil
	}
	return m, nil
}

// runPlanMsg executes the plan synchronously and returns the results
// message; the confirm handler wraps it in a tea.Cmd.
func (m Model) runPlanMsg() tea.Msg {
	results := m.eng.Execute(context.Background(), m.plan, nil)
	return resultsMsg(results)
}

func (m *Model) taggedList() []string {
	var out []string
	for name, on := range m.tagged {
		if on {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func (m Model) View() string {
	switch m.state {
	case StateProjects:
		return titleStyle.Render("treework: select project") + "\n" +
			m.projects.render(func(i int, sel bool) string {
				return renderRow(m.projects.items[i], sel, "")
			}) + dimStyle.Render("enter select · / search · d delete · q quit")
	case StateNewName:
		v := titleStyle.Render("new project") + "\n" + m.nameInput.View() + "\n"
		if m.inputErr != "" {
			v += errStyle.Render(m.inputErr) + "\n"
		}
		return v
	case StateRepos:
		return titleStyle.Render("project "+m.project+": select repos") + "\n" +
			m.repos.render(func(i int, sel bool) string {
				name := m.repoNames[i]
				note := ""
				switch {
				case m.attached[name]:
					note = dimStyle.Render(" (attached)")
				case m.tagged[name]:
					note = " [t]"
				}
				return renderRow(name, sel, note)
			}) + dimStyle.Render("t tag · enter confirm · / search · esc back")
	case StateConfirm:
		var b strings.Builder
		b.WriteString(titleStyle.Render("confirm: "+m.plan.Project) + "\n")
		for _, rp := range m.plan.Repos {
			fmt.Fprintf(&b, "  %s: branch %s (base %s) -> %s\n",
				rp.Repo, rp.Branch, rp.BaseBranch, rp.WorktreePath)
		}
		b.WriteString(dimStyle.Render("y confirm · n back"))
		return b.String()
	case StateRunning:
		return "creating worktrees..."
	case StateDone:
		var b strings.Builder
		if m.err != nil {
			b.WriteString(errStyle.Render("error: "+m.err.Error()) + "\n")
		}
		for _, r := range m.results {
			if r.Err != nil {
				fmt.Fprintf(&b, "%s: %s\n", r.Repo, errStyle.Render("FAILED: "+r.Err.Error()))
			} else {
				fmt.Fprintf(&b, "%s: done\n", r.Repo)
			}
		}
		b.WriteString(dimStyle.Render("q quit"))
		return b.String()
	}
	return ""
}

func renderRow(label string, selected bool, note string) string {
	prefix := "  "
	if selected {
		prefix = "> "
		return cursorStyle.Render(prefix+label) + note
	}
	return prefix + label + note
}

// Run starts the TUI and returns the final model.
func Run(eng Engine, sc Scanner) (Model, error) {
	m, err := NewModel(eng, sc)
	if err != nil {
		return Model{}, err
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return Model{}, err
	}
	return final.(Model), nil
}
