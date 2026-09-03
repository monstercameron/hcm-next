package model

import (
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// Catalog compiles the MODEL-011..MODEL-030 registry scoped to the fourteen
// aggregate roots the drafted intent catalog and the Promotion path require —
// Person, Worker, Employment, Assignment, Position, Job, CompensationGrade
// (Grade/PayBand), CompensationPackage (Compensation), OrganizationUnit
// (Organization), LegalEntity, IntentInstance (BusinessIntent),
// ProposalRevision, TransactionPlan and EvidenceRecord — plus the nine
// supporting entities [github.com/monstercameron/hcm-next/internal/intent.CoveredEntities]
// already names as covered (ApprovalBinding, BudgetReservation,
// CompensationComponent, ConnectorOperation, ExecutionBinding, Observation,
// OrganizationRelationship, PositionOccupancy, RepairPlan). Every property
// path here matches the strings
// [github.com/monstercameron/hcm-next/internal/intent/definitions.Bindings]
// already reads and writes, so this registry is grounded in the frozen intent
// kernel's own data rather than an independent guess.
func Catalog() (*Registry, error) {
	return NewRegistry(entityCatalog(), propertyCatalog(), aggregateCatalog(),
		relationshipCatalog(), authorityCatalog(), retentionCatalog())
}

func openInterval(year int, month time.Month, day int) values.EffectiveInterval {
	iv, err := values.NewOpenInstantInterval(values.NewInstant(time.Date(year, month, day, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		panic(err)
	}
	return iv
}

func entityCatalog() []EntityDefinition {
	e := func(name string, version int, key, domain string, class EntityClass, lifecycle string, aliases ...string) EntityDefinition {
		return EntityDefinition{
			Ref:                 EntityRef{Name: name, Version: version},
			Key:                 key,
			Aliases:             aliases,
			OwnerDomain:         domain,
			Class:               class,
			LifecycleAssignment: lifecycle,
			TenantScoped:        true,
			Status:              StatusActive,
		}
	}
	return []EntityDefinition{
		e("Person", 1, "person", "PEOPLE", ClassAggregateRoot, "RevisionedFactLifecycle"),
		e("Worker", 1, "worker", "PEOPLE", ClassAggregateRoot, "RevisionedFactLifecycle"),
		e("Employment", 1, "employment", "PEOPLE", ClassAggregateRoot, "EmploymentLifecycle"),
		e("Assignment", 1, "assignment", "PEOPLE", ClassAggregateRoot, "RevisionedFactLifecycle"),
		e("Position", 1, "position", "WORK", ClassAggregateRoot, "RevisionedFactLifecycle"),
		e("Job", 1, "job", "WORK", ClassAggregateRoot, "RevisionedReferenceLifecycle"),
		e("CompensationGrade", 1, "compensation_grade", "REWARDS", ClassAggregateRoot, "RevisionedReferenceLifecycle", "PayBand"),
		e("CompensationPackage", 1, "compensation_package", "REWARDS", ClassAggregateRoot, "CompensationPackageLifecycle", "Compensation"),
		e("CompensationComponent", 1, "compensation_component", "REWARDS", ClassChild, "RevisionedFactLifecycle"),
		e("OrganizationUnit", 1, "organization_unit", "WORK", ClassAggregateRoot, "RevisionedFactLifecycle", "Organization"),
		e("OrganizationRelationship", 1, "organization_relationship", "WORK", ClassAggregateRoot, "RevisionedFactLifecycle"),
		e("LegalEntity", 1, "legal_entity", "GOVERNANCE", ClassAggregateRoot, "RevisionedReferenceLifecycle"),
		e("IntentInstance", 1, "intent_instance", "GOVERNANCE", ClassAggregateRoot, "RequestLifecycle", "BusinessIntent"),
		e("ProposalRevision", 1, "proposal_revision", "GOVERNANCE", ClassAggregateRoot, "ImmutableEvidenceLifecycle"),
		e("TransactionPlan", 1, "transaction_plan", "GOVERNANCE", ClassAggregateRoot, "PlanLifecycle"),
		e("EvidenceRecord", 1, "evidence_record", "GOVERNANCE", ClassEvidence, "ImmutableEvidenceLifecycle"),
		e("PositionOccupancy", 1, "position_occupancy", "WORK", ClassAggregateRoot, "RevisionedFactLifecycle"),
		e("ApprovalBinding", 1, "approval_binding", "GOVERNANCE", ClassEvidence, "ImmutableEvidenceLifecycle"),
		e("ExecutionBinding", 1, "execution_binding", "GOVERNANCE", ClassEvidence, "ImmutableEvidenceLifecycle"),
		e("Observation", 1, "observation", "OPERATIONS", ClassEvidence, "ImmutableEvidenceLifecycle"),
		e("ConnectorOperation", 1, "connector_operation", "OPERATIONS", ClassAggregateRoot, "ConnectorOperationLifecycle"),
		e("BudgetReservation", 1, "budget_reservation", "REWARDS", ClassAggregateRoot, "AccountLifecycle"),
		e("RepairPlan", 1, "repair_plan", "OPERATIONS", ClassAggregateRoot, "PlanLifecycle"),
		// WorkerSummary is a rebuildable read model: NO_BUSINESS_LIFECYCLE is
		// the affirmative policy planning/data/models/registry-and-coverage-
		// contracts.md requires for a root with no commands (MODEL-012 GREEN).
		e("WorkerSummary", 1, "worker_summary", "PEOPLE", ClassReadModel, NoBusinessLifecycle),
	}
}

func propertyCatalog() []PropertyDefinition {
	p := func(ref PropertyRef, entity string, goType, schemaPath string, presence PresenceRule,
		class ClassificationLabel, temporal TemporalBehavior, authority string,
		correction CorrectionBehavior, retention string) PropertyDefinition {
		return PropertyDefinition{
			Ref:               ref,
			Entity:            EntityRef{Name: entity, Version: 1},
			GoType:            goType,
			SchemaPath:        schemaPath,
			Presence:          presence,
			Classification:    class,
			Temporal:          temporal,
			AuthorityRef:      authority,
			Correction:        correction,
			RetentionClassRef: retention,
			Status:            StatusActive,
		}
	}
	return []PropertyDefinition{
		p("person.identity", "Person", "string", "hcmnext.people.v1.Person.identity",
			PresenceRequired, ClassPII, TemporalPointInTime, "authority.person/v1",
			CorrectionSupersedes, "WORKER_TRANSACTION"),

		p("worker.status", "Worker", "lifecycle.StateID", "hcmnext.people.v1.Worker.status",
			PresenceRequired, ClassInternal, TemporalEffectiveDated, "authority.worker/v1",
			CorrectionSupersedes, "WORKER_TRANSACTION"),

		p("employment.status", "Employment", "lifecycle.StateID", "hcmnext.people.v1.Employment.status",
			PresenceRequired, ClassInternal, TemporalEffectiveDated, "authority.employment/v1",
			CorrectionSupersedes, "WORKER_TRANSACTION"),

		p("assignment.manager_relationship", "Assignment", "string",
			"hcmnext.people.v1.Assignment.manager_relationship_ref",
			PresenceOptional, ClassInternal, TemporalEffectiveDated, "authority.assignment/v1",
			CorrectionSupersedes, "WORKER_TRANSACTION"),
		p("assignment.position_ref", "Assignment", "string", "hcmnext.people.v1.Assignment.position_ref",
			PresenceRequired, ClassInternal, TemporalEffectiveDated, "authority.assignment/v1",
			CorrectionSupersedes, "WORKER_TRANSACTION"),
		p("assignment.effective_interval", "Assignment", "values.EffectiveInterval",
			"hcmnext.people.v1.Assignment.effective_interval",
			PresenceRequired, ClassInternal, TemporalEffectiveDated, "authority.assignment/v1",
			CorrectionSupersedes, "WORKER_TRANSACTION"),

		p("position.capacity", "Position", "uint32", "hcmnext.people.v1.Position.capacity",
			PresenceRequired, ClassInternal, TemporalEffectiveDated, "authority.position/v1",
			CorrectionSupersedes, "WORKER_TRANSACTION"),
		p("position.pay_band_ref", "Position", "string", "hcmnext.people.v1.Position.pay_band_ref",
			PresenceOptional, ClassInternal, TemporalEffectiveDated, "authority.position/v1",
			CorrectionSupersedes, "WORKER_TRANSACTION"),

		p("job.title", "Job", "string", "hcmnext.people.v1.Job.title",
			PresenceRequired, ClassInternal, TemporalPointInTime, "authority.job/v1",
			CorrectionSupersedes, "REFERENCE_DATA"),

		p("compensation_grade.grade_code", "CompensationGrade", "string",
			"hcmnext.rewards.v1.CompensationGrade.grade_code",
			PresenceRequired, ClassInternal, TemporalPointInTime, "authority.compensation_grade/v1",
			CorrectionSupersedes, "REFERENCE_DATA"),

		p("compensation_package.base_component_ref", "CompensationPackage", "string",
			"hcmnext.rewards.v1.CompensationPackage.base_component_ref",
			PresenceRequired, ClassCompensation, TemporalEffectiveDated, "authority.compensation_package/v1",
			CorrectionSupersedes, "COMPENSATION_RECORD"),
		p("compensation_package.currency", "CompensationPackage", "string",
			"hcmnext.rewards.v1.CompensationPackage.currency",
			PresenceRequired, ClassCompensation, TemporalEffectiveDated, "authority.compensation_package/v1",
			CorrectionSupersedes, "COMPENSATION_RECORD"),

		p("compensation_component.amount", "CompensationComponent", "values.Decimal",
			"hcmnext.rewards.v1.CompensationComponent.amount",
			PresenceRequired, ClassCompensation, TemporalEffectiveDated, "authority.compensation_component/v1",
			CorrectionSupersedes, "COMPENSATION_RECORD"),
		p("compensation_component.effective_interval", "CompensationComponent", "values.EffectiveInterval",
			"hcmnext.rewards.v1.CompensationComponent.effective_interval",
			PresenceRequired, ClassCompensation, TemporalEffectiveDated, "authority.compensation_component/v1",
			CorrectionSupersedes, "COMPENSATION_RECORD"),

		p("organization_unit.hierarchy_path", "OrganizationUnit", "string",
			"hcmnext.people.v1.OrganizationUnit.hierarchy_path",
			PresenceRequired, ClassInternal, TemporalEffectiveDated, "authority.organization_unit/v1",
			CorrectionSupersedes, "REFERENCE_DATA"),

		p("organization_relationship.manager_ref", "OrganizationRelationship", "string",
			"hcmnext.people.v1.OrganizationRelationship.manager_ref",
			PresenceRequired, ClassInternal, TemporalEffectiveDated, "authority.organization_relationship/v1",
			CorrectionSupersedes, "WORKER_TRANSACTION"),
		p("organization_relationship.effective_interval", "OrganizationRelationship", "values.EffectiveInterval",
			"hcmnext.people.v1.OrganizationRelationship.effective_interval",
			PresenceRequired, ClassInternal, TemporalEffectiveDated, "authority.organization_relationship/v1",
			CorrectionSupersedes, "WORKER_TRANSACTION"),

		p("legal_entity.registered_name", "LegalEntity", "string",
			"hcmnext.people.v1.LegalEntity.registered_name",
			PresenceRequired, ClassInternal, TemporalPointInTime, "authority.legal_entity/v1",
			CorrectionSupersedes, "REFERENCE_DATA"),

		p("intent_instance.lifecycle", "IntentInstance", "lifecycle.Dimensions",
			"hcmnext.intents.v1.IntentInstance.lifecycle",
			PresenceRequired, ClassInternal, TemporalPointInTime, "authority.intent_instance/v1",
			CorrectionAppends, "GOVERNANCE_EVIDENCE"),

		p("proposal_revision.material_digest", "ProposalRevision", "string",
			"hcmnext.intents.v1.ProposalRevision.material_digest",
			PresenceRequired, ClassInternal, TemporalImmutable, "authority.proposal_revision/v1",
			CorrectionNoCorrection, "GOVERNANCE_EVIDENCE"),

		p("transaction_plan.status", "TransactionPlan", "string",
			"hcmnext.intents.v1.TransactionPlan.status",
			PresenceRequired, ClassInternal, TemporalPointInTime, "authority.transaction_plan/v1",
			CorrectionAppends, "GOVERNANCE_EVIDENCE"),

		p("evidence_record.digest", "EvidenceRecord", "string",
			"hcmnext.intents.v1.EvidenceRecord.digest",
			PresenceRequired, ClassInternal, TemporalImmutable, "authority.evidence_record/v1",
			CorrectionNoCorrection, "GOVERNANCE_EVIDENCE"),

		p("position_occupancy.occupant_ref", "PositionOccupancy", "string",
			"hcmnext.people.v1.PositionOccupancy.occupant_ref",
			PresenceRequired, ClassInternal, TemporalEffectiveDated, "authority.position_occupancy/v1",
			CorrectionSupersedes, "WORKER_TRANSACTION"),

		p("approval_binding.decision", "ApprovalBinding", "string",
			"hcmnext.intents.v1.ApprovalBinding.decision",
			PresenceRequired, ClassInternal, TemporalImmutable, "authority.approval_binding/v1",
			CorrectionNoCorrection, "GOVERNANCE_EVIDENCE"),
		p("approval_binding.approved_proposal_digest", "ApprovalBinding", "string",
			"hcmnext.intents.v1.ApprovalBinding.approved_proposal_digest",
			PresenceRequired, ClassInternal, TemporalImmutable, "authority.approval_binding/v1",
			CorrectionNoCorrection, "GOVERNANCE_EVIDENCE"),
		p("approval_binding.reason_ref", "ApprovalBinding", "string",
			"hcmnext.intents.v1.ApprovalBinding.reason_ref",
			PresenceOptional, ClassInternal, TemporalImmutable, "authority.approval_binding/v1",
			CorrectionNoCorrection, "GOVERNANCE_EVIDENCE"),

		p("execution_binding.transaction_plan_id", "ExecutionBinding", "string",
			"hcmnext.intents.v1.ExecutionBinding.transaction_plan_id",
			PresenceRequired, ClassInternal, TemporalImmutable, "authority.execution_binding/v1",
			CorrectionNoCorrection, "GOVERNANCE_EVIDENCE"),

		p("observation.observed_state", "Observation", "string",
			"hcmnext.intents.v1.Observation.observed_state",
			PresenceRequired, ClassInternal, TemporalPointInTime, "authority.observation/v1",
			CorrectionAppends, "OPERATIONAL_EVIDENCE"),

		p("connector_operation.watermark", "ConnectorOperation", "string",
			"hcmnext.intents.v1.ConnectorOperation.watermark",
			PresenceRequired, ClassInternal, TemporalPointInTime, "authority.connector_operation/v1",
			CorrectionAppends, "OPERATIONAL_EVIDENCE"),

		p("budget_reservation.available_balance", "BudgetReservation", "values.Decimal",
			"hcmnext.rewards.v1.BudgetReservation.available_balance",
			PresenceRequired, ClassCompensation, TemporalPointInTime, "authority.budget_reservation/v1",
			CorrectionAppends, "COMPENSATION_RECORD"),
		p("budget_reservation.amount", "BudgetReservation", "values.Decimal",
			"hcmnext.rewards.v1.BudgetReservation.amount",
			PresenceRequired, ClassCompensation, TemporalPointInTime, "authority.budget_reservation/v1",
			CorrectionAppends, "COMPENSATION_RECORD"),
		p("budget_reservation.expiry", "BudgetReservation", "values.Instant",
			"hcmnext.rewards.v1.BudgetReservation.expiry",
			PresenceRequired, ClassInternal, TemporalPointInTime, "authority.budget_reservation/v1",
			CorrectionAppends, "COMPENSATION_RECORD"),
		p("budget_reservation.state", "BudgetReservation", "lifecycle.StateID",
			"hcmnext.rewards.v1.BudgetReservation.state",
			PresenceRequired, ClassInternal, TemporalPointInTime, "authority.budget_reservation/v1",
			CorrectionAppends, "COMPENSATION_RECORD"),

		p("repair_plan.targets", "RepairPlan", "[]string", "hcmnext.intents.v1.RepairPlan.targets",
			PresenceRequired, ClassInternal, TemporalPointInTime, "authority.repair_plan/v1",
			CorrectionAppends, "OPERATIONAL_EVIDENCE"),

		p("worker_summary.snapshot_ref", "WorkerSummary", "string",
			"hcmnext.people.v1.WorkerSummary.snapshot_ref",
			PresenceRequired, ClassInternal, TemporalPointInTime, "authority.worker_summary/v1",
			CorrectionAppends, "OPERATIONAL_EVIDENCE"),
	}
}

func aggregateCatalog() []AggregateDefinition {
	root := func(name string, boundary ConsistencyBoundary, streamKind string, invariants ...string) AggregateDefinition {
		return AggregateDefinition{
			Root:                EntityRef{Name: name, Version: 1},
			LifecycleAssignment: mustEntityLifecycle(name),
			CommandBoundary:     boundary,
			StreamKind:          streamKind,
			InvariantRefs:       invariants,
		}
	}
	agg := root("Person", BoundaryLocalACID, "PERSON", "invariant.person_identity_unique/v1")
	worker := root("Worker", BoundaryLocalACID, "WORKER", "invariant.worker_references_person/v1")
	employment := root("Employment", BoundaryLocalACID, "EMPLOYMENT", "invariant.employment_dates_ordered/v1")
	assignment := root("Assignment", BoundaryLocalACID, "", "invariant.assignment_within_employment/v1")
	position := root("Position", BoundaryLocalACID, "POSITION", "invariant.position_capacity_non_negative/v1")
	job := root("Job", BoundaryLocalACID, "")
	grade := root("CompensationGrade", BoundaryLocalACID, "")
	pkg := root("CompensationPackage", BoundaryLocalACID, "", "invariant.compensation_currency_consistent/v1")
	pkg.ChildRefs = []EntityRef{{Name: "CompensationComponent", Version: 1}}
	orgUnit := root("OrganizationUnit", BoundaryLocalACID, "", "invariant.organization_hierarchy_acyclic/v1")
	orgRel := root("OrganizationRelationship", BoundaryLocalACID, "", "invariant.manager_relationship_acyclic/v1")
	legalEntity := root("LegalEntity", BoundaryLocalACID, "")
	intentInstance := root("IntentInstance", BoundaryCrossAggregateTransaction, "TRANSACTION",
		"invariant.intent_five_dimensions/v1")
	proposal := root("ProposalRevision", BoundaryLocalACID, "")
	txPlan := root("TransactionPlan", BoundaryCrossAggregateTransaction, "TRANSACTION",
		"invariant.transaction_plan_participants_declared/v1")
	// ApprovalBinding, ExecutionBinding, Observation and EvidenceRecord are
	// EVIDENCE-class entities, not commanded aggregate roots: they are
	// immutable records appended alongside the transaction that produced
	// them, so they carry no independent AggregateDefinition (see
	// NewRegistry, which rejects an EVIDENCE-class root outright).
	occupancy := root("PositionOccupancy", BoundaryLocalACID, "", "invariant.occupancy_within_capacity/v1")
	connector := root("ConnectorOperation", BoundaryExternalObservation, "INTEGRATION_OPERATION")
	budget := root("BudgetReservation", BoundaryLocalACID, "", "invariant.budget_no_over_reservation/v1")
	repair := root("RepairPlan", BoundaryLocalACID, "")
	// WorkerSummary is NO_BUSINESS_LIFECYCLE: no command boundary, and it
	// accepts no commands (see AggregateDefinition.Rebuildable).
	summary := root("WorkerSummary", "", "")

	return []AggregateDefinition{
		agg, worker, employment, assignment, position, job, grade, pkg, orgUnit, orgRel,
		legalEntity, intentInstance, proposal, txPlan, occupancy,
		connector, budget, repair, summary,
	}
}

// mustEntityLifecycle looks up the compiled-in entity's own lifecycle
// assignment, so an AggregateDefinition never drifts from its
// EntityDefinition. It panics on a name typo in this file, never at registry
// compile time (NewRegistry cross-validates the published values, not this
// helper).
func mustEntityLifecycle(name string) string {
	for _, e := range entityCatalog() {
		if e.Ref.Name == name {
			return e.LifecycleAssignment
		}
	}
	panic("model: catalog entity " + name + " not found")
}

func relationshipCatalog() []RelationshipDefinition {
	return []RelationshipDefinition{
		{
			Ref:          RelationshipRef{Name: "ManagerRelationship", Version: 1},
			SourceEntity: EntityRef{Name: "Assignment", Version: 1},
			TargetEntity: EntityRef{Name: "Assignment", Version: 1},
			Cardinality:  CardinalityOneToOne,
			Exclusive:    true,
			AllowCycles:  false,
			TenantScoped: true,
		},
		{
			Ref:          RelationshipRef{Name: "OrganizationHierarchy", Version: 1},
			SourceEntity: EntityRef{Name: "OrganizationUnit", Version: 1},
			TargetEntity: EntityRef{Name: "OrganizationUnit", Version: 1},
			Cardinality:  CardinalityOneToOne,
			Exclusive:    true,
			AllowCycles:  false,
			TenantScoped: true,
		},
		{
			Ref:          RelationshipRef{Name: "AssignmentPosition", Version: 1},
			SourceEntity: EntityRef{Name: "Assignment", Version: 1},
			TargetEntity: EntityRef{Name: "Position", Version: 1},
			Cardinality:  CardinalityOneToOne,
			Exclusive:    true,
			AllowCycles:  true,
			TenantScoped: true,
		},
		{
			Ref:          RelationshipRef{Name: "PositionOccupant", Version: 1},
			SourceEntity: EntityRef{Name: "Position", Version: 1},
			TargetEntity: EntityRef{Name: "PositionOccupancy", Version: 1},
			Cardinality:  CardinalityOneToMany,
			Exclusive:    false,
			AllowCycles:  true,
			TenantScoped: true,
		},
		{
			Ref:          RelationshipRef{Name: "EmploymentLegalEntity", Version: 1},
			SourceEntity: EntityRef{Name: "Employment", Version: 1},
			TargetEntity: EntityRef{Name: "LegalEntity", Version: 1},
			Cardinality:  CardinalityOneToOne,
			Exclusive:    true,
			AllowCycles:  true,
			TenantScoped: true,
		},
	}
}

func authorityCatalog() []SourceAuthorityAssignment {
	interval := openInterval(2020, time.January, 1)
	internal := func(ref, scope string, exclusive bool) SourceAuthorityAssignment {
		return SourceAuthorityAssignment{
			AssignmentRef:    ref,
			Kind:             AuthorityInternal,
			DomainScope:      scope,
			Effective:        interval,
			Exclusive:        exclusive,
			FreshnessSeconds: 0,
			Merge:            MergeAuthorityPrecedence,
			EvidenceRef:      "evidence.authority_registration/v1",
		}
	}
	external := func(ref, scope string) SourceAuthorityAssignment {
		return SourceAuthorityAssignment{
			AssignmentRef:    ref,
			Kind:             AuthorityExternalSystem,
			DomainScope:      scope,
			Effective:        interval,
			Exclusive:        true,
			FreshnessSeconds: 3600,
			Merge:            MergeManualReconciliation,
			EvidenceRef:      "evidence.authority_registration/v1",
		}
	}
	return []SourceAuthorityAssignment{
		internal("authority.person/v1", "person", true),
		internal("authority.worker/v1", "worker", true),
		internal("authority.employment/v1", "employment", true),
		internal("authority.assignment/v1", "assignment", true),
		internal("authority.position/v1", "position", true),
		internal("authority.job/v1", "job", true),
		internal("authority.compensation_grade/v1", "compensation_grade", true),
		internal("authority.compensation_package/v1", "compensation_package", true),
		internal("authority.compensation_component/v1", "compensation_component", true),
		internal("authority.organization_unit/v1", "organization_unit", true),
		internal("authority.organization_relationship/v1", "organization_relationship", true),
		internal("authority.legal_entity/v1", "legal_entity", true),
		internal("authority.intent_instance/v1", "intent_instance", true),
		internal("authority.proposal_revision/v1", "proposal_revision", true),
		internal("authority.transaction_plan/v1", "transaction_plan", true),
		internal("authority.evidence_record/v1", "evidence_record", true),
		internal("authority.position_occupancy/v1", "position_occupancy", true),
		internal("authority.approval_binding/v1", "approval_binding", true),
		internal("authority.execution_binding/v1", "execution_binding", true),
		external("authority.observation/v1", "observation"),
		external("authority.connector_operation/v1", "connector_operation"),
		internal("authority.budget_reservation/v1", "budget_reservation", true),
		internal("authority.repair_plan/v1", "repair_plan", true),
		internal("authority.worker_summary/v1", "worker_summary", true),
	}
}

func retentionCatalog() []RetentionClass {
	return []RetentionClass{
		{
			ClassRef:          "WORKER_TRANSACTION",
			DefaultPeriodDays: 2555,
			JurisdictionOverrides: map[string]uint32{
				"US-CA": 1460,
				"US-NY": 2190,
			},
			TriggerEvent:     "EMPLOYMENT_END",
			DispositionOwner: "records-management",
			AuthorityRef:     "authority.employment/v1",
		},
		{
			ClassRef:          "COMPENSATION_RECORD",
			DefaultPeriodDays: 2555,
			TriggerEvent:      "RECORD_CREATED",
			DispositionOwner:  "records-management",
			AuthorityRef:      "authority.compensation_package/v1",
		},
		{
			ClassRef:          "GOVERNANCE_EVIDENCE",
			DefaultPeriodDays: 3650,
			TriggerEvent:      "RECORD_CREATED",
			DispositionOwner:  "governance",
			AuthorityRef:      "authority.intent_instance/v1",
		},
		{
			ClassRef:          "OPERATIONAL_EVIDENCE",
			DefaultPeriodDays: 1095,
			TriggerEvent:      "RECORD_CREATED",
			DispositionOwner:  "operations",
			AuthorityRef:      "authority.observation/v1",
		},
		{
			ClassRef:          "REFERENCE_DATA",
			DefaultPeriodDays: 3650,
			TriggerEvent:      "RECORD_SUPERSEDED",
			DispositionOwner:  "data-governance",
			AuthorityRef:      "authority.job/v1",
		},
	}
}
