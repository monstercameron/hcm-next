# Workflow Step-Type Sample Coverage

Coverage requires a workflow graph/table that uses the primitive with its actual
runtime meaning. Merely naming a primitive in a vocabulary list does not count.

| Primitive   | Step contract                          | Existing/deep sample                                                                                                    | Backfill status        |
| ----------- | -------------------------------------- | ----------------------------------------------------------------------------------------------------------------------- | ---------------------- |
| CAPABILITY  | [catalog](step-types.md#1-capability)  | [Contact information update](../people/contact-information-update.md)                                                   | covered                |
| DECISION    | [catalog](step-types.md#2-decision)    | [Verified endpoint](../samples/verified-contact-endpoint-change.md)                                                     | backfilled             |
| APPROVAL    | [catalog](step-types.md#3-approval)    | [Compensation change](../rewards/compensation-change.md)                                                                | covered                |
| TASK        | [catalog](step-types.md#4-task)        | [Legal name change](../people/legal-name-change.md)                                                                     | covered                |
| WAIT        | [catalog](step-types.md#5-wait)        | [Manager change](../people/manager-change.md)                                                                           | covered                |
| SIGNAL      | [catalog](step-types.md#6-signal)      | [Verified endpoint](../samples/verified-contact-endpoint-change.md)                                                     | backfilled             |
| PARALLEL    | [catalog](step-types.md#7-parallel)    | [Promotion](../rewards/promotion-into-management.md), [bulk acknowledgement](../samples/bulk-policy-acknowledgement.md) | covered + strengthened |
| JOIN        | [catalog](step-types.md#8-join)        | [Bulk acknowledgement](../samples/bulk-policy-acknowledgement.md)                                                       | backfilled             |
| SUBWORKFLOW | [catalog](step-types.md#9-subworkflow) | [Bulk acknowledgement](../samples/bulk-policy-acknowledgement.md)                                                       | backfilled             |
| TRANSFORM   | [catalog](step-types.md#10-transform)  | [Verified endpoint](../samples/verified-contact-endpoint-change.md)                                                     | backfilled             |
| RULE        | [catalog](step-types.md#11-rule)       | [Compensation change](../rewards/compensation-change.md)                                                                | covered                |
| AGENT       | [catalog](step-types.md#12-agent)      | [Agent-assisted case triage](../samples/agent-assisted-hr-case-triage.md)                                               | backfilled             |
| DOCUMENT    | [catalog](step-types.md#13-document)   | [Legal name](../people/legal-name-change.md), [bulk acknowledgement](../samples/bulk-policy-acknowledgement.md)         | covered                |
| OBSERVE     | [catalog](step-types.md#14-observe)    | [Payroll correction](../payroll/payroll-correction-retro.md)                                                            | covered                |
| CHECKPOINT  | [catalog](step-types.md#15-checkpoint) | [Termination](../lifecycle/termination-offboarding.md)                                                                  | covered                |
| COMPENSATE  | [catalog](step-types.md#16-compensate) | [Equipment shipment cancellation](../samples/equipment-shipment-cancellation.md)                                        | backfilled             |
| END         | [catalog](step-types.md#17-end)        | [Contact information update](../people/contact-information-update.md)                                                   | covered                |

```text
target primitives:        17
documented:               17
sampled:                  17
new sample gap closures:   7 primitive classes
```

## Legacy extraction coverage

The seven legacy JSON workflows contain these historical node classes:

```text
interaction       10
block              6
approval          10
approval_gate      2
policy_check       6
transaction_plan   7
data_write          3
projection_write    7
external_write      9
ledger_event         6
manual_repair        2
ai_review            1
terminal            28
```

These counts establish extraction coverage, not target vocabulary. Their mapping
is documented in the [legacy-node mapping](step-types.md#legacy-node-mapping).

## Publication gate

Before a primitive is implementable in the Go workflow engine it additionally
needs:

```text
Protobuf NodeDefinition variant and typed execution result
SchemaFlux source schema and deterministic compiler validation
Go executor interface/implementation
state transition and persistence contract
positive, negative, retry, cancellation and recovery conformance vectors
telemetry/evidence/redaction profile
sample compiled through the real toolchain
```

The current matrix proves planning-sample coverage only. No row claims the missing
compiler or Go runtime implementation exists.
