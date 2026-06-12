package tools

import "context"

type GitPullResult struct {
	Remote string `json:"remote"`
	Branch string `json:"branch"`
	Output string `json:"summary"` // trimmed stdout from git pull
}

func (r *GitPullResult) Summary() string {
	return r.Remote + "/" + r.Branch + ": " + r.Output
}

type GitPullTool struct{}

func (t *GitPullTool) Name() string { return "git_pull" }

func (t *GitPullTool) Run(ctx context.Context, args map[string]any) (any, error) {
	root, _ := args["root"].(string)
	if root == "" {
		return nil, &ToolError{Tool: t.Name(), Message: "missing required arg: root"}
	}
	remote, _ := args["remote"].(string)
	if remote == "" {
		remote = "origin"
	}
	branch, _ := args["branch"].(string)

	var out string
	var err error
	if branch == "" {
		out, err = runGit(ctx, root, "pull", remote)
	} else {
		out, err = runGit(ctx, root, "pull", remote, branch)
	}
	if err != nil {
		return nil, &ToolError{Tool: t.Name(), Message: "git pull failed", Err: err}
	}

	return &GitPullResult{Remote: remote, Branch: branch, Output: out}, nil
}
