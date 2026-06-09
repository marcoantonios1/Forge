package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/marcoantonios1/Forge/internal/agent"
	"github.com/marcoantonios1/Forge/internal/compiler"
	"github.com/marcoantonios1/Forge/internal/config"
	"github.com/marcoantonios1/Forge/internal/confirm"
	"github.com/marcoantonios1/Forge/internal/costguard"
	"github.com/marcoantonios1/Forge/internal/projectconfig"
	"github.com/marcoantonios1/Forge/internal/session"
	"github.com/marcoantonios1/Forge/internal/ui"
)

var (
	debugFlag = flag.Bool("debug", false, "enable debug event output")
	yesFlag   = flag.Bool("yes", false, "approve all patches without prompting")
)

func main() {
	flag.Parse()

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
			Model:   cfg.CompilerModel,
			MaxIter: 30,
			Debug:   cfg.Debug,
		}
		registry := agent.NewRegistry(cwd, renderer, id)
		a := agent.New(agentCfg, client, registry, renderer, confirmer)
		ac := agent.NewAgentContext(id, task, cwd, projectCfg)

		if err := a.Run(ctx, ac); err != nil {
			fmt.Fprintf(os.Stderr, "error: %s\n", err)
		}
	}
}
