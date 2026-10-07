# Changelog

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
