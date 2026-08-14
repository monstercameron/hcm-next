# Contact Information Update

## Identity and scope

```text
workflow_id: people.contact_information.update/v1
legacy_intent: employee.contact_info.update
target_intent: hcmnext.people.change_contact_information
kernel_family: ChangeRequest
subject: Person + Worker context
initiator: worker self-service; authorized HR proxy
state: EXTRACTED + EXPLORED
```

The workflow changes personal email, mobile phone and home address. Work contact
facts, tax residence, emergency contacts and preferred communication endpoints
are separate facts and MUST NOT change implicitly.

## Features and dependencies

| Feature               | Requirement                                                                                                                     |
| --------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| Self-service          | Required; proxy submission records subject and acting principal separately                                                      |
| Effective dating      | Required for address and endpoint validity; local date semantics                                                                |
| Approval              | Policy-dependent; low-risk verified endpoint changes may be no-approval                                                         |
| Verification          | Email/phone proof may be required before endpoint becomes verified                                                              |
| Jurisdiction          | Address normalization may trigger tax, benefit, work-location or privacy impact analysis but does not itself change those facts |
| External effect       | Conditional HRIS/master-data sync through Integration Plane                                                                     |
| Reconciliation/repair | Required when an external authority owns any changed field                                                                      |

Required dependencies: Identity/AuthN, People, Source Authority, AuthZ, Privacy/DLP,
address/reference data, workflow runtime, TransactionPlan/commit coordinator,
ledger, integration operation journal, provenance, records retention and
reconciliation. Conditional dependencies: geocoding/address validation,
Messaging for endpoint verification, Regulatory impact analysis.

## Steps

| #   | Primitive               | Reads                                                                         | Writes/evidence                                         | Failure route                                  |
| --- | ----------------------- | ----------------------------------------------------------------------------- | ------------------------------------------------------- | ---------------------------------------------- |
| 1   | CAPABILITY              | person/worker link, current contact revisions, source authority               | subject-resolution evidence                             | unresolved identity -> human resolution        |
| 2   | TASK                    | none beyond minimum current display                                           | typed proposed contact data and reason                  | incomplete/invalid -> remain in collection     |
| 3   | RULE                    | country/address formats, endpoint normalization, duplicate verified endpoints | normalized proposal, validation findings                | unknown reference data -> manual review        |
| 4   | CAPABILITY              | AuthZ, privacy purpose, classification, field authority                       | governance snapshot                                     | deny/unknown -> reject or block                |
| 5   | CAPABILITY              | current/future contact intervals, pending changes                             | simulation, write set, conflicts, downstream impact     | conflict -> rebase/supersede                   |
| 6   | APPROVAL/TASK           | policy and verification requirement                                           | exact proposal-bound decision or verification receipt   | reject/expire -> return/reject                 |
| 7   | WAIT                    | effective date if future                                                      | durable timer                                           | calendar/reference change -> configured review |
| 8   | CAPABILITY              | current version, authority, verification, conflicts                           | revalidation receipt                                    | stale -> revise/reapprove                      |
| 9   | CHECKPOINT + CAPABILITY | expected stream heads                                                         | contact revision, ledger, projection, outbox atomically | abort without external send                    |
| 10  | CAPABILITY              | ordered outbox operation                                                      | external sync journal                                   | retry/dead-letter -> repair                    |
| 11  | OBSERVE                 | authoritative external contact state                                          | observation + reconciliation result                     | mismatch/unknown -> RepairPlan                 |
| 12  | END                     | completion dimensions                                                         | closure or repair linkage                               | close only eligible dimensions                 |

## Data and candidate properties

Input properties:

```text
person_ref, worker_ref?, acting_principal_ref
personal_email?, mobile_phone?, home_address?
effective_range, reason_code, reason_detail?, verification_method?
```

`home_address` candidates:

```text
line_1, line_2?, locality, administrative_area, postal_code
country_code, address_type, normalized_address_ref?, validation_status
residency_assertion = false by default
```

Canonical properties: `ContactPointRevision`, `AddressRevision`, verified state,
valid/effective range, source authority, recorded-at, correction/supersession
links, classification and disclosure policy. Runtime properties include proposal
digest, validation versions, expected stream heads, idempotency/effect keys,
external ordering key, authority fence and reconciliation watermark.

Legacy gaps to reject: JSON `number` is irrelevant here, but nullable strings need
presence semantics; queued external sync is not completion; changing an address
must not silently change work location or tax residence.
