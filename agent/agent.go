package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"strings"
	"sync"
	"time"

	"agent/config"
	"agent/history"
	"agent/llm"
	"agent/markdown"
	"agent/tools"
)

// Agent is the main AI agent that processes user requests.
type Agent struct {
	client     *llm.Client
	config     *config.Config
	tools      *tools.Manager
	history    []llm.Message
	maxTurns   int
	maxHistory int
	renderer   *markdown.Renderer

	// Context management
	contextTokens int  // approx. token budget for session history
	summarize     bool // LLM-summarize old messages when over budget

	// Long-term memory (optional)
	memory *history.Store

	// Token usage tracking
	usageMu sync.Mutex
	usage   llm.Usage

	// Retry / backoff configuration
	retryCount      int           // max retry attempts
	retryMinDelay   time.Duration // initial backoff delay
	retryMaxDelay   time.Duration // maximum backoff delay
	retryBackoffMul float64       // multiplier for exponential backoff
}

// New creates a new Agent instance.
func New(cfg *config.Config) (*Agent, error) {
	maxTurns := cfg.Agent.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 10
	}
	maxHistory := cfg.Agent.MaxHistoryMessages
	if maxHistory <= 0 {
		maxHistory = 20
	}
	contextTokens := cfg.Agent.ContextTokens
	if contextTokens <= 0 {
		contextTokens = 8000
	}

	toolManager := tools.NewManager(cfg.Tools.Root, cfg.Tools.AllowedCommands, cfg.Tools.MaxResultSize, cfg.Tools.WeatherAPIKey, cfg.Tools.AutoConfirm)
	if len(cfg.Tools.Plugins) > 0 {
		toolManager.RegisterPlugins(cfg.Tools.Plugins, func(format string, args ...interface{}) {
			fmt.Fprintf(os.Stderr, "⚠️  "+format+"\n", args...)
		})
	}

	return &Agent{
		client:          llm.NewClient(&cfg.LLM),
		config:          cfg,
		tools:           toolManager,
		maxTurns:        maxTurns,
		maxHistory:      maxHistory,
		contextTokens:   contextTokens,
		summarize:       cfg.Agent.Summarize,
		renderer:        markdown.New(true),
		retryCount:      3,
		retryMinDelay:   500 * time.Millisecond,
		retryMaxDelay:   30 * time.Second,
		retryBackoffMul: 2.0,
	}, nil
}

// SetToolPolicy applies a confirmation policy for dangerous tool operations.
// mode is one of: "ask" (prompt via askFn), "always" (allow), "never" (deny).
func (a *Agent) SetToolPolicy(mode string, askFn func(toolName, action, detail string) bool) {
	a.tools.SetPolicy(tools.Policy{
		Mode: tools.ConfirmationMode(mode),
		Ask:  askFn,
	})
}

// SetMemory enables long-term memory backed by the given store.
func (a *Agent) SetMemory(store *history.Store) {
	a.memory = store
}

// Process sends a user message to the agent and runs the tool-calling loop
// until the model produces a final answer. Returns the raw (unrendered)
// response content.
func (a *Agent) Process(ctx context.Context, userMsg string) (string, error) {
	messages := a.newConversationMessages(ctx, userMsg)
	functions := a.tools.ToFunctionDefinitions()

	for turn := 0; turn < a.maxTurns; turn++ {
		resp, err := a.chatWithRetry(ctx, messages, functions)
		if err != nil {
			return "", fmt.Errorf("chat error: %w", err)
		}
		a.recordUsage(resp.Usage)

		// No function call — this is the final answer.
		if resp.FunctionCall == nil {
			a.appendAssistant(resp.Content)
			return resp.Content, nil
		}

		// Execute the requested tool and feed the result back to the model.
		// GigaChat requires the assistant message preceding the function
		// result to carry the function_call.
		argsJSON := normalizeArguments(resp.FunctionCall.Arguments)

		messages = append(messages, llm.Message{
			Role:    "assistant",
			Content: resp.Content,
			FunctionCall: &llm.FunctionCall{
				Name:      resp.FunctionCall.Name,
				Arguments: argumentsForMessage(resp.FunctionCall.Arguments),
			},
		})

		rawResult, err := a.tools.Execute(resp.FunctionCall.Name, argsJSON)
		if err != nil {
			rawResult = fmt.Sprintf("Error: %v", err)
		}

		// GigaChat requires function results as JSON strings.
		resultJSON, _ := json.Marshal(map[string]string{
			"result": rawResult,
		})

		messages = append(messages, llm.Message{
			Role:    "function",
			Name:    resp.FunctionCall.Name,
			Content: string(resultJSON),
		})
	}

	return "", fmt.Errorf("maximum tool-calling turns (%d) reached", a.maxTurns)
}

// ProcessStream sends a user message and streams the response. Text chunks
// are delivered to onChunk as they arrive. Tool calls are executed inside the
// loop, after streaming ends. Returns the full raw response content.
func (a *Agent) ProcessStream(ctx context.Context, userMsg string, onChunk func(content string)) (string, error) {
	messages := a.newConversationMessages(ctx, userMsg)
	functions := a.tools.ToFunctionDefinitions()

	for turn := 0; turn < a.maxTurns; turn++ {
		ch, err := a.client.Stream(ctx, messages, functions)
		if err != nil {
			return "", fmt.Errorf("stream error: %w", err)
		}

		var content strings.Builder
		var call *llm.FunctionCall

		for chunk := range ch {
			if chunk.Usage != nil {
				a.recordUsage(chunk.Usage)
				continue
			}
			if chunk.FunctionCall != nil {
				call = chunk.FunctionCall
				continue
			}
			if chunk.Content != "" && onChunk != nil {
				onChunk(chunk.Content)
			}
			content.WriteString(chunk.Content)
			if chunk.IsComplete {
				break
			}
		}

		// No function call — this is the final answer.
		if call == nil {
			answer := content.String()
			a.appendAssistant(answer)
			return answer, nil
		}

		// Execute the requested tool and feed the result back to the model.
		// GigaChat requires the assistant message preceding the function
		// result to carry the function_call.
		argsJSON := normalizeArguments(call.Arguments)

		messages = append(messages, llm.Message{
			Role:    "assistant",
			Content: content.String(),
			FunctionCall: &llm.FunctionCall{
				Name:      call.Name,
				Arguments: argumentsForMessage(call.Arguments),
			},
		})

		rawResult, err := a.tools.Execute(call.Name, argsJSON)
		if err != nil {
			rawResult = fmt.Sprintf("Error: %v", err)
		}

		resultJSON, _ := json.Marshal(map[string]string{
			"result": rawResult,
		})

		messages = append(messages, llm.Message{
			Role:    "function",
			Name:    call.Name,
			Content: string(resultJSON),
		})
	}

	return "", fmt.Errorf("maximum tool-calling turns (%d) reached", a.maxTurns)
}

// newConversationMessages builds the message list for a new turn: system
// prompt, relevant long-term memories, session history and the new user
// message. The user message is also appended to the session history.
func (a *Agent) newConversationMessages(ctx context.Context, userMsg string) []llm.Message {
	a.compactHistory(ctx)
	a.history = append(a.history, llm.Message{Role: "user", Content: userMsg})

	messages := make([]llm.Message, 0, len(a.history)+2)
	messages = append(messages, llm.Message{Role: "system", Content: a.systemPrompt(ctx, userMsg)})
	messages = append(messages, a.history...)
	return messages
}

// systemPrompt returns the system prompt, extended with relevant long-term
// memories when available.
func (a *Agent) systemPrompt(ctx context.Context, userMsg string) string {
	prompt := a.config.Agent.SystemPrompt
	memories := a.relevantMemories(ctx, userMsg)
	if len(memories) > 0 {
		prompt += "\n\nRelevant memories from previous sessions:\n- " + strings.Join(memories, "\n- ")
	}
	return prompt
}

// relevantMemories retrieves the most relevant long-term memories for the
// user message. Errors are ignored — memories are best-effort.
func (a *Agent) relevantMemories(ctx context.Context, userMsg string) []string {
	if a.memory == nil || strings.TrimSpace(userMsg) == "" {
		return nil
	}
	vec, err := a.client.Embed(ctx, userMsg)
	if err != nil {
		return nil
	}
	memories, err := a.memory.SearchMemories(vec, 3)
	if err != nil {
		return nil
	}
	var result []string
	for _, m := range memories {
		result = append(result, m.Text)
	}
	return result
}

// compactHistory enforces the message-count and token budgets on the session
// history. When summarization is enabled, old messages are compressed into a
// summary instead of being dropped.
// compactHistory enforces the message-count and token budgets on the session
// history. When summarization is enabled, one compaction pass replaces the
// oldest half of the history with an LLM summary; the rest is trimmed.
// A single pass per turn bounds the added latency.
func (a *Agent) compactHistory(ctx context.Context) {
	a.trimHistory()

	if !a.summarize || a.contextTokens <= 0 {
		return
	}

	if estimateTokens(a.history) > a.contextTokens && len(a.history) > 1 {
		// Drop the oldest half (at least one message) and summarize it.
		n := len(a.history) / 2
		if n < 1 {
			n = 1
		}
		dropped := a.history[:n]

		summary, err := a.summarizeMessages(ctx, dropped)
		if err != nil {
			// Fall back to plain trimming.
			a.trimHistory()
			return
		}

		rest := append([]llm.Message(nil), a.history[n:]...)
		a.history = append([]llm.Message{{
			Role:    "assistant",
			Content: "[Summary of earlier conversation]: " + summary,
		}}, rest...)
	}

	a.trimHistory()
}

// summarizeMessages asks the model to summarize a conversation excerpt.
func (a *Agent) summarizeMessages(ctx context.Context, msgs []llm.Message) (string, error) {
	var b strings.Builder
	for _, m := range msgs {
		fmt.Fprintf(&b, "%s: %s\n", m.Role, m.Content)
	}

	req := []llm.Message{
		{
			Role:    "system",
			Content: "Summarize the following conversation excerpt concisely, preserving key facts, decisions and results. Reply with the summary only.",
		},
		{Role: "user", Content: b.String()},
	}

	resp, err := a.chatWithRetry(ctx, req, nil)
	if err != nil {
		return "", err
	}
	a.recordUsage(resp.Usage)
	return resp.Content, nil
}

// estimateTokens approximates the token count of the history.
// Rough heuristic: ~3 characters per token (accounts for Cyrillic).
func estimateTokens(history []llm.Message) int {
	total := 0
	for _, m := range history {
		total += len([]rune(m.Content))/3 + 4 // +4 per-message overhead
	}
	return total
}

// appendAssistant records the final assistant answer in the session history.
func (a *Agent) appendAssistant(content string) {
	a.history = append(a.history, llm.Message{Role: "assistant", Content: content})
	a.trimHistory()
}

// trimHistory keeps at most maxHistory messages in the session history.
func (a *Agent) trimHistory() {
	if a.maxHistory > 0 && len(a.history) > a.maxHistory {
		a.history = a.history[len(a.history)-a.maxHistory:]
	}
}

// recordUsage accumulates token usage statistics from an API response.
func (a *Agent) recordUsage(u *llm.Usage) {
	if u == nil {
		return
	}
	a.usageMu.Lock()
	defer a.usageMu.Unlock()
	a.usage.PromptTokens += u.PromptTokens
	a.usage.CompletionTokens += u.CompletionTokens
	a.usage.TotalTokens += u.TotalTokens
}

// Usage returns the accumulated token usage for the session.
func (a *Agent) Usage() llm.Usage {
	a.usageMu.Lock()
	defer a.usageMu.Unlock()
	return a.usage
}

// ResetUsage resets the session token usage counter.
func (a *Agent) ResetUsage() {
	a.usageMu.Lock()
	defer a.usageMu.Unlock()
	a.usage = llm.Usage{}
}

// SetModel switches the LLM model used by the client from now on.
func (a *Agent) SetModel(model string) {
	a.client.SetModel(model)
}

// Model returns the currently selected model.
func (a *Agent) Model() string {
	return a.client.Model()
}

// argumentsForMessage normalizes function call arguments into a form
// suitable for a function_call message: a JSON object (map) when possible.
func argumentsForMessage(args interface{}) interface{} {
	switch v := args.(type) {
	case string:
		if v == "" {
			return map[string]interface{}{}
		}
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(v), &m); err == nil {
			return m
		}
		return v
	case nil:
		return map[string]interface{}{}
	default:
		return v
	}
}

// normalizeArguments converts function call arguments (string, JSON object or
// nil) into a JSON string.
func normalizeArguments(args interface{}) string {
	switch v := args.(type) {
	case string:
		if v == "" {
			return "{}"
		}
		return v
	case map[string]interface{}:
		b, _ := json.Marshal(v)
		return string(b)
	case nil:
		return "{}"
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

// chatWithRetry sends a chat request with exponential backoff retry logic.
// Non-retryable errors (auth, validation) are returned immediately.
func (a *Agent) chatWithRetry(ctx context.Context, messages []llm.Message, functions []llm.FunctionDefinition) (*llm.Response, error) {
	var lastErr error

	for attempt := 0; attempt <= a.retryCount; attempt++ {
		if attempt > 0 {
			delay := a.computeBackoff(attempt)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		resp, err := a.client.Chat(ctx, messages, functions)
		if err == nil {
			return resp, nil
		}

		lastErr = err

		// Don't retry on client errors (auth, validation, bad requests).
		if isNonRetryableError(err) {
			return nil, err
		}
	}

	return nil, fmt.Errorf("failed after %d attempts: %w", a.retryCount+1, lastErr)
}

// isNonRetryableError returns true for errors that should not be retried.
func isNonRetryableError(err error) bool {
	errStr := err.Error()
	// HTTP client errors that won't be resolved by retrying
	if strings.Contains(errStr, "401") ||
		strings.Contains(errStr, "403") ||
		strings.Contains(errStr, "422") ||
		strings.Contains(errStr, "400") {
		return true
	}
	return false
}

// computeBackoff calculates the delay for a given retry attempt using
// exponential backoff with random jitter (full mode).
func (a *Agent) computeBackoff(attempt int) time.Duration {
	// Exponential: minDelay * backoffMul^attempt
	delay := float64(a.retryMinDelay) * math.Pow(a.retryBackoffMul, float64(attempt))

	// Cap at maxDelay
	if delay > float64(a.retryMaxDelay) {
		delay = float64(a.retryMaxDelay)
	}

	// Add jitter: random value in [0.5, 1.0] of the calculated delay
	jitter := 0.5 + rand.Float64()*0.5
	delay *= jitter

	return time.Duration(delay)
}

// History returns the conversation history.
func (a *Agent) History() []llm.Message {
	return a.history
}

// LoadHistory replaces the session history with messages loaded from storage.
func (a *Agent) LoadHistory(msgs []llm.Message) {
	a.history = msgs
	a.trimHistory()
}

// ClearHistory clears the conversation history.
func (a *Agent) ClearHistory() {
	a.history = nil
}

// Client returns the LLM client.
func (a *Agent) Client() *llm.Client {
	return a.client
}

// Renderer returns the markdown renderer.
func (a *Agent) Renderer() *markdown.Renderer {
	return a.renderer
}

// RegisterTool registers a custom tool with the agent.
func (a *Agent) RegisterTool(t tools.Tool) {
	a.tools.Register(t)
}
