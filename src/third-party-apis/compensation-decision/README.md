# Simulated Compensation Decision API

This package is a separate simulated third-party compensation approval service.
It is intentionally outside `src/api`, which is the internal Human Capital Management Suite API server.

Run it locally:

```bash
npm run dev:third-party:compensation-decision
```

Default base URL:

```text
http://localhost:4302
```

Example decision:

```bash
curl -sS -X POST "http://localhost:4302/v1/compensation-decisions" \
  -H "content-type: application/json" \
  -d '{"workerId":"emp_123","changeRequestId":"chg_123","currentCompensation":{"amount":93000,"currency":"USD","payFrequency":"annual","bonusTargetPercent":5,"effectiveDate":"2026-01-01"},"proposedCompensation":{"amount":98000,"currency":"USD","payFrequency":"annual","bonusTargetPercent":5,"effectiveDate":"2026-06-01"},"effectiveAt":"2026-06-01","businessReason":"retention_adjustment"}'
```
