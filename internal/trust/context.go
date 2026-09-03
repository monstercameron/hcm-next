package trust

import (
	"context"
	"errors"
)

// ErrNoPrincipal is returned by [MustFromContext] when the context carries no
// authenticated principal. Reaching it means an interceptor was skipped, which
// is a programming error rather than a caller error.
var ErrNoPrincipal = errors.New("trust: context carries no authenticated principal")

// principalContextKey is the unexported context key type for the principal.
// Being unexported means no other package can inject a principal by writing
// to the context directly; [WithPrincipal] is the only door.
type principalContextKey struct{}

// WithPrincipal returns a child context carrying p as the authenticated
// principal. Only an authentication interceptor calls this, and only with a
// principal a [Verifier] produced.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	if p == nil {
		return ctx
	}
	return context.WithValue(ctx, principalContextKey{}, p)
}

// FromContext returns the authenticated principal carried by ctx.
func FromContext(ctx context.Context) (*Principal, bool) {
	p, ok := ctx.Value(principalContextKey{}).(*Principal)
	return p, ok && p != nil
}

// MustFromContext returns the authenticated principal carried by ctx, or
// [ErrNoPrincipal] when there is none.
func MustFromContext(ctx context.Context) (*Principal, error) {
	p, ok := FromContext(ctx)
	if !ok {
		return nil, ErrNoPrincipal
	}
	return p, nil
}
