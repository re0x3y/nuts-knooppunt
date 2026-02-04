package httpauth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAuthTransport_RoundTrip(t *testing.T) {
	t.Run("adds bearer token to request", func(t *testing.T) {
		var capturedAuth string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := &http.Client{
			Transport: NewAuthTransport(nil, StaticToken("test-token")),
		}

		resp, err := client.Get(server.URL)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer resp.Body.Close()

		if capturedAuth != "Bearer test-token" {
			t.Errorf("expected 'Bearer test-token', got '%s'", capturedAuth)
		}
	})

	t.Run("no auth header when token is empty", func(t *testing.T) {
		var capturedAuth string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := &http.Client{
			Transport: NewAuthTransport(nil, NoAuth()),
		}

		resp, err := client.Get(server.URL)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer resp.Body.Close()

		if capturedAuth != "" {
			t.Errorf("expected empty auth header, got '%s'", capturedAuth)
		}
	})

	t.Run("returns error when token function fails", func(t *testing.T) {
		client := &http.Client{
			Transport: NewAuthTransport(nil, func() (string, error) {
				return "", errors.New("token fetch failed")
			}),
		}

		_, err := client.Get("http://example.com")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("uses default transport when base is nil", func(t *testing.T) {
		transport := NewAuthTransport(nil, StaticToken("token"))
		if transport.Base != nil {
			t.Error("expected Base to be nil")
		}
	})

	t.Run("token function called on each request", func(t *testing.T) {
		var callCount int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := &http.Client{
			Transport: NewAuthTransport(nil, func() (string, error) {
				atomic.AddInt32(&callCount, 1)
				return "token", nil
			}),
		}

		for i := 0; i < 3; i++ {
			resp, err := client.Get(server.URL)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			resp.Body.Close()
		}

		if atomic.LoadInt32(&callCount) != 3 {
			t.Errorf("expected token function to be called 3 times, got %d", callCount)
		}
	})
}

func TestTokenProvider(t *testing.T) {
	t.Run("caches token until expiry", func(t *testing.T) {
		var callCount int32
		provider := NewTokenProvider(func() (string, time.Duration, error) {
			count := atomic.AddInt32(&callCount, 1)
			return "token-" + string(rune('0'+count)), 1 * time.Hour, nil
		}, 30*time.Second)

		token1, err := provider.GetToken()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token1 != "token-1" {
			t.Errorf("expected 'token-1', got '%s'", token1)
		}

		token2, err := provider.GetToken()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token2 != "token-1" {
			t.Errorf("expected cached 'token-1', got '%s'", token2)
		}

		if atomic.LoadInt32(&callCount) != 1 {
			t.Errorf("expected refresh function to be called once, got %d", callCount)
		}
	})

	t.Run("refreshes token when expired", func(t *testing.T) {
		var callCount int32
		provider := NewTokenProvider(func() (string, time.Duration, error) {
			count := atomic.AddInt32(&callCount, 1)
			return "token-" + string(rune('0'+count)), 1 * time.Millisecond, nil
		}, 0)

		token1, _ := provider.GetToken()
		if token1 != "token-1" {
			t.Errorf("expected 'token-1', got '%s'", token1)
		}

		time.Sleep(10 * time.Millisecond)

		token2, _ := provider.GetToken()
		if token2 != "token-2" {
			t.Errorf("expected 'token-2', got '%s'", token2)
		}
	})

	t.Run("returns error on refresh failure", func(t *testing.T) {
		provider := NewTokenProvider(func() (string, time.Duration, error) {
			return "", 0, errors.New("refresh failed")
		}, 0)

		_, err := provider.GetToken()
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("concurrent access is safe", func(t *testing.T) {
		var callCount int32
		provider := NewTokenProvider(func() (string, time.Duration, error) {
			atomic.AddInt32(&callCount, 1)
			time.Sleep(10 * time.Millisecond)
			return "token", 1 * time.Hour, nil
		}, 30*time.Second)

		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				token, err := provider.GetToken()
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if token != "token" {
					t.Errorf("expected 'token', got '%s'", token)
				}
			}()
		}
		wg.Wait()

		if atomic.LoadInt32(&callCount) > 5 {
			t.Errorf("expected <= 5 refresh calls due to caching, got %d", callCount)
		}
	})
}

func TestNewHTTPClient(t *testing.T) {
	var capturedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewHTTPClient(StaticToken("my-token"))

	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if capturedAuth != "Bearer my-token" {
		t.Errorf("expected 'Bearer my-token', got '%s'", capturedAuth)
	}
}
