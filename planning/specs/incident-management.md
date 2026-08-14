# Business and Technical Incident Management Contract

The Incident Service owns the operational truth of detected conditions,
declaration, affected populations, severity, responder coordination, mitigation,
repair and communication linkage, resolution, and review. Alerts and RepairPlans
are inputs/links, not substitutes for an Incident.

## Canonical State

```text
Condition
Incident
IncidentRevision
AffectedSet
SeverityAssessment
ResponderAssignment
Mitigation
RepairLink
CommunicationLink
PostIncidentReview
```

```text
DETECTED -> TRIAGED -> DECLARED -> MITIGATING -> MONITORING -> RESOLVED -> REVIEWED
               |           |                                    |
          FALSE_POSITIVE  MERGED/SPLIT/DUPLICATE                 +-> REOPENED
```

Fingerprints are versioned hypotheses, not permanent identity. Merge preserves
source incident IDs and timelines; split creates children and reallocates
conditions/affected scope; duplicate links without erasing evidence.

## APIs and Severity

```text
incidents.detect|triage|declare|assign|merge|split|mark_duplicate
incidents.affected_set.update|verify
incidents.mitigate|resolve|reopen|review
incidents.query|read|timeline
```

Business severity and technical severity are separate. Assessment includes
capability/domain, tenant/cell/region, worker/population, payroll/security/legal/
privacy/financial impact, data integrity, duration, workaround, SLO/error-budget,
external consistency, repair backlog and confidence. `AffectedSet` carries source
watermarks and may be `KNOWN_PARTIAL`; customer communication cannot claim a
complete population without verification.

Incident commander, security/privacy/legal leads, domain owner, communications
approver and repair authority are explicit assignments. An incident may mitigate
before full cause is known; resolution requires conditions stopped/contained,
affected set bounded, repair/reconciliation obligations owned, and monitoring
window satisfied. Review records causes, control failures, customer impact,
corrective actions/owners/dates and recurrence verification.

## Security and Evidence

Tenant-visible and internal/security compartments are distinct fields/views.
Cross-tenant aggregation never reveals identities or another tenant's impact.
Declare/severity/merge/split/resolve/customer-visible fields require scoped
authority; security and privacy incidents apply stricter compartments and legal
holds. Raw alerts remain telemetry; material incident transitions enter evidence.

Evidence includes conditions/fingerprints, revisions, affected-set queries and
watermarks, severity inputs/decisions, roles, actions/mitigations, workflow/
transaction/repair/SLO/communication links, resolution criteria, reopen, review,
and corrective-action verification.

Gate A implements deployment/connector/data-quality incident intake. Gate B
aggregates one connector outage across many transactions, links repair backlog and
customer advisory, and reconciles incident resolution with transaction recovery.
