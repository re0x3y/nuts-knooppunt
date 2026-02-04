package httpauth

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OAuth2Config holds the configuration for OAuth2 client credentials authentication.
type OAuth2Config struct {
	// TokenURL is the OAuth2 token endpoint URL
	TokenURL string `koanf:"tokenurl"`
	// ClientID is the OAuth2 client ID
	ClientID string `koanf:"clientid"`
	// ClientSecret is the OAuth2 client secret
	ClientSecret string `koanf:"clientsecret"`
	// Scopes is an optional list of scopes to request (space-separated in the request)
	Scopes []string `koanf:"scopes"`
}

// IsConfigured returns true if the OAuth2 configuration has all required fields set.
func (c OAuth2Config) IsConfigured() bool {
	return c.TokenURL != "" && c.ClientID != "" && c.ClientSecret != ""
}

// oauth2TokenResponse represents the response from the OAuth2 token endpoint.
type oauth2TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"` // Expiration time in seconds
	Scope       string `json:"scope"`
}

// NewOAuth2TokenProvider creates a TokenProvider that fetches tokens using OAuth2 client credentials grant.
// The refreshBuffer specifies how long before token expiry to trigger a refresh (default 30 seconds if zero).
func NewOAuth2TokenProvider(config OAuth2Config, refreshBuffer time.Duration) (*TokenProvider, error) {
	if !config.IsConfigured() {
		return nil, fmt.Errorf("OAuth2 configuration is incomplete: tokenurl, clientid, and clientsecret are required")
	}

	return NewTokenProvider(func() (string, time.Duration, error) {
		return fetchOAuth2Token(config)
	}, refreshBuffer), nil
}

// fetchOAuth2Token fetches a new access token using the OAuth2 client credentials grant.
func fetchOAuth2Token(config OAuth2Config) (string, time.Duration, error) {
	// Build form data
	data := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {config.ClientID},
		"client_secret": {config.ClientSecret},
	}
	if len(config.Scopes) > 0 {
		data.Set("scope", strings.Join(config.Scopes, " "))
	}

	// Create request
	req, err := http.NewRequest(http.MethodPost, config.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", 0, fmt.Errorf("failed to create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Send request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("failed to read token response: %w", err)
	}

	// Check for error response
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("token request returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var tokenResp oauth2TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", 0, fmt.Errorf("failed to parse token response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return "", 0, fmt.Errorf("token response did not contain access_token")
	}

	// Calculate expiration duration
	// Default to 1 hour if expires_in is not provided
	expiresIn := time.Duration(tokenResp.ExpiresIn) * time.Second
	if expiresIn <= 0 {
		expiresIn = 1 * time.Hour
		slog.Warn("OAuth2 token response did not include expires_in, defaulting to 1 hour")
	}

	slog.Debug("Successfully obtained OAuth2 access token", "expires_in", expiresIn.String())
	return tokenResp.AccessToken, expiresIn, nil
}

// NewOAuth2HTTPClient creates an http.Client that automatically handles OAuth2 client credentials authentication.
// It wraps the given base transport (use nil for default, or tracing.WrapTransport(nil) for tracing support).
func NewOAuth2HTTPClient(config OAuth2Config, baseTransport http.RoundTripper) (*http.Client, error) {
	tokenProvider, err := NewOAuth2TokenProvider(config, 30*time.Second)
	if err != nil {
		return nil, err
	}

	return &http.Client{
		Transport: NewAuthTransport(baseTransport, tokenProvider.TokenFunc()),
	}, nil
}

// MustNewOAuth2HTTPClient is like NewOAuth2HTTPClient but panics on error.
// Use this only when configuration is validated at startup.
func MustNewOAuth2HTTPClient(config OAuth2Config, baseTransport http.RoundTripper) *http.Client {
	client, err := NewOAuth2HTTPClient(config, baseTransport)
	if err != nil {
		panic(err)
	}
	return client
}
