# Recruiting, Onboarding and Offboarding Workflow Catalog

Rows use the complete [workflow archetypes](../_shared/workflow-archetypes.md).
`ATS`, background-check, signing, HRIS, payroll, IAM, benefits, ITSM/MDM and
learning providers are Connectivity dependencies, never embedded workflow logic.

| Intent                      | Arch. | Primary dependencies/data          | Required step delta                                                   |
| --------------------------- | ----- | ---------------------------------- | --------------------------------------------------------------------- |
| CreateRequisition           | A1    | Pos/Headcount, Job, Org, Budget    | bind approved capacity or record exception                            |
| ApproveRequisition          | A7    | Finance/HR approvals               | distinguish recruiting authorization from position authority          |
| ModifyRequisition           | A2    | ATS, Pos, Job, Budget              | reapproval on material scope/pay/location changes                     |
| CloseRequisition            | A3    | ATS, Applications                  | disposition active applications/postings and release capacity         |
| PublishJobPosting           | A1    | Requisition, Legal, Messaging      | localized/transparency/accessibility review and channel evidence      |
| UnpublishJobPosting         | A3    | Posting channels                   | preserve publication history; observe every channel removal           |
| CreateCandidate             | A1    | IdR, Privacy/Consent               | Candidate role linked to Person only under matching policy            |
| SubmitApplication           | A1    | Candidate, Requisition, Forms      | notice/consent, answers, attachments and source provenance            |
| ScreenCandidate             | A9    | Criteria, Decision Rights, Privacy | frozen criterion version, accommodations, explanation/contest path    |
| AdvanceCandidate            | A2    | Application stage model            | human/authorized decision evidence and notifications                  |
| RejectCandidate             | A3    | Decision Rights, Messaging, Legal  | reason compartment, adverse-action/retention obligations              |
| ScheduleInterview           | A2    | Calendar, Messaging, Accessibility | participants, timezone, accommodations, reschedule/cancel             |
| RecordInterviewFeedback     | A1    | Forms, Privacy                     | structured criterion evidence, authorship, late-edit/correction rules |
| RunBackgroundCheck          | A1    | Screening connector, Legal         | consent, permitted scope, provider operation and restricted result    |
| EvaluateBackgroundCheck     | A9    | Legal/Decision Rights              | result visibility, dispute/pre-adverse/adverse process as applicable  |
| SelectCandidate             | A2    | Applications, Decision Rights      | comparative evidence and SoD; no auto-selection by agent              |
| CreateOffer                 | A1/A2 | Comp, Position, Budget, Docs       | exact components/conditions/start date and expiry                     |
| ApproveOffer                | A2    | HR/Comp/Finance approvals          | bind exact offer digest; material revision invalidates                |
| SendOffer                   | A1    | Docs/E-sign, Messaging             | verified recipient, delivery/signature evidence and expiry            |
| AcceptOffer                 | A2    | E-sign/Identity Proofing           | signer identity, ceremony, conditions and timestamp                   |
| DeclineOffer                | A3    | Offer, Messaging                   | reason optional/protected; release reservation policy                 |
| RescindOffer                | A3    | Legal, Decision Rights             | authorization, notice and downstream cancellation/repair              |
| ConvertCandidateToWorker    | A8    | IdR, People                        | match/create Person, Worker/Employment proposals, lineage             |
| StartOnboarding             | A1/A8 | Accepted Offer, People             | create onboarding case/tasks and readiness dimensions                 |
| CollectOnboardingData       | A2    | Forms, Privacy/DLP                 | field purpose, compartment, validation and correction                 |
| CollectWorkAuthorization    | A2    | Identity Proofing, Docs, Legal     | evidence expiry, verifier, restricted access and deadlines            |
| GenerateEmploymentDocuments | A1    | Docs, Legal, Localization          | template/rule versions and exact render hashes                        |
| ProvisionWorkerAccess       | A8    | IAM, Access policy                 | identity/accounts/entitlements ordered by start/access time           |
| AssignEquipment             | A7    | Asset/ITSM                         | inventory reservation, shipment/custody/return evidence               |
| AssignOnboardingLearning    | A1    | Learning, Regulatory               | requirement source, due date, waiver and completion signal            |
| AssignBuddyOrMentor         | A1    | Relationships, Privacy             | eligibility, consent, duration and workload rules                     |
| VerifyOnboardingReadiness   | A9    | all onboarding dimensions          | PASS/FAIL/UNKNOWN/PARTIAL with blocking policy                        |
| CompleteOnboarding          | A3    | Onboarding case/tasks              | close only when mandatory dimensions/obligations allow                |
| InitiateTermination         | A1/A3 | People, Legal/ER                   | protected proposal and complete impact discovery                      |
| ApproveTermination          | A2    | HR/Legal/ER Decision Rights        | SoD, exact digest, step-up, authority-at-decision                     |
| ExecuteTermination          | A3/A8 | People, Payroll, Access            | irreversible boundary and atomic employment end/outbox                |
| StartOffboarding            | A8    | Access, Payroll, Benefits, Assets  | spawn ordered child obligations from committed termination            |
| RecoverEquipment            | A7/A3 | Asset/ITSM, Messaging              | custody, condition, reminders, loss/dispute route                     |
| RemoveWorkerAccess          | A3    | IAM/physical access                | P0 timing, expected entitlement set, observe/reconcile                |
| FinalizeOffboarding         | A9/A3 | all effect observations            | multidimensional closure and unresolved property ownership            |
| ReinstateWorker             | A4    | People, Payroll, Benefits, Access  | correction vs reinstatement, restoration and duplicate prevention     |
