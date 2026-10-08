package glassnode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"html"
	"io"
	"math/rand/v2"
	"mime"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
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
	refresh     *refreshState
}

// refreshState merges concurrent token refreshes: calls that were rejected
// with the same token share one Refresh instead of each asking the
// authorization server, which may rotate refresh tokens and invalidate the
// others' results.
type refreshState struct {
	mu          sync.Mutex
	rejected    string
	replacement string
}

// refreshToken returns a replacement for a token the API rejected, calling
// the TokenRefresher only if no other call has replaced that token yet.
func (c *Client) refreshToken(ctx context.Context, refresher TokenRefresher, rejected string) (string, error) {
	c.refresh.mu.Lock()
	defer c.refresh.mu.Unlock()
	if c.refresh.rejected == rejected && c.refresh.replacement != "" {
		return c.refresh.replacement, nil
	}
	fresh, err := refresher.Refresh(ctx)
	if err != nil {
		return fresh, err
	}
	c.refresh.rejected, c.refresh.replacement = rejected, fresh
	return fresh, nil
}

// defaultMaxDelay covers the API's one-minute rate-limit window, so a 429 at
// the start of a window is still retried once the window resets.
const defaultMaxDelay = 65 * time.Second

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
		refresh:    &refreshState{},
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
		if k == "f" && (len(values) != 1 || !strings.EqualFold(values[0], "json") && !strings.EqualFold(values[0], "csv")) {
			return nil, &InputError{"f", "only json and csv are supported"}
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
	body, _, err := c.do(ctx, endpoint, params)
	return body, err
}

// do sends a GET request with retries and returns the body of the first
// successful attempt. Nothing is decoded here, so a failed attempt leaves no
// trace in the caller's values.
func (c *Client) do(ctx context.Context, endpoint string, params url.Values) ([]byte, *redactor, error) {
	var body []byte
	r, err := c.doWith(ctx, endpoint, params, func(resp *http.Response) error {
		data, tooLarge, err := readBody(resp.Body, c.maxBytes)
		if err != nil {
			return &TransportError{endpoint, err}
		}
		if tooLarge {
			return &ResponseTooLargeError{endpoint, c.maxBytes}
		}
		body = data
		return nil
	})
	return body, r, err
}

// doWith sends a GET request with retries and hands each 2xx response to
// consume. consume returns the error to report: a TransportError is retried
// unless it wraps a deliveredError, which marks output already handed to the
// caller that a retry would duplicate.
func (c *Client) doWith(ctx context.Context, endpoint string, params url.Values, consume func(*http.Response) error) (*redactor, error) {
	r := &redactor{secrets: []string{c.apiKey, c.bearerToken}}
	u, err := c.buildURL(endpoint, params)
	if err != nil {
		return r, err
	}
	var refreshed string // token obtained after a 401, used for the rest of the call
	refreshTried := false
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		token, err := c.token(ctx, r, refreshed)
		if err != nil {
			return r, err
		}
		err = c.send(ctx, endpoint, u, token, r, consume)
		if err == nil {
			return r, nil
		}
		if ctx.Err() != nil {
			return r, ctx.Err()
		}
		var authErr *AuthError
		var tooLarge *ResponseTooLargeError
		var delivered *deliveredError
		if errors.As(err, &authErr) || errors.As(err, &tooLarge) || errors.As(err, &delivered) {
			return r, err
		}
		var apiErr *APIError
		var transportErr *TransportError
		if errors.As(err, &apiErr) {
			if apiErr.StatusCode == http.StatusUnauthorized && !refreshTried {
				if refresher, ok := c.tokenSource.(TokenRefresher); ok {
					refreshTried = true
					fresh, rerr := c.refreshToken(ctx, refresher, token)
					if fresh != "" {
						r.secrets = append(r.secrets, fresh)
					}
					if rerr != nil {
						return r, &AuthError{r.safe(rerr)}
					}
					if !validBearerToken(fresh) {
						return r, &AuthError{errors.New("token refresher returned an empty or whitespace-containing bearer token")}
					}
					if fresh != token {
						refreshed = fresh
						attempt-- // the repeat does not count against the retry budget
						continue
					}
					// The refresher has nothing newer: the API's 401 is the answer.
				}
			}
			if !apiErr.Retryable() {
				return r, err
			}
		} else if !errors.As(err, &transportErr) {
			return r, err
		}
		if attempt >= c.retry.MaxRetries {
			return r, err
		}
		delay := c.retryDelay(attempt)
		if apiErr != nil {
			if apiErr.RetryAfter > c.retry.MaxDelay {
				return r, err
			}
			delay = max(delay, apiErr.RetryAfter)
		}
		if err := wait(ctx, delay); err != nil {
			return r, err
		}
	}
}

// token returns the bearer token for an attempt: the refreshed one if a 401
// was answered by TokenRefresher, otherwise the source's current token. It is
// empty for API-key authentication.
func (c *Client) token(ctx context.Context, r *redactor, refreshed string) (string, error) {
	if refreshed != "" {
		return refreshed, nil
	}
	if c.tokenSource == nil {
		return c.bearerToken, nil
	}
	token, err := c.tokenSource.Token(ctx)
	if token != "" {
		r.secrets = append(r.secrets, token)
	}
	if err != nil {
		return "", &AuthError{r.safe(err)}
	}
	if !validBearerToken(token) {
		return "", &AuthError{errors.New("token source returned an empty or whitespace-containing bearer token")}
	}
	return token, nil
}

// deliveredError wraps a failure that happened after part of the response was
// already written to the caller's destination; such a call is never retried.
type deliveredError struct{ cause error }

func (e *deliveredError) Error() string { return e.cause.Error() + " (partial output was written)" }
func (e *deliveredError) Unwrap() error { return e.cause }

func (c *Client) send(ctx context.Context, endpoint string, u *url.URL, token string, r *redactor, consume func(*http.Response) error) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return &TransportError{endpoint, r.safe(err)}
	}
	req.Header.Set("User-Agent", c.userAgent)
	accept := "application/json"
	if strings.EqualFold(u.Query().Get("f"), "csv") {
		accept = "text/csv"
	}
	req.Header.Set("Accept", accept)
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
		detail := []rune(r.redact(apiErrorDetail(resp.StatusCode, resp.Header.Get("Content-Type"), body)))
		if len(detail) > 300 {
			detail = append(detail[:300], []rune("...")...)
		}
		return &APIError{resp.StatusCode, endpoint, string(detail), serverDelay(resp)}
	}
	if err := consume(resp); err != nil {
		var transportErr *TransportError
		if errors.As(err, &transportErr) {
			transportErr.Err = r.safe(transportErr.Err)
		}
		return err
	}
	return nil
}

// readBody reads a response body into memory, enforcing the optional size
// limit through copyLimited.
func readBody(body io.Reader, limit int64) (data []byte, tooLarge bool, err error) {
	var buf bytes.Buffer
	if _, tooLarge, err = copyLimited(&buf, body, limit); err != nil || tooLarge {
		return nil, tooLarge, err
	}
	return buf.Bytes(), false, nil
}

// copyLimited copies src to dst, enforcing the optional size limit: at most
// limit bytes reach dst, and a body of exactly limit bytes is accepted. If
// one more byte can be read, it is not written and tooLarge is reported.
// written counts the bytes dst received, including before an error.
func copyLimited(dst io.Writer, src io.Reader, limit int64) (written int64, tooLarge bool, err error) {
	if limit <= 0 {
		written, err = io.Copy(dst, src)
		return written, false, err
	}
	written, err = io.Copy(dst, io.LimitReader(src, limit))
	if err != nil || written < limit {
		return written, false, err
	}
	var probe [1]byte
	n, err := src.Read(probe[:])
	if n > 0 {
		return written, true, nil
	}
	if err != nil && err != io.EOF {
		return written, false, err
	}
	return written, false, nil
}

// A redactor belongs to one call, retaining the tokens that call used; the
// Client itself keeps only the latest token replacement, under refreshState.
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

var htmlTitlePattern = regexp.MustCompile(`(?is)<title>\s*(.*?)\s*</title>`)

// apiErrorDetail extracts a human-readable message from an error body: the
// JSON message or error field, the title of an HTML page (gateways answer 401
// and 5xx with HTML, which must not end up in error messages) or the text.
func apiErrorDetail(status int, contentType string, body []byte) string {
	text := strings.TrimSpace(string(body))
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if mediaType == "text/html" || strings.HasPrefix(text, "<") {
		if m := htmlTitlePattern.FindStringSubmatch(text); m != nil {
			title := strings.TrimSpace(strings.TrimPrefix(html.UnescapeString(m[1]), strconv.Itoa(status)))
			if title != "" {
				return title
			}
		}
		return http.StatusText(status)
	}
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
	return text
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

// epochThreshold separates a number of seconds to wait from a Unix timestamp.
// No server asks a client to wait a year; a larger value is a point in time.
const epochThreshold = 365 * 24 * 60 * 60

// parseRetryAfter reads a Retry-After or x-rate-limit-reset value: a number of
// seconds, a Unix timestamp or an HTTP date. The API sends seconds; the
// timestamp form guards against a format change silently disabling retries.
func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	if seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
		if seconds < 0 {
			return 0, false
		}
		if seconds > epochThreshold {
			return max(time.Unix(seconds, 0).Sub(now), 0), true
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
// The response is read completely, with retries, before anything is decoded,
// so dst is written to at most once, from a successful attempt. A failed
// request or invalid JSON leaves dst untouched; a type mismatch may leave it
// partially filled, as json.Unmarshal does.
func (c *Client) Get(ctx context.Context, endpoint string, params url.Values, dst any) error {
	if dst == nil || reflect.ValueOf(dst).Kind() != reflect.Pointer || reflect.ValueOf(dst).IsNil() {
		return &InputError{"destination", "must be a non-nil pointer"}
	}
	if strings.EqualFold(params.Get("f"), "csv") {
		return &InputError{"f", "Get decodes JSON; use GetMetricCSV for CSV"}
	}
	body, r, err := c.do(ctx, endpoint, params)
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
