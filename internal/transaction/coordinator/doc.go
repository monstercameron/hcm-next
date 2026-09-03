// Package coordinator owns the local atomic commit boundary for a resolved
// transaction plan. It coordinates only caller-supplied local writes; external
// publication is deliberately invoked after the database commit returns.
package coordinator
