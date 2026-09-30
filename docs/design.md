# Go API client design — GN-159

Status: implementation specification. Tracking: https://glassnode.atlassian.net/browse/GN-159

Extract the reusable HTTP client and endpoint models from
`glassnode/glassnode-cli/internal/api` into a private, independently versioned
`github.com/glassnode/glassnode-api-go-client` module. The root package is
`glassnode`; it uses only the Go standard library and supports Go 1.24+.

## Public API and developer experience

- `NewClient(apiKey, ...Option) (*Client, error)` validates configuration once.
  No implicit environment reads, config files, or global state. Authentication
  options are mutually exclusive. Explicit timeouts do not depend on option order.
- All calls take `context.Context`. Context deadlines bound the entire call,
  including retries. The default HTTP timeout is one minute per attempt.
- `MetricParams` gives common parameters readable Go field names and `time.Time`
  ranges. `Extra url.Values` accepts new metric-specific selectors and repeated
  values. Caller-owned maps/slices are copied, never mutated.
- `GetTimeSeries` and `GetObjectTimeSeries` return timestamped, nullable numeric
  values. `GetMetric(..., dst)` decodes custom metric shapes into caller-owned
  structs or `json.RawMessage`. Unix timestamps stay in seconds. Dynamic JSON
  decoding uses `json.Number` to preserve precision.
- Bulk calls return a slice of timestamped entries, unwrapping the API's `data`
  envelope. Metadata and usage get concrete Go response types. Unknown JSON
  fields are tolerated; raw decoding is available when callers need them.
- `Raw(ctx, endpoint, url.Values)` and `Get(ctx, endpoint, url.Values, dst)` are
  escape hatches for new GET endpoints. Paths cannot replace the configured host
  or add query strings. Query credentials are controlled by the client.
- Errors support `errors.As` for HTTP/input/decode/transport failures and
  `errors.Is` for context cancellation/deadlines. Error text and exported
  response detail exclude configured credentials. Transport causes are retained
  for inspection and may contain credentials: callers must not log unwrapped
  transport causes.
- Clients can be reused concurrently. Configure once; do not mutate a supplied
  HTTP transport concurrently. `WithHTTPClient` copies the client configuration
  and preserves its transport, timeout, cookie jar and instrumentation.

## Authentication and resilience

API keys default to the `X-Api-Key` header for server-side Go applications;
`WithAPIKeyInQuery` supports legacy query authentication. `WithBearerToken`
supports an existing OAuth token. `WithTokenSource` calls an application-owned,
concurrent-safe token source
before each attempt, allowing refresh without a placeholder token. Each call
retains its own tokens for error redaction without mutable Client state. Login,
persistence and expiry-aware refresh remain in the token-source implementation.
Token-source failures are `AuthError`s and never retried, even when their cause
is a transport error. HTTP 401 does not trigger refresh or retry.

Never follow redirects, in either auth mode. A 3xx is an HTTP error: this avoids
forwarding credentials to a different host. User HTTP-client redirect behavior
is deliberately overridden. OAuth token sources are supported; x402 payments
are deferred.

Default retries: two retries (three attempts) for GET transport/read failures,
429 and 5xx responses. Exponential backoff with full jitter, one-second base,
30-second cap. Honor `Retry-After` seconds or HTTP dates as a minimum wait; if it
exceeds the configured cap, return the HTTP error rather than retry early.
Caller cancellation never retries and also interrupts retry waits. Applications should supply usable OAuth tokens before the request; 401 responses
are returned immediately.

Input validation rejects malformed paths, empty repeated values, overridden
credentials, non-JSON formats and invalid time ranges. Decode failures never
retry. The SDK does not cache results or add a global rate limiter.

## TypeScript capability map

| TS feature | Go implementation |
| --- | --- |
| Asset metadata and metric list | `ListAssets`, `ListMetrics` |
| Metric metadata and lag stats | `GetMetricMetadata`, `GetMetricStats` |
| Generic metric + schema | `GetMetric` with a caller-owned Go destination; optional custom `UnmarshalJSON` |
| Scalar/object series | `GetTimeSeries`, `GetObjectTimeSeries`, nullable numbers |
| Bulk metric | `GetBulkMetric`, repeated query values and typed entries |
| Metadata tags/categories/blockchains | Named list methods and `ListNames` (from CLI) |
| API usage | `GetAPIUsage` (from CLI) |
| Dates and repeated parameters | `time.Time`, slices and `url.Values` |
| Fetch injection and observability hooks | `WithHTTPClient`, custom `http.RoundTripper` |
| AbortSignal and per-call timeout | `context.Context` deadline/cancellation |
| Error classes | Concrete errors, `errors.As` / `errors.Is` |
| Retries, jitter, Retry-After | Configurable `RetryPolicy` |
| Header/query credentials | Header default, explicit query option |
| Browser bundles, Zod runtime DSL | Not relevant to Go |
| OAuth refresh | `WithTokenSource`, application-owned refresh and storage |
| x402 signed payments | Deferred |

## CLI migration

Keep the existing `internal/api` entry point as a small CLI adapter, retaining
command signatures and output types. Remove its HTTP/decode implementation and
delegate to the pinned SDK module. Keep environment/config resolution, OAuth
refresh transport, dry-run URL rendering, asset pruning and usage presentation
inside the CLI. No committed filesystem `replace` directive; use a temporary
workspace for local cross-module testing.

Because the SDK is private, CLI builds require GitHub read access and
`GOPRIVATE=github.com/glassnode/glassnode-api-go-client`. Document this explicitly.
Public CLI CI cannot fetch a private module without an authorized read token;
configure its workflow to use a repository secret, without exposing it in logs
or to fork PRs. Publication of the Go SDK remains a separate decision.

## Verification

Use `httptest` for wire contracts and fixtures inherited from the CLI. Cover
nullable and custom metric data, precision, typed HTTP errors, redaction,
malformed responses, endpoint/query validation, redirects, retry counts and
Retry-After, cancellation in flight and during backoff, timeout behavior and
concurrent use. Run `go test -race ./...` and `go vet ./...` in both modules.
Examples must compile. Verify private visibility and pushed branches remotely.

## Module packaging

The library is the root module with one public package, `glassnode`. It has no
third-party runtime dependencies. Root semantic-version tags version the SDK;
commits resolve to pseudo-versions before a release is tagged.

`examples/go.mod` defines a separate, unpublished module. Go module archives
exclude nested modules, keeping executable examples and their setup out of the
client distributable. A relative replacement targets the local SDK checkout;
CI explicitly vets and builds examples in addition to testing the root module.
