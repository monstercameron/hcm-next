package cache

// Fake is the in-memory conformance implementation of Store. It intentionally
// has the same bounded, expiring and authorization-aware behavior expected of
// a remote cache adapter, while remaining deterministic and dependency-free in
// unit tests.
type Fake[V any] struct {
	*Cache[V]
}

// NewFake constructs an isolated in-memory cache for conformance tests.
func NewFake[V any](cfg Config) *Fake[V] {
	return &Fake[V]{Cache: New[V](cfg)}
}

var _ Store[string] = (*Fake[string])(nil)
