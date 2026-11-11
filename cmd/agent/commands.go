package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"agent/agent"
	"agent/config"
	"agent/history"
	"agent/llm"
	"agent/server"
	"agent/structured"
	"agent/tools"
	"agent/vision"
)

// command describes a slash-command handled by the REPL.
type command struct {
	pattern *regexp.Regexp
	help    string
	// handler executes the command and returns true when the REPL must stop.
	handler func(r *repl, input string) bool
}

// repl holds the interactive session state.
type repl struct {
	cfg     *config.Config
	ag      *agent.Agent
	store   *history.Store
	convID  int64
	scanner *bufio.Scanner
}

// commandTable returns the command table processed in order of definition.
// It is a function (not a package-level var) to avoid an initialization
// cycle: the /help handler prints the table via printHelp.
func commandTable() []command {
	return []command{
		{
			pattern: regexp.MustCompile(`^/?(exit|quit|bye)$`),
			help:    "/exit              - Exit the agent",
			handler: func(r *repl, input string) bool {
				fmt.Println("\nGoodbye!")
				return true
			},
		},
		{
			pattern: regexp.MustCompile(`^/help$`),
			help:    "/help              - Show this help message",
			handler: func(r *repl, input string) bool {
				printHelp()
				return false
			},
		},
		{
			pattern: regexp.MustCompile(`^/clear$`),
			help:    "/clear             - Start a new conversation",
			handler: func(r *repl, input string) bool {
				r.newConversation("")
				fmt.Println("\n✓ New conversation started")
				return false
			},
		},
		{
			pattern: regexp.MustCompile(`^/conversations?$`),
			help:    "/conversations     - List all conversations",
			handler: func(r *repl, input string) bool {
				if r.store == nil {
					fmt.Println("History is not available.")
					return false
				}
				listConversations(r.store)
				return false
			},
		},
		{
			pattern: regexp.MustCompile(`^/new(?:\s+(.+))?$`),
			help:    "/new [name]        - Create a new conversation",
			handler: func(r *repl, input string) bool {
				matches := regexp.MustCompile(`^/new(?:\s+(.+))?$`).FindStringSubmatch(input)
				name := ""
				if len(matches) >= 2 {
					name = strings.TrimSpace(matches[1])
				}
				r.newConversation(name)
				if name != "" {
					fmt.Printf("\n✓ New conversation '%s' created (ID: %d)\n\n", name, r.convID)
				} else {
					fmt.Printf("\n✓ New conversation created (ID: %d)\n\n", r.convID)
				}
				return false
			},
		},
		{
			pattern: regexp.MustCompile(`^/switch\s+(\d+)$`),
			help:    "/switch <id>       - Switch to a conversation",
			handler: func(r *repl, input string) bool {
				matches := regexp.MustCompile(`^/switch\s+(\d+)$`).FindStringSubmatch(input)
				if len(matches) < 2 {
					fmt.Println("Usage: /switch <conversation_id>")
					return false
				}
				id, err := strconv.ParseInt(matches[1], 10, 64)
				if err != nil {
					fmt.Println("Invalid conversation ID.")
					return false
				}
				if r.store == nil {
					fmt.Println("History is not available.")
					return false
				}
				conv, err := r.store.GetConversation(id)
				if err != nil {
					fmt.Printf("Conversation %d not found.\n", id)
					return false
				}
				r.convID = id
				r.store.SetCurrentConversation(id)
				r.loadHistory()
				name := conv.Name
				if name == "" {
					name = "(unnamed)"
				}
				fmt.Printf("\n✓ Switched to conversation '%s' (ID: %d, %d messages)\n\n", name, conv.ID, conv.MessageCount)
				return false
			},
		},
		{
			pattern: regexp.MustCompile(`^/rename\s+(\d+)\s+(.+)$`),
			help:    "/rename <id> <name>- Rename a conversation",
			handler: func(r *repl, input string) bool {
				matches := regexp.MustCompile(`^/rename\s+(\d+)\s+(.+)$`).FindStringSubmatch(input)
				if len(matches) < 3 {
					fmt.Println("Usage: /rename <conversation_id> <new_name>")
					return false
				}
				id, _ := strconv.ParseInt(matches[1], 10, 64)
				newName := strings.TrimSpace(matches[2])
				if r.store == nil {
					fmt.Println("History is not available.")
					return false
				}
				if _, err := r.store.GetConversation(id); err != nil {
					fmt.Printf("Conversation %d not found.\n", id)
					return false
				}
				if err := r.store.RenameConversation(id, newName); err != nil {
					fmt.Printf("Error renaming conversation: %v\n", err)
					return false
				}
				fmt.Printf("\n✓ Conversation renamed to '%s'\n\n", newName)
				return false
			},
		},
		{
			pattern: regexp.MustCompile(`^/delete\s+(\d+)$`),
			help:    "/delete <id>       - Delete a conversation",
			handler: func(r *repl, input string) bool {
				matches := regexp.MustCompile(`^/delete\s+(\d+)$`).FindStringSubmatch(input)
				if len(matches) < 2 {
					fmt.Println("Usage: /delete <conversation_id>")
					return false
				}
				id, err := strconv.ParseInt(matches[1], 10, 64)
				if err != nil {
					fmt.Println("Invalid conversation ID.")
					return false
				}
				if r.store == nil {
					fmt.Println("History is not available.")
					return false
				}
				if id == r.store.CurrentConversation() {
					fmt.Println("Cannot delete the current conversation. Switch to another first.")
					return false
				}
				if _, err := r.store.GetConversation(id); err != nil {
					fmt.Printf("Conversation %d not found.\n", id)
					return false
				}
				if err := r.store.DeleteConversation(id); err != nil {
					fmt.Printf("Error deleting conversation: %v\n", err)
					return false
				}
				fmt.Printf("\n✓ Conversation %d deleted\n\n", id)
				return false
			},
		},
		{
			pattern: regexp.MustCompile(`^/model(?:\s+(\S+))?$`),
			help:    "/model [name]      - Show or switch the LLM model",
			handler: (*repl).handleModel,
		},
		{
			pattern: regexp.MustCompile(`^/usage$`),
			help:    "/usage             - Show session token usage",
			handler: (*repl).handleUsage,
		},
		{
			pattern: regexp.MustCompile(`^/remember\s+(.+)$`),
			help:    "/remember <text>   - Save a long-term memory",
			handler: (*repl).handleRemember,
		},
		{
			pattern: regexp.MustCompile(`^/memories$`),
			help:    "/memories          - List long-term memories",
			handler: (*repl).handleMemories,
		},
		{
			pattern: regexp.MustCompile(`^/schema\s+(\S+)\s*(.*)$`),
			help:    "/schema <file> <prompt> - Structured output via JSON schema",
			handler: (*repl).handleSchema,
		},
		{
			pattern: regexp.MustCompile(`^/forget$`),
			help:    "/forget            - Delete all long-term memories",
			handler: (*repl).handleForget,
		},
		{
			pattern: regexp.MustCompile(`^/plan\s+(.+)$`),
			help:    "/plan <task>       - Plan and execute a multi-step task",
			handler: (*repl).handlePlan,
		},
		{
			pattern: regexp.MustCompile(`^/team\s+(.+)$`),
			help:    "/team <task>       - Run planner/executor/critic team",
			handler: (*repl).handleTeam,
		},
		{
			pattern: regexp.MustCompile(`^/export\s+(\d+)(?:\s+(\w+))?$`),
			help:    "/export <id> [md|json] - Export a conversation",
			handler: (*repl).handleExport,
		},
		{
			pattern: regexp.MustCompile(`^/serve$`),
			help:    "/serve             - Start the HTTP API server",
			handler: (*repl).handleServe,
		},
		{
			pattern: regexp.MustCompile(`^/stream$`),
			help:    "/stream            - Streaming mode (supports tools)",
			handler: (*repl).handleStream,
		},
		{
			pattern: regexp.MustCompile(`^/vision\s+(\S+)\s*(.*)$`),
			help:    "/vision <path> <q> - Analyze an image",
			handler: (*repl).handleVision,
		},
	}
}

// newRepl creates a new REPL session.
func newRepl(cfg *config.Config, ag *agent.Agent, store *history.Store) *repl {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	r := &repl{
		cfg:     cfg,
		ag:      ag,
		store:   store,
		scanner: scanner,
	}

	// Wire the confirmation policy for dangerous tool operations.
	mode := strings.ToLower(strings.TrimSpace(cfg.Tools.Confirm))
	if mode == "" {
		mode = string(tools.ConfirmAsk)
	}
	ag.SetToolPolicy(mode, r.askConfirmation)

	if store != nil {
		convID, _ := store.NewConversation()
		store.SetCurrentConversation(convID)
		r.convID = convID
	}
	return r
}

// askConfirmation prompts the user to approve a dangerous tool operation.
// Used when tools.confirm is "ask".
func (r *repl) askConfirmation(toolName, action, detail string) bool {
	fmt.Printf("\n⚠️  Tool '%s' requests a dangerous operation:\n", toolName)
	fmt.Printf("   action: %s\n   detail: %s\n", action, detail)
	fmt.Print("   Approve? [y/N]: ")

	line := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			line <- strings.TrimSpace(scanner.Text())
		} else {
			line <- ""
		}
	}()

	select {
	case answer := <-line:
		answer = strings.ToLower(answer)
		return answer == "y" || answer == "yes" || answer == "д" || answer == "да"
	case <-time.After(2 * time.Minute):
		fmt.Println("\n   (no answer — denied)")
		return false
	}
}

// Run starts the interactive read-eval-print loop.
func (r *repl) Run() {
	for {
		fmt.Print("You: ")
		if !r.scanner.Scan() {
			break
		}

		input := strings.TrimSpace(r.scanner.Text())
		if input == "" {
			continue
		}

		if r.dispatch(input) {
			break
		}
	}

	if err := r.scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Input error: %v\n", err)
		os.Exit(1)
	}
}

// dispatch matches input against the command table. Returns true when the
// REPL must stop. Unrecognized input is sent to the agent as a chat message.
func (r *repl) dispatch(input string) bool {
	for _, cmd := range commandTable() {
		if cmd.pattern.MatchString(input) {
			return cmd.handler(r, input)
		}
	}
	r.handleChat(input)
	return false
}

// newConversation starts a fresh conversation and clears the agent context.
func (r *repl) newConversation(name string) {
	if r.store != nil {
		convID, err := r.store.NewConversation()
		if err == nil && name != "" {
			_ = r.store.RenameConversation(convID, name)
		}
		r.store.SetCurrentConversation(convID)
		r.convID = convID
	}
	r.ag.ClearHistory()
}

// loadHistory loads the current conversation's messages from SQLite into the
// agent context, so the LLM sees the previous dialogue.
func (r *repl) loadHistory() {
	if r.store == nil || r.convID <= 0 {
		return
	}
	msgs, err := r.store.GetMessages(r.convID, r.cfg.Agent.MaxHistoryMessages)
	if err != nil {
		return
	}
	loaded := make([]llm.Message, 0, len(msgs))
	for _, m := range msgs {
		loaded = append(loaded, llm.Message{
			Role:    m.Role,
			Name:    m.Name,
			Content: m.Content,
		})
	}
	r.ag.LoadHistory(loaded)
}

// saveMessage persists a message to the current conversation.
func (r *repl) saveMessage(role, content string) {
	if r.store == nil || r.convID <= 0 {
		return
	}
	_ = r.store.SaveMessage(r.convID, history.Message{
		Role:    role,
		Content: content,
	})
}

// handleChat sends the input to the agent and shows a loading animation
// while waiting for the response.
func (r *repl) handleChat(input string) {
	r.saveMessage("user", input)

	fmt.Print("Agent: ")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		response, err := r.ag.Process(ctx, input)
		if err != nil {
			fmt.Printf("\r   \n❌ Error: %v\n\n", err)
		} else {
			fmt.Println("\r   \n" + r.ag.Renderer().Render(response) + "\n")
			r.saveMessage("assistant", response)
		}
		cancel()
	}()

	// Loading animation
	dots := 0
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			dots = (dots + 1) % 4
			fmt.Printf("\r%s", strings.Repeat(".", dots))
		case <-ctx.Done():
			ticker.Stop()
			return
		}
	}
}

// handleStream enables streaming mode for the next message.
func (r *repl) handleStream(input string) bool {
	fmt.Println("\nStreaming mode enabled. Type your message:")
	if !r.scanner.Scan() {
		return true
	}
	msg := strings.TrimSpace(r.scanner.Text())
	if msg == "" {
		return false
	}
	fmt.Println()

	r.saveMessage("user", msg)

	fmt.Print("Agent: ")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var full strings.Builder
	response, err := r.ag.ProcessStream(ctx, msg, func(chunk string) {
		fmt.Print(chunk)
		full.WriteString(chunk)
	})
	fmt.Println()

	if err != nil {
		fmt.Println("❌ Error:", err.Error())
		return false
	}

	r.saveMessage("assistant", response)
	return false
}

// handleModel shows the current model and available models, or switches
// the model: /model [name].
func (r *repl) handleModel(input string) bool {
	pattern := regexp.MustCompile(`^/model(?:\s+(\S+))?$`)
	matches := pattern.FindStringSubmatch(input)

	// No argument: show the current model and the available list.
	if len(matches) < 2 || matches[1] == "" {
		fmt.Printf("\n🤖 Current model: %s\n", r.ag.Model())

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		models, err := r.ag.Client().Models(ctx)
		if err != nil || len(models) == 0 {
			// Fallback to the known models list.
			fmt.Println("Available models (built-in list, API unavailable):")
			for _, m := range llm.KnownModels() {
				marker := " "
				if m == r.ag.Model() {
					marker = ">"
				}
				fmt.Printf("  %s %s\n", marker, m)
			}
			if err != nil {
				fmt.Printf("  (could not fetch from API: %v)\n", err)
			}
			fmt.Println()
			return false
		}

		fmt.Println("Available models:")
		for _, m := range models {
			marker := " "
			if m.ID == r.ag.Model() {
				marker = ">"
			}
			fmt.Printf("  %s %s\n", marker, m.ID)
		}
		fmt.Println("\nSwitch with: /model <name>")
		fmt.Println()
		return false
	}

	// Switch the model.
	newModel := matches[1]
	oldModel := r.ag.Model()
	if newModel == oldModel {
		fmt.Printf("\n✓ Already using %s\n\n", newModel)
		return false
	}

	r.ag.SetModel(newModel)
	fmt.Printf("\n✓ Model switched: %s → %s\n\n", oldModel, newModel)
	return false
}

// handlePlan generates and optionally executes a plan: /plan <task>.
func (r *repl) handlePlan(input string) bool {
	pattern := regexp.MustCompile(`^/plan\s+(.+)$`)
	matches := pattern.FindStringSubmatch(input)
	if len(matches) < 2 {
		fmt.Println("Usage: /plan <task>")
		return false
	}
	task := strings.TrimSpace(matches[1])

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Println("\n📋 Generating plan...")
	steps, err := r.ag.Plan(ctx, task)
	if err != nil {
		fmt.Println("❌ Planning failed:", err.Error())
		return false
	}
	if len(steps) == 0 {
		fmt.Println("❌ Empty plan.")
		return false
	}

	fmt.Println("\n📋 Plan:")
	for i, step := range steps {
		fmt.Printf("  %d. %s\n", i+1, step)
	}
	fmt.Print("\nExecute the plan? [y/N]: ")
	if !r.scanner.Scan() {
		return true
	}
	answer := strings.ToLower(strings.TrimSpace(r.scanner.Text()))
	if answer != "y" && answer != "yes" && answer != "д" && answer != "да" {
		fmt.Println("Plan not executed.")
		return false
	}

	result, err := r.ag.ExecutePlan(ctx, steps, func(done, total int, step string) {
		fmt.Printf("\r⚙️  Step %d/%d: %s   ", done, total, step)
	})
	fmt.Println()
	if err != nil {
		fmt.Println("❌", err.Error())
	}
	if result == nil {
		return false
	}

	fmt.Println("\n✅ Execution results:")
	for i, res := range result.Results {
		fmt.Printf("  %d. %s\n→ %s\n", i+1, result.Steps[i], r.ag.Renderer().Render(res))
	}
	fmt.Println()
	return false
}

// handleTeam runs the planner/executor/critic pipeline: /team <task>.
func (r *repl) handleTeam(input string) bool {
	pattern := regexp.MustCompile(`^/team\s+(.+)$`)
	matches := pattern.FindStringSubmatch(input)
	if len(matches) < 2 {
		fmt.Println("Usage: /team <task>")
		return false
	}
	task := strings.TrimSpace(matches[1])

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Println("\n🤝 Multi-agent run...")
	team, err := r.ag.RunTeam(ctx, task, func(stage string) {
		fmt.Printf("\r⚙️  stage: %-40s", stage)
	})
	fmt.Println()
	if err != nil {
		fmt.Println("❌", err.Error())
		return false
	}

	fmt.Println("\n📋 Plan:")
	for i, step := range team.Plan {
		fmt.Printf("  %d. %s\n", i+1, step)
	}
	fmt.Println("\n✅ Answer:")
	fmt.Println(r.ag.Renderer().Render(team.Answer))
	fmt.Printf("\n🧐 Critic: %s — %s (revised: %v)\n\n", team.CriticVerdict, team.CriticNotes, team.Revised)
	return false
}

// handleExport exports a conversation: /export <id> [markdown|json].
func (r *repl) handleExport(input string) bool {
	pattern := regexp.MustCompile(`^/export\s+(\d+)(?:\s+(\w+))?$`)
	matches := pattern.FindStringSubmatch(input)
	if len(matches) < 2 {
		fmt.Println("Usage: /export <conversation_id> [markdown|json]")
		return false
	}

	id, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil {
		fmt.Println("Invalid conversation ID.")
		return false
	}
	format := strings.ToLower(matches[2])
	if format == "" {
		format = "markdown"
	}
	if format != "markdown" && format != "md" && format != "json" {
		fmt.Println("Unknown format:", format, "(use markdown or json)")
		return false
	}

	if r.store == nil {
		fmt.Println("❌ History store is not available.")
		return false
	}

	msgs, err := r.store.GetMessages(id, 10000)
	if err != nil {
		fmt.Printf("❌ Conversation %d not found.\n", id)
		return false
	}

	var out strings.Builder
	var fileExt string
	if format == "json" {
		type exportMsg struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		items := make([]exportMsg, 0, len(msgs))
		for _, m := range msgs {
			items = append(items, exportMsg{Role: m.Role, Content: m.Content})
		}
		data, _ := json.MarshalIndent(items, "", "  ")
		out.Write(data)
		fileExt = "json"
	} else {
		out.WriteString(fmt.Sprintf("# Conversation %d\n\n", id))
		for _, m := range msgs {
			switch m.Role {
			case "user":
				out.WriteString(fmt.Sprintf("## 👤 User\n\n%s\n\n", m.Content))
			case "assistant":
				out.WriteString(fmt.Sprintf("## 🤖 Agent\n\n%s\n\n", m.Content))
			default:
				out.WriteString(fmt.Sprintf("## %s\n\n%s\n\n", m.Role, m.Content))
			}
		}
		fileExt = "md"
	}

	fileName := fmt.Sprintf("export_%d.%s", id, fileExt)
	if err := os.WriteFile(fileName, []byte(out.String()), 0644); err != nil {
		fmt.Println("❌ Failed to write export file:", err.Error())
		return false
	}
	fmt.Printf("\n✓ Exported conversation %d to %s (%d messages)\n\n", id, fileName, len(msgs))
	return false
}

// handleServe starts the HTTP API server and blocks.
func (r *repl) handleServe(input string) bool {
	port := r.cfg.Server.Port
	if port <= 0 {
		port = 8080
	}
	addr := fmt.Sprintf(":%d", port)

	srv := server.New(r.cfg)
	fmt.Printf("\n🌐 HTTP API server listening on %s\n", addr)
	fmt.Println("   POST /v1/chat         {\"message\": \"...\", \"session_id\": \"...\"}")
	fmt.Println("   POST /v1/chat/stream  (SSE)")
	fmt.Println("   GET  /healthz")
	fmt.Println("\nPress Ctrl+C to stop.")

	if err := srv.ListenAndServe(addr); err != nil {
		fmt.Println("❌ Server error:", err.Error())
	}
	return true
}

// handleForget deletes all long-term memories.
func (r *repl) handleForget(input string) bool {
	if r.store == nil {
		fmt.Println("❌ History store is not available.")
		return false
	}
	if err := r.store.DeleteMemories(); err != nil {
		fmt.Println("❌ Failed to delete memories:", err.Error())
		return false
	}
	fmt.Println("\n✓ All long-term memories deleted")
	fmt.Println()
	return false
}

// handleUsage shows accumulated session token usage.
func (r *repl) handleUsage(input string) bool {
	u := r.ag.Usage()
	fmt.Println("\n📊 Session token usage:")
	fmt.Printf("  prompt tokens:     %d\n", u.PromptTokens)
	fmt.Printf("  completion tokens: %d\n", u.CompletionTokens)
	fmt.Printf("  total tokens:      %d\n", u.TotalTokens)
	fmt.Println()
	return false
}

// handleRemember saves a long-term memory with its embedding.
func (r *repl) handleRemember(input string) bool {
	pattern := regexp.MustCompile(`^/remember\s+(.+)$`)
	matches := pattern.FindStringSubmatch(input)
	if len(matches) < 2 {
		fmt.Println("Usage: /remember <text>")
		return false
	}
	text := strings.TrimSpace(matches[1])

	if r.store == nil {
		fmt.Println("❌ History store is not available.")
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	vec, err := r.ag.Client().Embed(ctx, text)
	if err != nil {
		fmt.Printf("❌ Embedding failed: %v\n", err)
		return false
	}

	if err := r.store.SaveMemory(text, vec); err != nil {
		fmt.Printf("❌ Failed to save memory: %v\n", err)
		return false
	}

	fmt.Printf("\n✓ Remembered: %s\n\n", text)
	return false
}

// handleMemories lists long-term memories.
func (r *repl) handleMemories(input string) bool {
	if r.store == nil {
		fmt.Println("❌ History store is not available.")
		return false
	}

	memories, err := r.store.ListMemories(20)
	if err != nil {
		fmt.Println("❌ Failed to load memories:", err.Error())
		return false
	}

	if len(memories) == 0 {
		fmt.Println("\nNo memories saved. Use /remember <text>.")
		fmt.Println()
		return false
	}

	fmt.Println("\n🧠 Long-term memories:")
	for _, m := range memories {
		fmt.Printf("  [%d] %s (%s)\n", m.ID, m.Text, m.CreatedAt.Format("2006-01-02"))
	}
	fmt.Println("\nUse /forget to delete all memories.")
	fmt.Println()
	return false
}

// handleSchema generates a structured JSON answer validated against a schema
// file: /schema <file> [prompt].
func (r *repl) handleSchema(input string) bool {
	pattern := regexp.MustCompile(`^/schema\s+(\S+)\s*(.*)$`)
	matches := pattern.FindStringSubmatch(input)
	if len(matches) < 2 {
		fmt.Println("Usage: /schema <schema_file.json> [prompt]")
		return false
	}

	schemaPath := matches[1]
	prompt := strings.TrimSpace(matches[2])

	data, err := os.ReadFile(schemaPath)
	if err != nil {
		fmt.Println("❌ Cannot read schema file:", err.Error())
		return false
	}

	var schema structured.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		fmt.Println("❌ Invalid schema JSON:", err.Error())
		return false
	}
	validator := structured.NewValidator(schema)

	if prompt == "" {
		fmt.Print("What should the JSON describe? ")
		if !r.scanner.Scan() {
			return true
		}
		prompt = strings.TrimSpace(r.scanner.Text())
	}

	schemaJSON, _ := json.Marshal(schema)
	messages := []llm.Message{
		{
			Role: "system",
			Content: "You are a JSON generation assistant. Respond ONLY with a single JSON object " +
				"that validates against this JSON Schema. No explanations, no markdown:\n" +
				string(schemaJSON),
		},
		{Role: "user", Content: prompt},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resp, err := r.ag.Client().Chat(ctx, messages, nil)
	if err != nil {
		fmt.Println("❌ Error:", err.Error())
		return false
	}

	result, err := validator.Validate([]byte(stripCodeFences(resp.Content)))
	if err != nil {
		fmt.Println("\n❌ Schema validation failed:", err.Error())
		fmt.Println("Raw response:", resp.Content)
		return false
	}

	out, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println("\n✓ Valid JSON:")
	fmt.Println(string(out))
	fmt.Println()
	return false
}

// stripCodeFences removes markdown code fences around a JSON payload.
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

// handleVision analyzes an image: /vision <path> [question].
func (r *repl) handleVision(input string) bool {
	pattern := regexp.MustCompile(`^/vision\s+(\S+)\s*(.*)$`)
	matches := pattern.FindStringSubmatch(input)
	if len(matches) < 2 {
		fmt.Println("Usage: /vision <image_path> <question>")
		return false
	}

	imagePath := matches[1]
	image, err := vision.LoadImage(imagePath)
	if err != nil {
		fmt.Println("❌ Error loading image:", err.Error())
		return false
	}

	question := strings.TrimSpace(matches[2])
	if question == "" {
		fmt.Print("Question about image: ")
		if !r.scanner.Scan() {
			return true
		}
		question = strings.TrimSpace(r.scanner.Text())
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	messages := []llm.Message{
		{Role: "system", Content: r.cfg.Agent.SystemPrompt},
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
	resp, err := r.ag.Client().Chat(ctx, messages, nil)
	if err != nil {
		fmt.Println("\n❌ Error:", err.Error())
		return false
	}

	fmt.Println("\n" + r.ag.Renderer().Render(resp.Content) + "\n")
	return false
}

// showPreviousConversations displays a list of previous conversations on startup.
func (r *repl) showPreviousConversations() {
	if r.store == nil {
		return
	}

	convs, err := r.store.ListConversations()
	if err != nil || len(convs) <= 1 {
		// -1 because we just created one
		return
	}

	fmt.Println("📚 Previous conversations:")
	shown := 0
	for _, c := range convs {
		if c.ID == r.convID {
			continue // skip the one we just created
		}
		if shown >= 5 {
			break
		}
		name := c.Name
		if name == "" {
			name = "(unnamed)"
		}
		fmt.Printf("  [%d] %s (%d messages)\n", c.ID, name, c.MessageCount)
		shown++
	}
	fmt.Println()
}

// listConversations displays all conversations.
func listConversations(store *history.Store) {
	convs, err := store.ListConversations()
	if err != nil {
		fmt.Println("Error loading conversations:", err.Error())
		return
	}

	if len(convs) == 0 {
		fmt.Println("No conversations found.")
		return
	}

	fmt.Println("\n📚 Conversations:")
	fmt.Println(strings.Repeat("-", 60))
	for _, c := range convs {
		name := c.Name
		if name == "" {
			name = "(unnamed)"
		}
		current := ""
		if c.ID == store.CurrentConversation() {
			current = " ← current"
		}
		fmt.Printf("  [%d] %s (%d messages) %s\n", c.ID, name, c.MessageCount, current)
	}
	fmt.Println()
}

func printHelp() {
	fmt.Println("\nCommands:")
	for _, cmd := range commandTable() {
		fmt.Println("  " + cmd.help)
	}
	fmt.Println()
}
