package inbox_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/inbox"
	"github.com/monstercameron/hcm-next/internal/data/messagingmeta"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
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

// seedRecipientMessage writes the migration-00031 chain (message_intent,
// delivery_endpoint, recipient_message) an inbox_record's foreign key needs,
// and returns the recipient_message id.
func seedRecipientMessage(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var recipientMessageID uuid.UUID
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		intent := messagingmeta.MessageIntent{
			TenantID:            tenant,
			MessageIntentID:     uuid.New(),
			Purpose:             "TASK",
			AudienceExpression:  json.RawMessage(`{}`),
			TemplateKey:         "tpl:" + uuid.NewString(),
			TemplateVersion:     1,
			Classification:      "INTERNAL",
			Urgency:             "NORMAL",
			DeliveryRequirement: "BEST_EFFORT",
			ResponseRequirement: "NONE",
			WorkflowRef:         "workflow:inbox-seed",
			CorrelationKey:      "corr:" + uuid.NewString(),
			CreatedAt:           fixedInstant,
		}
		if err := messagingmeta.InsertMessageIntent(ctx, tx, intent); err != nil {
			return err
		}
		endpoint := messagingmeta.DeliveryEndpoint{
			TenantID:          tenant,
			EndpointID:        uuid.New(),
			PrincipalRef:      "principal:" + uuid.NewString(),
			Channel:           "INBOX",
			AddressDigest:     digestOf("address-" + uuid.NewString()),
			Ownership:         "BUSINESS",
			VerificationState: "VERIFIED",
			PurposeScope:      json.RawMessage(`{}`),
			Locale:            "en-US",
			EffectiveFrom:     fixedInstant,
			Status:            "ACTIVE",
		}
		if err := messagingmeta.InsertDeliveryEndpoint(ctx, tx, endpoint); err != nil {
			return err
		}
		message := messagingmeta.RecipientMessage{
			TenantID:           tenant,
			RecipientMessageID: uuid.New(),
			MessageIntentID:    intent.MessageIntentID,
			RecipientRef:       "principal:" + uuid.NewString(),
			EndpointID:         endpoint.EndpointID,
			RenderedDigest:     digestOf("rendered-" + uuid.NewString()),
			Classification:     "INTERNAL",
			CorrelationKey:     "corr:" + uuid.NewString(),
			RecipientState:     "UNSEEN",
			SatisfactionState:  "PENDING",
			CreatedAt:          fixedInstant,
			UpdatedAt:          fixedInstant,
		}
		if err := messagingmeta.InsertRecipientMessage(ctx, tx, message); err != nil {
			return err
		}
		recipientMessageID = message.RecipientMessageID
		return nil
	})
	return recipientMessageID
}

func newRecord(tenant uuid.UUID, subjectRef string, recipientMessageID uuid.UUID) inbox.Record {
	return inbox.Record{
		TenantID:           tenant,
		InboxRecordID:      uuid.New(),
		SubjectRef:         subjectRef,
		RecipientMessageID: recipientMessageID,
		ReadState:          inbox.Unread,
		CreatedAt:          fixedInstant,
	}
}

// seedInboxRecord seeds one full chain and its inbox record, returning both
// the store handle inputs a caller needs.
func seedInboxRecord(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, subjectRef string) inbox.Record {
	t.Helper()
	ctx := context.Background()
	recipientMessageID := seedRecipientMessage(t, conn, tenant)
	rec := newRecord(tenant, subjectRef, recipientMessageID)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		return inbox.Store{}.Create(ctx, tx, rec)
	})
	return rec
}

// TestTodo_MSG_005 is MSG-005's primary acceptance test: an inbox record
// with recipient, task reference and created/seen state is created, read
// back and advanced under compare-and-swap, with no separate channel,
// thread, preference or provider machinery -- messagingmeta's tables and
// this package's own two tables are the whole of it.
func TestTodo_MSG_005(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "msg005-primary")

	t.Run("the inbox tables exist in the live schema", func(t *testing.T) {
		for _, table := range []string{"inbox_record", "inbox_state_event"} {
			var found string
			if err := db.Conn.QueryRow(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema=current_schema() AND table_name=$1`, table).Scan(&found); err != nil {
				t.Errorf("table %s missing from the live schema: %v", table, err)
			}
		}
	})

	t.Run("a created record is readable and listable for its own subject", func(t *testing.T) {
		subject := "subject:" + uuid.NewString()
		rec := seedInboxRecord(t, conn, tenant, subject)

		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			loaded, err := inbox.Store{}.Load(ctx, tx, tenant, subject, rec.InboxRecordID)
			if err != nil {
				return err
			}
			if loaded.ReadState != inbox.Unread || loaded.Archived || loaded.Pinned || loaded.Version != 1 {
				t.Fatalf("newly created record loaded as %+v, want UNREAD/unarchived/unpinned/v1", loaded)
			}
			if loaded.RecipientMessageID != rec.RecipientMessageID {
				t.Fatalf("record lost its recipient_message reference: %+v", loaded)
			}
			return nil
		})

		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			list, err := inbox.Store{}.List(ctx, tx, tenant, subject)
			if err != nil {
				return err
			}
			if len(list) != 1 || list[0].InboxRecordID != rec.InboxRecordID {
				t.Fatalf("List returned %+v, want exactly the seeded record", list)
			}
			return nil
		})
	})

	t.Run("MarkRead moves UNREAD to READ under compare-and-swap and logs the event", func(t *testing.T) {
		subject := "subject:" + uuid.NewString()
		rec := seedInboxRecord(t, conn, tenant, subject)

		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return inbox.Store{}.MarkRead(ctx, tx, tenant, subject, rec.InboxRecordID, 1, fixedInstant.Add(time.Minute))
		})

		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			loaded, err := inbox.Store{}.Load(ctx, tx, tenant, subject, rec.InboxRecordID)
			if err != nil {
				return err
			}
			if loaded.ReadState != inbox.Read || loaded.Version != 2 {
				t.Fatalf("after MarkRead, record is %+v, want READ/v2", loaded)
			}
			if !loaded.StateChangedAt.Equal(fixedInstant.Add(time.Minute)) {
				t.Fatalf("MarkRead did not stamp state_changed_at: %+v", loaded)
			}
			return nil
		})

		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var eventType string
			var previous, next int64
			err := tx.QueryRow(ctx, `SELECT event_type, previous_version, new_version FROM inbox_state_event WHERE tenant_id=$1 AND inbox_record_id=$2`, tenant, rec.InboxRecordID).
				Scan(&eventType, &previous, &next)
			if err != nil {
				t.Fatalf("read inbox_state_event: %v", err)
			}
			if eventType != inbox.EventMarkedRead || previous != 1 || next != 2 {
				t.Fatalf("event = (%s, %d, %d), want (MARKED_READ, 1, 2)", eventType, previous, next)
			}
			return nil
		})

		// A stale expected version is refused and changes nothing.
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return inbox.Store{}.MarkRead(ctx, tx, tenant, subject, rec.InboxRecordID, 1, fixedInstant.Add(2*time.Minute))
		})
		if !errors.Is(err, inbox.ErrVersionConflict) {
			t.Fatalf("MarkRead with a stale version returned %v, want ErrVersionConflict", err)
		}
	})

	t.Run("Archive and Pin toggle independently under compare-and-swap", func(t *testing.T) {
		subject := "subject:" + uuid.NewString()
		rec := seedInboxRecord(t, conn, tenant, subject)

		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return inbox.Store{}.Archive(ctx, tx, tenant, subject, rec.InboxRecordID, true, 1, fixedInstant.Add(time.Minute))
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return inbox.Store{}.Pin(ctx, tx, tenant, subject, rec.InboxRecordID, true, 2, fixedInstant.Add(2*time.Minute))
		})

		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			loaded, err := inbox.Store{}.Load(ctx, tx, tenant, subject, rec.InboxRecordID)
			if err != nil {
				return err
			}
			if !loaded.Archived || !loaded.Pinned || loaded.Version != 3 {
				t.Fatalf("after Archive+Pin, record is %+v, want archived/pinned/v3", loaded)
			}
			return nil
		})

		// Unarchiving logs UNARCHIVED, not a second ARCHIVED.
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return inbox.Store{}.Archive(ctx, tx, tenant, subject, rec.InboxRecordID, false, 3, fixedInstant.Add(3*time.Minute))
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var eventType string
			err := tx.QueryRow(ctx, `SELECT event_type FROM inbox_state_event WHERE tenant_id=$1 AND inbox_record_id=$2 AND new_version=4`, tenant, rec.InboxRecordID).Scan(&eventType)
			if err != nil {
				return err
			}
			if eventType != inbox.EventUnarchived {
				t.Fatalf("unarchiving logged %s, want UNARCHIVED", eventType)
			}
			return nil
		})
	})

	t.Run("a repeated (subject, recipient_message) create is refused", func(t *testing.T) {
		subject := "subject:" + uuid.NewString()
		recipientMessageID := seedRecipientMessage(t, conn, tenant)
		first := newRecord(tenant, subject, recipientMessageID)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return inbox.Store{}.Create(ctx, tx, first)
		})
		second := newRecord(tenant, subject, recipientMessageID)
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return inbox.Store{}.Create(ctx, tx, second)
		})
		if !errors.Is(err, inbox.ErrDuplicate) {
			t.Fatalf("a second inbox record for the same subject/message returned %v, want ErrDuplicate", err)
		}
	})

	t.Run("append-only inbox_state_event rejects rewrites", func(t *testing.T) {
		subject := "subject:" + uuid.NewString()
		rec := seedInboxRecord(t, conn, tenant, subject)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return inbox.Store{}.MarkRead(ctx, tx, tenant, subject, rec.InboxRecordID, 1, fixedInstant.Add(time.Minute))
		})
		var eventID uuid.UUID
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return tx.QueryRow(ctx, `SELECT event_id FROM inbox_state_event WHERE tenant_id=$1 AND inbox_record_id=$2`, tenant, rec.InboxRecordID).Scan(&eventID)
		})
		if err := db.ExecErr(`UPDATE inbox_state_event SET event_type='ARCHIVED' WHERE tenant_id=$1 AND event_id=$2`, tenant, eventID); err == nil {
			t.Fatal("an append-only inbox_state_event row was rewritten")
		}
		if err := db.ExecErr(`DELETE FROM inbox_state_event WHERE tenant_id=$1 AND event_id=$2`, tenant, eventID); err == nil {
			t.Fatal("an append-only inbox_state_event row was deleted")
		}
	})
}

// TestTodo_MSG_005_Security proves MSG-005's RED clause: a wrong
// principal/tenant can neither read the record nor advance its state.
func TestTodo_MSG_005_Security(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "msg005-security")
	otherTenant := insertTenant(t, db, "msg005-security-other")

	t.Run("a wrong subject cannot read another subject's record", func(t *testing.T) {
		owner := "subject:" + uuid.NewString()
		intruder := "subject:" + uuid.NewString()
		rec := seedInboxRecord(t, conn, tenant, owner)

		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := inbox.Store{}.Load(ctx, tx, tenant, intruder, rec.InboxRecordID)
			return err
		})
		if !errors.Is(err, inbox.ErrNotFound) {
			t.Fatalf("a wrong subject's Load returned %v, want ErrNotFound", err)
		}

		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			list, err := inbox.Store{}.List(ctx, tx, tenant, intruder)
			if err != nil {
				return err
			}
			if len(list) != 0 {
				t.Fatalf("a wrong subject's List returned %d rows, want 0", len(list))
			}
			return nil
		})
	})

	t.Run("a wrong subject cannot advance another subject's record", func(t *testing.T) {
		owner := "subject:" + uuid.NewString()
		intruder := "subject:" + uuid.NewString()
		rec := seedInboxRecord(t, conn, tenant, owner)

		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return inbox.Store{}.MarkRead(ctx, tx, tenant, intruder, rec.InboxRecordID, 1, fixedInstant.Add(time.Minute))
		})
		if !errors.Is(err, inbox.ErrVersionConflict) {
			t.Fatalf("a wrong subject's MarkRead returned %v, want ErrVersionConflict", err)
		}
		err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return inbox.Store{}.Archive(ctx, tx, tenant, intruder, rec.InboxRecordID, true, 1, fixedInstant.Add(time.Minute))
		})
		if !errors.Is(err, inbox.ErrVersionConflict) {
			t.Fatalf("a wrong subject's Archive returned %v, want ErrVersionConflict", err)
		}
		err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return inbox.Store{}.Pin(ctx, tx, tenant, intruder, rec.InboxRecordID, true, 1, fixedInstant.Add(time.Minute))
		})
		if !errors.Is(err, inbox.ErrVersionConflict) {
			t.Fatalf("a wrong subject's Pin returned %v, want ErrVersionConflict", err)
		}

		// Nothing about the record moved.
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			loaded, err := inbox.Store{}.Load(ctx, tx, tenant, owner, rec.InboxRecordID)
			if err != nil {
				return err
			}
			if loaded.ReadState != inbox.Unread || loaded.Archived || loaded.Pinned || loaded.Version != 1 {
				t.Fatalf("the record moved after a wrong-subject attempt: %+v", loaded)
			}
			return nil
		})
	})

	t.Run("one tenant cannot reach another tenant's inbox record", func(t *testing.T) {
		owner := "subject:" + uuid.NewString()
		rec := seedInboxRecord(t, conn, tenant, owner)

		err := inTenantTxErr(conn, otherTenant, func(tx dbport.Tx) error {
			_, err := inbox.Store{}.Load(ctx, tx, tenant, owner, rec.InboxRecordID)
			return err
		})
		if !errors.Is(err, inbox.ErrNotFound) {
			t.Fatalf("another tenant's Load (RLS-scoped to itself, but naming this tenant's id) returned %v, want ErrNotFound", err)
		}

		err = inTenantTxErr(conn, otherTenant, func(tx dbport.Tx) error {
			return inbox.Store{}.MarkRead(ctx, tx, tenant, owner, rec.InboxRecordID, 1, fixedInstant.Add(time.Minute))
		})
		if err == nil {
			t.Fatal("another tenant's MarkRead against this tenant's record id succeeded")
		}
	})
}
