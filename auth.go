package glassnode

import (
	"context"
	"strings"
	"unicode"
)

// TokenSource supplies an OAuth access token for each HTTP attempt. Implementations
// own refresh and storage, must honor ctx and must be safe for concurrent calls.
// Token-source errors are never retried. A 401 response is returned as is
// unless the source also implements TokenRefresher.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

// TokenRefresher is a TokenSource that can replace a token the API rejected.
// When a request gets a 401, the client calls Refresh once and repeats the
// request with the new token if it differs from the rejected one; a second
// 401 is returned to the caller. Returning the same token means nothing newer
// exists, and the 401 is returned as is. Refresh errors and an empty or
// malformed token are reported as AuthError. Concurrent calls rejected with the
// same token share one Refresh.
type TokenRefresher interface {
	TokenSource
	Refresh(ctx context.Context) (string, error)
}

// TokenSourceFunc adapts a function to TokenSource.
type TokenSourceFunc func(context.Context) (string, error)

// Token calls f with the request context.
func (f TokenSourceFunc) Token(ctx context.Context) (string, error) { return f(ctx) }

func validBearerToken(token string) bool {
	return token != "" && strings.IndexFunc(token, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) == -1
}
