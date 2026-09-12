# 2026-09-11 — EP-PROMO-001: durable proposal candidates for the promotion facade

## What changed

The reopened audit on EP-PROMO-001 was right: the facade minted the proposal
revision inside an in-memory `intent.NewProposalLedger` and returned it, so
`intent_input_snapshot`, `intent_simulation_result`, `proposal_revision` and
the four proposal item tables were never written on the propose path. Only
`executionDecision.record` materialized `proposal_revision`, at decision time.

New `internal/intent/app/journey_candidates.go` adds
`journeyEngine.recordProposalCandidates`, called from both `ProposePromotion`
(typed contract) and `Propose` (page form — the same semantic capability, so
it must not silently produce a proposal with no durable candidate set). In one
tenant-scoped transaction it records, in foreign-key order:

1. `intent_input_snapshot` — purpose SIMULATION, sequence 1, the request
   digest and the proposal's source baselines in the canonical body;
2. `proposal_revision` — `EncodeFullProposal` payload,
   `produced_by='hcmnext:intent-cell'`, both digest columns set to the minted
   material digest;
3. `proposal_write_item` / `proposal_effect_item` /
   `proposal_approval_requirement` / `proposal_obligation` sets;
4. `intent_simulation_result` — status READY, bound to the snapshot id and
   the proposal digest.

A `kernel.Obligation` carries no `due_at`, which the obligation row requires;
a proposal that ever carries obligations is a hard error rather than a
silently dropped row (unreachable for P1A promote_worker, which mints none).

## Decisions

- Candidate identities are derived (`uuid.NewSHA1` over
  tenant:intent:kind:sequence) rather than allocated, so a replayed propose
  re-derives the same keys and the stores' `ON CONFLICT DO NOTHING` makes the
  second recording a no-op. This is replay-safe and race-safe.
- Recording happens only when the simulation minted a revision
  (`simulated.Revision != nil`); a BLOCKED answer has no candidate set to
  bind. It is skipped when the engine was composed without the durable
  database (`e.db == nil`), matching `beginTenant`'s availability rule.
- The generic `SimulateIntent` path is untouched: NEXT-005's P1A sweep
  asserts `proposal_revision` stays empty on that path and every
  non-chronology table stays byte-identical, so durable recording is
  deliberately scoped to the facade only. That scoping is the audit's exact
  wording — "facade-specific … persistence".

## Verified

- `go test -count=1 -run TestTodo_EP_PROMO_001_Unit ./internal/intent/app/` PASS
- `go test -count=1 -run TestTodo_EP_PROMO_001_Integration -v ./test/bootstrap/` PASS
  (durable snapshot/revision/sets/result rows, replay writes nothing a second
  time, a recomposed store over the same pool re-reads the same material
  digest and payload)
- `go test -count=1 ./internal/intent/app/` PASS (full package, no
  regressions in the journey suites)
- `go test -count=1 ./test/bootstrap/` PASS (full package; the PROMO-007 and
  NEXT-005 sweeps stay green because the durable writes only run on the
  facade path)

## Partial / not done

- `Inspect`/`ListJourneys` still do not consult the stored candidates for a
  never-executed journey — they re-simulate. That is a read-model decision
  for a later todo, not part of this GREEN contract.
- No migration added; every table written already exists under migration
  00024/00004 with its RLS and forbid-mutation triggers.
