package subscription

// Repository is the persistence-neutral port for immutable subscription
// revision history. Registry is the kernel-pure in-memory implementation;
// durable adapters may add tenant, context and error-returning methods around
// this small vocabulary-facing surface.
type Repository interface {
	Append(EventSubscription) error
	Revisions(subscriptionID string) []EventSubscription
	Active() []EventSubscription
}

var _ Repository = (*Registry)(nil)

// AuthorizationRepository is the persistence-neutral port for the separate
// authorization evidence stream. AuthorizationEvent intentionally carries no
// tenant field: durable implementations bind it to their tenant context.
type AuthorizationRepository interface {
	AppendAuthorization(AuthorizationEvent) error
	AuthorizationEvents(subscriptionID string) []AuthorizationEvent
}
