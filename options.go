package glassnode

import (
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultBaseURL is the standard Glassnode API endpoint.
const DefaultBaseURL = "https://api.glassnode.com"

// RetryPolicy controls GET retries. MaxRetries excludes the initial attempt.
// Retry-After is a minimum delay; a value above MaxDelay stops retries rather
// than retrying before the server permits it. Zero MaxRetries disables retries.
type RetryPolicy struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
}

// Option configures a Client at construction time.
type Option func(*Client) error

// WithBaseURL selects an API origin, optionally with a path prefix. Credentials,
// query strings and fragments are forbidden. HTTP is allowed for local testing.
func WithBaseURL(raw string) Option {
	return func(c *Client) error {
		u, err := url.Parse(raw)
		if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
			return &InputError{"base URL", "expected an HTTP(S) URL without credentials, query or fragment"}
		}
		c.baseURL = strings.TrimRight(u.String(), "/")
		return nil
	}
}

// WithHTTPClient uses a copy of the supplied client's configuration. Its
// transport and jar remain shared. Redirects are always disabled to prevent
// credential forwarding. Configure the transport before sharing the client.
func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) error {
		if client == nil {
			return &InputError{"HTTP client", "must not be nil"}
		}
		copy := *client
		c.httpClient = &copy
		return nil
	}
}

// WithTimeout sets the per-attempt HTTP timeout. Zero disables it. Use a context
// deadline to bound the entire operation, including all retries and waits.
func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) error {
		if timeout < 0 {
			return &InputError{"timeout", "must not be negative"}
		}
		c.httpClient.Timeout = timeout
		return nil
	}
}

// WithAPIKeyInQuery sends api_key in the URL instead of X-Api-Key. Prefer the
// default header authentication to keep credentials out of proxy/access logs.
func WithAPIKeyInQuery() Option { return func(c *Client) error { c.queryAuth = true; return nil } }

// WithBearerToken uses an existing OAuth access token instead of the API key.
// Token acquisition, persistence and refresh belong to the application. A
// refreshing http.RoundTripper can be supplied through WithHTTPClient.
func WithBearerToken(token string) Option {
	return func(c *Client) error {
		if strings.TrimSpace(token) == "" || strings.ContainsAny(token, "\r\n") {
			return &InputError{"bearer token", "must be non-empty and contain no line breaks"}
		}
		c.bearerToken = token
		return nil
	}
}

// WithRetryPolicy replaces the default two retries with full-jitter backoff
// from one second to thirty seconds. Delays must be positive when enabled.
func WithRetryPolicy(policy RetryPolicy) Option {
	return func(c *Client) error {
		if policy.MaxRetries < 0 || policy.MaxRetries > 20 || policy.MaxRetries > 0 && (policy.BaseDelay <= 0 || policy.MaxDelay < policy.BaseDelay) {
			return &InputError{"retry policy", "retries must be 0..20, with positive base delay and max delay >= base delay"}
		}
		c.retry = policy
		return nil
	}
}

// WithUserAgent identifies the calling application. The SDK's default is
// glassnode-api-go-client. An empty or multiline value is rejected.
func WithUserAgent(agent string) Option {
	return func(c *Client) error {
		if strings.TrimSpace(agent) == "" || strings.ContainsAny(agent, "\r\n") {
			return &InputError{"user agent", "must be non-empty and contain no line breaks"}
		}
		c.userAgent = agent
		return nil
	}
}
