package contracts

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	dataopsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/dataops/v1"
	evidencev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/evidence/v1"
	integrationv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/integration/v1"
)

// TestTodo_PROTO_004 proves the generated DataOps, integration and evidence
// service contracts (schema/proto/hcmnext/dataops/v1,
// schema/proto/hcmnext/integration/v1, schema/proto/hcmnext/evidence/v1)
// satisfy the todo's GREEN criteria: every declared method carries a total
// P1A disposition comment, every new enum declares an UNSPECIFIED zero
// value, presence rules are explicit where the convention requires them,
// resumable/artifact-ref-shaped and redacted-diagnostic messages round-trip,
// and clean `buf generate` runs are byte-identical.
func TestTodo_PROTO_004(t *testing.T) {
	repoRoot := findRepoRoot(t)
	dir := t.TempDir()
	fds := buildDescriptorSet(t, repoRoot, dir, "descriptor.binpb")

	t.Run("DataOpsServiceHasTotalDispositionComments", func(t *testing.T) {
		assertServiceHasTotalDispositionComments(t, fds,
			"hcmnext/dataops/v1/dataops_service.proto", "DataOpsService",
			[]string{"ExplainFieldHistory", "DiffRecord", "CreateRepairPlan", "SimulateRepair"})
	})

	t.Run("IntegrationServiceHasTotalDispositionComments", func(t *testing.T) {
		assertServiceHasTotalDispositionComments(t, fds,
			"hcmnext/integration/v1/integration_service.proto", "IntegrationService",
			[]string{
				"ListConnectorDefinitions", "GetConnectorDefinition",
				"ListConnectorConnections", "GetConnectorConnection",
				"TestConnectorConnection",
				"ListExternalObservations", "GetExternalObservation",
			})
	})

	t.Run("EvidenceServiceHasTotalDispositionComments", func(t *testing.T) {
		assertServiceHasTotalDispositionComments(t, fds,
			"hcmnext/evidence/v1/evidence_service.proto", "EvidenceService",
			[]string{"GetExecutionReceipt", "ExplainTransaction", "ExportIntentEvidence"})
	})

	t.Run("OperationsServiceHasTotalDispositionComments", func(t *testing.T) {
		assertServiceHasTotalDispositionComments(t, fds,
			"hcmnext/evidence/v1/evidence_service.proto", "OperationsService",
			[]string{"GetOperation", "CancelOperation"})
	})

	t.Run("DataOpsEnumsHaveUnspecifiedZero", func(t *testing.T) {
		_ = (&dataopsv1.RecordDiff{}).ProtoReflect().Descriptor()
		_ = (&dataopsv1.RepairPlan{}).ProtoReflect().Descriptor()
		assertEnumsHaveUnspecifiedZero(t, "hcmnext.dataops.v1", 10)
	})

	t.Run("IntegrationEnumsHaveUnspecifiedZero", func(t *testing.T) {
		_ = (&integrationv1.ConnectorDefinition{}).ProtoReflect().Descriptor()
		_ = (&integrationv1.ConnectorConnection{}).ProtoReflect().Descriptor()
		_ = (&integrationv1.ExternalObservation{}).ProtoReflect().Descriptor()
		assertEnumsHaveUnspecifiedZero(t, "hcmnext.integration.v1", 8)
	})

	t.Run("EvidenceEnumsHaveUnspecifiedZero", func(t *testing.T) {
		_ = (&evidencev1.ZeroEffectReceipt{}).ProtoReflect().Descriptor()
		_ = (&evidencev1.Explanation{}).ProtoReflect().Descriptor()
		assertEnumsHaveUnspecifiedZero(t, "hcmnext.evidence.v1", 8)
	})

	t.Run("PresenceRulesAreExplicit", func(t *testing.T) {
		canonicalField := findMessage(t, "hcmnext.dataops.v1", "hcmnext.dataops.v1.CanonicalField")
		assertFieldPresence(t, canonicalField, "updated_at", true)
		assertFieldPresence(t, canonicalField, "field", false)

		finding := findMessage(t, "hcmnext.dataops.v1", "hcmnext.dataops.v1.FieldFinding")
		assertFieldPresence(t, finding, "canonical_authority", true)
		assertFieldPresence(t, finding, "canonical_updated_at", true)

		observation := findMessage(t, "hcmnext.integration.v1", "hcmnext.integration.v1.ExternalObservation")
		assertFieldPresence(t, observation, "raw_artifact_ref", true)
		assertFieldPresence(t, observation, "observation_id", false)

		operation := findMessage(t, "hcmnext.evidence.v1", "hcmnext.evidence.v1.Operation")
		assertFieldPresence(t, operation, "result", true)
		assertFieldPresence(t, operation, "error", true)
		assertFieldPresence(t, operation, "operation_id", false)
	})

	t.Run("GoldenRoundTrip", func(t *testing.T) {
		now := timestamppb.New(time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC))
		subject := &commonv1.EntityRef{TenantId: "tenant-1", Kind: "worker", Id: "w-1"}

		// RecordDiff carries a redacted field (no compared value travels with
		// it) alongside a decided mismatch, proving the diagnostic stays
		// redacted rather than leaking values through the finding.
		diff := &dataopsv1.RecordDiff{
			Subject:         subject,
			CanonicalExists: true,
			ObservedExists:  true,
			Findings: []*dataopsv1.FieldFinding{
				{
					Subject: subject, Field: "compensation.base",
					Access: dataopsv1.Access_ACCESS_AUTHORIZED, Verdict: dataopsv1.Verdict_VERDICT_MISMATCH,
					Reason: "value_disagrees", Safety: dataopsv1.RepairSafety_REPAIR_SAFETY_SAFE, SafetyReason: "ordered_by_evidence",
					CanonicalAuthority: &evidencev1.SourceAuthority{
						Kind: evidencev1.AuthorityKind_AUTHORITY_KIND_LOCAL_AUTHORITATIVE, System: "hcmnext", PolicyRef: "authority.compensation.v1",
					},
					Observation: &dataopsv1.ObservationWatermark{
						Source: "workday.hcm", SchemaVersion: "1.0.0", RetrievedAt: now, Digest: "sha256:abc",
					},
				},
				{
					Subject: subject, Field: "person.ssn",
					Access: dataopsv1.Access_ACCESS_DENIED, Verdict: dataopsv1.Verdict_VERDICT_REDACTED,
					Reason: "not_authorized", Safety: dataopsv1.RepairSafety_REPAIR_SAFETY_UNDECIDABLE, SafetyReason: "not_authorized",
				},
			},
			Verdicts: &dataopsv1.VerdictCounts{Mismatch: 1, Redacted: 1},
			Safety:   &dataopsv1.SafetyCounts{Safe: 1, Undecidable: 1},
			Digest:   "sha256:diffdigest",
		}

		repairPlan := &dataopsv1.RepairPlan{
			IntentType: "hcmnext.operations.create_repair_plan", IntentVersion: "v1",
			Id: "plan-1", TenantId: "tenant-1", Subject: subject,
			DiffDigest: "sha256:diffdigest",
			Steps: []*dataopsv1.RepairStep{
				{
					Ordinal: 1, Action: dataopsv1.RepairAction_REPAIR_ACTION_REFRESH_PROJECTION,
					Target:          &dataopsv1.RepairTarget{System: "hcmnext", Subject: subject, Field: "compensation.base"},
					ExpectedCurrent: &dataopsv1.PresenceValue{Presence: commonv1.Presence_PRESENCE_VALUE, Value: "120000.00"},
					ExpectedPost:    &dataopsv1.PresenceValue{Presence: commonv1.Presence_PRESENCE_VALUE, Value: "130000.00"},
					ValueKind:       dataopsv1.ValueKind_VALUE_KIND_NUMBER,
					Treatment:       dataopsv1.RepairHistoryTreatment_REPAIR_HISTORY_TREATMENT_APPEND_CORRECTION,
					Risk:            dataopsv1.RepairRiskClass_REPAIR_RISK_CLASS_LOW,
					Preconditions: []*dataopsv1.RepairPrecondition{
						{Kind: dataopsv1.RepairPreconditionKind_REPAIR_PRECONDITION_KIND_DIFF_DIGEST_UNCHANGED, Ref: "diff", Expected: "sha256:diffdigest"},
					},
					IdempotencyKey: "idem-step-1", MaxAttempts: 3,
					WriteSet: []string{"hcmnext.compensation.base"}, Rollback: "revert_to_previous_projection",
					Observation: "projection.compensation.base", Success: "matches_authority_value", Reason: "safe_field_drift",
				},
			},
			// executable is always false in P1A; the plan is a recommendation
			// only, never an execution path.
			Executable:          false,
			NotExecutableReason: "p1a_repair_execution_not_authorized",
			ExecutionState:      "NOT_PLANNED",
			Digest:              "sha256:plandigest",
			Effects:             &evidencev1.EffectCounters{},
			Receipt: &evidencev1.ZeroEffectReceipt{
				IntentType: "hcmnext.operations.create_repair_plan", IntentVersion: "v1",
				Mode: evidencev1.ReceiptMode_RECEIPT_MODE_SIMULATE, RequestState: "SIMULATED", ExecutionState: "NOT_PLANNED",
				InputsDigest: "sha256:in", ResultDigest: "sha256:out", Counters: &evidencev1.EffectCounters{},
			},
		}

		connectorDef := &integrationv1.ConnectorDefinition{
			ConnectorId: "workday.hcm", Vendor: "Workday", Product: "HCM", ConnectorVersion: "1.0.0",
			Maturity: integrationv1.Maturity_MATURITY_CERTIFIED,
			Objects:  []integrationv1.ObjectKind{integrationv1.ObjectKind_OBJECT_KIND_WORKER},
			Capabilities: []*integrationv1.Capability{
				{Object: integrationv1.ObjectKind_OBJECT_KIND_WORKER, Operation: integrationv1.ConnectorOperation_CONNECTOR_OPERATION_READ},
			},
			AuthModes: []integrationv1.AuthMode{integrationv1.AuthMode_AUTH_MODE_OAUTH2_CLIENT_CREDENTIALS},
			ReadModes: []integrationv1.ReadMode{integrationv1.ReadMode_READ_MODE_FULL},
			Bounds:    &integrationv1.Bounds{MaxPageSize: 200, MaxPagesPerRun: 100, MaxRecordsPerRun: 20000, MaxRecordBytes: 65536},
			Pagination: &integrationv1.PaginationContract{
				Style: "KEYSET", SortKeyField: "updated_at", TieBreakField: "worker_id", StableUnderSnapshot: true,
			},
			Rate:        &integrationv1.RateContract{RequestsPerMinute: 60, ConcurrentReads: 2},
			Idempotency: &integrationv1.IdempotencyContract{ReadsAreIdempotent: true},
			Observation: &integrationv1.ObservationContract{WatermarkField: "last_modified", FreshnessBudget: nil},
			Reconciliation: &integrationv1.ReconciliationContract{
				KeyFields: []string{"worker_id"}, SupportsPointRead: true, ComparableFields: []string{"compensation.base"},
			},
			Health: &integrationv1.HealthContract{ProbeObject: integrationv1.ObjectKind_OBJECT_KIND_WORKER, DegradedAfterFailures: 3},
		}

		observation := &integrationv1.ExternalObservation{
			ObservationId: "obs-1", TenantId: "tenant-1", ConnectionId: "conn-1",
			ConnectorId: "workday.hcm", ConnectorVersion: "1.0.0", SourceRef: "workday-prod",
			AuthorityRef: "authority.compensation.v1", Object: integrationv1.ObjectKind_OBJECT_KIND_WORKER,
			SchemaVersion: "1.0.0", SnapshotId: "snap-1", PageSequence: 1,
			RecordCount: 1, Complete: true, RetrievedAt: now, Watermark: now,
			Freshness:      integrationv1.Freshness_FRESHNESS_FRESH,
			Classification: integrationv1.ObservationClassification_OBSERVATION_CLASSIFICATION_EXTERNAL_OBSERVATION,
			ContentDigest:  "sha256:contentdigest",
		}

		explanation := &evidencev1.Explanation{
			IntentType: "hcmnext.intelligence.explain_transaction", IntentVersion: "v1",
			Transaction: subject, Disclosure: evidencev1.TransactionDisclosure_TRANSACTION_DISCLOSURE_PARTIAL,
			Presence: evidencev1.TransactionPresence_TRANSACTION_PRESENCE_PRESENT, AsKnownAt: now,
			Sections: []*evidencev1.SectionDisclosureRecord{
				{Section: evidencev1.Section_SECTION_REQUEST, Access: evidencev1.SectionAccess_SECTION_ACCESS_AUTHORIZED, Entries: 1},
				{Section: evidencev1.Section_SECTION_WRITE_SET, Access: evidencev1.SectionAccess_SECTION_ACCESS_DENIED, DenialReason: "not_authorized"},
			},
			LedgerHead:         42,
			EvidencePrecedence: "LEDGER_OVER_PROJECTION",
			Completeness:       &evidencev1.Completeness{Complete: false, Redactions: []string{"WRITE_SET"}},
			InputsDigest:       "sha256:in", ResultDigest: "sha256:out",
			EffectCounters: &evidencev1.EffectCounters{},
			Receipt: &evidencev1.ZeroEffectReceipt{
				IntentType: "hcmnext.intelligence.explain_transaction", IntentVersion: "v1",
				Mode: evidencev1.ReceiptMode_RECEIPT_MODE_SIMULATE, Counters: &evidencev1.EffectCounters{},
			},
		}

		assertGoldenRoundTrip(t, []proto.Message{diff, repairPlan, connectorDef, observation, explanation})
	})

	t.Run("BufGenerateIdempotent", func(t *testing.T) {
		assertBufGenerateIdempotent(t)
	})
}
