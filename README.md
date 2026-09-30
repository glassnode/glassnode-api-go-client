# Glassnode API client for Go

An idiomatic, dependency-free Go client for the Glassnode API. Extracted from
[`glassnode-cli`](https://github.com/glassnode/glassnode-cli), with typed metric
helpers, metadata and usage endpoints, cancellation and bounded retries.

Go 1.24 or later. This repository is **private**; your GitHub account needs read
access. The API key is separate from your GitHub authentication.

## Install

Authenticate Git with GitHub (for example, `gh auth setup-git`) and configure
private-module resolution. Include this path in your existing `GOPRIVATE` value:

```sh
go env -w GOPRIVATE=github.com/glassnode/glassnode-api-go-client
go get github.com/glassnode/glassnode-api-go-client
```

Do not replace an existing `GOPRIVATE` list without preserving its entries.
The private setting keeps the module out of the public proxy and checksum
database. In applications, commit `go.mod` and `go.sum` to pin your SDK version.

## Fetch a metric

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
    if err != nil { log.Fatal(err) }

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    points, err := client.GetTimeSeries(ctx, "market/price_usd_close", &glassnode.MetricParams{
        Asset: "BTC",
        Since: time.Now().AddDate(0, 0, -30),
        Interval: "24h",
    })
    if err != nil { log.Fatal(err) }
    for _, point := range points {
        if point.Value != nil {
            fmt.Printf("%s: %.2f\n", time.Unix(point.Timestamp, 0).UTC(), *point.Value)
        }
    }
}
```

Create a client once and reuse it across goroutines. The SDK reads no environment
variables or files; the example chooses to read the API key from the environment.
Timestamps are Unix **seconds**. Scalar/bulk gaps are `nil`, distinct from zero.

## Choose the right response shape

| Operation | Method |
| --- | --- |
| Scalar `{t,v}` metrics | `GetTimeSeries(ctx, path, params)` |
| OHLC/breakdown `{t,o}` metrics | `GetObjectTimeSeries(ctx, path, params)` |
| Bulk metrics | `GetBulkMetric(ctx, basePath, params)` |
| Custom/nested metric shapes | `GetMetric(ctx, path, params, &destination)` |
| Asset metadata, with optional CEL filter | `ListAssets(ctx, filter)` |
| Metric paths, with optional selectors | `ListMetrics(ctx, params)` |
| Metric metadata and variants | `GetMetricMetadata(ctx, path, params)` |
| Lag percentiles | `GetMetricStats(ctx, path, params)` |
| Named metadata lists | `ListMetricTags`, `ListAssetTags`, `ListAssetCategories`, `ListAssetBlockchains` |
| Current API credits | `GetAPIUsage(ctx)` |
| New GET endpoints | `Get(ctx, endpoint, query, &destination)` or `Raw(ctx, endpoint, query)` |

`params` may be `nil`. Metric paths accept either `market/price_usd_close` or
`/market/price_usd_close`; full URLs and query strings are rejected. For bulk,
pass the base path without `/bulk`; the method appends it and unwraps `data`.

```go
bulk, err := client.GetBulkMetric(ctx, "market/marketcap_usd", &glassnode.MetricParams{
    Assets: []string{"BTC", "ETH"},
    Interval: "24h",
})
```

Use `Extra url.Values` for metric-specific selectors. Values are encoded as
repeated parameters, never comma-joined. Do not set the same parameter through
both a typed field and `Extra`. The SDK copies query maps and slices.

```go
params := &glassnode.MetricParams{
    Asset: "BTC",
    Exchanges: []string{"binance"},
    Extra: url.Values{"quote_symbol": {"USDT"}, "bps": {"100"}},
}
```

`GetMetric` supports caller-owned structs, custom `UnmarshalJSON` validation and
`json.RawMessage`. JSON decoded into `any` uses `json.Number` to preserve large
integers; the numeric convenience methods use `float64` for arithmetic. Nullable
values in custom structs should use pointers.

## Configure HTTP and authentication

```go
client, err := glassnode.NewClient(apiKey,
    glassnode.WithTimeout(15*time.Second),
    glassnode.WithRetryPolicy(glassnode.RetryPolicy{
        MaxRetries: 2,
        BaseDelay: time.Second,
        MaxDelay: 30*time.Second,
    }),
)
```

The default sends the key in `X-Api-Key`, avoiding credentials in request URLs.
`WithAPIKeyInQuery()` enables legacy `api_key` query authentication.
`NewClient("", WithBearerToken(token))` uses an existing OAuth access token.
Login, token storage and refresh belong to your application; supply a refreshing
transport through `WithHTTPClient` if needed.

`WithHTTPClient` copies the client's configuration and preserves its transport,
jar and timeout. Supply your own `http.RoundTripper` for tracing, metrics or
authentication. **Redirects are never followed**, even with a custom client,
to avoid forwarding credentials. Point `WithBaseURL` at the final origin.

Two retries are enabled by default for GET transport/read failures, 429 and
5xx responses. Backoff uses full jitter from one second to a 30-second cap.
`Retry-After` is a minimum wait; if it exceeds the cap, the call returns the
HTTP error instead of retrying early. `WithRetryPolicy(RetryPolicy{})` disables
retries. Decode errors, other HTTP statuses and caller cancellation never retry.

The default HTTP timeout is one minute per attempt. A context deadline bounds
the whole operation, including retries and waits; use one when latency matters.
`WithTimeout(0)` disables the per-attempt timeout. A supplied HTTP client's
timeout replaces the default unless a later `WithTimeout` option overrides it.
The client does not cache data or impose a global rate limiter.

## Handle errors

```go
var apiErr *glassnode.APIError
switch {
case errors.Is(err, context.Canceled):
    // The caller stopped the operation.
case errors.Is(err, context.DeadlineExceeded):
    // A context deadline or the HTTP client's timeout expired.
case errors.As(err, &apiErr):
    fmt.Printf("status=%d endpoint=%s\n", apiErr.StatusCode, apiErr.Endpoint)
}
```

`InputError`, `DecodeError` and `TransportError` distinguish other failures.
Error text and `APIError.Detail` redact configured credentials. Transport errors
retain the original cause for inspection; do not log unwrapped causes or raw
requests when they may include secrets. A custom refreshing transport must also
protect any newly acquired token that the SDK does not know.

## Develop

```sh
go test -race ./...
go vet ./...
go run ./examples/price
```

Tests use local HTTP servers and need no API credentials. CI checks the minimum
Go version and the latest stable Go version. API design, TS feature mapping and
CLI boundaries are in [docs/design.md](docs/design.md). Tracking issue:
[GN-159](https://glassnode.atlassian.net/browse/GN-159).

The module is pre-1.0 and uses Go module versions. No public release or paid
x402 signing is included. License: Apache 2.0; see [NOTICE](NOTICE) for provenance.
