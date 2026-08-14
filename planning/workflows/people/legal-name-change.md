# Legal Name Change

## Identity and scope

```text
workflow_id: people.legal_name.change/v1
legacy_intent: employee.legal_name.change
target_intent: hcmnext.people.change_legal_name
kernel_family: ChangeRequest
subject: Person; affected worker/employments
initiator: person self-service; authorized HR proxy
state: EXTRACTED + EXPLORED
```

The authoritative mutation is a new legal-name revision. Display name, preferred
name, username, email address, identity documents, payroll/tax registrations and
badges are separate effects governed by their own authority and policy.

## Features and dependencies

Required: self-service/proxy, evidence collection, sensitive artifact handling,
effective dating, approval, jurisdiction-aware validation, exact proposal
binding, multi-stream impact planning, document processing, external effects,
reconciliation and repair.

Dependencies: Person/Identity Resolution, People, Legal/Jurisdiction, AuthZ,
Privacy/DLP, Forms/Human Work, Document Processing, malware scanning, Records,
Source Authority, TransactionPlan/commit coordinator, Payroll/IAM/Benefits
connectors as applicable, provenance and reconciliation.

## Steps

```text
1 resolve canonical person and all affected relationships/authorities
2 collect proposed structured name, reason, effective date and jurisdiction
3 determine evidence obligations without exposing unnecessary document content
4 collect/upload evidence; scan, classify, validate and retain artifact reference
5 validate script/Unicode, components, effective range and jurisdiction rules
6 simulate legal-name write and downstream identity/payroll/document impacts
7 conflict-check pending hire, identity merge, payroll close and other name changes
8 resolve HR/legal reviewer and bind decision to proposal + evidence hashes
9 wait until effective date or external verification signal
10 revalidate evidence validity, identity, authority, approvals and downstream cutoffs
11 atomically append legal-name revision, ledger, projection and ordered outbox
12 dispatch HRIS/payroll/IAM/benefits/document effects according to authority
13 observe each mandatory authority; reconcile values and identifiers
14 communicate completion through a safe channel; close or repair
```

## Data and candidate properties

```text
StructuredPersonName
  given_names[], family_names[], middle_names[], prefixes[], suffixes[]
  full_name_local, full_name_latin?
  script, locale?, ordering_rule, normalization_profile

LegalNameChangeRequest
  person_ref, affected_relationship_refs[]
  proposed_name, effective_range
  reason_code, reason_detail?
  jurisdiction_context_ref
  evidence_artifact_refs[]
  preferred/display-name_followup_policy
```

Evidence properties include document type, issuing authority, issue/expiry dates,
subject-match result, malware status, classification, legal hold and retention.
Runtime properties include affected-authority set, payroll cutoff, proposal digest,
decision bindings, expected stream heads, effect order and observations.

Legacy gaps to reject: first/middle/last is not globally sufficient; evidence is
not a raw JSON blob; an AI review cannot decide legal sufficiency; `/person/displayName`
does not change automatically without an explicit policy-produced write.
