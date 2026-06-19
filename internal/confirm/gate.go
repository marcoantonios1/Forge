package confirm

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/marcoantonios1/Forge/internal/events"
	"github.com/marcoantonios1/Forge/internal/tools"
	"github.com/marcoantonios1/Forge/internal/ui"
)

// ErrPermissionDenied is returned when the user denies a tool call.
var ErrPermissionDenied = errors.New("permission denied by user")

// toolCategory maps each tool name to its permission category.
var toolCategory = map[string]string{
	"read_file":       "read",
	"list_files":      "read",
	"write_file":      "patch", // intercepted upstream in agent.go before Dispatch; this entry is a safety-net only
	"search_code":     "read",
	"semantic_search": "read",
	"git_status":  "git_read",
	"git_diff":    "git_read",
	"git_log":     "git_read",
	// patch is handled by SafeConfirmer; not gated here.
	"git_branch":   "git_write",
	"git_checkout": "git_write",
	"git_stash":    "git_write",
	"git_pull":     "git_write",
	"git_commit":   "git_write",
	"git_push":     "git_write",
	"run_command":  "run",
}

// allCategories is the complete set of known categories used by "all" expansion.
var allCategories = map[string]bool{
	"read":      true,
	"git_read":  true,
	"patch":     true,
	"git_write": true,
	"run":       true,
}

// ParseAllowedTools splits a comma-separated string into a set of pre-approved
// categories. "all" expands to every known category.
// Unknown category names are silently ignored.
// TODO: persist --allowed-tools choices across sessions (currently in-memory only).
func ParseAllowedTools(raw string) map[string]bool {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	result := make(map[string]bool)
	for _, part := range strings.Split(raw, ",") {
		cat := strings.TrimSpace(part)
		if cat == "all" {
			for k := range allCategories {
				result[k] = true
			}
			return result
		}
		if allCategories[cat] {
			result[cat] = true
		}
		// unknown categories silently ignored
	}
	return result
}

type categoryState int

const (
	stateUnset   categoryState = iota
	stateAllowed               // approved for all calls in this session
	// TODO: wire stateDenied to a "never" response option (block all session calls).
	stateDenied // reserved; not used in MVP
)

// Dispatcher is the interface PermissionGate forwards approved calls to.
// agent.Registry implements it.
type Dispatcher interface {
	DispatchDirect(ctx context.Context, call tools.ToolCall) (any, error)
}

// PermissionGate intercepts tool calls and prompts for approval before forwarding.
type PermissionGate struct {
	in           io.Reader
	out          io.Writer
	colour       bool
	debug        bool
	emitter      events.Emitter
	sessionID    string
	preApproved  map[string]bool
	sessionState map[string]categoryState // guarded by mu
	mu           sync.Mutex
	interactive  bool // false → forward all calls without prompting
}

func NewPermissionGate(
	in io.Reader,
	out io.Writer,
	colour bool,
	debug bool,
	emitter events.Emitter,
	sessionID string,
	preApproved map[string]bool,
	interactive bool,
) *PermissionGate {
	return &PermissionGate{
		in:           in,
		out:          out,
		colour:       colour,
		debug:        debug,
		emitter:      emitter,
		sessionID:    sessionID,
		preApproved:  preApproved,
		sessionState: make(map[string]categoryState),
		interactive:  interactive,
	}
}

// Dispatch checks permission for call.Name then either forwards to inner or
// returns ErrPermissionDenied wrapped in a *tools.ToolError.
//
// Decision order:
//  1. interactive=false → forward immediately (headless / autonomous)
//  2. category in preApproved → forward; emit granted(pre-approved)
//  3. sessionState[category]==stateAllowed → forward; emit granted(session)
//  4. Prompt: y → once, a → session, n → deny
//
// TODO: add per-tool (not per-category) grants as a finer-grained option.
func (g *PermissionGate) Dispatch(ctx context.Context, call tools.ToolCall, inner Dispatcher) (any, error) {
	// 1. Non-interactive → forward immediately, no event.
	if !g.interactive {
		return inner.DispatchDirect(ctx, call)
	}

	category, known := toolCategory[call.Name]
	if !known {
		// Unknown tool — forward; inner dispatcher handles the unknown-tool error.
		return inner.DispatchDirect(ctx, call)
	}

	// 2. Pre-approved via --allowed-tools.
	if g.preApproved[category] {
		g.emitter.Emit(events.PermissionGrantedEvent(g.sessionID, call.Name, category, "pre-approved"))
		return inner.DispatchDirect(ctx, call)
	}

	// 3. Already approved for this session via a previous "a" answer.
	g.mu.Lock()
	state := g.sessionState[category]
	g.mu.Unlock()
	if state == stateAllowed {
		g.emitter.Emit(events.PermissionGrantedEvent(g.sessionID, call.Name, category, "session"))
		return inner.DispatchDirect(ctx, call)
	}

	// 4. Prompt the user. The read runs in a goroutine so a cancelled ctx
	// (e.g. Ctrl+C during a running task) can interrupt the prompt instead
	// of blocking forever on stdin.
	reader := bufio.NewReader(g.in)
	for {
		toolLabel := ui.Colour(call.Name, ui.Cyan, g.colour)
		catLabel := ui.Colour(category, ui.Yellow, g.colour)
		fmt.Fprintf(g.out, "\n  ⚡ Tool: %s  Category: %s\n", toolLabel, catLabel)
		fmt.Fprintf(g.out, "  Allow? [y]es / [n]o / [a]ll session for %s  ", category)

		type readResult struct {
			line string
			err  error
		}
		resultCh := make(chan readResult, 1)
		go func() {
			line, err := reader.ReadString('\n')
			resultCh <- readResult{line, err}
		}()

		var line string
		select {
		case <-ctx.Done():
			fmt.Fprintln(g.out)
			g.emitter.Emit(events.PermissionDeniedEvent(g.sessionID, call.Name, category))
			return nil, ctx.Err()
		case res := <-resultCh:
			if res.err != nil {
				if errors.Is(res.err, io.EOF) {
					g.emitter.Emit(events.PermissionDeniedEvent(g.sessionID, call.Name, category))
					return nil, &tools.ToolError{Tool: call.Name, Message: "permission denied by user", Err: ErrPermissionDenied}
				}
				return nil, res.err
			}
			line = res.line
		}

		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
			g.emitter.Emit(events.PermissionGrantedEvent(g.sessionID, call.Name, category, "once"))
			return inner.DispatchDirect(ctx, call)
		case "a", "all":
			g.mu.Lock()
			g.sessionState[category] = stateAllowed
			g.mu.Unlock()
			g.emitter.Emit(events.PermissionGrantedEvent(g.sessionID, call.Name, category, "session"))
			return inner.DispatchDirect(ctx, call)
		case "n", "no", "":
			g.emitter.Emit(events.PermissionDeniedEvent(g.sessionID, call.Name, category))
			return nil, &tools.ToolError{Tool: call.Name, Message: "permission denied by user", Err: ErrPermissionDenied}
		default:
			// Re-prompt — does not consume an agent iteration.
		}
	}
}
