// main.go is the entry point for the Forge autonomous software engineering agent.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strings"
	"syscall"

	"github.com/marcoantonios1/Forge/internal/agent"
	"github.com/marcoantonios1/Forge/internal/compiler"
	"github.com/marcoantonios1/Forge/internal/config"
	"github.com/marcoantonios1/Forge/internal/confirm"
	"github.com/marcoantonios1/Forge/internal/costguard"
	"github.com/marcoantonios1/Forge/internal/events"
	"github.com/marcoantonios1/Forge/internal/patch"
	"github.com/marcoantonios1/Forge/internal/projectconfig"
	"github.com/marcoantonios1/Forge/internal/session"
	"github.com/marcoantonios1/Forge/internal/tools"
	"github.com/marcoantonios1/Forge/internal/ui"
)

var (
	debugFlag        = flag.Bool("debug", false, "enable debug event output")
	printFlag        = flag.Bool("print", false, "run a task non-interactively and exit")
	outputFlag       = flag.String("output", "text", "output format in --print mode: text or json")
	yesFlag          = flag.Bool("yes", false, "approve all patches without prompting")
	allowedToolsFlag  = flag.String("allowed-tools", "", `comma-separated tool categories to pre-approve.
	Categories: read, git_read, patch. Use "all" to pre-approve everything.
	Example: --allowed-tools=read,git_read`)
	allowMainCommit = flag.Bool("allow-main-commit", false,
		"allow committing directly to main or master (unsafe)")
)

type headlessResult struct {
	Status     string   `json:"status"`     // "success" | "failure" | "rejected"
	Summary    string   `json:"summary"`
	Files      []string `json:"files"`      // relative paths of files patched
	Iterations int      `json:"iterations"`
}

func runHeadless(rawTask, outputFmt string, debug bool) int {
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

	// 4. Project config.
	projectCfg, err := projectconfig.Load(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v\n", err)
	}

	// 5. Costguard client + compiler.
	appCfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		return 1
	}
	cgClient := costguard.New(appCfg)
	comp := compiler.New(cgClient, appCfg.CompilerModel, debug)

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

	// 7. Session history + agent setup.
	sessionHistory := patch.NewPatchHistory()
	ac := agent.NewAgentContext(sessionID, task, cwd, projectCfg, sessionHistory)

	registry := agent.NewRegistry(cwd, emitter, sessionID, nil) // headless: no permission gate
	confirmer := confirm.AutoConfirmer{} // always — no prompts in headless mode
	agentCfg := agent.Config{
		Model:     appCfg.AgentModel,
		MaxIter:   30,
		AutoApply: true,
		Debug:     debug,
	}
	ag := agent.New(agentCfg, cgClient, registry, emitter, confirmer)

	// 8. Run the agent.
	runErr := ag.Run(ctx, ac)

	// Post-task git workflow (headless always auto-commits).
	if runErr == nil && ac.Patches.Len() > 0 {
		if err := runGitWorkflow(ctx, ac, registry, emitter, true); err != nil {
			fmt.Fprintf(os.Stderr, "git workflow: %v\n", err)
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
	ctx      context.Context,
	ac       *agent.AgentContext,
	registry *agent.Registry,
	emitter  events.Emitter,
	auto     bool,
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

func main() {
	flag.Parse()

	if *printFlag {
		task := strings.Join(flag.Args(), " ")
		if strings.TrimSpace(task) == "" {
			fmt.Fprintln(os.Stderr, "forge --print: task argument required")
			os.Exit(2)
		}
		os.Exit(runHeadless(task, *outputFlag, *debugFlag))
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

	// 4. Renderer (is the emitter — never discarded)
	mode := ui.ModeHuman
	if *debugFlag {
		mode = ui.ModeDebug
	}
	renderer := ui.New(os.Stdout, mode)

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

	// 7. Session
	id := session.NewID()
	fmt.Printf("Forge — session %s\n", id)
	if projectCfg != nil {
		fmt.Printf("Loaded forge.md from %s\n", projectCfg.Path)
	}

	// Session-scoped patch history so undo works across tasks.
	// TODO: persist undo history across sessions (currently in-memory only).
	sessionHistory := patch.NewPatchHistory()

	// 8. Confirmer
	var confirmer agent.PatchConfirmer
	if *yesFlag {
		confirmer = confirm.AutoConfirmer{}
	} else {
		confirmer = confirm.NewSafeConfirmer(
			os.Stdin, os.Stderr,
			ui.IsTTY(os.Stdout),
			renderer,
			id,
		)
	}

	// 9. Signal handling
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT)
	go func() {
		<-sigs
		fmt.Println("\nbye.")
		os.Exit(0)
	}()

	// REPL
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("> ")
		line, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("\nbye.")
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if strings.TrimSpace(line) == "" {
			continue
		}

		if strings.TrimSpace(line) == "undo" {
			handleUndo(cwd, sessionHistory, renderer)
			continue
		}

		ctx := context.Background()

		// Compile
		task, err := comp.Compile(ctx, line)
		if err != nil {
			var re *compiler.RejectionError
			if errors.As(err, &re) {
				fmt.Fprintf(os.Stderr, "rejected: %s\n", re.Reason)
			} else {
				fmt.Fprintf(os.Stderr, "error: %s\n", err)
			}
			continue
		}

		fmt.Fprintf(os.Stderr, "compiled: %s/%s [%s]\n",
			task.Category, task.Scope, task.ExecutionPolicy)

		// Run agent
		agentCfg := agent.Config{
			Model:   cfg.AgentModel,
			MaxIter: 30,
			Debug:   cfg.Debug,
		}
		preApproved := confirm.ParseAllowedTools(*allowedToolsFlag)
		interactive := task.ExecutionPolicy != compiler.PolicyAutonomous && !*yesFlag
		gate := confirm.NewPermissionGate(
			os.Stdin, os.Stderr,
			ui.IsTTY(os.Stdout),
			*debugFlag,
			renderer,
			id,
			preApproved,
			interactive,
		)
		registry := agent.NewRegistry(cwd, renderer, id, gate)
		a := agent.New(agentCfg, client, registry, renderer, confirmer)
		ac := agent.NewAgentContext(id, task, cwd, projectCfg, sessionHistory)

		if err := a.Run(ctx, ac); err != nil {
			fmt.Fprintf(os.Stderr, "error: %s\n", err)
			continue
		}
		if ac.Patches.Len() > 0 {
			_, auto := confirmer.(confirm.AutoConfirmer)
			if err := runGitWorkflow(ctx, ac, registry, renderer, auto); err != nil {
				fmt.Fprintf(os.Stderr, "git workflow: %v\n", err)
			}
		}
	}
}
