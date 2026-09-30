# Changelog

## v0.1.0 — 2026-10-01

Initial private SDK release. One dependency-free root `glassnode` package, with
typed metrics, metadata and usage endpoints, context cancellation, configurable
HTTP transport and bounded retries. Runnable examples are a separate module and
excluded from the SDK distribution.

Changes following the Fable review:

- Preserve metric `parameters_defaults` from recorded API responses.
- Include redacted diagnostic causes in decode and transport errors, preserving
  the original error chain for `errors.Is` and `errors.As`.
- Add `TokenSource`, `TokenSourceFunc`, `WithTokenSource` and `AuthError` for
  application-owned OAuth refresh. Tokens are obtained per HTTP attempt and
  redacted per call, including tokens used on earlier retries.
- Reject conflicting authentication, invalid credential whitespace/control
  characters and OAuth combined with query API-key authentication. Previously,
  bearer tokens silently took precedence over a configured API key.
- Make explicit `WithTimeout` override a supplied HTTP client's timeout in
  either option order.
- Extract JSON API error messages and bound detail after redaction.
- Add recorded API contract coverage and tests for token-source concurrency,
  cancellation, failures and redaction, interrupted reads, per-attempt timeouts,
  HTTP-date Retry-After and malformed metadata/usage responses.

OAuth login and token storage remain application-owned. x402 is deferred.
