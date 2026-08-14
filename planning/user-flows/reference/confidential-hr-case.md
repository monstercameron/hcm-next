# User Flow: Confidential HR Concern and Case

## Record

```text
flow_id: UF-010
primary: employee, former worker or permitted confidential reporter
other participants: representative, intake specialist, investigator, legal reviewer, respondent, appeal reviewer
root intents: ReportConcern / CreateCase
child intents: RequestInformation, OpenInvestigation, RecordFinding, CreateCorrectiveAction, OpenAppeal, ApplyLegalHold
archetype: UF-A5 + UF-A4
```

Success means a reporter can safely submit the minimum necessary concern, select
an allowed disclosure mode, receive safe status/communications, and obtain a
fair process without ordinary users inferring reporter identity, evidence,
participants or findings.

## Main flow

| Stage                 | Participant experience                                                                                                                                    | Semantic action and required result                          |
| --------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------ |
| Safe entry            | Explain emergency limits, anonymity modes, identity escrow, retaliation safeguards, representation and safe-device guidance before authentication choices | resolve jurisdiction/tenant-specific safe intake routes      |
| Disclosure choice     | Select known, pseudonymous, anonymous or escrowed mode only where permitted; explain communication/recovery consequences                                  | create confidential actor context without granting authority |
| Collect minimum facts | Progressive form separates required concern facts from optional identities/evidence; unsafe free text/attachments are quarantined                         | save protected draft/artifact refs with classification       |
| Review/submit         | Reporter sees exactly what will be shared with intake versus held in escrow; submit creates concern/case once                                             | immutable receipt and safe recovery/contact method           |
| Triage                | Reporter sees received and expected next contact, not internal assignment/risk labels; specialist sees scoped facts                                       | triage task, severity/routing and immediate safeguards       |
| Investigation         | Participant-specific requests and interviews use secure channels; investigator recusal/conflict and representation are visible where relevant             | case tasks/evidence chain with compartments                  |
| Status                | Safe states such as received, information needed, under review, concluded and appeal available avoid confirming protected participants or details         | relationship-specific case projection                        |
| Finding/disposition   | Each audience receives only its authorized determination/notice; opinion history is not overwritten                                                       | versioned finding/disposition and delivery evidence          |
| Appeal                | Independent route binds contested decision and permitted grounds/evidence; original remains historical                                                    | child appeal intent and separate reviewer assignment         |
| Closure               | Retention/hold, follow-up safeguards and communication availability remain explicit                                                                       | multidimensional case closure                                |

## Critical alternatives

- Shared/monitored device: offer rapid safe exit without pretending browser-history
  deletion; avoid sensitive notification previews.
- Anonymous duplicate/spam protection cannot silently deanonymize or correlate
  reporters across cases.
- Investigator is manager, subject, related party or prior decision maker:
  require recusal/reassignment with no disclosure to unauthorized audiences.
- Identity escrow reveal requires explicit authority, reason, scope and evidence;
  ordinary support and search cannot retrieve it.
- Respondent data-access request cannot expose reporter/confidential witness data;
  denial/redaction is independently explainable.
- Legal hold and deletion/retention conflict is shown to authorized records/legal
  roles without exposing the case broadly.
- Translation/interpreter preserves meaning, confidentiality and deadlines; an
  interpreter is not automatically a case participant.
- Message delivery failure switches to an approved safe channel without including
  concern details in fallback content.

## Initial TDD seeds

```text
TestConcernSafeEntryExplainsDisclosureModesBeforeSensitiveCollection
TestAnonymousConcernCannotBeCorrelatedByOrdinarySearchOrTelemetry
TestConcernReviewShowsExactEscrowVersusIntakeDisclosure
TestManagerAndSupportCannotInferConfidentialCaseExistence
TestInvestigatorConflictRequiresRecusalBeforeEvidenceAccess
TestCaseStatusProjectionDiffersByParticipantRelationship
TestIdentityEscrowRevealRequiresExplicitGovernedReceipt
TestAppealPreservesOriginalFindingAndAssignsIndependentReviewer
TestConfidentialFallbackMessageContainsNoConcernDetail
TestConcernFlowSupportsSafeExitKeyboardScreenReaderAndInterpreterRoutes
```
