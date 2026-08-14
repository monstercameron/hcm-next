# User Flow: Medical Leave and Return to Work

## Record

```text
flow_id: UF-008
primary: employee requesting leave
other participants: representative, leave specialist, manager with restricted operational view, benefits/payroll/WFM observers
root intent: RequestLeave
child/related intents: ExtendLeave, ShortenLeave, CancelLeave, ReturnFromLeave, EstablishWorkRestriction, RequestAccommodation
archetype: UF-A3 + UF-A4 + UF-A6
workflow: ../../workflows/leave/leave-return-to-work.md
```

Success means the employee can request and track leave without being asked to
decide their own eligibility, medical evidence remains compartmented, legal and
company determinations are distinguishable, paid-leave replanning is truthful and
return obligations continue through verified closure.

## Main flow

| Stage               | Employee experience                                                                                                                                          | Semantic action and required result                                                             |
| ------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------- |
| Discover/orient     | Explain leave types in plain language, privacy, evidence handling, emergency/non-digital alternatives and that dates are requested rather than approved      | resolve permitted `RequestLeave` action and applicable content without pre-deciding eligibility |
| Collect             | Enter requested type/dates/schedule/reason and attach evidence reference; representation/interpreter is explicit                                             | save draft; ingest artifact through scanning/classification before leave evaluation             |
| Submit              | Review requested facts and privacy notice; one idempotent submit creates RequestLeave and LeaveRequest                                                       | zero employment/schedule/balance/payroll mutation                                               |
| Eligibility/plan    | Display program results as eligible/ineligible/conditional/unknown with explanation and missing facts; show proposed protected/paid/unpaid segments          | pinned LegalContext, eligibility and entitlement plan                                           |
| Evidence task       | Employee sees received/needs-more-information state; specialist sees restricted artifact; manager sees only operational absence facts                        | compartmented WorkItems and secure messages                                                     |
| Determination       | Accessible notice explains dates, programs, pay/balance plan, benefits treatment, return requirements, appeal/correction route and unknown external outcomes | immutable determination document/message evidence                                               |
| Wait/revalidate     | Before start, show scheduled state; balance/legal/employment drift may create a successor plan rather than deny valid leave                                  | partial replan and exact invalidated decisions                                                  |
| Active leave        | Status shows leave active, remaining/expected return, open obligations and separately observed payroll/benefits/WFM states                                   | authoritative LeaveRecord/availability and external reconciliation                              |
| Change during leave | Extend/shorten/cancel starts related intent and preserves chronology                                                                                         | related BusinessIntent with new snapshot and plan                                               |
| Return preparation  | Prompt only for required clearance/evidence; show deadline and accommodation route                                                                           | ReturnReadiness result and task loop                                                            |
| Return/restriction  | If conditional, show work restriction/accommodation process without exposing medical detail to manager                                                       | typed restriction and qualification result                                                      |
| Close               | Show leave ended, availability restored and downstream observation/repair dimensions                                                                         | closure policy, evidence and retained history                                                   |

## Critical alternatives

- Eligibility source unavailable: return `UNKNOWN`, request only necessary
  information and preserve deadline; never tell the employee they are ineligible.
- PTO changes after determination: leave remains valid while paid segments are
  replanned; present the exact 72-to-56-hour change and resulting unpaid segment.
- Evidence upload fails scanning: quarantine bytes, preserve safe draft and offer
  accessible/manual submission route.
- Manager opens deep link to medical document: hide existence/content according
  to compartment policy and log bounded denial evidence.
- Benefits provider unavailable: leave may be active with external consistency
  unknown; do not roll back leave.
- Earlier/later return, not-ready return and ready-with-restrictions have separate
  actions and explanations.
- Employee loses ordinary workforce login while on leave: approved personal or
  assisted secure route preserves legally required access without broad account
  restoration.
- Representative/interpreter route records identities, authority, translations
  and participant confirmation.

## Initial TDD seeds

```text
TestLeaveRequestNeverAcceptsClientEligibilityOrBalanceAsTruth
TestLeaveSubmitCreatesRequestButNoScheduleBalanceOrPayrollEffect
TestLeaveManagerCannotReceiveMedicalEvidence
TestLeaveEligibilityUnknownPreservesDeadlineAndDoesNotDeny
TestLeaveBalanceDriftCreatesSuccessorPlanWithoutCancellingEntitlement
TestLeaveActiveShowsBenefitsUnknownWithoutFalseFailure
TestLeaveExtensionCreatesRelatedIntentAndPreservesOriginalHistory
TestReturnWithRestrictionCreatesTypedFollowUpWithoutMedicalDisclosure
TestLeavePersonalAndAssistedRoutesPreserveSemanticParity
TestLeaveFlowPassesKeyboardScreenReaderRTLAndPlainLanguageFixtures
```
