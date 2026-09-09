package schema_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

// insertTenant registers one active tenant and returns its identifier.
func insertTenant(t *testing.T, db *pgtest.DB) uuid.UUID {
	t.Helper()
	return insertNamedTenant(t, db, "acme")
}

func insertNamedTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		id, key, "tenant "+key)
	return id
}

// ledgerFixture is a tenant with one stream, one payload schema and one
// authority assignment: the minimum needed to append a ledger event by hand.
type ledgerFixture struct {
	db        *pgtest.DB
	tenant    uuid.UUID
	streamKey string
	schemaRef string
	authority string
}

const (
	fixtureDigestA = "1111111111111111111111111111111111111111111111111111111111111111"
	fixtureDigestB = "2222222222222222222222222222222222222222222222222222222222222222"
)

func newLedgerFixture(t *testing.T, db *pgtest.DB) ledgerFixture {
	t.Helper()
	f := ledgerFixture{
		db:        db,
		tenant:    insertTenant(t, db),
		streamKey: "worker:1",
		schemaRef: "hcmnext.intents.v1.BusinessIntent@1",
		authority: "authority:internal",
	}

	db.Exec(t, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.intents.v1.BusinessIntent', 1,
			'hcmnext.intents.v1.BusinessIntent', 'PROTOBUF', 'LEDGER_EVENT')`,
		f.tenant, f.schemaRef)

	db.Exec(t, `
		INSERT INTO authority_assignment (
			tenant_id, authority_ref, authority_kind, domain_scope, effective_from)
		VALUES ($1, $2, 'INTERNAL', 'workforce.compensation', timestamptz '2026-01-01T00:00:00Z')`,
		f.tenant, f.authority)

	db.Exec(t, `
		INSERT INTO ledger_stream (tenant_id, stream_key, stream_kind, subject_ref)
		VALUES ($1, $2, 'WORKER', 'worker:1')`, f.tenant, f.streamKey)

	db.Exec(t, `
		INSERT INTO stream_head (tenant_id, stream_key, head_sequence)
		VALUES ($1, $2, 0)`, f.tenant, f.streamKey)

	return f
}

// eventSpec describes one hand-written ledger_event row. The Go append API is
// exercised in internal/data/ledger; here the point is the schema's own rules.
type eventSpec struct {
	sequence         int64
	assertionClass   string
	authorityRef     any
	payload          any
	artifactRef      any
	correctsStream   any
	correctsSequence any
	digest           string
	idempotencyKey   string
}

func (f ledgerFixture) event(sequence int64, assertionClass string) eventSpec {
	spec := eventSpec{
		sequence:       sequence,
		assertionClass: assertionClass,
		payload:        []byte("body"),
		digest:         fixtureDigestA,
		idempotencyKey: uuid.NewString(),
	}
	switch assertionClass {
	case "DOMAIN_FACT", "EXTERNAL_OBSERVATION":
		spec.authorityRef = f.authority
	case "CORRECTION":
		spec.correctsStream = f.streamKey
		spec.correctsSequence = int64(1)
	}
	return spec
}

// append writes the event and fails the test when the write is rejected.
func (f ledgerFixture) append(t *testing.T, spec eventSpec) {
	t.Helper()
	if err := f.appendErr(spec); err != nil {
		t.Fatalf("append event %d (%s): %v", spec.sequence, spec.assertionClass, err)
	}
}

// appendErr writes the event and returns the database's verdict.
func (f ledgerFixture) appendErr(spec eventSpec) error {
	length := 0
	if body, ok := spec.payload.([]byte); ok {
		length = len(body)
	}
	key := spec.idempotencyKey
	if key == "" {
		key = uuid.NewString()
	}
	return f.db.ExecErr(`
		INSERT INTO ledger_event (
			tenant_id, stream_key, sequence, event_id, assertion_class, authority_ref,
			source_ref, schema_ref, payload, artifact_ref, canonical_length, digest,
			digest_algorithm, occurred_at, effective_at, correlation_id, idempotency_key,
			corrects_stream_key, corrects_sequence)
		VALUES ($1, $2, $3, $4, $5, $6, 'test', $7, $8, $9, $10, $11,
			'sha256', timestamptz '2026-02-01T00:00:00Z', timestamptz '2026-02-01T00:00:00Z',
			$12, $13, $14, $15)`,
		f.tenant, f.streamKey, spec.sequence, uuid.New(), spec.assertionClass, spec.authorityRef,
		f.schemaRef, spec.payload, spec.artifactRef, length, spec.digest,
		uuid.New(), key, spec.correctsStream, spec.correctsSequence)
}

// seedAppendOnly puts one row in every append-only table other than
// ledger_event, so an immutability check has something to refuse.
func seedAppendOnly(t *testing.T, db *pgtest.DB, tenant uuid.UUID) {
	t.Helper()

	db.Exec(t, `
		INSERT INTO definition_version (
			tenant_id, definition_kind, definition_key, version, definition_digest,
			source_ref, body, published_by, published_at)
		VALUES ($1, 'INTENT', 'promotion', 1, $2, 'git://specs/promotion.yaml',
			'\x0102', 'publisher', timestamptz '2026-03-01T00:00:00Z')`,
		tenant, fixtureDigestA)

	intentID := uuid.New()
	db.Exec(t, `
		INSERT INTO intent_instance (
			tenant_id, intent_id, definition_ref, definition_version, request_digest,
			idempotency_key, request_state, execution_state, business_state,
			consistency_state, obligation_state, created_at, last_transition_at)
		VALUES ($1, $2, 'promotion', 1, $3, $4, 'SUBMITTED', 'NOT_PLANNED', 'NOT_STARTED',
			'NOT_APPLICABLE', 'NOT_APPLICABLE',
			timestamptz '2026-03-01T00:00:00Z', timestamptz '2026-03-01T00:00:00Z')`,
		tenant, intentID, fixtureDigestA, uuid.NewString())

	db.Exec(t, `
		INSERT INTO proposal_revision (
			tenant_id, intent_id, revision, proposal_digest, material_digest,
			schema_ref, payload, produced_by, produced_at)
		VALUES ($1, $2, 1, $3, $3, $4, '\x0102', 'planner', timestamptz '2026-03-01T00:00:00Z')`,
		tenant, intentID, fixtureDigestB, "hcmnext.intents.v1.BusinessIntent@1")
}

// appendEvent is the common case: a well-formed event at the given sequence.
func (f ledgerFixture) appendEvent(t *testing.T, sequence int64, assertionClass string) {
	t.Helper()
	f.append(t, f.event(sequence, assertionClass))
}
