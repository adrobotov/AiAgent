package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"agent/agent"
	"agent/config"
	"agent/history"
)

// sessionFile is the name of the file used to persist the current session.
const sessionFile = "session.json"

func main() {
	// --version prints the build version and exits.
	for _, arg := range os.Args[1:] {
		if arg == "--version" || arg == "-v" {
			printVersion()
			return
		}
	}

	printBanner()

	cfg, err := config.Load("config.yaml")
	if err != nil {
		fmt.Println("⚠️  Local config not found, trying home directory...")
		cfg, err = config.Load(config.ConfigPath())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Could not load config: %v\n", err)
			cfg = config.DefaultConfig()
		}
	}

	if cfg.LLM.AuthKey == "" {
		fmt.Println("⚠️  Auth key not configured!")
		fmt.Println("   Set auth_key in config.yaml or use the AGENT_AUTH_KEY environment variable.")
		fmt.Println()
	}

	fmt.Println("✓ Config loaded")
	fmt.Println("✓ Model:", cfg.LLM.Model)
	fmt.Println("✓ AuthKey configured:", cfg.LLM.AuthKey != "")
	fmt.Println()

	// Initialize history store
	store, err := history.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Could not initialize history: %v\n", err)
		store = nil
	}
	if store != nil {
		defer func() { _ = store.Close() }()
	}

	ag, err := agent.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create agent: %v\n", err)
		os.Exit(1)
	}

	// Enable long-term memory when configured (requires embeddings API).
	if cfg.Agent.Memory && store != nil {
		ag.SetMemory(store)
		fmt.Println("✓ Long-term memory enabled")
	}

	// Handle Ctrl+C gracefully
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\n\nExiting...")
		os.Exit(0)
	}()

	r := newRepl(cfg, ag, store)

	// Restore session if available.
	if r.restoreSession() {
		fmt.Println("✓ Session restored from previous run")
	}

	r.showPreviousConversations()
	r.Run()

	// Save session on exit.
	r.saveSession()
}

func printBanner() {
	fmt.Println("╔══════════════════════════════════════╗")
	fmt.Println("║     AI Agent - Interactive CLI       ║")
	fmt.Println("║  Type '/help' for commands           ║")
	fmt.Println("╚══════════════════════════════════════╝")
	fmt.Println()
}
