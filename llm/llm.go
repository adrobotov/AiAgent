package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"agent/auth"
	"agent/config"
)

// Message represents a chat message.
type Message struct {
	Role    string `json:"role"`
	Name    string `json:"name,omitempty"`
	Content string `json:"content"`
}

// FunctionCall represents a function call from GigaChat.
type FunctionCall struct {
	Name      string      `json:"name"`
	Arguments interface{} `json:"arguments"`
}

// Response represents an LLM response.
type Response struct {
	Content      string
	FunctionCall *FunctionCall
}

// Client is the LLM API client for GigaChat.
type Client struct {
	httpClient   *http.Client
	config       *config.LLMConfig
	tokenManager *auth.TokenManager
}

// NewClient creates a new LLM client.
func NewClient(cfg *config.LLMConfig) *Client {
	client := &Client{
		httpClient: &http.Client{},
		config:     cfg,
	}

	// Setup OAuth token manager for GigaChat
	if cfg.AuthKey != "" {
		oauthCfg := auth.DefaultOAuthConfig()
		if cfg.OAuthScope != "" {
			oauthCfg.Scope = auth.Scope(cfg.OAuthScope)
		}
		if cfg.OAuthRqUID != "" {
			oauthCfg.RqUID = cfg.OAuthRqUID
		}
		client.tokenManager = auth.NewTokenManager(oauthCfg, cfg.AuthKey)
	}

	return client
}

// Chat sends a message to GigaChat and returns the response.
func (c *Client) Chat(ctx context.Context, messages []Message, functions []FunctionDefinition) (*Response, error) {
	token, err := c.getAuthToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth error: %w", err)
	}

	// Build GigaChat-specific request
	reqBody := map[string]interface{}{
		"model":       c.config.Model,
		"temperature": c.config.Temperature,
		"max_tokens":  c.config.MaxTokens,
		"stream":      false,
		"top_p":       1.0,
		"messages":    messages,
	}

	if len(functions) > 0 {
		// GigaChat expects functions as a flat array at top level
		reqBody["functions"] = functions
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.BaseURL+"/chat/completions", bytes.NewReader(reqJSON))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Parse response
	var apiResp struct {
		Choices []struct {
			Message struct {
				Content      string      `json:"content"`
				Role         string      `json:"role"`
				FunctionCall *FunctionCall `json:"function_call"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("empty response from LLM")
	}

	result := &Response{
		Content: apiResp.Choices[0].Message.Content,
	}

	if apiResp.Choices[0].Message.FunctionCall != nil {
		// GigaChat returns arguments as JSON object, not string
		argsJSON, err := json.Marshal(apiResp.Choices[0].Message.FunctionCall.Arguments)
		if err != nil {
			return nil, fmt.Errorf("marshal arguments: %w", err)
		}
		result.FunctionCall = &FunctionCall{
			Name:      apiResp.Choices[0].Message.FunctionCall.Name,
			Arguments: string(argsJSON),
		}
	}

	return result, nil
}

// getAuthToken returns a valid access token for GigaChat.
func (c *Client) getAuthToken(ctx context.Context) (string, error) {
	if c.tokenManager == nil {
		return "", fmt.Errorf("auth key not configured")
	}

	return c.tokenManager.GetToken(ctx)
}

// BuildMessages creates the full message list with system prompt.
func BuildMessages(systemPrompt string, history []Message, userMsg string) []Message {
	messages := []Message{{Role: "system", Content: systemPrompt}}
	for _, m := range history {
		messages = append(messages, m)
	}
	messages = append(messages, Message{Role: "user", Content: userMsg})
	return messages
}

// FunctionDefinition represents a GigaChat function specification.
type FunctionDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters"`
}
