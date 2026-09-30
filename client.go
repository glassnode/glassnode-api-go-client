package glassnode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Client calls the Glassnode API. Its configuration is immutable after
// construction and it is safe for concurrent use.
type Client struct {
	baseURL     string
	apiKey      string
	bearerToken string
	queryAuth   bool
	httpClient  *http.Client
	retry       RetryPolicy
	userAgent   string
}

// NewClient constructs a client with header API-key authentication, a one-minute
// timeout per attempt and two GET retries. Options apply in order. An API key
// is required unless WithBearerToken is supplied.
func NewClient(apiKey string, options ...Option) (*Client, error) {
	c := &Client{
		baseURL: DefaultBaseURL, apiKey: apiKey,
		httpClient: &http.Client{Timeout: time.Minute},
		retry:      RetryPolicy{MaxRetries: 2, BaseDelay: time.Second, MaxDelay: 30 * time.Second},
		userAgent:  "glassnode-api-go-client",
	}
	for _, option := range options {
		if option == nil {
			return nil, &InputError{"option", "must not be nil"}
		}
		if err := option(c); err != nil {
			return nil, err
		}
	}
	if c.bearerToken == "" && strings.TrimSpace(c.apiKey) == "" {
		return nil, &InputError{"API key", "required unless a bearer token is configured"}
	}
	if strings.ContainsAny(c.apiKey, "\r\n") {
		return nil, &InputError{"API key", "must contain no line breaks"}
	}
	// Never forward authentication to a redirect target, including subdomains.
	c.httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return c, nil
}

var endpointPattern = regexp.MustCompile(`^/(?:[A-Za-z0-9_-]+/)*[A-Za-z0-9_-]+$`)
var queryKeyPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)
var credentialPattern = regexp.MustCompile(`(?i)(api_key=|X-Api-Key["\s:=]+|Bearer\s+)[^\s&"<>]+`)

func validateEndpoint(endpoint string) error {
	if !endpointPattern.MatchString(endpoint) {
		return &InputError{"endpoint", "expected a slash-separated API path without URL, query or traversal"}
	}
	return nil
}

func cloneQuery(params url.Values) (url.Values, error) {
	q := make(url.Values, len(params)+1)
	for k, values := range params {
		if !queryKeyPattern.MatchString(k) {
			return nil, &InputError{"query parameter", "invalid parameter name"}
		}
		if strings.EqualFold(k, "api_key") {
			return nil, &InputError{"api_key", "configure authentication on the client"}
		}
		if len(values) == 0 {
			return nil, &InputError{k, "must contain at least one value"}
		}
		for _, value := range values {
			if value == "" {
				return nil, &InputError{k, "must not contain an empty value"}
			}
		}
		if k == "f" && (len(values) != 1 || !strings.EqualFold(values[0], "json")) {
			return nil, &InputError{"f", "only JSON is supported"}
		}
		q[k] = append([]string(nil), values...)
	}
	return q, nil
}

func (c *Client) buildURL(endpoint string, params url.Values) (*url.URL, error) {
	if err := validateEndpoint(endpoint); err != nil {
		return nil, err
	}
	q, err := cloneQuery(params)
	if err != nil {
		return nil, err
	}
	if c.queryAuth && c.bearerToken == "" {
		q.Set("api_key", c.apiKey)
	}
	u, err := url.Parse(c.baseURL + endpoint)
	if err != nil {
		return nil, &InputError{"endpoint", "cannot build URL"}
	}
	u.RawQuery = q.Encode()
	return u, nil
}

// Raw calls a GET endpoint and returns its successful response bytes. It is an
// escape hatch for new endpoints; it does not decode or validate response JSON.
// Query maps are copied and reserved authentication parameters are rejected.
func (c *Client) Raw(ctx context.Context, endpoint string, params url.Values) ([]byte, error) {
	u, err := c.buildURL(endpoint, params)
	if err != nil {
		return nil, err
	}
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		body, header, err := c.send(ctx, endpoint, u)
		if err == nil {
			return body, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if attempt >= c.retry.MaxRetries {
			return nil, err
		}
		var apiErr *APIError
		var transportErr *TransportError
		if errors.As(err, &apiErr) {
			if !apiErr.Retryable() {
				return nil, err
			}
		} else if !errors.As(err, &transportErr) {
			return nil, err
		}
		delay := c.retryDelay(attempt)
		if retryAfter, ok := parseRetryAfter(header.Get("Retry-After"), time.Now()); ok {
			if retryAfter > c.retry.MaxDelay {
				return nil, err
			}
			if retryAfter > delay {
				delay = retryAfter
			}
		}
		if err := wait(ctx, delay); err != nil {
			return nil, err
		}
	}
}

func (c *Client) send(ctx context.Context, endpoint string, u *url.URL) ([]byte, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, nil, &TransportError{endpoint, err}
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	if c.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearerToken)
	} else if !c.queryAuth {
		req.Header.Set("X-Api-Key", c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, &TransportError{endpoint, err}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, resp.Header, &APIError{resp.StatusCode, endpoint, c.redact(strings.TrimSpace(string(body)))}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.Header, &TransportError{endpoint, err}
	}
	return body, resp.Header, nil
}

func (c *Client) redact(text string) string {
	for _, secret := range []string{c.apiKey, c.bearerToken} {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[redacted]")
			text = strings.ReplaceAll(text, url.QueryEscape(secret), "[redacted]")
			text = strings.ReplaceAll(text, url.PathEscape(secret), "[redacted]")
			if encoded, err := json.Marshal(secret); err == nil && len(encoded) > 2 {
				text = strings.ReplaceAll(text, string(encoded[1:len(encoded)-1]), "[redacted]")
			}
		}
	}
	return credentialPattern.ReplaceAllString(text, "[redacted]")
}

func (c *Client) retryDelay(attempt int) time.Duration {
	upper := c.retry.BaseDelay
	for i := 0; i < attempt; i++ {
		if upper >= c.retry.MaxDelay/2 {
			upper = c.retry.MaxDelay
			break
		}
		upper *= 2
	}
	if upper > c.retry.MaxDelay {
		upper = c.retry.MaxDelay
	}
	if upper <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(upper)))
}

func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	if seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
		if seconds < 0 {
			return 0, false
		}
		if seconds > int64((1<<63-1)/int64(time.Second)) {
			return time.Duration(1<<63 - 1), true
		}
		return time.Duration(seconds) * time.Second, true
	}
	if deadline, err := http.ParseTime(value); err == nil {
		delay := deadline.Sub(now)
		if delay < 0 {
			delay = 0
		}
		return delay, true
	}
	return 0, false
}

func wait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Get decodes a GET response into dst. Use a non-nil pointer (including
// *json.RawMessage). Dynamic values decode as json.Number rather than float64.
// Custom UnmarshalJSON methods allow application-specific validation.
func (c *Client) Get(ctx context.Context, endpoint string, params url.Values, dst any) error {
	if dst == nil || reflect.ValueOf(dst).Kind() != reflect.Pointer || reflect.ValueOf(dst).IsNil() {
		return &InputError{"destination", "must be a non-nil pointer"}
	}
	body, err := c.Raw(ctx, endpoint, params)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(dst); err != nil {
		return &DecodeError{endpoint, err}
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return &DecodeError{endpoint, err}
	}
	return nil
}
