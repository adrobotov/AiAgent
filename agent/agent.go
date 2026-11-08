package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"agent/config"
	"agent/llm"
	"agent/tools"
)

// Agent is the main AI agent that processes user requests.
type Agent struct {
	client   *llm.Client
	config   *config.Config
	tools    *tools.Manager
	history  []llm.Message
	maxTurns int
}

// New creates a new Agent instance.
func New(cfg *config.Config) (*Agent, error) {
	client := llm.NewClient(&cfg.LLM)
	toolManager := tools.NewManager()

	return &Agent{
		client:   client,
		config:   cfg,
		tools:    toolManager,
		maxTurns: cfg.Agent.MaxTurns,
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
		resp, err := a.client.Chat(ctx, messages, functions)
		if err != nil {
			return "", fmt.Errorf("chat error: %w", err)
		}

		// If no function call, return the content
		if resp.FunctionCall == nil {
			a.history = append(a.history,
				llm.Message{Role: "assistant", Content: resp.Content},
			)
			return resp.Content, nil
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
			// arguments is a JSON object like {"expression": "10*10+5"}
			// Marshal it back to proper JSON
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

		return rawResult, nil
	}

	return "Maximum turns reached. Please start a new conversation.", nil
}

// History returns the conversation history.
func (a *Agent) History() []llm.Message {
	return a.history
}

// ClearHistory clears the conversation history.
func (a *Agent) ClearHistory() {
	a.history = nil
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

	default:
		return "", fmt.Errorf("unknown function: %s", fn)
	}
}
