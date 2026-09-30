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
	"unicode"
)

// Client calls the Glassnode API. Its configuration is immutable after
// construction and it is safe for concurrent use.
type Client struct {
	baseURL     string
	apiKey      string
	bearerToken string
	tokenSource TokenSource
	queryAuth   bool
	httpClient  *http.Client
	retry       RetryPolicy
	userAgent   string
	timeout     *time.Duration
}

// NewClient constructs a client with header API-key authentication, a one-minute
// timeout per attempt and two GET retries. Configure exactly one of an API key,
// WithBearerToken or WithTokenSource. Explicit WithTimeout overrides a supplied
// HTTP client timeout regardless of option order; other options apply in order.
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
	if c.apiKey != "" && (strings.TrimSpace(c.apiKey) != c.apiKey || strings.IndexFunc(c.apiKey, unicode.IsControl) >= 0) {
		return nil, &InputError{"API key", "must contain no surrounding whitespace or control characters"}
	}
	modes := 0
	for _, enabled := range []bool{c.apiKey != "", c.bearerToken != "", c.tokenSource != nil} {
		if enabled {
			modes++
		}
	}
	if modes != 1 {
		return nil, &InputError{"authentication", "configure exactly one API key, bearer token or token source"}
	}
	if c.queryAuth && c.apiKey == "" {
		return nil, &InputError{"authentication", "query authentication requires an API key"}
	}
	if c.timeout != nil {
		c.httpClient.Timeout = *c.timeout
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
	if c.queryAuth {
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
	body, _, err := c.raw(ctx, endpoint, params)
	return body, err
}

func (c *Client) raw(ctx context.Context, endpoint string, params url.Values) ([]byte, *redactor, error) {
	r := &redactor{secrets: []string{c.apiKey, c.bearerToken}}
	u, err := c.buildURL(endpoint, params)
	if err != nil {
		return nil, r, err
	}
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, r, err
		}
		body, header, err := c.send(ctx, endpoint, u, r)
		if err == nil {
			return body, r, nil
		}
		if ctx.Err() != nil {
			return nil, r, ctx.Err()
		}
		var authErr *AuthError
		if errors.As(err, &authErr) {
			return nil, r, err
		}
		if attempt >= c.retry.MaxRetries {
			return nil, r, err
		}
		var apiErr *APIError
		var transportErr *TransportError
		if errors.As(err, &apiErr) {
			if !apiErr.Retryable() {
				return nil, r, err
			}
		} else if !errors.As(err, &transportErr) {
			return nil, r, err
		}
		delay := c.retryDelay(attempt)
		if retryAfter, ok := parseRetryAfter(header.Get("Retry-After"), time.Now()); ok {
			if retryAfter > c.retry.MaxDelay {
				return nil, r, err
			}
			if retryAfter > delay {
				delay = retryAfter
			}
		}
		if err := wait(ctx, delay); err != nil {
			return nil, r, err
		}
	}
}

func (c *Client) send(ctx context.Context, endpoint string, u *url.URL, r *redactor) ([]byte, http.Header, error) {
	token := c.bearerToken
	if c.tokenSource != nil {
		var err error
		token, err = c.tokenSource.Token(ctx)
		if token != "" {
			r.secrets = append(r.secrets, token)
		}
		if err != nil {
			return nil, nil, &AuthError{r.safe(err)}
		}
		if !validBearerToken(token) {
			return nil, nil, &AuthError{errors.New("token source returned an empty or whitespace-containing bearer token")}
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, nil, &TransportError{endpoint, r.safe(err)}
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	} else if !c.queryAuth {
		req.Header.Set("X-Api-Key", c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, &TransportError{endpoint, r.safe(err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		detail := []rune(r.redact(apiErrorDetail(body)))
		if len(detail) > 300 {
			detail = append(detail[:300], []rune("...")...)
		}
		return nil, resp.Header, &APIError{resp.StatusCode, endpoint, string(detail)}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.Header, &TransportError{endpoint, r.safe(err)}
	}
	return body, resp.Header, nil
}

func (c *Client) redact(text string) string {
	return (&redactor{secrets: []string{c.apiKey, c.bearerToken}}).redact(text)
}

// A redactor belongs to one call, retaining tokens across retries without
// shared mutable state or keeping refreshed credentials on the Client.
type redactor struct{ secrets []string }

func (r *redactor) redact(text string) string {
	for _, secret := range r.secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[redacted]")
			text = strings.ReplaceAll(text, url.QueryEscape(secret), "[redacted]")
			text = strings.ReplaceAll(text, url.PathEscape(secret), "[redacted]")
			if encoded, err := json.Marshal(secret); err == nil && len(encoded) > 2 {
				text = strings.ReplaceAll(text, string(encoded[1:len(encoded)-1]), "[redacted]")
			}
		}
	}
	return credentialPattern.ReplaceAllString(text, "${1}[redacted]")
}

func (r *redactor) safe(err error) error {
	return &redactedError{cause: err, message: r.redact(err.Error())}
}

type redactedError struct {
	cause   error
	message string
}

func (e *redactedError) Error() string { return e.message }
func (e *redactedError) Unwrap() error { return e.cause }

func apiErrorDetail(body []byte) string {
	var response struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if json.Unmarshal(body, &response) == nil {
		if response.Message != "" {
			return response.Message
		}
		if response.Error != "" {
			return response.Error
		}
	}
	return strings.TrimSpace(string(body))
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
	body, r, err := c.raw(ctx, endpoint, params)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(dst); err != nil {
		return &DecodeError{endpoint, r.safe(err)}
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return &DecodeError{endpoint, r.safe(err)}
	}
	return nil
}
