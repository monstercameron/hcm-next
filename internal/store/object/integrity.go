package object

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/envelope"
)

// This file adds integrity verification and repair on top of
// SealedObjectStore (sealed.go). SealedObjectStore already keeps one
// canonical, tenant-bound envelope per object and truthful metadata about
// it (Info.Digest, Info.Generation) even after crypto-erasure. That
// canonical copy and its metadata are never touched here. What this file
// adds is a second concern: independent physical replicas of an object's
// stored envelope bytes (the kind of thing a persistence adapter would
// actually keep on multiple disks or regions), and the ability to verify
// each replica against the object's own recorded identity and generation,
// quarantine an object whose serving replica fails that check, and repair
// it — but only by copying bytes from another replica that itself just
// passed verification. A digest that is merely self-consistent with a
// replica's own bytes is not accepted as proof: every check is made
// against SealedObjectStore's live Stat, never against anything a replica
// claims about itself.

// ReplicaLocation names one physical copy of an object's stored envelope
// bytes (a disk, a region, a backup target). It carries no meaning beyond
// being a label a caller chooses.
type ReplicaLocation string

// VerificationOutcome is the result of checking one replica.
type VerificationOutcome string

const (
	OutcomeVerified       VerificationOutcome = "VERIFIED"
	OutcomeBitRot         VerificationOutcome = "BIT_ROT"
	OutcomeGenerationSwap VerificationOutcome = "GENERATION_SWAP"
	OutcomeMissingReplica VerificationOutcome = "MISSING_REPLICA"
	OutcomePartialReplica VerificationOutcome = "PARTIAL_REPLICA"
)

var (
	// ErrQuarantined is returned by Get when the object's serving replica
	// has failed verification. It is always wrapped together with a more
	// specific sentinel identifying which RED condition caused it.
	ErrQuarantined = errors.New("object: object is quarantined pending a verified repair")
	// ErrBitRot means a replica's recomputed digest does not match the
	// object's recorded digest even though the same generation is claimed.
	ErrBitRot = errors.New("object: replica failed bit-rot verification")
	// ErrGenerationSwap means a replica is an internally well-formed,
	// correctly-digested envelope, but for an older generation of the
	// object than the one currently recorded as authoritative.
	ErrGenerationSwap = errors.New("object: replica generation does not match the object's current generation")
	// ErrReplicaMissing means the named replica has no stored bytes at all.
	ErrReplicaMissing = errors.New("object: replica is missing")
	// ErrReplicaPartial means the named replica's stored bytes exist but
	// are not a complete, well-formed envelope.
	ErrReplicaPartial = errors.New("object: replica is incomplete")
	// ErrNoVerifiedReplica is returned by Repair/RepairAny when no
	// candidate replica passes verification. Repair refuses rather than
	// guess from whatever bytes happen to be available.
	ErrNoVerifiedReplica = errors.New("object: repair refused, no verified replica is available")
)

// Receipt is the immutable record of one verification or repair operation.
// It is modeled on envelope.Evidence (internal/trust/envelope): it names
// the object and what happened without carrying plaintext or key material,
// so it is always safe to persist or export. Callers only ever receive
// copies (see Receipts and the return values of Verify/Repair); nothing in
// this package hands out a pointer into the stored log.
type Receipt struct {
	ReceiptID  string
	ObjectID   string
	Generation uint64
	Location   ReplicaLocation
	Outcome    VerificationOutcome
	Repaired   bool
	At         time.Time
	Detail     string
}

// replicaRecord is one physical copy of an object's stored envelope bytes,
// as tracked by the integrity layer, independent of SealedObjectStore's own
// canonical entry. A zero-value record (present=false) is exactly what a
// location that was never published, or one that was dropped, looks like.
type replicaRecord struct {
	Present    bool
	Bytes      []byte
	Generation uint64
}

// IntegrityStore verifies and repairs the physical replicas of objects held
// in a SealedObjectStore. It never stores plaintext or key material itself;
// it only ever moves already-sealed envelope bytes between replica slots.
type IntegrityStore struct {
	store   *SealedObjectStore
	manager *envelope.Manager

	mu          sync.Mutex
	replicas    map[string]map[ReplicaLocation]replicaRecord
	serving     map[string]ReplicaLocation
	quarantined map[string]bool
	receipts    map[string][]Receipt
	seq         uint64
}

// NewIntegrityStore binds an integrity layer to an existing sealed object
// store. A nil store is a caller error: there is no sealed data to verify.
func NewIntegrityStore(store *SealedObjectStore) (*IntegrityStore, error) {
	if store == nil || store.manager == nil {
		return nil, fmt.Errorf("%w: a sealed object store is required", ErrInvalidRequest)
	}
	return &IntegrityStore{
		store:       store,
		manager:     store.manager,
		replicas:    make(map[string]map[ReplicaLocation]replicaRecord),
		serving:     make(map[string]ReplicaLocation),
		quarantined: make(map[string]bool),
		receipts:    make(map[string][]Receipt),
	}, nil
}

// PublishReplica snapshots the object's current envelope, exactly as
// SealedObjectStore stores it, into a named replica slot. The first replica
// published for an object becomes its serving location (see Get); a
// distinct SetServingLocation call can change that later. Republishing a
// location refreshes it to the object's current generation, which is how a
// real replication pass would keep a replica from going stale — a location
// that is never republished after the object advances is exactly what
// models a replica that fell behind (see the generation-swap fault).
func (i *IntegrityStore) PublishReplica(objectID string, location ReplicaLocation) (Info, error) {
	if i == nil {
		return Info{}, ErrEncryptionUnavailable
	}
	info, err := i.store.Stat(context.Background(), objectID)
	if err != nil {
		return Info{}, err
	}
	env, err := i.store.Envelope(objectID)
	if err != nil {
		return Info{}, err
	}
	envBytes, err := json.Marshal(env)
	if err != nil {
		return Info{}, fmt.Errorf("%w: encode envelope: %v", ErrEncryptionUnavailable, err)
	}

	i.mu.Lock()
	defer i.mu.Unlock()
	if i.replicas[objectID] == nil {
		i.replicas[objectID] = make(map[ReplicaLocation]replicaRecord)
	}
	i.replicas[objectID][location] = replicaRecord{
		Present:    true,
		Bytes:      envBytes,
		Generation: info.Generation,
	}
	if _, ok := i.serving[objectID]; !ok {
		i.serving[objectID] = location
	}
	return info, nil
}

// SetServingLocation designates which replica location Get reads from. The
// location need not exist yet or be healthy; Get will report the honest
// outcome either way.
func (i *IntegrityStore) SetServingLocation(objectID string, location ReplicaLocation) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.serving[objectID] = location
}

// servingLocation returns the location Get/Repair treat as authoritative
// for serving, defaulting to "primary" if none has ever been published.
func (i *IntegrityStore) servingLocation(objectID string) ReplicaLocation {
	if loc, ok := i.serving[objectID]; ok {
		return loc
	}
	return "primary"
}

// Verify checks the stored replica at location against the object's
// independently-tracked authoritative generation and digest (Stat's live
// Info, never anything the replica itself claims). It always records an
// immutable receipt, and quarantines the object whenever the outcome is not
// VERIFIED and location is the object's current serving location.
func (i *IntegrityStore) Verify(ctx context.Context, objectID string, location ReplicaLocation) (Receipt, error) {
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	if i == nil {
		return Receipt{}, ErrEncryptionUnavailable
	}
	authoritative, err := i.store.Stat(ctx, objectID)
	if err != nil {
		return Receipt{}, err
	}

	i.mu.Lock()
	rec := i.replicas[objectID][location]
	i.mu.Unlock()

	outcome, detail := evaluateReplica(rec, authoritative)

	i.mu.Lock()
	if outcome != OutcomeVerified && location == i.servingLocation(objectID) {
		i.quarantined[objectID] = true
	}
	i.mu.Unlock()

	return i.recordReceipt(objectID, authoritative.Generation, location, outcome, false, detail), nil
}

// evaluateReplica is the pure decision function behind Verify: given what a
// replica actually holds and what the object's authoritative metadata
// says, decide the outcome. It never trusts anything the replica claims
// about its own correctness beyond the raw bytes and the generation label
// attached when the replica was captured.
func evaluateReplica(rec replicaRecord, authoritative Info) (VerificationOutcome, string) {
	if !rec.Present || len(rec.Bytes) == 0 {
		return OutcomeMissingReplica, "replica has no stored bytes"
	}
	if !json.Valid(rec.Bytes) {
		return OutcomePartialReplica, fmt.Sprintf("replica bytes are not a complete envelope (%d bytes)", len(rec.Bytes))
	}
	if rec.Generation != authoritative.Generation {
		return OutcomeGenerationSwap, fmt.Sprintf("replica claims generation %d, object is at generation %d", rec.Generation, authoritative.Generation)
	}
	if digest(rec.Bytes) != authoritative.Digest {
		return OutcomeBitRot, "replica digest does not match the object's recorded digest"
	}
	return OutcomeVerified, ""
}

// outcomeError maps a non-VERIFIED outcome to its typed sentinel.
func outcomeError(outcome VerificationOutcome) error {
	switch outcome {
	case OutcomeBitRot:
		return ErrBitRot
	case OutcomeGenerationSwap:
		return ErrGenerationSwap
	case OutcomeMissingReplica:
		return ErrReplicaMissing
	case OutcomePartialReplica:
		return ErrReplicaPartial
	default:
		return nil
	}
}

// Quarantined reports whether objectID's serving replica has failed
// verification and is currently unavailable to Get.
func (i *IntegrityStore) Quarantined(objectID string) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.quarantined[objectID]
}

// Get serves objectID from its current serving replica, verifying that
// replica first. A quarantined object, or one whose serving replica fails
// verification right now, fails closed with ErrQuarantined wrapping the
// specific fault; it never returns suspect bytes.
func (i *IntegrityStore) Get(ctx context.Context, cctx custody.Context, objectID string) ([]byte, Info, error) {
	if err := ctx.Err(); err != nil {
		return nil, Info{}, err
	}
	if i == nil {
		return nil, Info{}, ErrEncryptionUnavailable
	}
	authoritative, err := i.store.Stat(ctx, objectID)
	if err != nil {
		return nil, Info{}, err
	}
	location := i.servingLocation(objectID)

	i.mu.Lock()
	rec := i.replicas[objectID][location]
	i.mu.Unlock()

	outcome, detail := evaluateReplica(rec, authoritative)
	i.recordReceipt(objectID, authoritative.Generation, location, outcome, false, detail)
	if outcome != OutcomeVerified {
		i.mu.Lock()
		i.quarantined[objectID] = true
		i.mu.Unlock()
		return nil, Info{}, fmt.Errorf("%w: %w: %s", ErrQuarantined, outcomeError(outcome), detail)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(rec.Bytes, &env); err != nil {
		return nil, Info{}, fmt.Errorf("%w: %v", ErrSealedEnvelope, err)
	}
	plaintext, err := openObject(i.manager, cctx, objectID, env)
	if err != nil {
		return nil, Info{}, err
	}

	i.mu.Lock()
	i.quarantined[objectID] = false
	i.mu.Unlock()
	return plaintext, authoritative, nil
}

// Repair restores the object's serving replica from the named candidate,
// but only after that candidate independently passes verification.
// Repairing from whatever bytes happen to be available, without checking
// them first, is exactly the defect this method exists to prevent: a
// failed or missing candidate leaves the object quarantined and returns
// ErrNoVerifiedReplica rather than guessing. A successful repair keeps the
// object's identity untouched and appends an immutable receipt with
// Repaired set.
func (i *IntegrityStore) Repair(ctx context.Context, cctx custody.Context, objectID string, from ReplicaLocation) (Receipt, error) {
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	if i == nil {
		return Receipt{}, ErrEncryptionUnavailable
	}
	authoritative, err := i.store.Stat(ctx, objectID)
	if err != nil {
		return Receipt{}, err
	}

	// Evaluating the candidate and copying it happen under ONE lock hold, so
	// the bytes written are byte-for-byte the ones that passed verification.
	// Calling the exported Verify here instead would release the lock between
	// the check and the copy, and PublishReplica could land in that window --
	// repair would then source bytes nothing ever verified, which is the one
	// guarantee this todo exists to provide.
	i.mu.Lock()
	source := i.replicas[objectID][from]
	outcome, detail := evaluateReplica(source, authoritative)
	serving := i.servingLocation(objectID)
	if outcome != OutcomeVerified {
		if from == serving {
			i.quarantined[objectID] = true
		}
		i.mu.Unlock()
		i.recordReceipt(objectID, authoritative.Generation, from, outcome, false, detail)
		return Receipt{}, fmt.Errorf("%w: candidate replica %q is %s", ErrNoVerifiedReplica, from, outcome)
	}
	if i.replicas[objectID] == nil {
		i.replicas[objectID] = make(map[ReplicaLocation]replicaRecord)
	}
	i.replicas[objectID][serving] = replicaRecord{
		Present:    true,
		Bytes:      append([]byte(nil), source.Bytes...),
		Generation: source.Generation,
	}
	i.quarantined[objectID] = false
	i.mu.Unlock()

	return i.recordReceipt(objectID, authoritative.Generation, serving, OutcomeVerified, true,
		fmt.Sprintf("repaired serving replica %q from verified replica %q", serving, from)), nil
}

// RepairAny tries every replica location registered for objectID, in a
// stable order, and repairs the serving replica from the first one that
// passes verification. It returns ErrNoVerifiedReplica if none do.
func (i *IntegrityStore) RepairAny(ctx context.Context, cctx custody.Context, objectID string) (Receipt, error) {
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	i.mu.Lock()
	locations := make([]ReplicaLocation, 0, len(i.replicas[objectID]))
	for loc := range i.replicas[objectID] {
		locations = append(locations, loc)
	}
	i.mu.Unlock()
	sort.Slice(locations, func(a, b int) bool { return locations[a] < locations[b] })

	for _, loc := range locations {
		receipt, err := i.Repair(ctx, cctx, objectID, loc)
		if err == nil {
			return receipt, nil
		}
		if !errors.Is(err, ErrNoVerifiedReplica) {
			return Receipt{}, err
		}
	}
	return Receipt{}, fmt.Errorf("%w: object %s", ErrNoVerifiedReplica, objectID)
}

// Receipts returns a defensive copy of every receipt recorded for objectID,
// oldest first. The returned slice and its elements can be freely mutated
// by the caller without affecting the stored log.
func (i *IntegrityStore) Receipts(objectID string) []Receipt {
	i.mu.Lock()
	defer i.mu.Unlock()
	stored := i.receipts[objectID]
	out := make([]Receipt, len(stored))
	copy(out, stored)
	return out
}

// recordReceipt appends an immutable receipt to the log under lock and
// returns a copy of it.
func (i *IntegrityStore) recordReceipt(objectID string, generation uint64, location ReplicaLocation, outcome VerificationOutcome, repaired bool, detail string) Receipt {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.seq++
	receipt := Receipt{
		ReceiptID:  fmt.Sprintf("integrity-%s-%06d", objectID, i.seq),
		ObjectID:   objectID,
		Generation: generation,
		Location:   location,
		Outcome:    outcome,
		Repaired:   repaired,
		At:         time.Now().UTC(),
		Detail:     detail,
	}
	i.receipts[objectID] = append(i.receipts[objectID], receipt)
	return receipt
}

// ExplainIntegrity summarizes the integrity layer's state for policy and
// operator evidence, without naming any tenant or exposing key material.
func ExplainIntegrity(i *IntegrityStore) string {
	if i == nil {
		return "object: integrity store unavailable"
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	quarantinedCount := 0
	for _, q := range i.quarantined {
		if q {
			quarantinedCount++
		}
	}
	return fmt.Sprintf("object: %d object(s) tracked, %d quarantined", len(i.replicas), quarantinedCount)
}
