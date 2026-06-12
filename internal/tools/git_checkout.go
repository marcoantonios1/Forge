package tools

import "context"

type GitCheckoutResult struct {
	Branch string `json:"branch"` // branch switched to, or file path restored
	File   bool   `json:"file"`   // true if this was a file restore
}

func (r *GitCheckoutResult) Summary() string {
	if r.File {
		return "restored " + r.Branch
	}
	return "switched to " + r.Branch
}

type GitCheckoutTool struct{}

func (t *GitCheckoutTool) Name() string { return "git_checkout" }

func (t *GitCheckoutTool) Run(ctx context.Context, args map[string]any) (any, error) {
	root, _ := args["root"].(string)
	if root == "" {
		return nil, &ToolError{Tool: t.Name(), Message: "missing required arg: root"}
	}
	branch, _ := args["branch"].(string)
	if branch == "" {
		return nil, &ToolError{Tool: t.Name(), Message: "missing required arg: branch"}
	}
	file, _ := args["file"].(string)

	if file == "" {
		if _, err := runGit(ctx, root, "checkout", branch); err != nil {
			return nil, &ToolError{Tool: t.Name(), Message: "git checkout failed", Err: err}
		}
		return &GitCheckoutResult{Branch: branch, File: false}, nil
	}

	if _, err := runGit(ctx, root, "checkout", branch, "--", file); err != nil {
		return nil, &ToolError{Tool: t.Name(), Message: "git checkout -- file failed", Err: err}
	}
	return &GitCheckoutResult{Branch: file, File: true}, nil
}
