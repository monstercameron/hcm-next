// Package assetstore persists the asset domain's immutable inventory
// revisions and append-only custody events under tenant RLS.
package assetstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/asset"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

type DB interface{ dbport.Beginner }
type Executor interface {
	dbport.Execer
	dbport.Querier
}

var (
	ErrInvalid   = errors.New("assetstore: invalid row")
	ErrNotFound  = errors.New("assetstore: row not found")
	ErrDuplicate = errors.New("assetstore: duplicate revision")
	ErrStaleCAS  = errors.New("assetstore: stale compare-and-set")
	ErrStorage   = errors.New("assetstore: storage failure")
)

const (
	CodeInvalid   = "ASSET_INVALID"
	CodeNotFound  = "ASSET_NOT_FOUND"
	CodeDuplicate = "ASSET_DUPLICATE_REVISION"
	CodeStaleCAS  = "ASSET_STALE_CAS"
	CodeStorage   = "ASSET_STORAGE"
)

// Error is a stable, machine-readable refusal from the persistence boundary.
// Its cause remains available through errors.Is for domain and adapter tests.
type Error struct {
	code  string
	cause error
}

func (e *Error) Error() string {
	if e == nil {
		return "assetstore: <nil error>"
	}
	return e.code + ": " + e.cause.Error()
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// Code returns the stable storage disposition code.
func (e *Error) Code() string {
	if e == nil {
		return ""
	}
	return e.code
}

// CodeOf extracts the stable storage disposition code carried by err.
func CodeOf(err error) string {
	if err == nil {
		return ""
	}
	var coded interface{ Code() string }
	if errors.As(err, &coded) {
		return coded.Code()
	}
	return ""
}

func coded(code string, cause error) error {
	if cause == nil {
		cause = ErrStorage
	}
	return &Error{code: code, cause: cause}
}

// Store implements asset.Repository over the migration 00071 tables.
type Store struct{ db DB }

var _ asset.Repository = (*Store)(nil)

func New(db DB) *Store { return &Store{db: db} }

func NewStore(db DB) *Store { return New(db) }

func (s *Store) RegisterInventory(inventory asset.InventoryRevision) error {
	if err := inventory.Validate(); err != nil {
		return coded(CodeInvalid, fmt.Errorf("%w: inventory: %v", ErrInvalid, err))
	}
	tenant, err := resolveTenantName(inventory.InventoryID.Tenant)
	if err != nil {
		return err
	}
	owner, err := parseUUID(inventory.Owner.Id, "owner_ref")
	if err != nil {
		return err
	}
	revision, err := sequence(inventory.Revision)
	if err != nil {
		return err
	}
	digest, err := inventory.Digest()
	if err != nil {
		return coded(CodeInvalid, fmt.Errorf("%w: inventory digest: %v", ErrInvalid, err))
	}
	return s.withTenant(tenant, func(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID) error {
		current, err := latestRevision(ctx, tx, "asset_inventory", "inventory_id", inventory.InventoryID.Id, tenantID)
		if err != nil && !errors.Is(err, dbport.ErrNoRows) {
			return coded(CodeStorage, err)
		}
		if err == nil {
			if revision == current {
				return coded(CodeDuplicate, ErrDuplicate)
			}
			if revision < current {
				return coded(CodeStaleCAS, fmt.Errorf("%w: inventory revision %d after %d", ErrStaleCAS, revision, current))
			}
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO asset_inventory
			(row_id, tenant_id, inventory_id, owner_ref, classification, serial_number, revision, effective_at, status, digest)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			uuid.New(), tenantID, inventory.InventoryID.Id, owner, inventory.Classification,
			inventory.SerialNumber, revision, inventory.EffectiveAt.UTC(), string(inventory.Status), storageDigest(digest))
		if err != nil {
			return classifyWrite(err, ErrDuplicate)
		}
		return nil
	})
}

func (s *Store) AppendCustody(assetRef values.EntityRef, expected values.RevisionToken, next asset.CustodyRevision) error {
	if err := next.Validate(); err != nil {
		return coded(CodeInvalid, fmt.Errorf("%w: custody: %v", ErrInvalid, err))
	}
	if next.Asset != assetRef {
		return coded(CodeInvalid, asset.ErrInvalidAsset)
	}
	tenant, err := resolveTenantName(assetRef.Tenant)
	if err != nil {
		return err
	}
	assetID, err := parseUUID(assetRef.Id, "asset_ref")
	if err != nil {
		return err
	}
	revision, err := sequence(next.Revision)
	if err != nil {
		return err
	}
	if err := expected.Validate(); err != nil {
		return coded(CodeInvalid, fmt.Errorf("%w: expected revision: %v", ErrInvalid, err))
	}
	digest, err := next.Digest()
	if err != nil {
		return coded(CodeInvalid, fmt.Errorf("%w: custody digest: %v", ErrInvalid, err))
	}
	assignee, err := receiptJSON(next.AssigneeReceipt)
	if err != nil {
		return err
	}
	issuer, err := receiptJSON(next.IssuerReceipt)
	if err != nil {
		return err
	}
	worker, err := optionalUUID(next.Worker.Id, "worker_ref")
	if err != nil {
		return err
	}
	return s.withTenant(tenant, func(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID) error {
		var inventoryRevision int64
		var inventoryStatus string
		if err := tx.QueryRow(ctx, `SELECT revision, status FROM asset_inventory WHERE tenant_id=$1 AND inventory_id=$2 ORDER BY revision DESC LIMIT 1`, tenantID, assetRef.Id).Scan(&inventoryRevision, &inventoryStatus); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return coded(CodeNotFound, asset.ErrUnknownInventory)
			}
			return coded(CodeStorage, err)
		}

		var currentRevision *int64
		var currentStatus string
		err := tx.QueryRow(ctx, `SELECT revision, status FROM asset_custody_event WHERE tenant_id=$1 AND asset_ref=$2 ORDER BY event_sequence DESC LIMIT 1`, tenantID, assetID).Scan(&currentRevision, &currentStatus)
		switch {
		case errors.Is(err, dbport.ErrNoRows):
			if specified, ok := expectedSequence(expected); ok && specified != inventoryRevision {
				return stale(expected, inventoryRevision)
			}
		case err != nil:
			return coded(CodeStorage, err)
		default:
			if specified, ok := expectedSequence(expected); !ok || specified != *currentRevision {
				return stale(expected, *currentRevision)
			}
			if revision == *currentRevision {
				return coded(CodeDuplicate, ErrDuplicate)
			}
			if revision < *currentRevision {
				return coded(CodeStaleCAS, fmt.Errorf("%w: custody revision %d after %d", ErrStaleCAS, revision, *currentRevision))
			}
			if err := validateTransition(inventoryStatus, currentStatus, next.Status); err != nil {
				return coded(CodeInvalid, err)
			}
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO asset_custody_event
			(row_id, tenant_id, custody_id, asset_ref, worker_ref, location, condition, assignee_receipt, issuer_receipt, revision, effective_at, status, event_sequence, digest)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
			uuid.New(), tenantID, uuid.NewString(), assetID, worker, next.Location, next.Condition,
			assignee, issuer, revision, next.EffectiveAt.UTC(), string(next.Status), revision, storageDigest(digest))
		if err != nil {
			return classifyWrite(err, ErrDuplicate)
		}
		return nil
	})
}

func (s *Store) CurrentCustody(assetRef values.EntityRef) (asset.CustodyRevision, bool, error) {
	var current asset.CustodyRevision
	found := false
	err := s.load(assetRef, func(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID) error {
		assetID, err := parseUUID(assetRef.Id, "asset_ref")
		if err != nil {
			return err
		}
		row := tx.QueryRow(ctx, custodySelect+` WHERE tenant_id=$1 AND asset_ref=$2 ORDER BY event_sequence DESC LIMIT 1`, tenantID, assetID)
		current, err = scanCustody(row, assetRef.Tenant)
		if errors.Is(err, dbport.ErrNoRows) {
			return nil
		}
		if err == nil {
			found = true
			return nil
		}
		return coded(CodeStorage, err)
	})
	return current, found, err
}

func (s *Store) HistoryCustody(assetRef values.EntityRef) ([]asset.CustodyRevision, error) {
	var history []asset.CustodyRevision
	err := s.load(assetRef, func(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID) error {
		assetID, err := parseUUID(assetRef.Id, "asset_ref")
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, custodySelect+` WHERE tenant_id=$1 AND asset_ref=$2 ORDER BY event_sequence`, tenantID, assetID)
		if err != nil {
			return coded(CodeStorage, err)
		}
		defer rows.Close()
		for rows.Next() {
			event, scanErr := scanCustody(rows, assetRef.Tenant)
			if scanErr != nil {
				return coded(CodeStorage, scanErr)
			}
			history = append(history, event)
		}
		if err := rows.Err(); err != nil {
			return coded(CodeStorage, err)
		}
		return nil
	})
	return history, err
}

const custodySelect = `SELECT asset_ref, worker_ref, location, condition, assignee_receipt, issuer_receipt, revision, effective_at, status FROM asset_custody_event`

func scanCustody(row dbport.Row, tenant values.TenantId) (asset.CustodyRevision, error) {
	var (
		assetID                uuid.UUID
		workerID               *uuid.UUID
		location, condition    *string
		assigneeRaw, issuerRaw []byte
		revision               int64
		effectiveAt            time.Time
		status                 string
	)
	if err := row.Scan(&assetID, &workerID, &location, &condition, &assigneeRaw, &issuerRaw, &revision, &effectiveAt, &status); err != nil {
		return asset.CustodyRevision{}, err
	}
	if revision < 1 {
		return asset.CustodyRevision{}, fmt.Errorf("%w: custody revision %d", ErrInvalid, revision)
	}
	event := asset.CustodyRevision{
		Asset:       values.EntityRef{Tenant: tenant, Kind: "asset", Id: assetID.String()},
		Revision:    mustSequenceRevision(custodyStream, revision),
		EffectiveAt: effectiveAt.UTC(), Status: asset.Status(status),
	}
	if location != nil {
		event.Location = *location
	}
	if condition != nil {
		event.Condition = *condition
	}
	if workerID != nil {
		event.Worker = values.EntityRef{Tenant: tenant, Kind: "worker", Id: workerID.String()}
	}
	if err := decodeReceipt(assigneeRaw, &event.AssigneeReceipt); err != nil {
		return asset.CustodyRevision{}, err
	}
	if err := decodeReceipt(issuerRaw, &event.IssuerReceipt); err != nil {
		return asset.CustodyRevision{}, err
	}
	if err := event.Validate(); err != nil {
		return asset.CustodyRevision{}, fmt.Errorf("%w: decoded custody: %v", ErrInvalid, err)
	}
	return event, nil
}

const (
	inventoryStream = "asset.inventory"
	custodyStream   = "asset.custody"
)

func mustSequenceRevision(stream string, revision int64) values.RevisionToken {
	token, _ := values.NewSequenceRevision(stream, uint64(revision))
	return token
}

func (s *Store) load(assetRef values.EntityRef, fn func(context.Context, dbport.Tx, uuid.UUID) error) error {
	if err := assetRef.Validate(); err != nil || assetRef.Kind != "asset" {
		return coded(CodeInvalid, fmt.Errorf("%w: asset reference: %v", ErrInvalid, err))
	}
	tenant, err := resolveTenantName(assetRef.Tenant)
	if err != nil {
		return err
	}
	return s.withTenant(tenant, fn)
}

func (s *Store) withTenant(tenant values.TenantId, fn func(context.Context, dbport.Tx, uuid.UUID) error) error {
	if s == nil || s.db == nil {
		return coded(CodeStorage, ErrStorage)
	}
	if err := tenant.Validate(); err != nil {
		return coded(CodeInvalid, fmt.Errorf("%w: tenant: %v", ErrInvalid, err))
	}
	ctx := context.Background()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return coded(CodeStorage, err)
	}
	tenantID, err := resolveTenant(ctx, tx, tenant)
	if err == nil {
		err = tenancy.WithTenant(ctx, tx, tenantID)
	}
	if err == nil {
		err = fn(ctx, tx, tenantID)
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return coded(CodeStorage, err)
	}
	return nil
}

func resolveTenantName(tenant values.TenantId) (values.TenantId, error) {
	if err := tenant.Validate(); err != nil {
		return "", coded(CodeInvalid, fmt.Errorf("%w: tenant: %v", ErrInvalid, err))
	}
	return tenant, nil
}

func resolveTenant(ctx context.Context, ex Executor, tenant values.TenantId) (uuid.UUID, error) {
	if id, err := uuid.Parse(tenant.String()); err == nil {
		return id, nil
	}
	var id uuid.UUID
	if err := ex.QueryRow(ctx, `SELECT tenant_id FROM tenant WHERE tenant_key=$1`, tenant.String()).Scan(&id); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return uuid.Nil, coded(CodeNotFound, fmt.Errorf("%w: tenant %q", ErrNotFound, tenant))
		}
		return uuid.Nil, coded(CodeStorage, err)
	}
	return id, nil
}

func latestRevision(ctx context.Context, ex Executor, table, keyColumn, key string, tenantID uuid.UUID) (int64, error) {
	query := fmt.Sprintf("SELECT revision FROM %s WHERE tenant_id=$1 AND %s=$2 ORDER BY revision DESC LIMIT 1", table, keyColumn)
	var revision int64
	if err := ex.QueryRow(ctx, query, tenantID, key).Scan(&revision); err != nil {
		return 0, err
	}
	return revision, nil
}

func sequence(token values.RevisionToken) (int64, error) {
	n, ok := token.Sequence()
	if !ok || n == 0 || n > uint64(^uint64(0)>>1) {
		return 0, coded(CodeInvalid, fmt.Errorf("%w: revision must be a positive sequence token", ErrInvalid))
	}
	return int64(n), nil
}

func expectedSequence(token values.RevisionToken) (int64, bool) {
	n, ok := token.Sequence()
	if !ok || n > uint64(^uint64(0)>>1) {
		return 0, false
	}
	return int64(n), true
}

func stale(expected values.RevisionToken, actual int64) error {
	return coded(CodeStaleCAS, fmt.Errorf("%w: expected %q, actual %d", ErrStaleCAS, expected.String(), actual))
}

func validateTransition(inventoryStatus, currentStatus string, next asset.Status) error {
	if currentStatus == string(asset.Assigned) && next == asset.Assigned {
		return asset.ErrAlreadyAssigned
	}
	if currentStatus != string(asset.Assigned) && next == asset.ReturnPending {
		return asset.ErrNotAssigned
	}
	if currentStatus != string(asset.ReturnPending) && next == asset.Returned {
		return asset.ErrInvalidTransition
	}
	if next == asset.Assigned && inventoryStatus == string(asset.Retired) {
		return asset.ErrInvalidTransition
	}
	return nil
}

func parseUUID(value, field string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, coded(CodeInvalid, fmt.Errorf("%w: %s must be a uuid: %v", ErrInvalid, field, err))
	}
	return id, nil
}

func optionalUUID(value, field string) (*uuid.UUID, error) {
	if value == "" {
		return nil, nil
	}
	id, err := parseUUID(value, field)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func receiptJSON(receipt asset.Receipt) ([]byte, error) {
	if receipt.ID == (values.EntityRef{}) && receipt.Issuer == (values.EntityRef{}) {
		return nil, nil
	}
	if err := receipt.Validate(); err != nil {
		return nil, coded(CodeInvalid, fmt.Errorf("%w: receipt: %v", ErrInvalid, err))
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return nil, coded(CodeInvalid, fmt.Errorf("%w: receipt encoding: %v", ErrInvalid, err))
	}
	return raw, nil
}

func decodeReceipt(raw []byte, receipt *asset.Receipt) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, receipt); err != nil {
		return fmt.Errorf("%w: receipt decoding: %v", ErrInvalid, err)
	}
	return nil
}

func classifyWrite(err, duplicateCause error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return coded(CodeDuplicate, duplicateCause)
		case "23514", "23503", "42501":
			return coded(CodeInvalid, err)
		}
	}
	return coded(CodeStorage, err)
}

func storageDigest(value string) string {
	return strings.TrimPrefix(value, "sha256:")
}
