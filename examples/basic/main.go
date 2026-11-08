// Command basic demonstrates the minimal use of the agent library:
// load configuration, create an agent, send one message and print the answer.
//
// Configuration is read from config.yaml (or AGENT_* environment variables).
// Run: go run ./examples/basic "What is 2 + 2?"
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

	question := "Hello! Introduce yourself in one sentence."
	if len(os.Args) > 1 {
		question = os.Args[1]
	}

	resp, err := ag.Process(context.Background(), question)
	if err != nil {
		log.Fatalf("process: %v", err)
	}

	fmt.Println(resp)
	fmt.Printf("\n[tokens used: %d]\n", ag.Usage().TotalTokens)
}
