package messagingmeta_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/messagingmeta"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var fixedInstant = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

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

func newIntent(tenant uuid.UUID, requirement string) messagingmeta.MessageIntent {
	expiry := fixedInstant.Add(48 * time.Hour)
	return messagingmeta.MessageIntent{
		TenantID:            tenant,
		MessageIntentID:     uuid.New(),
		Purpose:             "LEGAL_NOTICE",
		AudienceExpression:  json.RawMessage(`{"population":"all-employees"}`),
		TemplateKey:         "tpl:" + uuid.NewString(),
		TemplateVersion:     1,
		Classification:      "CONFIDENTIAL",
		Urgency:             "HIGH",
		DeliveryRequirement: requirement,
		ResponseRequirement: "NONE",
		WorkflowRef:         "workflow:notice",
		CorrelationKey:      "corr:" + uuid.NewString(),
		ExpiresAt:           &expiry,
		CreatedAt:           fixedInstant,
	}
}

func newEndpoint(tenant uuid.UUID) messagingmeta.DeliveryEndpoint {
	return messagingmeta.DeliveryEndpoint{
		TenantID:          tenant,
		EndpointID:        uuid.New(),
		PrincipalRef:      "principal:" + uuid.NewString(),
		Channel:           "EMAIL",
		AddressDigest:     digestOf("address-" + uuid.NewString()),
		AddressHint:       "j***@example.com",
		ProviderAccount:   "acct-1",
		Ownership:         "BUSINESS",
		VerificationState: "VERIFIED",
		PurposeScope:      json.RawMessage(`{"legal":true}`),
		Locale:            "en-US",
		EffectiveFrom:     fixedInstant,
		Status:            "ACTIVE",
	}
}

func newRecipientMessage(tenant, intentID, endpointID uuid.UUID) messagingmeta.RecipientMessage {
	return messagingmeta.RecipientMessage{
		TenantID:           tenant,
		RecipientMessageID: uuid.New(),
		MessageIntentID:    intentID,
		RecipientRef:       "principal:" + uuid.NewString(),
		EndpointID:         endpointID,
		RenderedDigest:     digestOf("rendered-" + uuid.NewString()),
		Classification:     "CONFIDENTIAL",
		CorrelationKey:     "corr:" + uuid.NewString(),
		RecipientState:     "UNSEEN",
		SatisfactionState:  "PENDING",
		CreatedAt:          fixedInstant,
		UpdatedAt:          fixedInstant,
	}
}

func newAttempt(tenant, messageID, endpointID uuid.UUID, state string) messagingmeta.DeliveryAttempt {
	return messagingmeta.DeliveryAttempt{
		TenantID:           tenant,
		AttemptID:          uuid.New(),
		RecipientMessageID: messageID,
		EndpointID:         endpointID,
		Provider:           "postmark",
		IdempotencyKey:     "idem:" + uuid.NewString(),
		PayloadDigest:      digestOf("payload-" + uuid.NewString()),
		State:              state,
		ProviderMessageID:  "pm-" + uuid.NewString(),
		SubmittedAt:        fixedInstant,
		DeadlineAt:         fixedInstant.Add(time.Hour),
	}
}

func newReceipt(tenant, attemptID uuid.UUID, normalized, signature string) messagingmeta.DeliveryReceipt {
	return messagingmeta.DeliveryReceipt{
		TenantID:          tenant,
		ReceiptID:         uuid.New(),
		AttemptID:         attemptID,
		ProviderEventID:   "evt-" + uuid.NewString(),
		EventType:         "delivery",
		EventTime:         fixedInstant.Add(time.Minute),
		ReceivedAt:        fixedInstant.Add(2 * time.Minute),
		SignatureResult:   signature,
		SequenceNo:        1,
		DedupeState:       "FIRST",
		NormalizedResult:  normalized,
		RawArtifactDigest: digestOf("raw-" + uuid.NewString()),
	}
}

type thread struct {
	intentID, endpointID, messageID, attemptID uuid.UUID
}

func writeThread(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, requirement, attemptState string) thread {
	t.Helper()
	ctx := context.Background()
	var th thread
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		intent := newIntent(tenant, requirement)
		if err := messagingmeta.InsertMessageIntent(ctx, tx, intent); err != nil {
			return err
		}
		endpoint := newEndpoint(tenant)
		if err := messagingmeta.InsertDeliveryEndpoint(ctx, tx, endpoint); err != nil {
			return err
		}
		message := newRecipientMessage(tenant, intent.MessageIntentID, endpoint.EndpointID)
		if err := messagingmeta.InsertRecipientMessage(ctx, tx, message); err != nil {
			return err
		}
		attempt := newAttempt(tenant, message.RecipientMessageID, endpoint.EndpointID, attemptState)
		if err := messagingmeta.InsertDeliveryAttempt(ctx, tx, attempt); err != nil {
			return err
		}
		th = thread{intent.MessageIntentID, endpoint.EndpointID, message.RecipientMessageID, attempt.AttemptID}
		return nil
	})
	return th
}

// TestTodo_DB_014_Messaging is DB-014's communication-side test. The matrix
// names live in internal/data/integrationmeta (the lane's primary package);
// this suite proves the same clauses for the seven tables migration 00031
// creates for messaging, which that package does not own.
func TestTodo_DB_014_Messaging(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "db014-messaging")
	other := insertTenant(t, db, "db014-messaging-other")

	t.Run("the messaging table set is exactly what 00031 declares", func(t *testing.T) {
		want := []string{
			"conversation_thread", "delivery_attempt", "delivery_endpoint",
			"delivery_receipt", "message_intent", "recipient_message",
			"thread_participant",
		}
		if !slices.Equal(messagingmeta.MessagingTables, want) {
			t.Fatalf("MessagingTables=%v, want %v", messagingmeta.MessagingTables, want)
		}
		for _, table := range want {
			var found string
			if err := db.Conn.QueryRow(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema=current_schema() AND table_name=$1`, table).Scan(&found); err != nil {
				t.Errorf("table %s missing from the live schema: %v", table, err)
			}
		}
	})

	t.Run("an endpoint stores a digest, never an address", func(t *testing.T) {
		for _, raw := range []string{"jane@example.com", "+15550100", "https://hooks.example/x", "", "not-a-digest"} {
			e := newEndpoint(tenant)
			e.AddressDigest = raw
			if err := e.Validate(); !errors.Is(err, messagingmeta.ErrRawAddress) {
				t.Errorf("address %q validated as %v, want ErrRawAddress", raw, err)
			}
		}
		long := newEndpoint(tenant)
		long.AddressHint = "this-hint-is-far-too-long-to-be-a-redacted-fragment@example.com"
		if err := long.Validate(); !errors.Is(err, messagingmeta.ErrRawAddress) {
			t.Fatalf("an over-long hint validated as %v, want ErrRawAddress", err)
		}
		if err := db.ExecErr(`INSERT INTO delivery_endpoint (tenant_id, endpoint_id, principal_ref, channel, address_digest, address_hint, ownership, verification_state, effective_from) VALUES ($1,$2,'p','EMAIL',$3,'this-hint-is-far-too-long-to-be-a-redacted-fragment','BUSINESS','VERIFIED',now())`,
			tenant, uuid.New(), digestOf("x")); err == nil {
			t.Fatal("the schema accepted an over-long address hint")
		}
	})

	t.Run("provider acceptance alone never satisfies a delivery requirement", func(t *testing.T) {
		th := writeThread(t, conn, tenant, "ACKNOWLEDGED", "PROVIDER_ACCEPTED")

		// No receipt at all.
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return messagingmeta.SatisfyRecipientMessage(ctx, tx, tenant, th.messageID, uuid.Nil, fixedInstant)
		})
		if !errors.Is(err, messagingmeta.ErrProviderAcceptanceNotCompletion) {
			t.Fatalf("satisfying with no receipt returned %v", err)
		}

		// A DELIVERED receipt does not satisfy an ACKNOWLEDGED requirement.
		delivered := newReceipt(tenant, th.attemptID, "DELIVERED", "VALID")
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return messagingmeta.InsertDeliveryReceipt(ctx, tx, delivered)
		})
		err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return messagingmeta.SatisfyRecipientMessage(ctx, tx, tenant, th.messageID, delivered.ReceiptID, fixedInstant)
		})
		if !errors.Is(err, messagingmeta.ErrProviderAcceptanceNotCompletion) {
			t.Fatalf("a DELIVERED receipt satisfied an ACKNOWLEDGED requirement: %v", err)
		}

		// Nor does an acknowledgement whose signature did not verify.
		unsigned := newReceipt(tenant, th.attemptID, "ACKNOWLEDGED", "INVALID")
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return messagingmeta.InsertDeliveryReceipt(ctx, tx, unsigned)
		})
		err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return messagingmeta.SatisfyRecipientMessage(ctx, tx, tenant, th.messageID, unsigned.ReceiptID, fixedInstant)
		})
		if !errors.Is(err, messagingmeta.ErrProviderAcceptanceNotCompletion) {
			t.Fatalf("an unverified receipt satisfied the requirement: %v", err)
		}

		// A verified acknowledgement does.
		acked := newReceipt(tenant, th.attemptID, "ACKNOWLEDGED", "VALID")
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return messagingmeta.InsertDeliveryReceipt(ctx, tx, acked)
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return messagingmeta.SatisfyRecipientMessage(ctx, tx, tenant, th.messageID, acked.ReceiptID, fixedInstant.Add(time.Hour))
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			m, err := messagingmeta.LoadRecipientMessage(ctx, tx, tenant, th.messageID)
			if err != nil {
				return err
			}
			if m.SatisfactionState != "SATISFIED" || m.SatisfyingReceiptID == nil || *m.SatisfyingReceiptID != acked.ReceiptID {
				return fmt.Errorf("message %+v did not record its satisfying receipt", m)
			}
			return nil
		})

		// A raw UPDATE that skips the Go layer is refused by the schema.
		second := writeThread(t, conn, tenant, "ACKNOWLEDGED", "PROVIDER_ACCEPTED")
		if err := db.ExecErr(`UPDATE recipient_message SET satisfaction_state='SATISFIED' WHERE tenant_id=$1 AND recipient_message_id=$2`, tenant, second.messageID); err == nil {
			t.Fatal("a raw UPDATE satisfied a message with no receipt")
		}
	})

	t.Run("a receipt for another message never satisfies this one", func(t *testing.T) {
		mine := writeThread(t, conn, tenant, "DELIVERED", "SUBMITTED")
		theirs := writeThread(t, conn, tenant, "DELIVERED", "SUBMITTED")
		foreign := newReceipt(tenant, theirs.attemptID, "DELIVERED", "VALID")
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return messagingmeta.InsertDeliveryReceipt(ctx, tx, foreign)
		})
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return messagingmeta.SatisfyRecipientMessage(ctx, tx, tenant, mine.messageID, foreign.ReceiptID, fixedInstant)
		})
		if !errors.Is(err, messagingmeta.ErrProviderAcceptanceNotCompletion) {
			t.Fatalf("a foreign receipt satisfied this message: %v", err)
		}
	})

	t.Run("nothing loses its correlation, idempotency key or deadline", func(t *testing.T) {
		noCorrelation := newIntent(tenant, "DELIVERED")
		noCorrelation.CorrelationKey = ""
		if err := noCorrelation.Validate(); !errors.Is(err, messagingmeta.ErrMissingCorrelation) {
			t.Errorf("an intent without a correlation key validated as %v", err)
		}
		th := writeThread(t, conn, tenant, "DELIVERED", "SUBMITTED")
		noDeadline := newAttempt(tenant, th.messageID, th.endpointID, "SUBMITTED")
		noDeadline.DeadlineAt = time.Time{}
		if err := noDeadline.Validate(); !errors.Is(err, messagingmeta.ErrMissingDeadline) {
			t.Errorf("an attempt without a deadline validated as %v", err)
		}
		noKey := newAttempt(tenant, th.messageID, th.endpointID, "SUBMITTED")
		noKey.IdempotencyKey = ""
		if err := noKey.Validate(); !errors.Is(err, messagingmeta.ErrMissingCorrelation) {
			t.Errorf("an attempt without an idempotency key validated as %v", err)
		}
	})

	t.Run("a resubmission under the same idempotency key is refused", func(t *testing.T) {
		th := writeThread(t, conn, tenant, "DELIVERED", "SUBMITTED")
		var key string
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			a, err := messagingmeta.LoadDeliveryAttempt(ctx, tx, tenant, th.attemptID)
			key = a.IdempotencyKey
			return err
		})
		replay := newAttempt(tenant, th.messageID, th.endpointID, "SUBMITTED")
		replay.IdempotencyKey = key
		if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return messagingmeta.InsertDeliveryAttempt(ctx, tx, replay)
		}); err == nil {
			t.Fatal("a resubmission under the same idempotency key was accepted")
		}
	})

	t.Run("a duplicate provider event is refused", func(t *testing.T) {
		th := writeThread(t, conn, tenant, "DELIVERED", "SUBMITTED")
		first := newReceipt(tenant, th.attemptID, "DELIVERED", "VALID")
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return messagingmeta.InsertDeliveryReceipt(ctx, tx, first)
		})
		duplicate := newReceipt(tenant, th.attemptID, "DELIVERED", "VALID")
		duplicate.ProviderEventID = first.ProviderEventID
		if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return messagingmeta.InsertDeliveryReceipt(ctx, tx, duplicate)
		}); err == nil {
			t.Fatal("the same provider event was recorded twice")
		}
	})

	t.Run("threads bound participation in time and in visibility", func(t *testing.T) {
		threadID := uuid.New()
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return messagingmeta.InsertConversationThread(ctx, tx, messagingmeta.ConversationThread{
				TenantID: tenant, ThreadID: threadID,
				SubjectRef: "case:" + uuid.NewString(), Purpose: "CASE",
				Classification: "RESTRICTED", OpenedAt: fixedInstant, Status: "OPEN",
			})
		})
		participant := messagingmeta.ThreadParticipant{
			TenantID: tenant, ParticipantID: uuid.New(), ThreadID: threadID,
			PrincipalRef: "principal:" + uuid.NewString(), ParticipantRole: "MEMBER",
			MembershipFrom: fixedInstant, AuthorizationDigest: digestOf("authz"),
			HistoricalVisibility: "FROM_JOIN", Status: "ACTIVE",
		}
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return messagingmeta.InsertThreadParticipant(ctx, tx, participant)
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			p, err := messagingmeta.LoadThreadParticipant(ctx, tx, tenant, participant.ParticipantID)
			if err != nil {
				return err
			}
			if p.HistoricalVisibility != "FROM_JOIN" || p.AuthorizationDigest == "" {
				return fmt.Errorf("participant %+v lost its visibility or authorization snapshot", p)
			}
			return nil
		})
		reversed := participant
		reversed.ParticipantID = uuid.New()
		to := fixedInstant.Add(-time.Hour)
		reversed.MembershipTo = &to
		if err := reversed.Validate(); !errors.Is(err, messagingmeta.ErrInvalidInterval) {
			t.Fatalf("a reversed membership interval validated as %v", err)
		}
		if err := db.ExecErr(`UPDATE conversation_thread SET status='CLOSED' WHERE tenant_id=$1 AND thread_id=$2`, tenant, threadID); err == nil {
			t.Fatal("a thread closed with no closed_at")
		}
	})

	t.Run("append-only messaging evidence rejects rewrites", func(t *testing.T) {
		th := writeThread(t, conn, tenant, "DELIVERED", "SUBMITTED")
		receipt := newReceipt(tenant, th.attemptID, "DELIVERED", "VALID")
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return messagingmeta.InsertDeliveryReceipt(ctx, tx, receipt)
		})
		if err := db.ExecErr(`UPDATE delivery_attempt SET state='DELIVERED' WHERE tenant_id=$1 AND attempt_id=$2`, tenant, th.attemptID); err == nil {
			t.Fatal("an append-only delivery_attempt row was rewritten")
		}
		if err := db.ExecErr(`DELETE FROM delivery_receipt WHERE tenant_id=$1 AND receipt_id=$2`, tenant, receipt.ReceiptID); err == nil {
			t.Fatal("an append-only delivery_receipt row was deleted")
		}
	})

	t.Run("one tenant cannot reach another tenant's messages", func(t *testing.T) {
		mine := writeThread(t, conn, tenant, "DELIVERED", "SUBMITTED")
		err := inTenantTxErr(conn, other, func(tx dbport.Tx) error {
			_, err := messagingmeta.LoadRecipientMessage(ctx, tx, tenant, mine.messageID)
			return err
		})
		if !errors.Is(err, dbport.ErrNoRows) {
			t.Fatalf("another tenant read this tenant's message: %v", err)
		}
		crossTenant := newRecipientMessage(other, mine.intentID, mine.endpointID)
		if err := inTenantTxErr(conn, other, func(tx dbport.Tx) error {
			return messagingmeta.InsertRecipientMessage(ctx, tx, crossTenant)
		}); err == nil {
			t.Fatal("another tenant attached a message to this tenant's intent")
		}
	})

	t.Run("the purpose taxonomy is exactly the one the model declares", func(t *testing.T) {
		if len(messagingmeta.MessagePurposes) != 15 {
			t.Fatalf("the model declares 15 message purposes, this package holds %d", len(messagingmeta.MessagePurposes))
		}
		for _, purpose := range messagingmeta.MessagePurposes {
			m := newIntent(tenant, "DELIVERED")
			m.Purpose = purpose
			if err := m.Validate(); err != nil {
				t.Errorf("purpose %s was refused: %v", purpose, err)
			}
		}
		invalid := newIntent(tenant, "DELIVERED")
		invalid.Purpose = "MARKETING"
		if err := invalid.Validate(); !errors.Is(err, messagingmeta.ErrInvalidEnum) {
			t.Fatalf("an undeclared purpose validated as %v", err)
		}
	})
}
