# Changelog

## Unreleased

Behaviour changes:

- `GetBulkMetric` requires `Since`, as the API does, and returns an
  `InputError` before sending a request without it.
- `WithUserAgent` rejects any control character, not only CR and LF.
- The default retry cap is 65 seconds instead of 30, so a `429` at the start
  of a rate-limit window is retried after the window resets.
- Responses are decoded as they stream in instead of being buffered first.

Added:

- `TimeSeriesPoint.ComputedAt` and `ObjectTimeSeriesPoint.ComputedAt` for
  point-in-time metrics.
- `BulkEntry.Params` with every selector of a bulk entry (`e`, `network`,
  `category`, ...), which were dropped before.
- `APIError.RetryAfter`, the wait requested through `Retry-After` or
  `x-rate-limit-reset`; a `429` without `Retry-After` now honours the latter.
- `ListExchanges`, `ListNetworks` and `ListMiners`.
- `WithMaxResponseBytes` and `ErrResponseTooLarge` for callers that want to
  bound response size; there is no limit by default.

Fixed:

- `GetMetricMetadata` accepts `parameters_defaults` values given as strings,
  the form the API documentation describes, as well as lists.
- `GetAPIUsage` no longer fails for accounts without API add-ons.
- Error response bodies are drained so the connection is reused on retry.

## v0.1.0 — 2026-10-01

Initial release.

- Metric time series: scalar (`GetTimeSeries`), object (`GetObjectTimeSeries`),
  bulk (`GetBulkMetric`) and custom shapes (`GetMetric`).
- Asset and metric metadata, metric lag statistics, metadata tag and category
  lists, and API credit usage.
- Generic `Get` and `Raw` for endpoints without a dedicated method.
- Authentication with an API key (header or query) or an OAuth access token,
  either fixed or from a `TokenSource`.
- Context cancellation, per-attempt timeouts and configurable retries with
  backoff and `Retry-After` support.
- Typed errors with credentials redacted from messages.
