# Recruit, Hire and Onboard

## Identity and scope

```text
workflow_id: lifecycle.recruit_hire_onboard/v1
parent intent: HireWorker / ProcessRequest
subjects: Requisition, Candidate, Person, Worker, Employment, Assignment, Position
state: REFERENCE + EXPLORED
```

## Dependencies and features

Required domains: Workforce Planning, Position/Headcount, Recruiting, Identity
Resolution, People/Employment, Compensation, Payroll, Benefits, Access, Equipment,
Learning, Documents/E-signature and Communications. Required governance includes
AuthZ, legal/jurisdiction/privacy, worker-classification, source authority,
decision rights, conflicts/reservations and records. Integrations may include ATS,
background check, HRIS, payroll, IAM, ITSM/MDM, benefits and signing providers.

Features: position/budget reservation, candidate privacy, forms/evidence,
decision-support boundaries, offer approval/signature, future-effective employment,
ordered provisioning, deadlines, multidimensional readiness, reconciliation and
repair.

## Steps

```text
1 identify workforce need and resolve/create position/headcount/budget authority
2 create/approve requisition and reserve capacity
3 publish jurisdiction/localization-safe job posting
4 receive application and resolve candidate identity/consent/source
5 screen against job requirements using governed criteria and human decision rights
6 schedule interviews; collect structured, access-controlled feedback
7 select candidate; record decision evidence and adverse-action obligations
8 simulate offer compensation, budget, pay-equity and legal terms
9 approve exact offer; generate localized documents; send through safe channel
10 collect signature/acceptance/decline and preserve evidence
11 run applicable pre-employment checks with restricted result visibility
12 match-or-create canonical Person; never merge solely from probabilistic score
13 create Worker, Employment and Assignment proposals linked to accepted offer
14 collect onboarding, tax, payroll, benefits and work-authorization data/forms
15 revalidate position, budget, checks, law, identity, offer and start date
16 atomically establish owned worker/employment/assignment/comp facts and outbox
17 provision payroll, benefits, IAM, equipment, schedule/location and learning
18 deliver required notices and onboarding tasks
19 observe all mandatory systems and evaluate ready-to-work invariants
20 reconcile; repair partial setup without duplicating employment or accounts
21 release unused reservations and close recruiting/onboarding dimensions separately
```

## Candidate data

```text
Position/Requisition: job, org, location, legal entity, capacity, budget, dates
Candidate/Application: person claims, contact, source, consent/notices, answers
Assessment: criterion version, evaluator, evidence, result, correction/contest path
Offer: components, currency/basis, conditions, dates, document/signature evidence
Hire: person link decision, worker/employment/assignment proposals, authority
Onboarding: forms, tax/payroll elections, benefit dependents, authorization evidence
Provisioning: account/equipment/course/schedule effects, ordering and observations
Readiness: business, legal, payroll, access, equipment and learning dimensions
```

Person, Candidate and Worker are overlapping roles/relationships, not exclusive
person states. Background-check content, demographic data and medical information
remain compartmented and are never broadly copied into workflow context.
