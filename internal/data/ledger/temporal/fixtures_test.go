package temporal_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	datalogger "github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/temporal"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestMain(m *testing.M) {
	pgtest.RunMain(m)
}

// The fixture's schema references. "Field" at this layer means schema_ref
// (internal/data/bitemporal.Decision documents why), so each of these is one
// field a decision can allow or deny.
const (
	fieldTitle = "hcmnext.people.v1.JobTitle@1"
	fieldBase  = "hcmnext.people.v1.CompensationBase@1"
	fieldBadge = "hcmnext.people.v1.BadgeSwipe@1"

	authorityInternal = "hcmnext:people"
	authorityPayroll  = "adp:payroll"

	subject      = "worker:1"
	otherSubject = "worker:2"
)

// The fixture's instants. Business time and recording time are deliberately
// unrelated: the correction below is effective in March and recorded in May,
// which is the only way a known-as-of query can be proven to withhold it.
var (
	march     = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	midMarch  = time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	april     = time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	may       = time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	june      = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	occurred  = time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC)
	recordFeb = time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
)

// fixture is a migrated schema holding one tenant, three registered payload
// schemas, two authority assignments and a seeded ledger.
type fixture struct {
	db     *pgtest.DB
	tenant uuid.UUID
	// seq maps a fixture label to the sequence the event was appended at, so
	// a test asserts against a name rather than a magic number.
	seq map[string]int64
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	db := pgtest.New(t)
	f := fixture{db: db, tenant: uuid.New(), seq: map[string]int64{}}

	f.seedTenant(t, f.tenant)
	f.seedEvents(t)
	return f
}

func (f fixture) seedTenant(t *testing.T, tenant uuid.UUID) {
	t.Helper()
	f.db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'Acme', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenant, "tenant-"+tenant.String()[:8])

	for _, ref := range []string{fieldTitle, fieldBase, fieldBadge} {
		f.db.Exec(t, `
			INSERT INTO payload_schema (
				tenant_id, schema_ref, schema_id, schema_version,
				message_full_name, wire_format, canonicalization_profile)
			VALUES ($1, $2, $3, 1, $3, 'PROTOBUF', 'LEDGER_EVENT')`,
			tenant, ref, ref[:len(ref)-2])
	}

	// An open-ended internal authority, and an external one whose half-open
	// interval closes on 2026-06-01 so a later effective instant is provably
	// outside it.
	f.db.Exec(t, `
		INSERT INTO authority_assignment (
			tenant_id, authority_ref, authority_kind, domain_scope, effective_from, effective_to)
		VALUES ($1, $2, 'INTERNAL', 'people', timestamptz '2026-01-01T00:00:00Z', NULL)`,
		tenant, authorityInternal)
	f.db.Exec(t, `
		INSERT INTO authority_assignment (
			tenant_id, authority_ref, authority_kind, domain_scope, effective_from, effective_to)
		VALUES ($1, $2, 'EXTERNAL_SYSTEM', 'payroll', timestamptz '2026-01-01T00:00:00Z',
			timestamptz '2026-06-01T00:00:00Z')`,
		tenant, authorityPayroll)

	for _, stream := range []string{subject, otherSubject} {
		f.inTx(t, func(tx dbport.Tx) error {
			return datalogger.EnsureStream(context.Background(), tx, tenant, stream, "WORKER", stream)
		})
	}
}

// seedEvents writes the fixture ledger. Every recorded time is pinned
// through the appender's own clock, because a bitemporal answer that depends
// on time.Now() cannot be asserted against.
func (f fixture) seedEvents(t *testing.T) {
	t.Helper()

	// A domain fact this platform is the authority for.
	f.seq["title-domain"] = f.append(t, recordFeb, datalogger.AppendRequest{
		StreamKey: subject, ExpectedHead: 0,
		AssertionClass: datalogger.DomainFact, Authority: authorityInternal,
		SchemaRef: fieldTitle, Payload: []byte("title:analyst"), EffectiveAt: march,
	})

	// An external observation about the same field, effective later and
	// recorded later. A naive latest-wins resolution returns this as the
	// current job title; it must never be presented as domain truth.
	f.seq["title-observed"] = f.append(t, recordFeb.AddDate(0, 0, 1), datalogger.AppendRequest{
		StreamKey: subject, ExpectedHead: 1,
		AssertionClass: datalogger.ExternalObservation, Authority: authorityPayroll,
		SchemaRef: fieldTitle, Payload: []byte("title:from-adp"), EffectiveAt: april,
	})

	// A domain fact and, months later in recording time, its correction.
	f.seq["base-original"] = f.append(t, recordFeb.AddDate(0, 0, 2), datalogger.AppendRequest{
		StreamKey: subject, ExpectedHead: 2,
		AssertionClass: datalogger.DomainFact, Authority: authorityInternal,
		SchemaRef: fieldBase, Payload: []byte("base:100"), EffectiveAt: march,
	})
	f.seq["base-corrected"] = f.append(t, may, datalogger.AppendRequest{
		StreamKey: subject, ExpectedHead: 3,
		AssertionClass: datalogger.Correction, Authority: authorityInternal,
		SchemaRef: fieldBase, Payload: []byte("base:120"), EffectiveAt: march,
		Corrects: &datalogger.EventRef{StreamKey: subject, Sequence: f.seq["base-original"]},
	})

	// This platform's own transaction record.
	f.seq["badge-transaction"] = f.append(t, recordFeb.AddDate(0, 0, 3), datalogger.AppendRequest{
		StreamKey: subject, ExpectedHead: 4,
		AssertionClass: datalogger.TransactionFact,
		SchemaRef:      fieldBadge, Payload: []byte("badge:issued"), EffectiveAt: midMarch,
	})

	// An unpromoted claim about the job title.
	f.seq["title-claim"] = f.append(t, recordFeb.AddDate(0, 0, 4), datalogger.AppendRequest{
		StreamKey: subject, ExpectedHead: 5,
		AssertionClass: datalogger.Claim,
		SchemaRef:      fieldTitle, Payload: []byte("title:claimed-director"), EffectiveAt: may,
	})

	// A second subject, so every subject-scoped assertion has something to
	// fail to include.
	f.seq["other-title"] = f.append(t, recordFeb, datalogger.AppendRequest{
		StreamKey: otherSubject, ExpectedHead: 0,
		AssertionClass: datalogger.DomainFact, Authority: authorityInternal,
		SchemaRef: fieldTitle, Payload: []byte("title:other"), EffectiveAt: march,
	})
}

// append fills in the invariant parts of a request, pins the recorded time,
// and returns the allocated sequence.
func (f fixture) append(t *testing.T, recordedAt time.Time, req datalogger.AppendRequest) int64 {
	t.Helper()
	if req.Tenant == uuid.Nil {
		req.Tenant = f.tenant
	}
	req.SourceRef = "hcmnext:test"
	req.OccurredAt = occurred
	req.CorrelationID = uuid.New()
	req.IdempotencyKey = uuid.NewString()

	appender := datalogger.New(datalogger.WithClock(func() time.Time { return recordedAt.UTC() }))
	var receipt datalogger.AppendReceipt
	err := f.inTxErr(func(tx dbport.Tx) error {
		var appendErr error
		receipt, appendErr = appender.Append(context.Background(), tx, req)
		return appendErr
	})
	if err != nil {
		t.Fatalf("append %s@%d: %v", req.StreamKey, req.ExpectedHead+1, err)
	}
	return receipt.Sequence
}

func (f fixture) inTx(t *testing.T, fn func(dbport.Tx) error) {
	t.Helper()
	if err := f.inTxErr(fn); err != nil {
		t.Fatalf("transaction: %v", err)
	}
}

func (f fixture) inTxErr(fn func(dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := f.db.Conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	if err := fn(tx); err != nil {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("rollback: %w", rollbackErr))
		}
		return err
	}
	return tx.Commit(ctx)
}

// decision is an unrestricted decision for the fixture tenant.
func (f fixture) decision() temporal.Decision {
	return temporal.Decision{Tenant: f.tenant}
}

// request is a RECONSTRUCT request at the given coordinate.
func (f fixture) request(effectiveAt, knownAt time.Time) temporal.Request {
	return temporal.Request{
		Tenant:      f.tenant,
		Mode:        temporal.ModeReconstruct,
		Subject:     subject,
		EffectiveAt: effectiveAt,
		KnownAt:     knownAt,
	}
}

// payloadsOf returns each assertion's payload as a string, in order, for
// readable assertions about which assertion won.
func payloadsOf(assertions []temporal.Assertion) []string {
	out := make([]string, 0, len(assertions))
	for _, a := range assertions {
		out = append(out, string(a.Payload))
	}
	return out
}

// byField indexes assertions by schema_ref.
func byField(assertions []temporal.Assertion) map[string]temporal.Assertion {
	out := make(map[string]temporal.Assertion, len(assertions))
	for _, a := range assertions {
		out[a.SchemaRef] = a
	}
	return out
}
