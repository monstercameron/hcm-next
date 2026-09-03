// Package depadmission implements the TOOL-019 dependency license and
// vulnerability admission policy, and the TOOL-024 symbol-aware
// govulncheck evidence layered on top of it.
//
// # Scope
//
// TOOL-019 answers one question per module in the build: is this
// dependency admissible? A module is admitted only when:
//
//   - its license resolves to an allow-listed SPDX identifier (detected
//     from a LICENSE/COPYING file in the module cache, or from a reviewed
//     override in definitions/architecture/dependency-admission.yaml for a
//     module whose repository ships no machine-readable license file);
//   - it carries a complete governance row in
//     definitions/architecture/dependency-roles.yaml (owner, security
//     owner, upgrade SLA, replacement strategy) via
//     tools/policy/depmanifest, for every module go.mod's own require
//     block lists (see that manifest's documented scope note for why
//     purely transitive, non-required modules are exempt); and
//   - it carries no reachable vulnerability finding without a governed,
//     unexpired, digest-bound exception.
//
// TOOL-024 supplies the last bullet's evidence: it parses `govulncheck
// -json` output (a stream of Message objects, matching the real tool's
// wire format) and classifies each Finding as reachable (a traced call
// chain reaches the vulnerable symbol) or unreachable (module-only,
// no traced call). An unreachable finding is still recorded in the report,
// never dropped; only a reachable finding blocks admission unless excepted.
//
// # Evidence, not just an exit code
//
// govulncheck's own vulnerability database and reachability analysis are
// symbol-aware. What this package adds is admission policy over that
// evidence: exceptions require an owner, an expiry, a compensating
// control, and a pinned module_version (so a later, different, still
// vulnerable version cannot silently inherit an old exception). Missing
// evidence (no scan performed at all) is reported as SKIPPED_WITH_REASON,
// never as a silent PASS; the offline development machine cannot reach
// golang.org/x/vuln's database, so `go run
// golang.org/x/vuln/cmd/govulncheck@latest` runs in CI only (see
// .github/workflows/tests.yml), and this package's own tests cover the
// parsing and policy logic entirely from synthetic fixtures under
// testdata/.
//
// # Determinism
//
// Report is always serialized with modules sorted by path and findings
// sorted by vulnerability ID, so two runs over the same inputs produce
// byte-identical JSON.
package depadmission
