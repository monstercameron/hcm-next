// Package timeauth implements the TIME-001 trusted-time health and temporal
// evidence contract (planning/todos.md `TIME-001`;
// planning/data/models/operations-production.md, "Scheduling, Timekeeping,
// and Clock Integrity" and "Cyber Recovery, Trusted Time, and Crypto
// Agility"; planning/data/models/wire-contract-primitives.md,
// "TemporalInstant" and "Temporal invariants").
//
// # Clock port
//
// Clock is the one-method port every time source implements: the host wall
// clock, an NTP-disciplined source, an external attested time authority, or
// FakeClock in tests. A Monitor wraps a Clock and turns each Sample into
// Evidence: a health verdict (TRUSTED, DEGRADED, or UNTRUSTED), the drift it
// measured against the previous sample, and the reasons behind that
// verdict. Nothing about a Sample is trusted merely because the source
// claims it is; SelfReportedHealth is one input among several, and a
// backward wall jump or an unknown/excessive uncertainty bound forces
// UNTRUSTED regardless of what the source claims about itself.
//
// # Refusal path
//
// RequireTrusted is how a sensitive operation should obtain time: it
// returns a TrustedInstant bound to the Evidence that justified it, or
// ErrTimeUntrusted (matchable with errors.Is) when health is UNTRUSTED. An
// approval, a ledger signature, a cutoff decision, or a token-expiry check
// must go through this path rather than reading a clock directly, so that
// none of those decisions can be made on time the platform does not trust.
//
// # Business time stays distinct from host clock time
//
// TrustedInstant carries a values.Instant — the kernel's business-effective
// temporal value — and the Evidence that justified it. It never carries a
// monotonic tick count or any other host-clock internal: replaying or
// auditing a decision needs the business instant and its evidence, never
// the raw counter a monotonic clock happened to be at.
package timeauth
