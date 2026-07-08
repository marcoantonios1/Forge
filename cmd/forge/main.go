package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/marcoantonios1/Forge/internal/agent"
	"github.com/marcoantonios1/Forge/internal/compiler"
	"github.com/marcoantonios1/Forge/internal/config"
	"github.com/marcoantonios1/Forge/internal/embeddings"
	"github.com/marcoantonios1/Forge/internal/confirm"
	"github.com/marcoantonios1/Forge/internal/memory"
	"github.com/marcoantonios1/Forge/internal/costguard"
	"github.com/marcoantonios1/Forge/internal/events"
	"github.com/marcoantonios1/Forge/internal/forgeinit"
	"github.com/marcoantonios1/Forge/internal/mcp"
	"github.com/marcoantonios1/Forge/internal/mode"
	"github.com/marcoantonios1/Forge/internal/patch"
	"github.com/marcoantonios1/Forge/internal/projectconfig"
	"github.com/marcoantonios1/Forge/internal/session"
	"github.com/marcoantonios1/Forge/internal/timeline"
	"github.com/marcoantonios1/Forge/internal/tools"
	"github.com/marcoantonios1/Forge/internal/ui"
)

var (
	debugFlag        = flag.Bool("debug", false, "enable debug event output")
	timelineFlag     = flag.Bool("timeline", false, "print a readable execution timeline after task completion")
	printFlag        = flag.Bool("print", false, "run a task non-interactively and exit")
	outputFlag       = flag.String("output", "text", "output format in --print mode: text or json")
	yesFlag          = flag.Bool("yes", false, "approve all patches without prompting")
	allowedToolsFlag = flag.String("allowed-tools", "", `comma-separated tool categories to pre-approve.
	Categories: read, git_read, patch. Use "all" to pre-approve everything.
	Example: --allowed-tools=read,git_read`)
	allowMainCommit = flag.Bool("allow-main-commit", false,
		"allow committing directly to main or master (unsafe)")
	modeFlag   = flag.String("mode", "safe", "execution mode: safe, balanced, or autonomous")
	resumeFlag = flag.Bool("resume", false, "resume the last session for this repo")
)

type headlessResult struct {
	Status     string   `json:"status"` // "success" | "failure" | "rejected"
	Summary    string   `json:"summary"`
	Files      []string `json:"files"` // relative paths of files patched
	Iterations int      `json:"iterations"`
}

func runHeadless(rawTask, outputFmt string, debug bool, sessionMode mode.SessionMode) int {
	// TODO: add --timeout <duration> flag to cancel ctx after a fixed wall-clock duration.

	// 1. Signal handling — Ctrl+C exits with code 130.
	ctx, cancel := context.WithCancel(context.Background())
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT)
	go func() {
		<-sigs
		fmt.Fprintln(os.Stderr, "\nforge: cancelled")
		cancel()
	}()
	defer signal.Stop(sigs)

	// 2. Session setup.
	sessionID := session.NewID()
	cwd, _ := os.Getwd()

	// 3. Renderer — events go to stderr so stdout is clean for --output json.
	renderer := ui.New(os.Stderr, ui.ModePlain)
	var emitter events.Emitter = renderer
	if debug {
		debugRenderer := ui.New(os.Stderr, ui.ModeDebug)
		emitter = events.Multi(renderer, debugRenderer)
	}
	var tc *timeline.TimelineCollector
	if *timelineFlag {
		tc = timeline.NewTimelineCollector()
		emitter = events.Multi(emitter, tc)
	}
	auditLogger, auditErr := mode.NewAuditLogger(cwd, sessionID, sessionMode)
	if auditErr != nil {
		fmt.Fprintf(os.Stderr, "warning: audit logging disabled: %v\n", auditErr)
	}
	defer auditLogger.Close()
	if sessionMode == mode.ModeAutonomous {
		emitter = &mode.EmitterTap{Inner: emitter, Audit: auditLogger}
	}

	// 4. Project config.
	projectCfg, err := projectconfig.Load(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v\n", err)
	}

	// 4b. Persistent memory.
	mem, err := memory.Load(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to load memory: %v\n", err)
		mem = &memory.Memory{Version: memory.CurrentVersion, TaskHistory: []memory.TaskHistoryEntry{}}
	}

	// 5. Costguard client + compiler.
	appCfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		return 1
	}
	cgClient := costguard.New(appCfg)
	comp := compiler.New(cgClient, appCfg.CompilerModel, debug)

	// 5b. Embedding index (skipped when FORGE_EMBEDDING_MODEL is unset).
	// TODO: print a one-time hint when EmbeddingModel is unset:
	// "Set FORGE_EMBEDDING_MODEL to enable semantic search across the repo"
	var embedClient *embeddings.EmbedClient
	var index *embeddings.Index
	if appCfg.EmbeddingModel != "" {
		embedClient = embeddings.NewEmbedClient(cgClient, appCfg.EmbeddingModel)
		existing, loadErr := embeddings.Load(cwd)
		if loadErr != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to load existing index: %v\n", loadErr)
		}
		fmt.Fprint(os.Stderr, "Indexing repo...")
		newIndex, buildErr := embeddings.Build(ctx, cwd, embedClient, existing,
			func(current, total int, path string) {
				fmt.Fprintf(os.Stderr, "\rIndexing repo... %d/%d files", current, total)
			})
		if buildErr != nil {
			fmt.Fprintf(os.Stderr, "\nwarning: indexing failed: %v — semantic_search will fall back to grep\n", buildErr)
		} else {
			fmt.Fprintf(os.Stderr, "\rIndexing repo... %d files done.                    \n", len(newIndex.FileHashes))
			if saveErr := embeddings.Save(cwd, newIndex); saveErr != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to save index: %v\n", saveErr)
			}
			index = newIndex
		}
	}

	// 6. Compile.
	task, err := comp.Compile(ctx, rawTask)
	var reject *compiler.RejectionError
	if errors.As(err, &reject) {
		writeResult(outputFmt, headlessResult{Status: "rejected", Summary: reject.Reason})
		return 1
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "compile error: %v\n", err)
		return 1
	}

	// 6e. MCP servers declared in forge.md [mcp] section (session-scoped: connect
	// once, forward into the registry/agent below, close on return).
	mcpClients := connectMCPServers(projectCfg, emitter, sessionID)
	defer closeMCPClients(mcpClients)

	// 7. Session history + agent setup.
	sessionHistory := patch.NewPatchHistory()
	ac := agent.NewAgentContext(sessionID, task, cwd, projectCfg, sessionHistory, mem)

	registry := agent.NewRegistry(cwd, emitter, sessionID, nil, embedClient, index, mcpClients) // headless: no permission gate
	confirmer := confirm.AutoConfirmer{}                                                        // always — no prompts in headless mode
	agentCfg := agent.Config{
		PlannerModel:          appCfg.PlannerModel,
		CoderModel:            appCfg.CoderModel,
		ToolCallerModel:       appCfg.ToolCallerModel,
		CompactorModel:        appCfg.CompactorModel,
		ReviewerModel:         appCfg.ReviewerModel,
		ReviewerExplicitlyOff: isReviewerExplicitlyOff(),
		Limits: agent.ModelLimits{
			CompilerMaxTokens:   appCfg.Limits.CompilerMaxTokens,
			PlannerMaxTokens:    appCfg.Limits.PlannerMaxTokens,
			CoderMaxTokens:      appCfg.Limits.CoderMaxTokens,
			ToolCallerMaxTokens: appCfg.Limits.ToolCallerMaxTokens,
			CompactorMaxTokens:  appCfg.Limits.CompactorMaxTokens,
			ReviewerMaxTokens:       appCfg.Limits.ReviewerMaxTokens,
			ReviewerContextTokens:   appCfg.Limits.ReviewerContextTokens,
			EmbeddingMaxTokens:      appCfg.Limits.EmbeddingMaxTokens,
			CompilerContextTokens:   appCfg.Limits.CompilerContextTokens,
			PlannerContextTokens:    appCfg.Limits.PlannerContextTokens,
			CoderContextTokens:      appCfg.Limits.CoderContextTokens,
			ToolCallerContextTokens: appCfg.Limits.ToolCallerContextTokens,
			CompactorContextTokens:  appCfg.Limits.CompactorContextTokens,
		},
		AutoApply: true,
		Debug:     debug,
	}
	ag := agent.New(agentCfg, cgClient, registry, emitter, confirmer, nil, nil, nil, mcpClients)

	// 8. Run the agent.
	runErr := ag.Run(ctx, ac)

	// Print timeline after agent finishes (stderr — keep stdout clean for --output json).
	if tc != nil {
		rows := timeline.BuildRows(tc.Steps())
		timeline.RenderTable(os.Stderr, rows)
	}

	// Post-task git workflow (headless always auto-commits).
	if runErr == nil && ac.Patches.Len() > 0 {
		if err := runGitWorkflow(ctx, ac, registry, emitter, true); err != nil {
			fmt.Fprintf(os.Stderr, "git workflow: %v\n", err)
		}
	}

	// Persist memory after a successful run.
	if runErr == nil {
		var memFiles []string
		for _, rec := range sessionHistory.AllRecords() {
			if !rec.Reverted {
				for p := range rec.Originals {
					memFiles = append(memFiles, p)
				}
			}
		}
		sort.Strings(memFiles)
		memFiles = dedup(memFiles)
		mem.Update(ac.LastSummary, memFiles, memory.Conventions{})
		if err := memory.Save(cwd, mem); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to save memory: %v\n", err)
		}
	}

	// Ctrl+C: context.Canceled → exit 130 (standard shell convention).
	if runErr != nil && errors.Is(runErr, context.Canceled) {
		writeResult(outputFmt, headlessResult{Status: "failure", Summary: "cancelled"})
		return 130
	}

	// 9. Build result from session history.
	var files []string
	for _, rec := range sessionHistory.AllRecords() {
		if !rec.Reverted {
			for path := range rec.Originals {
				files = append(files, path)
			}
		}
	}
	sort.Strings(files)
	files = dedup(files)

	status := "success"
	summary := ac.LastSummary
	exitCode := 0
	if runErr != nil {
		status = "failure"
		if summary == "" {
			summary = runErr.Error()
		}
		exitCode = 1
	}

	// 10. Write output and exit.
	writeResult(outputFmt, headlessResult{
		Status:     status,
		Summary:    summary,
		Files:      files,
		Iterations: ac.Iteration,
	})
	return exitCode
}

// connectMCPServers parses forge.md's [mcp] section and connects to each
// declared MCP server, emitting an MCPConnectedEvent or MCPErrorEvent for
// each one. Parse and connect failures are non-fatal — MCP support is
// best-effort and a bad server entry must not block the rest of the session.
// MCP clients are session-scoped: connected once here, forwarded as-is into
// every task's registry/agent construction, and closed on exit.
func connectMCPServers(projectCfg *projectconfig.ProjectConfig, emitter events.Emitter, sessionID string) []mcp.Client {
	if projectCfg == nil || projectCfg.IsZero() {
		return nil
	}
	servers, parseErr := mcp.ParseMCPServers(projectCfg.Raw)
	if parseErr != nil {
		fmt.Fprintf(os.Stderr, "warning: mcp: parsing forge.md: %v\n", parseErr)
	}
	servers = validateMCPServers(servers)
	var clients []mcp.Client
	for _, srv := range servers {
		client, newErr := mcp.New(srv)
		if newErr != nil {
			emitter.Emit(events.MCPErrorEvent(sessionID, srv.Name, newErr.Error()))
			fmt.Fprintf(os.Stderr, "warning: mcp: %v\n", newErr)
			continue
		}
		connectCtx, connectCancel := context.WithTimeout(context.Background(), 10*time.Second)
		connectErr := client.Connect(connectCtx)
		connectCancel()
		if connectErr != nil {
			emitter.Emit(events.MCPErrorEvent(sessionID, srv.Name, connectErr.Error()))
			fmt.Fprintf(os.Stderr, "warning: mcp: connect %q: %v\n", srv.Name, connectErr)
			continue
		}
		emitter.Emit(events.MCPConnectedEvent(sessionID, srv.Name, len(client.ListTools())))
		clients = append(clients, client)
	}
	return clients
}

// closeMCPClients kills stdio subprocesses / releases HTTP clients so MCP
// servers don't become orphans on exit. Safe to call with a nil/empty slice.
func closeMCPClients(clients []mcp.Client) {
	for _, c := range clients {
		c.Close() //nolint:errcheck
	}
}

// validateMCPServers checks each server's config for basic sanity before
// Connect() is attempted — a missing executable or clearly wrong URL causes
// a warning and removes the server from the list so it doesn't produce a
// confusing error at Connect() time.
func validateMCPServers(servers []mcp.MCPServer) []mcp.MCPServer {
	var valid []mcp.MCPServer
	for _, srv := range servers {
		if err := validateMCPServer(srv); err != nil {
			fmt.Fprintf(os.Stderr, "warning: mcp server %q skipped: %v\n", srv.Name, err)
			continue
		}
		valid = append(valid, srv)
	}
	return valid
}

func validateMCPServer(srv mcp.MCPServer) error {
	switch srv.Transport {
	case mcp.TransportStdio:
		if srv.Command == "" {
			return fmt.Errorf("stdio server requires a command")
		}
		// Split the command the same way stdio.go does (strings.Fields)
		// and check the resulting executable.
		parts := strings.Fields(srv.Command)
		if len(parts) == 0 {
			return fmt.Errorf("stdio server has empty command")
		}
		if _, err := exec.LookPath(parts[0]); err != nil {
			return fmt.Errorf("executable %q not found on PATH: %w", parts[0], err)
		}
		return nil

	case mcp.TransportHTTP:
		if srv.URL == "" {
			return fmt.Errorf("http server requires a url")
		}
		if !strings.HasPrefix(srv.URL, "http://") && !strings.HasPrefix(srv.URL, "https://") {
			return fmt.Errorf("http server url must start with http:// or https://")
		}
		// Lightweight reachability check: HEAD request with a short timeout.
		// Failure is a warning, not a hard error — the server might be on a VPN,
		// in a container, or starting up; we still attempt Connect() after this.
		checkCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(checkCtx, http.MethodHead, srv.URL, nil)
		if err != nil {
			// URL construction failed — that's a hard validation error.
			return fmt.Errorf("invalid url %q: %w", srv.URL, err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			// Network error — warn but don't skip (server might start later).
			fmt.Fprintf(os.Stderr, "warning: mcp server %q may be unreachable: %v\n", srv.Name, err)
			return nil // not a hard failure
		}
		resp.Body.Close()
		if resp.StatusCode >= 500 {
			fmt.Fprintf(os.Stderr, "warning: mcp server %q returned %d — it may not be ready\n",
				srv.Name, resp.StatusCode)
		}
		return nil

	default:
		return fmt.Errorf("unknown transport %q", srv.Transport)
	}
}

func writeResult(format string, r headlessResult) {
	// TODO: add --output jsonl (one JSON event per line) for streaming pipeline consumption.
	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(r) //nolint:errcheck
		return
	}
	fmt.Printf("status:     %s\n", r.Status)
	fmt.Printf("summary:    %s\n", r.Summary)
	if len(r.Files) > 0 {
		fmt.Printf("files:\n")
		for _, f := range r.Files {
			fmt.Printf("  %s\n", f)
		}
	}
	fmt.Printf("iterations: %d\n", r.Iterations)
}

// isReviewerExplicitlyOff reports whether FORGE_REVIEWER_MODEL was present in
// the environment with an empty value — the documented opt-out signal.
// config.Load() already resolved cfg.ReviewerModel's fallback, so this
// re-checks the raw env var directly rather than threading a second field
// through config.Config.
func isReviewerExplicitlyOff() bool {
	v, present := os.LookupEnv("FORGE_REVIEWER_MODEL")
	return present && v == ""
}

func dedup(sorted []string) []string {
	if len(sorted) == 0 {
		return sorted
	}
	out := sorted[:1]
	for _, s := range sorted[1:] {
		if s != out[len(out)-1] {
			out = append(out, s)
		}
	}
	return out
}

// slugify converts a task description into a URL-safe branch-name segment.
func slugify(s string) string {
	s = strings.ToLower(s)
	var sb strings.Builder
	inDash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
			inDash = false
		} else if !inDash && sb.Len() > 0 {
			sb.WriteByte('-')
			inDash = true
		}
	}
	result := strings.TrimRight(sb.String(), "-")
	if len(result) > 50 {
		result = strings.TrimRight(result[:50], "-")
	}
	return result
}

// runTask compiles and runs a single REPL task: compile, build the agent,
// run it under ctx, and run the post-task git workflow. ctx is cancelled by
// the caller's signal handler on Ctrl+C — it does not read taskCancel/taskMu.
func runTask(
	ctx context.Context,
	line string,
	cwd string,
	cfg *config.Config,
	client *costguard.Client,
	comp *compiler.Compiler,
	renderer *ui.Renderer,
	sessionHistory *patch.PatchHistory,
	projectCfg *projectconfig.ProjectConfig,
	sessionID string,
	yesOverride bool,
	allowedTools string,
	debug bool,
	embedClient *embeddings.EmbedClient,
	index *embeddings.Index,
	mem *memory.Memory,
	sessionMode mode.SessionMode,
	emitter events.Emitter,
	mcpClients []mcp.Client,
) error {
	task, err := comp.Compile(ctx, line)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "compiled: %s/%s [%s]\n",
		task.Category, task.Scope, task.ExecutionPolicy)

	// autonomous mode always auto-confirms patches, regardless of task.ExecutionPolicy.
	var confirmer agent.PatchConfirmer
	if sessionMode.AutoConfirmPatches() || yesOverride || task.ExecutionPolicy == compiler.PolicyAutonomous {
		confirmer = confirm.AutoConfirmer{}
	} else {
		confirmer = confirm.NewSafeConfirmer(os.Stdin, os.Stderr, ui.IsTTY(os.Stdout), emitter, sessionID)
	}
	fmt.Fprintf(os.Stderr, "mode: %s\n", task.ExecutionPolicy)

	agentCfg := agent.Config{
		PlannerModel:          cfg.PlannerModel,
		CoderModel:            cfg.CoderModel,
		ToolCallerModel:       cfg.ToolCallerModel,
		CompactorModel:        cfg.CompactorModel,
		ReviewerModel:         cfg.ReviewerModel,
		ReviewerExplicitlyOff: isReviewerExplicitlyOff(),
		Limits: agent.ModelLimits{
			CompilerMaxTokens:   cfg.Limits.CompilerMaxTokens,
			PlannerMaxTokens:    cfg.Limits.PlannerMaxTokens,
			CoderMaxTokens:      cfg.Limits.CoderMaxTokens,
			ToolCallerMaxTokens: cfg.Limits.ToolCallerMaxTokens,
			CompactorMaxTokens:    cfg.Limits.CompactorMaxTokens,
			ReviewerMaxTokens:     cfg.Limits.ReviewerMaxTokens,
			ReviewerContextTokens: cfg.Limits.ReviewerContextTokens,
			EmbeddingMaxTokens:    cfg.Limits.EmbeddingMaxTokens,
			CompilerContextTokens: cfg.Limits.CompilerContextTokens,
			PlannerContextTokens:    cfg.Limits.PlannerContextTokens,
			CoderContextTokens:      cfg.Limits.CoderContextTokens,
			ToolCallerContextTokens: cfg.Limits.ToolCallerContextTokens,
			CompactorContextTokens:  cfg.Limits.CompactorContextTokens,
		},
		Debug:   debug,
	}

	// Merge --allowed-tools with the session mode's auto-approved categories.
	preApproved := confirm.ParseAllowedTools(allowedTools)
	if preApproved == nil {
		preApproved = map[string]bool{}
	}
	for cat := range sessionMode.PreApprovedCategories() {
		preApproved[cat] = true
	}

	// interactive: false when --yes, task is autonomous, or session mode is autonomous.
	interactive := !yesOverride &&
		task.ExecutionPolicy != compiler.PolicyAutonomous &&
		sessionMode.Interactive()

	gate := confirm.NewPermissionGate(
		os.Stdin, os.Stderr,
		ui.IsTTY(os.Stdout),
		debug,
		emitter,
		sessionID,
		preApproved,
		interactive,
	)
	registry := agent.NewRegistry(cwd, emitter, sessionID, gate, embedClient, index, mcpClients)
	a := agent.New(agentCfg, client, registry, emitter, confirmer, comp, os.Stdin, os.Stderr, mcpClients)
	ac := agent.NewAgentContext(sessionID, task, cwd, projectCfg, sessionHistory, mem)

	if err := a.Run(ctx, ac); err != nil {
		return err
	}
	if ac.Patches.Len() > 0 {
		_, autoConfirmer := confirmer.(confirm.AutoConfirmer)
		autoGit := autoConfirmer || sessionMode == mode.ModeAutonomous
		if err := runGitWorkflow(ctx, ac, registry, emitter, autoGit); err != nil {
			fmt.Fprintf(os.Stderr, "git workflow: %v\n", err)
		}
	}

	// Persist memory after a successful task.
	var memFiles []string
	for _, rec := range sessionHistory.AllRecords() {
		if !rec.Reverted {
			for p := range rec.Originals {
				memFiles = append(memFiles, p)
			}
		}
	}
	sort.Strings(memFiles)
	memFiles = dedup(memFiles)
	mem.Update(ac.LastSummary, memFiles, memory.Conventions{})
	if err := memory.Save(cwd, mem); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to save memory: %v\n", err)
	}

	// Save session for --resume (REPL path only).
	// TODO: headless (--print) mode could optionally save sessions behind an
	// explicit flag like --print --save-session; intentionally out of scope here.
	sess := &session.SavedSession{
		SessionID: sessionID,
		Repo:      cwd,
		RawInput:  task.RawInput,
		History:   ac.History,
		Patches:   sessionHistory.AllRecords(),
		Timestamp: time.Now(),
	}
	if err := session.Save(cwd, sess); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to save session: %v\n", err)
	}
	return nil
}

func confirmPolicy(
	policy compiler.ExecutionPolicy,
	yesOverride bool,
	in io.Reader,
	out io.Writer,
	colour bool,
	emitter events.Emitter,
	sessionID string,
) agent.PatchConfirmer {
	if yesOverride || policy == compiler.PolicyAutonomous {
		return confirm.AutoConfirmer{}
	}
	return confirm.NewSafeConfirmer(in, out, colour, emitter, sessionID)
}

// openEditor writes initial to a temp file, opens $EDITOR, and returns the
// edited content. Returns an error if $EDITOR is unset.
func openEditor(initial string) (string, error) {
	editorCmd := os.Getenv("EDITOR")
	if editorCmd == "" {
		return "", errors.New("EDITOR not set")
	}
	f, err := os.CreateTemp("", "forge-commit-*.txt")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(initial); err != nil {
		f.Close()
		return "", err
	}
	f.Close()
	cmd := exec.Command(editorCmd, f.Name())
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", err
	}
	data, err := os.ReadFile(f.Name())
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// runGitWorkflow creates a branch (when on main/master), commits, and pushes
// applied patches. In interactive mode the user is shown a proposal first.
//
// Calls DispatchDirect — bypasses the permission gate since the user has already
// been asked to confirm via the branch/commit prompt below.
func runGitWorkflow(
	ctx context.Context,
	ac *agent.AgentContext,
	registry *agent.Registry,
	emitter events.Emitter,
	auto bool,
) error {
	// 1. Derive branch name.
	baseBranch := "forge/" + slugify(ac.Task.RawInput)

	// 2. Determine current branch.
	statusRaw, err := registry.DispatchDirect(ctx, tools.ToolCall{
		Name: "git_status", Args: map[string]any{"root": ac.Root},
	})
	if err != nil {
		return fmt.Errorf("git_status: %w", err)
	}
	status, ok := statusRaw.(*tools.GitStatusResult)
	if !ok {
		return fmt.Errorf("unexpected git_status result type")
	}
	currentBranch := status.Branch
	needNewBranch := (currentBranch == "main" || currentBranch == "master") && !*allowMainCommit
	branchName := currentBranch
	if needNewBranch {
		branchName = baseBranch
	}

	// 3. Build commit message from agent summary or task input.
	commitMsg := ac.LastSummary
	if commitMsg == "" {
		raw := ac.Task.RawInput
		if len(raw) > 72 {
			raw = raw[:72]
		}
		commitMsg = "forge: " + raw
	}
	// Append patched file list to body.
	var patchedFiles []string
	for _, rec := range ac.Patches.AllRecords() {
		if !rec.Reverted {
			for p := range rec.Originals {
				patchedFiles = append(patchedFiles, p)
			}
		}
	}
	sort.Strings(patchedFiles)
	patchedFiles = dedup(patchedFiles)
	if len(patchedFiles) > 0 {
		commitMsg += "\n\nFiles:\n"
		for _, p := range patchedFiles {
			commitMsg += "  " + p + "\n"
		}
		commitMsg = strings.TrimRight(commitMsg, "\n")
	}

	// 4. Interactive: show proposal and prompt.
	if !auto {
		fmt.Printf("  Branch:  %s\n", branchName)
		fmt.Printf("  Commit:  %s\n", strings.SplitN(commitMsg, "\n", 2)[0])
		reader := bufio.NewReader(os.Stdin)
		for {
			fmt.Print("Create branch and commit? [y]es / [e]dit / [n]o  ")
			line, err := reader.ReadString('\n')
			if err != nil {
				return nil
			}
			switch strings.ToLower(strings.TrimSpace(line)) {
			case "y", "yes":
				// fall through to commit
			case "e":
				initial := fmt.Sprintf("branch: %s\nmessage: %s\n", branchName, strings.SplitN(commitMsg, "\n", 2)[0])
				edited, err := openEditor(initial)
				if err != nil {
					// $EDITOR unset or failed — fall back to inline prompts.
					fmt.Print("Branch name: ")
					bl, _ := reader.ReadString('\n')
					if v := strings.TrimSpace(bl); v != "" {
						branchName = v
					}
					fmt.Print("Commit message: ")
					ml, _ := reader.ReadString('\n')
					if v := strings.TrimSpace(ml); v != "" {
						commitMsg = v
					}
				} else {
					for _, l := range strings.Split(edited, "\n") {
						if strings.HasPrefix(l, "branch:") {
							if v := strings.TrimSpace(strings.TrimPrefix(l, "branch:")); v != "" {
								branchName = v
							}
						} else if strings.HasPrefix(l, "message:") {
							if v := strings.TrimSpace(strings.TrimPrefix(l, "message:")); v != "" {
								commitMsg = v
							}
						}
					}
				}
			case "n", "no", "":
				fmt.Println("Skipped.")
				return nil
			default:
				continue
			}
			break
		}
	}

	// 5. Create branch if needed (retry up to 5 times on collision).
	if needNewBranch {
		created := false
		for i := 0; i < 5; i++ {
			name := baseBranch
			if i > 0 {
				name = fmt.Sprintf("%s-%d", baseBranch, i+1)
			}
			_, err := registry.DispatchDirect(ctx, tools.ToolCall{
				Name: "git_branch",
				Args: map[string]any{"root": ac.Root, "name": name, "checkout": true},
			})
			if err == nil {
				branchName = name
				emitter.Emit(events.GitBranchEvent(ac.SessionID, branchName, true))
				created = true
				break
			}
		}
		if !created {
			return fmt.Errorf("could not create branch after 5 attempts")
		}
	}

	// 6. Commit.
	commitRaw, err := registry.DispatchDirect(ctx, tools.ToolCall{
		Name: "git_commit",
		Args: map[string]any{"root": ac.Root, "message": commitMsg, "stage_all": true},
	})
	if err != nil {
		return fmt.Errorf("git_commit: %w", err)
	}
	cr, ok := commitRaw.(*tools.GitCommitResult)
	if !ok {
		return fmt.Errorf("unexpected git_commit result type")
	}
	emitter.Emit(events.GitCommitEvent(ac.SessionID, cr.Hash, cr.Message, cr.Files))
	ac.AppliedCommit = cr.Hash
	ac.AppliedBranch = branchName

	// 7. Push — prompt in interactive mode.
	if !auto {
		reader := bufio.NewReader(os.Stdin)
		fmt.Printf("Push to origin/%s? [y]es / [n]o  ", branchName)
		line, err := reader.ReadString('\n')
		if err != nil || strings.ToLower(strings.TrimSpace(line)) != "y" {
			return nil
		}
	}
	pushRaw, err := registry.DispatchDirect(ctx, tools.ToolCall{
		Name: "git_push",
		Args: map[string]any{"root": ac.Root, "remote": "origin", "branch": branchName, "set_upstream": true},
	})
	if err != nil {
		return fmt.Errorf("git_push: %w", err)
	}
	pr, ok := pushRaw.(*tools.GitPushResult)
	if !ok {
		return fmt.Errorf("unexpected git_push result type")
	}
	emitter.Emit(events.GitPushEvent(ac.SessionID, pr.Remote, pr.Branch))
	return nil
}

// handleUndo reverts the most recently applied patch set.
// TODO: extend to support undoing the last N patch sets (multi-level undo).
func handleUndo(root string, history *patch.PatchHistory, emitter events.Emitter) {
	err := history.Undo(root, emitter)
	if errors.Is(err, patch.ErrNothingToUndo) {
		fmt.Println("nothing to undo")
		return
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "undo failed: %v\n", err)
		return
	}
	// EventFilePatchReverted is emitted per file by history.Undo() via the
	// emitter — the renderer prints each reverted file automatically.
	fmt.Println("undo complete")
}

// runInit implements `forge init`: detects project conventions via filesystem
// heuristics (no LLM call) and writes a starter forge.md to the repo root.
// TODO: `forge init --llm` would make a Costguard call to generate richer
// project conventions from the actual source files.
func runInit() int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	forgePath := filepath.Join(cwd, "forge.md")
	if _, err := os.Stat(forgePath); err == nil {
		fmt.Print("forge.md already exists. Overwrite? [y/n]  ")
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(line)) != "y" {
			fmt.Println("Aborted.")
			return 0
		}
	}

	d := forgeinit.Detect(cwd)
	content := forgeinit.Render(d)

	if err := os.WriteFile(forgePath, []byte(content), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "forge init: write failed: %v\n", err)
		return 1
	}

	fmt.Printf("Wrote forge.md:\n\n%s\n", content)
	return 0
}

func runMemoryCommand(args []string) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: forge memory <show|clear>")
		return 2
	}
	// TODO: add --json flag to `forge memory show` for a human-readable summary
	// view vs the current raw JSON output.
	switch args[0] {
	case "show":
		mem, err := memory.Load(cwd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "memory: %v\n", err)
			return 1
		}
		data, _ := json.MarshalIndent(mem, "", "  ")
		fmt.Println(string(data))
		return 0
	case "clear":
		if err := memory.Clear(cwd); err != nil {
			fmt.Fprintf(os.Stderr, "memory: %v\n", err)
			return 1
		}
		fmt.Println("memory cleared")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "usage: forge memory <show|clear>, got %q\n", args[0])
		return 2
	}
}

func runLogsCommand(args []string) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if len(args) < 2 || args[0] != "show" {
		fmt.Fprintln(os.Stderr, "usage: forge logs show <session-id>")
		return 2
	}
	sessionID := args[1]
	path := timeline.LogPath(cwd, sessionID)
	rows, err := timeline.ReadAuditLog(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "logs: %v\n", err)
		return 1
	}
	if len(rows) == 0 {
		fmt.Println("no log entries found")
		return 0
	}
	timeline.RenderTable(os.Stdout, rows)
	return 0
}

func runSessionsCommand(args []string) int {
	// TODO: add `forge sessions clear` / `forge sessions clear <repo>` mirroring
	// `forge memory clear`.
	if len(args) == 0 || args[0] != "list" {
		fmt.Fprintln(os.Stderr, "usage: forge sessions list")
		return 2
	}
	sessions := session.List(session.KnownRepos())
	if len(sessions) == 0 {
		fmt.Println("no saved sessions")
		return 0
	}
	for _, s := range sessions {
		id := s.SessionID
		if len(id) > 8 {
			id = id[:8]
		}
		fmt.Printf("%s  [%s]  %s  (%d files)  %s\n",
			s.Repo, id, s.RawInput, s.Files, s.Timestamp)
	}
	return 0
}

func main() {
	flag.Parse()

	sessionMode := mode.Parse(*modeFlag)

	if flag.NArg() > 0 && flag.Arg(0) == "init" {
		os.Exit(runInit())
	}

	if flag.NArg() > 0 && flag.Arg(0) == "memory" {
		os.Exit(runMemoryCommand(flag.Args()[1:]))
	}

	if flag.NArg() > 0 && flag.Arg(0) == "logs" {
		os.Exit(runLogsCommand(flag.Args()[1:]))
	}

	if flag.NArg() > 0 && flag.Arg(0) == "sessions" {
		os.Exit(runSessionsCommand(flag.Args()[1:]))
	}

	if *printFlag {
		task := strings.Join(flag.Args(), " ")
		if strings.TrimSpace(task) == "" {
			fmt.Fprintln(os.Stderr, "forge --print: task argument required")
			os.Exit(2)
		}
		os.Exit(runHeadless(task, *outputFlag, *debugFlag, sessionMode))
	}

	// 1. Config
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// 2. Costguard client
	client := costguard.New(cfg)

	// 3. Compiler
	comp := compiler.New(client, cfg.CompilerModel, cfg.Debug)

	// 4. Renderer + emitter (emitter wraps renderer; in autonomous mode also taps audit log)
	uiMode := ui.ModeHuman
	if *debugFlag {
		uiMode = ui.ModeDebug
	}
	renderer := ui.New(os.Stdout, uiMode)

	// 5. Working directory — fatal, not a warning
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatalf("cwd: %v", err)
	}

	// 6. Project config — declared before the if block so it stays in scope
	var projectCfg *projectconfig.ProjectConfig
	projectCfg, err = projectconfig.Load(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v\n", err)
	}

	// 6b. Persistent memory.
	mem, err := memory.Load(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to load memory: %v\n", err)
		mem = &memory.Memory{Version: memory.CurrentVersion, TaskHistory: []memory.TaskHistoryEntry{}}
	}

	// 6c. Signal handling — installed HERE, before embedding indexing, so that
	// Ctrl+C during the (potentially slow) indexing step actually cancels it.
	// Previously this block lived after indexing (step 8), which meant
	// context.Background() had to be passed to Build() making Ctrl+C a no-op
	// during startup. Fix: share taskCancel/taskMu between startup and the REPL
	// loop; each phase sets taskCancel before its long-running work.
	var (
		taskCancel  context.CancelFunc
		taskMu      sync.Mutex
		taskQueue   []string // pending raw task strings, FIFO
		queueMu     sync.Mutex
		taskRunning atomic.Bool // true while a task is actively executing
	)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT)
	go func() {
		var lastSig time.Time
		for range sigs {
			if !lastSig.IsZero() && time.Since(lastSig) < time.Second {
				fmt.Println("\nbye.")
				os.Exit(0)
			}
			lastSig = time.Now()

			taskMu.Lock()
			cancel := taskCancel
			taskMu.Unlock()

			if cancel != nil {
				cancel()
			} else {
				// TODO: consider warning "N tasks still queued — exit anyway? [y/n]"
				// on Ctrl+C at the idle prompt if the queue is non-empty, rather than
				// exiting silently and losing queued tasks.
				fmt.Println("\nbye.")
				os.Exit(0)
			}
		}
	}()

	// 6d. Embedding index (skipped when FORGE_EMBEDDING_MODEL is unset).
	// TODO: print a one-time hint when EmbeddingModel is unset:
	// "Set FORGE_EMBEDDING_MODEL to enable semantic search across the repo"
	// TODO: add a `make bench-largerepo` target that generates a synthetic
	// 10k-file fixture and times list_files/search_code/startup, for
	// tracking regressions over time without making it a hard CI gate.
	var embedClient *embeddings.EmbedClient
	var index *embeddings.Index
	if cfg.EmbeddingModel != "" {
		embedClient = embeddings.NewEmbedClient(client, cfg.EmbeddingModel)
		existing, loadErr := embeddings.Load(cwd)
		if loadErr != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to load existing index: %v\n", loadErr)
		}
		startupCtx, startupCancel := context.WithCancel(context.Background())
		taskMu.Lock()
		taskCancel = startupCancel // indexing counts as "a running operation" for Ctrl+C
		taskMu.Unlock()

		fmt.Fprint(os.Stderr, "Indexing repo... (Ctrl+C to cancel)")
		newIndex, buildErr := embeddings.Build(startupCtx, cwd, embedClient, existing,
			func(current, total int, path string) {
				fmt.Fprintf(os.Stderr, "\rIndexing repo... %d/%d files (Ctrl+C to cancel)", current, total)
			})

		taskMu.Lock()
		taskCancel = nil
		taskMu.Unlock()
		startupCancel() // release resources; safe to call even if already cancelled

		if buildErr != nil {
			if errors.Is(buildErr, context.Canceled) {
				fmt.Fprintln(os.Stderr, "\nindexing cancelled — continuing without semantic search for this session")
				// Do NOT save — a partial cancelled build must never be persisted.
				// The existing index (if any) remains untouched on disk.
			} else {
				fmt.Fprintf(os.Stderr, "\nwarning: indexing failed: %v — semantic_search will fall back to grep\n", buildErr)
			}
			// index stays nil — semantic_search falls back to grep.
		} else {
			fmt.Fprintf(os.Stderr, "\rIndexing repo... %d files done.                    \n", len(newIndex.FileHashes))
			// TODO: revisit embeddings.json sharding (see existing TODO in Save()) if
			// benchmarking on 10k+ file repos reveals this is a bottleneck.
			if saveErr := embeddings.Save(cwd, newIndex); saveErr != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to save index: %v\n", saveErr)
			}
			index = newIndex
		}
	}

	// 7. Session
	id := session.NewID()

	// Audit logger (non-nil only in autonomous mode) + emitter tap.
	auditLogger, auditErr := mode.NewAuditLogger(cwd, id, sessionMode)
	if auditErr != nil {
		fmt.Fprintf(os.Stderr, "warning: audit logging disabled: %v\n", auditErr)
	}
	defer auditLogger.Close()
	var tc *timeline.TimelineCollector
	var emitter events.Emitter = renderer
	if *timelineFlag {
		tc = timeline.NewTimelineCollector()
		emitter = events.Multi(renderer, tc)
	}
	if sessionMode == mode.ModeAutonomous {
		emitter = &mode.EmitterTap{Inner: emitter, Audit: auditLogger}
	}

	fmt.Printf("Forge — session %s [%s]\n", id, sessionMode)
	if projectCfg != nil {
		fmt.Printf("Loaded forge.md from %s\n", projectCfg.Path)
	}

	// MCP servers declared in forge.md [mcp] section (session-scoped: connect
	// once here, forward into every task's registry/agent construction below,
	// close cleanly on exit so subprocesses don't become orphans).
	mcpClients := connectMCPServers(projectCfg, emitter, id)
	defer closeMCPClients(mcpClients)

	// Session-scoped patch history so undo works across tasks.
	sessionHistory := patch.NewPatchHistory()

	// --resume: load last session, confirm, and run agent with restored context.
	if *resumeFlag {
		saved, err := session.Load(cwd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "session: %v\n", err)
			os.Exit(1)
		}
		if saved == nil {
			fmt.Println("no session to resume")
			os.Exit(0)
		}

		fileSet := map[string]bool{}
		for _, rec := range saved.Patches {
			for p := range rec.Originals {
				fileSet[p] = true
			}
		}
		shortID := saved.SessionID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		fmt.Printf("Resuming session %s: %s\n%d files modified — continue? [y/n]  ",
			shortID, saved.RawInput, len(fileSet))

		resumeReader := bufio.NewReader(os.Stdin)
		ans, _ := resumeReader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(ans)) != "y" {
			fmt.Println("Resume cancelled.")
			os.Exit(0)
		}

		// Re-compile original input — guarantees Task is valid under the current
		// compiler schema rather than a potentially stale serialized one.
		// TODO: consider persisting the compiled Task alongside RawInput if
		// resume-time re-compilation drift becomes a problem in practice.
		resumeCtx := context.Background()
		resumedTask, compErr := comp.Compile(resumeCtx, saved.RawInput)
		var reject *compiler.RejectionError
		if errors.As(compErr, &reject) || compErr != nil {
			fmt.Fprintf(os.Stderr, "could not resume: re-compiling original task failed: %v\n", compErr)
			os.Exit(1)
		}

		restoredHistory := patch.RestoreHistory(saved.Patches)
		ac := agent.NewAgentContext(saved.SessionID, resumedTask, cwd, projectCfg, restoredHistory, mem)
		ac.History = saved.History

		resumeAgentCfg := agent.Config{
			PlannerModel:          cfg.PlannerModel,
			CoderModel:            cfg.CoderModel,
			ToolCallerModel:       cfg.ToolCallerModel,
			CompactorModel:        cfg.CompactorModel,
			ReviewerModel:         cfg.ReviewerModel,
			ReviewerExplicitlyOff: isReviewerExplicitlyOff(),
			Limits: agent.ModelLimits{
				CompilerMaxTokens:   cfg.Limits.CompilerMaxTokens,
				PlannerMaxTokens:    cfg.Limits.PlannerMaxTokens,
				CoderMaxTokens:      cfg.Limits.CoderMaxTokens,
				ToolCallerMaxTokens: cfg.Limits.ToolCallerMaxTokens,
				CompactorMaxTokens:    cfg.Limits.CompactorMaxTokens,
				ReviewerMaxTokens:     cfg.Limits.ReviewerMaxTokens,
				ReviewerContextTokens: cfg.Limits.ReviewerContextTokens,
				EmbeddingMaxTokens:    cfg.Limits.EmbeddingMaxTokens,
				CompilerContextTokens: cfg.Limits.CompilerContextTokens,
				PlannerContextTokens:    cfg.Limits.PlannerContextTokens,
				CoderContextTokens:      cfg.Limits.CoderContextTokens,
				ToolCallerContextTokens: cfg.Limits.ToolCallerContextTokens,
				CompactorContextTokens:  cfg.Limits.CompactorContextTokens,
			},
			Debug:   *debugFlag,
		}
		resumePreApproved := confirm.ParseAllowedTools(*allowedToolsFlag)
		if resumePreApproved == nil {
			resumePreApproved = map[string]bool{}
		}
		for cat := range sessionMode.PreApprovedCategories() {
			resumePreApproved[cat] = true
		}
		resumeInteractive := !*yesFlag &&
			resumedTask.ExecutionPolicy != compiler.PolicyAutonomous &&
			sessionMode.Interactive()
		var resumeConfirmer agent.PatchConfirmer
		if sessionMode.AutoConfirmPatches() || *yesFlag || resumedTask.ExecutionPolicy == compiler.PolicyAutonomous {
			resumeConfirmer = confirm.AutoConfirmer{}
		} else {
			resumeConfirmer = confirm.NewSafeConfirmer(os.Stdin, os.Stderr, ui.IsTTY(os.Stdout), emitter, saved.SessionID)
		}
		resumeGate := confirm.NewPermissionGate(
			os.Stdin, os.Stderr, ui.IsTTY(os.Stdout), *debugFlag,
			emitter, saved.SessionID, resumePreApproved, resumeInteractive,
		)
		resumeRegistry := agent.NewRegistry(cwd, emitter, saved.SessionID, resumeGate, embedClient, index, mcpClients)
		resumeAgent := agent.New(resumeAgentCfg, client, resumeRegistry, emitter, resumeConfirmer, comp, os.Stdin, os.Stderr, mcpClients)

		runErr := resumeAgent.Run(resumeCtx, ac)
		if runErr == nil && ac.Patches.Len() > 0 {
			_, autoC := resumeConfirmer.(confirm.AutoConfirmer)
			runGitWorkflow(resumeCtx, ac, resumeRegistry, emitter, autoC || sessionMode == mode.ModeAutonomous) //nolint:errcheck
		}
		if runErr != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", runErr)
			os.Exit(1)
		}
		// Save the continued session so a second --resume picks up here.
		session.Save(cwd, &session.SavedSession{ //nolint:errcheck
			SessionID: ac.SessionID,
			Repo:      cwd,
			RawInput:  resumedTask.RawInput,
			History:   ac.History,
			Patches:   ac.Patches.AllRecords(),
			Timestamp: time.Now(),
		})
		os.Exit(0)
	}

	// 8. REPL task runner — signal handling and queue state were moved to step
	// 6c so they cover embedding indexing too. Variables (taskCancel, taskMu,
	// taskQueue, queueMu, taskRunning) are already declared above.

	// runQueuedTask runs a single task to completion, then drains and runs any
	// tasks that were enqueued while it was running before returning control to
	// the REPL's select loop.
	var runQueuedTask func(string)
	runQueuedTask = func(line string) {
		taskRunning.Store(true)
		defer taskRunning.Store(false)

		ctx, cancel := context.WithCancel(context.Background())
		taskMu.Lock()
		taskCancel = cancel
		taskMu.Unlock()

		runErr := runTask(ctx, line, cwd, cfg, client, comp, renderer,
			sessionHistory, projectCfg, id, *yesFlag, *allowedToolsFlag, *debugFlag,
			embedClient, index, mem, sessionMode, emitter, mcpClients)

		if tc != nil {
			rows := timeline.BuildRows(tc.Steps())
			timeline.RenderTable(os.Stdout, rows)
		}

		cancel()
		taskMu.Lock()
		taskCancel = nil
		taskMu.Unlock()

		if runErr != nil {
			if errors.Is(runErr, context.Canceled) {
				// TODO: if the task had applied patches before cancellation,
				// hint "run `undo` to revert applied patches" here.
				fmt.Println("task cancelled")
				// Cancelling the current task does NOT clear the queue —
				// the next queued task still runs automatically.
			} else {
				var re *compiler.RejectionError
				if errors.As(runErr, &re) {
					fmt.Fprintf(os.Stderr, "rejected: %s\n", re.Reason)
				} else {
					fmt.Fprintf(os.Stderr, "error: %s\n", runErr)
				}
				// Task failure also does not clear the queue — drain continues.
			}
		}

		// Drain: if anything was enqueued during this task, run the next one
		// immediately without waiting for a fresh prompt read.
		queueMu.Lock()
		var next string
		hasNext := len(taskQueue) > 0
		if hasNext {
			next = taskQueue[0]
			taskQueue = taskQueue[1:]
		}
		queueMu.Unlock()

		if hasNext {
			// TODO: suppress the background reader's "> " prompt print while a task
			// is running, to avoid visual interleaving with the active task's
			// renderer output; print exactly one "> " when the queue fully drains.
			fmt.Printf("\nrunning next queued task (%d remaining)\n", len(taskQueue))
			runQueuedTask(next)
		}
		// TODO: add `queue clear` and `queue remove <n>` commands for queue
		// management beyond list/append.
	}

	// REPL — background goroutine reads stdin continuously so new lines can be
	// received even while a task is running (they get enqueued rather than
	// executed immediately). Multiline "<<<" / "." accumulation is handled here
	// so a complete task string arrives on lineCh regardless of input method.
	type inputLine struct {
		text string
		err  error
	}
	lineCh := make(chan inputLine)
	reader := bufio.NewReader(os.Stdin)

	go func() {
		for {
			// TODO: suppress this "> " while taskRunning is true, to avoid
			// visual interleaving with the active task's renderer output.
			fmt.Print("> ")
			line, err := reader.ReadString('\n')
			if err != nil {
				lineCh <- inputLine{err: err}
				return
			}
			// Drain any remaining buffered data so multi-line pastes are captured whole.
			for reader.Buffered() > 0 {
				more, readErr := reader.ReadString('\n')
				line += more
				if readErr != nil {
					break
				}
			}
			line = strings.TrimRight(line, "\r\n")

			// Multiline mode: "<<<" (alone or as prefix) starts accumulation; a lone "." line submits.
			if strings.HasPrefix(strings.TrimSpace(line), "<<<") {
				var parts []string
				if first := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "<<<")); first != "" {
					parts = append(parts, first)
				}
				for {
					fmt.Print("... ")
					ml, mlErr := reader.ReadString('\n')
					if mlErr != nil {
						break
					}
					ml = strings.TrimRight(ml, "\r\n")
					if ml == "." {
						break
					}
					parts = append(parts, ml)
				}
				line = strings.Join(parts, "\n")
			}

			lineCh <- inputLine{text: line}
		}
	}()

	for {
		select {
		case in := <-lineCh:
			if in.err != nil {
				fmt.Println("\nbye.")
				return
			}
			line := in.text
			if strings.TrimSpace(line) == "" {
				continue
			}

			if strings.TrimSpace(line) == "undo" {
				if taskRunning.Load() {
					fmt.Println("cannot undo while a task is running")
					continue
				}
				handleUndo(cwd, sessionHistory, renderer)
				continue
			}

			if strings.TrimSpace(line) == "queue list" {
				queueMu.Lock()
				if len(taskQueue) == 0 {
					fmt.Println("queue is empty")
				} else {
					for i, t := range taskQueue {
						fmt.Printf("%d. %s\n", i+1, t)
					}
				}
				queueMu.Unlock()
				continue
			}

			if strings.HasPrefix(strings.TrimSpace(line), "queue ") {
				// Explicit `queue "<task>"` — strip prefix, enqueue regardless of run state.
				queued := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "queue "))
				queued = strings.Trim(queued, `"`)
				if queued == "" {
					fmt.Println(`usage: queue "<task description>"`)
					continue
				}
				queueMu.Lock()
				taskQueue = append(taskQueue, queued)
				pos := len(taskQueue)
				queueMu.Unlock()
				fmt.Printf("queued (position %d)\n", pos)
				continue
			}

			if taskRunning.Load() {
				// A task is currently executing — enqueue instead of running now.
				queueMu.Lock()
				taskQueue = append(taskQueue, line)
				pos := len(taskQueue)
				queueMu.Unlock()
				fmt.Printf("task in progress — queued (position %d)\n", pos)
				continue
			}

			runQueuedTask(line)
		}
	}
}
