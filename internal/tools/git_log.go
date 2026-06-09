package tools

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	defaultLogLimit = 20
	maxLogLimit     = 200
	// NUL-delimited format for safe multi-line body parsing.
	logFormat = "--format=%x00%h%x01%an%x01%ae%x01%aI%x01%s%x01%b%x00"
)

type GitLogTool struct{}

func (t *GitLogTool) Name() string { return "git_log" }

type GitCommit struct {
	Hash    string    `json:"hash"`
	Author  string    `json:"author"`
	Email   string    `json:"email"`
	Date    time.Time `json:"date"`
	Subject string    `json:"subject"`
	Body    string    `json:"body"`
}

type GitLogResult struct {
	Commits []GitCommit `json:"commits"`
	Count   int         `json:"count"`
}

func (r *GitLogResult) Summary() string {
	return fmt.Sprintf("%d commits", r.Count)
}

func (t *GitLogTool) Run(ctx context.Context, args map[string]any) (any, error) {
	root, _ := args["root"].(string)
	if root == "" {
		return nil, &ToolError{Tool: t.Name(), Message: "missing required arg: root"}
	}

	limit := defaultLogLimit
	if v, ok := args["limit"]; ok {
		switch n := v.(type) {
		case int:
			limit = n
		case float64:
			limit = int(n)
		}
	}
	if limit <= 0 {
		limit = defaultLogLimit
	}
	if limit > maxLogLimit {
		limit = maxLogLimit
	}

	branch := "HEAD"
	if v, _ := args["branch"].(string); v != "" {
		branch = v
	}

	path, _ := args["path"].(string)

	gitArgs := []string{
		"log", branch,
		fmt.Sprintf("-n%d", limit),
		logFormat,
	}
	if path != "" {
		gitArgs = append(gitArgs, "--", path)
	}

	out, err := runGit(ctx, root, gitArgs...)
	if err != nil {
		if isNotGitRepo(err) {
			return nil, &ToolError{Tool: t.Name(), Message: "not a git repository: " + root}
		}
		return nil, &ToolError{Tool: t.Name(), Message: "git log failed", Err: err}
	}

	var commits []GitCommit
	// Records are wrapped in NUL bytes: \x00<fields>\x00
	for _, record := range strings.Split(out, "\x00") {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}
		fields := strings.SplitN(record, "\x01", 6)
		if len(fields) < 5 {
			continue
		}
		date, _ := time.Parse(time.RFC3339, strings.TrimSpace(fields[3]))
		body := ""
		if len(fields) == 6 {
			body = strings.TrimSpace(fields[5])
		}
		commits = append(commits, GitCommit{
			Hash:    strings.TrimSpace(fields[0]),
			Author:  strings.TrimSpace(fields[1]),
			Email:   strings.TrimSpace(fields[2]),
			Date:    date,
			Subject: strings.TrimSpace(fields[4]),
			Body:    body,
		})
	}
	if commits == nil {
		commits = []GitCommit{}
	}

	return &GitLogResult{Commits: commits, Count: len(commits)}, nil
}
