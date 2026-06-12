package tools

import "context"

type GitBranchResult struct {
	Branch  string `json:"branch"`
	Created bool   `json:"created"`
	Active  bool   `json:"active"` // true if checked out
}

func (r *GitBranchResult) Summary() string {
	if r.Active {
		return "created and checked out " + r.Branch
	}
	return "created " + r.Branch
}

type GitBranchTool struct{}

func (t *GitBranchTool) Name() string { return "git_branch" }

func (t *GitBranchTool) Run(ctx context.Context, args map[string]any) (any, error) {
	root, _ := args["root"].(string)
	if root == "" {
		return nil, &ToolError{Tool: t.Name(), Message: "missing required arg: root"}
	}
	name, _ := args["name"].(string)
	if name == "" {
		return nil, &ToolError{Tool: t.Name(), Message: "missing required arg: name"}
	}
	checkout, _ := args["checkout"].(bool)

	if checkout {
		if _, err := runGit(ctx, root, "checkout", "-b", name); err != nil {
			return nil, &ToolError{Tool: t.Name(), Message: "git checkout -b failed", Err: err}
		}
	} else {
		if _, err := runGit(ctx, root, "branch", name); err != nil {
			return nil, &ToolError{Tool: t.Name(), Message: "git branch failed", Err: err}
		}
	}

	return &GitBranchResult{Branch: name, Created: true, Active: checkout}, nil
}
