package tools

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// runGit runs git -C <root> <args...> and returns trimmed stdout.
func runGit(ctx context.Context, root string, args ...string) (string, error) {
	cmdArgs := append([]string{"-C", root}, args...)
	cmd := exec.CommandContext(ctx, "git", cmdArgs...)
	out, err := cmd.Output()
	if err != nil {
		stderr := ""
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		} else if err == exec.ErrNotFound {
			stderr = "git not found: " + err.Error()
		} else {
			stderr = err.Error()
		}
		return "", &ToolError{Tool: "git", Message: stderr, Err: err}
	}
	return strings.TrimSpace(string(out)), nil
}

func isNotGitRepo(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not a git repository") ||
		strings.Contains(msg, "not inside a git repository")
}

// TODO: git_commit tool (write path) — post-MVP
// TODO: git_branch tool (create/switch branches) — post-MVP

type GitStatusTool struct{}

func (t *GitStatusTool) Name() string { return "git_status" }

type GitStatusResult struct {
	Branch     string   `json:"branch"`
	Upstream   string   `json:"upstream"`
	Ahead      int      `json:"ahead"`
	Behind     int      `json:"behind"`
	Staged     []string `json:"staged"`
	Unstaged   []string `json:"unstaged"`
	Untracked  []string `json:"untracked"`
	IsClean    bool     `json:"is_clean"`
	IsDetached bool     `json:"is_detached"`
}

func (r *GitStatusResult) Summary() string {
	if r.IsDetached {
		branch := r.Branch
		if len(branch) > 8 {
			branch = branch[:8]
		}
		return fmt.Sprintf("detached HEAD at %s, %d untracked files", branch, len(r.Untracked))
	}
	parts := []string{"on branch " + r.Branch}
	if len(r.Staged) > 0 {
		parts = append(parts, fmt.Sprintf("%d staged", len(r.Staged)))
	}
	if len(r.Unstaged) > 0 {
		parts = append(parts, fmt.Sprintf("%d unstaged", len(r.Unstaged)))
	}
	if len(r.Untracked) > 0 {
		parts = append(parts, fmt.Sprintf("%d untracked", len(r.Untracked)))
	}
	if r.IsClean {
		parts = append(parts, "clean")
	}
	return strings.Join(parts, ", ")
}

func (t *GitStatusTool) Run(ctx context.Context, args map[string]any) (any, error) {
	root, _ := args["root"].(string)
	if root == "" {
		return nil, &ToolError{Tool: t.Name(), Message: "missing required arg: root"}
	}

	out, err := runGit(ctx, root, "status", "--porcelain=v2", "--branch")
	if err != nil {
		if isNotGitRepo(err) {
			return nil, &ToolError{Tool: t.Name(), Message: "not a git repository: " + root}
		}
		return nil, &ToolError{Tool: t.Name(), Message: "git status failed", Err: err}
	}

	result := &GitStatusResult{
		Staged:    []string{},
		Unstaged:  []string{},
		Untracked: []string{},
	}

	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "# branch.head "):
			result.Branch = strings.TrimPrefix(line, "# branch.head ")
			if result.Branch == "(detached)" {
				result.IsDetached = true
			}

		case strings.HasPrefix(line, "# branch.upstream "):
			result.Upstream = strings.TrimPrefix(line, "# branch.upstream ")

		case strings.HasPrefix(line, "# branch.ab "):
			// "+<ahead> -<behind>"
			parts := strings.Fields(strings.TrimPrefix(line, "# branch.ab "))
			if len(parts) == 2 {
				result.Ahead, _ = strconv.Atoi(strings.TrimPrefix(parts[0], "+"))
				result.Behind, _ = strconv.Atoi(strings.TrimPrefix(parts[1], "-"))
			}

		case strings.HasPrefix(line, "1 ") || strings.HasPrefix(line, "2 "):
			// Ordinary / renamed changed entry: "1 XY ..."
			if len(line) < 4 {
				continue
			}
			xy := line[2:4]
			// Extract path — field 9 (0-indexed) for ordinary, field 10 for rename.
			fields := strings.Fields(line)
			path := ""
			if strings.HasPrefix(line, "1 ") && len(fields) >= 9 {
				path = fields[8]
			} else if strings.HasPrefix(line, "2 ") && len(fields) >= 10 {
				path = fields[9]
			}
			if path == "" {
				continue
			}
			if xy[0] != '.' {
				result.Staged = append(result.Staged, path)
			}
			if xy[1] != '.' {
				result.Unstaged = append(result.Unstaged, path)
			}

		case strings.HasPrefix(line, "? "):
			result.Untracked = append(result.Untracked, strings.TrimPrefix(line, "? "))
		}
	}

	result.IsClean = len(result.Staged) == 0 && len(result.Unstaged) == 0 && len(result.Untracked) == 0
	return result, nil
}
