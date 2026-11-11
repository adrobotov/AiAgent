package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"agent/auth"
	"agent/config"
)

// Message represents a chat message.
type Message struct {
	Role         string        `json:"role"`
	Name         string        `json:"name,omitempty"`
	Content      string        `json:"content"`
	FunctionCall *FunctionCall `json:"function_call,omitempty"`
}

// Usage holds token usage statistics from the API response.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
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
	Usage        *Usage
}

// StreamChunk represents a chunk from streaming response.
type StreamChunk struct {
	Content      string
	FunctionCall *FunctionCall
	Usage        *Usage
	IsComplete   bool
}

// KnownModels is the fallback list of GigaChat models, used when the
// /models endpoint is unavailable.
func KnownModels() []string {
	return []string{
		"GigaChat-3-Ultra",
		"GigaChat-2-Max",
		"GigaChat-2-Pro",
		"GigaChat-2",
	}
}

// ModelInfo describes an available model from the /models endpoint.
type ModelInfo struct {
	ID      string `json:"id"`
	OwnedBy string `json:"owned_by"`
}

// Client is the LLM API client for GigaChat.
type Client struct {
	httpClient   *http.Client
	config       *config.LLMConfig
	tokenManager *auth.TokenManager

	modelMu sync.RWMutex // guards config.Model runtime changes
}

// NewClient creates a new LLM client.
func NewClient(cfg *config.LLMConfig) *Client {
	client := &Client{
		httpClient: &http.Client{
			Transport: &http.Transport{
				ForceAttemptHTTP2: false,
			},
		},
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

// SetModel changes the model used for subsequent requests.
func (c *Client) SetModel(model string) {
	c.modelMu.Lock()
	defer c.modelMu.Unlock()
	c.config.Model = model
}

// Model returns the currently selected model.
func (c *Client) Model() string {
	c.modelMu.RLock()
	defer c.modelMu.RUnlock()
	return c.config.Model
}

// Models returns the list of models available to the account via the
// /models endpoint.
func (c *Client) Models(ctx context.Context) ([]ModelInfo, error) {
	token, err := c.getAuthToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth error: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.config.BaseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models API error (status %d): %s", resp.StatusCode, string(body))
	}

	var parsed struct {
		Data []ModelInfo `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return parsed.Data, nil
}

// Chat sends a message to GigaChat and returns the response.
func (c *Client) Chat(ctx context.Context, messages []Message, functions []FunctionDefinition) (*Response, error) {
	token, err := c.getAuthToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth error: %w", err)
	}

	reqBody := map[string]interface{}{
		"model":       c.Model(),
		"temperature": c.config.Temperature,
		"max_tokens":  c.config.MaxTokens,
		"stream":      false,
		"top_p":       1.0,
		"messages":    messages,
	}

	if len(functions) > 0 {
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
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

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
				Content      string        `json:"content"`
				Role         string        `json:"role"`
				FunctionCall *FunctionCall `json:"function_call"`
			} `json:"message"`
		} `json:"choices"`
		Usage *Usage `json:"usage"`
	}

	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("empty response from LLM")
	}

	result := &Response{
		Content: apiResp.Choices[0].Message.Content,
		Usage:   apiResp.Usage,
	}

	if apiResp.Choices[0].Message.FunctionCall != nil {
		result.FunctionCall = apiResp.Choices[0].Message.FunctionCall
	}

	return result, nil
}

// Embed returns the embedding vector for the text using the embeddings API.
// The returned vector uses float32 precision.
func (c *Client) Embed(ctx context.Context, text string) ([]float32, error) {
	token, err := c.getAuthToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth error: %w", err)
	}

	reqBody := map[string]interface{}{
		"model": "embeddings",
		"input": text,
	}
	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.BaseURL+"/embeddings", bytes.NewReader(reqJSON))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embeddings API error (status %d): %s", resp.StatusCode, string(body))
	}

	var parsed struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if len(parsed.Data) == 0 {
		return nil, fmt.Errorf("empty embeddings response")
	}
	return parsed.Data[0].Embedding, nil
}

// Stream sends a message to GigaChat and returns a channel of streaming chunks.
func (c *Client) Stream(ctx context.Context, messages []Message, functions []FunctionDefinition) (<-chan StreamChunk, error) {
	token, err := c.getAuthToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth error: %w", err)
	}

	reqBody := map[string]interface{}{
		"model":       c.Model(),
		"temperature": c.config.Temperature,
		"max_tokens":  c.config.MaxTokens,
		"stream":      true,
		"top_p":       1.0,
		"messages":    messages,
	}

	if len(functions) > 0 {
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
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Connection", "keep-alive")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	ch := make(chan StreamChunk, 10)

	go func() {
		defer close(ch)
		defer func() { _ = resp.Body.Close() }()

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

		var fullFuncCall *FunctionCall

		for scanner.Scan() {
			line := scanner.Text()

			// Skip empty lines and comment lines
			if line == "" || strings.HasPrefix(line, ":") {
				continue
			}

			// Parse SSE data
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")

			// End of stream
			if data == "[DONE]" {
				break
			}

			// Parse chunk
			var chunk struct {
				Choices []struct {
					Delta struct {
						Content      string        `json:"content"`
						FunctionCall *FunctionCall `json:"function_call"`
					} `json:"delta"`
				} `json:"choices"`
				Usage *Usage `json:"usage"`
			}

			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}

			// Usage may arrive in a separate final SSE chunk without choices.
			if chunk.Usage != nil && len(chunk.Choices) == 0 {
				ch <- StreamChunk{Usage: chunk.Usage, IsComplete: false}
				continue
			}

			if len(chunk.Choices) == 0 {
				continue
			}

			delta := chunk.Choices[0].Delta

			if delta.Content != "" {
				ch <- StreamChunk{
					Content:    delta.Content,
					IsComplete: false,
				}
			}

			if delta.FunctionCall != nil {
				fullFuncCall = mergeFunctionCalls(fullFuncCall, delta.FunctionCall)
			}
		}

		// Send the final chunk. Function calls arrive in fragments across
		// deltas, so the accumulated call is delivered once, at the end.
		if fullFuncCall != nil {
			ch <- StreamChunk{
				FunctionCall: fullFuncCall,
				IsComplete:   true,
			}
		} else {
			ch <- StreamChunk{
				IsComplete: true,
			}
		}
	}()

	return ch, nil
}

// mergeFunctionCalls merges a function call delta into the accumulated call.
// Some APIs stream arguments as accumulated JSON, others as fragments —
// both cases are handled.
func mergeFunctionCalls(acc *FunctionCall, delta *FunctionCall) *FunctionCall {
	if acc == nil {
		return &FunctionCall{Name: delta.Name, Arguments: delta.Arguments}
	}
	if delta.Name != "" {
		acc.Name = delta.Name
	}

	os, ook := acc.Arguments.(string)
	ns, nok := delta.Arguments.(string)
	if ook && nok && ns != "" {
		// If the new fragment is valid JSON on its own, the API is sending
		// accumulated arguments — replace. Otherwise treat as a fragment.
		var tmp map[string]interface{}
		if json.Unmarshal([]byte(ns), &tmp) == nil {
			acc.Arguments = ns
		} else {
			acc.Arguments = os + ns
		}
	} else if delta.Arguments != nil {
		acc.Arguments = delta.Arguments
	}
	return acc
}

// getAuthToken returns a valid access token for GigaChat.
func (c *Client) getAuthToken(ctx context.Context) (string, error) {
	if c.tokenManager == nil {
		// No auth key configured — proceed without a token. This is useful
		// for tests and self-hosted endpoints without authentication.
		return "", nil
	}

	return c.tokenManager.GetToken(ctx)
}

// BuildMessages creates the full message list with system prompt.
func BuildMessages(systemPrompt string, history []Message, userMsg string) []Message {
	messages := []Message{{Role: "system", Content: systemPrompt}}
	messages = append(messages, history...)
	messages = append(messages, Message{Role: "user", Content: userMsg})
	return messages
}

// FunctionDefinition represents a GigaChat function specification.
type FunctionDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters"`
}
