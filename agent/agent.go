package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"agent/config"
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
	renderer   *markdown.Renderer
	retryCount int
	retryDelay time.Duration
}

// New creates a new Agent instance.
func New(cfg *config.Config) (*Agent, error) {
	client := llm.NewClient(&cfg.LLM)
	toolManager := tools.NewManager()

	return &Agent{
		client:     client,
		config:     cfg,
		tools:      toolManager,
		maxTurns:   cfg.Agent.MaxTurns,
		renderer:   markdown.New(true),
		retryCount: 3,
		retryDelay: 2 * time.Second,
	}, nil
}

// Process sends a user message to the agent and returns the response.
func (a *Agent) Process(ctx context.Context, userMsg string) (string, error) {
	if len(a.history) >= a.maxTurns {
		a.history = a.history[len(a.history)-10:]
	}

	messages := llm.BuildMessages(a.config.Agent.SystemPrompt, a.history, userMsg)
	functions := a.tools.ToFunctionDefinitions()

	turn := 0
	for turn < a.maxTurns {
		resp, err := a.chatWithRetry(ctx, messages, functions)
		if err != nil {
			return "", fmt.Errorf("chat error: %w", err)
		}

		// If no function call, return the content
		if resp.FunctionCall == nil {
			a.history = append(a.history,
				llm.Message{Role: "assistant", Content: resp.Content},
			)
			// Render markdown for display
			return a.renderer.Render(resp.Content), nil
		}

		// Process function call
		messages = append(messages, llm.Message{
			Role:    "assistant",
			Content: "",
		})

		// Handle arguments - GigaChat returns them as JSON object
		var argsJSON string
		switch v := resp.FunctionCall.Arguments.(type) {
		case string:
			argsJSON = v
		case map[string]interface{}:
			b, _ := json.Marshal(v)
			argsJSON = string(b)
		case nil:
			argsJSON = "{}"
		default:
			b, _ := json.Marshal(v)
			argsJSON = string(b)
		}

		rawResult, err := a.tools.Execute(resp.FunctionCall.Name, argsJSON)
		if err != nil {
			rawResult = fmt.Sprintf("Error: %v", err)
		}

		// GigaChat requires function results as JSON strings
		resultJSON, _ := json.Marshal(map[string]string{
			"result": rawResult,
		})

		messages = append(messages, llm.Message{
			Role:    "function",
			Name:    resp.FunctionCall.Name,
			Content: string(resultJSON),
		})

		// Store result for return
		a.history = append(a.history,
			llm.Message{Role: "assistant", Content: rawResult},
		)

		return a.renderer.Render(rawResult), nil
	}

	return "Maximum turns reached. Please start a new conversation.", nil
}

// chatWithRetry sends a chat request with retry logic.
func (a *Agent) chatWithRetry(ctx context.Context, messages []llm.Message, functions []llm.FunctionDefinition) (*llm.Response, error) {
	var lastErr error

	for attempt := 0; attempt <= a.retryCount; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(a.retryDelay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		resp, err := a.client.Chat(ctx, messages, functions)
		if err == nil {
			return resp, nil
		}

		lastErr = err

		// Don't retry on certain errors
		if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "422") {
			return nil, err
		}
	}

	return nil, fmt.Errorf("failed after %d attempts: %w", a.retryCount, lastErr)
}

// History returns the conversation history.
func (a *Agent) History() []llm.Message {
	return a.history
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

// ExecuteJSON executes a JSON-encoded function call.
func ExecuteJSON(fn string, argsJSON string) (string, error) {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	switch fn {
	case "echo":
		msg, ok := args["message"].(string)
		if !ok {
			return "", fmt.Errorf("message must be a string")
		}
		return msg, nil

	case "calc":
		expr, ok := args["expression"].(string)
		if !ok {
			return "", fmt.Errorf("expression must be a string")
		}
		return tools.Calculate(expr)

	case "time":
		return tools.GetCurrentTime()

	case "weather":
		location, ok := args["location"].(string)
		if !ok {
			return "", fmt.Errorf("location must be a string")
		}
		return tools.FetchWeather(location)

	case "web_search":
		query, ok := args["query"].(string)
		if !ok {
			return "", fmt.Errorf("query must be a string")
		}
		return tools.WebSearch(query)

	case "code_interpreter":
		code, ok := args["code"].(string)
		if !ok {
			return "", fmt.Errorf("code must be a string")
		}
		return tools.ExecuteCode(code)

	default:
		return "", fmt.Errorf("unknown function: %s", fn)
	}
}
