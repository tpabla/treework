// Package engine holds TreeWork's core logic: plan what worktrees and
// branches to create, then execute the plan. Plan-then-execute keeps the
// confirmation UI and dry-run free.
package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/taranpabla/treework/internal/config"
	"github.com/taranpabla/treework/internal/gitx"
	"github.com/taranpabla/treework/internal/scan"
)

// ErrDirty is returned when a removal would discard uncommitted changes.
var ErrDirty = errors.New("worktree has uncommitted changes")

// HookRunner executes a post-create hook command inside dir.
type HookRunner func(ctx context.Context, dir, command string) error

// Engine wires core logic to its dependencies (constructor injection).
type Engine struct {
	git     gitx.Runner
	global  config.Global
	configs config.Repository
	hook    HookRunner
}

func New(git gitx.Runner, global config.Global, configs config.Repository, hook HookRunner) *Engine {
	return &Engine{git: git, global: global, configs: configs, hook: hook}
}

// RepoPlan is the resolved plan for one repo.
type RepoPlan struct {
	Repo         string
	SourceDir    string // path to the clone in repos_dir
	WorktreePath string // destination under the project dir
	Branch       string
	BaseBranch   string // resolved concrete base, e.g. "main"
	HasRemote    bool
	BranchExists bool   // reuse instead of -b
	Fetch        bool   // fetch origin before creating
	Hook         string // post-create hook, empty if none
}

// Plan is the full set of actions shown on the confirmation screen.
type Plan struct {
	Project    string
	ProjectDir string
	Repos      []RepoPlan
}

// RepoResult reports the outcome of executing one RepoPlan.
type RepoResult struct {
	Repo string
	Err  error
}

// BuildPlan resolves config layers and queries git state (default
// branch, branch existence, remotes) to produce an executable plan.
func (e *Engine) BuildPlan(ctx context.Context, project string, repos []string) (Plan, error) {
	if err := config.ValidateProjectName(project); err != nil {
		return Plan{}, err
	}
	projectDir := e.ProjectDir(project)
	projCfg, err := e.configs.LoadProject(projectDir)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{Project: project, ProjectDir: projectDir}
	for _, repo := range repos {
		res, err := config.Resolve(e.global, projCfg, project, repo)
		if err != nil {
			return Plan{}, err
		}
		src := filepath.Join(e.global.ReposDir, repo)
		hasRemote, err := e.hasRemote(ctx, src)
		if err != nil {
			return Plan{}, fmt.Errorf("repo %s: %w", repo, err)
		}
		base := res.BaseBranch
		if base == "" {
			base, err = e.defaultBranch(ctx, src, hasRemote)
			if err != nil {
				return Plan{}, fmt.Errorf("repo %s: %w", repo, err)
			}
		}
		branchExists, err := e.branchExists(ctx, src, res.Branch)
		if err != nil {
			return Plan{}, fmt.Errorf("repo %s: %w", repo, err)
		}
		plan.Repos = append(plan.Repos, RepoPlan{
			Repo:         repo,
			SourceDir:    src,
			WorktreePath: filepath.Join(projectDir, repo),
			Branch:       res.Branch,
			BaseBranch:   base,
			HasRemote:    hasRemote,
			BranchExists: branchExists,
			Fetch:        hasRemote && res.PullBeforeWorktree && !branchExists,
			Hook:         res.PostCreateHook,
		})
	}
	return plan, nil
}

// Execute runs a plan. Repos are independent: one failure does not stop
// the others. progress (optional) is called after each repo finishes.
func (e *Engine) Execute(ctx context.Context, plan Plan, progress func(RepoResult)) []RepoResult {
	var results []RepoResult
	for _, rp := range plan.Repos {
		res := RepoResult{Repo: rp.Repo, Err: e.executeRepo(ctx, plan, rp)}
		if progress != nil {
			progress(res)
		}
		results = append(results, res)
	}
	e.ensureProjectConfig(plan.ProjectDir)
	return results
}

func (e *Engine) executeRepo(ctx context.Context, plan Plan, rp RepoPlan) error {
	if err := os.MkdirAll(plan.ProjectDir, 0o755); err != nil {
		return err
	}
	if rp.Fetch {
		if _, err := e.git.Run(ctx, rp.SourceDir, "fetch", "origin"); err != nil {
			return err
		}
	}
	args := []string{"worktree", "add", rp.WorktreePath}
	if rp.BranchExists {
		args = append(args, rp.Branch)
	} else {
		start := rp.BaseBranch
		if rp.HasRemote {
			start = "origin/" + rp.BaseBranch
		}
		args = append(args, "-b", rp.Branch, start)
	}
	if _, err := e.git.Run(ctx, rp.SourceDir, args...); err != nil {
		return err
	}
	if rp.Hook != "" && e.hook != nil {
		if err := e.hook(ctx, rp.WorktreePath, rp.Hook); err != nil {
			return fmt.Errorf("post-create hook: %w", err)
		}
	}
	return nil
}

// ensureProjectConfig writes a .treework.toml so the project is
// self-describing; existing files are left untouched.
func (e *Engine) ensureProjectConfig(projectDir string) {
	if existing, err := e.configs.LoadProject(projectDir); err != nil || existing != nil {
		return
	}
	e.configs.WriteProject(projectDir, config.Project{
		BranchTemplate: e.global.BranchTemplate,
		PostCreateHook: e.global.PostCreateHook,
	})
}

// RemoveRepo removes one repo's worktree from a project. Refuses with
// ErrDirty when the worktree has uncommitted changes, unless force.
func (e *Engine) RemoveRepo(ctx context.Context, project, repo string, force bool) error {
	wt := filepath.Join(e.ProjectDir(project), repo)
	src := filepath.Join(e.global.ReposDir, repo)
	if !force {
		out, err := e.git.Run(ctx, wt, "status", "--porcelain")
		if err != nil {
			return err
		}
		if strings.TrimSpace(out) != "" {
			return fmt.Errorf("%s/%s: %w", project, repo, ErrDirty)
		}
	}
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	_, err := e.git.Run(ctx, src, append(args, wt)...)
	return err
}

// RemoveProject removes all worktrees of a project then the project
// directory. Local branches are kept; remote branches are never touched.
func (e *Engine) RemoveProject(ctx context.Context, project string, force bool) error {
	projectDir := e.ProjectDir(project)
	repos, err := scan.AttachedRepos(projectDir)
	if err != nil {
		return err
	}
	for _, repo := range repos {
		if err := e.RemoveRepo(ctx, project, repo, force); err != nil {
			return err
		}
	}
	return os.RemoveAll(projectDir)
}

// Sync rebases every attached repo's worktree onto its base branch
// (fetching first when a remote exists). Repos are independent; failures
// are collected per repo like Execute.
func (e *Engine) Sync(ctx context.Context, project string, progress func(RepoResult)) ([]RepoResult, error) {
	projectDir := e.ProjectDir(project)
	repos, err := scan.AttachedRepos(projectDir)
	if err != nil {
		return nil, err
	}
	projCfg, err := e.configs.LoadProject(projectDir)
	if err != nil {
		return nil, err
	}
	var results []RepoResult
	for _, repo := range repos {
		res := RepoResult{Repo: repo, Err: e.syncRepo(ctx, projCfg, project, repo)}
		if progress != nil {
			progress(res)
		}
		results = append(results, res)
	}
	return results, nil
}

func (e *Engine) syncRepo(ctx context.Context, projCfg *config.Project, project, repo string) error {
	res, err := config.Resolve(e.global, projCfg, project, repo)
	if err != nil {
		return err
	}
	src := filepath.Join(e.global.ReposDir, repo)
	wt := filepath.Join(e.ProjectDir(project), repo)
	hasRemote, err := e.hasRemote(ctx, src)
	if err != nil {
		return err
	}
	base := res.BaseBranch
	if base == "" {
		if base, err = e.defaultBranch(ctx, src, hasRemote); err != nil {
			return err
		}
	}
	onto := base
	if hasRemote {
		if _, err := e.git.Run(ctx, src, "fetch", "origin"); err != nil {
			return err
		}
		onto = "origin/" + base
	}
	_, err = e.git.Run(ctx, wt, "rebase", "--autostash", onto)
	return err
}

// InferProject maps a working directory inside projectsDir to its
// project name; empty when cwd is not under projectsDir.
func InferProject(cwd, projectsDir string) string {
	rel, err := filepath.Rel(projectsDir, cwd)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	return parts[0]
}

// ProjectDir returns the path of a project (for `treework open`).
func (e *Engine) ProjectDir(project string) string {
	return filepath.Join(e.global.ProjectsDir, project)
}

func (e *Engine) hasRemote(ctx context.Context, repoDir string) (bool, error) {
	out, err := e.git.Run(ctx, repoDir, "remote")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func (e *Engine) branchExists(ctx context.Context, repoDir, branch string) (bool, error) {
	out, err := e.git.Run(ctx, repoDir, "branch", "--list", branch)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// defaultBranch resolves the repo's default branch: origin/HEAD when a
// remote exists, then local main, then master.
func (e *Engine) defaultBranch(ctx context.Context, repoDir string, hasRemote bool) (string, error) {
	if hasRemote {
		out, err := e.git.Run(ctx, repoDir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
		if err == nil && out != "" {
			return strings.TrimPrefix(out, "origin/"), nil
		}
	}
	for _, candidate := range []string{"main", "master"} {
		exists, err := e.branchExists(ctx, repoDir, candidate)
		if err != nil {
			return "", err
		}
		if exists {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("cannot determine default branch (no origin/HEAD, main, or master); set default_base_branch")
}
