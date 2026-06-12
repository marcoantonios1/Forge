package tools

import "context"

type GitPushResult struct {
	Remote string `json:"remote"`
	Branch string `json:"branch"`
	Output string `json:"summary"`
}

func (r *GitPushResult) Summary() string {
	return r.Remote + "/" + r.Branch + ": " + r.Output
}

type GitPushTool struct{}

func (t *GitPushTool) Name() string { return "git_push" }

func (t *GitPushTool) Run(ctx context.Context, args map[string]any) (any, error) {
	root, _ := args["root"].(string)
	if root == "" {
		return nil, &ToolError{Tool: t.Name(), Message: "missing required arg: root"}
	}
	remote, _ := args["remote"].(string)
	if remote == "" {
		remote = "origin"
	}
	branch, _ := args["branch"].(string)

	// Resolve current branch if not provided.
	if branch == "" {
		var err error
		branch, err = runGit(ctx, root, "rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			return nil, &ToolError{Tool: t.Name(), Message: "could not determine current branch", Err: err}
		}
	}

	// set_upstream defaults to true when not explicitly set.
	setUpstream := true
	if v, ok := args["set_upstream"].(bool); ok {
		setUpstream = v
	}

	// TODO: skip push if no remote is configured (check git remote -v first).
	var gitArgs []string
	if setUpstream {
		gitArgs = []string{"push", "--set-upstream", remote, branch}
	} else {
		gitArgs = []string{"push", remote, branch}
	}

	out, err := runGit(ctx, root, gitArgs...)
	if err != nil {
		return nil, &ToolError{Tool: t.Name(), Message: "git push failed", Err: err}
	}

	return &GitPushResult{Remote: remote, Branch: branch, Output: out}, nil
}
