package integrationmeta_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documentmeta"
	"github.com/monstercameron/human-capital-management-suite/internal/data/integrationmeta"
	"github.com/monstercameron/human-capital-management-suite/internal/data/messagingmeta"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy/storagedisposition"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// migrationFile is the migration this package's tables come from. Every
// assertion that reaches outside the package names it, so a renumbering is a
// single-line change here rather than a scattered one.
const migrationFile = "00031_integration_messaging_document_metadata.sql"

var fixedInstant = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repo root")
		}
		dir = parent
	}
}

func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, key, "tenant "+key)
	return id
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

func inTenantTx(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	if err := inTenantTxErr(conn, tenant, fn); err != nil {
		t.Fatalf("tenant transaction: %v", err)
	}
}

func inTenantTxErr(conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

func digestOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// allTables is every base table migration 00031 creates, across the three
// store packages that own them.
func allTables() []string {
	out := slices.Clone(integrationmeta.IntegrationTables)
	out = append(out, messagingmeta.MessagingTables...)
	out = append(out, documentmeta.DocumentTables...)
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------------------
// fixtures
// ---------------------------------------------------------------------------

func newSystem(tenant uuid.UUID) integrationmeta.ExternalSystem {
	return integrationmeta.ExternalSystem{
		TenantID:           tenant,
		SystemID:           uuid.New(),
		SystemKey:          "system:" + uuid.NewString(),
		Vendor:             "acme",
		Product:            "payroll",
		Environment:        "PRODUCTION",
		ResidencyRegion:    "us-east",
		DataClassification: "CONFIDENTIAL",
		OwnerPrincipalRef:  "principal:owner",
		Lifecycle:          "ACTIVE",
		Health:             "HEALTHY",
		CreatedAt:          fixedInstant,
		UpdatedAt:          fixedInstant,
	}
}

func newConnector(tenant uuid.UUID) integrationmeta.ConnectorDefinition {
	return integrationmeta.ConnectorDefinition{
		TenantID:             tenant,
		ConnectorID:          uuid.New(),
		ConnectorKey:         "connector:" + uuid.NewString(),
		ConnectorVersion:     1,
		Vendor:               "acme",
		SupportedObjects:     json.RawMessage(`{"worker":true}`),
		Capabilities:         json.RawMessage(`{"read":true,"write":true}`),
		AuthMode:             "OAUTH2",
		WriteMode:            "IDEMPOTENT",
		IdempotencySemantics: "CLIENT_KEY",
		ObservationSemantics: "READ_AFTER_WRITE",
		DescriptorDigest:     digestOf("descriptor-" + uuid.NewString()),
		Status:               "CERTIFIED",
		CreatedAt:            fixedInstant,
	}
}

func newConnection(tenant, systemID, connectorID uuid.UUID) integrationmeta.ConnectorConnection {
	return integrationmeta.ConnectorConnection{
		TenantID:        tenant,
		ConnectionID:    uuid.New(),
		SystemID:        systemID,
		ConnectorID:     connectorID,
		Environment:     "PRODUCTION",
		Endpoint:        "https://acme.example/api",
		CredentialRef:   "vault://tenant/" + uuid.NewString(),
		ResidencyRegion: "us-east",
		Configuration:   json.RawMessage(`{"page_size":100}`),
		Generation:      1,
		Lifecycle:       "ACTIVE",
		Health:          "HEALTHY",
		CreatedAt:       fixedInstant,
		UpdatedAt:       fixedInstant,
	}
}

func newMapping(tenant uuid.UUID, connectionID *uuid.UUID) integrationmeta.MappingProfile {
	return integrationmeta.MappingProfile{
		TenantID:             tenant,
		MappingID:            uuid.New(),
		MappingKey:           "mapping:" + uuid.NewString(),
		MappingVersion:       1,
		ConnectionID:         connectionID,
		SourceSchemaKey:      "acme.worker.v2",
		DestinationSchemaKey: "hcm.person.v1",
		TransformLanguage:    "hcmmap",
		TransformVersion:     1,
		FieldRules:           json.RawMessage(`{"given_name":"first_name"}`),
		LossinessPolicy:      "LOSSLESS",
		ContentDigest:        digestOf("mapping-" + uuid.NewString()),
		PublicationState:     "PUBLISHED",
		EffectiveFrom:        fixedInstant,
		CreatedAt:            fixedInstant,
	}
}

func newReceipt(tenant, connectionID uuid.UUID) integrationmeta.IntegrationReceipt {
	return integrationmeta.IntegrationReceipt{
		TenantID:            tenant,
		ReceiptID:           uuid.New(),
		ConnectionID:        connectionID,
		ProviderEventID:     "evt-" + uuid.NewString(),
		DedupeKey:           "dedupe:" + uuid.NewString(),
		CorrelationKey:      "corr:" + uuid.NewString(),
		TrustProfileVersion: 1,
		AuthResult:          "AUTHENTICATED",
		SignatureResult:     "VALID",
		ReplayDisposition:   "FRESH",
		RawArtifactDigest:   digestOf("raw-" + uuid.NewString()),
		SchemaKey:           "acme.worker.v2",
		ParserVersion:       1,
		Classification:      "CONFIDENTIAL",
		ReceivedAt:          fixedInstant,
		TrustedAt:           fixedInstant.Add(time.Second),
		Disposition:         "ACCEPTED",
	}
}

func newItem(tenant, receiptID uuid.UUID, index int32, mappingID *uuid.UUID) integrationmeta.IntegrationReceiptItem {
	return integrationmeta.IntegrationReceiptItem{
		TenantID:       tenant,
		ItemID:         uuid.New(),
		ReceiptID:      receiptID,
		ItemIndex:      index,
		ItemPath:       fmt.Sprintf("$.workers[%d]", index),
		PayloadDigest:  digestOf("payload-" + uuid.NewString()),
		SchemaResult:   "VALID",
		CorrelationKey: "corr:" + uuid.NewString(),
		MappingID:      mappingID,
		Disposition:    "MAPPED",
		RecordedAt:     fixedInstant,
	}
}

func newExecution(tenant, mappingID, itemID uuid.UUID) integrationmeta.MappingExecution {
	return integrationmeta.MappingExecution{
		TenantID:            tenant,
		ExecutionID:         uuid.New(),
		MappingID:           mappingID,
		MappingVersion:      1,
		ItemID:              itemID,
		InputDigest:         digestOf("in-" + uuid.NewString()),
		OutputDigest:        digestOf("out-" + uuid.NewString()),
		FieldResults:        json.RawMessage(`{"given_name":"ok"}`),
		PresenceDiagnostics: json.RawMessage(`{"absent":[]}`),
		Lossiness:           "LOSSLESS",
		Status:              "APPLIED",
		ExecutedAt:          fixedInstant,
	}
}

func newOperation(tenant, connectionID uuid.UUID, sequence int64) integrationmeta.ConnectorOperation {
	return integrationmeta.ConnectorOperation{
		TenantID:               tenant,
		OperationID:            uuid.New(),
		ConnectionID:           connectionID,
		SequenceNo:             sequence,
		EffectRef:              "effect:" + uuid.NewString(),
		WorkflowRef:            "workflow:promotion",
		SemanticOperation:      "UPSERT_WORKER",
		ResourceKey:            "worker:" + uuid.NewString(),
		CanonicalPayloadDigest: digestOf("canonical-" + uuid.NewString()),
		MappedPayloadDigest:    digestOf("mapped-" + uuid.NewString()),
		IdempotencyKey:         "idem:" + uuid.NewString(),
		FenceToken:             1,
		AuthorityDigest:        digestOf("authority-" + uuid.NewString()),
		Classification:         "CONFIDENTIAL",
		DeadlineAt:             fixedInstant.Add(time.Hour),
		State:                  "PLANNED",
		CompletionState:        "PENDING",
		CreatedAt:              fixedInstant,
		UpdatedAt:              fixedInstant,
	}
}

func newAttempt(tenant, operationID uuid.UUID, number int32, result, retry string) integrationmeta.ConnectorOperationAttempt {
	received := fixedInstant.Add(time.Minute)
	return integrationmeta.ConnectorOperationAttempt{
		TenantID:          tenant,
		AttemptID:         uuid.New(),
		OperationID:       operationID,
		AttemptNumber:     number,
		RequestDigest:     digestOf("req-" + uuid.NewString()),
		ProviderRequestID: "prq-" + uuid.NewString(),
		FenceToken:        1,
		AttemptedAt:       fixedInstant,
		ReceivedAt:        &received,
		ProviderResult:    result,
		RetryDisposition:  retry,
	}
}

func newObservation(tenant, operationID uuid.UUID) integrationmeta.ExternalOperationObservation {
	return integrationmeta.ExternalOperationObservation{
		TenantID:          tenant,
		ObservationID:     uuid.New(),
		OperationID:       operationID,
		ResourceKey:       "worker:" + uuid.NewString(),
		ObservedState:     json.RawMessage(`{"status":"ACTIVE"}`),
		ObservedDigest:    digestOf("observed-" + uuid.NewString()),
		ProviderVersion:   "v7",
		Watermark:         "wm-7",
		Completeness:      "CURRENT",
		Authority:         "AUTHORITATIVE",
		ObservedAt:        fixedInstant.Add(2 * time.Minute),
		ReceivedAt:        fixedInstant.Add(3 * time.Minute),
		FreshnessDeadline: fixedInstant.Add(time.Hour),
	}
}

func newJob(tenant, connectionID uuid.UUID) integrationmeta.ReconciliationJob {
	return integrationmeta.ReconciliationJob{
		TenantID:     tenant,
		JobID:        uuid.New(),
		ConnectionID: connectionID,
		ObjectKey:    "worker",
		Direction:    "OUTBOUND",
		Mode:         "DELTA",
		CursorToken:  "cursor-1",
		Watermark:    "wm-7",
		Counts:       json.RawMessage(`{"compared":10}`),
		StartedAt:    fixedInstant,
		Status:       "RUNNING",
	}
}

func newResult(tenant, jobID uuid.UUID, comparison string, discrepancies int32) integrationmeta.ReconciliationResult {
	observed := digestOf("observed-" + uuid.NewString())
	return integrationmeta.ReconciliationResult{
		TenantID:         tenant,
		ResultID:         uuid.New(),
		JobID:            jobID,
		ResourceKey:      "worker:" + uuid.NewString(),
		ExpectedDigest:   digestOf("expected-" + uuid.NewString()),
		ObservedDigest:   &observed,
		ComparisonResult: comparison,
		DiscrepancyCount: discrepancies,
		Severity:         "LOW",
		RecordedAt:       fixedInstant,
	}
}

// chain writes one full inbound-then-outbound flow and returns its ids.
type chain struct {
	systemID, connectorID, connectionID, mappingID uuid.UUID
	receiptID, itemID, executionID                 uuid.UUID
	operationID, attemptID, observationID          uuid.UUID
	jobID, resultID                                uuid.UUID
}

func writeChain(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID) chain {
	t.Helper()
	ctx := context.Background()
	var c chain
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		system := newSystem(tenant)
		if err := integrationmeta.InsertExternalSystem(ctx, tx, system); err != nil {
			return err
		}
		connector := newConnector(tenant)
		if err := integrationmeta.InsertConnectorDefinition(ctx, tx, connector); err != nil {
			return err
		}
		connection := newConnection(tenant, system.SystemID, connector.ConnectorID)
		if err := integrationmeta.InsertConnectorConnection(ctx, tx, connection); err != nil {
			return err
		}
		mapping := newMapping(tenant, &connection.ConnectionID)
		if err := integrationmeta.InsertMappingProfile(ctx, tx, mapping); err != nil {
			return err
		}
		receipt := newReceipt(tenant, connection.ConnectionID)
		if err := integrationmeta.InsertIntegrationReceipt(ctx, tx, receipt); err != nil {
			return err
		}
		item := newItem(tenant, receipt.ReceiptID, 0, &mapping.MappingID)
		if err := integrationmeta.InsertIntegrationReceiptItem(ctx, tx, item); err != nil {
			return err
		}
		execution := newExecution(tenant, mapping.MappingID, item.ItemID)
		if err := integrationmeta.InsertMappingExecution(ctx, tx, execution); err != nil {
			return err
		}
		operation := newOperation(tenant, connection.ConnectionID, 1)
		operation.MappingID = &mapping.MappingID
		if err := integrationmeta.InsertConnectorOperation(ctx, tx, operation); err != nil {
			return err
		}
		attempt := newAttempt(tenant, operation.OperationID, 1, "SUCCESS", "NONE")
		if err := integrationmeta.InsertConnectorOperationAttempt(ctx, tx, attempt); err != nil {
			return err
		}
		observation := newObservation(tenant, operation.OperationID)
		if err := integrationmeta.InsertExternalOperationObservation(ctx, tx, observation); err != nil {
			return err
		}
		job := newJob(tenant, connection.ConnectionID)
		if err := integrationmeta.InsertReconciliationJob(ctx, tx, job); err != nil {
			return err
		}
		result := newResult(tenant, job.JobID, "PASS", 0)
		if err := integrationmeta.InsertReconciliationResult(ctx, tx, result); err != nil {
			return err
		}
		c = chain{
			systemID: system.SystemID, connectorID: connector.ConnectorID,
			connectionID: connection.ConnectionID, mappingID: mapping.MappingID,
			receiptID: receipt.ReceiptID, itemID: item.ItemID, executionID: execution.ExecutionID,
			operationID: operation.OperationID, attemptID: attempt.AttemptID,
			observationID: observation.ObservationID, jobID: job.JobID, resultID: result.ResultID,
		}
		return nil
	})
	return c
}

// ---------------------------------------------------------------------------
// TestTodo_DB_014 -- the primary test
// ---------------------------------------------------------------------------

func TestTodo_DB_014(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "db014-primary")

	t.Run("migration 00031 creates exactly its declared tables", func(t *testing.T) {
		inv, err := integrationmeta.Inspect(ctx, db.Conn, allTables())
		if err != nil {
			t.Fatalf("inspect: %v", err)
		}
		if !inv.Exact() {
			t.Fatalf("tables missing from the live schema: %v", inv.Missing)
		}
		if len(inv.Present) != 25 {
			t.Fatalf("migration 00031 declares %d tables, want 25: %v", len(inv.Present), inv.Present)
		}
	})

	t.Run("no table another migration owns is redefined here", func(t *testing.T) {
		mine := allTables()
		for _, foreign := range integrationmeta.ForeignTables {
			if integrationmeta.Contains(mine, foreign) {
				t.Errorf("table %s is owned by another migration but appears in DB-014's own set", foreign)
			}
		}
	})

	t.Run("every table declares its tenant and forces row level security", func(t *testing.T) {
		for _, table := range allTables() {
			var nullable string
			err := db.Conn.QueryRow(ctx, `SELECT is_nullable FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=$1 AND column_name='tenant_id'`, table).Scan(&nullable)
			if err != nil {
				t.Errorf("table %s has no tenant_id column: %v", table, err)
				continue
			}
			if nullable != "NO" {
				t.Errorf("table %s tenant_id is nullable", table)
			}
			var enabled, forced bool
			if err := db.Conn.QueryRow(ctx, `SELECT relrowsecurity, relforcerowsecurity FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relname=$1`, table).Scan(&enabled, &forced); err != nil {
				t.Errorf("pg_class for %s: %v", table, err)
				continue
			}
			if !enabled || !forced {
				t.Errorf("table %s row level security enabled=%v forced=%v, want both true", table, enabled, forced)
			}
			var policy string
			if err := db.Conn.QueryRow(ctx, `SELECT polname FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relname=$1`, table).Scan(&policy); err != nil {
				t.Errorf("no policy on %s: %v", table, err)
				continue
			}
			if policy != "tenant_isolation" {
				t.Errorf("table %s policy is %s, want tenant_isolation", table, policy)
			}
		}
	})

	t.Run("the inbound and outbound chain round trips", func(t *testing.T) {
		c := writeChain(t, conn, tenant)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			receipt, err := integrationmeta.LoadIntegrationReceipt(ctx, tx, tenant, c.receiptID)
			if err != nil {
				return err
			}
			if receipt.Disposition != "ACCEPTED" || receipt.CorrelationKey == "" {
				return fmt.Errorf("receipt %+v lost its disposition or correlation", receipt)
			}
			exec, err := integrationmeta.LoadMappingExecution(ctx, tx, tenant, c.executionID)
			if err != nil {
				return err
			}
			if exec.MappingVersion != 1 {
				return fmt.Errorf("mapping execution lost its mapping version")
			}
			op, err := integrationmeta.LoadConnectorOperation(ctx, tx, tenant, c.operationID)
			if err != nil {
				return err
			}
			if op.IdempotencyKey == "" || op.DeadlineAt.IsZero() || op.Classification == "" {
				return fmt.Errorf("operation %+v lost its idempotency key, deadline or classification", op)
			}
			obs, err := integrationmeta.LoadExternalOperationObservation(ctx, tx, tenant, c.observationID)
			if err != nil {
				return err
			}
			if !obs.FreshnessDeadline.After(obs.ObservedAt) {
				return fmt.Errorf("observation lost its freshness horizon")
			}
			return nil
		})
	})

	t.Run("provider acceptance is not business completion", func(t *testing.T) {
		c := writeChain(t, conn, tenant)

		// A successful attempt alone leaves the operation PENDING.
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			op, err := integrationmeta.LoadConnectorOperation(ctx, tx, tenant, c.operationID)
			if err != nil {
				return err
			}
			if op.CompletionState != "PENDING" {
				return fmt.Errorf("operation completion_state=%s after a successful attempt, want PENDING", op.CompletionState)
			}
			return nil
		})

		// Completing with no observation is refused before any SQL runs.
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return integrationmeta.CompleteOperation(ctx, tx, tenant, c.operationID, uuid.Nil, fixedInstant)
		})
		if !errors.Is(err, integrationmeta.ErrProviderAcceptanceNotCompletion) {
			t.Fatalf("completing without an observation returned %v, want ErrProviderAcceptanceNotCompletion", err)
		}

		// Completing with another operation's observation is refused too.
		other := writeChain(t, conn, tenant)
		err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return integrationmeta.CompleteOperation(ctx, tx, tenant, c.operationID, other.observationID, fixedInstant)
		})
		if !errors.Is(err, integrationmeta.ErrProviderAcceptanceNotCompletion) {
			t.Fatalf("completing with a foreign observation returned %v, want ErrProviderAcceptanceNotCompletion", err)
		}

		// A raw UPDATE that skips the Go layer is refused by the schema.
		if err := db.ExecErr(`UPDATE connector_operation SET completion_state='COMPLETE' WHERE tenant_id=$1 AND operation_id=$2`, tenant, c.operationID); err == nil {
			t.Fatal("a raw UPDATE marked an operation COMPLETE with no observation")
		}

		// With its own observation it completes.
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return integrationmeta.CompleteOperation(ctx, tx, tenant, c.operationID, c.observationID, fixedInstant.Add(time.Hour))
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			op, err := integrationmeta.LoadConnectorOperation(ctx, tx, tenant, c.operationID)
			if err != nil {
				return err
			}
			if op.CompletionState != "COMPLETE" || op.ObservationID == nil || *op.ObservationID != c.observationID {
				return fmt.Errorf("operation %+v did not complete against its observation", op)
			}
			return nil
		})
	})

	t.Run("an ambiguous provider answer routes to observation or repair", func(t *testing.T) {
		c := writeChain(t, conn, tenant)
		bad := newAttempt(tenant, c.operationID, 2, "AMBIGUOUS", "RETRY")
		if err := bad.Validate(); !errors.Is(err, integrationmeta.ErrProviderAcceptanceNotCompletion) {
			t.Fatalf("an AMBIGUOUS attempt routed to RETRY validated as %v", err)
		}
		if err := db.ExecErr(`INSERT INTO connector_operation_attempt (tenant_id, attempt_id, operation_id, attempt_number, request_digest, fence_token, attempted_at, provider_result, retry_disposition) VALUES ($1,$2,$3,2,$4,1,now(),'AMBIGUOUS','RETRY')`,
			tenant, uuid.New(), c.operationID, digestOf("req")); err == nil {
			t.Fatal("the schema accepted an AMBIGUOUS attempt routed to RETRY")
		}
		good := newAttempt(tenant, c.operationID, 2, "AMBIGUOUS", "OBSERVATION_REQUIRED")
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return integrationmeta.InsertConnectorOperationAttempt(ctx, tx, good)
		})
	})

	t.Run("dedupe and idempotency keys are unique in their scope", func(t *testing.T) {
		c := writeChain(t, conn, tenant)
		first, err := func() (integrationmeta.IntegrationReceipt, error) {
			var r integrationmeta.IntegrationReceipt
			e := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
				var err error
				r, err = integrationmeta.LoadIntegrationReceipt(ctx, tx, tenant, c.receiptID)
				return err
			})
			return r, e
		}()
		if err != nil {
			t.Fatalf("load receipt: %v", err)
		}
		replay := newReceipt(tenant, c.connectionID)
		replay.DedupeKey = first.DedupeKey
		if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return integrationmeta.InsertIntegrationReceipt(ctx, tx, replay)
		}); err == nil {
			t.Fatal("a replayed provider delivery became a second receipt")
		}

		var idem string
		if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			op, err := integrationmeta.LoadConnectorOperation(ctx, tx, tenant, c.operationID)
			idem = op.IdempotencyKey
			return err
		}); err != nil {
			t.Fatalf("load operation: %v", err)
		}
		duplicate := newOperation(tenant, c.connectionID, 2)
		duplicate.IdempotencyKey = idem
		if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return integrationmeta.InsertConnectorOperation(ctx, tx, duplicate)
		}); err == nil {
			t.Fatal("a second operation reused an idempotency key on the same connection")
		}
	})

	t.Run("the effect graph records causation without a self edge", func(t *testing.T) {
		c := writeChain(t, conn, tenant)
		successor := newOperation(tenant, c.connectionID, 2)
		successor.CausalPredecessorID = &c.operationID
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return integrationmeta.InsertConnectorOperation(ctx, tx, successor)
		})
		selfCaused := newOperation(tenant, c.connectionID, 3)
		selfCaused.CausalPredecessorID = &selfCaused.OperationID
		if err := selfCaused.Validate(); err == nil {
			t.Fatal("an operation validated as its own causal predecessor")
		}
	})

	t.Run("a reconciliation result cannot pass with discrepancies", func(t *testing.T) {
		c := writeChain(t, conn, tenant)
		bad := newResult(tenant, c.jobID, "PASS", 3)
		if err := bad.Validate(); err == nil {
			t.Fatal("a PASS result with three discrepancies validated")
		}
		if err := db.ExecErr(`INSERT INTO reconciliation_result (tenant_id, result_id, job_id, resource_key, expected_digest, comparison_result, discrepancy_count, severity, recorded_at) VALUES ($1,$2,$3,'worker:x',$4,'PASS',3,'LOW',now())`,
			tenant, uuid.New(), c.jobID, digestOf("expected")); err == nil {
			t.Fatal("the schema accepted a PASS result with three discrepancies")
		}
	})
}

// ---------------------------------------------------------------------------
// TestTodo_DB_014_Golden -- the pinned inventory
// ---------------------------------------------------------------------------

func TestTodo_DB_014_Golden(t *testing.T) {
	t.Parallel()
	expected := []string{
		"connector_connection",
		"connector_definition",
		"connector_operation",
		"connector_operation_attempt",
		"conversation_thread",
		"delivery_attempt",
		"delivery_endpoint",
		"delivery_receipt",
		"document",
		"document_artifact_reference",
		"document_template",
		"document_version",
		"external_operation_observation",
		"external_system",
		"integration_receipt",
		"integration_receipt_item",
		"mapping_execution",
		"mapping_profile",
		"message_intent",
		"recipient_message",
		"reconciliation_job",
		"reconciliation_result",
		"signature",
		"signature_request",
		"thread_participant",
	}
	if got := allTables(); !slices.Equal(got, expected) {
		t.Fatalf("DB-014 table set is %v, want %v", got, expected)
	}

	db := pgtest.New(t)
	ctx := context.Background()

	t.Run("every append-only table carries the forbid_mutation trigger", func(t *testing.T) {
		for _, table := range integrationmeta.AppendOnlyTables {
			names, err := integrationmeta.TriggerNames(ctx, db.Conn, table)
			if err != nil {
				t.Fatalf("triggers on %s: %v", table, err)
			}
			if !integrationmeta.Contains(names, table+"_append_only") {
				t.Errorf("table %s carries triggers %v, want %s_append_only", table, names, table)
			}
		}
	})

	t.Run("storage disposition rows, once registered, name migration 00031", func(t *testing.T) {
		// The registry lives under definitions/, which this lane may not
		// edit; the rows DB-014 needs are reported rather than written. This
		// assertion is therefore forward compatible: it says nothing while
		// the rows are absent and pins their migration once they land, so
		// the registering commit cannot attribute them to the wrong file.
		root := repoRoot(t)
		reg, err := storagedisposition.Load(filepath.Join(root, "definitions", "storage", "storage-disposition.yaml"))
		if err != nil {
			t.Fatalf("load storage-disposition registry: %v", err)
		}
		registered := 0
		for _, table := range allTables() {
			entry, ok := reg.Lookup(table)
			if !ok {
				continue
			}
			registered++
			if entry.Migration != migrationFile {
				t.Errorf("table %s is registered against %s, want %s", table, entry.Migration, migrationFile)
			}
			if !entry.TenantScoped() {
				t.Errorf("table %s is registered without a tenant scoping column", table)
			}
			wantAppendOnly := integrationmeta.Contains(integrationmeta.AppendOnlyTables, table)
			if entry.AppendOnly != wantAppendOnly {
				t.Errorf("table %s registered append_only=%v, want %v", table, entry.AppendOnly, wantAppendOnly)
			}
		}
		t.Logf("%d of %d DB-014 tables are registered in storage-disposition.yaml", registered, len(allTables()))
	})
}

// ---------------------------------------------------------------------------
// TestTodo_DB_014_Security
// ---------------------------------------------------------------------------

func TestTodo_DB_014_Security(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	alpha := insertTenant(t, db, "db014-alpha")
	beta := insertTenant(t, db, "db014-beta")

	t.Run("a credential is a reference, never a secret", func(t *testing.T) {
		c := writeChain(t, conn, alpha)
		raw := newConnection(alpha, c.systemID, c.connectorID)
		raw.CredentialRef = "hunter2"
		if err := raw.Validate(); !errors.Is(err, integrationmeta.ErrRawSecret) {
			t.Fatalf("a bare secret validated as %v, want ErrRawSecret", err)
		}
		if err := db.ExecErr(`INSERT INTO connector_connection (tenant_id, connection_id, system_id, connector_id, environment, endpoint, credential_ref, residency_region, generation, lifecycle, health) VALUES ($1,$2,$3,$4,'PRODUCTION','https://x','hunter2','us-east',1,'ACTIVE','HEALTHY')`,
			alpha, uuid.New(), c.systemID, c.connectorID); err == nil {
			t.Fatal("the schema accepted a bare secret as a credential reference")
		}
	})

	t.Run("a secret cannot hide in the configuration object", func(t *testing.T) {
		c := writeChain(t, conn, alpha)
		for _, key := range []string{"secret", "password", "token", "api_key", "client_secret", "private_key"} {
			smuggled := newConnection(alpha, c.systemID, c.connectorID)
			smuggled.Configuration = json.RawMessage(fmt.Sprintf(`{%q:"value"}`, key))
			if err := smuggled.Validate(); !errors.Is(err, integrationmeta.ErrRawSecret) {
				t.Errorf("configuration key %q validated as %v, want ErrRawSecret", key, err)
			}
			if err := db.ExecErr(`INSERT INTO connector_connection (tenant_id, connection_id, system_id, connector_id, environment, endpoint, credential_ref, residency_region, configuration, generation, lifecycle, health) VALUES ($1,$2,$3,$4,'PRODUCTION','https://x','vault://x','us-east',$5::jsonb,1,'ACTIVE','HEALTHY')`,
				alpha, uuid.New(), c.systemID, c.connectorID, fmt.Sprintf(`{%q:"value"}`, key)); err == nil {
				t.Errorf("the schema accepted a configuration carrying %q", key)
			}
		}
	})

	t.Run("one tenant cannot read or reference another tenant's rows", func(t *testing.T) {
		alphaChain := writeChain(t, conn, alpha)
		_ = writeChain(t, conn, beta)

		// Beta's session cannot see alpha's connection at all.
		err := inTenantTxErr(conn, beta, func(tx dbport.Tx) error {
			_, err := integrationmeta.LoadConnectorConnection(ctx, tx, alpha, alphaChain.connectionID)
			return err
		})
		if !errors.Is(err, dbport.ErrNoRows) {
			t.Fatalf("beta reading alpha's connection returned %v, want ErrNoRows", err)
		}

		// Nor can beta hang an operation off it: the composite foreign key
		// carries tenant_id, so the reference simply does not exist.
		crossTenant := newOperation(beta, alphaChain.connectionID, 1)
		if err := inTenantTxErr(conn, beta, func(tx dbport.Tx) error {
			return integrationmeta.InsertConnectorOperation(ctx, tx, crossTenant)
		}); err == nil {
			t.Fatal("beta hung an operation off alpha's connection")
		}
	})

	t.Run("an unauthenticated payload is never accepted", func(t *testing.T) {
		c := writeChain(t, conn, alpha)
		unsigned := newReceipt(alpha, c.connectionID)
		unsigned.AuthResult = "UNSIGNED"
		unsigned.SignatureResult = "ABSENT"
		unsigned.Disposition = "ACCEPTED"
		if err := unsigned.Validate(); err == nil {
			t.Fatal("an unsigned payload validated as ACCEPTED")
		}
		if err := db.ExecErr(`INSERT INTO integration_receipt (tenant_id, receipt_id, connection_id, provider_event_id, dedupe_key, correlation_key, trust_profile_version, auth_result, signature_result, replay_disposition, raw_artifact_digest, schema_key, parser_version, classification, received_at, trusted_at, disposition) VALUES ($1,$2,$3,'e','d-'||$2::text,'c',1,'UNSIGNED','ABSENT','FRESH',$4,'s',1,'CONFIDENTIAL',now(),now(),'ACCEPTED')`,
			alpha, uuid.New(), c.connectionID, digestOf("raw")); err == nil {
			t.Fatal("the schema accepted an unsigned payload as ACCEPTED")
		}
	})
}

// ---------------------------------------------------------------------------
// TestTodo_DB_014_Mutation
// ---------------------------------------------------------------------------

func TestTodo_DB_014_Mutation(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "db014-mutation")
	c := writeChain(t, conn, tenant)

	cases := []struct {
		table  string
		column string
		key    string
		id     uuid.UUID
	}{
		{"integration_receipt", "classification", "receipt_id", c.receiptID},
		{"integration_receipt_item", "item_path", "item_id", c.itemID},
		{"mapping_execution", "lossiness", "execution_id", c.executionID},
		{"connector_operation_attempt", "provider_request_id", "attempt_id", c.attemptID},
		{"external_operation_observation", "watermark", "observation_id", c.observationID},
		{"reconciliation_result", "severity", "result_id", c.resultID},
	}
	for _, tc := range cases {
		t.Run(tc.table+" rejects UPDATE", func(t *testing.T) {
			sql := fmt.Sprintf(`UPDATE %s SET %s='rewritten' WHERE tenant_id=$1 AND %s=$2`, tc.table, tc.column, tc.key)
			if err := db.ExecErr(sql, tenant, tc.id); err == nil {
				t.Fatalf("an append-only %s row was rewritten", tc.table)
			}
		})
		t.Run(tc.table+" rejects DELETE", func(t *testing.T) {
			sql := fmt.Sprintf(`DELETE FROM %s WHERE tenant_id=$1 AND %s=$2`, tc.table, tc.key)
			if err := db.ExecErr(sql, tenant, tc.id); err == nil {
				t.Fatalf("an append-only %s row was deleted", tc.table)
			}
		})
	}

	t.Run("live serving state still updates", func(t *testing.T) {
		if err := db.ExecErr(`UPDATE connector_connection SET health='DEGRADED' WHERE tenant_id=$1 AND connection_id=$2`, tenant, c.connectionID); err != nil {
			t.Fatalf("connector_connection is live serving state and must stay updatable: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// TestTodo_DB_014_Property
// ---------------------------------------------------------------------------

func TestTodo_DB_014_Property(t *testing.T) {
	t.Parallel()

	t.Run("no credential scheme outside the governed set is ever accepted", func(t *testing.T) {
		accepted := []string{"vault://a/b", "kms://key/1", "secretref://x", "keyring://y"}
		refused := []string{"", "hunter2", "http://x", "vault:/a", "vaults://a", "vault://", "VAULT://a", "vault:// a"}
		base := newConnection(uuid.New(), uuid.New(), uuid.New())
		for _, ref := range accepted {
			c := base
			c.CredentialRef = ref
			if err := c.Validate(); err != nil {
				t.Errorf("credential %q was refused: %v", ref, err)
			}
		}
		for _, ref := range refused {
			c := base
			c.CredentialRef = ref
			if err := c.Validate(); !errors.Is(err, integrationmeta.ErrRawSecret) {
				t.Errorf("credential %q returned %v, want ErrRawSecret", ref, err)
			}
		}
	})

	t.Run("completion always implies an observation", func(t *testing.T) {
		observation := uuid.New()
		for _, completion := range []string{"PENDING", "COMPLETE", "ABANDONED"} {
			for _, withObservation := range []bool{false, true} {
				op := newOperation(uuid.New(), uuid.New(), 1)
				op.CompletionState = completion
				if withObservation {
					op.ObservationID = &observation
				}
				err := op.Validate()
				wantRefusal := completion == "COMPLETE" && !withObservation
				if wantRefusal != errors.Is(err, integrationmeta.ErrProviderAcceptanceNotCompletion) {
					t.Errorf("completion=%s observation=%v returned %v", completion, withObservation, err)
				}
			}
		}
	})

	t.Run("a comparison result and its discrepancy count never disagree", func(t *testing.T) {
		for _, comparison := range []string{"PASS", "FAIL", "PARTIAL", "UNKNOWN"} {
			for _, count := range []int32{0, 1, 7} {
				r := newResult(uuid.New(), uuid.New(), comparison, count)
				err := r.Validate()
				impossible := (comparison == "PASS" && count != 0) || (comparison == "FAIL" && count == 0)
				if impossible != (err != nil) {
					t.Errorf("comparison=%s count=%d returned %v", comparison, count, err)
				}
			}
		}
	})

	t.Run("a freshness horizon always follows the observation it bounds", func(t *testing.T) {
		for _, offset := range []time.Duration{-time.Hour, 0, time.Nanosecond, time.Hour} {
			o := newObservation(uuid.New(), uuid.New())
			o.FreshnessDeadline = o.ObservedAt.Add(offset)
			err := o.Validate()
			if (offset > 0) == (err != nil) {
				t.Errorf("freshness offset %v returned %v", offset, err)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// TestTodo_DB_014_Race
// ---------------------------------------------------------------------------

func TestTodo_DB_014_Race(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	setup := appConn(t, db)
	tenant := insertTenant(t, db, "db014-race")
	c := writeChain(t, setup, tenant)

	const writers = 6
	sharedKey := "idem:" + uuid.NewString()
	var wins, losses atomic.Int64
	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	for i := range writers {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			conn := appConn(t, db)
			op := newOperation(tenant, c.connectionID, int64(i+10))
			op.IdempotencyKey = sharedKey
			start.Wait()
			err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
				return integrationmeta.InsertConnectorOperation(ctx, tx, op)
			})
			if err == nil {
				wins.Add(1)
			} else {
				losses.Add(1)
			}
		}(i)
	}
	start.Done()
	done.Wait()

	if wins.Load() != 1 {
		t.Fatalf("%d of %d concurrent writers claimed the same idempotency key, want exactly 1", wins.Load(), writers)
	}
	if losses.Load() != writers-1 {
		t.Fatalf("%d writers were refused, want %d", losses.Load(), writers-1)
	}

	var count int64
	if err := db.QueryRow(ctx, `SELECT count(*) FROM connector_operation WHERE tenant_id=$1 AND idempotency_key=$2`, tenant, sharedKey).Scan(&count); err != nil {
		t.Fatalf("count operations: %v", err)
	}
	if count != 1 {
		t.Fatalf("%d operations hold the shared idempotency key, want 1", count)
	}
}

// ---------------------------------------------------------------------------
// TestTodo_DB_014_Integration
// ---------------------------------------------------------------------------

func TestTodo_DB_014_Integration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	writer := appConn(t, db)
	tenant := insertTenant(t, db, "db014-integration")
	c := writeChain(t, writer, tenant)
	inTenantTx(t, writer, tenant, func(tx dbport.Tx) error {
		return integrationmeta.CompleteOperation(ctx, tx, tenant, c.operationID, c.observationID, fixedInstant.Add(time.Hour))
	})

	// Everything above is committed. A brand new connection must be able to
	// reconstruct the whole flow from durable state alone.
	reader := appConn(t, db)
	inTenantTx(t, reader, tenant, func(tx dbport.Tx) error {
		if _, err := integrationmeta.LoadExternalSystem(ctx, tx, tenant, c.systemID); err != nil {
			return fmt.Errorf("external_system: %w", err)
		}
		if _, err := integrationmeta.LoadConnectorDefinition(ctx, tx, tenant, c.connectorID); err != nil {
			return fmt.Errorf("connector_definition: %w", err)
		}
		conn, err := integrationmeta.LoadConnectorConnection(ctx, tx, tenant, c.connectionID)
		if err != nil {
			return fmt.Errorf("connector_connection: %w", err)
		}
		if conn.CredentialRef == "" || conn.Generation < 1 {
			return fmt.Errorf("connection lost its credential reference or generation")
		}
		if _, err := integrationmeta.LoadMappingProfile(ctx, tx, tenant, c.mappingID); err != nil {
			return fmt.Errorf("mapping_profile: %w", err)
		}
		if _, err := integrationmeta.LoadIntegrationReceipt(ctx, tx, tenant, c.receiptID); err != nil {
			return fmt.Errorf("integration_receipt: %w", err)
		}
		if _, err := integrationmeta.LoadIntegrationReceiptItem(ctx, tx, tenant, c.itemID); err != nil {
			return fmt.Errorf("integration_receipt_item: %w", err)
		}
		if _, err := integrationmeta.LoadMappingExecution(ctx, tx, tenant, c.executionID); err != nil {
			return fmt.Errorf("mapping_execution: %w", err)
		}
		op, err := integrationmeta.LoadConnectorOperation(ctx, tx, tenant, c.operationID)
		if err != nil {
			return fmt.Errorf("connector_operation: %w", err)
		}
		if op.CompletionState != "COMPLETE" || op.ObservationID == nil {
			return fmt.Errorf("completion did not survive the commit: %+v", op)
		}
		if _, err := integrationmeta.LoadConnectorOperationAttempt(ctx, tx, tenant, c.attemptID); err != nil {
			return fmt.Errorf("connector_operation_attempt: %w", err)
		}
		if _, err := integrationmeta.LoadExternalOperationObservation(ctx, tx, tenant, c.observationID); err != nil {
			return fmt.Errorf("external_operation_observation: %w", err)
		}
		if _, err := integrationmeta.LoadReconciliationJob(ctx, tx, tenant, c.jobID); err != nil {
			return fmt.Errorf("reconciliation_job: %w", err)
		}
		if _, err := integrationmeta.LoadReconciliationResult(ctx, tx, tenant, c.resultID); err != nil {
			return fmt.Errorf("reconciliation_result: %w", err)
		}
		return nil
	})
}
