# BusinessIntent Workflow Design Registry

## Purpose and truth boundary

This registry gives every reproducible repository-known material HCM action a
high-level execution design. It is the design bridge between the
[BusinessIntent catalog](../specs/business-intent-catalog.md), the
[workflow archetypes](_shared/workflow-archetypes.md), domain catalogs and the
production backlog.

The original numbered 530-candidate manifest is not checked in. Therefore this
document must not claim `530/530 VERIFIED`. It covers:

1. every intent named by the six current HR workflow catalogs;
2. every Phase 1 SchemaFlux intent definition;
3. every additional intent family named in the accepted planning vocabulary;
4. a mandatory `UNBOUND_SOURCE` result for any numbered candidate that cannot be
   enumerated until `MODEL-008` checks in the immutable source manifest.

Once that manifest exists, generation must produce one `WorkflowDesignRecord`
per accepted definition and fail on any absent disposition.

## Complete high-level design contract

An intent is designed at high level only when its record resolves all fields:

```text
WorkflowDesignRecord
  intent_type_id / accepted candidate identity
  display_name and aliases
  kernel_family
  execution_disposition = DURABLE_WORKFLOW | DIRECT_CAPABILITY | CHILD_ONLY
  archetype
  domain_profile
  input and trusted-context boundary
  authoritative reads and snapshot/freshness policy
  reusable engines and domain capabilities
  human work, decision rights and evidence compartments
  planned authoritative writes
  external effects and irreversible boundary
  waits, signals, timers and deadlines
  revalidation invalidators
  observation and reconciliation policy
  cancellation, correction, supersession and repair policy
  multidimensional completion policy
  reference or conformance scenario
  maturity = CANDIDATE_MAPPED | CATALOG_MAPPED | CONTRACTED | IMPLEMENTED | VERIFIED
```

An archetype supplies the complete phase sequence. The domain profile supplies
the default data, engines, controls and effects. The per-intent mapping supplies
the unique semantic delta. No layer may be omitted silently.

## Domain profiles

| Profile     | Authoritative reads / engines                                                                          | Writes and effects                                                                     | Required closure evidence                                                |
| ----------- | ------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------- | ------------------------------------------------------------------------ |
| `PPL`       | Person/Worker/Employment/Assignment, Identity Resolution, Organization, Authority, temporal/provenance | effective-dated people facts; payroll/benefits/access/messaging effects where declared | stream commit plus each declared downstream reconciliation               |
| `ORG`       | organization/job/position graph, capacity, population, budget, rules                                   | graph/job/position/headcount revisions and scoped child moves                          | graph invariants, capacity and every affected child outcome              |
| `REW`       | compensation package/band/budget, decimal/rules/FX/eligibility                                         | compensation/budget facts; payroll/accounting effects                                  | exact-money trace, approval digest and payroll/budget observation        |
| `PAY`       | frozen payroll population/input/tax/time/benefit/balance snapshots                                     | run/results/payments/GL/filings; bank and government effects                           | run integrity, settlement, accounting and filing reconciliation          |
| `REG`       | jurisdiction/legal context/rule pack/deadline/evidence                                                 | obligation, calculation or filing package/submission                                   | cited rule release and acknowledged/reconciled obligation                |
| `BEN`       | plan/program/cycle/population/eligibility/dependents/balances                                          | election/coverage/deduction revisions; carrier/payroll effects                         | carrier coverage and payroll deduction reconciliation                    |
| `WFM`       | schedule/time/demand/availability/qualification/balance/rules                                          | punches/timecards/shifts/availability/balances; payroll effects                        | trusted observation, approval/attestation and payroll reconciliation     |
| `LEAVE`     | employment/service/schedule/legal/program/eligibility/balance/evidence                                 | leave/availability/balance facts; payroll/benefit/schedule/access effects              | determination/notice, return obligations and every external comparison   |
| `ATS`       | requisition/job/position/candidate/application/consent                                                 | candidacy/stage/offer revisions; channels/screeners/calendar/e-sign effects            | stage/decision evidence and accepted-offer-to-hire linkage               |
| `LIFE`      | accepted offer or termination proposal plus readiness requirements                                     | onboarding/offboarding plan and bounded child intents                                  | mandatory requirements plus per-child outcome; no aggregate masking      |
| `ACCESS`    | workforce identity/lifecycle/expected entitlement graph/risk                                           | expected access/account/device/badge facts; IAM/MDM/physical effects                   | actual access observation and drift-free or explicitly degraded closure  |
| `TALENT`    | cycles/goals/reviews/skills/learning/career/succession populations                                     | review/rating/goal/credential/plan revisions and learning-provider effects             | provenance, contest/correction policy and provider reconciliation        |
| `EXP`       | audience/population/contact/preferences/content/localization                                           | message/survey/recognition/community state and channel effects                         | per-recipient delivery/acknowledgement or privacy-safe aggregate         |
| `CASE`      | case type/participants/compartments/SLA/evidence/holds                                                 | case revisions, human work and separately governed remedy intents                      | disposition, obligations, appeal/reopen and evidence package             |
| `MOB`       | home/host employment/location/work authorization/tax/payroll/privacy                                   | assignment/relocation/visa milestones and provider effects                             | authorization, tax/payroll/vendor and return reconciliation              |
| `SAFE`      | incident/injury/workplace/evidence/reportability/rules                                                 | incident/claim/restriction/corrective action/filing/payment state                      | protected evidence, filing/payment and work-readiness reconciliation     |
| `PRIV`      | data subject/purpose/authority/inventory/retention/holds                                               | request/consent/notice/hold/disposition facts and notification effects                 | scoped search, reviewer evidence, disposition/notification verification  |
| `DOC`       | artifact/template/form/classification/signature/retention                                              | immutable artifact/form/signature revisions and delivery effects                       | scan/classification/signature/delivery/retention evidence                |
| `INTG`      | connector/connection/schema/mapping/credential/authority                                               | operation journal/attempt/observation; external API/file/webhook effects               | fresh observation and explicit reconciliation result                     |
| `DATAOPS`   | imports/config/reference/provenance/transactions/workflows                                             | staged/admin corrections through governed transactions                                 | simulation/approval plus ledger/projection/external reconciliation       |
| `SEC`       | principal/identity/session/scope/policy/risk/secrets                                                   | security decision/access/key/incident facts and provider effects                       | decision receipt, expiry/revocation and audit/review evidence            |
| `ANALYTICS` | governed semantic model/population/metrics/watermarks                                                  | result/evidence only; proposed action is a separate intent                             | reproducible result, uncertainty, DLP and access evidence                |
| `AGENT`     | model/agent/tool/prompt/policy/context/evaluation versions                                             | proposal/analysis only unless a separately authorized capability executes              | trace, citations, tool receipts, human escalation and outcome evaluation |
| `OPS`       | expected state/observations/telemetry/incidents/repair plans                                           | governed intervention/repair/recovery/rollout facts                                    | verified postcondition and incident/repair evidence                      |
| `COMM`      | tenant/account/product/contract/usage/rating                                                           | entitlement/contract/usage/invoice/credit/refund facts and payment effects             | rated-usage evidence, financial reconciliation and dispute state         |
| `TENANT`    | tenant/cell/config/entitlement/quota/keys/recovery state                                               | tenant lifecycle/config/placement/credential operations                                | health, isolation, recovery and customer-visible outcome evidence        |
| `TRIGGER`   | schedule/event/subscription/dedup/admission policy                                                     | TriggerFiring plus typed target BusinessIntent                                         | firing and target outcomes remain separately provable                    |

## Workflow mappings already modeled in domain catalogs

The following catalogs are normative at `CATALOG_MAPPED` discovery depth. Every
row inherits its complete archetype and the named domain profile above:

- [People and Employment](people/catalog.md): `PPL`.
- [Organization, Job, Position and Headcount](workforce/catalog.md): `ORG`.
- [Compensation and Rewards](rewards/catalog.md): `REW`.
- [Recruiting, Onboarding and Offboarding](lifecycle/catalog.md): `ATS`/`LIFE`.
- [Benefits, Time, Leave and Accommodation](leave/catalog.md): `BEN`/`WFM`/`LEAVE`.
- [HR Service Delivery and Employee Relations](hr-service/catalog.md): `CASE`.

Those 198 mappings are not repeated here. The following compact mappings close
the remaining repository-known vocabulary. Every comma-separated name is an
individual `WorkflowDesignRecord` inheriting the recipe and profile; it is not a
single combined intent.

## Payroll (`PAY`)

| Recipe    | Individual intents                                                                                                                                                                                                                          | Unique semantic delta                                                                                                |
| --------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| A1/A2     | EnrollWorkerInPayroll, RemoveWorkerFromPayroll, ChangePayGroup, AddDeduction, ChangeDeduction, EndDeduction                                                                                                                                 | effective-dated worker payroll/input assignment; revalidate cutoffs and finalized-run impact                         |
| A10       | OpenPayrollRun, FreezePayrollPopulation, CollectPayrollInputs, ValidatePayrollInputs, CalculatePayroll, PreviewPayroll, RecalculatePayroll, RunTrialPayroll, ApprovePayroll, LockPayroll, FinalizePayroll, ReleasePayroll, ReopenPayrollRun | immutable run/cycle phase, population/input manifest and exception gates; only release crosses irreversible boundary |
| A18       | ResolvePayrollException, ReconcilePayroll, CorrectPayroll, CalculateRetroPay, IssueOffCyclePay, CalculateFinalPay, ReversePayrollPayment, ResolveReturnedPayment                                                                            | create successor correction/off-cycle/payment intents; never rewrite finalized run or settled payment                |
| A7/A11    | FundPayroll, SubmitPayrollPayment, ObserveSettlement                                                                                                                                                                                        | fenced funding and payment instructions; provider acceptance differs from settlement                                 |
| D1/A16    | GeneratePayStatement, ExplainPaycheck                                                                                                                                                                                                       | field-authorized deterministic statement/explanation; generated artifact uses A16                                    |
| A1/A5     | ApplyGarnishment, ChangeGarnishment, ReleaseGarnishment                                                                                                                                                                                     | restricted legal order, priority/limits/balance and remittance lifecycle                                             |
| A11       | SubmitPayrollFiling, AmendPayrollFiling                                                                                                                                                                                                     | exact filing package, authority/signature, acknowledgement and amendment linkage                                     |
| A1/A5/A14 | GeneratePayrollJournal, PostPayrollGL, CorrectPayrollGL                                                                                                                                                                                     | balanced immutable journal, ERP posting observation and correction relationship                                      |

## Tax and regulatory (`REG`)

| Recipe  | Individual intents                                                                                                                                                                                                                                          | Unique semantic delta                                                                                         |
| ------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| D1      | ResolveJurisdiction, ResolveLegalContext, EvaluateLegalObligation, EvaluateWorkerClassification, EvaluateWageCompliance, EvaluateOvertime, EvaluateBreakRequirement, EvaluateStatutoryLeave, CalculateTax, CalculateSocialContribution, DetermineTaxability | pinned fact/rule releases, uncertainty and cited deterministic trace; mutation forbidden                      |
| A11     | GenerateGovernmentReport, ValidateGovernmentReport, ApproveFiling, SubmitFiling, ObserveFiling, CorrectFiling, AmendFiling, ReconcileFiling                                                                                                                 | immutable package lineage and irreversible submission boundary; correction/amendment never overwrite original |
| A17/A12 | TrackStatutoryDeadline, SatisfyObligation, WaiveObligation, EscalateOverdueObligation                                                                                                                                                                       | calendar-versioned obligation, permitted waiver authority and durable escalation                              |
| A19     | PublishRulePack, SimulateRulePack, AnalyzeRegulatoryChange, ActivateRegulatoryChange, QuarantineRulePack, RetireRulePack                                                                                                                                    | signed rule provenance, impact fixtures, four-eyes publication, canary/rollback and historical resolution     |

## Talent, performance, learning and skills (`TALENT`)

| Recipe    | Individual intents                                                                                                                                                                                                              | Unique semantic delta                                                                                     |
| --------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| A10       | CreatePerformanceCycle, OpenPerformanceCycle, AssignReviewers, Launch360Review, CreateCalibrationSession, SubmitCalibration, ResolveCalibrationConflict, FinalizeRating                                                         | frozen population/reviewer graph, calibrated evidence, contest and correction; opinions remain attributed |
| A1/A2/A5  | StartSelfReview, SubmitSelfReview, SubmitManagerReview, RequestPeerFeedback, SubmitPeerFeedback, CreateGoal, ModifyGoal, CloseGoal, RecordFeedback, CorrectRating                                                               | exact cycle/subject/reviewer binding, visibility windows and append-only correction                       |
| D1/A20    | CreateTalentAssessment, EvaluatePotential, EvaluatePromotionReadiness, NominateSuccessor, AssessSuccessor, CreateSuccessionPlan, CreateDevelopmentPlan, RecordCareerPreference, EvaluateInternalMobility, RecommendInternalRole | governed assessment/readiness evidence; recommendations create proposals, never mutate employment         |
| A12/A20   | CreatePerformanceImprovementPlan, ClosePerformanceImprovementPlan                                                                                                                                                               | expectations, support, checkpoints, acknowledgements and separately governed employment consequences      |
| A19       | CreateCourse, PublishCourseVersion, RetireCourse, CreateLearningPath                                                                                                                                                            | immutable content/prerequisite/credential definitions and publication lifecycle                           |
| A1/A2     | AssignLearning, EnrollLearning, WithdrawEnrollment, StartCourse, CompleteCourse, RecordAssessment, WaiveRequirement                                                                                                             | eligibility/prerequisites, due dates, provider signals, assessment/waiver evidence                        |
| A1/A5/A17 | IssueCertification, RecordExternalCertification, RenewCertification, ExpireCertification, RevokeCertification                                                                                                                   | credential evidence, validity, expiry trigger and correction/revocation semantics                         |
| A1/D1     | RecordSkill, AssessSkill, VerifySkillEvidence, IdentifySkillGap, RecommendTraining, DetermineLearningEligibility, ResolveCredentialEquivalence                                                                                  | versioned ontology/evidence and explainable proficiency/equivalence; recommendation is separate action    |
| A14       | ReconcileLMSCompletion                                                                                                                                                                                                          | expected assignment/completion versus provider observation and scoped repair                              |

## Workforce access and assets (`ACCESS`)

| Recipe    | Individual intents                                                                                            | Unique semantic delta                                                                                |
| --------- | ------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| A1/A15    | CreateWorkforceIdentity, LinkExternalIdentity, ProvisionAccount, ProvisionBadge                               | proofed identity link, expected account/badge and provider observation                               |
| A3/A4/A15 | DeprovisionAccount, DisableIdentity, ReactivateIdentity, RevokePhysicalAccess, RevokeBadge                    | urgency-aware revocation/restoration with no dependence on slow ordinary workflow for P0 termination |
| A15       | RequestAccess, ApproveAccess, GrantEntitlement, RevokeEntitlement, ModifyEntitlement, RecalculateEntitlements | entitlement graph, SoD/risk/step-up, exact approval and drift reconciliation                         |
| A10/A15   | ReviewAccess, CertifyAccess, RejectAccess, CreateAccessReviewCampaign, CloseAccessReviewCampaign              | frozen population/expected grants, per-item decision and no aggregate masking                        |
| A18/A15   | DetectAccessDrift, RepairAccessDrift, InvestigateSuspiciousAccess                                             | expected-versus-observed graph, containment without accusation and verified repair                   |
| A7/A15    | AssignDevice, RecoverDevice, LockDevice, WipeDevice, GrantPhysicalAccess                                      | custody/reservation, authorization, destructive boundary and MDM/badge observations                  |

## Experience, communications, surveys and recognition (`EXP`)

| Recipe    | Individual intents                                                                                                                                                                                                                       | Unique semantic delta                                                                                |
| --------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| A13       | SendWorkerMessage, SendManagerMessage, SendApprovalReminder, SendTaskReminder, SendAnnouncement, TargetAnnouncement, SendLegalNotice, SendBulkCommunication, ScheduleCommunication, SendEmergencyCommunication, CreateSecureInboxMessage | authorized audience/content, localization/DLP, channel/fallback and per-recipient outcome            |
| A2/A13    | RequestAcknowledgement, RecordAcknowledgement, OpenConversation, ReplyToConversation, EscalateConversation, CancelScheduledCommunication, RetryFailedCommunication, TriggerCommunicationFallback, ResolveDeliveryFailure                 | exact content revision, identity/time evidence and transport-versus-business outcome separation      |
| A19/A10   | CreateSurvey, PublishSurvey, LaunchSurvey, SelectSurveyPopulation, SampleSurveyPopulation, CloseSurvey, ReopenSurvey, CreatePulseSurvey, LaunchEngagementCampaign                                                                        | population/sample freeze, confidential actor mode, minimum cohort and campaign phases                |
| A1/D1/A13 | SubmitSurveyResponse, SubmitAnonymousResponse, CalculateSurveyMetrics, ApplyCohortSuppression, PublishSurveyResults, GenerateFollowUpAction, EscalateHighRiskFeedback                                                                    | prevent re-identification; high-risk signal creates governed case/action rather than deanonymization |
| A1/A7/A10 | GrantRecognition, NominateAward, ApproveAward, IssueAward, CreateRecognitionProgram                                                                                                                                                      | program eligibility/funding and auditable award outcome                                              |
| A1/A3/A13 | CreateCommunity, JoinCommunity, LeaveCommunity, ModerateCommunity, PublishCommunityAnnouncement, CreateMentorshipMatch, EndMentorship                                                                                                    | consent, membership/moderation policy, duration and protected communication scope                    |

## Privacy and records (`PRIV`)

| Recipe      | Individual intents                                                                                                                      | Unique semantic delta                                                                                                                 |
| ----------- | --------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| A12/A6      | RequestDataAccess, RequestDataCorrection, RequestDataDeletion, RequestDataPortability, RequestProcessingRestriction, ObjectToProcessing | identity proofing, scoped inventory search, exemptions, deadlines and evidence package; correction/deletion spawn owned child intents |
| A1/A3       | GrantConsent, WithdrawConsent, PresentPrivacyNotice, AcknowledgePrivacyNotice                                                           | exact purpose/notice version, channel, effective time and downstream invalidation                                                     |
| D1/A12      | EvaluateProcessingAuthority, EvaluateCrossBorderTransfer, PerformDPIA, RegisterProcessingActivity                                       | cited authority/risk/transfer result and reviewable assessment; no silent approval                                                    |
| A10/A16     | ApplyRetentionSchedule, SimulateDisposition, DestroyRecord, AnonymizeRecord, ApplyLegalHold, ReleaseLegalHold                           | frozen inventory, holds/retention precedence, destructive checkpoint and verification                                                 |
| A12/A13/A11 | CreateDataBreachCase, AssessDataBreach, NotifyDataSubject, NotifyRegulator, GeneratePrivacyEvidencePackage                              | incident clock, jurisdiction decisions, minimum disclosure and acknowledged notifications                                             |

## Documents, forms and signatures (`DOC`)

| Recipe  | Individual intents                                                                                                                                                                                                                                                 | Unique semantic delta                                                                              |
| ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------- |
| A16     | UploadDocument, ScanDocument, ClassifyDocument, ExtractDocumentData, RedactDocument, GenerateDocument, RequestDocument, AcknowledgeDocument, CorrectDocumentMetadata, SupersedeDocument, ApplyRetention, ApplyHold, GenerateEvidencePackage, VerifyEvidencePackage | immutable artifact revisions, malware/DLP/classification, source/extraction confidence and custody |
| A19/A16 | PublishTemplate, RetireTemplate                                                                                                                                                                                                                                    | reviewed template/schema/localization versions and derivative invalidation                         |
| A16/A14 | RequestSignature, SignDocument, CountersignDocument, DeclineSignature, ExpireSignatureRequest                                                                                                                                                                      | signer identity/assurance, exact artifact hash, ceremony/provider observation and expiry           |

## Integration and event subscriptions (`INTG`)

| Recipe  | Individual intents                                                                                                                                                                                                                    | Unique semantic delta                                                                           |
| ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| A19/A14 | CreateConnectorDefinition, CreateConnectorConnection, ValidateConnection, ActivateConnection, SuspendConnection, QuarantineConnection, RevokeConnection, RotateConnectorCredential, RunConnectorHealthCheck                           | separate immutable connector definition from tenant connection/credential lifecycle             |
| D1/A19  | DiscoverSchema, DiffExternalSchema, CreateMapping, ValidateMapping, PublishMapping, RollbackMapping, CreateCrosswalk, UpdateCrosswalk                                                                                                 | typed transform/crosswalk lineage, compatibility and publication; discovery is observation only |
| A10/A14 | RunFullSync, RunIncrementalSync, RunTargetedSync, PauseSync, ResumeSync, CancelSync                                                                                                                                                   | frozen scope/watermark, resumable partitions and per-item reconciliation                        |
| A14/A18 | ReceiveWebhook, ReplayWebhook, SendExternalEffect, RedriveExternalEffect, ObserveExternalState, ReconcileExternalState, RepairExternalDrift                                                                                           | authenticated/deduplicated inbox or operation journal; replay/redrive never duplicates effect   |
| A19/A13 | CreateEventSubscription, AuthorizeSubscription, SetEventFilter, SetOrganizationScope, SetSchemaVersion, ActivateSubscription, PauseSubscription, ResumeSubscription, DisableSubscription, RotateWebhookSecret, TestSubscriberEndpoint | scope/schema/secret lifecycle and non-disclosing endpoint tests                                 |
| A13/A18 | DeliverEvent, RetryEvent, DeadLetterEvent, ReplayEvent, ReconcileDelivery                                                                                                                                                             | signed envelope, declared ordering, retry/dead-letter/replay and subscriber delivery evidence   |

## DataOps, configuration and extension administration (`DATAOPS`)

| Recipe       | Individual intents                                                                                                                                                                                                                                                                                                                                                                                                                 | Unique semantic delta                                                                                                                                                      |
| ------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| A10/A5       | StageImport, ProfileImport, MapImport, ValidateImport, SimulateImport, ApproveImport, CommitImport, ResumeImport, CorrectImport, ReconcileImport                                                                                                                                                                                                                                                                                   | staging is non-authoritative; commit uses bounded transactions; correction preserves source lineage                                                                        |
| D1/A18       | CompareSystems, CompareWorkerRecords, CompareConfigurations, ExplainDataHistory, ExplainDataLineage, InspectEffectiveDate, ResolveIdentityMatch, ResolveCrosswalk, ValidateReferenceData, ValidateOrgGraph, TestConnector, DiagnoseConnector, InspectWebhook, InspectIdempotency, InspectTransaction, InspectWorkflow, InspectRepair, ExportAuditData                                                                              | purpose-scoped read/diagnosis; any mutation becomes separate correction/repair intent                                                                                      |
| A5/A19       | CorrectReferenceData                                                                                                                                                                                                                                                                                                                                                                                                               | exact prior assertion binding, affected-reference analysis and non-duplication proof; connector redrive and webhook replay remain owned by their Integration records above |
| A19          | CreateConfigurationDraft, ValidateConfiguration, DiffConfiguration, AnalyzeConfigurationImpact, PackageConfiguration, ApproveConfiguration, PublishConfiguration, PromoteConfiguration, RollbackConfiguration, SupersedeConfiguration, RetireConfiguration, CloneConfiguration, ExportConfiguration, ImportConfiguration, ResolveDependency, QuarantineBadConfiguration, ScheduleEffectiveConfiguration, CancelFutureConfiguration | dependency/compatibility/impact simulation, exact approval and progressive activation                                                                                      |
| A19/D1       | CreateRule, CreateFormula, CreateDecisionTable, ValidateRule, TestRule, SimulateRule, ExplainRule, PublishRule, FutureDateRule, SupersedeRule, RetireRule, AnalyzeRuleImpact, ReevaluateAffectedIntents, GenerateRuleRegressionTests                                                                                                                                                                                               | one bounded expression substrate, golden vectors and scoped reevaluation                                                                                                   |
| A19/A10      | CreatePopulationDefinition, ModifyPopulationDefinition, ResolvePopulation, FreezePopulationSnapshot, ComparePopulationSnapshots, ExplainInclusion, ExplainExclusion, ValidatePopulation, SchedulePopulationRefresh, ReconcilePopulation, ExportPopulation, UsePopulationInBulkIntent, UsePopulationInReporting, UsePopulationInProgram, UsePopulationInWorkflow                                                                    | as-of/known-at/AuthZ resolution and immutable, non-leaking cohort snapshot                                                                                                 |
| A19/A20      | CreateProgram, ModifyProgram, PublishProgram, RetireProgram, DefineEligibility, DefinePopulation, DefineCycle, DefineFunding, EnrollParticipant, RemoveParticipant, CalculateProgramOutcome, CloseProgramCycle, CorrectProgramOutcome, AnalyzeProgramPerformance                                                                                                                                                                   | only after cross-domain conformance proves the shared Program abstraction                                                                                                  |
| A19/A15      | RegisterApplication, PublishApplicationVersion, RequestCapabilityScope, ApproveApplicationScope, InstallApplication, ConfigureApplication, IssueClientCredential, RotateClientCredential, RevokeApplication, UpgradeApplication, SuspendApplication, QuarantineApplication, CertifyApplication, CreateSandboxAppInstallation, SubscribeToEvents, RequestHigherQuota, PublishMarketplaceListing                                     | installation-scoped grants, security/data review, credentials, upgrades and quarantine                                                                                     |
| D1           | ListAvailableCapabilities, GenerateClientSDK, RunAPITest, InspectAPIUsage                                                                                                                                                                                                                                                                                                                                                          | governed discovery/generation/test/usage results with no production authority mutation                                                                                     |
| A19/A1/A2/A3 | DefineCustomObjectType, CreateCustomObject, ModifyCustomObject, RetireCustomObject, CreateCustomRelationship, EndCustomRelationship, VersionCustomSchema, MigrateCustomObjects, GenerateCustomCapabilities, CreateCustomWorkflow, SearchCustomObjects, ReportOnCustomObjects, ApplyCustomObjectAuthZ, ApplyCustomObjectRetention                                                                                                   | typed schema/relationship/effective-date/security/retention semantics; migration is resumable and reversible where possible                                                |

## Analytics, semantic intelligence and agents (`ANALYTICS`/`AGENT`)

| Recipe | Individual intents                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             | Unique semantic delta                                                                                                             |
| ------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------- |
| D1/A10 | AskWorkforceQuestion, RunReport, ScheduleReport, ExportReport, CreateMetric, CalculateMetric, ExplainMetric, CreateDashboard, AnalyzeHeadcount, AnalyzeTurnover, AnalyzeCompensation, AnalyzePayEquity, AnalyzeHiring, AnalyzeTime, AnalyzeOvertime, AnalyzeAbsence, AnalyzeBenefits, AnalyzePerformance, AnalyzeSkills, AnalyzeLearning, AnalyzeAccess, AnalyzeHRCases, AnalyzeWorkflowPerformance, AnalyzeDecisionOutcomes, AnalyzeDataQuality, AnalyzeConnectorReliability, AnalyzeProcessBottlenecks, PerformProcessMining | purpose/population/field scope, pinned semantic model/watermarks, reproducibility, suppression and no mutation                    |
| D1/A20 | ForecastHeadcount, ForecastTurnover, ForecastWorkforceDemand, ForecastLaborCost, GenerateHypothesis, EvaluateHypothesis, GeneratePrediction, EvaluatePrediction, RecommendActionFromAnalysis, ConvertAnalysisToBusinessIntent                                                                                                                                                                                                                                                                                                  | assumptions/uncertainty/model version and outcome evaluation; proposed action is a typed child intent requiring normal governance |
| D1/A20 | HCMConcierge, ManagerAgent, HRISAgent, CompensationAgent, PayrollAgent, RecruitingAgent, TalentAgent, LearningAgent, LeaveAgent, BenefitsAgent, AccessAgent, LegalAgent, RegulatoryAgent, AnalyticsAgent, PlanningAgent, WorkflowAuthoringAgent, RuleAuthoringAgent, MappingAgent, RepairAgent, DataQualityAgent, CaseSummarizationAgent, DocumentAgent, KnowledgeAgent                                                                                                                                                        | bounded tools, minimum context, citations, proposal-only default, deterministic gateway receipts and human escalation             |
| A20    | AgentDelegation, AgentApprovalHandoff, AgentSimulation, AgentBulkPlanning, AgentMonitoring                                                                                                                                                                                                                                                                                                                                                                                                                                     | child intent identity, no authority amplification, simulation isolation and monitored-condition provenance                        |

## Reconciliation, repair and operations (`OPS`)

| Recipe  | Individual intents                                                                                                                                                                                                                                                                                                     | Unique semantic delta                                                                                                                                       |
| ------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| A18     | CreateRepairPlan, SimulateRepair, ApproveRepair, ExecuteRepair, VerifyRepair, RetryFailedEffect, RedriveConnectorOperation, RepairProjection, RepairSearchIndex, RepairMapping, RepairCrosswalk, RepairConfiguration, ResolveConflict, ResolvePartialTransaction, ResolveAmbiguousExternalEffect, ReconcileAfterRepair | diagnose from evidence, immutable repair proposal, current-state revalidation and verified postcondition                                                    |
| A5/A18  | CorrectWorkerFact, CorrectCompensation, CorrectTime, CorrectBenefitElection, CorrectLeave, CorrectDocument, CorrectIdentityLink, SupersedeBadIntent                                                                                                                                                                    | append correction/supersession and calculate every dependent replay/replan effect; payroll and filing correction remain owned by their domain records above |
| A18     | PauseWorkflow, ResumeWorkflow, CancelWorkflow, RetryWorkflowNode, SkipWorkflowNode, SatisfyWorkflowNode, OverrideWorkflowCondition, RewindWorkflow, CompensateWorkflow, MigrateWorkflow, QuarantineWorkflowVersion, QuarantineCapability, QuarantineConnector, QuarantineAgent                                         | typed intervention, safe point, elevated approval and no database surgery                                                                                   |
| A18/A12 | DrainWorkload, ResumeWorkload, OpenIncident, EscalateIncident, ResolveIncident, PublishCustomerAdvisory, GrantJITSupportAccess, RevokeJITSupportAccess, BreakGlass, ReviewBreakGlass                                                                                                                                   | incident-bound authority, expiry, dual control, customer communication and post-action review                                                               |
| A18/A20 | SuspendTenant, ResumeTenant, CloseTenant, RelocateTenant, ExportTenant, RunRecovery, VerifyRecovery, RebuildProjection, ReplayEventStream, VerifyLedger, RotateCredential, RotateSigningKey, RotateCertificate, TriggerFailover, TriggerFailback                                                                       | tenant isolation, destructive/irreversible checkpoints, recovery objectives and integrity verification                                                      |

## Commercial, tenant and platform (`COMM`/`TENANT`)

| Recipe    | Individual intents                                                                                                                                                                                                                                                                                                                                                                                                                                                              | Unique semantic delta                                                                          |
| --------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| A1/A2/A3  | ProvisionTenant, ActivateTenant, ChangeTenantProduct, GrantCommercialEntitlement, RevokeCommercialEntitlement, ChangeContract, SuspendForCommercialReason, ResumeCommercialAccess                                                                                                                                                                                                                                                                                               | contract/entitlement/tenant lifecycle, no collision with workforce access entitlement          |
| A1/D1/A10 | RecordUsage, SetUsageBudget, ChangeQuota, ForecastUsage, ForecastCost, AllocateCost, RateUsage, GenerateUsageEvidence                                                                                                                                                                                                                                                                                                                                                           | meter provenance, exact rating version, budget/quota controls and reproducible evidence        |
| A1/A5/A18 | GenerateInvoice, IssueCredit, IssueRefund, AdjustCharge, ResolveBillingDispute                                                                                                                                                                                                                                                                                                                                                                                                  | immutable financial document, payment/refund effect and correction/dispute linkage             |
| A20/A19   | CreateSandbox, ResetSandbox, SeedSyntheticWorker, SeedSyntheticOrganization, RunIntentSimulation, ReplayHistoricalIntent, ShadowNewWorkflow, ShadowNewRule, ShadowNewConnector, RunConformanceWorkflow, InjectExternalFailure, InjectConflict, InjectStaleApproval, InjectProviderTimeout, InjectDatabaseCrash, RunRecoveryScenario, CompareOldNewWorkflow, CompareOldNewRule, CompareOldNewProjection, GenerateTestIntent, ValidateIntentDefinition, GenerateRegressionFixture | environment isolation, synthetic markers, no production side effects and reproducible evidence |

## System and trigger-driven intents (`TRIGGER`)

All scheduled and event-driven names use `A17` and then the target intent's
recipe. This includes effective-date activation, future promotion/compensation/
manager/termination changes, leave start/return, payroll cutoffs/runs, enrollment,
merit/performance/access-review cycles, certification/visa/document expiry,
compliance/filing/retention/legal-hold deadlines, reminders/escalations,
reconciliation/sync/scans/reports/communications/population refreshes, and the
named Worker/Employment/Manager/Position/Compensation/Leave/Candidate/Access/
Payroll/Benefit/Connector/Webhook/Regulatory/Organization/Identity/Budget/
Population/DataQuality/Invariant/Security/Provider/Recovery/Message/Document/
Approval/SLA events.

Each source event or deadline produces a typed `TriggerFiring`; it never invokes
domain persistence directly. Deduplication of the firing and idempotency of the
target intent are separate contracts.

## Higher-order BusinessIntent workflows

| Recipe       | Individual intents                                                                                                                                                                                             | Unique semantic delta                                                                   |
| ------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| A20          | IntentComposer, IntentTemplateBuilder, IntentBundle, IntentDependencyGraph, IntentFork, IntentCompare                                                                                                          | immutable composition and alternative revisions; children retain independent truth      |
| D1           | IntentSimulation, IntentCostEstimate, IntentRiskEstimate, IntentConflictCheck, IntentApprovalPlanning, IntentObligationPlanning, IntentSideEffectPlanning, IntentExplain, IntentReplay, IntentShadowEvaluation | pure governed planning/explanation with pinned versions and zero side effects           |
| A2/A3/A5/A18 | IntentRevalidation, IntentSupersession, IntentCompensation, IntentRepair, IntentMigration, IntentScheduling, IntentSubscription, IntentOutcomeLinking, IntentToIntentTriggering, IntentEvidenceExport          | explicit relationship, lifecycle, causation limits, revalidation and evidence semantics |

## Completion policy common to all mappings

No recipe may collapse the independent runtime, business, obligation, external
consistency, reconciliation, operational and outcome dimensions. A direct
calculation can close after its governed result/evidence exists. A local HCM
mutation may be business-complete while declared external effects remain
degraded. A filing cannot be complete merely because transport accepted bytes.
A batch cannot hide failed children. A case cannot erase prior disposition when
reopened. A repair cannot close until the postcondition is freshly observed.

## Source-manifest convergence

After `MODEL-008`, generation performs an exact join:

```text
accepted BusinessIntent definition
  -> exactly one WorkflowDesignRecord
  -> exactly one execution disposition
  -> one archetype or justified DIRECT_CAPABILITY
  -> domain profile + unique delta
  -> model/capability/engine/workflow/evidence references
  -> conformance fixture and todo coverage
```

Missing, duplicated, ambiguous, alias-only or `UNBOUND_SOURCE` rows fail the
coverage gate. This registry may grow with reviewed extensions, but extensions
remain separately numbered/versioned from the original accepted baseline.
