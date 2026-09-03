package bitemporal

import (
	"time"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
)

// evidenceSchema names the canonical stream Evidence writes through
// internal/engines/canonicalbytes. It is versioned independently of any
// ledger payload schema: it describes the query, not a business fact.
const evidenceSchema = "hcmnext.data.bitemporal.QueryEvidence"
const evidenceSchemaVersion = 1

// Evidence is the reproducible record of one authorized query: the exact
// request, the decision it executed under, and the exact ordered result it
// produced. Two Evidence values built from the same request, decision and
// ledger state carry the same Digest - this is the replay-stability property
// TestTodo_DATA_005 exercises by appending new events after a fixed KnownAt
// and proving the digest for that KnownAt does not move.
type Evidence struct {
	// Digest is "sha256:<hex>" over the request, decision and result.
	Digest string
	// FactCount is the number of facts the digest was computed over.
	FactCount int
}

// BuildEvidence computes the evidence digest for one already-executed query.
// It hashes:
//
//   - the request's tenant, mode, subject, field and every time bound that
//     mode actually uses (never a defaulted "now", so the digest reflects
//     what was materially asked rather than the clock the caller used)
//   - the normalized decision's tenant and sorted allow/deny lists, so a
//     digest also attests to the authorization the answer was filtered
//     through
//   - each returned fact's identity and integrity fields, in the exact order
//     they were returned, plus the page's cursors
//
// The ledger's own per-event digest (Fact.Digest) is included rather than
// the payload bytes themselves, so evidence stays small while still binding
// to the exact canonical bytes the ledger hashed at append time.
func BuildEvidence(req Request, dec Decision, result Result) (Evidence, error) {
	dec = dec.normalize()
	w := canonicalbytes.New(evidenceSchema, evidenceSchemaVersion).
		String("tenant", req.Tenant.String()).
		String("mode", string(req.Mode)).
		String("subject", req.Subject).
		String("field", req.Field).
		Value("effective_at", instantOrZero(req.EffectiveAt)).
		Value("effective_from", instantOrZero(req.EffectiveFrom)).
		Value("effective_to", instantOrZero(req.EffectiveTo)).
		Value("known_at", instantOrZero(req.KnownAt)).
		String("cursor_in", req.Cursor).
		Int("limit", int64(req.pageSize())).
		String("decision_tenant", dec.Tenant.String()).
		SortedStrings("allow_subjects", dec.AllowSubjects).
		Bool("allow_subjects_set", dec.AllowSubjects != nil).
		SortedStrings("deny_subjects", dec.DenySubjects).
		SortedStrings("allow_fields", dec.AllowFields).
		Bool("allow_fields_set", dec.AllowFields != nil).
		SortedStrings("deny_fields", dec.DenyFields).
		Value("max_known_at", instantOrZero(dec.MaxKnownAt)).
		String("next_cursor", result.NextCursor).
		Count("facts", len(result.Facts))
	for _, f := range result.Facts {
		w.String("fact.stream_key", f.StreamKey).
			Int("fact.sequence", f.Sequence).
			String("fact.schema_ref", f.SchemaRef).
			String("fact.assertion_class", string(f.AssertionClass)).
			String("fact.correction_kind", string(f.CorrectionKind)).
			String("fact.digest", f.Digest).
			Value("fact.effective_at", instantOrZero(f.EffectiveAt)).
			Value("fact.recorded_at", instantOrZero(f.RecordedAt))
	}
	digest, err := w.Digest()
	if err != nil {
		return Evidence{}, err
	}
	return Evidence{Digest: digest, FactCount: len(result.Facts)}, nil
}

// nanoInstant is a minimal canonicalbytes.Canonicalizer over a time.Time,
// local to this package: evidence digests only need a stable, unambiguous
// encoding of "this instant or nothing", not the full kernel Instant type.
type nanoInstant struct {
	present bool
	unixNS  int64
}

func instantOrZero(t time.Time) nanoInstant {
	if t.IsZero() {
		return nanoInstant{}
	}
	return nanoInstant{present: true, unixNS: t.UTC().UnixNano()}
}

// Canonical implements canonicalbytes.Canonicalizer.
func (n nanoInstant) Canonical() []byte {
	w := canonicalbytes.New("hcmnext.data.bitemporal.Instant", 1).
		Bool("present", n.present).
		Int("unix_ns", n.unixNS)
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}
