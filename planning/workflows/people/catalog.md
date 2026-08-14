# People and Employment Workflow Catalog

Every row uses the complete steps of its [archetype](../_shared/workflow-archetypes.md)
plus the explicit delta below. `Ppl` means People/Employment, `IdR` Identity
Resolution, `Org` Organization/Relationships, `Pos` Position, `Ref` Master Data,
`Reg` Legal/Regulatory, `Conn` Integration, and `Rec` Reconciliation/Repair.

| Intent                         | Arch. | Unique dependencies                    | Primary data/writes                      | Required step delta                                                           |
| ------------------------------ | ----- | -------------------------------------- | ---------------------------------------- | ----------------------------------------------------------------------------- |
| CreatePerson                   | A1    | IdR, Privacy, Records                  | identity claims -> Person                | possible-match/no-match/human-resolution branches                             |
| ResolvePersonIdentity          | A9    | IdR, DLP                               | claims, candidates, match evidence       | never auto-merge above uncertainty policy                                     |
| MergePersonRecords             | A5    | IdR, all referencing domains           | survivor/loser links, redirects          | simulate affected graph; dual review; reversible separation plan              |
| SeparatePersonRecords          | A5    | IdR, all referencing domains           | split plan, restored links               | reconstruct pre-merge ownership and repair every reference                    |
| CreateWorker                   | A1    | Ppl, IdR, Ref                          | Person -> Worker relationship            | prove person link; preserve candidate/contractor roles                        |
| CreateEmployment               | A1    | Ppl, Reg, Org, Ref                     | Employment, legal entity, type           | classification/jurisdiction/contract obligations                              |
| ModifyEmployment               | A2    | Ppl, Reg, Payroll/Benefits conditional | employment revision                      | calculate downstream eligibility/pay/access impact                            |
| SuspendEmployment              | A3    | Ppl, Reg, Payroll, Access              | suspension interval/status               | define access/pay/benefit effects and restoration criteria                    |
| ReactivateEmployment           | A4    | Ppl, Access, Payroll/Benefits          | reactivation revision                    | re-evaluate authority/eligibility before reprovisioning                       |
| EndEmployment                  | A3    | lifecycle/offboarding dependencies     | employment end                           | invoke termination/offboarding obligations, not simple status patch           |
| ReinstateEmployment            | A4    | Ppl, Payroll, Benefits, Access         | correction/restoration facts             | distinguish erroneous termination from new employment                         |
| RehireWorker                   | A4    | Ppl, IdR, Recruiting, Payroll          | new Employment/Assignment                | reuse Person/Worker; allocate new relationship identifiers as policy dictates |
| ConvertWorkerType              | A8    | Ppl, Reg, Payroll, Benefits, Access    | end/create relationship types            | classification analysis; contract/pay/tax/access migration                    |
| ChangeEmploymentType           | A2    | Ppl, Reg, Payroll, Benefits            | employment type revision                 | entitlement, hours, overtime and benefit simulation                           |
| ChangeWorkerStatus             | A2    | Ppl, policy registry                   | governed lifecycle status                | reject status values that bypass domain-specific workflows                    |
| ChangeLegalName                | A2    | IdR, Docs, Reg, Conn, Rec              | LegalNameRevision                        | evidence compartment and multi-authority observation                          |
| ChangePreferredName            | A2    | Ppl, IAM/Comms conditional             | PreferredNameRevision                    | keep legal name unchanged; safety/privacy display policy                      |
| ChangePersonalDetails          | A2    | Ppl, Privacy, Reg conditional          | scoped demographic revisions             | field-specific authority/purpose; no catch-all blob                           |
| ChangeAddress                  | A2    | Ppl, Ref, Reg conditional              | AddressRevision                          | normalization; impact analysis does not auto-change tax/work residence        |
| ChangeContactInformation       | A2    | Ppl, Messaging, Conn                   | ContactPoint revisions                   | verification and endpoint-purpose eligibility                                 |
| ChangeEmergencyContact         | A2    | Ppl, Privacy, Conn                     | contact relationship revisions           | separate dependent/beneficiary/legal roles                                    |
| ChangeWorkLocation             | A2/A8 | Ppl, Org, Reg, Payroll, Time, Access   | assignment location revision             | jurisdiction/tax/schedule/access/benefit impact graph                         |
| ChangeRemoteWorkArrangement    | A2    | Ppl, Reg, Time, Tax, Security          | arrangement terms/location set           | physical-work evidence and permanent-establishment review                     |
| ChangeLegalEntity              | A8    | Ppl, Reg, Payroll, Benefits, Docs      | end/create employment or entity revision | determine transfer vs termination/rehire; cross-company obligations           |
| ChangeOrganization             | A2    | Ppl, Org, Pos, Access                  | assignment organization revision         | graph/scope/access and source-authority revalidation                          |
| ChangeDepartment               | A2    | Ppl, Org, Finance                      | department assignment                    | canonical department/cost-center distinction                                  |
| ChangeCostCenter               | A2    | Ppl, Finance, Payroll                  | cost allocation revision                 | budget/GL/payroll crosswalk and observation                                   |
| ChangeManager                  | A2    | Ppl, Org, Access                       | manager edge replacement                 | cycle/cardinality validation and relationship-derived access                  |
| AddSecondaryManager            | A1    | Ppl, Org, AuthZ                        | secondary edge                           | purpose/type/precedence; cannot become primary implicitly                     |
| RemoveSecondaryManager         | A3    | Ppl, Org, AuthZ                        | end secondary edge                       | recalculate delegated approvals/access                                        |
| AddEmploymentRelationship      | A1    | Ppl, Reg, Payroll/Benefits             | simultaneous Employment                  | primary relationship rules and cross-employment conflicts                     |
| CorrectWorkerData              | A5    | owning domain, provenance              | corrective fact                          | route to field owner; reject generic unrestricted patch                       |
| RetroactivelyCorrectEmployment | A5    | Ppl, Payroll, Benefits, Reg            | corrective employment revision           | calculate historical downstream/reporting impact                              |
| FutureDateWorkerChange         | A2    | owning domains, Workflow timers        | scheduled proposal/execution binding     | persist invalidators/reference-update policy                                  |
| CancelFutureWorkerChange       | A3    | owning domains, Conflict Registry      | cancellation/supersession record         | assess dependent proposals/reservations and approval invalidation             |
| ExplainWorkerState             | A6    | Ppl, Provenance                        | explanation artifact                     | distinguish domain fact, external observation and claim                       |
| ReconstructWorkerState         | A6    | Ppl, Ledger/Provenance                 | bitemporal state projection              | pin rule/schema/source versions and disclose gaps                             |
| CompareWorkerState             | A6    | Ppl, Provenance                        | field-level temporal diff                | authorize each side/date; explain correction effects                          |
| ExportWorkerRecord             | A6    | Privacy/DLP, Records, Egress           | export package/evidence                  | purpose, field filters, watermark, delivery and retention                     |
