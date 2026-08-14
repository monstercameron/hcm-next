# HR Service Delivery and Employee Relations Workflow Catalog

Rows use the complete [workflow archetypes](../_shared/workflow-archetypes.md).
Cases are not generic unrestricted folders: each case type declares participants,
compartments, evidence, deadlines, decision rights, retention and permissible
links to worker transactions.

| Intent                            | Arch. | Primary dependencies/data                      | Required step delta                                                   |
| --------------------------------- | ----- | ---------------------------------------------- | --------------------------------------------------------------------- |
| CreateHRRequest                   | A1    | Service Catalog, Forms, Knowledge              | classify request; resolve self-service answer vs case/workflow        |
| CreateCase                        | A1    | Case kernel, Privacy, Records                  | case type/version, participants, compartment and SLA                  |
| AssignCase                        | A2/A7 | Human Work queues, AuthZ                       | skills/scope/capacity routing and claim lease                         |
| EscalateCase                      | A2    | SLA/Obligation tracking                        | reason, target queue, visibility and notification                     |
| AddCaseEvidence                   | A1    | Documents, malware/DLP                         | provenance, classification, access, retention/hold                    |
| AddConfidentialCaseNote           | A1    | restricted compartment                         | author, purpose, no broad search/agent exposure                       |
| ResolveCase                       | A2    | Decision/Outcome model                         | resolution type, evidence, linked transaction intents                 |
| CloseCase                         | A3    | Obligations/Records                            | completion/retention/hold and reopen policy                           |
| ReopenCase                        | A4    | Case authority                                 | reason, prior resolution relationship and SLA reset policy            |
| RequestEmploymentVerification     | A1/A6 | People, Docs, Consent/DLP                      | authorized fields, as-of date, recipient and delivery evidence        |
| AnswerPolicyQuestion              | A6/A9 | Knowledge/RAG, Policy versions                 | citations, freshness, jurisdiction and human escalation               |
| EscalateToHumanHR                 | A1    | Human Work                                     | transfer conversation/context under minimum necessary rule            |
| ReportConcern                     | A1    | Confidential intake, anonymous identity option | protected reporter/contact/evidence and anti-retaliation controls     |
| OpenInvestigation                 | A1    | ER Case, Legal, Records                        | scope/allegations/decision rights/hold and compartment                |
| AssignInvestigator                | A2/A7 | Human Work, Conflict checks                    | independence, conflicts of interest and delegated authority           |
| CollectInvestigationEvidence      | A1    | Documents/Interviews, Legal Hold               | chain of custody, authenticity, minimization and access               |
| InterviewInvestigationParticipant | A1    | Scheduling, Forms, Messaging                   | notice/consent/representation, structured record and correction       |
| RecordInvestigationFinding        | A2    | Decision Rights, Evidence                      | allegation-level result, standard, rationale, dissent/review          |
| TakeDisciplinaryAction            | A8    | People, Legal/ER, Docs                         | separate immutable proposal/approval and employment effect            |
| CreatePerformanceImprovementPlan  | A1/A8 | Performance, Docs, Messaging                   | expectations, measures, support, dates, acknowledgement, reviews      |
| ResolveGrievance                  | A2/A8 | CBA/Policy, Decision Rights                    | stages, representation, remedy and appeal                             |
| ApplyLegalHold                    | A1/A8 | Records, Custodians, Data inventory            | scope/query, systems/copies, notice, acknowledgement and verification |
| ReleaseLegalHold                  | A3    | Records/Legal                                  | authority, remaining holds and resumed disposition calculation        |
