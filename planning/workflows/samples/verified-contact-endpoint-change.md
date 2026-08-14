# Sample: Verified Contact Endpoint Change

Purpose: demonstrate `TRANSFORM`, `RULE`, `DECISION` and `SIGNAL` in a small
self-service workflow. This is exploratory pseudo-definition, not compiled
SchemaFlux input.

```text
TASK collect endpoint
  -> TRANSFORM normalize endpoint
  -> RULE validate endpoint/policy
  -> DECISION verification required?
       no  -> CAPABILITY commit verified-by-policy endpoint
       yes -> CAPABILITY send challenge
              -> SIGNAL wait for verified challenge
  -> CHECKPOINT
  -> CAPABILITY commit contact revision + outbox
  -> OBSERVE authoritative endpoint
  -> DECISION consistent?
       yes -> END completed-consistent
       no  -> SUBWORKFLOW contact-repair
```

| Node                | Type        | Typed input                                                    | Typed output                                      | Routes/evidence                                  |
| ------------------- | ----------- | -------------------------------------------------------------- | ------------------------------------------------- | ------------------------------------------------ |
| collect             | TASK        | `ContactEndpointDraftForm/v1`                                  | `ContactEndpointDraft/v1`                         | submitted/cancelled; task/submission refs        |
| normalize           | TRANSFORM   | draft                                                          | normalized endpoint with normalization profile    | success/invalid; input/output digest             |
| validate            | RULE        | normalized endpoint + purpose/policy snapshot                  | PASS/FAIL/UNKNOWN + verification requirement      | pass/fail/unknown; rule trace                    |
| verification_needed | DECISION    | rule result                                                    | `required` or `not_required`                      | explicit two routes and unknown -> manual task   |
| send_challenge      | CAPABILITY  | endpoint, challenge purpose                                    | challenge ref/expiry; no secret token in workflow | sent/failed; messaging operation ref             |
| wait_verified       | SIGNAL      | `contact.challenge.verified/v1`, challenge correlation         | accepted verified-endpoint assertion              | timeout/invalid/duplicate/verified signal refs   |
| commit              | CAPABILITY  | proposal, verification assertion, expected contact stream head | committed contact revision/outbox                 | committed/conflict/ambiguous transaction receipt |
| observe             | OBSERVE     | expected endpoint + authority                                  | observation/reconciliation                        | PASS/FAIL/UNKNOWN                                |
| repair              | SUBWORKFLOW | failed reconciliation                                          | pinned `people.contact_repair/v1` child result    | repaired/repair-required child refs              |
| done                | END         | completion dimensions                                          | workflow result                                   | consistent/degraded/cancelled                    |

Signal-specific negative cases:

```text
wrong tenant or challenge ID       reject and record security evidence
invalid provider signature         reject; do not resume
duplicate verified event           replay accepted signal; one continuation
event after timeout/cancellation    record late signal; do not resume
valid signal before subscription    inbox/dedupe matching policy decides; never lose silently
```
