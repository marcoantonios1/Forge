package tools

import (
	"context"
	"strconv"
	"strings"
)

type GitCommitResult struct {
	Hash    string `json:"hash"`    // short commit hash
	Message string `json:"message"`
	Files   int    `json:"files"`   // number of files committed
}

func (r *GitCommitResult) Summary() string {
	return "[" + r.Hash + "] " + r.Message
}

type GitCommitTool struct{}

func (t *GitCommitTool) Name() string { return "git_commit" }

func (t *GitCommitTool) Run(ctx context.Context, args map[string]any) (any, error) {
	root, _ := args["root"].(string)
	if root == "" {
		return nil, &ToolError{Tool: t.Name(), Message: "missing required arg: root"}
	}
	message, _ := args["message"].(string)
	if message == "" {
		return nil, &ToolError{Tool: t.Name(), Message: "missing required arg: message"}
	}

	// stage_all defaults to true when not explicitly set.
	stageAll := true
	if v, ok := args["stage_all"].(bool); ok {
		stageAll = v
	}

	// TODO: invoke pre-commit hooks before staging/committing.
	if stageAll {
		if _, err := runGit(ctx, root, "add", "-A"); err != nil {
			return nil, &ToolError{Tool: t.Name(), Message: "git add -A failed", Err: err}
		}
	}

	out, err := runGit(ctx, root, "commit", "-m", message)
	if err != nil {
		return nil, &ToolError{Tool: t.Name(), Message: "git commit failed", Err: err}
	}

	result := &GitCommitResult{Message: message}

	// Parse short hash from first output line: "[branch abc1234] message"
	firstLine := strings.SplitN(out, "\n", 2)[0]
	if open := strings.Index(firstLine, "["); open >= 0 {
		rest := firstLine[open+1:]
		if close := strings.Index(rest, "]"); close >= 0 {
			fields := strings.Fields(rest[:close])
			if len(fields) >= 2 {
				result.Hash = fields[len(fields)-1]
			}
		}
	}

	// Parse file count from lines like " 2 files changed, ..."
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "changed") {
			parts := strings.Fields(line)
			if len(parts) > 0 {
				result.Files, _ = strconv.Atoi(parts[0])
			}
			break
		}
	}

	return result, nil
}
