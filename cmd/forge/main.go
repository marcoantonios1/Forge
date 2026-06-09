package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/marcoantonios1/Forge/internal/agent"
	"github.com/marcoantonios1/Forge/internal/confirm"
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

	mode := ui.ModeHuman
	if *debugFlag {
		mode = ui.ModeDebug
	}
	renderer := ui.New(os.Stdout, mode)

	id := session.NewID()
	fmt.Printf("Forge — session %s\n", id)

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not determine working directory: %v\n", err)
	} else {
		cfg, err := projectconfig.Load(cwd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		}
		if cfg != nil {
			fmt.Printf("Loaded forge.md from %s\n", cfg.Path)
		}
	}

	// Confirmer selection — policy-based switching (autonomous vs safe/supervised)
	// happens per-task in the REPL wiring ticket. For now, --yes selects
	// AutoConfirmer; otherwise SafeConfirmer is the default.
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
	_ = confirmer // consumed by agent in REPL wiring ticket

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT)
	go func() {
		<-sigs
		fmt.Println("\nbye.")
		os.Exit(0)
	}()

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("> ")
		line, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("\nbye.")
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			continue
		}
		fmt.Printf("task received: %s\n", line)
	}
}
