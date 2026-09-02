// Package config loads and layers TreeWork configuration: global
// (~/.config/treework/config.toml) and per-project (.treework.toml).
package config

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	projectFileName       = ".treework.toml"
	defaultBranchTemplate = "{username}/{project}"
)

// Global is the user-level config file.
type Global struct {
	ReposDir           string `toml:"repos_dir"`
	ProjectsDir        string `toml:"projects_dir"`
	Username           string `toml:"username,omitempty"`
	BranchTemplate     string `toml:"branch_template,omitempty"`
	DefaultBaseBranch  string `toml:"default_base_branch,omitempty"`
	PullBeforeWorktree *bool  `toml:"pull_before_worktree,omitempty"`
	PostCreateHook     string `toml:"post_create_hook,omitempty"`
}

// RepoOverride is a per-repo table inside a project's .treework.toml.
type RepoOverride struct {
	BaseBranch     string `toml:"base_branch,omitempty"`
	Branch         string `toml:"branch,omitempty"`
	BranchTemplate string `toml:"branch_template,omitempty"`
	PostCreateHook string `toml:"post_create_hook,omitempty"`
}

// Project is a project-level .treework.toml.
type Project struct {
	BranchTemplate string                  `toml:"branch_template,omitempty"`
	PostCreateHook string                  `toml:"post_create_hook,omitempty"`
	Repos          map[string]RepoOverride `toml:"repos,omitempty"`
}

// Resolved holds the effective settings for one (project, repo) pair.
type Resolved struct {
	ReposDir           string
	ProjectsDir        string
	Username           string
	Branch             string
	BaseBranch         string // empty means "detect repo default branch"
	PullBeforeWorktree bool
	PostCreateHook     string
}

// Repository abstracts config file access (repository pattern).
type Repository interface {
	LoadGlobal() (Global, error)
	LoadProject(projectDir string) (*Project, error) // nil, nil when absent
	WriteProject(projectDir string, p Project) error
}

// FileRepository is the disk-backed Repository.
type FileRepository struct {
	// GlobalPath overrides the default global config path (for tests).
	GlobalPath string
}

func NewFileRepository() *FileRepository { return &FileRepository{} }

// DefaultGlobalPath honors $XDG_CONFIG_HOME, falling back to ~/.config.
func DefaultGlobalPath() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "treework", "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "treework", "config.toml"), nil
}

func (r *FileRepository) globalPath() (string, error) {
	if r.GlobalPath != "" {
		return r.GlobalPath, nil
	}
	return DefaultGlobalPath()
}

func (r *FileRepository) LoadGlobal() (Global, error) {
	path, err := r.globalPath()
	if err != nil {
		return Global{}, err
	}
	var g Global
	if _, err := toml.DecodeFile(path, &g); err != nil {
		return Global{}, fmt.Errorf("%s: %w", path, err)
	}
	g.ReposDir = expandHome(g.ReposDir)
	g.ProjectsDir = expandHome(g.ProjectsDir)
	if g.ReposDir == "" {
		return Global{}, fmt.Errorf("%s: repos_dir is required", path)
	}
	if g.ProjectsDir == "" {
		return Global{}, fmt.Errorf("%s: projects_dir is required", path)
	}
	if g.Username == "" {
		g.Username = currentUsername()
	}
	return g, nil
}

// WriteGlobal writes the global config file, creating parent dirs.
func (r *FileRepository) WriteGlobal(g Global) error {
	path, err := r.globalPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := toml.NewEncoder(f).Encode(g); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func (r *FileRepository) LoadProject(projectDir string) (*Project, error) {
	path := filepath.Join(projectDir, projectFileName)
	var p Project
	if _, err := toml.DecodeFile(path, &p); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &p, nil
}

func (r *FileRepository) WriteProject(projectDir string, p Project) error {
	path := filepath.Join(projectDir, projectFileName)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := toml.NewEncoder(f).Encode(p); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// Resolve layers per-repo override > project-wide > global > defaults.
func Resolve(g Global, p *Project, projectName, repoName string) (Resolved, error) {
	res := Resolved{
		ReposDir:           g.ReposDir,
		ProjectsDir:        g.ProjectsDir,
		Username:           g.Username,
		BaseBranch:         g.DefaultBaseBranch,
		PostCreateHook:     g.PostCreateHook,
		PullBeforeWorktree: g.PullBeforeWorktree == nil || *g.PullBeforeWorktree,
	}
	if res.Username == "" {
		res.Username = currentUsername()
	}
	template := firstNonEmpty(g.BranchTemplate, defaultBranchTemplate)
	exactBranch := ""
	if p != nil {
		template = firstNonEmpty(p.BranchTemplate, template)
		res.PostCreateHook = firstNonEmpty(p.PostCreateHook, res.PostCreateHook)
		if o, ok := p.Repos[repoName]; ok {
			template = firstNonEmpty(o.BranchTemplate, template)
			res.BaseBranch = firstNonEmpty(o.BaseBranch, res.BaseBranch)
			res.PostCreateHook = firstNonEmpty(o.PostCreateHook, res.PostCreateHook)
			exactBranch = o.Branch
		}
	}
	if exactBranch != "" {
		res.Branch = exactBranch
	} else {
		res.Branch = RenderBranch(template, res.Username, projectName, repoName)
	}
	if res.Branch == "" {
		return Resolved{}, fmt.Errorf("resolved branch name is empty for repo %q", repoName)
	}
	return res, nil
}

// RenderBranch expands {username}, {project}, {repo} in a branch template.
func RenderBranch(template, username, project, repo string) string {
	r := strings.NewReplacer("{username}", username, "{project}", project, "{repo}", repo)
	return r.Replace(template)
}

var projectNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ValidateProjectName rejects names unsafe for filesystems or branch names.
func ValidateProjectName(name string) error {
	if !projectNameRe.MatchString(name) || strings.Contains(name, "..") {
		return fmt.Errorf("invalid project name %q: use letters, digits, '.', '_', '-'; must not start with '.' or '-'", name)
	}
	return nil
}

func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}

func currentUsername() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return "user"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
