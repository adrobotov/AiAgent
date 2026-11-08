package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"agent/agent"
	"agent/config"
)

var (
	clearScreenCmd = regexp.MustCompile(`^\/clear$`)
	exitCmd        = regexp.MustCompile(`^\/?(exit|quit|bye)$`)
	helpCmd        = regexp.MustCompile(`^\/help$`)
)

func main() {
	fmt.Println("╔══════════════════════════════════════╗")
	fmt.Println("║     AI Agent - Interactive CLI       ║")
	fmt.Println("║  Type 'help' for commands             ║")
	fmt.Println("╚══════════════════════════════════════╝")
	fmt.Println()

	// Try to load config from current directory first, then from home directory
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
		fmt.Println("   Edit config.yaml and set auth_key.")
		fmt.Println()
	}

	fmt.Printf("✓ Config loaded from: %s\n", "config.yaml")
	fmt.Printf("✓ Model: %s\n", cfg.LLM.Model)
	fmt.Printf("✓ AuthKey configured: %v\n", cfg.LLM.AuthKey != "")
	fmt.Println()

	ag, err := agent.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create agent: %v\n", err)
		os.Exit(1)
	}

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	// Handle Ctrl+C gracefully
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\n\nExiting...")
		os.Exit(0)
	}()

	for {
		fmt.Print("You: ")
		if !scanner.Scan() {
			break
		}

		input := scanner.Text()
		input = strings.TrimSpace(input)

		if input == "" {
			continue
		}

		// Check for commands
		if clearScreenCmd.MatchString(input) {
			fmt.Println("\n✓ Conversation cleared\n")
			ag.ClearHistory()
			continue
		}

		if exitCmd.MatchString(input) {
			fmt.Println("\nGoodbye! 👋")
			break
		}

		if helpCmd.MatchString(input) {
			printHelp()
			continue
		}

		// Process the message
		func() {
			fmt.Print("\nAgent: ")

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			done := make(chan string, 1)
			go func() {
				response, err := ag.Process(ctx, input)
				if err != nil {
					fmt.Printf("\n❌ Error: %v\n", err)
					return
				}
				done <- response
			}()

			select {
			case response := <-done:
				fmt.Printf("\n%s\n\n", response)
			case <-ctx.Done():
				return
			case <-time.After(60 * time.Second):
				fmt.Println("\n⏱ Timeout, please try again.")
			}
		}()
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Input error: %v\n", err)
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Println(`
Commands:
  /help      - Show this help message
  /clear     - Clear conversation history
  /exit      - Exit the agent

Config File:
  Path: ./config.yaml
  
  Example config:
    llm:
      base_url: https://api.giga.chat/v1
      model: GigaChat-2-Max
      auth_key: your-authorization-key
      oauth_scope: GIGACHAT_API_PERS
      oauth_rquid: your-rquid-uuid
      temperature: 0.7
      max_tokens: 4096
`)
}
