# Design notes

This document explains the main decisions behind the client and what it
deliberately leaves out. For usage, see the [README](../README.md).

The client was extracted from the HTTP layer of
[`glassnode-cli`](https://github.com/glassnode/glassnode-cli) and aligned with the
features of the TypeScript client,
[`glassnode-api-ts-client`](https://github.com/glassnode/glassnode-api-ts-client).
Tracking issue: [GN-159](https://glassnode.atlassian.net/browse/GN-159).

## Goals

- **Standard library only.** The client is a single package with no third-party
  dependencies. It is easy to vendor and audit, and it never forces a version of
  another library on its users.
- **Explicit configuration.** Everything is passed to `NewClient`. The package
  never reads environment variables, config files or global state, so two
  clients in one process never affect each other.
- **Immutable, shareable client.** Options are validated once in `NewClient`.
  After that the `Client` has no mutable state, so it can be shared between
  goroutines without locking.
- **Typed where the API is stable, open where it is not.** Common response
  shapes have typed methods. Everything else goes through `GetMetric`, `Get`
  or `Raw`, so new metrics and endpoints work without a client release.

## Requests and parameters

`MetricParams` names the common query parameters (`a`, `e`, `s`, `u`, `i`, `c`)
and uses `time.Time` for the time range. Less common parameters go in `Extra`,
a `url.Values`, because the API adds metric-specific parameters regularly.
Setting the same parameter in both places is rejected rather than resolved
silently.

Lists are sent as repeated parameters (`a=BTC&a=ETH`), which is the form the
API expects for bulk requests. The client copies every map and slice it
receives, so callers can reuse their parameters.

Paths are validated against a strict pattern. A path cannot contain a scheme,
host, query string or `..`, so it can never point a request, and the
credentials attached to it, at a different server.

## Responses

- **Nullable values are pointers.** The API returns `null` for missing data. A
  `*float64` keeps that distinct from a real zero.
- **Timestamps stay as Unix seconds** (`int64`), as the API returns them. The
  client does not convert them to `time.Time`, so no time zone or precision
  choice is hidden in it.
- **Numbers in untyped JSON are `json.Number`.** Decoding into `any` would
  otherwise turn large integers into imprecise `float64` values.
- **Unknown fields are ignored**, so new fields in API responses do not break
  existing code. Required fields such as `t` and `v` are checked, and a missing
  one produces a `DecodeError` instead of a silent zero.
- **Bulk responses are unwrapped.** `GetBulkMetric` returns the contents of the
  `data` envelope directly. Each entry keeps all of its selectors in `Params`,
  because they depend on the metric (`e`, `network`, `category`, ...) and are
  the only way to tell entries apart. Bulk requests without `Since` are
  rejected before sending, since the API requires it.

## Authentication

API keys are sent in the `X-Api-Key` header by default. Query authentication
(`api_key=...`) is still supported, but it puts the key in URLs, which end up
in proxy and access logs.

OAuth access tokens can be supplied as a fixed token or through a
`TokenSource`. The token source is called before every HTTP attempt, so a
refreshed token is picked up on retries. The client does not perform the OAuth
login, store tokens or refresh them on a `401`: how a session is obtained and
kept is specific to each application. `glassnode-cli`, for example, keeps its
own login and refresh logic.

Exactly one authentication method must be configured. Accepting several and
picking one would hide configuration mistakes.

## Retries

Only GET requests exist, so every request is idempotent and safe to retry.
Retries happen after network errors, `429` and `5xx` responses, with two
retries by default.

The delay uses exponential backoff with full jitter: a random duration between
zero and an upper bound that starts at `BaseDelay` and doubles with each retry,
up to `MaxDelay`. Jitter keeps many clients
from retrying at the same moment after an outage.

`Retry-After` (seconds or an HTTP date) is treated as a minimum delay. The API
reports rate limits per minute through `x-rate-limit-reset`, so a `429` without
`Retry-After` uses that header instead; retrying a rate-limited request after
a second would only fail again. If the delay exceeds `MaxDelay`, the client
returns the error rather than retrying early or blocking for longer than the
caller configured. The delay is exposed as `APIError.RetryAfter`.

The per-attempt timeout comes from the HTTP client. The total duration of a
call is controlled by the context, which also interrupts the waits between
retries.

## Security

- **No redirects.** Redirects are disabled even on a user-supplied
  `http.Client`, because a redirect would resend the credentials to whatever
  host it points at. A `3xx` response is returned as an `APIError`.
- **Redacted errors.** Error messages and `APIError.Detail` have the API key and
  every token used in the call removed, including tokens from earlier retry
  attempts. The original errors are kept for `errors.Is` and `errors.As`, so
  callers should not log unwrapped causes.
- **Validated credentials.** Credentials with control characters or surrounding
  whitespace are rejected before any request, so they cannot inject headers.

## Out of scope

- OAuth login flows and token storage.
- [x402](https://www.x402.org) payments, which the TypeScript client supports.
- Response caching and client-side rate limiting. Applications that need them
  can add them in a custom `http.RoundTripper` or around the client.
- Reading configuration from the environment or files.

## Compared with the TypeScript client

| TypeScript client | Go client |
| --- | --- |
| Asset metadata, metric list | `ListAssets`, `ListMetrics` |
| Metric metadata, lag statistics | `GetMetricMetadata`, `GetMetricStats` |
| Generic metric with a Zod schema | `GetMetric` with a Go type and, optionally, `UnmarshalJSON` |
| Scalar and object series | `GetTimeSeries`, `GetObjectTimeSeries` |
| Bulk metrics | `GetBulkMetric` |
| Metadata tags, categories, blockchains | `ListMetricTags`, `ListAssetTags`, `ListAssetCategories`, `ListAssetBlockchains` |
| Custom `fetch`, observability hooks | `WithHTTPClient` with a custom `http.RoundTripper` |
| `AbortSignal`, per-call timeout | `context.Context` |
| Error classes | Error types for `errors.As` |
| Retries with jitter and `Retry-After` | `RetryPolicy` |
| OAuth token refresh | `WithTokenSource` |
| x402 payments | Not supported |

`GetAPIUsage`, `ListExchanges`, `ListNetworks`, `ListMiners` and the generic
`ListNames` have no TypeScript counterpart.

## Packaging

The module root is the library. Releases are Git tags such as `v0.1.0`.

`examples/` is a separate Go module with its own `go.mod`. Go leaves nested
modules out of the module archive, so users who install the client do not
download the examples. The examples module uses a relative `replace` directive
to build against the local checkout. CI vets and builds it separately.
