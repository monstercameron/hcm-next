// Package productui owns the production presentation composition layer.
//
// Enterprise extension boundaries:
//   - registry.go: stable page inventory and routing identity
//   - shell.go: application chrome and authorization-resolved navigation
//   - components.go: reusable, domain-neutral design-system components
//   - page_*.go: feature-owned page composition
//   - selectors.go: pure presentation selectors over authorized view models
//   - model.go: presentation-only view contracts
//
// Business rules, authorization, workflow state, and credentials do not belong
// in this package. Production callers must provide an already-authorized View.
package productui
