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
	"agent/history"
)

var (
	clearScreenCmd = regexp.MustCompile(`^\/clear$`)
	exitCmd        = regexp.MustCompile(`^\/?(exit|quit|bye)$`)
	helpCmd        = regexp.MustCompile(`^\/help$`)
)

// ANSI color codes
const (
	yellow    = "\033[33m"
	green     = "\033[32m"
	blue      = "\033[34m"
	red       = "\033[31m"
	white     = "\033[37m"
	gray      = "\033[90m"
	reset     = "\033[0m"
	bold      = "\033[1m"
	dim       = "\033[2m"
)

func main() {
	printBanner()

	cfg, err := config.Load("config.yaml")
	if err != nil {
		fmt.Println(gray + "⚠️  Local config not found, trying home directory..." + reset)
		cfg, err = config.Load(config.ConfigPath())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Could not load config: %v\n", err)
			cfg = config.DefaultConfig()
		}
	}

	if cfg.LLM.AuthKey == "" {
		fmt.Println(yellow + "⚠️  Auth key not configured!" + reset)
		fmt.Println("   Edit config.yaml and set auth_key.")
		fmt.Println()
	}

	fmt.Printf(white + "✓" + reset + " Config loaded from: %s\n", "config.yaml")
	fmt.Printf(white + "✓" + reset + " Model: %s\n", cfg.LLM.Model)
	fmt.Printf(white + "✓" + reset + " AuthKey configured: %v\n", cfg.LLM.AuthKey != "")
	fmt.Println()

	// Initialize history store
	store, err := history.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, gray+"Warning: Could not initialize history: %v\n"+reset, err)
		store = nil
	}
	if store != nil {
		defer store.Close()
	}

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
		fmt.Println("\n\n" + gray + "Exiting..." + reset)
		os.Exit(0)
	}()

	// Track conversation ID
	convID, err := store.NewConversation()
	if err != nil {
		convID = 0
	}

	for {
		fmt.Print(green + "You: " + reset)
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
			if store != nil {
				convID, _ = store.NewConversation()
			}
			fmt.Println("\n" + gray + "✓ New conversation started" + reset + "\n")
			continue
		}

		if exitCmd.MatchString(input) {
			fmt.Println("\n" + gray + "Goodbye!" + reset)
			break
		}

		if helpCmd.MatchString(input) {
			printHelp()
			continue
		}

		// Save user message to history
		if store != nil && convID > 0 {
			store.SaveMessage(convID, history.Message{
				Role:    "user",
				Content: input,
			})
		}

		// Process the message with loading animation
		fmt.Print(blue + "Agent: " + reset)

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			response, err := ag.Process(ctx, input)
			if err != nil {
				fmt.Printf("\n" + red + "❌ Error: %v" + reset + "\n\n", err)
			} else {
				fmt.Printf("\n%s\n\n", response)
			}

			// Save assistant message to history
			if store != nil && convID > 0 && err == nil {
				store.SaveMessage(convID, history.Message{
					Role:    "assistant",
					Content: response,
				})
			}
			cancel()
		}()

		// Loading animation
		dots := 0
		ticker := time.NewTicker(150 * time.Millisecond)

		for {
			select {
			case <-ticker.C:
				dots = (dots + 1) % 4
				fmt.Printf("\r" + dim + "%s" + reset, strings.Repeat(".", dots))
			case <-ctx.Done():
				ticker.Stop()
				goto nextInput
			}
		}
	nextInput:
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Input error: %v\n", err)
		os.Exit(1)
	}
}

func printBanner() {
	fmt.Println(bold + "╔══════════════════════════════════════╗" + reset)
	fmt.Println(bold + "║     " + white + "AI Agent - Interactive CLI" + bold + "       ║" + reset)
	fmt.Println(bold + "║  " + dim + "Type 'help' for commands" + bold + "             ║" + reset)
	fmt.Println(bold + "╚══════════════════════════════════════╝" + reset)
	fmt.Println()
}

func printHelp() {
	fmt.Println(dim + "Commands:" + reset)
	fmt.Println("  " + bold + "/help" + reset + "      - Show this help message")
	fmt.Println("  " + bold + "/clear" + reset + "     - Clear conversation history")
	fmt.Println("  " + bold + "/exit" + reset + "      - Exit the agent")
	fmt.Println()
	fmt.Println(dim + "Config File:" + reset)
	fmt.Println("  Path: ./config.yaml")
}
