package main

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/marcoantonios1/Forge/internal/projectconfig"
	"github.com/marcoantonios1/Forge/internal/session"
)

func main() {
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
			// EOF (Ctrl+D)
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
