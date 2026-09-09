package schema_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

// TestTodo_DB_005 proves the shared tenant, cell and temporal primitives:
// tenancy is explicit, business time is explicit and half-open, and the
// compare-and-swap and digest domains reject malformed values.
func TestTodo_DB_005(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	t.Run("business time is never inferred from created_at", func(t *testing.T) {
		// created_at and recorded_at default; effective_from does not, so a row
		// cannot silently borrow its business time from its recording time.
		if err := db.ExecErr(`
			INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status)
			VALUES ($1, 'no-business-time', 'cell-local', 'x', 'ACTIVE')`, uuid.New()); err == nil {
			t.Fatal("a tenant without an explicit effective_from was accepted")
		}
	})

	t.Run("intervals are half-open", func(t *testing.T) {
		cases := []struct {
			name       string
			from, to   string
			acceptable bool
		}{
			{"open ended", "2026-01-01T00:00:00Z", "", true},
			{"ordered", "2026-01-01T00:00:00Z", "2026-02-01T00:00:00Z", true},
			{"empty", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z", false},
			{"inverted", "2026-02-01T00:00:00Z", "2026-01-01T00:00:00Z", false},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				var to any
				if tc.to != "" {
					to = tc.to
				}
				err := db.ExecErr(`
					INSERT INTO tenant (
						tenant_id, tenant_key, cell_id, display_name, status,
						effective_from, effective_to)
					VALUES ($1, $2, 'cell-local', 'x', 'ACTIVE', $3::timestamptz, $4::timestamptz)`,
					uuid.New(), "interval-"+tc.name, tc.from, to)
				if tc.acceptable && err != nil {
					t.Fatalf("half-open interval [%s, %v) was rejected: %v", tc.from, to, err)
				}
				if !tc.acceptable && err == nil {
					t.Fatalf("interval [%s, %v) was accepted; it is not a valid half-open interval", tc.from, to)
				}
			})
		}
	})

	t.Run("tenant status and cell epoch are constrained", func(t *testing.T) {
		if err := db.ExecErr(`
			INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
			VALUES ($1, 'bad-status', 'cell-local', 'x', 'ENABLED', now())`, uuid.New()); err == nil {
			t.Fatal("an unknown tenant status was accepted")
		}
		if err := db.ExecErr(`
			INSERT INTO tenant (
				tenant_id, tenant_key, cell_id, cell_epoch, display_name, status, effective_from)
			VALUES ($1, 'bad-epoch', 'cell-local', 0, 'x', 'ACTIVE', now())`, uuid.New()); err == nil {
			t.Fatal("a zero cell epoch was accepted")
		}
	})

	t.Run("compare-and-swap versions start at one", func(t *testing.T) {
		if err := db.ExecErr(`
			INSERT INTO tenant (
				tenant_id, tenant_key, cell_id, display_name, status, effective_from, instance_version)
			VALUES ($1, 'bad-version', 'cell-local', 'x', 'ACTIVE', now(), 0)`, uuid.New()); err == nil {
			t.Fatal("instance_version 0 was accepted; CAS versions begin at 1")
		}
	})

	t.Run("digests are canonical", func(t *testing.T) {
		tenant := insertNamedTenant(t, db, "digest-domain")
		if err := db.ExecErr(`
			INSERT INTO definition_version (
				tenant_id, definition_kind, definition_key, version, definition_digest,
				source_ref, body, published_by, published_at)
			VALUES ($1, 'INTENT', 'promotion', 1, 'NOT-A-DIGEST', 'file://x', '\x00', 'test', now())`,
			tenant); err == nil {
			t.Fatal("a non-hexadecimal content digest was accepted")
		}
	})

	t.Run("canonical identifiers reject blank keys", func(t *testing.T) {
		if err := db.ExecErr(`
			INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
			VALUES ($1, '  padded  ', 'cell-local', 'x', 'ACTIVE', now())`, uuid.New()); err == nil {
			t.Fatal("an untrimmed semantic key was accepted")
		}
		if err := db.ExecErr(`
			INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
			VALUES ($1, '', 'cell-local', 'x', 'ACTIVE', now())`, uuid.New()); err == nil {
			t.Fatal("an empty semantic key was accepted")
		}
	})

	t.Run("recorded and business time are separate columns", func(t *testing.T) {
		tenant := insertNamedTenant(t, db, "temporal-columns")
		var recorded, effective, created string
		err := db.QueryRow(ctx, `
			SELECT recorded_at::text, effective_from::text, created_at::text
			FROM tenant WHERE tenant_id = $1`, tenant).Scan(&recorded, &effective, &created)
		if err != nil {
			t.Fatalf("read tenant times: %v", err)
		}
		if effective == recorded {
			t.Fatalf("effective_from %s equals recorded_at %s; the fixture set business time explicitly", effective, recorded)
		}
	})
}

// TestTodo_DB_005_Property drives a spread of interval boundaries through the
// half-open constraint on every table that carries one.
func TestTodo_DB_005_Property(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	tenant := insertTenant(t, db)

	intervals := []struct {
		from, to string
		valid    bool
	}{
		{"2026-01-01T00:00:00Z", "2026-01-01T00:00:00.000001Z", true},
		{"2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z", false},
		{"2026-01-01T00:00:00.000001Z", "2026-01-01T00:00:00Z", false},
		{"1970-01-01T00:00:00Z", "2999-12-31T00:00:00Z", true},
	}
	for i, iv := range intervals {
		err := db.ExecErr(`
			INSERT INTO authority_assignment (
				tenant_id, authority_ref, authority_kind, domain_scope, effective_from, effective_to)
			VALUES ($1, $2, 'EXTERNAL_SYSTEM', 'workforce', $3::timestamptz, $4::timestamptz)`,
			tenant, uuid.NewString(), iv.from, iv.to)
		if iv.valid && err != nil {
			t.Fatalf("interval %d [%s, %s) rejected: %v", i, iv.from, iv.to, err)
		}
		if !iv.valid && err == nil {
			t.Fatalf("interval %d [%s, %s) accepted", i, iv.from, iv.to)
		}
	}
}

// TestTodo_DB_005_Golden pins the five intent lifecycle dimensions to the
// specification. An intent carries exactly five state columns, never a status.
func TestTodo_DB_005_Golden(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	rows, err := db.Conn.Query(ctx, `
		SELECT column_name FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = 'intent_instance'
		  AND column_name LIKE '%_state'
		ORDER BY column_name`, db.Schema)
	if err != nil {
		t.Fatalf("read intent columns: %v", err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read intent columns: %v", err)
	}

	want := []string{
		"business_state", "consistency_state", "execution_state",
		"obligation_state", "request_state",
	}
	if len(got) != len(want) {
		t.Fatalf("intent_instance declares state columns %v, want exactly %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("intent_instance state column %d is %q, want %q", i, got[i], want[i])
		}
	}

	var hasStatus bool
	if err := db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = $1 AND table_name = 'intent_instance' AND column_name = 'status')`,
		db.Schema).Scan(&hasStatus); err != nil {
		t.Fatalf("check for a status column: %v", err)
	}
	if hasStatus {
		t.Fatal("intent_instance has a status column; no single status substitutes for the five dimensions")
	}
}

// TestTodo_DB_005_Security proves the five dimensions reject values outside their
// declared vocabularies and that a terminated request cannot be executing.
func TestTodo_DB_005_Security(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	tenant := insertTenant(t, db)

	insert := func(request, execution, business, consistency, obligation string) error {
		return db.ExecErr(`
			INSERT INTO intent_instance (
				tenant_id, intent_id, definition_ref, definition_version, request_digest,
				idempotency_key, request_state, execution_state, business_state,
				consistency_state, obligation_state, created_at, last_transition_at)
			VALUES ($1, $2, 'promotion', 1, $3, $4, $5, $6, $7, $8, $9,
				timestamptz '2026-03-01T00:00:00Z', timestamptz '2026-03-01T00:00:00Z')`,
			tenant, uuid.New(), fixtureDigestA, uuid.NewString(),
			request, execution, business, consistency, obligation)
	}

	if err := insert("DRAFT", "NOT_PLANNED", "NOT_STARTED", "NOT_APPLICABLE", "NOT_APPLICABLE"); err != nil {
		t.Fatalf("a well-formed intent was rejected: %v", err)
	}
	if err := insert("UNSPECIFIED", "NOT_PLANNED", "NOT_STARTED", "NOT_APPLICABLE", "NOT_APPLICABLE"); err == nil {
		t.Fatal("UNSPECIFIED was accepted as a persisted request state")
	}
	if err := insert("DRAFT", "RUNNING", "NOT_STARTED", "NOT_APPLICABLE", "NOT_APPLICABLE"); err == nil {
		t.Fatal("an execution state outside the declared vocabulary was accepted")
	}
	if err := insert("CANCELLED", "EXECUTING", "NOT_STARTED", "NOT_APPLICABLE", "NOT_APPLICABLE"); err == nil {
		t.Fatal("a cancelled request was allowed to be executing")
	}
}
