package tools

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/marcoantonios1/Forge/internal/events"
)

const (
	defaultTimeoutSeconds = 30
	maxTimeoutSeconds     = 300
)

// bannedPrefixes is a defence-in-depth guard against obviously destructive
// commands; the system prompt is the primary guard.
var bannedPrefixes = []string{
	"rm ", "rm\t", "rmdir", "mkfs", "dd ", "dd\t",
	"sudo rm", "sudo mkfs", ":(){:|:&};:",
	"shutdown", "reboot", "halt", "poweroff",
	"mv /", "chmod -R 777 /", "chown -R",
}

type RunCommandResult struct {
	Command    string `json:"command"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExitCode   int    `json:"exit_code"`
	TimedOut   bool   `json:"timed_out"`
	DurationMs int64  `json:"duration_ms"`
}

func (r *RunCommandResult) Summary() string {
	if r.TimedOut {
		return r.Command + ": timed out"
	}
	return r.Command + ": exit " + strconv.Itoa(r.ExitCode)
}

// RunCommandTool runs build/test/lint commands in the repo root and streams
// output line-by-line via command.output events.
//
// RunCommandTool requires the emitter to stream output events.
// Construct via NewRunCommandTool, not a bare struct literal.
type RunCommandTool struct {
	emitter   events.Emitter
	sessionID string
}

func NewRunCommandTool(emitter events.Emitter, sessionID string) *RunCommandTool {
	return &RunCommandTool{emitter: emitter, sessionID: sessionID}
}

func (t *RunCommandTool) Name() string { return "run_command" }

func (t *RunCommandTool) Run(ctx context.Context, args map[string]any) (any, error) {
	command, _ := args["command"].(string)
	root, _ := args["root"].(string)
	if strings.TrimSpace(command) == "" {
		return nil, &ToolError{Tool: t.Name(), Message: "missing required arg: command"}
	}
	if root == "" {
		return nil, &ToolError{Tool: t.Name(), Message: "missing required arg: root"}
	}

	lower := strings.ToLower(strings.TrimSpace(command))
	for _, prefix := range bannedPrefixes {
		if strings.HasPrefix(lower, strings.ToLower(prefix)) {
			return nil, &ToolError{Tool: t.Name(), Message: "command rejected: destructive OS commands are not permitted"}
		}
	}

	timeout := defaultTimeoutSeconds
	switch v := args["timeout_seconds"].(type) {
	case int:
		timeout = v
	case float64:
		timeout = int(v)
	}
	if timeout <= 0 {
		timeout = defaultTimeoutSeconds
	}
	if timeout > maxTimeoutSeconds {
		timeout = maxTimeoutSeconds
	}

	// TODO: opt-in "shell": true arg would wrap the command in sh -c for
	// pipes/redirects; for now the agent must avoid shell metacharacters.
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return nil, &ToolError{Tool: t.Name(), Message: "missing required arg: command"}
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(timeoutCtx, parts[0], parts[1:]...)
	cmd.Dir = root

	t.emitter.Emit(events.CommandStartedEvent(t.sessionID, command, root))
	start := time.Now()

	stdoutPipe, _ := cmd.StdoutPipe()
	stderrPipe, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		return nil, &ToolError{Tool: t.Name(), Message: "failed to start command", Err: err}
	}

	// TODO: enforce per-command output size limits (e.g. max 64KB of stdout)
	// to avoid flooding the agent's context window.
	var wg sync.WaitGroup
	var stdoutBuf, stderrBuf strings.Builder
	var bufMu sync.Mutex

	stream := func(pipe io.Reader, streamName string) {
		defer wg.Done()
		sc := bufio.NewScanner(pipe)
		for sc.Scan() {
			line := sc.Text()
			bufMu.Lock()
			if streamName == "stdout" {
				stdoutBuf.WriteString(line + "\n")
			} else {
				stderrBuf.WriteString(line + "\n")
			}
			bufMu.Unlock()
			t.emitter.Emit(events.CommandOutputEvent(t.sessionID, command, streamName, line))
		}
	}

	wg.Add(2)
	go stream(stdoutPipe, "stdout")
	go stream(stderrPipe, "stderr")
	wg.Wait()

	err := cmd.Wait()
	exitCode := 0
	timedOut := false
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		if errors.Is(timeoutCtx.Err(), context.DeadlineExceeded) {
			timedOut = true
			exitCode = -1
		}
	}

	durationMs := time.Since(start).Milliseconds()
	t.emitter.Emit(events.CommandFinishedEvent(t.sessionID, command, exitCode, timedOut, durationMs))

	return &RunCommandResult{
		Command:    command,
		Stdout:     stdoutBuf.String(),
		Stderr:     stderrBuf.String(),
		ExitCode:   exitCode,
		TimedOut:   timedOut,
		DurationMs: durationMs,
	}, nil
}
