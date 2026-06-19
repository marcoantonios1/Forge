package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/marcoantonios1/Forge/internal/compiler"
	"github.com/marcoantonios1/Forge/internal/costguard"
	"github.com/marcoantonios1/Forge/internal/events"
	"github.com/marcoantonios1/Forge/internal/patch"
	"github.com/marcoantonios1/Forge/internal/reposummary"
	"github.com/marcoantonios1/Forge/internal/tools"
)

const (
	defaultMaxIter    = 100
	defaultStuckAfter = 3 // minimum iterations before stuck check fires
)

type ModelRole string

const (
	RolePlanner    ModelRole = "planner"
	RoleCoder      ModelRole = "coder"
	RoleToolCaller ModelRole = "tool_caller"
	RoleCompactor  ModelRole = "compactor"
	RoleReviewer   ModelRole = "reviewer"
)

// ModelLimits mirrors config.ModelLimits. Duplicated here so agent does not
// import internal/config (consistent with how model names are already passed
// as plain strings from main.go rather than as a config.Config reference).
type ModelLimits struct {
	CompilerMaxTokens   int
	PlannerMaxTokens    int
	CoderMaxTokens      int
	ToolCallerMaxTokens int
	CompactorMaxTokens  int
	EmbeddingMaxTokens  int
}

type Config struct {
	PlannerModel          string
	CoderModel            string
	ToolCallerModel       string // empty = disabled, planner emits TOOL:/ARGS: directly
	CompactorModel        string
	ReviewerModel         string // empty string = review disabled (explicit opt-out)
	ReviewerExplicitlyOff bool   // true when FORGE_REVIEWER_MODEL="" was explicitly set
	EmbeddingModel        string
	Limits                ModelLimits
	MaxIter               int
	AutoApply             bool
	Debug                 bool
	StuckAfterIterations  int // minimum iterations before stuck check; default 3
	// TODO: expose stuck-window sizes (tool call/result/response history
	// lengths) as additional Config fields for fine-grained tuning.
	// TODO: wire CompilerMaxTokens into compiler.Compile() if/when its calls
	// need budget-aware truncation; currently compiler inputs are short
	// enough in practice that this has not been needed.
	// TODO: wire EmbeddingMaxTokens into embeddings.Build()/Search() chunk
	// sizing (internal/embeddings/chunk.go's maxChunkChars) for closer
	// alignment between token budget and character-based chunking.
}

// limitForRole returns the configured token budget for a role.
func (c Config) limitForRole(role ModelRole) int {
	switch role {
	case RoleCoder:
		return c.Limits.CoderMaxTokens
	case RoleToolCaller:
		return c.Limits.ToolCallerMaxTokens
	case RoleCompactor:
		return c.Limits.CompactorMaxTokens
	case RoleReviewer:
		return c.Limits.PlannerMaxTokens // reviewer shares the planner's budget
	default:
		return c.Limits.PlannerMaxTokens
	}
}

// selectModel returns the model string for the given role, falling back to
// PlannerModel for any role left unconfigured. ToolCaller has no fallback
// value returned here — callers must check ToolCallerEnabled() first.
func (c Config) selectModel(role ModelRole) string {
	switch role {
	case RoleCoder:
		if c.CoderModel != "" {
			return c.CoderModel
		}
	case RoleToolCaller:
		if c.ToolCallerModel != "" {
			return c.ToolCallerModel
		}
	case RoleCompactor:
		if c.CompactorModel != "" {
			return c.CompactorModel
		}
	case RoleReviewer:
		if c.ReviewerModel != "" {
			return c.ReviewerModel
		}
	}
	return c.PlannerModel
}

// ToolCallerEnabled reports whether a distinct tool-caller model is configured.
func (c Config) ToolCallerEnabled() bool {
	return strings.TrimSpace(c.ToolCallerModel) != ""
}

// ReviewEnabled reports whether patch review is active. Disabled only when the
// user explicitly set FORGE_REVIEWER_MODEL="" — an unset env var still means
// "review with the planner model" (the default), not "no review."
func (c Config) ReviewEnabled() bool {
	return !c.ReviewerExplicitlyOff
}

// PatchConfirmer abstracts user confirmation for patches.
// Autonomous mode: always returns true. Supervised/safe: prompts the user.
type PatchConfirmer interface {
	Confirm(ctx context.Context, ps *patch.PatchSet) (bool, error)
}

type Agent struct {
	cfg        Config
	client     *costguard.Client
	registry   *Registry
	emitter    events.Emitter
	patcher    PatchConfirmer
	comp       *compiler.Compiler // nil = clarification disabled (headless)
	clarifyIn  io.Reader          // nil = clarification disabled
	clarifyOut io.Writer          // nil = clarification disabled
}

func New(
	cfg        Config,
	client     *costguard.Client,
	registry   *Registry,
	emitter    events.Emitter,
	patcher    PatchConfirmer,
	comp       *compiler.Compiler,
	clarifyIn  io.Reader,
	clarifyOut io.Writer,
) *Agent {
	if cfg.MaxIter <= 0 {
		cfg.MaxIter = defaultMaxIter
	}
	if cfg.StuckAfterIterations <= 0 {
		cfg.StuckAfterIterations = defaultStuckAfter
	}
	return &Agent{
		cfg:        cfg,
		client:     client,
		registry:   registry,
		emitter:    emitter,
		patcher:    patcher,
		comp:       comp,
		clarifyIn:  clarifyIn,
		clarifyOut: clarifyOut,
	}
}

// stuckState tracks a rolling window of recent agent activity to detect
// loops the backstop iteration limit would otherwise take much longer to catch.
type stuckState struct {
	recentToolCalls   []string // last 3 tool calls as "toolname:normalized_args_json"
	recentResponses   []string // last 2 model responses (trimmed)
	recentToolResults []string // last 3 tool results as "toolname:result_json"
}

func newStuckState() *stuckState {
	return &stuckState{}
}

// recordToolCall appends a normalized tool call fingerprint.
// Normalisation: marshal args without the "root" key (always injected, never meaningful).
func (s *stuckState) recordToolCall(name string, args map[string]any) {
	normalized := make(map[string]any, len(args))
	for k, v := range args {
		if k != "root" {
			normalized[k] = v
		}
	}
	b, _ := json.Marshal(normalized)
	fp := name + ":" + string(b)
	s.recentToolCalls = append(s.recentToolCalls, fp)
	if len(s.recentToolCalls) > 3 {
		s.recentToolCalls = s.recentToolCalls[len(s.recentToolCalls)-3:]
	}
}

// recordResponse appends the trimmed model response.
func (s *stuckState) recordResponse(response string) {
	s.recentResponses = append(s.recentResponses, strings.TrimSpace(response))
	if len(s.recentResponses) > 2 {
		s.recentResponses = s.recentResponses[len(s.recentResponses)-2:]
	}
}

// recordToolResult appends a tool result fingerprint.
func (s *stuckState) recordToolResult(name string, result any) {
	b, _ := json.Marshal(result)
	fp := name + ":" + string(b)
	s.recentToolResults = append(s.recentToolResults, fp)
	if len(s.recentToolResults) > 3 {
		s.recentToolResults = s.recentToolResults[len(s.recentToolResults)-3:]
	}
}

// isStuck returns a non-empty reason string if stuck, or "" if not.
// Only evaluates after minIter iterations have passed.
func (s *stuckState) isStuck(iteration, minIter int) string {
	if iteration < minIter {
		return ""
	}

	// Condition 1: last 3 tool calls are identical.
	if len(s.recentToolCalls) == 3 &&
		s.recentToolCalls[0] == s.recentToolCalls[1] &&
		s.recentToolCalls[1] == s.recentToolCalls[2] {
		return "repeated identical tool call: " + s.recentToolCalls[0]
	}

	// Condition 2: last 2 model responses are identical.
	if len(s.recentResponses) == 2 &&
		s.recentResponses[0] == s.recentResponses[1] {
		return "repeated identical model response"
	}

	// Condition 3: same tool returned the same result 3 times in a row.
	if len(s.recentToolResults) == 3 &&
		s.recentToolResults[0] == s.recentToolResults[1] &&
		s.recentToolResults[1] == s.recentToolResults[2] {
		return "tool returning identical result 3 times: " + s.recentToolResults[0][:min(60, len(s.recentToolResults[0]))]
	}

	return ""
}

// estimateTokens gives a cheap, deterministic token estimate for a slice of
// messages: total character count divided by 4 (no tokenizer dependency).
// TODO: replace with a BPE-based estimate for better accuracy when a
// lightweight tokenizer is available without adding external dependencies.
func estimateTokens(messages []costguard.Message) int {
	total := 0
	for _, m := range messages {
		total += len(m.Content)
	}
	return total / 4
}

const keepTurns = 10

const compactorSystemPrompt = "Summarize the following conversation history " +
	"in 2-3 sentences, preserving key decisions and file paths."

// trimHistory checks whether messages for the given role would exceed its
// token budget. If so, it keeps the last keepTurns entries of ac.History,
// summarises everything older via the compactor model, and replaces ac.History
// in place. Returns the rebuilt full message slice ready to send.
//
// Never returns an error — compactor failures fall back to a hard truncation
// with a placeholder summary (logged in debug mode only).
//
// TODO: trigger compaction proactively at 80% of budget rather than only when
// already over limit, to avoid last-second compaction on very long sessions.
func (a *Agent) trimHistory(
	ctx context.Context,
	ac *AgentContext,
	baseMessages []costguard.Message,
	role ModelRole,
) []costguard.Message {
	limit := a.cfg.limitForRole(role)
	messages := append(append([]costguard.Message{}, baseMessages...), ac.History...)

	estimated := estimateTokens(messages)
	if a.cfg.Debug {
		fmt.Fprintf(os.Stderr, "[context] %s: ~%d / %d tokens\n", role, estimated, limit)
	}
	if estimated <= limit {
		return messages
	}

	if len(ac.History) <= keepTurns {
		// Nothing meaningful to trim — return as-is. The model may reject the
		// payload but we cannot reduce further without dropping base messages.
		return messages
	}

	toSummarize := ac.History[:len(ac.History)-keepTurns]
	kept := ac.History[len(ac.History)-keepTurns:]

	summary := a.summarizeHistory(ctx, ac, toSummarize)

	newHistory := make([]costguard.Message, 0, len(kept)+1)
	newHistory = append(newHistory, costguard.Message{
		Role:    "user",
		Content: "Summary of earlier conversation (older turns were truncated to fit context):\n" + summary,
	})
	newHistory = append(newHistory, kept...)
	ac.History = newHistory

	rebuilt := append(append([]costguard.Message{}, baseMessages...), ac.History...)
	if a.cfg.Debug {
		fmt.Fprintf(os.Stderr, "[context] %s: trimmed to ~%d tokens after compaction\n",
			role, estimateTokens(rebuilt))
	}
	return rebuilt
}

// summarizeHistory asks the compactor model to summarise a slice of history
// messages. If the slice itself would exceed the compactor's own token budget,
// it is split into chunks that are each summarised independently.
//
// Never returns an error — Costguard failures produce a placeholder string.
func (a *Agent) summarizeHistory(ctx context.Context, ac *AgentContext, history []costguard.Message) string {
	compactorModel := a.cfg.selectModel(RoleCompactor)
	compactorLimit := a.cfg.limitForRole(RoleCompactor)

	chunks := chunkMessages(history, compactorLimit)
	summaries := make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		a.logCall(ac, RoleCompactor, compactorModel)
		resp, err := a.client.Chat(ctx, costguard.ChatRequest{
			Model: compactorModel,
			Messages: append([]costguard.Message{
				{Role: "system", Content: compactorSystemPrompt},
			}, chunk...),
		})
		if err != nil || len(resp.Choices) == 0 {
			if a.cfg.Debug {
				fmt.Fprintf(os.Stderr, "[context] compactor chunk %d/%d failed: %v\n", i+1, len(chunks), err)
			}
			summaries = append(summaries, fmt.Sprintf("[chunk %d/%d: summary unavailable]", i+1, len(chunks)))
			continue
		}
		summaries = append(summaries, strings.TrimSpace(resp.Choices[0].Message.Content))
	}
	return strings.Join(summaries, " ")
}

// chunkMessages splits history into groups whose estimated token count stays
// under limit (with 25% headroom reserved for the system prompt). A single
// message that alone exceeds the budget forms its own chunk rather than being
// dropped or truncated.
func chunkMessages(history []costguard.Message, limit int) [][]costguard.Message {
	if len(history) == 0 {
		return nil
	}
	budget := limit * 3 / 4
	var chunks [][]costguard.Message
	var current []costguard.Message
	currentTokens := 0

	for _, m := range history {
		msgTokens := len(m.Content) / 4
		if currentTokens+msgTokens > budget && len(current) > 0 {
			chunks = append(chunks, current)
			current = nil
			currentTokens = 0
		}
		current = append(current, m)
		currentTokens += msgTokens
	}
	if len(current) > 0 {
		chunks = append(chunks, current)
	}
	return chunks
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

	// Step 0.5 — supervised clarification (before any tool calls).
	if a.comp != nil && a.clarifyIn != nil {
		if refined := a.clarify(ctx, ac); refined != ac.Task {
			ac.Task = refined
		}
	}

	// Step 1a — stash dirty working tree and pull latest (best-effort).
	a.prepareRepo(ctx, ac)

	// Step 1b — gather git context (best-effort).
	a.gatherGitContext(ctx, ac)

	// Step 1c — repo summary for repo-wide/module tasks (skip for file-specific).
	repoSummary := a.generateRepoSummary(ctx, ac)

	// Seed history with system + task messages.
	baseMessages := []costguard.Message{
		SystemMessage(ac.ProjectConfig, ac.Memory),
	}
	if repoSummary != "" {
		baseMessages = append(baseMessages, RepoSummaryMessage(repoSummary))
	}
	baseMessages = append(baseMessages, TaskMessage(ac.Task))
	if a.cfg.Debug && ac.Memory != nil {
		if block := ac.Memory.Inject(); block != "" {
			fmt.Fprintf(os.Stderr, "[memory] injected %d bytes of session history\n", len(block))
		}
	}

	stuck := newStuckState()
	useCoderNext := false // set after a code-intent resolves; flipped to RoleCoder on next iteration

	for {
		// TODO: context window trimming — truncate ac.History when approaching
		// model token limit (post-MVP).
		// TODO: wire RoleCompactor into context-window trimming when implemented
		// (see TODO above about ac.History truncation).

		// Step 2.5 — stuck detection (runs before the Costguard call).
		if reason := stuck.isStuck(ac.Iteration, a.cfg.StuckAfterIterations); reason != "" {
			stuckMsg := "agent stuck in loop: " + reason
			a.emitter.Emit(events.Event{
				Type:      events.EventTaskFailed,
				Timestamp: time.Now(),
				SessionID: ac.SessionID,
				Payload: map[string]any{
					"session_id": ac.SessionID,
					"reason":     stuckMsg,
					"iterations": ac.Iteration,
				},
			})
			return fmt.Errorf("agent: %s", stuckMsg)
		}

		plannerRole := RolePlanner
		if useCoderNext {
			plannerRole = RoleCoder
			useCoderNext = false
		}
		messages := a.trimHistory(ctx, ac, baseMessages, plannerRole)
		plannerModel := a.cfg.selectModel(plannerRole)
		a.logCall(ac, plannerRole, plannerModel)

		resp, err := a.client.Chat(ctx, costguard.ChatRequest{
			Model:    plannerModel,
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
		stuck.recordResponse(response)

		// If the planner emitted an intent and the tool caller is enabled,
		// resolve it into a TOOL:/ARGS: call via a second Costguard round trip.
		if strings.HasPrefix(response, "INTENT:") {
			intent := strings.TrimSpace(strings.TrimPrefix(response, "INTENT:"))
			if a.cfg.ToolCallerEnabled() {
				resolved, tcErr := a.resolveIntent(ctx, ac, intent)
				if tcErr != nil {
					// Tool-caller failure is non-fatal — feed it back to the planner.
					// TODO: tool-caller failures could fall back to direct TOOL:/ARGS:
					// emission within the same iteration rather than costing a full
					// extra loop iteration.
					ac.History = append(ac.History, userMsg(
						"Tool caller error: "+tcErr.Error()+". Try restating your intent or emit TOOL:/ARGS: directly."))
					if err := a.advanceIteration(ac); err != nil {
						return err
					}
					continue
				}
				// TODO: replace intentSuggestsCode heuristic with an explicit signal
				// from the planner (e.g. "INTENT_TYPE: code" vs "INTENT_TYPE: read")
				// rather than substring matching.
				useCoderNext = intentSuggestsCode(intent)
				response = resolved
				ac.History = append(ac.History, costguard.Message{Role: "assistant", Content: response})
			}
			// If tool caller is disabled, INTENT: is unrecognised — falls through to
			// the default branch below, which re-prompts the planner to emit TOOL:/ARGS:
			// directly, preserving single-model backwards compatibility.
		}

		// Step 3 — parse response.
		switch {

		// a) FORGE_DONE
		case strings.HasPrefix(response, "FORGE_DONE:"):
			summary := strings.TrimSpace(strings.TrimPrefix(response, "FORGE_DONE:"))
			ac.LastSummary = summary
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
			// TODO: reset stuck state after a successful patch application —
			// applying a patch counts as genuine progress, not a loop.
			if err := a.handlePatch(ctx, ac, response); err != nil {
				return err
			}

		// d) TOOL: / ARGS:
		case strings.Contains(response, "TOOL:"):
			call, ok := ParseToolCall(response)
			if !ok {
				retryRole := RolePlanner
				if a.cfg.ToolCallerEnabled() {
					retryRole = RoleToolCaller
				}
				call, ok = a.retryToolCallFormat(ctx, ac, retryRole, response)
				if !ok {
					// TODO: log "[<role>] retry also malformed, falling back" in debug
					// mode as a distinct line to distinguish the retry failure from the
					// original parse failure.
					ac.History = append(ac.History, userMsg(
						"Could not parse tool call. Use exactly:\nTOOL: <name>\nARGS: {\"key\": \"value\"}"))
					break
				}
				// Retry succeeded — proceed as the happy path below.
			}
			// Always force root to the repo root — the model must not control it.
			call.Args["root"] = ac.Root
			// Record for stuck detection. Only the successfully-parsed call is
			// fingerprinted — the malformed attempt that triggered the retry is
			// never recorded, so it cannot cause a false stuck signal.
			stuck.recordToolCall(call.Name, call.Args)
			result, err := a.registry.Dispatch(ctx, call)
			stuck.recordToolResult(call.Name, result)
			ac.History = append(ac.History, ToolResultMessage(call.Name, result, err))

		// e) unrecognised
		default:
			ac.History = append(ac.History, userMsg(
				"Unrecognised response format. Emit a tool call, FORGE_PATCH_BEGIN block, FORGE_DONE, or FORGE_FAILED."))
		}

		// Step 4 — backstop iteration limit (stuck detector fires first in practice).
		if err := a.advanceIteration(ac); err != nil {
			return err
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

	confirmed, err := a.patcher.Confirm(ctx, ps)
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
		if p.IsNew {
			originals[p.Path] = nil
			continue
		}
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
		result, err := a.registry.DispatchDirect(ctx, ToolCall{Name: name, Args: args})
		if err != nil {
			if a.cfg.Debug {
				fmt.Printf("[agent] git context warning (%s): %v\n", name, err)
			}
			return ""
		}
		b, _ := json.MarshalIndent(result, "", "  ")
		return string(b)
	}

	const maxGitDiffBytes = 8 * 1024

	status := runGit("git_status", map[string]any{})
	diff := runGit("git_diff", map[string]any{})
	if len(diff) > maxGitDiffBytes {
		diff = diff[:maxGitDiffBytes] + "\n... diff truncated at 8KB for context budget ..."
	}
	log := runGit("git_log", map[string]any{"limit": 10})

	if status != "" || diff != "" || log != "" {
		ac.History = append(ac.History, GitContextMessage(status, diff, log))
	}
}

// clarify makes a single pre-loop LLM call to check whether the agent wants to
// ask a clarifying question for a supervised task. If it does, the question is
// shown to the user, their answer is re-compiled, and the refined task is returned.
//
// Returns ac.Task unchanged when:
//   - policy is not supervised, or ClarificationAsked is already true
//   - the LLM does not emit FORGE_CLARIFY
//   - the user presses Enter with no input
//   - re-compilation fails or is rejected
//
// TODO: extend to multi-turn clarification (up to N rounds) when needed.
// TODO: surface the clarification answer in the git commit message.
func (a *Agent) clarify(ctx context.Context, ac *AgentContext) *compiler.Task {
	ac.ClarificationAsked = true // always set — prevents any second round

	if ac.Task.ExecutionPolicy != compiler.PolicySupervised {
		return ac.Task
	}

	// Guard: don't proceed if context is already cancelled.
	if ctx.Err() != nil {
		return ac.Task
	}

	// One-shot LLM call to check for FORGE_CLARIFY.
	plannerModel := a.cfg.selectModel(RolePlanner)
	a.logCall(ac, RolePlanner, plannerModel)
	clarifyMsgs := []costguard.Message{
		SystemMessage(ac.ProjectConfig, ac.Memory),
		TaskMessage(ac.Task),
		{Role: "user", Content: "Should you ask a clarifying question before starting? " +
			"If yes, emit FORGE_CLARIFY: <one specific question>. " +
			"If the task is clear enough to proceed, emit FORGE_DONE: ready."},
	}
	if a.cfg.Debug {
		fmt.Fprintf(os.Stderr, "[context] planner: ~%d / %d tokens\n",
			estimateTokens(clarifyMsgs), a.cfg.limitForRole(RolePlanner))
	}
	resp, err := a.client.Chat(ctx, costguard.ChatRequest{
		Model:    plannerModel,
		Messages: clarifyMsgs,
	})
	if err != nil || len(resp.Choices) == 0 {
		return ac.Task
	}

	response := strings.TrimSpace(resp.Choices[0].Message.Content)
	if !strings.HasPrefix(response, "FORGE_CLARIFY:") {
		return ac.Task
	}

	question := strings.TrimSpace(strings.TrimPrefix(response, "FORGE_CLARIFY:"))
	if question == "" {
		return ac.Task
	}

	a.emitter.Emit(events.ClarificationAskedEvent(ac.SessionID, question, ac.Task.RawInput))
	fmt.Fprint(a.clarifyOut, "  clarify> ")

	// Guard: check cancellation before blocking on user input.
	if ctx.Err() != nil {
		return ac.Task
	}

	// Read the answer in a goroutine so a cancelled ctx (Ctrl+C) interrupts
	// the prompt instead of blocking forever on stdin.
	type readResult struct {
		line string
		err  error
	}
	resultCh := make(chan readResult, 1)
	go func() {
		line, err := bufio.NewReader(a.clarifyIn).ReadString('\n')
		resultCh <- readResult{line, err}
	}()

	var line string
	select {
	case <-ctx.Done():
		a.emitter.Emit(events.ClarificationAnsweredEvent(ac.SessionID, "", false))
		return ac.Task
	case res := <-resultCh:
		line, err = res.line, res.err
	}

	answer := strings.TrimSpace(line)
	if err != nil || answer == "" {
		a.emitter.Emit(events.ClarificationAnsweredEvent(ac.SessionID, "", false))
		return ac.Task
	}

	refined, err := a.comp.Compile(ctx, answer)
	var reject *compiler.RejectionError
	if errors.As(err, &reject) || err != nil {
		a.emitter.Emit(events.ClarificationAnsweredEvent(ac.SessionID, answer, false))
		return ac.Task
	}

	// Carry over constraints/deliverables the refined task may have omitted.
	if len(refined.Constraints) == 0 {
		refined.Constraints = ac.Task.Constraints
	}
	if len(refined.Deliverables) == 0 {
		refined.Deliverables = ac.Task.Deliverables
	}

	a.emitter.Emit(events.ClarificationAnsweredEvent(ac.SessionID, answer, true))
	return refined
}

// prepareRepo stashes a dirty working tree and pulls latest before the agent runs.
// Both operations are best-effort — errors are silently ignored so the agent
// can still proceed when git is unavailable or there is no upstream.
func (a *Agent) prepareRepo(ctx context.Context, ac *AgentContext) {
	statusResult, err := a.registry.DispatchDirect(ctx, ToolCall{
		Name: "git_status", Args: map[string]any{"root": ac.Root},
	})
	if err != nil {
		// Not a git repo or git unavailable — skip stash, still attempt pull.
		a.registry.DispatchDirect(ctx, ToolCall{ //nolint:errcheck
			Name: "git_pull", Args: map[string]any{"root": ac.Root, "remote": "origin"},
		})
		return
	}
	if status, ok := statusResult.(*tools.GitStatusResult); ok && !status.IsClean {
		a.registry.DispatchDirect(ctx, ToolCall{ //nolint:errcheck
			Name:  "git_stash",
			Args:  map[string]any{"root": ac.Root, "action": "push", "message": "forge: auto-stash"},
		})
		a.emitter.Emit(events.GitStashEvent(ac.SessionID, "push"))
	}
	a.registry.DispatchDirect(ctx, ToolCall{ //nolint:errcheck
		Name: "git_pull", Args: map[string]any{"root": ac.Root, "remote": "origin"},
	})
}

func userMsg(content string) costguard.Message {
	return costguard.Message{Role: "user", Content: content}
}

func (a *Agent) logCall(ac *AgentContext, role ModelRole, model string) {
	if !a.cfg.Debug {
		return
	}
	fmt.Fprintf(os.Stderr, "[agent] iteration %d/%d  [%s]  model=%s\n",
		ac.Iteration+1, a.cfg.MaxIter, role, model)
}

func (a *Agent) advanceIteration(ac *AgentContext) error {
	ac.Iteration++
	if ac.Iteration >= a.cfg.MaxIter {
		backstopMsg := fmt.Sprintf("task exceeded maximum iterations (%d)", a.cfg.MaxIter)
		a.emitter.Emit(events.Event{
			Type:      events.EventTaskFailed,
			Timestamp: time.Now(),
			SessionID: ac.SessionID,
			Payload: map[string]any{
				"session_id": ac.SessionID,
				"reason":     backstopMsg,
				"iterations": ac.Iteration,
			},
		})
		return fmt.Errorf("agent: %s", backstopMsg)
	}
	return nil
}

// resolveIntent asks the tool-caller model to convert a planner's natural-language
// intent into a valid TOOL:/ARGS: call. Returns the raw tool-caller response
// (expected to start with "TOOL:") or an error if the call fails or the response
// doesn't look like a tool call.
func (a *Agent) resolveIntent(ctx context.Context, ac *AgentContext, intent string) (string, error) {
	toolCallerModel := a.cfg.selectModel(RoleToolCaller)
	a.logCall(ac, RoleToolCaller, toolCallerModel)

	req := costguard.ChatRequest{
		Model: toolCallerModel,
		Messages: []costguard.Message{
			{Role: "system", Content: toolCallerSystemPrompt},
			{Role: "user", Content: fmt.Sprintf(
				"Convert this intent into a tool call: %s\n\nAvailable tools:\n%s",
				intent, availableToolsList)},
		},
	}
	if a.cfg.Debug {
		fmt.Fprintf(os.Stderr, "[context] tool_caller: ~%d / %d tokens\n",
			estimateTokens(req.Messages), a.cfg.limitForRole(RoleToolCaller))
	}
	resp, err := a.client.Chat(ctx, req)
	if err != nil {
		return "", fmt.Errorf("tool caller: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", errors.New("tool caller: empty response")
	}
	out := strings.TrimSpace(resp.Choices[0].Message.Content)
	if !strings.Contains(out, "TOOL:") {
		return "", fmt.Errorf("tool caller: response did not contain a TOOL: call: %q", out)
	}
	return out, nil
}

// generateRepoSummary calls reposummary.Generate for repo-wide/module scoped
// tasks, using list_files (not a second filesystem walk) and the planner model.
// Returns "" for file-specific tasks or on any failure — never blocks the loop.
func (a *Agent) generateRepoSummary(ctx context.Context, ac *AgentContext) string {
	if ac.Task.Scope == compiler.ScopeFileSpecific {
		return ""
	}

	fmt.Fprintln(os.Stderr, "Summarizing repo structure...")

	listResult, err := a.registry.DispatchDirect(ctx, ToolCall{
		Name: "list_files",
		Args: map[string]any{"root": ac.Root},
	})
	if err != nil {
		return ""
	}
	lf, ok := listResult.(*tools.ListFilesResult)
	if !ok || len(lf.Files) == 0 {
		return ""
	}

	var forgeMDBlock string
	if ac.ProjectConfig != nil && !ac.ProjectConfig.IsZero() {
		forgeMDBlock = ac.ProjectConfig.Raw
	}

	model := a.cfg.selectModel(RolePlanner)
	chat := func(ctx context.Context, model, systemPrompt, userPrompt string) (string, error) {
		a.logCall(ac, RolePlanner, model)
		resp, err := a.client.Chat(ctx, costguard.ChatRequest{
			Model: model,
			Messages: []costguard.Message{
				{Role: "system", Content: systemPrompt},
				{Role: "user", Content: userPrompt},
			},
		})
		if err != nil {
			return "", err
		}
		if len(resp.Choices) == 0 {
			return "", fmt.Errorf("empty response")
		}
		return strings.TrimSpace(resp.Choices[0].Message.Content), nil
	}

	summary, fromCache, err := reposummary.Generate(ctx, ac.Root, lf.Files, forgeMDBlock, chat, model)
	if err != nil && a.cfg.Debug {
		fmt.Fprintf(os.Stderr, "[reposummary] generation warning: %v\n", err)
	}
	if a.cfg.Debug {
		src := "generated"
		if fromCache {
			src = "cached"
		}
		fmt.Fprintf(os.Stderr, "[reposummary] (%s) %s\n", src, summary)
	}
	return summary
}

// retryToolCallFormat is called when ParseToolCall fails on a response that
// was supposed to contain a tool call. It retries exactly once against the
// same role that produced the malformed response, with an explicit reformat
// instruction, and returns the parsed ToolCall if the retry succeeds.
//
// role is whichever role generated lastResponse: RoleToolCaller if the tool
// caller is enabled and resolved the call, otherwise RolePlanner.
//
// retryToolCallFormat must not append anything to ac.History — the retry
// exchange is scaffolding for this one attempt and must not become part of
// the persisted conversation whether it succeeds or fails.
func (a *Agent) retryToolCallFormat(
	ctx context.Context,
	ac *AgentContext,
	role ModelRole,
	lastResponse string,
) (ToolCall, bool) {
	if a.cfg.Debug {
		fmt.Fprintf(os.Stderr, "[%s] retrying malformed tool call\n", role)
	}

	model := a.cfg.selectModel(role)
	reformatMsg := costguard.Message{
		Role: "user",
		Content: "Your last response was not valid. Reformat as exactly:\n" +
			"TOOL: <tool_name>\nARGS: {\"key\": \"value\"}\n\n" +
			"Your previous response was:\n" + lastResponse,
	}

	var messages []costguard.Message
	if role == RoleToolCaller {
		// Mirror resolveIntent's self-contained message shape: system prompt +
		// the reformat instruction. No ac.History involvement — the tool caller
		// never sees conversation history.
		messages = []costguard.Message{
			{Role: "system", Content: toolCallerSystemPrompt},
			reformatMsg,
		}
	} else {
		// Planner retry: same system/task context as the main loop, plus the
		// reformat instruction as the latest turn. Constructed fresh — does not
		// mutate ac.History.
		messages = append(append([]costguard.Message{
			SystemMessage(ac.ProjectConfig, ac.Memory),
			TaskMessage(ac.Task),
		}, ac.History...), reformatMsg)
	}

	a.logCall(ac, role, model)
	resp, err := a.client.Chat(ctx, costguard.ChatRequest{
		Model:    model,
		Messages: messages,
	})
	if err != nil || len(resp.Choices) == 0 {
		return ToolCall{}, false
	}

	retried := strings.TrimSpace(resp.Choices[0].Message.Content)
	return ParseToolCall(retried)
}

// intentSuggestsCode returns true if the lowercased intent contains a keyword
// suggesting a code-modification operation.
func intentSuggestsCode(intent string) bool {
	lower := strings.ToLower(intent)
	for _, kw := range []string{"patch", "write", "fix", "implement", "create", "modify"} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

var _ = tools.IsRecoverable
