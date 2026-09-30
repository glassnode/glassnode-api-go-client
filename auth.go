package glassnode

import (
	"context"
	"strings"
	"unicode"
)

// TokenSource supplies an OAuth access token for each HTTP attempt. Implementations
// own refresh and storage, must honor ctx and must be safe for concurrent calls.
// The SDK does not automatically refresh on 401 or retry token-source errors.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

// TokenSourceFunc adapts a function to TokenSource.
type TokenSourceFunc func(context.Context) (string, error)

// Token calls f with the request context.
func (f TokenSourceFunc) Token(ctx context.Context) (string, error) { return f(ctx) }

func validBearerToken(token string) bool {
	return token != "" && strings.IndexFunc(token, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) == -1
}
