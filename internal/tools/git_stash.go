package tools

import (
	"context"
	"strings"
)

type GitStashResult struct {
	Action  string   `json:"action"`  // "push"|"pop"|"drop"|"list"
	Message string   `json:"message"` // stash entry message or confirmation
	Entries []string `json:"entries"` // for list action
}

func (r *GitStashResult) Summary() string {
	if r.Action == "list" {
		return strings.Join(r.Entries, "; ")
	}
	return r.Action + ": " + r.Message
}

type GitStashTool struct{}

func (t *GitStashTool) Name() string { return "git_stash" }

func (t *GitStashTool) Run(ctx context.Context, args map[string]any) (any, error) {
	root, _ := args["root"].(string)
	if root == "" {
		return nil, &ToolError{Tool: t.Name(), Message: "missing required arg: root"}
	}
	action, _ := args["action"].(string)
	if action == "" {
		return nil, &ToolError{Tool: t.Name(), Message: "missing required arg: action"}
	}

	switch action {
	case "push":
		msg, _ := args["message"].(string)
		if msg == "" {
			msg = "forge: auto-stash"
		}
		out, err := runGit(ctx, root, "stash", "push", "-m", msg)
		if err != nil {
			return nil, &ToolError{Tool: t.Name(), Message: "git stash push failed", Err: err}
		}
		return &GitStashResult{Action: action, Message: out}, nil

	case "pop":
		out, err := runGit(ctx, root, "stash", "pop")
		if err != nil {
			return nil, &ToolError{Tool: t.Name(), Message: "git stash pop failed", Err: err}
		}
		return &GitStashResult{Action: action, Message: out}, nil

	case "drop":
		out, err := runGit(ctx, root, "stash", "drop")
		if err != nil {
			return nil, &ToolError{Tool: t.Name(), Message: "git stash drop failed", Err: err}
		}
		return &GitStashResult{Action: action, Message: out}, nil

	case "list":
		out, err := runGit(ctx, root, "stash", "list")
		if err != nil {
			return nil, &ToolError{Tool: t.Name(), Message: "git stash list failed", Err: err}
		}
		var entries []string
		for _, line := range strings.Split(out, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				entries = append(entries, line)
			}
		}
		return &GitStashResult{Action: action, Entries: entries}, nil

	default:
		return nil, &ToolError{Tool: t.Name(), Message: "unknown action: " + action + " (must be push, pop, drop, or list)"}
	}
}
