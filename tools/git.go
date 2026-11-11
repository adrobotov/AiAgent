package tools

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// GitTool provides git repository operations.
type GitTool struct {
	// repoPath is the path to the git repository root.
	// If empty, the current working directory is used.
	repoPath string

	// timeout is the maximum execution time for git commands.
	timeout time.Duration
}

// NewGitTool creates a new git tool.
// repoPath restricts operations to a specific repository (empty = cwd).
func NewGitTool(repoPath string) *GitTool {
	if repoPath == "" {
		if cwd, err := exec.Command("git", "rev-parse", "--show-toplevel").CombinedOutput(); err == nil {
			repoPath = strings.TrimSpace(string(cwd))
		}
	}
	return &GitTool{
		repoPath: repoPath,
		timeout:  30 * time.Second,
	}
}

// Name returns the tool name.
func (t *GitTool) Name() string { return "git" }

// Description returns the tool description.
func (GitTool) Description() string {
	return "Performs git operations: status, diff, log, commit, add, push, pull, branch, merge, checkout, and blame."
}

// Parameters returns the tool parameters schema.
func (GitTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"status", "diff", "log", "commit", "add", "push", "pull", "branch", "merge", "checkout", "blame", "remote"},
				"description": "The git operation to perform.",
			},
			"args": map[string]interface{}{
				"type":        "string",
				"description": "Additional arguments for the git command.",
			},
			"message": map[string]interface{}{
				"type":        "string",
				"description": "Commit message (for 'commit' action).",
			},
			"branch": map[string]interface{}{
				"type":        "string",
				"description": "Branch name (for 'checkout', 'merge', 'branch' actions).",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum number of log entries (for 'log' action, default: 20).",
			},
		},
		"required": []string{"action"},
	}
}

// Execute executes the git operation.
func (t *GitTool) Execute(argsJSON string) (string, error) {
	var args struct {
		Action  string `json:"action"`
		Args    string `json:"args"`
		Message string `json:"message"`
		Branch  string `json:"branch"`
		Limit   int    `json:"limit"`
	}
	if err := parseArgs(argsJSON, &args); err != nil {
		return "", err
	}

	action := strings.ToLower(strings.TrimSpace(args.Action))
	if action == "" {
		return "", fmt.Errorf("action is required")
	}

	switch action {
	case "status":
		return t.gitStatus(args.Args)
	case "diff":
		return t.gitDiff(args.Args)
	case "log":
		if args.Limit <= 0 {
			args.Limit = 20
		}
		return t.gitLog(args.Limit, args.Args)
	case "commit":
		if args.Message == "" {
			return "", fmt.Errorf("commit message is required")
		}
		return t.gitCommit(args.Message, args.Args)
	case "add":
		return t.gitAdd(args.Args)
	case "push":
		return t.gitPush(args.Args)
	case "pull":
		return t.gitPull(args.Args)
	case "branch":
		return t.gitBranch(args.Args, args.Branch)
	case "merge":
		return t.gitMerge(args.Branch)
	case "checkout":
		return t.gitCheckout(args.Branch)
	case "blame":
		return t.gitBlame(args.Args)
	case "remote":
		return t.gitRemote(args.Args)
	default:
		return "", fmt.Errorf("unknown action: %s. Available: status, diff, log, commit, add, push, pull, branch, merge, checkout, blame, remote", action)
	}
}

// runGit executes a git command with timeout and returns the output.
func (t *GitTool) runGit(args ...string) (string, error) {
	ctx, cancel := execCommandContext(t.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	if t.repoPath != "" {
		cmd.Dir = t.repoPath
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Trim trailing newline for cleaner output
		result := strings.TrimRight(string(output), "\n")
		if result == "" {
			result = "(no output)"
		}
		return "", fmt.Errorf("git %s failed: %w\n%s", strings.Join(args, " "), err, result)
	}

	return strings.TrimRight(string(output), "\n"), nil
}

// gitStatus returns the working tree status.
func (t *GitTool) gitStatus(extraArgs string) (string, error) {
	args := []string{"status", "--short", "--branch"}
	if extraArgs != "" {
		args = append(args, strings.Fields(extraArgs)...)
	}
	return t.runGit(args...)
}

// gitDiff returns unstaged or staged changes.
func (t *GitTool) gitDiff(extraArgs string) (string, error) {
	args := []string{"diff", "--stat", "-U3"}
	if extraArgs != "" {
		args = append(args, strings.Fields(extraArgs)...)
	}
	return t.runGit(args...)
}

// gitLog returns recent commits.
func (t *GitTool) gitLog(limit int, extraArgs string) (string, error) {
	args := []string{"log", fmt.Sprintf("--max-count=%d", limit), "--pretty=format:%h %s (%an, %ai)", "--stat"}
	if extraArgs != "" {
		args = append(args, strings.Fields(extraArgs)...)
	}
	return t.runGit(args...)
}

// gitCommit creates a new commit.
func (t *GitTool) gitCommit(message, extraArgs string) (string, error) {
	args := []string{"commit", "-m", message}
	if extraArgs != "" {
		args = append(args, strings.Fields(extraArgs)...)
	}
	return t.runGit(args...)
}

// gitAdd stages files.
func (t *GitTool) gitAdd(extraArgs string) (string, error) {
	args := []string{"add"}
	if extraArgs != "" {
		args = append(args, strings.Fields(extraArgs)...)
	} else {
		args = append(args, "-A") // add all
	}
	return t.runGit(args...)
}

// gitPush pushes to remote.
func (t *GitTool) gitPush(extraArgs string) (string, error) {
	args := []string{"push"}
	if extraArgs != "" {
		args = append(args, strings.Fields(extraArgs)...)
	}
	return t.runGit(args...)
}

// gitPull pulls from remote.
func (t *GitTool) gitPull(extraArgs string) (string, error) {
	args := []string{"pull"}
	if extraArgs != "" {
		args = append(args, strings.Fields(extraArgs)...)
	}
	return t.runGit(args...)
}

// gitBranch lists, creates, or deletes branches.
func (t *GitTool) gitBranch(extraArgs, branchName string) (string, error) {
	args := []string{"branch"}
	if branchName != "" && extraArgs == "" {
		args = append(args, branchName)
	}
	if extraArgs != "" {
		args = append(args, strings.Fields(extraArgs)...)
	}
	return t.runGit(args...)
}

// gitMerge merges a branch into the current branch.
func (t *GitTool) gitMerge(branch string) (string, error) {
	if branch == "" {
		return "", fmt.Errorf("branch name is required for merge")
	}
	return t.runGit("merge", branch)
}

// gitCheckout switches branches.
func (t *GitTool) gitCheckout(branch string) (string, error) {
	if branch == "" {
		return "", fmt.Errorf("branch name is required for checkout")
	}
	return t.runGit("checkout", branch)
}

// gitBlame shows line-by-line authorship.
func (t *GitTool) gitBlame(file string) (string, error) {
	if file == "" {
		return "", fmt.Errorf("file path is required for blame")
	}
	return t.runGit("blame", file)
}

// gitRemote manages remote repositories.
func (t *GitTool) gitRemote(extraArgs string) (string, error) {
	args := []string{"remote", "-v"}
	if extraArgs != "" {
		args = append(args, strings.Fields(extraArgs)...)
	}
	return t.runGit(args...)
}

// execCommandContext creates a context with timeout for exec commands.
func execCommandContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}
