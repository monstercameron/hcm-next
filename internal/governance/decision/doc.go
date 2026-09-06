// Package decision composes the evidence-bearing governance decision for a
// material proposal. It is a pure coordinator: source authorities supply
// typed findings, and this package records their versions, applies the
// declared precedence table, unions obligations, and never performs effects.
package decision
