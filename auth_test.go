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

type refreshingSource struct {
	mu       sync.Mutex
	token    string
	next     string
	err      error
	delay    time.Duration // widens the window in which concurrent 401s overlap
	refreshs int
}

func (s *refreshingSource) Token(context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.token, nil
}

func (s *refreshingSource) Refresh(context.Context) (string, error) {
	time.Sleep(s.delay)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshs++
	if s.err != nil {
		return "", s.err
	}
	if s.next != "" {
		s.token = s.next
	}
	return s.token, nil
}

func TestTokenRefresherRetriesOnceAfter401(t *testing.T) {
	var headers []string
	var mu sync.Mutex
	handler := func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		headers = append(headers, r.Header.Get("Authorization"))
		n := len(headers)
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer fresh-token" {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprintf(w, `{"message":"token %s rejected"}`, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
			return
		}
		_ = n
		fmt.Fprint(w, `[]`)
	}
	t.Run("new token is used and reused", func(t *testing.T) {
		headers = nil
		source := &refreshingSource{token: "stale-token", next: "fresh-token"}
		client, _ := testClientWithKey(t, "", handler, WithTokenSource(source), WithRetryPolicy(RetryPolicy{2, time.Millisecond, 5 * time.Millisecond}))
		if _, err := client.GetTimeSeries(context.Background(), "market/price", nil); err != nil {
			t.Fatal(err)
		}
		if _, err := client.GetTimeSeries(context.Background(), "market/price", nil); err != nil {
			t.Fatal(err)
		}
		if len(headers) != 3 || headers[0] != "Bearer stale-token" || headers[1] != "Bearer fresh-token" || headers[2] != "Bearer fresh-token" || source.refreshs != 1 {
			t.Fatalf("headers=%v refreshes=%d", headers, source.refreshs)
		}
	})
	t.Run("second 401 is returned and both tokens are redacted", func(t *testing.T) {
		headers = nil
		source := &refreshingSource{token: "stale-token", next: "other-token"}
		client, _ := testClientWithKey(t, "", handler, WithTokenSource(source), WithRetryPolicy(RetryPolicy{2, time.Millisecond, 5 * time.Millisecond}))
		_, err := client.GetTimeSeries(context.Background(), "market/price", nil)
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != 401 || len(headers) != 2 {
			t.Fatalf("err=%v headers=%v", err, headers)
		}
		if strings.Contains(err.Error(), "stale-token") || strings.Contains(err.Error(), "other-token") || !strings.Contains(err.Error(), "[redacted]") {
			t.Fatalf("token leaked: %v", err)
		}
	})
	t.Run("unchanged token is not retried", func(t *testing.T) {
		headers = nil
		source := &refreshingSource{token: "stale-token"}
		client, _ := testClientWithKey(t, "", handler, WithTokenSource(source))
		_, err := client.GetTimeSeries(context.Background(), "market/price", nil)
		var apiErr *APIError
		if !errors.As(err, &apiErr) || len(headers) != 1 || source.refreshs != 1 {
			t.Fatalf("err=%v headers=%v refreshes=%d", err, headers, source.refreshs)
		}
	})
	t.Run("empty refreshed token is an AuthError", func(t *testing.T) {
		headers = nil
		source := &refreshingSource{token: "stale-token", next: " "}
		client, _ := testClientWithKey(t, "", handler, WithTokenSource(source))
		_, err := client.GetTimeSeries(context.Background(), "market/price", nil)
		var authErr *AuthError
		if !errors.As(err, &authErr) || len(headers) != 1 || !strings.Contains(err.Error(), "token refresher") {
			t.Fatalf("err=%v headers=%v", err, headers)
		}
	})
	t.Run("refresh failure is an AuthError", func(t *testing.T) {
		headers = nil
		source := &refreshingSource{token: "stale-token", err: errors.New("session expired, run login (stale-token)")}
		client, _ := testClientWithKey(t, "", handler, WithTokenSource(source))
		_, err := client.GetTimeSeries(context.Background(), "market/price", nil)
		var authErr *AuthError
		if !errors.As(err, &authErr) || len(headers) != 1 || strings.Contains(err.Error(), "stale-token") {
			t.Fatalf("err=%v headers=%v", err, headers)
		}
	})
	t.Run("concurrent 401s share one refresh", func(t *testing.T) {
		headers = nil
		source := &refreshingSource{token: "stale-token", next: "fresh-token", delay: 20 * time.Millisecond}
		client, _ := testClientWithKey(t, "", handler, WithTokenSource(source))
		var group sync.WaitGroup
		errs := make(chan error, 10)
		for range 10 {
			group.Add(1)
			go func() {
				defer group.Done()
				_, err := client.GetTimeSeries(context.Background(), "market/price", nil)
				errs <- err
			}()
		}
		group.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		stale, fresh := 0, 0
		for _, h := range headers {
			switch h {
			case "Bearer stale-token":
				stale++
			case "Bearer fresh-token":
				fresh++
			}
		}
		if source.refreshs != 1 || stale != 10 || fresh != 10 {
			t.Fatalf("refreshes=%d stale=%d fresh=%d", source.refreshs, stale, fresh)
		}
	})
	t.Run("plain token source gets the 401", func(t *testing.T) {
		headers = nil
		client, _ := testClientWithKey(t, "", handler, WithTokenSource(TokenSourceFunc(func(context.Context) (string, error) { return "stale-token", nil })))
		_, err := client.GetTimeSeries(context.Background(), "market/price", nil)
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != 401 || len(headers) != 1 {
			t.Fatalf("err=%v headers=%v", err, headers)
		}
	})
}
