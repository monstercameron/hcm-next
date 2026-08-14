# Sample: Equipment Shipment Cancellation

Purpose: demonstrate explicit `COMPENSATE` behavior for a genuinely reversible
external effect. Compensation is not used to erase business history.

```text
CAPABILITY reserve equipment
  -> APPROVAL equipment assignment
  -> CAPABILITY create shipment order
  -> OBSERVE shipment accepted
  -> WAIT until onboarding cancellation or dispatch deadline
  -> SIGNAL onboarding.cancelled OR timer fired
  -> DECISION shipment state and cancellation reason
       continue -> END shipment-active
       cancellable -> COMPENSATE cancel shipment
                       -> OBSERVE shipment cancelled
                       -> END compensated
       dispatched -> SUBWORKFLOW equipment-recovery
```

| Node                | Type        | Contract                                                                |
| ------------------- | ----------- | ----------------------------------------------------------------------- |
| reserve             | CAPABILITY  | fenced inventory reservation; idempotent                                |
| approve             | APPROVAL    | exact equipment/recipient/cost proposal binding                         |
| order               | CAPABILITY  | external resource key, expected provider version and stable effect key  |
| observe_order       | OBSERVE     | provider order/version/status; PASS/FAIL/UNKNOWN                        |
| cancellation_window | WAIT        | durable dispatch deadline with timezone/reference policy                |
| cancellation_signal | SIGNAL      | correlated committed onboarding cancellation event                      |
| decide              | DECISION    | `continue`, `cancellable`, `already_dispatched`, `unknown`              |
| cancel_order        | COMPENSATE  | authorized `equipment.shipment.cancel/v1` targeting original effect ref |
| verify_cancel       | OBSERVE     | provider status must be cancelled; otherwise RepairPlan                 |
| recovery            | SUBWORKFLOW | pinned physical recovery workflow for irreversible dispatched state     |
| end                 | END         | original order and compensation both remain in evidence                 |

Negative cases: cancellation API timeout becomes ambiguous and is observed before
retry; a changed provider version causes revalidation; duplicate compensation
replays the original result; dispatch racing cancellation routes to recovery; an
unauthorized onboarding-cancellation claim cannot trigger compensation.
