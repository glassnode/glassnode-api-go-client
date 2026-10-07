package glassnode

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestInterruptedSuccessfulBodyRetries(t *testing.T) {
	var attempts atomic.Int32
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Content-Length", "100")
			fmt.Fprint(w, "[")
			return
		}
		fmt.Fprint(w, `[]`)
	}, WithRetryPolicy(RetryPolicy{1, time.Millisecond, time.Millisecond}))
	points, err := client.GetTimeSeries(context.Background(), "market/price", nil)
	if err != nil || points == nil || attempts.Load() != 2 {
		t.Fatalf("points=%v err=%v attempts=%d", points, err, attempts.Load())
	}
}

func TestPerAttemptTimeoutRetries(t *testing.T) {
	var attempts atomic.Int32
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) { attempts.Add(1); <-r.Context().Done() }, WithTimeout(100*time.Millisecond), WithRetryPolicy(RetryPolicy{2, time.Millisecond, time.Millisecond}))
	_, err := client.Raw(context.Background(), "/v1/test", nil)
	if !errors.Is(err, context.DeadlineExceeded) || attempts.Load() != 3 {
		t.Fatalf("err=%v attempts=%d", err, attempts.Load())
	}
}

func TestRetryAfterHTTPDate(t *testing.T) {
	var attempts atomic.Int32
	var deadline atomic.Int64
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			earliest := time.Now().UTC().Truncate(time.Second).Add(2 * time.Second)
			deadline.Store(earliest.UnixNano())
			w.Header().Set("Retry-After", earliest.Format(http.TimeFormat))
			w.WriteHeader(429)
			return
		}
		if time.Now().UnixNano() < deadline.Load() {
			t.Error("retried before HTTP date permitted")
		}
		fmt.Fprint(w, `[]`)
	}, WithRetryPolicy(RetryPolicy{1, time.Millisecond, 3 * time.Second}))
	if _, err := client.Raw(context.Background(), "/v1/test", nil); err != nil || attempts.Load() != 2 {
		t.Fatalf("err=%v attempts=%d", err, attempts.Load())
	}
}

func TestExplicitTimeoutIndependentOfOptionOrder(t *testing.T) {
	original := &http.Client{Timeout: time.Hour}
	for _, timeout := range []time.Duration{0, time.Second} {
		for _, options := range [][]Option{{WithTimeout(timeout), WithHTTPClient(original)}, {WithHTTPClient(original), WithTimeout(timeout)}} {
			client, err := NewClient("key", options...)
			if err != nil || client.httpClient.Timeout != timeout {
				t.Fatalf("client=%v err=%v", client, err)
			}
		}
	}
	if original.Timeout != time.Hour || original.CheckRedirect != nil {
		t.Fatal("mutated caller's HTTP client")
	}
}

func TestBaseURLPrefixAndUserAgent(t *testing.T) {
	for _, agent := range []string{"glassnode-api-go-client", "my-application/1.0"} {
		_, server := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/proxy/v1/test" || r.Header.Get("User-Agent") != agent {
				t.Errorf("path=%s agent=%s", r.URL.Path, r.Header.Get("User-Agent"))
			}
			fmt.Fprint(w, `[]`)
		})
		client, err := NewClient("test-secret-key", WithBaseURL(server.URL+"/proxy/"), WithUserAgent(agent))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.Raw(context.Background(), "/v1/test", nil); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInvalidUserAgent(t *testing.T) {
	for _, agent := range []string{"", "  ", "app\x00", "app\tname", "app\r\nX-Injected: 1", "app\x7f"} {
		_, err := NewClient("test-secret-key", WithUserAgent(agent))
		var inputErr *InputError
		if !errors.As(err, &inputErr) || inputErr.Field != "user agent" {
			t.Errorf("WithUserAgent(%q): got %v, want user agent InputError", agent, err)
		}
	}
}

type rejectingDecoder struct{ cause error }

func (d *rejectingDecoder) UnmarshalJSON([]byte) error { return d.cause }

func TestUsefulRedactedErrorCauses(t *testing.T) {
	t.Run("query transport", func(t *testing.T) {
		sentinel := errors.New("DNS lookup failed test-secret-key")
		client, err := NewClient("test-secret-key", WithAPIKeyInQuery(), WithHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, sentinel })}), WithRetryPolicy(RetryPolicy{}))
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.Raw(context.Background(), "/v1/test", nil)
		if !errors.Is(err, sentinel) || strings.Contains(err.Error(), "test-secret-key") || !strings.Contains(err.Error(), "DNS lookup failed") {
			t.Fatalf("bad diagnostic: %v", err)
		}
	})
	t.Run("custom decoder with refreshed token", func(t *testing.T) {
		sentinel := errors.New("invalid custom shape live-token-secret")
		client, _ := testClientWithKey(t, "", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{}`) }, WithTokenSource(TokenSourceFunc(func(context.Context) (string, error) { return "live-token-secret", nil })))
		destination := rejectingDecoder{cause: sentinel}
		err := client.Get(context.Background(), "/v1/test", nil, &destination)
		var decode *DecodeError
		if !errors.As(err, &decode) || !errors.Is(err, sentinel) || strings.Contains(err.Error(), "live-token-secret") || !strings.Contains(err.Error(), "invalid custom shape") {
			t.Fatalf("bad diagnostic: %v", err)
		}
	})
	t.Run("JSON API message", func(t *testing.T) {
		client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(403)
			fmt.Fprint(w, `{"message":"upgrade access test-secret-key"}`)
		})
		_, err := client.Raw(context.Background(), "/v1/test", nil)
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Detail != "upgrade access [redacted]" {
			t.Fatalf("bad detail: %v", err)
		}
	})
}

func TestAPIErrorDetailRedactedBeforeTruncation(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(502)
		fmt.Fprint(w, strings.Repeat("z", 295)+"test-secret-key"+strings.Repeat("z", 100))
	})
	_, err := client.Raw(context.Background(), "/v1/test", nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || len([]rune(apiErr.Detail)) != 303 || !strings.HasSuffix(apiErr.Detail, "...") || strings.Contains(apiErr.Detail, "test-") {
		t.Fatalf("bad bounded detail: %v", err)
	}
}

func TestInterruptedBodyIsRetried(t *testing.T) {
	var attempts atomic.Int32
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			// Announce more bytes than are sent, so the client sees an
			// unexpected EOF while decoding.
			w.Header().Set("Content-Length", "100")
			fmt.Fprint(w, `[{"t":1,"v":1},`)
			return
		}
		fmt.Fprint(w, `[{"t":1,"v":1},{"t":2,"v":2}]`)
	}, WithRetryPolicy(RetryPolicy{2, time.Millisecond, 5 * time.Millisecond}))
	points, err := client.GetTimeSeries(context.Background(), "market/price", nil)
	if err != nil || len(points) != 2 || attempts.Load() != 2 {
		t.Fatalf("points=%v err=%v attempts=%d", points, err, attempts.Load())
	}
}

func TestInvalidJSONIsNotRetried(t *testing.T) {
	var attempts atomic.Int32
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		fmt.Fprint(w, `[{"t":1,"v":1} oops`)
	}, WithRetryPolicy(RetryPolicy{2, time.Millisecond, 5 * time.Millisecond}))
	_, err := client.GetTimeSeries(context.Background(), "market/price", nil)
	var decodeErr *DecodeError
	if !errors.As(err, &decodeErr) || attempts.Load() != 1 {
		t.Fatalf("err=%v attempts=%d", err, attempts.Load())
	}
}

func TestMaxResponseBytes(t *testing.T) {
	body := `[{"t":1,"v":1},{"t":2,"v":2}]`
	var attempts atomic.Int32
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		fmt.Fprint(w, body)
	}, WithRetryPolicy(RetryPolicy{2, time.Millisecond, 5 * time.Millisecond}), WithMaxResponseBytes(int64(len(body))-1))
	if _, err := client.GetTimeSeries(context.Background(), "market/price", nil); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("Get: %v", err)
	}
	if _, err := client.Raw(context.Background(), "/v1/test", nil); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("Raw: %v", err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("retried a too-large response: attempts=%d", attempts.Load())
	}
	exact, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }, WithMaxResponseBytes(int64(len(body))))
	if got, err := exact.Raw(context.Background(), "/v1/test", nil); err != nil || string(got) != body {
		t.Fatalf("exact limit: %s %v", got, err)
	}
	if _, err := NewClient("key", WithMaxResponseBytes(-1)); err == nil {
		t.Fatal("accepted negative limit")
	}
}

func TestDefaultRetryCoversRateLimitWindow(t *testing.T) {
	client, err := NewClient("key")
	if err != nil {
		t.Fatal(err)
	}
	if client.retry.MaxDelay < 60*time.Second {
		t.Fatalf("default MaxDelay %v does not cover x-rate-limit-reset of up to 60s", client.retry.MaxDelay)
	}
}
