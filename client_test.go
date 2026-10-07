package glassnode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(t *testing.T, handler http.HandlerFunc, options ...Option) (*Client, *httptest.Server) {
	t.Helper()
	return testClientWithKey(t, "test-secret-key", handler, options...)
}

func testClientWithKey(t *testing.T, key string, handler http.HandlerFunc, options ...Option) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	opts := []Option{WithBaseURL(server.URL), WithHTTPClient(server.Client()), WithRetryPolicy(RetryPolicy{})}
	opts = append(opts, options...)
	client, err := NewClient(key, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return client, server
}

func TestMetricWireContract(t *testing.T) {
	var calls atomic.Int32
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/metrics/market/price_usd_close" || r.URL.Query().Get("a") != "BTC" || r.URL.Query().Get("s") != "1700000000" || r.URL.Query().Get("i") != "24h" || r.URL.Query().Get("f") != "json" {
			t.Errorf("request %s", r.URL)
		}
		if r.Header.Get("X-Api-Key") != "test-secret-key" || r.URL.Query().Has("api_key") {
			t.Error("expected header-only API key")
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Error("missing Accept")
		}
		fmt.Fprint(w, `[{"t":1700000000,"v":12.5,"new_field":true},{"t":1700000001,"v":null}]`)
	})
	points, err := client.GetTimeSeries(context.Background(), "market/price_usd_close", &MetricParams{Asset: "BTC", Since: time.Unix(1700000000, 0), Interval: "24h"})
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 2 || points[0].Timestamp != 1700000000 || *points[0].Value != 12.5 || points[1].Value != nil {
		t.Fatalf("points %#v", points)
	}
	if calls.Load() != 1 {
		t.Fatal("unexpected calls")
	}
}

func TestAuthModesAndRedirects(t *testing.T) {
	for _, mode := range []string{"header", "query", "bearer"} {
		t.Run(mode, func(t *testing.T) {
			var targetCalls atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls.Add(1) }))
			defer target.Close()
			options := []Option{}
			key := "test-secret-key"
			if mode == "query" {
				options = append(options, WithAPIKeyInQuery())
			}
			if mode == "bearer" {
				key = ""
				options = append(options, WithBearerToken("bearer-secret"))
			}
			client, _ := testClientWithKey(t, key, func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "header":
					if r.Header.Get("X-Api-Key") != "test-secret-key" {
						t.Error("header auth")
					}
				case "query":
					if r.URL.Query().Get("api_key") != "test-secret-key" || r.Header.Get("X-Api-Key") != "" {
						t.Error("query auth")
					}
				case "bearer":
					if r.Header.Get("Authorization") != "Bearer bearer-secret" || r.URL.Query().Has("api_key") || r.Header.Get("X-Api-Key") != "" {
						t.Error("bearer auth")
					}
				}
				http.Redirect(w, r, target.URL, http.StatusFound)
			}, options...)
			_, err := client.Raw(context.Background(), "/v1/test", nil)
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != 302 || targetCalls.Load() != 0 {
				t.Fatalf("redirect: calls=%d err=%v", targetCalls.Load(), err)
			}
		})
	}
}

func TestInputRejectedBeforeRequest(t *testing.T) {
	var calls atomic.Int32
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
	for _, path := range []string{"https://evil.example/a", "//evil.example/a", "market/../price", "market/%2e%2e/price", "market/price?a=ETH", "market/price#x", ""} {
		_, err := client.GetTimeSeries(context.Background(), path, nil)
		var input *InputError
		if !errors.As(err, &input) {
			t.Errorf("path %q: %v", path, err)
		}
	}
	for _, params := range []*MetricParams{
		{Asset: "BTC", Assets: []string{"ETH"}}, {Assets: []string{"BTC", ""}},
		{Extra: url.Values{"a": {}}}, {Extra: url.Values{"api_key": {"override"}}},
		{Extra: url.Values{"f": {"csv"}}}, {Extra: url.Values{"i": {"1h", "24h"}}},
		{Asset: "BTC", Extra: url.Values{"a": {"ETH"}}},
		{Since: time.Unix(200, 0), Until: time.Unix(100, 0)}, {Extra: url.Values{"s": {"invalid"}}},
	} {
		_, err := client.GetTimeSeries(context.Background(), "market/price", params)
		var input *InputError
		if !errors.As(err, &input) {
			t.Errorf("params %#v: %v", params, err)
		}
	}
	var dst any
	for _, dest := range []any{nil, dst, (*int)(nil), 123} {
		if err := client.Get(context.Background(), "/v1/test", nil, dest); err == nil {
			t.Error("accepted invalid destination")
		}
	}
	if _, err := client.GetBulkMetric(context.Background(), "market/price/bulk", nil); err == nil {
		t.Error("accepted bulk suffix")
	}
	if _, err := client.GetMetricMetadata(context.Background(), "market/price", &MetricParams{Extra: url.Values{"path": {"/other"}}}); err == nil {
		t.Error("accepted reserved path")
	}
	if calls.Load() != 0 {
		t.Fatalf("sent %d requests", calls.Load())
	}
}

func TestErrorsAndRedaction(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `test-secret-key api_key=other-secret Bearer token-secret`)
	})
	_, err := client.Raw(context.Background(), "/v1/test", nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 429 || !apiErr.Retryable() {
		t.Fatalf("error %v", err)
	}
	for _, secret := range []string{"test-secret-key", "other-secret", "token-secret"} {
		if strings.Contains(err.Error(), secret) || strings.Contains(apiErr.Detail, secret) {
			t.Errorf("leaked %s", secret)
		}
	}
}

func TestRetriesAndRetryAfter(t *testing.T) {
	for _, status := range []int{429, 500, 503, 400, 401, 403, 404} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var attempts atomic.Int32
			client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) { attempts.Add(1); w.WriteHeader(status) }, WithRetryPolicy(RetryPolicy{2, time.Millisecond, 5 * time.Millisecond}))
			_, err := client.Raw(context.Background(), "/v1/test", nil)
			if err == nil {
				t.Fatal("missing error")
			}
			want := int32(1)
			if status == 429 || status >= 500 {
				want = 3
			}
			if attempts.Load() != want {
				t.Errorf("attempts=%d want %d", attempts.Load(), want)
			}
		})
	}
	var attempts atomic.Int32
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Retry-After", "1000")
		w.WriteHeader(429)
	}, WithRetryPolicy(RetryPolicy{2, time.Millisecond, time.Second}))
	_, err := client.Raw(context.Background(), "/v1/test", nil)
	if err == nil || attempts.Load() != 1 {
		t.Fatal("retried before server permits")
	}
	now := time.Now().UTC().Truncate(time.Second)
	for _, value := range []string{"2", now.Add(2 * time.Second).Format(http.TimeFormat)} {
		if delay, ok := parseRetryAfter(value, now); !ok || delay != 2*time.Second {
			t.Errorf("Retry-After %s: %v %v", value, delay, ok)
		}
	}
	if _, ok := parseRetryAfter("-1", now); ok {
		t.Fatal("negative retry delay")
	}
}

func TestCancellationDuringRequestAndBackoff(t *testing.T) {
	for _, phase := range []string{"request", "backoff"} {
		t.Run(phase, func(t *testing.T) {
			started := make(chan struct{}, 1)
			var attempts atomic.Int32
			client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				started <- struct{}{}
				if phase == "request" {
					<-r.Context().Done()
					return
				}
				w.Header().Set("Retry-After", "20")
				w.WriteHeader(429)
			}, WithRetryPolicy(RetryPolicy{2, time.Second, 30 * time.Second}))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := client.Raw(ctx, "/v1/test", nil); done <- err }()
			<-started
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("error %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("cancellation blocked")
			}
			if attempts.Load() != 1 {
				t.Fatal("retried cancellation")
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTransportRetriesAndUnwrap(t *testing.T) {
	sentinel := errors.New("private transport cause test-secret-key")
	var attempts atomic.Int32
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { attempts.Add(1); return nil, sentinel })}
	client, err := NewClient("test-secret-key", WithHTTPClient(httpClient), WithRetryPolicy(RetryPolicy{2, time.Millisecond, time.Millisecond}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Raw(context.Background(), "/v1/test", nil)
	var transport *TransportError
	if !errors.As(err, &transport) || !errors.Is(err, sentinel) || strings.Contains(err.Error(), "test-secret-key") || attempts.Load() != 3 {
		t.Fatalf("error=%v attempts=%d", err, attempts.Load())
	}
}

func TestTimeoutAndHTTPClientIsolation(t *testing.T) {
	client, server := testClient(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }, WithTimeout(10*time.Millisecond))
	_, err := client.Raw(context.Background(), "/v1/test", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error %v", err)
	}
	original := server.Client()
	original.Timeout = time.Minute
	_, err = NewClient("key", WithHTTPClient(original), WithTimeout(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if original.Timeout != time.Minute || original.CheckRedirect != nil {
		t.Fatal("mutated supplied HTTP client")
	}
}

func TestConcurrentClientAndQueryIsolation(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query()["a"]; len(got) != 2 || got[0] != "BTC" || got[1] != "ETH" {
			t.Errorf("assets %v", got)
		}
		fmt.Fprint(w, `{"data":[]}`)
	})
	params := &MetricParams{Assets: []string{"BTC", "ETH"}, Extra: url.Values{"network": {"eth", "sol"}}}
	var group sync.WaitGroup
	for i := 0; i < 20; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, err := client.GetBulkMetric(context.Background(), "market/price", params); err != nil {
				t.Error(err)
			}
		}()
	}
	group.Wait()
	if params.Extra.Has("f") || params.Extra.Has("api_key") || params.Extra.Has("a") {
		t.Fatal("mutated query")
	}
}

func TestDecodeCustomShapeAndPrecision(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"t":1700000000,"v":{"large":9007199254740993,"nested":[1,null]}}]`)
	})
	var result []struct {
		T int64          `json:"t"`
		V map[string]any `json:"v"`
	}
	if err := client.GetMetric(context.Background(), "custom/metric", nil, &result); err != nil {
		t.Fatal(err)
	}
	if result[0].V["large"] != json.Number("9007199254740993") {
		t.Fatalf("lost precision: %v", result)
	}
	var raw json.RawMessage
	if err := client.GetMetric(context.Background(), "custom/metric", nil, &raw); err != nil || !strings.Contains(string(raw), "9007199254740993") {
		t.Fatalf("raw: %s %v", raw, err)
	}
}

func TestInvalidJSONAndShapeNeverRetry(t *testing.T) {
	for _, body := range []string{`not JSON`, `[] {}`, `null`, `{}`, `[{"v":3}]`, `[{"t":1}]`, `[{"t":1,"v":"bad"}]`} {
		t.Run(body, func(t *testing.T) {
			var attempts atomic.Int32
			client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) { attempts.Add(1); io.WriteString(w, body) }, WithRetryPolicy(RetryPolicy{2, time.Millisecond, time.Millisecond}))
			_, err := client.GetTimeSeries(context.Background(), "market/price", nil)
			var decode *DecodeError
			if !errors.As(err, &decode) || attempts.Load() != 1 {
				t.Fatalf("err=%v attempts=%d", err, attempts.Load())
			}
		})
	}
}

func TestConstructorValidation(t *testing.T) {
	for _, options := range [][]Option{{nil}, {WithHTTPClient(nil)}, {WithTimeout(-1)}, {WithBaseURL("https://user:secret@host")}, {WithBaseURL("https://host?key=x")}, {WithBaseURL("file:///tmp")}, {WithBearerToken("")}, {WithRetryPolicy(RetryPolicy{-1, 0, 0})}, {WithRetryPolicy(RetryPolicy{1, 0, 0})}} {
		if _, err := NewClient("key", options...); err == nil {
			t.Errorf("accepted invalid options")
		}
	}
	if _, err := NewClient(""); err == nil {
		t.Fatal("accepted missing auth")
	}
	if _, err := NewClient("", WithBearerToken("token")); err != nil {
		t.Fatal(err)
	}
}

func TestRetryAfterMinimumWait(t *testing.T) {
	var attempts atomic.Int32
	var first atomic.Int64
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			first.Store(time.Now().UnixNano())
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			return
		}
		if time.Since(time.Unix(0, first.Load())) < time.Second {
			t.Error("retried before Retry-After elapsed")
		}
		fmt.Fprint(w, `[]`)
	}, WithRetryPolicy(RetryPolicy{1, time.Millisecond, 2 * time.Second}))
	if _, err := client.GetTimeSeries(context.Background(), "market/price", nil); err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 2 {
		t.Fatal("expected one retry")
	}
}

func TestCredentialRedactionVariants(t *testing.T) {
	secret := `secret"with spaces&chars`
	r := &redactor{secrets: []string{secret}}
	encoded, _ := json.Marshal(secret)
	for _, text := range []string{secret, url.QueryEscape(secret), url.PathEscape(secret), string(encoded)} {
		if got := r.redact(text); strings.Contains(got, "secret") {
			t.Errorf("failed redaction: %s", got)
		}
	}
}
