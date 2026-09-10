# Promotion

```text
workflow_id: people.promotion/v1
legacy_intent: employee.promotion
target_intent: hcmnext.people.promote_worker
kernel_family: ChangeRequest
subject: Person + Worker context
initiator: manager self-service
state: REFERENCE + EXPLORED
owner_domain: people
```

## Steps

| #   | Primitive  |
| --- | ---------- |
| 1   | CAPABILITY |

## Features and dependencies

Required dependencies: Identity/AuthN, People.
