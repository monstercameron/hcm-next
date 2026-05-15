# Go Block Contract

Go blocks keep deterministic workflow-specific business logic out of the TypeScript control plane. Existing block-specific JSON fields remain supported, but new and migrated blocks should also emit the generic output contract fields from `internal/executor` or `sdk`:

```json
{
  "facts": [],
  "routeKey": "approval_required",
  "validationErrors": [],
  "warnings": [],
  "transactionPlan": null,
  "externalCalls": [],
  "projectionPatches": [],
  "ledgerFacts": []
}
```

Validation blocks should set `routeKey` with the standard validation route keys and mirror business errors into `validationErrors`. Transaction planning blocks should set `routeKey` to `transaction_plan_ready` and populate `transactionPlan`, `ledgerFacts`, `externalCalls`, and `projectionPatches`.

Customer-authored blocks can import `hcm-next-executor/sdk` for the stable contract aliases and helper constructors.
