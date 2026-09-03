// Package gateevidence implements the NEXT-002/NEXT-003 P1A gate-evidence
// pipeline: a signed delivery manifest naming exactly the P1A release's
// intents, capabilities, commands, migrations and qualification decisions
// (NEXT-002), and a compiler that closes that manifest against real test
// evidence (NEXT-003).
//
// # NEXT-002 - the signed manifest
//
// [P1AManifest] is the schema for definitions/planning/gates/p1a-manifest.yaml:
// the eight P1A intent contracts (next-steps.md "P1A - paid observation,
// preflight and simulation"), the ten bootstrap capabilities
// (internal/capability/bootstrap.go, cross-checked by TOOL-004), the four
// P1A commands (cmd/hcmnext, cmd/worker, cmd/projector, cmd/migrate), the
// fourteen embedded migrations plus the one recorded numbering gap, the four
// P1A/P1B toolchain qualification decisions (TOOL-004, TOOL-008,
// UX-QUAL-001, WF-RUN-000), and the six todos' evidence tests that
// substantiate them. [CanonicalDigest] hashes every field except the
// signature itself; [Sign] and [Verify] cover that digest with Ed25519
// using a repo-local development key checked in for tests only under
// testdata/dev-signing-key.yaml (see doc.go's warning there - it is not a
// production signing key and must never be used to sign anything but this
// fixture manifest).
//
// [P1BTemplate] is the disjoint, deferred sibling schema for
// definitions/planning/gates/p1b-template.yaml: the six P1B candidate
// write/approval contracts, each held at ActivationStatus
// BLOCKED_PENDING_GATE_A. [P1BTemplate.CanActivate] is false until a real
// Gate A PROCEED decision (tools/planning/authoritygate, which this package
// does not populate) and a fresh authority digest both exist - this package
// only proves the template cannot silently activate, not that Gate A has
// happened.
//
// # NEXT-003 - the evidence compiler
//
// [Compile] closes a [P1AManifest]'s Evidence entries against either a
// checked-in results file (default; see [LoadResults] and
// definitions/planning/gates/p1a-evidence-results.json) or a live
// `go test -count=1 -run ...` run per entry when CompileOptions.Live is
// true. Every entry resolves to exactly one [Verdict]: OK, MISSING, STALE,
// OUT_OF_MANIFEST or EFFECTFUL. A manifest with any non-OK finding compiles
// to GateDecisionBlocked; this is an evidence-closure verdict, not the
// human-signed Gate A authority decision authoritygate.Record represents -
// NEXT-003's own GREEN clause is explicit that "passing evidence can yield
// a signed decision but never write authority."
//
// [RenderMarkdown] and [RenderJSON] render a [Report] deterministically:
// same manifest, same results and same CompileOptions.Now always produce
// byte-identical output, which is what lets
// definitions/planning/gates/p1a-evidence-report.md and .json be checked in
// and drift-checked (see manifest_test.go's
// TestP1AEvidenceReportArtifactsAreCurrent) the same way TOOL-010 drift-
// checks generated code.
//
// This package reuses tools/planning/todoregistry (to parse
// planning/todos.md when a caller wants to cross-check a todo ID) and
// tools/planning/traceability.ScanTestNames (to confirm an evidence test
// function actually exists in the repository) rather than re-implementing
// either scan.
package gateevidence
