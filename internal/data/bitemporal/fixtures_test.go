package bitemporal_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/monstercameron/hcm-next/internal/data/bitemporal"
	"github.com/monstercameron/hcm-next/internal/data/ledger"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
)

func TestMain(m *testing.M) {
	pgtest.RunMain(m)
}

// Fixed schema references used across the test timeline. Each names a
// distinct fact type, which is what "field" means to this package's
// authorization scope (see decision.go).
const (
	schemaComp = "hcmnext.people.v1.CompensationBase@1"
	schemaJob  = "hcmnext.people.v1.JobTitle@1"
	authority  = "authority:hr"
)

// fixture is a migrated schema with one tenant, both payload schemas
// registered, one authority assignment covering all business time used by
// the tests, and three empty worker streams.
type fixture struct {
	db      *pgtest.DB
	tenant  uuid.UUID
	streams []string
	heads   map[string]int64
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := pgtest.New(t)
	f := &fixture{db: db, tenant: uuid.New(), streams: []string{"worker:1", "worker:2", "worker:3"}, heads: map[string]int64{}}

	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, 'acme', 'cell-local', 'Acme', 'ACTIVE', timestamptz '2020-01-01T00:00:00Z')`,
		f.tenant)

	for _, ref := range []string{schemaComp, schemaJob} {
		db.Exec(t, `
			INSERT INTO payload_schema (
				tenant_id, schema_ref, schema_id, schema_version,
				message_full_name, wire_format, canonicalization_profile)
			VALUES ($1, $2, $2, 1, $2, 'PROTOBUF', 'LEDGER_EVENT')`,
			f.tenant, ref)
	}

	db.Exec(t, `
		INSERT INTO authority_assignment (
			tenant_id, authority_ref, authority_kind, domain_scope, effective_from)
		VALUES ($1, $2, 'INTERNAL', 'workforce', timestamptz '2020-01-01T00:00:00Z')`,
		f.tenant, authority)

	f.inTx(t, func(tx pgx.Tx) error {
		for _, stream := range f.streams {
			if err := ledger.EnsureStream(context.Background(), tx, f.tenant, stream, "WORKER", stream); err != nil {
				return err
			}
		}
		return nil
	})

	return f
}

func (f *fixture) inTx(t *testing.T, fn func(pgx.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := f.db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			t.Fatalf("rollback after %v: %v", err, rbErr)
		}
		t.Fatalf("transaction: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// factSpec is everything a test supplies to append one ledger event; the
// fixture fills in tenant, expected head, source, occurred/correlation and
// idempotency identity so tests read as a timeline, not as ledger plumbing.
type factSpec struct {
	Stream      string
	Schema      string
	Class       ledger.AssertionClass
	EffectiveAt time.Time
	RecordedAt  time.Time
	Payload     string
	Corrects    *ledger.EventRef
}

// append writes one event at the fixture's tracked head for its stream, under
// an appender whose clock is pinned to spec.RecordedAt, and returns the
// receipt. It fails the test on any append error.
func (f *fixture) append(t *testing.T, spec factSpec) ledger.AppendReceipt {
	t.Helper()
	if spec.Class == "" {
		spec.Class = ledger.DomainFact
	}
	appender := ledger.New(ledger.WithClock(func() time.Time { return spec.RecordedAt }))
	req := ledger.AppendRequest{
		Tenant:         f.tenant,
		StreamKey:      spec.Stream,
		ExpectedHead:   f.heads[spec.Stream],
		AssertionClass: spec.Class,
		Authority:      authority,
		SourceRef:      "test:fixture",
		SchemaRef:      spec.Schema,
		Payload:        []byte(spec.Payload),
		OccurredAt:     spec.EffectiveAt,
		EffectiveAt:    spec.EffectiveAt,
		CorrelationID:  uuid.New(),
		IdempotencyKey: uuid.NewString(),
		Corrects:       spec.Corrects,
	}
	var receipt ledger.AppendReceipt
	f.inTx(t, func(tx pgx.Tx) error {
		var err error
		receipt, err = appender.Append(context.Background(), tx, req)
		return err
	})
	f.heads[spec.Stream] = receipt.Sequence
	return receipt
}

// ref returns the EventRef for a receipt, for use as a later fact's Corrects.
func ref(r ledger.AppendReceipt) *ledger.EventRef {
	return &ledger.EventRef{StreamKey: r.StreamKey, Sequence: r.Sequence}
}

// decision returns an unrestricted authorization for the fixture's tenant.
func (f *fixture) decision() bitemporal.Decision {
	return bitemporal.Decision{Tenant: f.tenant}
}

// query runs q and fails the test on error.
func (f *fixture) query(t *testing.T, req bitemporal.Request, dec bitemporal.Decision, opts ...bitemporal.Option) bitemporal.Result {
	t.Helper()
	result, err := bitemporal.Query(context.Background(), f.db.Conn, req, dec, opts...)
	if err != nil {
		t.Fatalf("query %s: %v", req.Mode, err)
	}
	return result
}

// countSQL runs BuildSQL's own statement directly against PostgreSQL and
// returns how many rows it returned. Tests use it to prove authorization
// filtering happens inside PostgreSQL: the same statement Query would run,
// executed and counted independently of any Go-side post-processing.
func (f *fixture) countSQL(t *testing.T, req bitemporal.Request, dec bitemporal.Decision, now time.Time) int {
	t.Helper()
	sqlText, args, err := bitemporal.BuildSQL(req, dec, now)
	if err != nil {
		t.Fatalf("build SQL: %v", err)
	}
	rows, err := f.db.Conn.Query(context.Background(), sqlText, args...)
	if err != nil {
		t.Fatalf("run built SQL: %v", err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("run built SQL: %v", err)
	}
	return n
}

// rawStreamsAndSchemas runs the built SQL directly and returns the distinct
// (stream_key, schema_ref) pairs PostgreSQL itself returned - used to prove a
// denied subject or field never reaches the driver's result set.
func (f *fixture) rawSlots(t *testing.T, req bitemporal.Request, dec bitemporal.Decision, now time.Time) map[[2]string]bool {
	t.Helper()
	sqlText, args, err := bitemporal.BuildSQL(req, dec, now)
	if err != nil {
		t.Fatalf("build SQL: %v", err)
	}
	rows, err := f.db.Conn.Query(context.Background(), sqlText, args...)
	if err != nil {
		t.Fatalf("run built SQL: %v", err)
	}
	defer rows.Close()

	// The select list is fixed (sql.go selectColumns): stream_key is column 2,
	// schema_ref is column 8 (1-indexed).
	out := map[[2]string]bool{}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			t.Fatalf("read row values: %v", err)
		}
		stream, ok1 := vals[1].(string)
		schema, ok2 := vals[7].(string)
		if !ok1 || !ok2 {
			t.Fatalf("unexpected column types: %T %T", vals[1], vals[7])
		}
		out[[2]string{stream, schema}] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("run built SQL: %v", err)
	}
	return out
}

// uuidOtherThan returns a fresh UUID guaranteed not to equal avoid.
func uuidOtherThan(avoid uuid.UUID) uuid.UUID {
	for {
		if u := uuid.New(); u != avoid {
			return u
		}
	}
}

// fact finds the single fact for (stream, schema) in a result, failing the
// test if it is absent or duplicated.
func fact(t *testing.T, result bitemporal.Result, stream, schema string) bitemporal.Fact {
	t.Helper()
	var found *bitemporal.Fact
	for i := range result.Facts {
		f := result.Facts[i]
		if f.StreamKey == stream && f.SchemaRef == schema {
			if found != nil {
				t.Fatalf("more than one fact for %s/%s in result", stream, schema)
			}
			found = &f
		}
	}
	if found == nil {
		t.Fatalf("no fact for %s/%s in result of %d facts", stream, schema, len(result.Facts))
	}
	return *found
}

func mustNotFind(t *testing.T, result bitemporal.Result, stream, schema string) {
	t.Helper()
	for _, f := range result.Facts {
		if f.StreamKey == stream && f.SchemaRef == schema {
			t.Fatalf("fact for %s/%s present, want absent", stream, schema)
		}
	}
}

func date(s string) time.Time {
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(fmt.Sprintf("bad fixture date %q: %v", s, err))
	}
	return tm
}
