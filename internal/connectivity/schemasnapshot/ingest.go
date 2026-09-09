package schemasnapshot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
)

// IngestRequest is everything [Ingest] needs to quarantine and evaluate one
// captured schema.
type IngestRequest struct {
	TenantID       string
	Provider       ProviderRef
	CapturedAt     time.Time
	DeclaredFormat DeclaredFormat
	// Raw is the captured schema's exact bytes, unmodified. Ingest stores
	// them as a content-addressed artifact before anything else happens.
	Raw []byte
	// Supersedes, when set, names a prior snapshot of the same provider that
	// this one supersedes.
	Supersedes *uuid.UUID

	// Classification, RetentionClass, CreatorPrincipalRef and EvidenceID are
	// passed straight through to the artifact write; see
	// internal/data/artifacts.PutRequest for why each is required.
	Classification      model.ClassificationLabel
	RetentionClass      string
	CreatorPrincipalRef string
	EvidenceID          string

	// DecidedAt is the instant the decision is recorded under, supplied
	// rather than read from the wall clock so Ingest is reproducible in
	// tests. A zero value uses time.Now().
	DecidedAt time.Time
}

func (r IngestRequest) validate() error {
	const op = "schemasnapshot.Ingest"
	if strings.TrimSpace(r.TenantID) == "" {
		return newError(op, ErrIncomplete, "ingest has no tenant")
	}
	if err := r.Provider.Validate(); err != nil {
		return err
	}
	switch {
	case r.CapturedAt.IsZero():
		return newError(op, ErrIncomplete, "ingest has no capture time")
	case !r.DeclaredFormat.Valid():
		return newError(op, ErrIncomplete, "ingest declares no recognized format")
	case len(r.Raw) == 0:
		return newError(op, ErrIncomplete, "ingest carries no bytes")
	case r.Supersedes != nil && *r.Supersedes == uuid.Nil:
		return newError(op, ErrInvalid, "ingest supersedes a nil snapshot id")
	}
	return validateArtifactPutRequest(ArtifactPutRequest{
		TenantID:            r.TenantID,
		Raw:                 r.Raw,
		MediaType:           mediaTypeFor(r.DeclaredFormat),
		Classification:      r.Classification,
		RetentionClass:      r.RetentionClass,
		CreatorPrincipalRef: r.CreatorPrincipalRef,
		EvidenceID:          r.EvidenceID,
	})
}

// IngestResult is what [Ingest] produced: the snapshot in its final state
// (StateQuarantined only if a verdict genuinely could not be reached -- see
// [Ingest]) and the evidence that justifies it, when one has been recorded.
type IngestResult struct {
	Snapshot SchemaSnapshot
	// Evidence is the zero value when Snapshot is still StateQuarantined.
	Evidence Evidence
	// ArtifactCreated reports whether this call was the one that first wrote
	// the raw bytes (false means byte-identical content was already on
	// file).
	ArtifactCreated bool
	// SnapshotCreated reports whether this call was the one that first
	// inserted the quarantine row (false means an identical snapshot -- by
	// tenant, provider and canonical digest -- already existed).
	SnapshotCreated bool
}

// Ingest performs the whole INTG-004 flow, in order, with no shorter path to
// [StateAdmitted]:
//
//  1. Store req.Raw as a content-addressed artifact via artifactStore.
//  2. Record a [StateQuarantined] [SchemaSnapshot] under the identity
//     derived from (tenant, provider, canonical digest) via store.Insert.
//  3. Run every validator against the declared format and the raw bytes
//     ([Decide]).
//  4. Transition to [StateAdmitted] or [StateRejected] and record the
//     deciding [Evidence], atomically, via store.Decide.
//
// Re-ingesting byte-identical content for the same provider is idempotent:
// step 2 resolves to the same row, and if it is already decided, Ingest
// returns that decision unchanged without re-running validators or writing
// a second [Evidence] record. If a prior call reached step 2 but never
// completed step 4 (a crash, a timeout), this call resumes from step 3
// rather than reporting success prematurely.
//
// len(validators) == 0 is refused: a snapshot admitted with zero evidence
// would satisfy [SchemaSnapshot.RequireAdmitted] on nothing.
func Ingest(ctx context.Context, artifactStore ArtifactStore, store Store, validators []Validator, req IngestRequest) (IngestResult, error) {
	const op = "schemasnapshot.Ingest"
	if err := req.validate(); err != nil {
		return IngestResult{}, err
	}
	if len(validators) == 0 {
		return IngestResult{}, newError(op, ErrIncomplete,
			"no validators configured; a snapshot cannot be admitted on zero evidence")
	}

	contentID, byteSize, artifactCreated, err := artifactStore.Put(ctx, ArtifactPutRequest{
		TenantID:            req.TenantID,
		Raw:                 req.Raw,
		MediaType:           mediaTypeFor(req.DeclaredFormat),
		Classification:      req.Classification,
		RetentionClass:      req.RetentionClass,
		CreatorPrincipalRef: req.CreatorPrincipalRef,
		EvidenceID:          req.EvidenceID,
	})
	if err != nil {
		return IngestResult{}, newError(op, ErrStore, "store raw bytes as artifact: %v", err)
	}

	identity := Identity{TenantID: req.TenantID, Provider: req.Provider, CanonicalDigest: contentID}
	snap := SchemaSnapshot{
		SnapshotID:      identity.SnapshotID(),
		TenantID:        req.TenantID,
		Provider:        req.Provider,
		CapturedAt:      req.CapturedAt.UTC(),
		DeclaredFormat:  req.DeclaredFormat,
		DigestAlgorithm: Algorithm,
		CanonicalDigest: contentID,
		ArtifactRef:     contentID,
		ByteSize:        byteSize,
		Supersedes:      req.Supersedes,
		State:           StateQuarantined,
	}

	stored, snapshotCreated, err := store.Insert(ctx, snap)
	if err != nil {
		return IngestResult{}, err
	}

	if !snapshotCreated && stored.State != StateQuarantined {
		ev, ok, err := store.Evidence(ctx, req.TenantID, stored.SnapshotID)
		if err != nil {
			return IngestResult{}, err
		}
		if !ok {
			return IngestResult{}, newError(op, ErrIncomplete,
				"snapshot %s is decided %s but carries no evidence", stored.SnapshotID, stored.State)
		}
		return IngestResult{
			Snapshot: stored, Evidence: ev,
			ArtifactCreated: artifactCreated, SnapshotCreated: snapshotCreated,
		}, nil
	}

	verdict, results := Decide(req.DeclaredFormat, req.Raw, validators)
	decidedAt := req.DecidedAt
	if decidedAt.IsZero() {
		decidedAt = time.Now().UTC()
	}
	reason := decisionReason(verdict, results)

	finalSnap, ev, err := store.Decide(ctx, req.TenantID, stored.SnapshotID, verdict, reason, decidedAt, results)
	if err != nil {
		return IngestResult{}, err
	}
	return IngestResult{
		Snapshot: finalSnap, Evidence: ev,
		ArtifactCreated: artifactCreated, SnapshotCreated: snapshotCreated,
	}, nil
}

// decisionReason renders a short, evidence-consistent human summary of a
// verdict: an admitted snapshot names how many validators passed, a rejected
// one names which ones failed.
func decisionReason(verdict State, results []ValidatorResult) string {
	if verdict == StateAdmitted {
		return fmt.Sprintf("%d/%d validators passed", len(results), len(results))
	}
	failed := make([]string, 0, len(results))
	for _, r := range results {
		if !r.Passed {
			failed = append(failed, r.Name)
		}
	}
	return fmt.Sprintf("failed validators: %s", strings.Join(failed, ", "))
}

// mediaTypeFor maps a declared format to the media type recorded against the
// artifact write.
func mediaTypeFor(f DeclaredFormat) string {
	switch f {
	case FormatJSONSchema, FormatOpenAPI:
		return "application/json"
	case FormatXSD, FormatWSDL:
		return "application/xml"
	case FormatCSVHeader:
		return "text/csv"
	case FormatGraphQLSDL:
		return "application/graphql"
	case FormatProtobuf:
		return "text/x-proto"
	default:
		return "application/octet-stream"
	}
}
