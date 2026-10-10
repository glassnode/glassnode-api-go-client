# Changelog

## Unreleased

- `APIAddon.Period` (`daily` or `monthly`), `APIAddon.RPM` and
  `APIUsage.DailyRequestsUsed` from the usage endpoint.
- `APIUsage.Allowance` returns the limit, its period and the usage counted
  against it: requests today on a daily allowance, credits this month on a
  monthly one. It reports false for no, unknown or mixed periods instead of
  guessing.
- `CreditsPerMonth` is deprecated: an allowance may be daily.

## v0.1.0 — 2026-10-08

Initial public release.

- Metric time series: scalar (`GetTimeSeries`), object (`GetObjectTimeSeries`),
  bulk (`GetBulkMetric`) and custom shapes (`GetMetric`). Point-in-time metrics
  expose `ComputedAt`; bulk entries keep every selector in `Params`.
- Asset and metric metadata, metric lag statistics, and lists of metric tags,
  asset tags, categories, blockchains, exchanges, networks and miners.
- API credit usage (`GetAPIUsage`).
- CSV downloads of a metric to an `io.Writer` (`GetMetricCSV`).
- Generic `Get` and `Raw` for endpoints without a dedicated method.
- Authentication with an API key (header or query) or an existing OAuth access
  token, fixed or from a `TokenSource`; a `TokenRefresher` can replace a token
  the API rejected with `401`.
- Context cancellation, a per-attempt timeout and retries with jittered
  backoff that honour `Retry-After` and `x-rate-limit-reset`, with the wait
  exposed as `APIError.RetryAfter`.
- Optional response size limit (`WithMaxResponseBytes`).
- Typed errors with credentials redacted from messages; redirects are never
  followed.
- No dependencies outside the standard library.
