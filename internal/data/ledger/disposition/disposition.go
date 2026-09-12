package disposition

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/recordsmeta"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

// Sentinel and typed errors. None of them is a Go zero value: a caller that
// does not check the returned error explicitly cannot mistake a refusal for
// success, and Erase changes nothing in the database when any of them is
// returned.
var (
	// ErrRequestInvalid reports a missing or malformed EraseRequest field.
	ErrRequestInvalid = errors.New("disposition: request is invalid")
	// ErrNoInlinePayload reports that the named event has no inline payload
	// to erase (it is an ArtifactRef-only event, or was already erased --
	// callers get ErrAlreadyDisposed for the latter).
	ErrNoInlinePayload = errors.New("disposition: event carries no inline payload to erase")
	// ErrAlreadyDisposed reports that a disposition row already exists for
	// this event. Erase never overwrites one.
	ErrAlreadyDisposed = errors.New("disposition: event payload was already disposed")
	// ErrLinkMismatch reports that the supplied LinkID names a tracked copy
	// whose own ledger_stream/ledger_sequence do not match the event the
	// request named. Erase refuses rather than trust the caller's pairing.
	ErrLinkMismatch = errors.New("disposition: link does not reference the requested event")
)

// ErrEventHeld reports that Erase refused to run because an active legal
// hold grips this event's tracked copy. LEDGER-011 does not reimplement
// hold semantics: this wraps recordsmeta.ErrCopyHeld, the same sentinel
// RECORDS-HOLD-001's DisposeCopy returns, under a type that names the exact
// event so a caller does not need to import recordsmeta just to branch on
// it.
type ErrEventHeld struct {
	Tenant    uuid.UUID
	StreamKey string
	Sequence  int64
}

// Code returns the stable identifier for this refusal.
func (ErrEventHeld) Code() string { return "LEDGER_PAYLOAD_ERASURE_HELD" }

func (e ErrEventHeld) Error() string {
	return fmt.Sprintf("%s: event %s@%d is under an active legal hold and cannot be erased",
		e.Code(), e.StreamKey, e.Sequence)
}

// Unwrap lets callers test errors.Is(err, recordsmeta.ErrCopyHeld) without
// this package re-declaring a second, unrelated sentinel for the same fact.
func (e ErrEventHeld) Unwrap() error { return recordsmeta.ErrCopyHeld }

// EraseRequest is everything a caller must state to erase one event's
// inline payload. DeclarationID and LinkID name the record_declaration and
// record_copy_link row (recordsmeta, DATA-018/RECORDS-HOLD-001) that already
// track this event as a governed copy: LEDGER-011 requires an event to be
// under declared records governance before its payload can be erased, the
// same way RECORDS-DISP-001 requires a declaration before disposition, so
// there is always a hold-checkable copy to consult rather than a payload
// that can be destroyed with nothing to prove no hold applied.
type EraseRequest struct {
	Tenant         uuid.UUID
	StreamKey      string
	Sequence       int64
	DeclarationID  uuid.UUID
	LinkID         uuid.UUID
	Classification Classification
	Reason         string
	Actor          string
	At             time.Time
}

func (r EraseRequest) validate() error {
	switch {
	case r.Tenant == uuid.Nil:
		return fmt.Errorf("%w: Tenant is required", ErrRequestInvalid)
	case r.StreamKey == "":
		return fmt.Errorf("%w: StreamKey is required", ErrRequestInvalid)
	case r.Sequence < 1:
		return fmt.Errorf("%w: Sequence must be positive", ErrRequestInvalid)
	case r.DeclarationID == uuid.Nil:
		return fmt.Errorf("%w: DeclarationID is required", ErrRequestInvalid)
	case r.LinkID == uuid.Nil:
		return fmt.Errorf("%w: LinkID is required", ErrRequestInvalid)
	case r.Reason == "":
		return fmt.Errorf("%w: Reason is required", ErrRequestInvalid)
	case r.Actor == "":
		return fmt.Errorf("%w: Actor is required", ErrRequestInvalid)
	case r.At.IsZero():
		return fmt.Errorf("%w: At is required", ErrRequestInvalid)
	}
	return nil
}

// Erase destroys public read access to one ledger event's inline payload
// while leaving the event's chronology (sequence, the three times,
// correlation) and its digest exactly as recorded.
//
// It runs entirely inside the caller's transaction, alongside
// recordsmeta.DisposeCopy, so the hold check and the destruction are one
// atomic unit and there is no gap between them for a concurrent
// PropagateHold to land in: DisposeCopy takes its own row lock on
// record_copy_link (SELECT ... FOR UPDATE) before it tests hold_state, that
// lock is held for the rest of this transaction, and PropagateHold takes the
// identical lock before it can ever set hold_state to HELD. Erase makes no
// decision and no write between DisposeCopy succeeding and its own INSERT
// into ledger_payload_disposition.
//
// Erase never issues UPDATE or DELETE against ledger_event. It relies on
// that table's own append-only trigger and grants rather than duplicating
// the guarantee, and it never needs to: what it destroys is read access
// through this package, recorded as a new fact, not a mutation of an old
// one.
func Erase(ctx context.Context, tx dbport.Tx, req EraseRequest) (View, error) {
	if err := req.validate(); err != nil {
		return View{}, err
	}
	mechanism, state, err := Classify(req.Classification)
	if err != nil {
		return View{}, err
	}
	if err := tenancy.WithTenant(ctx, tx, req.Tenant); err != nil {
		return View{}, fmt.Errorf("disposition: set tenant context: %w", err)
	}

	rec, err := ledger.NewReader().ReadEvent(ctx, tx, req.Tenant, req.StreamKey, req.Sequence)
	if err != nil {
		return View{}, err
	}
	// An already-disposed event reports PayloadErased/PayloadRestricted from
	// ReadEvent itself, with Payload already nil (ledger.Reader withholds it
	// unconditionally, LEDGER-011). Check that state before checking
	// rec.Payload == nil: otherwise a second Erase attempt against an
	// already-erased event would misreport ErrNoInlinePayload -- true of the
	// withheld read, but not what actually happened -- instead of the more
	// precise ErrAlreadyDisposed.
	if rec.PayloadState == ledger.PayloadErased || rec.PayloadState == ledger.PayloadRestricted {
		return View{}, fmt.Errorf("%w: %s@%d", ErrAlreadyDisposed, req.StreamKey, req.Sequence)
	}
	if rec.Payload == nil {
		return View{}, fmt.Errorf("%w: %s@%d", ErrNoInlinePayload, req.StreamKey, req.Sequence)
	}

	// Lock the declaration first, as its own statement, so every Erase
	// attempt against the same declaration serializes on one row rather than
	// racing straight to the disposition uniqueness check below. This is
	// the same "lock, then ask, then act" discipline
	// internal/data/recordsmeta.ReleaseHold uses for exactly the reason
	// documented there: relying on snapshot reads under concurrent writers
	// is how a resurrection bug gets in.
	var declarationLock int
	err = tx.QueryRow(ctx, `SELECT 1 FROM record_declaration WHERE tenant_id=$1 AND declaration_id=$2 FOR UPDATE`,
		req.Tenant, req.DeclarationID).Scan(&declarationLock)
	if errors.Is(err, dbport.ErrNoRows) {
		return View{}, fmt.Errorf("%w: declaration %s not found", ErrRequestInvalid, req.DeclarationID)
	}
	if err != nil {
		return View{}, fmt.Errorf("disposition: lock declaration %s: %w", req.DeclarationID, err)
	}

	var linkStream string
	var linkSequence int64
	err = tx.QueryRow(ctx, `SELECT ledger_stream, ledger_sequence FROM record_copy_link WHERE tenant_id=$1 AND declaration_id=$2 AND link_id=$3`,
		req.Tenant, req.DeclarationID, req.LinkID).Scan(&linkStream, &linkSequence)
	if errors.Is(err, dbport.ErrNoRows) {
		return View{}, fmt.Errorf("%w: link %s not found under declaration %s", recordsmeta.ErrCopyNotFound, req.LinkID, req.DeclarationID)
	}
	if err != nil {
		return View{}, fmt.Errorf("disposition: load copy link %s: %w", req.LinkID, err)
	}
	if linkStream != req.StreamKey || linkSequence != req.Sequence {
		return View{}, fmt.Errorf("%w: link %s names %s@%d, request named %s@%d",
			ErrLinkMismatch, req.LinkID, linkStream, linkSequence, req.StreamKey, req.Sequence)
	}

	if _, found, err := loadDisposition(ctx, tx, req.Tenant, req.StreamKey, req.Sequence); err != nil {
		return View{}, err
	} else if found {
		return View{}, fmt.Errorf("%w: %s@%d", ErrAlreadyDisposed, req.StreamKey, req.Sequence)
	}

	// The hold check and the destructive act are this one call: DisposeCopy
	// locks record_copy_link FOR UPDATE, tests hold_state under that lock,
	// and only then marks the copy disposed. If it refuses, nothing below
	// runs and nothing above this point wrote anything either.
	if _, err := recordsmeta.DisposeCopy(ctx, tx, req.Tenant, req.DeclarationID, req.LinkID, req.At); err != nil {
		if errors.Is(err, recordsmeta.ErrCopyHeld) {
			return View{}, ErrEventHeld{Tenant: req.Tenant, StreamKey: req.StreamKey, Sequence: req.Sequence}
		}
		return View{}, fmt.Errorf("disposition: dispose copy %s: %w", req.LinkID, err)
	}

	var keyRef, keyDestroyedAt any
	if mechanism == MechanismCryptoErasure {
		// Symbolic: see the package doc for why this names a destroyed key
		// reference as governance evidence rather than proving the
		// underlying bytes became cryptographically unrecoverable.
		keyRef = uuid.NewString()
		keyDestroyedAt = req.At.UTC()
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO ledger_payload_disposition (
			tenant_id, stream_key, sequence, event_id, declaration_id, link_id,
			classification, mechanism, state, reason, actor,
			key_ref, key_destroyed_at, digest, digest_algorithm, recorded_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		req.Tenant, req.StreamKey, req.Sequence, rec.EventID, req.DeclarationID, req.LinkID,
		string(req.Classification), string(mechanism), string(state), req.Reason, req.Actor,
		keyRef, keyDestroyedAt, rec.Digest, rec.DigestAlgorithm, req.At.UTC(),
	); err != nil {
		return View{}, fmt.Errorf("disposition: record erasure of %s@%d: %w", req.StreamKey, req.Sequence, err)
	}

	return View{
		Tenant: req.Tenant, StreamKey: req.StreamKey, Sequence: req.Sequence,
		State: state, Classification: req.Classification, Mechanism: mechanism,
		Reason: req.Reason, Actor: req.Actor, RecordedAt: req.At.UTC(),
	}, nil
}

// ReadView is the one supported way to learn what an event's payload
// disposition is. It never returns internal/data/ledger.EventRecord.Payload
// directly: it consults, in order, whether a disposition row exists (the
// event was erased -- State is StatePayloadErased or StateRestricted, never
// the original bytes), whether the tracked copy naming this event is
// currently held (State is StateHeld), and only then falls back to the raw
// event row to report StatePresent or StateReferenced.
func ReadView(ctx context.Context, q ledger.Querier, tenant uuid.UUID, streamKey string, sequence int64) (View, error) {
	rec, err := ledger.NewReader().ReadEvent(ctx, q, tenant, streamKey, sequence)
	if err != nil {
		return View{}, err
	}

	disp, found, err := loadDisposition(ctx, q, tenant, streamKey, sequence)
	if err != nil {
		return View{}, err
	}
	if found {
		return View{
			Tenant: tenant, StreamKey: streamKey, Sequence: sequence,
			State: disp.State, Classification: disp.Classification, Mechanism: disp.Mechanism,
			Reason: disp.Reason, Actor: disp.Actor, RecordedAt: disp.RecordedAt,
		}, nil
	}

	holdID, held, err := activeHold(ctx, q, tenant, streamKey, sequence)
	if err != nil {
		return View{}, err
	}
	if held {
		return View{Tenant: tenant, StreamKey: streamKey, Sequence: sequence, State: StateHeld, HoldID: holdID}, nil
	}

	if rec.Payload == nil {
		return View{Tenant: tenant, StreamKey: streamKey, Sequence: sequence, State: StateReferenced, ArtifactRef: rec.ArtifactRef}, nil
	}
	return View{
		Tenant: tenant, StreamKey: streamKey, Sequence: sequence,
		State: StatePresent, Bytes: append([]byte(nil), rec.Payload...),
	}, nil
}

// loadDisposition reads the disposition row for one event, if any exists.
func loadDisposition(ctx context.Context, q dbport.Querier, tenant uuid.UUID, streamKey string, sequence int64) (Disposition, bool, error) {
	var (
		d                                     Disposition
		classification, mechanism, stateValue string
		keyRef                                *string
		keyDestroyedAt                        *time.Time
	)
	err := q.QueryRow(ctx, `
		SELECT event_id, declaration_id, link_id, classification, mechanism, state,
		       reason, actor, key_ref, key_destroyed_at, digest, digest_algorithm, recorded_at
		FROM ledger_payload_disposition
		WHERE tenant_id=$1 AND stream_key=$2 AND sequence=$3`,
		tenant, streamKey, sequence).Scan(
		&d.EventID, &d.DeclarationID, &d.LinkID, &classification, &mechanism, &stateValue,
		&d.Reason, &d.Actor, &keyRef, &keyDestroyedAt, &d.Digest, &d.DigestAlgorithm, &d.RecordedAt)
	if errors.Is(err, dbport.ErrNoRows) {
		return Disposition{}, false, nil
	}
	if err != nil {
		return Disposition{}, false, fmt.Errorf("disposition: read disposition for %s@%d: %w", streamKey, sequence, err)
	}
	d.Tenant, d.StreamKey, d.Sequence = tenant, streamKey, sequence
	d.Classification, d.Mechanism, d.State = Classification(classification), Mechanism(mechanism), PayloadState(stateValue)
	if keyRef != nil {
		d.KeyRef = *keyRef
	}
	d.KeyDestroyedAt = keyDestroyedAt
	return d, true, nil
}

// activeHold reports whether the tracked copy naming (streamKey, sequence)
// is currently held, and if so, which hold grips it. It reads
// record_copy_link.hold_state -- the field recordsmeta.PropagateHold and
// recordsmeta.ReleaseHold already maintain transactionally -- rather than
// re-deriving hold status from hold_intersection itself, so this package
// composes recordsmeta's own model instead of computing a second, possibly
// divergent answer to the same question.
func activeHold(ctx context.Context, q dbport.Querier, tenant uuid.UUID, streamKey string, sequence int64) (uuid.UUID, bool, error) {
	var linkID uuid.UUID
	var holdState string
	err := q.QueryRow(ctx, `SELECT link_id, hold_state FROM record_copy_link WHERE tenant_id=$1 AND ledger_stream=$2 AND ledger_sequence=$3`,
		tenant, streamKey, sequence).Scan(&linkID, &holdState)
	if errors.Is(err, dbport.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("disposition: read tracked copy for %s@%d: %w", streamKey, sequence, err)
	}
	if holdState != "HELD" {
		return uuid.Nil, false, nil
	}

	var holdID uuid.UUID
	err = q.QueryRow(ctx, `
		SELECT hi.hold_id FROM hold_intersection hi
		JOIN legal_hold h ON h.tenant_id = hi.tenant_id AND h.hold_id = hi.hold_id
		WHERE hi.tenant_id=$1 AND hi.link_id=$2 AND hi.state='ACTIVE' AND h.status <> 'RELEASED'
		ORDER BY hi.matched_at
		LIMIT 1`, tenant, linkID).Scan(&holdID)
	if errors.Is(err, dbport.ErrNoRows) {
		// record_copy_link.hold_state says HELD but no ACTIVE intersection
		// resolved to a specific hold -- fail closed by still reporting
		// Held with no identified hold, never by downgrading to Present.
		return uuid.Nil, true, nil
	}
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("disposition: resolve holding hold for copy %s: %w", linkID, err)
	}
	return holdID, true, nil
}
