package temporal

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// DigestAlgorithm names the algorithm [State.Digest] and [Result.Digest]
// use. It is recorded next to every digest this repository mints rather than
// assumed, so a later algorithm change is a visible version change.
const DigestAlgorithm = "sha256"

// assertionProjection is the canonical, order-fixed projection of one
// assertion that a digest covers. It deliberately carries the resolved
// labels - truth class, authority, supersession - and not just the raw
// envelope: two answers that disagree about whether an assertion is domain
// truth are different answers, and must digest differently.
//
// Payload bytes are covered by their recorded digest rather than inlined:
// the ledger already binds payload and schema into that digest
// (internal/data/ledger.Digester), so repeating the bytes here would
// duplicate a guarantee without adding one, and would put domain content
// into an evidence value that is meant to be safe to log.
type assertionProjection struct {
	StreamKey         string `json:"stream_key"`
	Sequence          int64  `json:"sequence"`
	SourceEventID     string `json:"source_event_id"`
	SchemaRef         string `json:"schema_ref"`
	AssertionClass    string `json:"assertion_class"`
	CorrectionKind    string `json:"correction_kind"`
	TruthClass        string `json:"truth_class"`
	AuthorityRef      string `json:"authority_ref"`
	AuthorityKind     string `json:"authority_kind"`
	AuthorityResolved bool   `json:"authority_resolved"`
	AuthorityCovers   bool   `json:"authority_covers_effective_at"`
	SourceRef         string `json:"source_ref"`
	ArtifactRef       string `json:"artifact_ref"`
	Digest            string `json:"digest"`
	DigestAlgorithm   string `json:"digest_algorithm"`
	OccurredAt        int64  `json:"occurred_at_ns"`
	EffectiveAt       int64  `json:"effective_at_ns"`
	RecordedAt        int64  `json:"recorded_at_ns"`
	CorrelationID     string `json:"correlation_id"`
	CausationID       string `json:"causation_id"`
	CorrectsStream    string `json:"corrects_stream_key"`
	CorrectsSequence  int64  `json:"corrects_sequence"`
	Superseded        bool   `json:"superseded"`
}

func (a Assertion) project() assertionProjection {
	p := assertionProjection{
		StreamKey:         a.Ref.StreamKey,
		Sequence:          a.Ref.Sequence,
		SourceEventID:     a.SourceEventID.String(),
		SchemaRef:         a.SchemaRef,
		AssertionClass:    string(a.AssertionClass),
		CorrectionKind:    string(a.CorrectionKind),
		TruthClass:        string(a.TruthClass),
		AuthorityRef:      a.Authority.Ref,
		AuthorityKind:     a.Authority.Kind,
		AuthorityResolved: a.Authority.Resolved,
		AuthorityCovers:   a.Authority.CoversEffectiveAt,
		SourceRef:         a.SourceRef,
		ArtifactRef:       a.ArtifactRef,
		Digest:            a.Digest,
		DigestAlgorithm:   a.DigestAlgorithm,
		OccurredAt:        a.OccurredAt.UTC().UnixNano(),
		EffectiveAt:       a.EffectiveAt.UTC().UnixNano(),
		RecordedAt:        a.RecordedAt.UTC().UnixNano(),
		CorrelationID:     a.CorrelationID.String(),
		CausationID:       a.CausationID.String(),
		Superseded:        a.Superseded,
	}
	if a.Corrects != nil {
		p.CorrectsStream = a.Corrects.StreamKey
		p.CorrectsSequence = a.Corrects.Sequence
	}
	return p
}

// stateProjection is the canonical projection [State.Digest] hashes.
type stateProjection struct {
	Version     int                   `json:"version"`
	Tenant      string                `json:"tenant"`
	Subject     string                `json:"subject"`
	EffectiveAt int64                 `json:"effective_at_ns"`
	KnownAt     int64                 `json:"known_at_ns"`
	Considered  int                   `json:"considered"`
	Domain      []assertionProjection `json:"domain"`
	Transaction []assertionProjection `json:"transaction"`
	Unpromoted  []assertionProjection `json:"unpromoted"`
}

// projectionVersion is bumped whenever the canonical projection changes
// shape. A digest is only comparable to another digest of the same version,
// so the version is inside the hashed bytes, not beside them.
const projectionVersion = 1

func project(assertions []Assertion) []assertionProjection {
	out := make([]assertionProjection, 0, len(assertions))
	for _, a := range assertions {
		out = append(out, a.project())
	}
	return out
}

// Digest returns the hex-encoded sha256 digest of the state's canonical
// projection. Two reconstructions of the same subject at the same
// coordinate, under the same decision and over the same ledger contents,
// always digest identically - including across query plans, which is what
// [VerifyPlanEquivalence] compares.
//
// It is deliberately a function of the answer, not of the query: a caller
// can hand the digest to an auditor together with the coordinate and the
// decision and let them recompute it from their own read.
func (s State) Digest() (string, error) {
	body, err := json.Marshal(stateProjection{
		Version:     projectionVersion,
		Tenant:      s.Tenant.String(),
		Subject:     s.Subject,
		EffectiveAt: s.Coordinate.EffectiveAt.UTC().UnixNano(),
		KnownAt:     s.Coordinate.KnownAt.UTC().UnixNano(),
		Considered:  s.Considered,
		Domain:      project(s.Domain),
		Transaction: project(s.Transaction),
		Unpromoted:  project(s.Unpromoted),
	})
	if err != nil {
		return "", fmt.Errorf("temporal: marshal canonical state: %w", err)
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

// resultProjection is the canonical projection [Result.Digest] hashes.
type resultProjection struct {
	Version     int                   `json:"version"`
	Mode        string                `json:"mode"`
	EffectiveAt int64                 `json:"effective_at_ns"`
	KnownAt     int64                 `json:"known_at_ns"`
	Assertions  []assertionProjection `json:"assertions"`
}

// Digest returns the hex-encoded sha256 digest of one page of answers. The
// cursor is not covered: a cursor is how a page was reached, not what it
// says, and two identical pages reached by different routes must digest
// alike.
func (r Result) Digest() (string, error) {
	body, err := json.Marshal(resultProjection{
		Version:     projectionVersion,
		Mode:        string(r.Mode),
		EffectiveAt: r.Coordinate.EffectiveAt.UTC().UnixNano(),
		KnownAt:     r.Coordinate.KnownAt.UTC().UnixNano(),
		Assertions:  project(r.Assertions),
	})
	if err != nil {
		return "", fmt.Errorf("temporal: marshal canonical result: %w", err)
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}
