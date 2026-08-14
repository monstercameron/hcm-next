# Sample: Bulk Policy Acknowledgement Campaign

Purpose: demonstrate bounded `PARALLEL`, `JOIN`, and `SUBWORKFLOW` behavior.

```text
CAPABILITY resolve authorized audience snapshot
  -> RULE validate disclosure/localization requirements
  -> PARALLEL bounded partitions (max 20)
       branch per partition:
         SUBWORKFLOW policy-acknowledgement-recipient/v3
           DOCUMENT deliver policy
           SIGNAL acknowledgement received
           END recipient result
  -> JOIN REQUIRED_SET
  -> DECISION mandatory population satisfied?
       yes -> END completed
       no  -> TASK resolve unreachable recipients
              -> END completed-degraded or repair-required
```

| Node                | Type        | Contract                                                                                             |
| ------------------- | ----------- | ---------------------------------------------------------------------------------------------------- |
| audience            | CAPABILITY  | returns frozen authorized `AudienceSnapshot` with source watermark and exclusions                    |
| validate_campaign   | RULE        | verifies template/locales, legal requirement, delivery policy, DLP and cost/capacity                 |
| fanout              | PARALLEL    | partitions frozen audience; bounded concurrency/cost; per-recipient semantic key                     |
| recipient           | SUBWORKFLOW | pins child v3; maps one recipient; parent cancellation propagates                                    |
| deliver             | DOCUMENT    | renders versioned localized artifact and requests required delivery semantics                        |
| acknowledge         | SIGNAL      | accepts verified recipient acknowledgement correlated to artifact/version                            |
| join                | JOIN        | `REQUIRED_SET`; all legally mandatory recipients accounted for; permitted exclusions remain explicit |
| completion_decision | DECISION    | routes satisfied/degraded/repair-required from per-branch dimensions                                 |
| exception_task      | TASK        | human resolves invalid endpoint, accommodation or permitted manual delivery                          |
| end                 | END         | includes delivered/acknowledged/unreachable/excluded counts and obligation state                     |

The parent does not copy every child payload. It stores child references and a
typed aggregate. A branch retry never duplicates delivery because the child and
message intent retain stable recipient/campaign idempotency keys. `JOIN` timeout
cannot convert missing mandatory acknowledgements into success.
