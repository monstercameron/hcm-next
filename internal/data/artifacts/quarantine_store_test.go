package artifacts_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/monstercameron/hcm-next/internal/data/artifacts"
	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/asset/quarantine"
)

// pgScanner is a [quarantine.Scanner] double configured with a fixed
// verdict or error, the same shape internal/domains/asset/quarantine's own
// unit tests use, reimplemented here because this package's tests run in a
// different test binary.
type pgScanner struct {
	verdict quarantine.Verdict
	err     error
}

func (s pgScanner) Scan(ctx context.Context, digest string, r io.Reader) (quarantine.Verdict, error) {
	return s.verdict, s.err
}

func quarantinePolicy() quarantine.Policy {
	return quarantine.Policy{
		MaxContentBytes:     1 << 20,
		AllowedContentTypes: []quarantine.ContentType{quarantine.ContentTXT, quarantine.ContentPDF},
		ScannerID:           "pg-fake-scanner",
		ScannerVersion:      "1.0.0",
	}
}

// uploadInTx runs one quarantine.Upload inside its own committed
// transaction against f's schema and tenant.
func (f fixture) uploadInTx(t *testing.T, scanner quarantine.Scanner, req quarantine.UploadRequest, at time.Time) quarantine.Result {
	t.Helper()
	req.Tenant = f.tenant.String()
	var result quarantine.Result
	err := f.inTx(func(tx dbport.Tx) error {
		store := artifacts.QuarantineStore{Tx: tx, Schema: f.schema}
		var uploadErr error
		result, uploadErr = quarantine.Upload(context.Background(), store, scanner, quarantinePolicy(), req, at)
		return uploadErr
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	return result
}

// useInTx runs quarantine.Use inside its own committed transaction.
func (f fixture) useInTx(contentID string) (quarantine.StateRecord, error) {
	var state quarantine.StateRecord
	err := f.inTx(func(tx dbport.Tx) error {
		store := artifacts.QuarantineStore{Tx: tx, Schema: f.schema}
		var useErr error
		state, useErr = quarantine.Use(context.Background(), store, f.tenant.String(), contentID)
		return useErr
	})
	return state, err
}

// TestTodo_DOC_MAL_001_Integration proves the PostgreSQL-backed
// [artifacts.QuarantineStore] round trip: an admitted upload's bytes and
// verdict log persist across the companion schema
// (migrations/00030_artifact_quarantine.sql), the Use gate reads that same
// state back through real SQL, a rejected upload is refused by the gate,
// row level security keeps one tenant's quarantine rows out of another's
// reach exactly like migrations/00010's own `artifact` table, and the
// append-only triggers refuse UPDATE and DELETE on both new tables.
func TestTodo_DOC_MAL_001_Integration(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	t.Run("an admitted upload is usable through the gate afterward", func(t *testing.T) {
		content := []byte("quarterly compensation review notes, no action required")
		req := quarantine.UploadRequest{
			Content:             content,
			DeclaredContentType: quarantine.ContentTXT,
			CreatorPrincipalRef: creatorRef,
			EvidenceID:          "ev:pg:admit",
		}
		result := f.uploadInTx(t, pgScanner{verdict: quarantine.Verdict{Safe: true}}, req, fixedNow)
		if result.State != quarantine.Admitted {
			t.Fatalf("state = %s, want %s (reason: %s)", result.State, quarantine.Admitted, result.Reason)
		}

		state, err := f.useInTx(result.ContentID)
		if err != nil {
			t.Fatalf("use: %v", err)
		}
		if state.State != quarantine.Admitted {
			t.Fatalf("use gate state = %s, want %s", state.State, quarantine.Admitted)
		}
		if state.ScannerID != "pg-fake-scanner" {
			t.Fatalf("scanner id = %s, want the declared policy scanner id", state.ScannerID)
		}

		if got := quarantineByteCount(t, f, result.ContentID); got != 1 {
			t.Fatalf("artifact_quarantine has %d rows for this content id, want 1", got)
		}
		if got := quarantineStateCount(t, f, result.ContentID); got != 2 {
			t.Fatalf("artifact_quarantine_state has %d rows, want 2 (QUARANTINED then ADMITTED)", got)
		}
	})

	t.Run("a rejected upload is refused by the gate and stays evidenced", func(t *testing.T) {
		content := []byte("another quarantine candidate")
		req := quarantine.UploadRequest{
			Content:             content,
			DeclaredContentType: quarantine.ContentTXT,
			CreatorPrincipalRef: creatorRef,
			EvidenceID:          "ev:pg:reject",
		}
		result := f.uploadInTx(t, pgScanner{verdict: quarantine.Verdict{Safe: false, Reason: "signature match"}}, req, fixedNow)
		if result.State != quarantine.Rejected {
			t.Fatalf("state = %s, want %s", result.State, quarantine.Rejected)
		}

		_, err := f.useInTx(result.ContentID)
		var refused quarantine.ErrUseRefused
		if !errors.As(err, &refused) {
			t.Fatalf("error = %v, want ErrUseRefused", err)
		}
		if refused.State != quarantine.Rejected {
			t.Fatalf("refused.State = %s, want %s", refused.State, quarantine.Rejected)
		}

		// The original bytes remain recorded as restricted evidence even
		// though they are refused for use.
		if got := quarantineByteCount(t, f, result.ContentID); got != 1 {
			t.Fatalf("artifact_quarantine has %d rows for a rejected upload, want 1 (bytes stay evidenced)", got)
		}
	})

	t.Run("the gate refuses a still-quarantined artifact with no verdict yet", func(t *testing.T) {
		digest := "1111111111111111111111111111111111111111111111111111111111111111"[:64]
		err := f.inTx(func(tx dbport.Tx) error {
			return artifacts.RecordQuarantined(context.Background(), tx, f.schema, f.tenant, quarantine.QuarantinedRecord{
				ContentID:           digest,
				DigestAlgorithm:     quarantine.Algorithm,
				ByteSize:            3,
				DeclaredContentType: quarantine.ContentTXT,
				SniffedCategory:     quarantine.CategoryText,
				CreatorPrincipalRef: creatorRef,
				EvidenceID:          "ev:pg:pending",
				Content:             []byte("abc"),
			})
		})
		if err != nil {
			t.Fatalf("record quarantined: %v", err)
		}

		_, err = f.useInTx(digest)
		var refused quarantine.ErrUseRefused
		if !errors.As(err, &refused) {
			t.Fatalf("error = %v, want ErrUseRefused", err)
		}
		if refused.State != quarantine.Quarantined {
			t.Fatalf("refused.State = %s, want %s", refused.State, quarantine.Quarantined)
		}
	})

	t.Run("the gate refuses a content id nothing was ever quarantined for", func(t *testing.T) {
		_, err := f.useInTx("2222222222222222222222222222222222222222222222222222222222222222"[:64])
		if _, ok := errors.AsType[quarantine.ErrNotFound](err); !ok {
			t.Fatalf("error = %v, want ErrNotFound", err)
		}
	})

	t.Run("row level security keeps a foreign tenant's queries from seeing this tenant's quarantine rows", func(t *testing.T) {
		content := []byte("tenant isolation probe")
		req := quarantine.UploadRequest{
			Content:             content,
			DeclaredContentType: quarantine.ContentTXT,
			CreatorPrincipalRef: creatorRef,
			EvidenceID:          "ev:pg:rls",
		}
		result := f.uploadInTx(t, pgScanner{verdict: quarantine.Verdict{Safe: true}}, req, fixedNow)

		foreignTenant := insertTenant(t, f.db, "foreign-tenant-quarantine")
		if rlsQuarantineVisibleCrossTenant(t, f, foreignTenant, result.ContentID) {
			t.Fatal("a foreign tenant's scope could see this tenant's artifact_quarantine row")
		}
	})

	t.Run("append-only: UPDATE and DELETE are refused on both new tables", func(t *testing.T) {
		content := []byte("append only probe")
		req := quarantine.UploadRequest{
			Content:             content,
			DeclaredContentType: quarantine.ContentTXT,
			CreatorPrincipalRef: creatorRef,
			EvidenceID:          "ev:pg:append-only",
		}
		result := f.uploadInTx(t, pgScanner{verdict: quarantine.Verdict{Safe: true}}, req, fixedNow)

		quarantineTable := pgx.Identifier{f.schema, "artifact_quarantine"}.Sanitize()
		if err := f.db.ExecErr(fmt.Sprintf(`UPDATE %s SET byte_size = byte_size WHERE content_id = $1`, quarantineTable), result.ContentID); err == nil {
			t.Fatal("UPDATE on artifact_quarantine succeeded, want the append-only trigger to refuse it")
		}
		if err := f.db.ExecErr(fmt.Sprintf(`DELETE FROM %s WHERE content_id = $1`, quarantineTable), result.ContentID); err == nil {
			t.Fatal("DELETE on artifact_quarantine succeeded, want the append-only trigger to refuse it")
		}

		stateTable := pgx.Identifier{f.schema, "artifact_quarantine_state"}.Sanitize()
		if err := f.db.ExecErr(fmt.Sprintf(`UPDATE %s SET reason = 'edited' WHERE content_id = $1`, stateTable), result.ContentID); err == nil {
			t.Fatal("UPDATE on artifact_quarantine_state succeeded, want the append-only trigger to refuse it")
		}
		if err := f.db.ExecErr(fmt.Sprintf(`DELETE FROM %s WHERE content_id = $1`, stateTable), result.ContentID); err == nil {
			t.Fatal("DELETE on artifact_quarantine_state succeeded, want the append-only trigger to refuse it")
		}
	})

	t.Run("a verdict for a content id that was never quarantined is refused", func(t *testing.T) {
		err := f.inTx(func(tx dbport.Tx) error {
			return artifacts.RecordVerdict(context.Background(), tx, f.schema, f.tenant, quarantine.VerdictRecord{
				ContentID:      "3333333333333333333333333333333333333333333333333333333333333333"[:64],
				State:          quarantine.Admitted,
				ScannerID:      "x",
				ScannerVersion: "1",
				EvidenceID:     "ev:orphan",
			})
		})
		if _, ok := errors.AsType[quarantine.ErrNotFound](err); !ok {
			t.Fatalf("error = %v, want ErrNotFound", err)
		}
	})
}

func quarantineByteCount(t *testing.T, f fixture, contentID string) int {
	t.Helper()
	table := pgx.Identifier{f.schema, "artifact_quarantine"}.Sanitize()
	var count int
	if err := f.db.QueryRow(context.Background(),
		fmt.Sprintf(`SELECT count(*) FROM %s WHERE tenant_id = $1 AND content_id = $2`, table),
		f.tenant, contentID).Scan(&count); err != nil {
		t.Fatalf("count artifact_quarantine rows for %s: %v", contentID, err)
	}
	return count
}

func quarantineStateCount(t *testing.T, f fixture, contentID string) int {
	t.Helper()
	table := pgx.Identifier{f.schema, "artifact_quarantine_state"}.Sanitize()
	var count int
	if err := f.db.QueryRow(context.Background(),
		fmt.Sprintf(`SELECT count(*) FROM %s WHERE tenant_id = $1 AND content_id = $2`, table),
		f.tenant, contentID).Scan(&count); err != nil {
		t.Fatalf("count artifact_quarantine_state rows for %s: %v", contentID, err)
	}
	return count
}

// rlsQuarantineVisibleCrossTenant mirrors rlsVisibleCrossTenant
// (fixtures_test.go) for the new artifact_quarantine table.
func rlsQuarantineVisibleCrossTenant(t *testing.T, f fixture, foreignTenant uuid.UUID, contentID string) bool {
	t.Helper()
	ctx := context.Background()
	conn := appRoleConn(t, f.db)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin as %s: %v", tenancy.AppRole, err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, foreignTenant); err != nil {
		t.Fatalf("scope to foreign tenant: %v", err)
	}

	table := pgx.Identifier{f.schema, "artifact_quarantine"}.Sanitize()
	var count int
	if err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s WHERE content_id = $1`, table), contentID).Scan(&count); err != nil {
		t.Fatalf("query %s as %s scoped to a foreign tenant: %v", table, tenancy.AppRole, err)
	}
	return count > 0
}
