package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Scope represents the OAuth scope for GigaChat API.
type Scope string

const (
	// ScopePers is for individuals (физические лица).
	ScopePers Scope = "GIGACHAT_API_PERS"
	// ScopeB2B is for individual entrepreneurs and legal entities with paid packages.
	ScopeB2B Scope = "GIGACHAT_API_B2B"
	// ScopeCorp is for individual entrepreneurs and legal entities with pay-as-you-go.
	ScopeCorp Scope = "GIGACHAT_API_CORP"
)

// OAuthConfig holds GigaChat OAuth settings.
type OAuthConfig struct {
	AuthURL string
	Scope   Scope
	RqUID   string // Optional: auto-generated if empty
	Timeout time.Duration
}

// DefaultOAuthConfig returns default GigaChat OAuth configuration for individuals.
func DefaultOAuthConfig() *OAuthConfig {
	return &OAuthConfig{
		AuthURL: "https://ngw.devices.sberbank.ru:9443/api/v2/oauth",
		Scope:   ScopePers,
		Timeout: 30 * time.Second,
	}
}

// TokenResponse represents the OAuth token response.
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresAt   int64  `json:"expires_at"` // Unix timestamp in milliseconds
}

// TokenManager manages OAuth tokens with automatic refresh.
type TokenManager struct {
	config  *OAuthConfig
	client  *http.Client
	authKey string

	mu      sync.RWMutex
	token   string
	expired time.Time
}

// NewTokenManager creates a new token manager.
func NewTokenManager(config *OAuthConfig, authKey string) *TokenManager {
	return &TokenManager{
		config:  config,
		client:  &http.Client{Timeout: config.Timeout},
		authKey: authKey,
	}
}

// GetToken returns a valid access token, refreshing if necessary.
func (tm *TokenManager) GetToken(ctx context.Context) (string, error) {
	tm.mu.RLock()
	if tm.token != "" && time.Now().Before(tm.expired) {
		token := tm.token
		tm.mu.RUnlock()
		return token, nil
	}
	tm.mu.RUnlock()

	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Double-check after acquiring write lock
	if tm.token != "" && time.Now().Before(tm.expired) {
		return tm.token, nil
	}

	return tm.refreshToken(ctx)
}

// refreshToken performs OAuth token refresh.
func (tm *TokenManager) refreshToken(ctx context.Context) (string, error) {
	// Generate RqUID (UUID4) - required by GigaChat API, must be unique per request
	rqUID := tm.config.RqUID
	if rqUID == "" {
		rqUID = uuid.New().String()
	}

	// Build request body
	reqBody := url.Values{}
	reqBody.Set("scope", string(tm.config.Scope))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tm.config.AuthURL, bytes.NewBufferString(reqBody.Encode()))
	if err != nil {
		return "", fmt.Errorf("create auth request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("RqUID", rqUID)

	// Determine auth format: check if key already contains "Basic " prefix
	authHeader := tm.authKey
	if !strings.HasPrefix(authHeader, "Basic ") {
		// Try to decode as base64 and re-encode properly
		_, err := base64.StdEncoding.DecodeString(tm.authKey)
		if err == nil {
			// Key is valid base64, use it as-is with Basic prefix
			authHeader = "Basic " + tm.authKey
		} else {
			// Key might already be the raw value, encode it
			encoded := base64.StdEncoding.EncodeToString([]byte(tm.authKey))
			authHeader = "Basic " + encoded
		}
	}
	req.Header.Set("Authorization", authHeader)

	resp, err := tm.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("auth request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body for error handling
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		var errResp map[string]interface{}
		if err := json.Unmarshal(respBody, &errResp); err == nil {
			return "", fmt.Errorf("auth failed (status %d): %v", resp.StatusCode, errResp)
		}
		return "", fmt.Errorf("auth failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return "", fmt.Errorf("parse token response: %w", err)
	}

	// expires_at is a Unix timestamp in milliseconds
	expiryTime := time.Unix(0, tokenResp.ExpiresAt*int64(time.Millisecond))
	// Refresh 2 minutes before expiry to be safe
	tm.expired = expiryTime.Add(-2 * time.Minute)
	tm.token = tokenResp.AccessToken

	return tm.token, nil
}
