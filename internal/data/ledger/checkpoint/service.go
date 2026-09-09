package checkpoint

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// AnchorReceipt is what an external anchor returns: enough to find the
// anchored copy again, and nothing that changes what the checkpoint says.
type AnchorReceipt struct {
	Provider   string
	Reference  string
	AnchoredAt time.Time
}

// Anchor publishes a signed manifest somewhere outside this database - a
// write-once store, a transparency log, a counterparty. It is a port with a
// no-op default because anchoring must never be able to change a
// checkpoint's content or its verification: a provider that is absent,
// slow or wrong produces no anchor receipt and an unchanged, still-valid
// checkpoint.
type Anchor interface {
	Name() string
	Anchor(ctx context.Context, m Manifest) (AnchorReceipt, error)
}

// NoAnchor is the default [Anchor]: it publishes nothing and says so.
type NoAnchor struct{}

// Name implements [Anchor].
func (NoAnchor) Name() string { return "none" }

// Anchor implements [Anchor].
func (NoAnchor) Anchor(context.Context, Manifest) (AnchorReceipt, error) {
	return AnchorReceipt{Provider: "none"}, nil
}

// Service creates signed checkpoints over a tenant's ledger.
type Service struct {
	signer Signer
	dir    KeyDirectory
	store  *Store
	anchor Anchor
	now    func() time.Time
	newID  func() uuid.UUID
}

// ServiceOption configures a [Service].
type ServiceOption func(*Service)

// WithClock replaces the source of a checkpoint's creation instant. Tests
// use it to make a manifest - and therefore its digest and its signature -
// reproducible.
func WithClock(now func() time.Time) ServiceOption {
	return func(s *Service) { s.now = now }
}

// WithAnchor replaces the external anchoring provider.
func WithAnchor(a Anchor) ServiceOption {
	return func(s *Service) { s.anchor = a }
}

// WithEpochIDs replaces the epoch identifier source. It exists so a golden
// test can pin an identifier that is otherwise random; production always
// uses uuid.New.
func WithEpochIDs(newID func() uuid.UUID) ServiceOption {
	return func(s *Service) { s.newID = newID }
}

// NewService builds a Service. A signer and a key directory are both
// required: signing without a directory would mean nothing could refuse a
// revoked key, which is exactly the failure this todo exists to prevent.
func NewService(signer Signer, dir KeyDirectory, opts ...ServiceOption) (*Service, error) {
	if signer == nil {
		return nil, fmt.Errorf("checkpoint: a signer is required")
	}
	if dir == nil {
		return nil, fmt.Errorf("checkpoint: a key directory is required; without one no key can be refused")
	}
	s := &Service{
		signer: signer,
		dir:    dir,
		store:  NewStore(),
		anchor: NoAnchor{},
		now:    func() time.Time { return time.Now().UTC() },
		newID:  uuid.New,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// Store exposes the service's store so a caller can read epochs back
// without building a second one.
func (s *Service) Store() *Store { return s.store }

// CreateRequest is what a caller states about a checkpoint. Everything else
// - the epoch number, the stream heads, the root digest, the covered window,
// the signature - is derived, because a caller that could supply them could
// attest to a ledger state that never existed.
type CreateRequest struct {
	Tenant uuid.UUID
	// Schema binds the checkpoint to the physical schema it was taken under.
	Schema SchemaRelease
	// At is the checkpoint instant: the exclusive end of the covered
	// recorded-time window, and the manifest's creation time. Zero uses the
	// service clock.
	At time.Time
}

// Create takes a checkpoint over every stream in the tenant that holds an
// event recorded before the checkpoint instant, signs it, and records it as
// the next epoch, all inside the caller's transaction.
//
// It refuses rather than degrades:
//
//   - a tenant with no events yet has nothing to attest to
//     ([ErrIncompleteCoverage]);
//   - a stream whose hash chain has no link at its head cannot be covered,
//     so the whole checkpoint is refused and the stream is named;
//   - a signing key that the directory says was expired or revoked at the
//     checkpoint instant produces no signature at all ([ErrKeyNotUsable]).
//
// External anchoring runs after the manifest is signed and recorded and its
// failure is returned to the caller without invalidating the checkpoint;
// what is stored is already complete evidence.
func (s *Service) Create(ctx context.Context, tx dbport.Tx, req CreateRequest) (Manifest, error) {
	return s.create(ctx, tx, req, uuid.Nil, "")
}

// Supersede records a corrective epoch: a fresh checkpoint, numbered after
// the current head, that names the epoch it corrects and says why.
//
// It never touches the corrected epoch. Its row is append-only at the
// database level and its signature is left exactly as it was made, because
// the fact that a particular attestation was signed at a particular time is
// itself part of the record. "Late discovery" - finding out afterwards that
// a checkpoint was taken over an already-damaged ledger - is recorded by
// adding evidence, not by editing it.
func (s *Service) Supersede(ctx context.Context, tx dbport.Tx, req CreateRequest, correctsEpoch int64, reason string) (Manifest, error) {
	if reason == "" {
		return Manifest{}, ErrManifestInvalid{
			EpochNumber: correctsEpoch,
			Missing:     []string{"a corrective epoch must say why it supersedes the epoch it names"},
		}
	}
	corrected, err := s.store.Read(ctx, tx, req.Tenant, correctsEpoch)
	if err != nil {
		return Manifest{}, err
	}
	return s.create(ctx, tx, req, corrected.EpochID, reason)
}

func (s *Service) create(ctx context.Context, tx dbport.Tx, req CreateRequest, correctsEpochID uuid.UUID, reason string) (Manifest, error) {
	if req.Tenant == uuid.Nil {
		return Manifest{}, ErrManifestInvalid{Missing: []string{"tenant is required"}}
	}
	at := req.At
	if at.IsZero() {
		at = s.now()
	}
	at = Truncate(at)

	heads, err := readCoverage(ctx, tx, req.Tenant, at)
	if err != nil {
		return Manifest{}, err
	}

	previous, hasPrevious, err := s.store.Latest(ctx, tx, req.Tenant)
	if err != nil {
		return Manifest{}, err
	}

	coversFrom := time.Time{}
	epochNumber := int64(1)
	manifest := Manifest{
		SchemaVersion: ManifestSchemaVersion,
		Tenant:        req.Tenant,
		EpochID:       s.newID(),
		Schema:        req.Schema,
		Streams:       heads,
		CreatedAt:     at,
	}
	if hasPrevious {
		epochNumber = previous.EpochNumber + 1
		coversFrom = previous.CoversTo
		digest, digestErr := previous.CanonicalDigest()
		if digestErr != nil {
			return Manifest{}, digestErr
		}
		manifest.PreviousEpochID = previous.EpochID
		manifest.PreviousManifestDigest = digest
	} else {
		// The first epoch covers the ledger from its earliest recording.
		earliest, found, earliestErr := earliestRecordedAt(ctx, tx, req.Tenant, at)
		if earliestErr != nil {
			return Manifest{}, earliestErr
		}
		if !found {
			return Manifest{}, ErrIncompleteCoverage{
				Tenant: req.Tenant,
				Reason: "the tenant has no events recorded before the checkpoint instant",
			}
		}
		coversFrom = earliest
	}
	manifest.EpochNumber = epochNumber
	manifest.CoversFrom = Truncate(coversFrom)
	manifest.CoversTo = at
	manifest.CorrectsEpochID = correctsEpochID
	manifest.CorrectsReason = reason

	// A checkpoint taken at the same instant as its predecessor would cover
	// an empty window and prove nothing new; refuse it rather than record an
	// attestation with no content.
	if !manifest.CoversFrom.Before(manifest.CoversTo) {
		return Manifest{}, ErrManifestInvalid{
			EpochNumber: epochNumber,
			Missing: []string{fmt.Sprintf("the covered window [%s, %s) is empty; nothing has been recorded since the previous epoch",
				manifest.CoversFrom.Format(time.RFC3339Nano), manifest.CoversTo.Format(time.RFC3339Nano))},
		}
	}

	root, err := ComputeRootDigest(heads)
	if err != nil {
		return Manifest{}, err
	}
	manifest.RootDigest = root
	manifest.RootDigestAlgorithm = DigestAlgorithm

	signed, err := Sign(manifest, s.signer, s.dir)
	if err != nil {
		return Manifest{}, err
	}
	if err := s.store.Append(ctx, tx, signed, s.dir); err != nil {
		return Manifest{}, err
	}
	if _, err := s.anchor.Anchor(ctx, signed); err != nil {
		return signed, fmt.Errorf("checkpoint: anchor epoch %d with %s: %w", signed.EpochNumber, s.anchor.Name(), err)
	}
	return signed, nil
}

// readCoverage returns one head per stream that holds an event recorded
// before at, each joined to the hash-chain link recorded for that exact
// sequence.
//
// The head is cumulative - a chain hash at sequence N folds every event
// before it - so a checkpoint's window describes what is new since the last
// epoch while its heads describe the whole stream. A stream whose head has
// no chain link is reported as uncoverable rather than silently omitted:
// omitting it would produce a manifest that looks complete and is not.
func readCoverage(ctx context.Context, q Querier, tenant uuid.UUID, at time.Time) ([]StreamHead, error) {
	rows, err := q.Query(ctx, `
		SELECT head.stream_key, head.head_sequence, link.chain_hash, link.chain_algorithm
		FROM (
			SELECT stream_key, MAX(sequence) AS head_sequence
			FROM ledger_event
			WHERE tenant_id = $1 AND recorded_at < $2
			GROUP BY stream_key
		) head
		LEFT JOIN ledger_hash_chain_link link
			ON link.tenant_id = $1
		   AND link.stream_key = head.stream_key
		   AND link.sequence = head.head_sequence
		ORDER BY head.stream_key ASC`, tenant, at)
	if err != nil {
		return nil, fmt.Errorf("checkpoint: read coverage: %w", err)
	}
	defer rows.Close()

	var (
		heads   []StreamHead
		missing []string
	)
	for rows.Next() {
		var (
			head      StreamHead
			chainHash *string
			algorithm *string
		)
		if err := rows.Scan(&head.StreamKey, &head.Sequence, &chainHash, &algorithm); err != nil {
			return nil, fmt.Errorf("checkpoint: scan coverage row: %w", err)
		}
		if chainHash == nil || algorithm == nil {
			missing = append(missing, fmt.Sprintf("%s@%d", head.StreamKey, head.Sequence))
			continue
		}
		head.ChainHash = *chainHash
		head.ChainAlgorithm = *algorithm
		heads = append(heads, head)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("checkpoint: read coverage: %w", err)
	}
	if len(missing) > 0 {
		return nil, ErrIncompleteCoverage{
			Tenant:  tenant,
			Missing: missing,
			Reason:  "these stream heads have no recorded hash-chain link and cannot be attested to",
		}
	}
	if len(heads) == 0 {
		return nil, ErrIncompleteCoverage{
			Tenant: tenant,
			Reason: "no stream holds an event recorded before the checkpoint instant",
		}
	}
	return heads, nil
}

// earliestRecordedAt returns the first instant this tenant recorded
// anything before at.
func earliestRecordedAt(ctx context.Context, q Querier, tenant uuid.UUID, at time.Time) (time.Time, bool, error) {
	var earliest *time.Time
	if err := q.QueryRow(ctx, `
		SELECT MIN(recorded_at) FROM ledger_event
		WHERE tenant_id = $1 AND recorded_at < $2`, tenant, at).Scan(&earliest); err != nil {
		return time.Time{}, false, fmt.Errorf("checkpoint: read earliest recording: %w", err)
	}
	if earliest == nil {
		return time.Time{}, false, nil
	}
	return *earliest, true, nil
}

// VerifyChain verifies every epoch a tenant has, independently and in order.
//
// Each manifest is checked on its own terms first ([Verify]: root digest
// re-folded, canonical digest re-projected, signature re-checked, signing
// key re-authorized as of the moment it signed), and then against its
// neighbours: epoch numbers must be contiguous from 1, each epoch must name
// its immediate predecessor, and the predecessor's manifest digest must
// reproduce. A removed epoch therefore breaks the chain at a named number
// rather than closing the gap silently.
//
// It returns the number of epochs verified.
func VerifyChain(ctx context.Context, q Querier, tenant uuid.UUID, dir KeyDirectory) (int, error) {
	epochs, err := NewStore().List(ctx, q, tenant)
	if err != nil {
		return 0, err
	}

	var previousDigest string
	var previousID uuid.UUID
	for i, m := range epochs {
		want := int64(i + 1)
		if m.EpochNumber != want {
			return i, ErrEpochChainBroken{
				Tenant: tenant, EpochNumber: want, Reason: "epoch numbering gap or reordering",
				Expected: fmt.Sprintf("%d", want), Actual: fmt.Sprintf("%d", m.EpochNumber),
			}
		}
		if err := Verify(m, dir); err != nil {
			return i, err
		}
		if want > 1 {
			if m.PreviousEpochID != previousID {
				return i, ErrEpochChainBroken{
					Tenant: tenant, EpochNumber: want, Reason: "epoch does not name its immediate predecessor",
					Expected: previousID.String(), Actual: m.PreviousEpochID.String(),
				}
			}
			if m.PreviousManifestDigest != previousDigest {
				return i, ErrEpochChainBroken{
					Tenant: tenant, EpochNumber: want, Reason: "recorded previous manifest digest does not reproduce",
					Expected: previousDigest, Actual: m.PreviousManifestDigest,
				}
			}
		}
		digest, digestErr := m.CanonicalDigest()
		if digestErr != nil {
			return i, digestErr
		}
		previousDigest = digest
		previousID = m.EpochID
	}
	return len(epochs), nil
}
