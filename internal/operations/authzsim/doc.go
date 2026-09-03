// Package authzsim implements the AuthZ simulator half of ADMIN-003: a
// read-only wrapper over internal/trust/authz.Simulate that pins a request
// to a stated policy version, renders a redaction-safe explanation, and
// diffs two decisions so an operator can see exactly what a role,
// relationship or field-grant change would alter before it is made
// (planning/specs/organization-scope-and-authz.md).
//
// [Simulate] calls only authz.Simulate, never authz.Enforce - and
// authz.Simulate is itself defined as "evaluate", the identical pure
// function authz.Enforce calls, so a simulated decision can never diverge
// from what a real request with the same inputs would produce, and nothing
// this package does can affect what a later real request resolves to. This
// package holds no state, opens no connection and performs no write of any
// kind: every exported function is a pure function of its arguments.
//
// [Diff] compares two authz.Decision values - typically "before" and
// "after" a proposed role or relationship change - field by field. It
// applies authz.Decision.Explain's own redaction discipline: when either
// side's subject is not disclosable, no field name, domain or rule ID is
// named, only the coarse before/after disclosability itself, so a diff can
// never be used to infer what a denied decision would otherwise have
// granted.
package authzsim
