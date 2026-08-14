# Emergency Contact Update

## Identity and scope

```text
workflow_id: people.emergency_contact.update/v1
legacy_intent: employee.emergency_contact.update
target_intent: hcmnext.people.change_emergency_contact
kernel_family: ChangeRequest
subject: Person
initiator: worker self-service; authorized HR proxy
state: EXTRACTED + EXPLORED
```

Emergency contacts are relationships to another person or supplied contact
record. They are not dependents, beneficiaries, authorized representatives or
privacy delegates unless separate governed relationships establish those roles.

## Features and dependencies

Self-service, effective dating, privacy compartmenting, ordered priorities,
optional endpoint verification, external synchronization, reconciliation and
repair are required. Approval is policy-dependent and should normally be replaced
by validation unless a high-risk proxy or protected-worker policy requires human
review.

Dependencies: People/Relationship model, Identity/AuthN, AuthZ, Privacy/DLP,
Source Authority, workflow runtime, rules/forms, commit coordinator, ledger,
records, integration journal and reconciliation.

## Steps

```text
1 resolve subject and acting authority
2 collect contact identity, relationship label, endpoints, language and priority
3 normalize phone/email; validate priority uniqueness and minimum reachable set
4 evaluate privacy, minimum-necessary display and proxy-submission policy
5 simulate add/change/remove plus effective interval and external effects
6 collect conditional approval or verification task
7 revalidate subject, proposal, duplicate contacts, priority and authority
8 atomically write relationship/contact revision + ledger + projection + outbox
9 synchronize each external authority in resource order
10 observe contact set, reconcile exact membership/order/values
11 close or create bounded repair
```

## Data and candidate properties

```text
EmergencyContactRelationship
  relationship_id, subject_person_ref
  contact_person_ref?                 // only after governed identity link
  supplied_name
  relationship_code, custom_label?
  phone_points[], email_points[]
  preferred_language?, accessibility_notes?
  priority, availability_notes?
  effective_range, source_authority, verification_state
  correction_of?, supersedes?, classification, retention_class
```

Input must distinguish `ADD`, `REVISE`, `REMOVE`, `REORDER` and `CORRECT`; a
missing `contactId` cannot ambiguously mean create or replace. Derived properties
include `is_primary`, `reachable`, external sync state and last verification.

Legacy gaps to reject: approval for every self-service update is not assumed;
`priority` must be an integer with collision rules; emergency relationship labels
do not establish legal dependency; provider acceptance is not reconciliation.
