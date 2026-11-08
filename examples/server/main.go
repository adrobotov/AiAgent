// Command server demonstrates running the agent as an HTTP service with
// streaming support. Endpoints: POST /v1/chat, POST /v1/chat/stream (SSE),
// GET /v1/usage, GET /healthz.
//
// The port can be set in config.yaml (server.port) or via the PORT env var.
// Run: go run ./examples/server
package main

import (
	"fmt"
	"log"
	"os"
	"strconv"

	"agent/config"
	"agent/server"
)

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	if port := os.Getenv("PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			cfg.Server.Port = p
		}
	}

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Printf("listening on %s", addr)
	log.Printf("try: curl -X POST %s/v1/chat -d '{\"message\":\"hello\"}'", addr)

	if err := server.New(cfg).ListenAndServe(addr); err != nil {
		log.Fatalf("server: %v", err)
	}
}
