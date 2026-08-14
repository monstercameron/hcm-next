# User-Flow Archetypes

These archetypes provide reusable experience spines. A concrete flow composes
one or more and declares its domain-specific delta.

## `UF-A1` Direct self-service change

```text
DISCOVER -> ORIENT -> COLLECT -> VALIDATE -> COMPARE -> CONFIRM -> SUBMIT
         -> TRACK -> COMPLETE
```

Use for contact, emergency-contact, preference and other bounded changes. Add
step-up, verification signal, future-effective wait, external observation or
approval only when required by the intent.

## `UF-A2` Governed proposal and approval

```text
DISCOVER -> ORIENT -> COLLECT -> SIMULATE -> COMPARE -> CONFIRM -> SUBMIT
         -> TRACK -> REVIEW* -> WAIT -> REVALIDATE/REPLAN -> EXECUTE
         -> RECONCILE -> COMPLETE or REPAIR
```

Use for promotion, compensation, manager, position and high-risk access changes.
Every reviewer sees the exact proposal digest and only permitted evidence.

## `UF-A3` Eligibility, evidence and entitlement process

```text
DISCOVER -> ORIENT -> COLLECT -> SUBMIT -> TRACK
         -> server eligibility/composition
         -> TASK evidence/review loop
         -> determination/notice
         -> WAIT effective event
         -> REVALIDATE/REPLAN -> EXECUTE -> TRACK
         -> return/closure process
```

Use for leave, benefits, accommodation, authorization and regulated programs.
Eligibility is not silently delegated to human approval.

## `UF-A4` Assigned human work

```text
NOTIFY/QUEUE -> ORIENT -> CLAIM/OPEN -> REVIEW -> DECIDE or REQUEST_INFO
             -> CONFIRM -> SUBMIT -> COMPLETE
```

Use for approvals, evidence review, exceptions and case tasks. It includes
assignment changes, delegation, recusal, SLA, proposal staleness and safe
continuity when the assignee loses authority.

## `UF-A5` Case and investigation

```text
REPORT -> SAFETY/CONFIDENTIALITY ORIENTATION -> COLLECT MINIMUM FACTS
       -> SUBMIT -> SAFE CONTACT/STATUS
       -> TRIAGE -> ASSIGN/RECUSE -> INVESTIGATE/TASK LOOPS
       -> FINDING/DISPOSITION -> NOTICE -> APPEAL? -> CLOSE
```

Use for HR requests, employee relations, complaints, safety and privacy cases.
Participant visibility is relationship- and compartment-specific; one universal
case screen is prohibited.

## `UF-A6` Expert exception and repair

```text
ALERT/SEARCH -> ORIENT -> INSPECT CORRELATED EVIDENCE
             -> DIAGNOSE -> SIMULATE REPAIR -> REVIEW/APPROVE
             -> EXECUTE -> OBSERVE -> VERIFY/RECONCILE -> CLOSE
```

Use for DataOps, integration, payroll exceptions, projection drift and access
repair. The workbench must distinguish a new business transaction from targeted
repair and prohibit blind redrive after ambiguous effects.

## `UF-A7` Batch/population operation

```text
DISCOVER -> DEFINE/FREEZE POPULATION -> PREVIEW SAMPLE/AGGREGATES
         -> VALIDATE -> SIMULATE COST/FAILURES -> CONFIRM
         -> SUBMIT -> TRACK PARTITIONS/EXCEPTIONS
         -> RECONCILE -> COMPLETE or REPAIR SUBSET
```

Use for bulk changes, campaigns, imports, cycles and filings. Unauthorized users
cannot infer excluded members or suppressed counts.

## `UF-A8` Analysis to action

```text
ASK/SELECT QUESTION -> CLARIFY PURPOSE/SCOPE -> PLAN/ESTIMATE
                    -> RUN -> INSPECT RESULT/LINEAGE/UNCERTAINTY
                    -> PROPOSE FOLLOW-UP INTENT -> SIMULATE -> HUMAN HANDOFF
```

Use for reports, dashboards, Ask HCM, planning and agents. Correlation,
prediction and causation remain visibly distinct. Analysis never executes a
material action directly.

## `UF-A9` Candidate or external participant

```text
INVITATION/DISCOVERY -> IDENTITY/CONSENT -> COLLECT -> SAVE/RESUME
                     -> SUBMIT -> SCHEDULE/TASK LOOPS
                     -> OFFER/DOCUMENT REVIEW -> SIGN/DECLINE
                     -> CONVERT/HANDOFF -> TRACK
```

Use for recruiting, onboarding, dependent and former-worker experiences. The
flow explicitly handles identity linkage, expiring links, post-employment
access, withdrawal and data-rights routes.

## `UF-A10` Operator intervention

```text
ALERT/REQUEST -> STEP-UP/JIT ACCESS -> SCOPE/PURPOSE CONFIRMATION
              -> INSPECT REDACTED STATE -> SIMULATE INTERVENTION
              -> SECOND REVIEW where required -> EXECUTE
              -> VERIFY -> REVOKE ACCESS -> REVIEW EVIDENCE
```

Use for workflow intervention, incident response, tenant operations and
break-glass. View-as is visibly distinct from impersonation and cannot approve
on behalf of a customer participant.

## Composition rules

- Flow archetypes add presentation and continuity stages; workflow archetypes
  remain authoritative for execution.
- A concrete flow may compose archetypes, such as `UF-A3 + UF-A4 + UF-A6` for
  medical leave with evidence review and downstream repair.
- A stage may be system-only in execution while still requiring a truthful
  participant-facing state.
- Removing simulation, confirmation, tracking, correction or recovery requires
  an explicit risk/effect rationale.
- Child BusinessIntents may have their own flows and return to the parent flow
  through stable intent relationships.
