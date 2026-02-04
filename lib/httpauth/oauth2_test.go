package httpauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOAuth2Config_IsConfigured(t *testing.T) {
	tests := []struct {
		name     string
		config   OAuth2Config
		expected bool
	}{
		{
			name:     "empty config",
			config:   OAuth2Config{},
			expected: false,
		},
		{
			name: "missing token URL",
			config: OAuth2Config{
				ClientID:     "id",
				ClientSecret: "secret",
			},
			expected: false,
		},
		{
			name: "missing client ID",
			config: OAuth2Config{
				TokenURL:     "http://example.com/token",
				ClientSecret: "secret",
			},
			expected: false,
		},
		{
			name: "missing client secret",
			config: OAuth2Config{
				TokenURL: "http://example.com/token",
				ClientID: "id",
			},
			expected: false,
		},
		{
			name: "fully configured",
			config: OAuth2Config{
				TokenURL:     "http://example.com/token",
				ClientID:     "id",
				ClientSecret: "secret",
			},
			expected: true,
		},
		{
			name: "with scopes",
			config: OAuth2Config{
				TokenURL:     "http://example.com/token",
				ClientID:     "id",
				ClientSecret: "secret",
				Scopes:       []string{"read", "write"},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.IsConfigured(); got != tt.expected {
				t.Errorf("IsConfigured() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestNewOAuth2TokenProvider(t *testing.T) {
	t.Run("returns error for incomplete config", func(t *testing.T) {
		_, err := NewOAuth2TokenProvider(OAuth2Config{}, 0)
		if err == nil {
			t.Error("expected error for incomplete config")
		}
	})

	t.Run("successfully fetches token", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify request
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
				t.Errorf("expected content-type application/x-www-form-urlencoded")
			}

			err := r.ParseForm()
			if err != nil {
				t.Errorf("failed to parse form: %v", err)
			}
			if r.PostForm.Get("grant_type") != "client_credentials" {
				t.Errorf("expected grant_type=client_credentials")
			}
			if r.PostForm.Get("client_id") != "test-client" {
				t.Errorf("expected client_id=test-client")
			}
			if r.PostForm.Get("client_secret") != "test-secret" {
				t.Errorf("expected client_secret=test-secret")
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(oauth2TokenResponse{
				AccessToken: "test-access-token",
				TokenType:   "Bearer",
				ExpiresIn:   3600,
			})
		}))
		defer server.Close()

		config := OAuth2Config{
			TokenURL:     server.URL,
			ClientID:     "test-client",
			ClientSecret: "test-secret",
		}

		provider, err := NewOAuth2TokenProvider(config, 0)
		if err != nil {
			t.Fatalf("failed to create provider: %v", err)
		}

		token, err := provider.GetToken()
		if err != nil {
			t.Fatalf("failed to get token: %v", err)
		}

		if token != "test-access-token" {
			t.Errorf("expected token 'test-access-token', got '%s'", token)
		}
	})

	t.Run("includes scopes in request", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			err := r.ParseForm()
			if err != nil {
				t.Errorf("failed to parse form: %v", err)
			}
			if r.PostForm.Get("scope") != "read write" {
				t.Errorf("expected scope='read write', got '%s'", r.PostForm.Get("scope"))
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(oauth2TokenResponse{
				AccessToken: "token",
				ExpiresIn:   3600,
			})
		}))
		defer server.Close()

		config := OAuth2Config{
			TokenURL:     server.URL,
			ClientID:     "id",
			ClientSecret: "secret",
			Scopes:       []string{"read", "write"},
		}

		provider, _ := NewOAuth2TokenProvider(config, 0)
		_, err := provider.GetToken()
		if err != nil {
			t.Fatalf("failed to get token: %v", err)
		}
	})

	t.Run("handles error response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error": "invalid_client"}`))
		}))
		defer server.Close()

		config := OAuth2Config{
			TokenURL:     server.URL,
			ClientID:     "id",
			ClientSecret: "wrong-secret",
		}

		provider, _ := NewOAuth2TokenProvider(config, 0)
		_, err := provider.GetToken()
		if err == nil {
			t.Error("expected error for failed token request")
		}
	})

	t.Run("caches token until expiry", func(t *testing.T) {
		callCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(oauth2TokenResponse{
				AccessToken: "token",
				ExpiresIn:   3600,
			})
		}))
		defer server.Close()

		config := OAuth2Config{
			TokenURL:     server.URL,
			ClientID:     "id",
			ClientSecret: "secret",
		}

		provider, _ := NewOAuth2TokenProvider(config, 30*time.Second)

		// First call
		_, _ = provider.GetToken()
		// Second call should use cached token
		_, _ = provider.GetToken()
		// Third call should use cached token
		_, _ = provider.GetToken()

		if callCount != 1 {
			t.Errorf("expected 1 token fetch, got %d", callCount)
		}
	})
}

func TestNewOAuth2HTTPClient(t *testing.T) {
	t.Run("returns error for incomplete config", func(t *testing.T) {
		_, err := NewOAuth2HTTPClient(OAuth2Config{}, nil)
		if err == nil {
			t.Error("expected error for incomplete config")
		}
	})

	t.Run("makes authenticated requests", func(t *testing.T) {
		// Token server
		tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(oauth2TokenResponse{
				AccessToken: "my-access-token",
				ExpiresIn:   3600,
			})
		}))
		defer tokenServer.Close()

		// Resource server
		var capturedAuth string
		resourceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}))
		defer resourceServer.Close()

		config := OAuth2Config{
			TokenURL:     tokenServer.URL,
			ClientID:     "id",
			ClientSecret: "secret",
		}

		client, err := NewOAuth2HTTPClient(config, nil)
		if err != nil {
			t.Fatalf("failed to create client: %v", err)
		}

		resp, err := client.Get(resourceServer.URL)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if capturedAuth != "Bearer my-access-token" {
			t.Errorf("expected 'Bearer my-access-token', got '%s'", capturedAuth)
		}
	})
}
