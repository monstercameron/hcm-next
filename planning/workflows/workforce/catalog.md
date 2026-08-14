# Organization, Job, Position and Headcount Workflow Catalog

Rows use the complete [workflow archetypes](../_shared/workflow-archetypes.md).

| Intent                     | Arch.  | Unique dependencies                 | Primary data/writes                 | Required step delta                                             |
| -------------------------- | ------ | ----------------------------------- | ----------------------------------- | --------------------------------------------------------------- |
| CreateJob                  | A1     | Ref, Compensation, Skills           | Job definition/version              | uniqueness, localization and external-code crosswalk            |
| ModifyJob                  | A2     | Ref, Compensation, Skills, Impact   | JobRevision                         | impact current assignments/positions before publication         |
| RetireJob                  | A3     | Ref, Ppl, Pos                       | retired interval                    | block while active/future references lack migration plan        |
| CreatePosition             | A1     | Org, Job, Budget                    | PositionRevision                    | capacity/budget/authority validation                            |
| ModifyPosition             | A2     | Org, Job, Budget, Ppl               | PositionRevision                    | occupancy and future requisition impact                         |
| ReservePosition            | A7     | Pos, Conflict                       | fenced reservation                  | quantity/capacity and expiry semantics                          |
| ReleasePositionReservation | A7     | Pos, Conflict                       | release receipt                     | authority and consumption race check                            |
| FillPosition               | A2     | Pos, Ppl, Org                       | occupancy + assignment link         | capacity/cardinality and effective-overlap checks               |
| VacatePosition             | A3     | Pos, Ppl                            | ended occupancy                     | reason and downstream vacancy/recruiting trigger                |
| ClosePosition              | A3     | Pos, Ppl, Planning                  | closed position                     | require no surviving occupancy/reservations/future fills        |
| SplitPosition              | A8     | Pos, Budget, Ppl                    | source reduction + target positions | preserve total capacity and migrate occupancy explicitly        |
| ChangePositionFTE          | A2     | Pos, Budget, Ppl                    | capacity revision                   | fixed-decimal capacity arithmetic and occupancy impact          |
| ChangeJobAssignment        | A2     | Ppl, Job, Pos, Comp                 | assignment job revision             | validate qualification/band/access impacts                      |
| PromoteWorker              | A8     | Ppl, Job, Pos, Comp, Budget, Access | composite child changes             | promotion-specific simulation/approvals/learning                |
| DemoteWorker               | A8     | same + Reg/ER                       | composite child changes             | protected reason/decision rights/pay/access consequences        |
| LateralTransfer            | A8     | Ppl, Org, Pos, Finance, Access      | assignment/relationship changes     | explicitly prove not promotion/demotion by policy               |
| CreateOrganization         | A1     | Org, Ref, Finance, Legal Entity     | OrgUnitRevision                     | parent/type/authority and code uniqueness                       |
| ModifyOrganization         | A2     | Org, Impact                         | OrgUnitRevision                     | descendant/role/policy implications                             |
| MoveOrganization           | A8     | Org, AuthZ, Finance                 | reparent edge revision              | cycle, legal boundary, scope and inherited-policy recalculation |
| MergeOrganizations         | A8     | Org, Ppl, Pos, Finance, AuthZ       | merge plan/revisions                | map children, workers, positions, budgets, roles and history    |
| SplitOrganization          | A8     | same                                | split plan/revisions                | deterministic allocation and unresolved-item queue              |
| CloseOrganization          | A3     | Org, Ppl, Pos                       | closed interval                     | require disposition of children, assignments and positions      |
| ReorganizeWorkforce        | A8/A10 | all workforce domains               | batched child proposals             | frozen affected set, partitioning, sequencing and mass repair   |
| CreateHeadcountPlan        | A1     | Planning, Finance, Org              | plan/version/lines                  | scenario assumptions, units, period and authority               |
| ModifyHeadcountPlan        | A2     | Planning, Finance                   | plan revision                       | compare approved/consumed/reserved capacity                     |
| ApproveHeadcount           | A7     | Planning, Finance, Human Work       | authorization/reservation           | distinguish approval from position creation                     |
| FreezeHeadcount            | A2     | Planning, Conflict, Policy          | freeze scope/interval               | define exemptions, existing reservations and expiry             |
| AnalyzeOrgImpact           | A9     | Org, Ppl, Pos, AuthZ, Finance       | impact report                       | affected-set watermark, uncertainty, no mutation                |
