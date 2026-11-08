// Command stream demonstrates streaming responses: text arrives chunk by
// chunk through a callback while the agent executes tool calls internally.
//
// Run: go run ./examples/stream "Tell me a short story"
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"agent/agent"
	"agent/config"
)

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ag, err := agent.New(cfg)
	if err != nil {
		log.Fatalf("create agent: %v", err)
	}

	prompt := "Write a two-sentence story about a robot."
	if len(os.Args) > 1 {
		prompt = os.Args[1]
	}

	fmt.Print("Agent: ")
	resp, err := ag.ProcessStream(context.Background(), prompt, func(chunk string) {
		fmt.Print(chunk)
	})
	fmt.Println()

	if err != nil {
		log.Fatalf("stream: %v", err)
	}

	fmt.Printf("\n[full response: %d chars, tokens: %d]\n", len(resp), ag.Usage().TotalTokens)
}
