# Glassnode Go client

[![CI](https://github.com/glassnode/glassnode-api-go-client/actions/workflows/ci.yml/badge.svg)](https://github.com/glassnode/glassnode-api-go-client/actions/workflows/ci.yml)
[![Go version](https://img.shields.io/badge/go-%E2%89%A51.24-00ADD8.svg?logo=go)](go.mod)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

A Go client for the [Glassnode API](https://docs.glassnode.com). It covers
metric time series, bulk metrics, asset and metric metadata, and API usage.
It has no dependencies outside the standard library.

## Installation

The repository is private, so Go needs to fetch it directly from GitHub instead
of the public module proxy. Make sure Git can authenticate to GitHub (for
example with `gh auth setup-git`), then add the module to `GOPRIVATE` and
install it:

```sh
go env -w GOPRIVATE="$(go env GOPRIVATE),github.com/glassnode/glassnode-api-go-client"
go get github.com/glassnode/glassnode-api-go-client
```

The client requires Go 1.24 or later.

## Quick start

You need a Glassnode API key; see [API Setup](https://docs.glassnode.com/basic-api/api)
for how to create one.

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	glassnode "github.com/glassnode/glassnode-api-go-client"
)

func main() {
	client, err := glassnode.NewClient(os.Getenv("GLASSNODE_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	points, err := client.GetTimeSeries(ctx, "market/price_usd_close", &glassnode.MetricParams{
		Asset:    "BTC",
		Since:    time.Now().AddDate(0, 0, -30),
		Interval: "24h",
	})
	if err != nil {
		log.Fatal(err)
	}

	for _, p := range points {
		if p.Value == nil {
			continue // no data for this timestamp
		}
		fmt.Printf("%s  %.2f\n", time.Unix(p.Timestamp, 0).UTC().Format(time.DateOnly), *p.Value)
	}
}
```

A `Client` is safe for concurrent use; create one and share it. The client
never reads environment variables or files on its own: the example above reads
the key itself and passes it in.

## Fetching data

Pick the method that matches the shape of the metric's response:

| Data | Method |
| --- | --- |
| Scalar metric, `{"t", "v"}` | `GetTimeSeries` |
| Object metric (OHLC, breakdowns), `{"t", "o"}` | `GetObjectTimeSeries` |
| Several assets in one request | `GetBulkMetric` |
| Any other shape | `GetMetric` with your own destination type |
| Metric metadata, variants and availability | `GetMetricMetadata` |
| Data lag percentiles | `GetMetricStats` |
| Available metric paths | `ListMetrics` |
| Asset metadata | `ListAssets` |
| Metric tags, asset tags, categories, blockchains | `ListMetricTags`, `ListAssetTags`, `ListAssetCategories`, `ListAssetBlockchains` |
| Account credit usage | `GetAPIUsage` |
| Endpoints without a dedicated method | `Get` (decodes JSON) or `Raw` (returns bytes) |

Things to keep in mind:

- Metric paths are written as in the API docs, with or without a leading slash:
  `market/price_usd_close`.
- Timestamps are Unix **seconds**.
- Missing values are `nil`, so a gap is never confused with a zero.
- `params` may be `nil`. The asset list methods take an optional
  [CEL](https://cel.dev) filter expression; pass `""` for no filter.

### Bulk metrics

Pass the base metric path; the client adds the `/bulk` suffix and unwraps the
response envelope.

```go
points, err := client.GetBulkMetric(ctx, "market/marketcap_usd", &glassnode.MetricParams{
	Assets:   []string{"BTC", "ETH"},
	Interval: "24h",
})
```

### Additional parameters

`MetricParams` has typed fields for the common parameters (asset, exchanges,
time range, interval, currency). Use `Extra` for anything else that a metric
accepts. Values with several entries are sent as repeated query parameters.

```go
params := &glassnode.MetricParams{
	Asset:     "BTC",
	Exchanges: []string{"binance"},
	Extra:     url.Values{"quote_symbol": {"USDT"}},
}
```

Set each parameter in one place only: setting it both in a typed field and in
`Extra` is an error.

### Custom response types

`GetMetric` decodes into any pointer you pass, so you can define a struct for an
unusual metric, add your own `UnmarshalJSON` validation, or keep the raw
response with `json.RawMessage`. Use pointer fields for values that can be
`null`. When decoding into `any`, numbers become `json.Number`, so large
integers keep their precision.

## Configuration

`NewClient` works without options. The defaults are:

| Setting | Default | Option |
| --- | --- | --- |
| Authentication | API key in the `X-Api-Key` header | `WithAPIKeyInQuery`, `WithBearerToken`, `WithTokenSource` |
| Timeout | 1 minute per HTTP attempt | `WithTimeout` |
| Retries | 2 retries for GET requests | `WithRetryPolicy` |
| HTTP client | Fresh `http.Client` | `WithHTTPClient` |
| Base URL | `https://api.glassnode.com` | `WithBaseURL` |
| User agent | `glassnode-api-go-client` | `WithUserAgent` |

```go
client, err := glassnode.NewClient(apiKey,
	glassnode.WithTimeout(15*time.Second),
	glassnode.WithRetryPolicy(glassnode.RetryPolicy{
		MaxRetries: 3,
		BaseDelay:  500 * time.Millisecond,
		MaxDelay:   10 * time.Second,
	}),
	glassnode.WithUserAgent("my-service/1.2"),
)
```

### Authentication

Most applications should use an API key. The client sends it in the
`X-Api-Key` header, which keeps it out of URLs and access logs.
`WithAPIKeyInQuery` sends it as the `api_key` query parameter instead, for
setups that require it.

If your application already has a Glassnode OAuth access token, pass an empty
API key and use one of the bearer-token options instead. The client does not
implement the OAuth login flow; it only sends the token you give it.

- `WithBearerToken(token)` sends a fixed token. Use it for short-lived scripts.
- `WithTokenSource(source)` calls `source` before every HTTP attempt, including
  retries, so it can return a refreshed token. It must be safe for concurrent
  use and should respect context cancellation.

For example, to reuse a token source from
[`golang.org/x/oauth2`](https://pkg.go.dev/golang.org/x/oauth2):

```go
source := glassnode.TokenSourceFunc(func(ctx context.Context) (string, error) {
	token, err := oauthTokens.Token() // an oauth2.TokenSource
	if err != nil {
		return "", err
	}
	return token.AccessToken, nil
})
client, err := glassnode.NewClient("", glassnode.WithTokenSource(source))
```

Configure exactly one way to authenticate; `NewClient` returns an error
otherwise. A failing token source produces an `AuthError` and is not retried.
A `401` response is returned as is: the client does not refresh the token and
try again.

### Timeouts and retries

The timeout applies to each HTTP attempt separately. To limit the total time of
a call, including retries and the waits between them, use a context deadline.
`WithTimeout(0)` removes the per-attempt timeout.

GET requests are retried after network errors and `429` or `5xx` responses.
Other errors, including `4xx` responses and responses that fail to decode, are
returned immediately. The delay before retry *n* is a random duration between
zero and `BaseDelay × 2ⁿ⁻¹`, capped at `MaxDelay` (with the defaults: up to
1s, then up to 2s). A `Retry-After` header raises the delay to at least the
value the server asks for; if that is longer than `MaxDelay`, the client returns
the error instead of waiting. `WithRetryPolicy(glassnode.RetryPolicy{})`
disables retries.

The client has no cache and no rate limiter.

### Custom HTTP client

`WithHTTPClient` lets you supply your own transport, for example for tracing,
metrics or a proxy. The client copies the `http.Client` and keeps its
transport, cookie jar and timeout. An explicit `WithTimeout` takes precedence
over the supplied client's timeout.

Redirects are never followed, even with a custom client, so credentials are
never sent to another host. A `3xx` response is returned as an `APIError`.

## Error handling

All errors can be inspected with `errors.As` and `errors.Is`:

```go
points, err := client.GetTimeSeries(ctx, path, params)

var apiErr *glassnode.APIError
switch {
case errors.Is(err, context.DeadlineExceeded):
	// The context deadline or the HTTP timeout expired.
case errors.As(err, &apiErr):
	log.Printf("API returned %d for %s: %s", apiErr.StatusCode, apiErr.Endpoint, apiErr.Detail)
case err != nil:
	log.Print(err)
}
```

| Error | Meaning |
| --- | --- |
| `*APIError` | The API responded with a non-2xx status. `Detail` holds the server's message, shortened to 300 characters. |
| `*InputError` | Invalid configuration or arguments; no request was sent. |
| `*TransportError` | The request failed on the network or while reading the response. |
| `*DecodeError` | The response did not match the expected type. |
| `*AuthError` | The token source failed or returned an invalid token. |

Context cancellation and deadlines are returned as `context.Canceled` and
`context.DeadlineExceeded`.

Error messages never contain the API key or tokens used for the call. The
original underlying error is still available through `errors.Unwrap`, and it is
not redacted, so avoid logging it on its own.

## Examples

The [examples](examples) directory contains runnable programs for the main use
cases. Clone the repository and run them with your API key:

```sh
cd examples
export GLASSNODE_API_KEY=your-api-key
go run ./price
```

See [examples/README.md](examples/README.md) for the full list.

## Development

```sh
go test -race ./...
go vet ./...
(cd examples && go vet ./... && go build ./...)
```

Tests run against local HTTP servers and recorded API responses, so they need
no API key. Design decisions are described in [docs/design.md](docs/design.md).

## License

Apache License 2.0; see [LICENSE](LICENSE) and [NOTICE](NOTICE).
