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
	"github.com/marcoantonios1/Forge/internal/ui"
)

var (
	debugFlag  = flag.Bool("debug", false, "enable debug event output")
	printFlag  = flag.Bool("print", false, "run a task non-interactively and exit")
	outputFlag = flag.String("output", "text", "output format in --print mode: text or json")
	yesFlag    = flag.Bool("yes", false, "approve all patches without prompting")
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

	registry := agent.NewRegistry(cwd, emitter, sessionID)
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
		registry := agent.NewRegistry(cwd, renderer, id)
		a := agent.New(agentCfg, client, registry, renderer, confirmer)
		ac := agent.NewAgentContext(id, task, cwd, projectCfg, sessionHistory)

		if err := a.Run(ctx, ac); err != nil {
			fmt.Fprintf(os.Stderr, "error: %s\n", err)
		}
	}
}
