# HR Workflow Discovery Backlog

This backlog sequences detailed workflow deep dives. The People, Organization/
Position, Rewards, Hire-to-retire, Benefits/Time/Leave, and HR Service/Employee
Relations items below now have archetype-level modeling in their domain catalogs.
They remain listed until an individual workflow document resolves their unique
steps, dependencies, features, data, properties, failure paths and domain
questions to the same depth as the initial reference set.

```text
CATALOG_MAPPED   complete archetype + unique dependency/data/step delta exists
DEEP_DIVE        individual exploration document exists
CONTRACTED       normative typed contracts resolve
```

## People and employment

```text
CreatePerson, ResolvePersonIdentity, MergePersonRecords, SeparatePersonRecords
CreateWorker, CreateEmployment, ModifyEmployment, SuspendEmployment
ReactivateEmployment, EndEmployment, ReinstateEmployment, RehireWorker
ConvertWorkerType, ChangeEmploymentType, ChangeWorkerStatus
ChangePreferredName, ChangeAddress, ChangeWorkLocation
ChangeRemoteWorkArrangement, ChangeLegalEntity, ChangeOrganization
ChangeDepartment, ChangeCostCenter, ChangeManager
AddSecondaryManager, RemoveSecondaryManager, AddEmploymentRelationship
CorrectWorkerData, RetroactivelyCorrectEmployment, FutureDateWorkerChange
CancelFutureWorkerChange, ExplainWorkerState, ReconstructWorkerState
CompareWorkerState, ExportWorkerRecord
```

## Organization, jobs and positions

```text
CreateJob, ModifyJob, RetireJob
CreatePosition, ModifyPosition, ReservePosition, ReleasePositionReservation
FillPosition, VacatePosition, ClosePosition, SplitPosition, ChangePositionFTE
ChangeJobAssignment, PromoteWorker, DemoteWorker, LateralTransfer
CreateOrganization, ModifyOrganization, MoveOrganization, MergeOrganizations
SplitOrganization, CloseOrganization, ReorganizeWorkforce
CreateHeadcountPlan, ModifyHeadcountPlan, ApproveHeadcount, FreezeHeadcount
AnalyzeOrgImpact
```

## Rewards and payroll adjacency

```text
SetCompensation, ChangeBasePay, ChangePayFrequency, ChangePayGrade
ChangePayBand, ChangeBonusTarget, GrantBonus, GrantCommission, GrantEquity
AdjustAllowance, RemoveAllowance, PlanMeritIncrease, RunMeritCycle
SimulateCompensation, EvaluatePayBandPosition, AnalyzePayEquity
ReserveCompensationBudget, ReleaseCompensationBudget
CorrectCompensation, RetroactivelyAdjustCompensation, ExplainCompensation
EnrollWorkerInPayroll, ChangePayGroup, CalculateFinalPay, ReconcilePayroll
```

## Hire-to-retire lifecycle

```text
CreateRequisition, ApproveRequisition, PublishJobPosting, CreateCandidate
SubmitApplication, ScreenCandidate, ScheduleInterview, RecordInterviewFeedback
RunBackgroundCheck, SelectCandidate, CreateOffer, ApproveOffer, SendOffer
AcceptOffer, ConvertCandidateToWorker
StartOnboarding, CollectOnboardingData, CollectWorkAuthorization
GenerateEmploymentDocuments, ProvisionWorkerAccess, AssignEquipment
AssignOnboardingLearning, VerifyOnboardingReadiness, CompleteOnboarding
InitiateTermination, ApproveTermination, ExecuteTermination, StartOffboarding
RecoverEquipment, RemoveWorkerAccess, FinalizeOffboarding, ReinstateWorker
```

## Benefits, leave, time and accommodations

```text
DetermineBenefitEligibility, EnrollBenefits, ChangeBenefitElection
ProcessLifeEvent, AddDependent, RemoveDependent, CalculateBenefitDeduction
ContinueBenefits, ReconcileBenefitEnrollment
RequestLeave, DetermineLeaveEligibility, ApproveLeave, DenyLeave
ExtendLeave, ShortenLeave, CancelLeave, StartLeave, ReturnFromLeave
RequestLeaveEvidence, CertifyLeave, RequestAccommodation
ApproveAccommodation, ModifyAccommodation, EvaluateReturnToWork
ReconcileLeaveState
RecordTimePunch, CorrectTimePunch, SubmitTimecard, ApproveTimecard
AssignSchedule, ChangeSchedule, PublishSchedule, SwapShift
DetectTimeException, ResolveTimeException, CalculateTimeBalance
```

## HR service delivery and employee relations

```text
CreateHRRequest, CreateCase, AssignCase, EscalateCase, AddCaseEvidence
AddConfidentialCaseNote, ResolveCase, CloseCase, ReopenCase
RequestEmploymentVerification, AnswerPolicyQuestion, EscalateToHumanHR
ReportConcern, OpenInvestigation, AssignInvestigator
CollectInvestigationEvidence, InterviewInvestigationParticipant
RecordInvestigationFinding, TakeDisciplinaryAction
CreatePerformanceImprovementPlan, ResolveGrievance
ApplyLegalHold, ReleaseLegalHold
```

## Supporting workflow families

Talent, learning, access, communications, documents, privacy, global mobility,
safety, regulatory, analytics, DataOps and platform-operation intents remain in
the broader BusinessIntent catalog. They are dependencies or follow-on workflow
families, not silently absorbed into the HR service.
