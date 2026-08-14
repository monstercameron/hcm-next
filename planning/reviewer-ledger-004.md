# Adversarial reviewer ledger — 004

Date: 2026-08-14

## Execution status

Reviewer 004 inspected the planning corpus and the current execution plan. The
runtime exposed to this agent has no `spawn_agent`, `followup_task`, or
`send_message` collaboration tool, so reviewers 005–256 could not be launched
from this context. No reviewer IDs are claimed complete and no synthetic
findings are attributed to absent reviewers.

## Actionable findings

1. **[P0 / Gate B] Make the acceptance evidence executable and machine-checkable.**
   The plan requires dozens of behaviors (atomic streams/outbox, process-death
   resume, idempotency retention, restore, upgrade rollback, revocation,
   quarantine, SSRF, WCAG, locale/direction, and IdP outage), but does not point
   each requirement to a named test/fixture, command, owner, and expected
   result. Add a Gate B evidence matrix with stable IDs, exact test commands,
   expected pass/fail output, artifact retention, and sign-off owner.
   Citation: `planning/execution-plan.md:373-435`.

2. **[P0 / Gate A] Add a quantitative baseline and denominator protocol.**
   “Repeatedly” and “measurable improvement” are not operational thresholds;
   define minimum run count, eligible-volume denominator, sampling period,
   confidence/variance handling, bypass classification, and stop/reselect rules
   before partner observation. Citation: `planning/execution-plan.md:347-369`.

3. **[P0 / Gate B] Define the security/privacy test corpus, not just controls.**
   Tenant/org/field/purpose authorization, safe-content quarantine, DestinationTrust,
   SSRF/rebinding, session revocation, step-up, and IdP outage each need explicit
   adversarial fixtures and expected denial/audit behavior, including cross-tenant
   and stale-federation cases. Citation: `planning/execution-plan.md:401-434`.

4. **[P1 / Gate B] Close external-effect ambiguity.**
   The plan requires handling post-submit/pre-commit ambiguity and observed
   reconciliation, but does not prescribe connector-specific evidence for unknown
   outcomes, timeout/duplicate response, remote version absence, or manual repair
   authorization. Add a failure matrix mapping each outcome to state, retry,
   operator action, customer-visible status, and ledger evidence.
   Citation: `planning/execution-plan.md:385-397`.

5. **[P1 / Gate B] Pin legal/privacy obligations to the actual promotion path.**
   The high-impact decision requirement mentions notice, explanation, correction,
   and contest, but does not identify applicable jurisdictions, retention/legal
   hold interaction, worker access/deletion exceptions, or who signs the legal
   interpretation. Add a jurisdiction/obligation fixture and a legal approval
   record before write authority. Citation: `planning/execution-plan.md:408-412`.

6. **[P1 / Gate C] Separate deferred Workforce OS obligations from pilot gates.**
   Gate C currently lists broad additions but lacks explicit entry criteria and
   owner for the later Workforce OS planes (billing, regulatory, analytics,
   agent, multi-cell, deletion, and relocation). Add a Gate C obligation register
   with trigger, evidence, staffing, and “not a Gate B blocker” status.
   Citation: `planning/execution-plan.md:437-443`; `planning/plan.md` final plan.

7. **[P1 / Operations] Add recovery consistency checks across derived stores.**
   Restore acceptance says ledger, artifacts, projections, configuration, and the
   reference workflow reproduce, but omits outbox/connector-operation journal
   replay ordering, external-effect deduplication, and post-restore watermark
   validation. Add a restore drill with expected hashes/counts and explicit
   external redrive prohibition.
   Citation: `planning/execution-plan.md:403-404`.

8. **[P2 / Product] Specify commercial readiness evidence.**
   Paid agreement, price, cost-to-serve, and stop thresholds are named, but no
   invoice/entitlement boundary, support SLA, data-export promise, or renewal
   acceptance artifact is required for the pilot. Add these to the design-partner
   manifest while keeping billing implementation deferred unless sold.
   Citation: `planning/execution-plan.md:244-287`.

## Gate interpretation

These findings do not imply CLEAN. Items explicitly deferred remain later
Workforce OS obligations; they must be tracked in Gate C rather than silently
treated as satisfied by deferral. Gate A is read/observe only; Gate B is the
limited write authority gate; Gate C is general production authority.
