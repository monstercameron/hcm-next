# Changelog

## 2026-05-15

- `ca3e00f` - Allowed scoped org-transfer approvers to retrieve available
  approval actions without requiring broad workflow visibility.
- `196f8e9` - Fixed the org-transfer E2E available-action helper used by the
  expanded approval-chain test.
- `0df392c` - Updated the changelog after the final org-transfer test assertion
  fix.
- `312c71d` - Tightened the org-transfer E2E approval-task assertion to coerce
  the task identifier through the same string path used by workflow transitions.
- `8ceb7dd` - Updated the changelog after expanding org-transfer E2E coverage.
- `97d8e12` - Expanded org-transfer E2E coverage for the HR-started workflow,
  approval routing, execution, idempotent replay, projection changes, and
  filtered timeline checks.
- `e29c0b0` - Updated the changelog after the org-transfer metadata alignment
  commit.
- `0b37642` - Aligned the org-transfer demo fixture date and approval metadata
  with the workflow configuration used by the org-transfer runtime.
- `b72f7fa` - Started the root changelog and documented the initial logical
  implementation commits.
- `503fcc2` - Updated workflow planning documentation, including the workflow
  schema spec, org-aware RBAC plan, API demo notes, project layout notes, and
  detailed TODO workstreams.
- `ff81534` - Added workflow execution improvements, compensation and org-transfer
  workflow configs, Go execution blocks, simulated third-party compensation APIs,
  employee/RBAC E2E coverage, and V0 legal-name guardrail tests.
- `923b887` - Added the org-aware RBAC data foundation: org units,
  relationships, worker assignments, role bindings, expanded demo seed data, and
  reusable employee access filtering helpers.
