package glassnode

import (
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
	maxBytes    int64
}

// defaultMaxDelay covers the API's one-minute rate-limit window, so a 429 at
// the start of a window is still retried once the window resets.
const defaultMaxDelay = 65 * time.Second

// ErrResponseTooLarge reports a response body above the WithMaxResponseBytes
// limit. The call is not retried.
var ErrResponseTooLarge = errors.New("response exceeds the configured size limit")

// NewClient constructs a client with header API-key authentication, a one-minute
// timeout per attempt and two GET retries. Configure exactly one of an API key,
// WithBearerToken or WithTokenSource. Explicit WithTimeout overrides a supplied
// HTTP client timeout regardless of option order; other options apply in order.
func NewClient(apiKey string, options ...Option) (*Client, error) {
	c := &Client{
		baseURL: DefaultBaseURL, apiKey: apiKey,
		httpClient: &http.Client{Timeout: time.Minute},
		retry:      RetryPolicy{MaxRetries: 2, BaseDelay: time.Second, MaxDelay: defaultMaxDelay},
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

// Error bodies are short JSON messages; anything larger is not worth keeping
// or draining, and closing the body discards the connection instead.
const (
	maxErrorBody  = 4096
	maxErrorDrain = 1 << 20
)

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
	var body []byte
	err := c.do(ctx, endpoint, params, func(reader io.Reader) error {
		var err error
		body, err = io.ReadAll(reader)
		return err
	})
	if err != nil {
		return nil, err
	}
	return body, nil
}

// do sends a GET request with retries and passes each successful response
// body to consume. A read error inside consume is retried like any other
// transport failure; consume must therefore tolerate being called again.
func (c *Client) do(ctx context.Context, endpoint string, params url.Values, consume func(io.Reader) error) error {
	r := &redactor{secrets: []string{c.apiKey, c.bearerToken}}
	u, err := c.buildURL(endpoint, params)
	if err != nil {
		return err
	}
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := c.send(ctx, endpoint, u, r, consume)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var authErr *AuthError
		if errors.As(err, &authErr) || errors.Is(err, ErrResponseTooLarge) {
			return err
		}
		if attempt >= c.retry.MaxRetries {
			return err
		}
		var apiErr *APIError
		var transportErr *TransportError
		if errors.As(err, &apiErr) {
			if !apiErr.Retryable() {
				return err
			}
		} else if !errors.As(err, &transportErr) {
			return err
		}
		delay := c.retryDelay(attempt)
		if apiErr != nil {
			if apiErr.RetryAfter > c.retry.MaxDelay {
				return err
			}
			delay = max(delay, apiErr.RetryAfter)
		}
		if err := wait(ctx, delay); err != nil {
			return err
		}
	}
}

func (c *Client) send(ctx context.Context, endpoint string, u *url.URL, r *redactor, consume func(io.Reader) error) error {
	token := c.bearerToken
	if c.tokenSource != nil {
		var err error
		token, err = c.tokenSource.Token(ctx)
		if token != "" {
			r.secrets = append(r.secrets, token)
		}
		if err != nil {
			return &AuthError{r.safe(err)}
		}
		if !validBearerToken(token) {
			return &AuthError{errors.New("token source returned an empty or whitespace-containing bearer token")}
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return &TransportError{endpoint, r.safe(err)}
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
		return &TransportError{endpoint, r.safe(err)}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		// Drain a bounded remainder so the connection can be reused on retry.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorDrain))
		detail := []rune(r.redact(apiErrorDetail(body)))
		if len(detail) > 300 {
			detail = append(detail[:300], []rune("...")...)
		}
		return &APIError{resp.StatusCode, endpoint, string(detail), serverDelay(resp)}
	}
	reader := &bodyReader{reader: resp.Body, remaining: c.maxBytes, limited: c.maxBytes > 0}
	if err := consume(reader); err != nil {
		if reader.err != nil {
			// The body failed before consume could judge its content.
			return &TransportError{endpoint, r.safe(reader.err)}
		}
		return &DecodeError{endpoint, r.safe(err)}
	}
	return nil
}

// bodyReader separates transport failures from the errors of whatever
// consumes the body, and enforces the optional response size limit.
type bodyReader struct {
	reader    io.Reader
	remaining int64
	limited   bool
	err       error
}

func (b *bodyReader) Read(p []byte) (int, error) {
	if b.limited {
		if b.remaining <= 0 {
			b.err = ErrResponseTooLarge
			return 0, b.err
		}
		if int64(len(p)) > b.remaining {
			p = p[:b.remaining]
		}
	}
	n, err := b.reader.Read(p)
	b.remaining -= int64(n)
	if err != nil && err != io.EOF {
		b.err = err
	}
	return n, err
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

// serverDelay returns the wait requested by a response. The API reports rate
// limits through x-rate-limit-reset, the seconds until the window resets, and
// may not send Retry-After.
func serverDelay(resp *http.Response) time.Duration {
	now := time.Now()
	if delay, ok := parseRetryAfter(resp.Header.Get("Retry-After"), now); ok {
		return delay
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		if delay, ok := parseRetryAfter(resp.Header.Get("X-Rate-Limit-Reset"), now); ok {
			return delay
		}
	}
	return 0
}

func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	if seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
		if seconds < 0 {
			return 0, false
		}
		if seconds > (1<<63-1)/int64(time.Second) {
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
//
// The body is decoded as it streams in, so only the decoded value is held in
// memory. A read failure during decoding is retried, and dst is then decoded
// into again with encoding/json's usual semantics for existing values.
func (c *Client) Get(ctx context.Context, endpoint string, params url.Values, dst any) error {
	if dst == nil || reflect.ValueOf(dst).Kind() != reflect.Pointer || reflect.ValueOf(dst).IsNil() {
		return &InputError{"destination", "must be a non-nil pointer"}
	}
	return c.do(ctx, endpoint, params, func(body io.Reader) error {
		decoder := json.NewDecoder(body)
		decoder.UseNumber()
		if err := decoder.Decode(dst); err != nil {
			return err
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			if err == nil {
				err = errors.New("multiple JSON values")
			}
			return err
		}
		return nil
	})
}
