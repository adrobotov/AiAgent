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
	"agent/llm"
	"agent/vision"
)

var (
	clearScreenCmd = regexp.MustCompile(`^\/clear$`)
	exitCmd        = regexp.MustCompile(`^\/?(exit|quit|bye)$`)
	helpCmd        = regexp.MustCompile(`^\/help$`)
	streamCmd      = regexp.MustCompile(`^\/stream$`)
	visionCmd      = regexp.MustCompile(`^\/vision\s+(.+)$`)
)

func main() {
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
		fmt.Println("   Edit config.yaml and set auth_key.")
		fmt.Println()
	}

	fmt.Println("✓ Config loaded from: config.yaml")
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
		fmt.Println("\n\nExiting...")
		os.Exit(0)
	}()

	// Track conversation ID
	convID, err := store.NewConversation()
	if err != nil {
		convID = 0
	}

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
			if store != nil {
				convID, _ = store.NewConversation()
			}
			fmt.Println("\n✓ New conversation started\n")
			continue
		}

		if exitCmd.MatchString(input) {
			fmt.Println("\nGoodbye!")
			break
		}

		if helpCmd.MatchString(input) {
			printHelp()
			continue
		}

		// Handle /stream command
		if streamCmd.MatchString(input) {
			fmt.Println("\nStreaming mode enabled. Type your message:")
			scanner.Scan()
			streamInput := scanner.Text()
			fmt.Println()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			messages := llm.BuildMessages(cfg.Agent.SystemPrompt, nil, streamInput)
			ch, err := ag.Client().Stream(ctx, messages, nil)
			if err != nil {
				fmt.Println("❌ Error:", err.Error())
				continue
			}

			var fullResponse strings.Builder
			for chunk := range ch {
				if chunk.IsComplete {
					break
				}
				fmt.Print(chunk.Content)
				fullResponse.WriteString(chunk.Content)
			}
			fmt.Println("\n")
			continue
		}

		// Handle /vision command
		if visionCmd.MatchString(input) {
			matches := visionCmd.FindStringSubmatch(input)
			if len(matches) < 2 {
				fmt.Println("Usage: /vision <image_path> <question>")
				continue
			}

			imagePath := matches[1]
			image, err := vision.LoadImage(imagePath)
			if err != nil {
				fmt.Println("❌ Error loading image:", err.Error())
				continue
			}

			// Read next line for question
			fmt.Print("Question about image: ")
			scanner.Scan()
			question := scanner.Text()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Build message with image
			messages := []llm.Message{
				{Role: "system", Content: cfg.Agent.SystemPrompt},
				{
					Role: "user",
					Content: fmt.Sprintf(
						`<img src="%s"> %s`,
						image.ToURL(),
						question,
					),
				},
			}

			fmt.Print("Agent: ")
			resp, err := ag.Client().Chat(ctx, messages, nil)
			if err != nil {
				fmt.Println("\n❌ Error:", err.Error())
				continue
			}

			fmt.Println("\n" + ag.Renderer().Render(resp.Content) + "\n")
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
		fmt.Print("Agent: ")

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			response, err := ag.Process(ctx, input)
			if err != nil {
				fmt.Printf("\n❌ Error: %v\n\n", err)
			} else {
				fmt.Println("\n" + response + "\n")
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
				fmt.Printf("\r%s", strings.Repeat(".", dots))
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
	fmt.Println("╔══════════════════════════════════════╗")
	fmt.Println("║     AI Agent - Interactive CLI       ║")
	fmt.Println("║  Type 'help' for commands             ║")
	fmt.Println("╚══════════════════════════════════════╝")
	fmt.Println()
}

func printHelp() {
	fmt.Println("Commands:")
	fmt.Println("  /help      - Show this help message")
	fmt.Println("  /clear     - Clear conversation history")
	fmt.Println("  /exit      - Exit the agent")
	fmt.Println()
	fmt.Println("Config File:")
	fmt.Println("  Path: ./config.yaml")
}
