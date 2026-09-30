package glassnode

import "fmt"

// APIError describes a non-2xx response. Detail is bounded and credential-redacted.
type APIError struct {
	StatusCode int
	Endpoint   string
	Detail     string
}

func (e *APIError) Error() string {
	message := fmt.Sprintf("glassnode: %s: HTTP %d", e.Endpoint, e.StatusCode)
	if e.Detail != "" {
		message += ": " + e.Detail
	}
	return message
}

// Retryable reports whether this status is eligible for a GET retry.
func (e *APIError) Retryable() bool {
	return e.StatusCode == 429 || e.StatusCode >= 500 && e.StatusCode <= 599
}

// InputError describes invalid configuration or arguments, before any request.
type InputError struct{ Field, Message string }

func (e *InputError) Error() string { return "glassnode: " + e.Field + ": " + e.Message }

// DecodeError describes a response that does not match the destination Go type.
// It is never retried. SDK-generated messages redact credentials, including
// custom decoder errors. Unwrap exposes the original error for inspection.
type DecodeError struct {
	Endpoint string
	Err      error
}

func (e *DecodeError) Error() string {
	return "glassnode: decoding " + e.Endpoint + " response: " + e.Err.Error()
}
func (e *DecodeError) Unwrap() error { return e.Err }

// TransportError describes a failure in transit. SDK-generated error messages
// include the credential-redacted cause. Unwrap retains the original cause.
// The cause may contain credentials and should not be logged indiscriminately.
type TransportError struct {
	Endpoint string
	Err      error
}

func (e *TransportError) Error() string {
	return "glassnode: request failed for " + e.Endpoint + ": " + e.Err.Error()
}
func (e *TransportError) Unwrap() error { return e.Err }

// AuthError describes a token-source failure or an invalid token. It is never
// retried. Messages redact credentials used in the call; the token source must
// avoid returning errors containing other credentials unknown to the SDK.
// Unwrapped causes may contain secrets and should not be logged indiscriminately.
type AuthError struct{ Err error }

func (e *AuthError) Error() string { return "glassnode: acquiring bearer token: " + e.Err.Error() }
func (e *AuthError) Unwrap() error { return e.Err }
