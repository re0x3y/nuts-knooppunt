package httpauth

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

// TokenFunc is a function that returns a bearer token.
// It is called on every HTTP request, allowing for dynamic token refresh.
// Return an empty string to skip adding the Authorization header.
// Return an error if the token cannot be obtained.
type TokenFunc func() (string, error)

// AuthTransport is an http.RoundTripper that adds an Authorization header to requests.
// The token is fetched dynamically on each request using the provided TokenFunc,
// which allows for automatic token refresh when tokens expire.
type AuthTransport struct {
	// Base is the underlying RoundTripper to use for actual HTTP requests.
	// If nil, http.DefaultTransport is used.
	Base http.RoundTripper

	// GetToken is called on every request to get the current bearer token.
	// If nil or returns empty string, no Authorization header is added.
	GetToken TokenFunc
}

// RoundTrip implements http.RoundTripper.
func (t *AuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone the request to avoid mutating the original
	reqClone := req.Clone(req.Context())

	if t.GetToken != nil {
		token, err := t.GetToken()
		if err != nil {
			return nil, fmt.Errorf("failed to get auth token: %w", err)
		}
		if token != "" {
			reqClone.Header.Set("Authorization", "Bearer "+token)
		}
	}

	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(reqClone)
}

// NewAuthTransport creates a new AuthTransport with the given base transport and token function.
// If base is nil, http.DefaultTransport is used.
func NewAuthTransport(base http.RoundTripper, getToken TokenFunc) *AuthTransport {
	return &AuthTransport{
		Base:     base,
		GetToken: getToken,
	}
}

// NewHTTPClient creates an http.Client with auth support.
// The getToken function is called on every request to get the current bearer token.
func NewHTTPClient(getToken TokenFunc) *http.Client {
	return &http.Client{
		Transport: NewAuthTransport(nil, getToken),
	}
}

// TokenProvider manages token caching and automatic refresh.
// It is safe for concurrent use.
type TokenProvider struct {
	mu          sync.RWMutex
	token       string
	expiresAt   time.Time
	refreshFunc func() (token string, expiresIn time.Duration, err error)
	// refreshBuffer is subtracted from expiresAt to trigger refresh before actual expiry
	refreshBuffer time.Duration
}

// NewTokenProvider creates a new TokenProvider with the given refresh function.
// The refreshFunc is called when a token is needed and the current one is expired or about to expire.
// refreshBuffer specifies how long before expiry to trigger a refresh (default 30 seconds if zero).
func NewTokenProvider(refreshFunc func() (token string, expiresIn time.Duration, err error), refreshBuffer time.Duration) *TokenProvider {
	if refreshBuffer == 0 {
		refreshBuffer = 30 * time.Second
	}
	return &TokenProvider{
		refreshFunc:   refreshFunc,
		refreshBuffer: refreshBuffer,
	}
}

// GetToken returns a valid token, refreshing if necessary.
// This method is safe for concurrent use.
func (p *TokenProvider) GetToken() (string, error) {
	p.mu.RLock()
	if time.Now().Before(p.expiresAt.Add(-p.refreshBuffer)) {
		token := p.token
		p.mu.RUnlock()
		return token, nil
	}
	p.mu.RUnlock()

	// Token expired or about to expire, refresh it
	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check after acquiring write lock (another goroutine may have refreshed)
	if time.Now().Before(p.expiresAt.Add(-p.refreshBuffer)) {
		return p.token, nil
	}

	token, expiresIn, err := p.refreshFunc()
	if err != nil {
		return "", fmt.Errorf("token refresh failed: %w", err)
	}
	p.token = token
	p.expiresAt = time.Now().Add(expiresIn)
	return token, nil
}

// TokenFunc returns a TokenFunc that can be used with AuthTransport.
func (p *TokenProvider) TokenFunc() TokenFunc {
	return p.GetToken
}

// StaticToken returns a TokenFunc that always returns the same token.
// Useful for testing or when tokens don't expire.
func StaticToken(token string) TokenFunc {
	return func() (string, error) {
		return token, nil
	}
}

// NoAuth returns a TokenFunc that returns an empty token (no auth header added).
func NoAuth() TokenFunc {
	return func() (string, error) {
		return "", nil
	}
}

// WrapTransport wraps the given transport with auth support.
// This is useful when you want to combine auth with other transports (e.g., tracing).
// Example with tracing:
//
//	transport := httpauth.WrapTransport(tracing.WrapTransport(nil), tokenProvider.TokenFunc())
func WrapTransport(base http.RoundTripper, getToken TokenFunc) http.RoundTripper {
	return NewAuthTransport(base, getToken)
}

// NewHTTPClientWithTransport creates an http.Client with auth support using the given base transport.
// This is useful when you want to combine auth with other transports (e.g., tracing).
func NewHTTPClientWithTransport(base http.RoundTripper, getToken TokenFunc) *http.Client {
	return &http.Client{
		Transport: NewAuthTransport(base, getToken),
	}
}
