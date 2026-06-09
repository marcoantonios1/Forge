package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/marcoantonios1/Forge/internal/costguard"
	"github.com/marcoantonios1/Forge/internal/events"
	"github.com/marcoantonios1/Forge/internal/patch"
	"github.com/marcoantonios1/Forge/internal/tools"
)

const defaultMaxIter = 30

type Config struct {
	Model     string
	MaxIter   int
	AutoApply bool
	Debug     bool
}

// PatchConfirmer abstracts user confirmation for patches.
// Autonomous mode: always returns true. Supervised/safe: prompts the user.
type PatchConfirmer interface {
	Confirm(ps *patch.PatchSet) (bool, error)
}

type Agent struct {
	cfg      Config
	client   *costguard.Client
	registry *Registry
	emitter  events.Emitter
	patcher  PatchConfirmer
}

func New(cfg Config, client *costguard.Client, registry *Registry,
	emitter events.Emitter, patcher PatchConfirmer) *Agent {
	if cfg.MaxIter <= 0 {
		cfg.MaxIter = defaultMaxIter
	}
	return &Agent{cfg: cfg, client: client, registry: registry, emitter: emitter, patcher: patcher}
}

func (a *Agent) Run(ctx context.Context, ac *AgentContext) error {
	// Step 0 — emit task.started.
	taskJSON, _ := json.Marshal(ac.Task)
	a.emitter.Emit(events.Event{
		Type:      events.EventTaskStarted,
		Timestamp: time.Now(),
		SessionID: ac.SessionID,
		Payload:   map[string]any{"session_id": ac.SessionID, "task": string(taskJSON), "root": ac.Root},
	})

	// Step 1 — gather git context (best-effort).
	a.gatherGitContext(ctx, ac)

	// Seed history with system + task messages.
	baseMessages := []costguard.Message{
		SystemMessage(ac.ProjectConfig),
		TaskMessage(ac.Task),
	}

	for {
		// TODO: context window trimming — truncate ac.History when approaching
		// model token limit (post-MVP).

		messages := append(baseMessages, ac.History...)

		resp, err := a.client.Chat(ctx, costguard.ChatRequest{
			Model:    a.cfg.Model,
			Messages: messages,
		})
		if err != nil {
			return fmt.Errorf("agent: costguard error: %w", err)
		}
		if len(resp.Choices) == 0 {
			return fmt.Errorf("agent: empty response from model")
		}

		response := strings.TrimSpace(resp.Choices[0].Message.Content)
		ac.History = append(ac.History, costguard.Message{Role: "assistant", Content: response})

		// Step 3 — parse response.
		switch {

		// a) FORGE_DONE
		case strings.HasPrefix(response, "FORGE_DONE:"):
			summary := strings.TrimSpace(strings.TrimPrefix(response, "FORGE_DONE:"))
			a.emitter.Emit(events.Event{
				Type:      events.EventTaskComplete,
				Timestamp: time.Now(),
				SessionID: ac.SessionID,
				Payload:   map[string]any{"session_id": ac.SessionID, "summary": summary, "iterations": ac.Iteration},
			})
			return nil

		// b) FORGE_FAILED
		case strings.HasPrefix(response, "FORGE_FAILED:"):
			reason := strings.TrimSpace(strings.TrimPrefix(response, "FORGE_FAILED:"))
			a.emitter.Emit(events.Event{
				Type:      events.EventTaskFailed,
				Timestamp: time.Now(),
				SessionID: ac.SessionID,
				Payload:   map[string]any{"session_id": ac.SessionID, "reason": reason, "iterations": ac.Iteration},
			})
			return fmt.Errorf("agent: task failed: %s", reason)

		// c) FORGE_PATCH_BEGIN...FORGE_PATCH_END
		case strings.Contains(response, "FORGE_PATCH_BEGIN"):
			if err := a.handlePatch(ctx, ac, response); err != nil {
				return err
			}

		// d) TOOL: / ARGS:
		case strings.Contains(response, "TOOL:"):
			call, ok := ParseToolCall(response)
			if !ok {
				ac.History = append(ac.History, userMsg(
					"Could not parse tool call. Use exactly:\nTOOL: <name>\nARGS: {\"key\": \"value\"}"))
				break
			}
			// Always force root to the repo root — the model must not control it.
			call.Args["root"] = ac.Root
			result, err := a.registry.Dispatch(ctx, call)
			ac.History = append(ac.History, ToolResultMessage(call.Name, result, err))

		// e) unrecognised
		default:
			ac.History = append(ac.History, userMsg(
				"Unrecognised response format. Emit a tool call, FORGE_PATCH_BEGIN block, FORGE_DONE, or FORGE_FAILED."))
		}

		// Step 4 — iteration limit.
		ac.Iteration++
		if ac.Iteration >= a.cfg.MaxIter {
			a.emitter.Emit(events.Event{
				Type:      events.EventTaskFailed,
				Timestamp: time.Now(),
				SessionID: ac.SessionID,
				Payload:   map[string]any{"session_id": ac.SessionID, "reason": "max iterations reached", "iterations": ac.Iteration},
			})
			return fmt.Errorf("agent: max iterations (%d) reached", a.cfg.MaxIter)
		}
	}
}

func (a *Agent) handlePatch(ctx context.Context, ac *AgentContext, response string) error {
	start := strings.Index(response, "FORGE_PATCH_BEGIN")
	end := strings.Index(response, "FORGE_PATCH_END")
	if start == -1 || end == -1 || end <= start {
		ac.History = append(ac.History, userMsg(
			"Malformed patch block. Ensure FORGE_PATCH_BEGIN and FORGE_PATCH_END are present."))
		return nil
	}
	diffText := strings.TrimSpace(response[start+len("FORGE_PATCH_BEGIN") : end])

	ps, err := patch.ParsePatchSet(ac.SessionID, ac.Task.RawInput, diffText)
	if err != nil {
		ac.History = append(ac.History, userMsg(fmt.Sprintf("Patch parse error: %v", err)))
		return nil
	}

	vr := patch.Validate(ac.Root, ps)
	if !vr.Valid {
		msgs := make([]string, len(vr.Errors))
		for i, e := range vr.Errors {
			msgs[i] = e.Path + ": " + e.Message
		}
		ac.History = append(ac.History, userMsg(
			"Patch validation failed:\n"+strings.Join(msgs, "\n")+"\nPlease fix and resubmit."))
		return nil
	}

	confirmed, err := a.patcher.Confirm(ps)
	if err != nil {
		return fmt.Errorf("agent: patch confirmation error: %w", err)
	}
	if !confirmed {
		ac.History = append(ac.History, userMsg("Patch rejected by user."))
		return nil
	}

	// Read originals before Apply so PatchHistory.Undo can restore them.
	// TODO: parallel tool execution — read/apply multiple files concurrently (post-MVP).
	originals := make(map[string][]byte)
	for _, p := range ps.Patches {
		absPath := ac.Root + "/" + p.Path
		data, readErr := os.ReadFile(absPath)
		if readErr == nil {
			originals[p.Path] = data
		}
	}

	result, err := patch.Apply(ac.Root, ps, a.emitter)
	if err != nil {
		ac.History = append(ac.History, userMsg(fmt.Sprintf("Patch apply failed (rolled back %d files): %v",
			len(result.RolledBack), err)))
		return nil
	}

	// Capture originals for history (Apply already wrote files; we store what was there before).
	// originals is populated by the applier internally; we record the record for undo.
	ac.Patches.Push(&patch.PatchRecord{
		PatchSet:  *ps,
		Originals: originals,
		AppliedAt: time.Now(),
	})

	ac.History = append(ac.History, userMsg(
		fmt.Sprintf("Patch applied successfully. %d file(s) modified: %s",
			len(result.Applied), strings.Join(result.Applied, ", "))))

	return nil
}

// gatherGitContext runs the three read-only git tools and appends a context message.
func (a *Agent) gatherGitContext(ctx context.Context, ac *AgentContext) {
	runGit := func(name string, args map[string]any) string {
		args["root"] = ac.Root
		result, err := a.registry.Dispatch(ctx, ToolCall{Name: name, Args: args})
		if err != nil {
			if a.cfg.Debug {
				fmt.Printf("[agent] git context warning (%s): %v\n", name, err)
			}
			return ""
		}
		b, _ := json.MarshalIndent(result, "", "  ")
		return string(b)
	}

	status := runGit("git_status", map[string]any{})
	diff := runGit("git_diff", map[string]any{})
	log := runGit("git_log", map[string]any{"limit": 10})

	if status != "" || diff != "" || log != "" {
		ac.History = append(ac.History, GitContextMessage(status, diff, log))
	}
}

func userMsg(content string) costguard.Message {
	return costguard.Message{Role: "user", Content: content}
}

var _ = tools.IsRecoverable
