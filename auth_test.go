package glassnode

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAuthenticationConfiguration(t *testing.T) {
	source := TokenSourceFunc(func(context.Context) (string, error) { return "token", nil })
	var nilSource TokenSourceFunc
	for _, tt := range []struct {
		name, key string
		options   []Option
	}{
		{"missing", "", nil},
		{"key whitespace", " key ", nil},
		{"key line break", "key\n", nil},
		{"key control", "key\x00", nil},
		{"key and bearer", "key", []Option{WithBearerToken("token")}},
		{"key and source", "key", []Option{WithTokenSource(source)}},
		{"bearer and source", "", []Option{WithBearerToken("token"), WithTokenSource(source)}},
		{"query and bearer", "", []Option{WithAPIKeyInQuery(), WithBearerToken("token")}},
		{"query and source", "", []Option{WithTokenSource(source), WithAPIKeyInQuery()}},
		{"nil source", "", []Option{WithTokenSource(nil)}},
		{"typed nil source", "", []Option{WithTokenSource(nilSource)}},
		{"bearer whitespace", "", []Option{WithBearerToken(" token ")}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewClient(tt.key, tt.options...)
			var input *InputError
			if !errors.As(err, &input) {
				t.Fatalf("expected input error, got %v", err)
			}
		})
	}
}

func TestTokenSourceRotatesAndRedactsAcrossRetries(t *testing.T) {
	var tokens, requests atomic.Int32
	source := TokenSourceFunc(func(context.Context) (string, error) {
		return fmt.Sprintf("rotated-secret-%d", tokens.Add(1)), nil
	})
	client, _ := testClientWithKey(t, "", func(w http.ResponseWriter, r *http.Request) {
		attempt := requests.Add(1)
		if r.Header.Get("Authorization") != fmt.Sprintf("Bearer rotated-secret-%d", attempt) || r.Header.Get("X-Api-Key") != "" || r.URL.Query().Has("api_key") {
			t.Errorf("incorrect authentication on attempt %d", attempt)
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"message":"temporarily unavailable rotated-secret-1 rotated-secret-2"}`)
	}, WithTokenSource(source), WithRetryPolicy(RetryPolicy{1, time.Millisecond, time.Millisecond}))
	_, err := client.Raw(context.Background(), "/v1/test", nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || requests.Load() != 2 || tokens.Load() != 2 {
		t.Fatalf("err=%v requests=%d tokens=%d", err, requests.Load(), tokens.Load())
	}
	if strings.Contains(err.Error(), "rotated-secret") || !strings.Contains(err.Error(), "temporarily unavailable") {
		t.Fatalf("bad diagnostic: %v", err)
	}
}

func TestTokenSourceFailureNeverRetriesOrSendsRequest(t *testing.T) {
	for _, token := range []string{"", "returned-secret"} {
		t.Run(token, func(t *testing.T) {
			cause := &TransportError{Endpoint: "/token", Err: errors.New("refresh failed returned-secret")}
			var tokens, requests atomic.Int32
			source := TokenSourceFunc(func(context.Context) (string, error) { tokens.Add(1); return token, cause })
			client, _ := testClientWithKey(t, "", func(http.ResponseWriter, *http.Request) { requests.Add(1) }, WithTokenSource(source))
			_, err := client.Raw(context.Background(), "/v1/test", nil)
			var auth *AuthError
			if !errors.As(err, &auth) || !errors.Is(err, cause) || tokens.Load() != 1 || requests.Load() != 0 {
				t.Fatalf("err=%v token calls=%d requests=%d", err, tokens.Load(), requests.Load())
			}
			if token != "" && strings.Contains(err.Error(), token) {
				t.Fatalf("leaked returned token: %v", err)
			}
		})
	}
}

func TestTokenSourceInvalidToken(t *testing.T) {
	for _, token := range []string{"", "bad token", " token", "token\n", "token\u00a0", "token\x00"} {
		var requests atomic.Int32
		client, _ := testClientWithKey(t, "", func(http.ResponseWriter, *http.Request) { requests.Add(1) }, WithTokenSource(TokenSourceFunc(func(context.Context) (string, error) { return token, nil })))
		_, err := client.Raw(context.Background(), "/v1/test", nil)
		var auth *AuthError
		if !errors.As(err, &auth) || requests.Load() != 0 {
			t.Fatalf("token %q: %v", token, err)
		}
	}
}

func TestTokenSourceCancellationAndConcurrentUse(t *testing.T) {
	t.Run("cancellation", func(t *testing.T) {
		started := make(chan struct{})
		source := TokenSourceFunc(func(ctx context.Context) (string, error) { close(started); <-ctx.Done(); return "", ctx.Err() })
		client, err := NewClient("", WithTokenSource(source))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() { _, err := client.Raw(ctx, "/v1/test", nil); done <- err }()
		<-started
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("token acquisition did not cancel")
		}
	})
	t.Run("concurrent", func(t *testing.T) {
		var tokens atomic.Int32
		source := TokenSourceFunc(func(context.Context) (string, error) { return fmt.Sprintf("parallel-secret-%d", tokens.Add(1)), nil })
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(401)
			fmt.Fprint(w, r.Header.Get("Authorization"))
		}))
		defer server.Close()
		client, err := NewClient("", WithBaseURL(server.URL), WithTokenSource(source))
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := client.Raw(context.Background(), "/v1/test", nil)
				if err == nil || strings.Contains(err.Error(), "parallel-secret") {
					t.Errorf("unsafe error %v", err)
				}
			}()
		}
		wg.Wait()
		if tokens.Load() != 20 {
			t.Fatalf("expected one token acquisition per call, got %d", tokens.Load())
		}
	})
}

func TestTokenSource401DoesNotRetry(t *testing.T) {
	var calls atomic.Int32
	client, _ := testClientWithKey(t, "", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) }, WithTokenSource(TokenSourceFunc(func(context.Context) (string, error) { calls.Add(1); return "expired-token", nil })))
	_, err := client.Raw(context.Background(), "/v1/test", nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 401 || calls.Load() != 1 {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
}
