# Legal Name Change Workflow

This folder is the workflow-owned home for the V0 `employee.legal_name.change` definition and orchestration.

## Blocks

- `system.employee_data.legal_name.preflight`
- `system.employee_data.legal_name.plan_transaction`

## Runtime Path

```text
start intent
submit input
preflight
provide evidence
create approval task
approve
create transaction plan
execute
update projection
queue external write
```
