// Package diagnosticsession defines the support-safe ADMIN-006 diagnostic
// session contract. It is deliberately transport and persistence agnostic:
// callers receive a short-lived, purpose-bound JIT scope and may only render
// redacted evidence or invoke the allowlisted, read-only UI actions.
package diagnosticsession
