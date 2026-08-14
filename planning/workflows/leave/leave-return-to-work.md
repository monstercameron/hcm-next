# Leave of Absence and Return to Work

## Identity and scope

```text
workflow_id: leave.leave_and_return/v1
parent intents: RequestLeave, StartLeave, ReturnFromLeave
subjects: Worker, Employment, LeaveCase, Entitlement, Schedule
state: REFERENCE + EXPLORED
```

Dependencies: People/Employment, Leave/Entitlement, Regulatory/Jurisdiction,
Benefits, Payroll, Time/Scheduling, Compensation, Documents, Human Work, Messaging,
Privacy/medical compartment, AuthZ, Source Authority, Records, Integration,
Reconciliation and Repair.

Features: statutory/company/CBA rule composition, protected evidence, eligibility
calculation/explanation, concurrent/stacked entitlements, intermittent leave,
deadlines, certification, pay/benefit/schedule effects, accommodation and return
readiness.

## Steps

```text
1 resolve worker/employment, jurisdiction, schedule and eligible policy universe
2 collect requested dates/pattern/reason category using minimum necessary fields
3 create protected LeaveCase; route medical evidence to restricted compartment
4 calculate eligibility and entitlement across statutory, CBA and company programs
5 explain program composition, concurrency, offsets, pay and benefit implications
6 request evidence/certification only when authorized and necessary
7 resolve approval/administrative determination and worker notices
8 simulate schedule, payroll, benefits, accrual, access and obligation effects
9 conflict-check termination, transfer, payroll close and overlapping leave
10 schedule start; revalidate eligibility/evidence/policy before activation
11 atomically activate leave facts and ordered downstream effects
12 observe payroll/benefits/time/schedule state and reconcile
13 monitor intermittent usage, exhaustion, evidence expiry and statutory deadlines
14 process extension/shortening/cancellation as new revisions
15 collect return date, restrictions and accommodation requirements separately
16 evaluate return readiness without exposing medical details to unauthorized actors
17 revalidate employment, position/schedule, benefits/payroll and access
18 atomically return worker/update leave and dispatch restoration effects
19 observe and reconcile; create repair for pay, benefit, schedule or access drift
20 close case under retention/hold rules
```

Candidate data includes leave program/rule versions, eligibility service and hours,
entitlement amount/unit, requested/approved/used intervals, intermittent pattern,
concurrency/offset links, evidence references, deadlines, pay/benefit treatment,
schedule effects, notices, restrictions as opaque capability constraints,
accommodation link and completion dimensions.
