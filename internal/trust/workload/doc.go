// Package workload owns two related trust-plane concerns for process-to-
// process traffic inside Human Capital Management Suite: issuing and verifying short-lived
// workload identities (TRUST-006), and deciding, deny-by-default, whether
// one verified workload identity may act on another workload's resource
// (TRUST-007).
//
// Semantic owner: governance-and-trust. Phase: P1A. Todos: TRUST-006,
// TRUST-007. Depends on: TOOL-018 (release provenance signing/verification;
// this package reuses ed25519, the same asymmetric primitive that todo
// already puts in the release supply chain, rather than introducing a
// second signing scheme for the same trust boundary).
//
// # Identity
//
// [ProcessRole] is the closed vocabulary of
// definitions/architecture/process-roles.yaml: hcmnext, worker, projector
// and migrate ship in P1A; scheduler and admin are reserved for P1B and
// carry no allow-matrix entries yet. A [Identity] is never constructed from
// a bare node or network identity (a pod name, an IP, an mTLS peer string
// with no further meaning) — [Issuer.Issue] mints one signed, time-bounded
// credential per workload instance, and [Verifier.Verify] is the only door
// through which an [Identity] value comes into existence in a running
// process, exactly as [trust.Verifier] is the only door for a
// [trust.Principal]. A lifetime longer than 15 minutes is refused at
// issuance, and an expired or wrong-cell credential is refused at
// verification: network reachability between two processes never implies
// authority between them.
//
// # Authorization
//
// [Authorize] evaluates one [Request] — a verified caller [Identity], a
// [Resource] and an [Action] — against [AllowMatrix], a compiled-in,
// deny-by-default table derived from the reads/writes each process role
// declares in process-roles.yaml. A caller/resource/action triple absent
// from the table is denied, not merely unmatched: there is no wildcard and
// no fallback allow. Every [Decision], allow or deny, carries the rule id
// that produced it (or the fixed no-match reason for a deny), so a denial
// is explainable without exposing anything about the resource beyond
// whether this one triple is permitted.
package workload
