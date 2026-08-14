# Vertical-Slice Gap Register

## Current blocking gaps

| Gap                      | Scope                                           | Required closure                                                                                         |
| ------------------------ | ----------------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| `SLICE-GAP-SOURCE`       | 516 of 530 accepted baseline slots              | immutable numbered source manifest plus exact identity/provenance join                                   |
| `SLICE-GAP-IDENTITY`     | 807 repository vocabulary candidates            | classify as baseline identity, alias, reviewed extension, event/capability rather than intent, or reject |
| `SLICE-GAP-DEFINITION`   | every source-bound candidate below `CONTRACTED` | complete IntentDefinition metadata and typed request/result schemas                                      |
| `SLICE-GAP-CONFIG`       | every slice                                     | MAX-v1 applicability, prohibited values and mandatory interaction scenarios                              |
| `SLICE-GAP-DATA`         | every slice                                     | exact entity/property read/write/effect, authority, temporal and freshness contracts                     |
| `SLICE-GAP-GOVERNANCE`   | every slice                                     | AuthZ/legal/privacy/risk/entitlement/obligation composition and decision rights                          |
| `SLICE-GAP-EXECUTION`    | every slice                                     | capability/engine/workflow/transaction/irreversible-boundary ownership                                   |
| `SLICE-GAP-HUMAN`        | applicable slices                               | assignee resolution, compartments, representation, delegation, SoD, expiry, appeal and continuity        |
| `SLICE-GAP-CONNECTIVITY` | applicable slices                               | external operation, credential, mapping, observation, ambiguity and reconciliation contracts             |
| `SLICE-GAP-LIFECYCLE`    | every material slice                            | cancellation, correction, supersession, compensation, retention and multidimensional completion          |
| `SLICE-GAP-ENDPOINT`     | every contracted feature                        | typed/public/generic/internal/event/schedule/no-endpoint disposition and parity tests                    |
| `SLICE-GAP-TEST`         | every slice                                     | exact maximal-configuration scenario/test/evidence manifest and production limits                        |

## Cross-catalog overlaps requiring one stable identity decision

The current repository maps these names from more than one domain source:

```text
CreatePerformanceImprovementPlan
ApplyLegalHold
ReleaseLegalHold
AnalyzePayEquity
CorrectCompensation
```

An overlap may legitimately represent one intent consuming multiple domain
profiles. It may also be a display-name collision requiring qualified identities.
The source-manifest join decides; generation must not merge by display name.

## Fixed-point rule

The gap compiler emits a finding only when the expanded slice references a
missing, ambiguous or insufficient contract. It resolves findings against the
current todo and ownership registries and emits no duplicate implementation
todo. Re-running against unchanged inputs must produce the same finding set and
digest; otherwise convergence is not measurable.
