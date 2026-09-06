// Package paymethodstore persists the payment-method and settlement table set
// declared by migrations/00106_paymethod.sql. Callers pass an already-open,
// tenant-scoped transaction so the database remains the authority for RLS and
// transaction lifetime.
package paymethodstore

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/domains/paymethod"
	"github.com/monstercameron/hcm-next/internal/domains/settlement"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// Executor is the driver-free capability required by this store.
type Executor interface {
	dbport.Execer
	dbport.Querier
}

// ErrorCode is stable telemetry for a refusal made by this adapter.
type ErrorCode string

const (
	CodeInvalid            ErrorCode = "PAYMETHOD_INVALID"
	CodeDuplicateRevision  ErrorCode = "PAYMETHOD_DUPLICATE_REVISION"
	CodeDuplicateEvent     ErrorCode = "PAYMETHOD_DUPLICATE_EVENT"
	CodeNotFound           ErrorCode = "PAYMETHOD_NOT_FOUND"
	CodeVersionConflict    ErrorCode = "PAYMETHOD_VERSION_CONFLICT"
	CodeIntegrityViolation ErrorCode = "PAYMETHOD_INTEGRITY_VIOLATION"
)

var (
	ErrInvalid          = errors.New("paymethodstore: invalid row")
	ErrDuplicate        = errors.New("paymethodstore: duplicate row")
	ErrNotFound         = errors.New("paymethodstore: not found")
	ErrVersionConflict  = errors.New("paymethodstore: version conflict")
	ErrIntegrity        = errors.New("paymethodstore: integrity violation")
	ErrReferenceInvalid = errors.New("paymethodstore: invalid UUID reference")
)

var (
	ErrDuplicateRevision = ErrDuplicate
	ErrDuplicateEvent    = ErrDuplicate
	ErrStaleRevision     = ErrVersionConflict
	ErrStaleCAS          = ErrVersionConflict
)

// Error is a typed store refusal. Callers can use errors.Is for Cause and
// errors.As to inspect Code without parsing PostgreSQL text.
type Error struct {
	Code   ErrorCode
	Cause  error
	Detail string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s: %s", e.Code, e.Cause, e.Detail) }
func (e *Error) Unwrap() error { return e.Cause }

func refusal(code ErrorCode, cause error, detail string) error {
	return &Error{Code: code, Cause: cause, Detail: detail}
}

func invalid(field, detail string) error {
	return fmt.Errorf("%w: %s: %s", ErrInvalid, field, detail)
}

// Store is stateless. Executor and TenantID are optional convenience fields
// for implementing paymethod.DestinationCatalog; the explicit methods below
// are preferred because they make transaction and tenant scope visible.
type Store struct {
	Executor Executor
	TenantID uuid.UUID
}

func New() Store { return Store{} }

// Put implements the existing in-memory catalog port when Store is bound with
// Executor and TenantID.
func (s Store) Put(destination paymethod.Destination) error {
	if s.Executor == nil || s.TenantID == uuid.Nil {
		return invalid("store", "Executor and TenantID are required")
	}
	_, err := s.PutDestination(context.Background(), s.Executor, s.TenantID, destination)
	return err
}

// Get implements the existing in-memory catalog port when Store is bound.
func (s Store) Get(id string) (paymethod.Destination, error) {
	if s.Executor == nil || s.TenantID == uuid.Nil {
		return paymethod.Destination{}, invalid("store", "Executor and TenantID are required")
	}
	return s.LoadDestination(context.Background(), s.Executor, s.TenantID, id, 0)
}

// RecordChange implements the existing in-memory catalog port as an initial
// awaiting-confirmation control revision. The richer BankDetailChange path is
// PutBankDetailChange, which persists the complete domain revision envelope.
func (s Store) RecordChange(change paymethod.DestinationChange) error {
	if s.Executor == nil || s.TenantID == uuid.Nil {
		return invalid("store", "Executor and TenantID are required")
	}
	if err := change.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	worker, err := uuid.Parse(change.WorkerRef)
	if err != nil {
		return fmt.Errorf("%w: worker_ref: %v", ErrReferenceInvalid, err)
	}
	from, to := effectiveProjection(change.Effective)
	_ = from
	_ = to
	// The existing catalog port carries a proposal, not the contact endpoint
	// proof required by BankDetailChange. It is recorded as revision one; the
	// full governed workflow uses PutBankDetailChange below.
	affected, err := s.Executor.Exec(context.Background(), `
		INSERT INTO paymethod_bank_detail_change (
			tenant_id,row_id,change_id,destination_id,worker_ref,before_digest,
			after_digest,requested_by,approver,requested_at,cooling_off,status,
			revision,canonical_digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,interval '0 seconds',$11,1,$12)
		ON CONFLICT DO NOTHING`, s.TenantID, uuid.New(), change.ID, change.DestinationID,
		worker, storageDigest(change.PreviousDigest), storageDigest(change.ProposedDigest),
		change.RequestedBy, change.Approver, time.Now().UTC(), "AWAITING_CONFIRMATION",
		storageDigest(change.CanonicalDigest))
	if err != nil {
		return fmt.Errorf("paymethodstore: record destination change %s: %w", change.ID, err)
	}
	if affected == 0 {
		return refusal(CodeDuplicateRevision, ErrDuplicate, "bank-detail change already exists")
	}
	return nil
}

// PutDestination appends one immutable destination revision.
func (s Store) PutDestination(ctx context.Context, ex Executor, tenantID uuid.UUID, in paymethod.Destination) (paymethod.Destination, error) {
	if err := requireTenant(tenantID); err != nil {
		return paymethod.Destination{}, err
	}
	if err := in.Validate(); err != nil {
		return paymethod.Destination{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	worker, err := uuid.Parse(in.WorkerRef)
	if err != nil {
		return paymethod.Destination{}, fmt.Errorf("%w: worker_ref: %v", ErrReferenceInvalid, err)
	}
	id := destinationID(in)
	if err := checkRevisionHead(ctx, ex, "paymethod_destination", "destination_id", tenantID, id, in.Revision, in.SupersedesRevision, in.SupersedesDigest); err != nil {
		return paymethod.Destination{}, err
	}
	from, to := effectiveProjection(in.Effective)
	affected, err := ex.Exec(ctx, `
		INSERT INTO paymethod_destination (
			tenant_id,row_id,destination_id,worker_ref,rail,risk_class,governed_ref,
			provider_ref,bank_detail_ref,token_ref,verification_state,effective_from,
			effective_to,state,revision,supersedes_revision,supersedes_digest,
			canonical_digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
		ON CONFLICT DO NOTHING`, tenantID, uuid.New(), id, worker, string(in.Rail),
		nullableText(string(in.RiskClass)), nullableText(in.GovernedRef), nullableText(in.ProviderRef),
		nullableText(in.BankDetailRef), nullableText(in.TokenRef), string(in.VerificationState),
		from, to, string(in.State), int64(in.Revision), nullableRevision(in.SupersedesRevision),
		nullableDigest(in.SupersedesDigest), storageDigest(in.CanonicalDigest))
	if err != nil {
		return paymethod.Destination{}, fmt.Errorf("paymethodstore: insert destination %s/%d: %w", id, in.Revision, err)
	}
	if affected == 0 {
		return paymethod.Destination{}, refusal(CodeDuplicateRevision, ErrDuplicate, "destination revision already exists")
	}
	return in, nil
}

// LoadDestination loads a requested revision. If revision is zero, the latest
// revision is returned. The SQL contract intentionally stores only the
// governed destination projection; fields absent from migration 00106 remain
// zero in the returned domain value and the stored digest remains authoritative.
func (s Store) LoadDestination(ctx context.Context, ex Executor, tenantID uuid.UUID, id string, revision uint64) (paymethod.Destination, error) {
	if err := requireTenant(tenantID); err != nil {
		return paymethod.Destination{}, err
	}
	if strings.TrimSpace(id) == "" {
		return paymethod.Destination{}, invalid("destination_id", "destination id is required")
	}
	whereRevision := ""
	args := []any{tenantID, id}
	if revision != 0 {
		whereRevision = " AND revision=$3"
		args = append(args, int64(revision))
	}
	order := " ORDER BY revision DESC LIMIT 1"
	var (
		rowID, worker                               uuid.UUID
		storedID, rail, verification, state, digest string
		risk, governed, provider, bank, token       *string
		from, to                                    *time.Time
		storedRevision                              int64
		supersedes                                  *int64
		supersedesDigest                            *string
	)
	err := ex.QueryRow(ctx, `SELECT row_id,destination_id,worker_ref,rail,risk_class,governed_ref,
		provider_ref,bank_detail_ref,token_ref,verification_state,effective_from,effective_to,
		state,revision,supersedes_revision,supersedes_digest,canonical_digest
		FROM paymethod_destination WHERE tenant_id=$1 AND destination_id=$2`+whereRevision+order, args...).Scan(
		&rowID, &storedID, &worker, &rail, &risk, &governed, &provider, &bank, &token, &verification,
		&from, &to, &state, &storedRevision, &supersedes, &supersedesDigest, &digest)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return paymethod.Destination{}, refusal(CodeNotFound, ErrNotFound, "destination revision is absent")
		}
		return paymethod.Destination{}, fmt.Errorf("paymethodstore: load destination %s: %w", id, err)
	}
	_ = rowID
	effective, err := intervalFromProjection(from, to)
	if err != nil {
		return paymethod.Destination{}, refusal(CodeIntegrityViolation, ErrIntegrity, "destination effective interval is invalid")
	}
	out := paymethod.Destination{
		ID: storedID, DestinationID: storedID, WorkerRef: worker.String(), Rail: paymethod.Rail(rail),
		Risk: paymethod.RiskClass(stringValue(risk)), RiskClass: paymethod.RiskClass(stringValue(risk)),
		GovernedRef: stringValue(governed), ProviderRef: stringValue(provider), BankDetailRef: stringValue(bank), TokenRef: stringValue(token),
		Verification: paymethod.VerificationState(verification), VerificationState: paymethod.VerificationState(verification),
		Effective: effective, State: paymethod.DestinationState(state), Status: paymethod.DestinationState(state),
		Revision: uint64(storedRevision), SupersedesRevision: uint64(int64Value(supersedes)), SupersedesDigest: domainDigest(stringValue(supersedesDigest)), CanonicalDigest: domainDigest(digest),
	}
	if out.SupersedesRevision == 0 {
		out.SupersedesDigest = ""
	}
	return out, nil
}

func (s Store) GetDestination(ctx context.Context, ex Executor, tenantID uuid.UUID, id string, revision uint64) (paymethod.Destination, error) {
	return s.LoadDestination(ctx, ex, tenantID, id, revision)
}

func (s Store) SaveDestination(ctx context.Context, ex Executor, tenantID uuid.UUID, in paymethod.Destination) (paymethod.Destination, error) {
	return s.PutDestination(ctx, ex, tenantID, in)
}

func (s Store) ListDestinationRevisions(ctx context.Context, ex Executor, tenantID uuid.UUID, id string) ([]paymethod.Destination, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	rows, err := ex.Query(ctx, `SELECT revision FROM paymethod_destination WHERE tenant_id=$1 AND destination_id=$2 ORDER BY revision`, tenantID, id)
	if err != nil {
		return nil, fmt.Errorf("paymethodstore: list destinations: %w", err)
	}
	defer rows.Close()
	var out []paymethod.Destination
	for rows.Next() {
		var revision int64
		if err := rows.Scan(&revision); err != nil {
			return nil, err
		}
		item, err := s.LoadDestination(ctx, ex, tenantID, id, uint64(revision))
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// PutBankDetailChange appends one immutable bank-detail change revision and
// checks its predecessor under a transaction-local advisory lock.
func (s Store) PutBankDetailChange(ctx context.Context, ex Executor, tenantID uuid.UUID, in paymethod.BankDetailChange) (paymethod.BankDetailChange, error) {
	if err := requireTenant(tenantID); err != nil {
		return paymethod.BankDetailChange{}, err
	}
	if err := in.Validate(); err != nil {
		return paymethod.BankDetailChange{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if in.TenantID != tenantID.String() {
		return paymethod.BankDetailChange{}, invalid("tenant_id", "domain and store tenants differ")
	}
	worker, err := uuid.Parse(in.WorkerRef)
	if err != nil {
		return paymethod.BankDetailChange{}, fmt.Errorf("%w: worker_ref: %v", ErrReferenceInvalid, err)
	}
	if err := checkRevisionHead(ctx, ex, "paymethod_bank_detail_change", "change_id", tenantID, in.ID, in.Revision, in.SupersedesRevision, in.SupersedesDigest); err != nil {
		return paymethod.BankDetailChange{}, err
	}
	affected, err := ex.Exec(ctx, `
		INSERT INTO paymethod_bank_detail_change (
			tenant_id,row_id,change_id,destination_id,worker_ref,before_digest,after_digest,
			requested_by,approver,requested_at,confirmed_at,available_at,cooling_off,status,
			revision,supersedes_revision,canonical_digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::interval,$14,$15,$16,$17)
		ON CONFLICT DO NOTHING`, tenantID, uuid.New(), in.ID, in.DestinationID, worker,
		storageDigest(in.BeforeDigest), storageDigest(in.AfterDigest), in.RequestedBy, in.Approver,
		in.RequestedAt.UTC(), nullableTime(in.ConfirmedAt), nullableTime(in.AvailableAt), intervalArg(in.CoolingOff),
		string(in.Status), int64(in.Revision), nullableRevision(in.SupersedesRevision), storageDigest(in.CanonicalDigest))
	if err != nil {
		return paymethod.BankDetailChange{}, fmt.Errorf("paymethodstore: insert bank-detail change %s/%d: %w", in.ID, in.Revision, err)
	}
	if affected == 0 {
		return paymethod.BankDetailChange{}, refusal(CodeDuplicateRevision, ErrDuplicate, "bank-detail change revision already exists")
	}
	return in, nil
}

func (s Store) LoadBankDetailChange(ctx context.Context, ex Executor, tenantID uuid.UUID, id string, revision uint64) (paymethod.BankDetailChange, error) {
	if err := requireTenant(tenantID); err != nil {
		return paymethod.BankDetailChange{}, err
	}
	where := ""
	args := []any{tenantID, id}
	if revision != 0 {
		where = " AND revision=$3"
		args = append(args, int64(revision))
	}
	var (
		rowID, worker                         uuid.UUID
		changeID, destination, status, digest string
		before, after, requestedBy, approver  *string
		requested, confirmed, available       *time.Time
		coolingText                           string
		storedRevision                        int64
		supersedes                            *int64
		supersedesDigest                      *string
	)
	err := ex.QueryRow(ctx, `SELECT row_id,change_id,destination_id,worker_ref,before_digest,after_digest,
		requested_by,approver,requested_at,confirmed_at,available_at,cooling_off::text,status,revision,
		supersedes_revision,canonical_digest FROM paymethod_bank_detail_change
		WHERE tenant_id=$1 AND change_id=$2`+where+` ORDER BY revision DESC LIMIT 1`, args...).Scan(
		&rowID, &changeID, &destination, &worker, &before, &after, &requestedBy, &approver, &requested,
		&confirmed, &available, &coolingText, &status, &storedRevision, &supersedes, &digest)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return paymethod.BankDetailChange{}, refusal(CodeNotFound, ErrNotFound, "bank-detail change is absent")
		}
		return paymethod.BankDetailChange{}, fmt.Errorf("paymethodstore: load bank-detail change %s: %w", id, err)
	}
	_ = rowID
	cooling, err := parsePGInterval(coolingText)
	if err != nil {
		return paymethod.BankDetailChange{}, refusal(CodeIntegrityViolation, ErrIntegrity, "cooling_off is invalid")
	}
	// Migration 00106 deliberately stores the table contract's change-control
	// projection. Contact proof and operation metadata are supplied by the
	// domain workflow; they are not fabricated on reload.
	out := paymethod.BankDetailChange{
		ID: changeID, TenantID: tenantID.String(), DestinationID: destination, WorkerRef: worker.String(),
		BeforeDigest: domainDigest(stringValue(before)), AfterDigest: domainDigest(stringValue(after)), RequestedBy: stringValue(requestedBy), Approver: stringValue(approver),
		RequestedAt: timeValue(requested), ConfirmedAt: timeValue(confirmed), AvailableAt: timeValue(available),
		CoolingOff: cooling, Status: paymethod.ChangeStatus(status), Revision: uint64(storedRevision),
		SupersedesRevision: uint64(int64Value(supersedes)), SupersedesDigest: domainDigest(stringValue(supersedesDigest)), CanonicalDigest: domainDigest(digest),
	}
	return out, nil
}

func (s Store) SaveBankDetailChange(ctx context.Context, ex Executor, tenantID uuid.UUID, in paymethod.BankDetailChange) (paymethod.BankDetailChange, error) {
	return s.PutBankDetailChange(ctx, ex, tenantID, in)
}

// AppendVerificationEvent records one immutable verification event.
func (s Store) AppendVerificationEvent(ctx context.Context, ex Executor, tenantID uuid.UUID, in paymethod.VerificationEvent, eventSequence uint64) error {
	if err := requireTenant(tenantID); err != nil {
		return err
	}
	if err := in.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if eventSequence == 0 {
		return invalid("event_sequence", "positive event sequence is required")
	}
	affected, err := ex.Exec(ctx, `INSERT INTO paymethod_verification_event
		(tenant_id,row_id,challenge_id,destination_id,method,attempt,verified_at,evidence_digest,canonical_digest,event_sequence)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT DO NOTHING`, tenantID, uuid.New(),
		in.ChallengeID, in.DestinationID, string(in.Method), in.Attempt, in.VerifiedAt.Time(), storageDigest(in.EvidenceDigest),
		storageDigest(in.CanonicalDigest), int64(eventSequence))
	if err != nil {
		return fmt.Errorf("paymethodstore: append verification event %s: %w", in.ChallengeID, err)
	}
	if affected == 0 {
		return refusal(CodeDuplicateEvent, ErrDuplicate, "verification event sequence already exists")
	}
	return nil
}

func (s Store) PutVerificationEvent(ctx context.Context, ex Executor, tenantID uuid.UUID, in paymethod.VerificationEvent, eventSequence uint64) error {
	return s.AppendVerificationEvent(ctx, ex, tenantID, in, eventSequence)
}

// AppendChangeConfirmation records one immutable dispatch event.
func (s Store) AppendChangeConfirmation(ctx context.Context, ex Executor, tenantID uuid.UUID, in paymethod.ChangeConfirmation, eventSequence uint64) error {
	if err := requireTenant(tenantID); err != nil {
		return err
	}
	if strings.TrimSpace(in.ChangeDigest) == "" || strings.TrimSpace(in.DestinationID) == "" || in.DispatchedAt.IsZero() {
		return invalid("change_confirmation", "digest, destination and dispatch time are required")
	}
	if eventSequence == 0 {
		return invalid("event_sequence", "positive event sequence is required")
	}
	affected, err := ex.Exec(ctx, `INSERT INTO paymethod_change_confirmation
		(tenant_id,row_id,change_digest,destination_id,dispatched_at,event_sequence)
		VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, tenantID, uuid.New(), storageDigest(in.ChangeDigest),
		in.DestinationID, in.DispatchedAt.UTC(), int64(eventSequence))
	if err != nil {
		return fmt.Errorf("paymethodstore: append change confirmation: %w", err)
	}
	if affected == 0 {
		return refusal(CodeDuplicateEvent, ErrDuplicate, "change confirmation sequence already exists")
	}
	return nil
}

func (s Store) PutChangeConfirmation(ctx context.Context, ex Executor, tenantID uuid.UUID, in paymethod.ChangeConfirmation, eventSequence uint64) error {
	return s.AppendChangeConfirmation(ctx, ex, tenantID, in, eventSequence)
}

// PutProtectedAccount inserts a ciphertext/token reference and digest only.
func (s Store) PutProtectedAccount(ctx context.Context, ex Executor, tenantID uuid.UUID, accountID string, in paymethod.ProtectedAccount) (paymethod.ProtectedAccount, error) {
	if err := requireTenant(tenantID); err != nil {
		return paymethod.ProtectedAccount{}, err
	}
	if strings.TrimSpace(accountID) == "" {
		return paymethod.ProtectedAccount{}, invalid("account_id", "account id is required")
	}
	if err := in.Validate(); err != nil {
		return paymethod.ProtectedAccount{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	affected, err := ex.Exec(ctx, `INSERT INTO paymethod_protected_account
		(tenant_id,row_id,account_id,mode,opaque_reference,value_digest)
		VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, tenantID, uuid.New(), accountID,
		string(in.Mode), in.OpaqueReference, storageDigest(in.ValueDigest))
	if err != nil {
		return paymethod.ProtectedAccount{}, fmt.Errorf("paymethodstore: insert protected account %s: %w", accountID, err)
	}
	if affected == 0 {
		return paymethod.ProtectedAccount{}, refusal(CodeDuplicateRevision, ErrDuplicate, "protected account row already exists")
	}
	return in, nil
}

func (s Store) LoadProtectedAccount(ctx context.Context, ex Executor, tenantID uuid.UUID, accountID string) (paymethod.ProtectedAccount, error) {
	if err := requireTenant(tenantID); err != nil {
		return paymethod.ProtectedAccount{}, err
	}
	var rowID uuid.UUID
	var mode, opaque, digest string
	err := ex.QueryRow(ctx, `SELECT row_id,mode,opaque_reference,value_digest FROM paymethod_protected_account WHERE tenant_id=$1 AND account_id=$2 ORDER BY row_id LIMIT 1`, tenantID, accountID).Scan(&rowID, &mode, &opaque, &digest)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return paymethod.ProtectedAccount{}, refusal(CodeNotFound, ErrNotFound, "protected account is absent")
		}
		return paymethod.ProtectedAccount{}, fmt.Errorf("paymethodstore: load protected account %s: %w", accountID, err)
	}
	_ = rowID
	out, err := paymethod.NewProtectedAccount(paymethod.AccountStorageMode(mode), opaque, domainDigest(digest))
	if err != nil {
		return paymethod.ProtectedAccount{}, refusal(CodeIntegrityViolation, ErrIntegrity, "protected account does not satisfy domain validation")
	}
	return out, nil
}

func (s Store) SaveProtectedAccount(ctx context.Context, ex Executor, tenantID uuid.UUID, accountID string, in paymethod.ProtectedAccount) (paymethod.ProtectedAccount, error) {
	return s.PutProtectedAccount(ctx, ex, tenantID, accountID, in)
}

// PutAccountValidation appends one validation revision. Evidence is retained
// in the domain input for the caller; the migration contract stores only the
// revision and superseding digest fields it declares.
func (s Store) PutAccountValidation(ctx context.Context, ex Executor, tenantID uuid.UUID, accountID string, in paymethod.AccountValidationRecord) (paymethod.AccountValidationRecord, error) {
	if err := requireTenant(tenantID); err != nil {
		return paymethod.AccountValidationRecord{}, err
	}
	if strings.TrimSpace(accountID) == "" {
		return paymethod.AccountValidationRecord{}, invalid("account_id", "account id is required")
	}
	if err := in.Validate(); err != nil {
		return paymethod.AccountValidationRecord{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if in.Revision > 1 {
		if err := checkRevisionNumberHead(ctx, ex, "paymethod_account_validation", "account_id", tenantID, accountID, in.Revision, in.Revision-1); err != nil {
			return paymethod.AccountValidationRecord{}, err
		}
	}
	affected, err := ex.Exec(ctx, `INSERT INTO paymethod_account_validation
		(tenant_id,row_id,account_id,method,validated_at,result,revision,supersedes_digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`, tenantID, uuid.New(), accountID, in.Method,
		in.ValidatedAt.UTC(), string(in.Result), int64(in.Revision), nullableDigest(in.SupersedesDigest))
	if err != nil {
		return paymethod.AccountValidationRecord{}, fmt.Errorf("paymethodstore: insert validation %s/%d: %w", accountID, in.Revision, err)
	}
	if affected == 0 {
		return paymethod.AccountValidationRecord{}, refusal(CodeDuplicateRevision, ErrDuplicate, "account validation revision already exists")
	}
	return in, nil
}

func (s Store) LoadAccountValidation(ctx context.Context, ex Executor, tenantID uuid.UUID, accountID string, revision uint64) (paymethod.AccountValidationRecord, error) {
	if err := requireTenant(tenantID); err != nil {
		return paymethod.AccountValidationRecord{}, err
	}
	var rowID uuid.UUID
	var method, result string
	var validated time.Time
	var storedRevision int64
	var supersedes *string
	query := `SELECT row_id,method,validated_at,result,revision,supersedes_digest FROM paymethod_account_validation WHERE tenant_id=$1 AND account_id=$2`
	args := []any{tenantID, accountID}
	if revision != 0 {
		query += " AND revision=$3"
		args = append(args, int64(revision))
	}
	query += " ORDER BY revision DESC LIMIT 1"
	if err := ex.QueryRow(ctx, query, args...).Scan(&rowID, &method, &validated, &result, &storedRevision, &supersedes); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return paymethod.AccountValidationRecord{}, refusal(CodeNotFound, ErrNotFound, "account validation is absent")
		}
		return paymethod.AccountValidationRecord{}, fmt.Errorf("paymethodstore: load validation %s: %w", accountID, err)
	}
	_ = rowID
	return paymethod.AccountValidationRecord{Method: method, ValidatedAt: validated.UTC(), Result: paymethod.ValidationResult(result), Revision: uint64(storedRevision), SupersedesDigest: domainDigest(stringValue(supersedes))}, nil
}

func (s Store) SaveAccountValidation(ctx context.Context, ex Executor, tenantID uuid.UUID, accountID string, in paymethod.AccountValidationRecord) (paymethod.AccountValidationRecord, error) {
	return s.PutAccountValidation(ctx, ex, tenantID, accountID, in)
}

// PutPaymentInstruction appends one immutable settlement instruction revision.
func (s Store) PutPaymentInstruction(ctx context.Context, ex Executor, tenantID uuid.UUID, in settlement.PaymentInstruction) (settlement.PaymentInstruction, error) {
	if err := requireTenant(tenantID); err != nil {
		return settlement.PaymentInstruction{}, err
	}
	if err := in.Validate(); err != nil {
		return settlement.PaymentInstruction{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	payee, err := uuid.Parse(in.PayeeRef)
	if err != nil {
		return settlement.PaymentInstruction{}, fmt.Errorf("%w: payee_ref: %v", ErrReferenceInvalid, err)
	}
	if err := checkRevisionHead(ctx, ex, "settlement_payment_instruction", "instruction_id", tenantID, in.InstructionID, in.Revision, in.SupersedesRevision, ""); err != nil {
		return settlement.PaymentInstruction{}, err
	}
	var valueDate any
	if in.ValueDate.IsSet() {
		valueDate = in.ValueDate.String()
	}
	affected, err := ex.Exec(ctx, `INSERT INTO settlement_payment_instruction
		(tenant_id,row_id,instruction_id,payroll_run_ref,payee_ref,amount,currency,funding_source_ref,rail,
		bank_detail_ref,schedule_ref,value_date,state,revision,supersedes_revision,evidence_ref,canonical_digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17) ON CONFLICT DO NOTHING`,
		tenantID, uuid.New(), in.InstructionID, nullableText(in.PayrollRunRef), payee, in.Amount.String(), in.Currency,
		nullableText(in.FundingSourceRef), string(in.Rail), nullableText(in.BankDetailRef), nullableText(in.ScheduleRef), valueDate,
		string(in.State), int64(in.Revision), nullableRevision(in.SupersedesRevision), nullableText(in.EvidenceRef), storageDigest(in.CanonicalDigest))
	if err != nil {
		return settlement.PaymentInstruction{}, fmt.Errorf("paymethodstore: insert payment instruction %s/%d: %w", in.InstructionID, in.Revision, err)
	}
	if affected == 0 {
		return settlement.PaymentInstruction{}, refusal(CodeDuplicateRevision, ErrDuplicate, "payment instruction revision already exists")
	}
	return in, nil
}

func (s Store) LoadPaymentInstruction(ctx context.Context, ex Executor, tenantID uuid.UUID, id string, revision uint64) (settlement.PaymentInstruction, error) {
	if err := requireTenant(tenantID); err != nil {
		return settlement.PaymentInstruction{}, err
	}
	query := `SELECT row_id,instruction_id,payroll_run_ref,payee_ref,amount::text,currency,funding_source_ref,rail,
		bank_detail_ref,schedule_ref,value_date,state,revision,supersedes_revision,evidence_ref,canonical_digest
		FROM settlement_payment_instruction WHERE tenant_id=$1 AND instruction_id=$2`
	args := []any{tenantID, id}
	if revision != 0 {
		query += " AND revision=$3"
		args = append(args, int64(revision))
	}
	query += " ORDER BY revision DESC LIMIT 1"
	var (
		rowID, payee                                             uuid.UUID
		storedID, amountText, currency, rail, state, digest      string
		payrollRun, funding, bank, schedule, evidence, valueDate *string
		valueDateTime                                            *time.Time
		storedRevision                                           int64
		supersedes                                               *int64
	)
	err := ex.QueryRow(ctx, query, args...).Scan(&rowID, &storedID, &payrollRun, &payee, &amountText, &currency, &funding, &rail, &bank, &schedule, &valueDateTime, &state, &storedRevision, &supersedes, &evidence, &digest)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return settlement.PaymentInstruction{}, refusal(CodeNotFound, ErrNotFound, "payment instruction is absent")
		}
		return settlement.PaymentInstruction{}, fmt.Errorf("paymethodstore: load payment instruction %s: %w", id, err)
	}
	_ = rowID
	_ = valueDate
	amount, err := values.NewDecimal(amountText, 4, values.RoundingHalfEven)
	if err != nil {
		return settlement.PaymentInstruction{}, refusal(CodeIntegrityViolation, ErrIntegrity, "payment amount is invalid")
	}
	var localDate values.LocalDate
	if valueDateTime != nil {
		localDate, err = values.ParseLocalDate(valueDateTime.UTC().Format("2006-01-02"))
		if err != nil {
			return settlement.PaymentInstruction{}, refusal(CodeIntegrityViolation, ErrIntegrity, "payment value date is invalid")
		}
	}
	out := settlement.PaymentInstruction{InstructionID: storedID, PayrollRunRef: stringValue(payrollRun), PayeeRef: payee.String(), Amount: amount,
		Currency: currency, FundingSourceRef: stringValue(funding), Rail: settlement.SettlementRail(rail), BankDetailRef: stringValue(bank),
		ScheduleRef: stringValue(schedule), ValueDate: localDate, State: settlement.SettlementState(state), Revision: uint64(storedRevision),
		SupersedesRevision: uint64(int64Value(supersedes)), EvidenceRef: stringValue(evidence), CanonicalDigest: domainDigest(digest)}
	return out, nil
}

func (s Store) GetPaymentInstruction(ctx context.Context, ex Executor, tenantID uuid.UUID, id string, revision uint64) (settlement.PaymentInstruction, error) {
	return s.LoadPaymentInstruction(ctx, ex, tenantID, id, revision)
}

func (s Store) SavePaymentInstruction(ctx context.Context, ex Executor, tenantID uuid.UUID, in settlement.PaymentInstruction) (settlement.PaymentInstruction, error) {
	return s.PutPaymentInstruction(ctx, ex, tenantID, in)
}

func requireTenant(tenantID uuid.UUID) error {
	if tenantID == uuid.Nil {
		return invalid("tenant_id", "tenant is required")
	}
	return nil
}

func destinationID(in paymethod.Destination) string {
	if in.DestinationID != "" {
		return in.DestinationID
	}
	return in.ID
}

func checkRevisionHead(ctx context.Context, ex Executor, table, idColumn string, tenantID uuid.UUID, id string, revision, parentRevision uint64, parentDigest string) error {
	lockKey := tenantID.String() + ":" + table + ":" + id
	if _, err := ex.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, lockKey); err != nil {
		return fmt.Errorf("paymethodstore: lock revision head: %w", err)
	}
	if revision == 0 {
		return invalid("revision", "positive revision is required")
	}
	var latestRevision int64
	var latestDigest *string
	err := ex.QueryRow(ctx, `SELECT revision,canonical_digest FROM `+table+` WHERE tenant_id=$1 AND `+idColumn+`=$2 ORDER BY revision DESC LIMIT 1`, tenantID, id).Scan(&latestRevision, &latestDigest)
	if errors.Is(err, dbport.ErrNoRows) {
		if revision != 1 {
			return refusal(CodeVersionConflict, ErrVersionConflict, "successor has no current parent")
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("paymethodstore: read %s head: %w", table, err)
	}
	if uint64(latestRevision) == revision {
		return refusal(CodeDuplicateRevision, ErrDuplicate, "revision identity already exists")
	}
	if revision == 1 || uint64(latestRevision) != parentRevision {
		return refusal(CodeVersionConflict, ErrVersionConflict, "successor parent is stale")
	}
	if parentDigest != "" && storageDigest(parentDigest) != stringValue(latestDigest) {
		return refusal(CodeVersionConflict, ErrVersionConflict, "successor digest is stale")
	}
	return nil
}

func checkRevisionNumberHead(ctx context.Context, ex Executor, table, idColumn string, tenantID uuid.UUID, id string, revision, parentRevision uint64) error {
	lockKey := tenantID.String() + ":" + table + ":" + id
	if _, err := ex.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, lockKey); err != nil {
		return fmt.Errorf("paymethodstore: lock revision head: %w", err)
	}
	var latest int64
	err := ex.QueryRow(ctx, `SELECT revision FROM `+table+` WHERE tenant_id=$1 AND `+idColumn+`=$2 ORDER BY revision DESC LIMIT 1`, tenantID, id).Scan(&latest)
	if errors.Is(err, dbport.ErrNoRows) {
		return refusal(CodeVersionConflict, ErrVersionConflict, "successor has no current parent")
	}
	if err != nil {
		return fmt.Errorf("paymethodstore: read %s head: %w", table, err)
	}
	if uint64(latest) == revision {
		return refusal(CodeDuplicateRevision, ErrDuplicate, "revision identity already exists")
	}
	if uint64(latest) != parentRevision {
		return refusal(CodeVersionConflict, ErrVersionConflict, "successor parent is stale")
	}
	return nil
}

func int64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func nullableRevision(value uint64) any {
	if value == 0 {
		return nil
	}
	return int64(value)
}

func nullableDigest(value string) any {
	if value == "" {
		return nil
	}
	return storageDigest(value)
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func storageDigest(value string) string { return strings.TrimPrefix(value, "sha256:") }

func domainDigest(value string) string {
	if value == "" || strings.HasPrefix(value, "sha256:") {
		return value
	}
	return "sha256:" + value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func timeValue(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.UTC()
}

func effectiveProjection(interval values.EffectiveInterval) (time.Time, any) {
	if instant, ok := interval.StartInstant(); ok {
		if end, hasEnd := interval.EndInstant(); hasEnd {
			return instant.Time(), end.Time()
		}
		return instant.Time(), nil
	}
	if date, ok := interval.StartDate(); ok {
		from := time.Date(int(date.Year()), date.Month(), int(date.Day()), 0, 0, 0, 0, time.UTC)
		if end, hasEnd := interval.EndDate(); hasEnd {
			return from, time.Date(int(end.Year()), end.Month(), int(end.Day()), 0, 0, 0, 0, time.UTC)
		}
		return from, nil
	}
	return time.Time{}, nil
}

func intervalFromProjection(from, to *time.Time) (values.EffectiveInterval, error) {
	if from == nil {
		return values.EffectiveInterval{}, nil
	}
	start := values.NewInstant(from.UTC())
	if to == nil {
		return values.NewOpenInstantInterval(start)
	}
	return values.NewInstantInterval(start, values.NewInstant(to.UTC()))
}

func intervalArg(value time.Duration) string {
	return strconv.FormatInt(value.Microseconds(), 10) + " microseconds"
}

func parsePGInterval(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	if strings.Contains(value, " day") {
		parts := strings.Fields(value)
		if len(parts) == 2 {
			days, err := strconv.ParseInt(parts[0], 10, 64)
			if err == nil {
				return time.Duration(days) * 24 * time.Hour, nil
			}
		}
	}
	parts := strings.Split(value, ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("unsupported PostgreSQL interval %q", value)
	}
	hours, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, err
	}
	minutes, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, err
	}
	seconds, err := strconv.ParseFloat(parts[2], 64)
	if err != nil {
		return 0, err
	}
	return time.Duration(hours)*time.Hour + time.Duration(minutes)*time.Minute + time.Duration(seconds*float64(time.Second)), nil
}

var _ paymethod.DestinationCatalog = Store{}
