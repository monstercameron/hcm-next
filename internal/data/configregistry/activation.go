package configregistry

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	platformconfig "github.com/monstercameron/human-capital-management-suite/internal/platform/configregistry"
)

// PutActivation implements [platformconfig.Store]. It appends rec as a new
// row, never editing or removing an earlier activation for the same
// (scope, kind, id) — migration 00027's config_object_activation table is
// append-only (forbid_mutation trigger, no UPDATE/DELETE grant).
//
// activation_sequence is computed as one more than the current maximum for
// this (tenant, cell, kind, object_id) group. This reads the highest
// existing row with a plain SELECT rather than SELECT ... FOR UPDATE:
// migration 00027 deliberately grants hcmnext_app only SELECT and INSERT on
// this append-only table (REVOKE UPDATE, DELETE, matching its
// forbid_mutation trigger), and PostgreSQL requires UPDATE privilege to
// acquire a FOR UPDATE row lock even when the statement never writes
// anything — so a locking read would fail with a permission error for the
// very role this adapter always runs as. A genuine race between two
// concurrent activations for the same group is still caught by the
// sequence's own UNIQUE constraint (config_object_activation_sequence_unique),
// which surfaces here as an insert error rather than silently losing an
// activation's evidence; this is optimistic concurrency by design, not a
// missing lock.
func (s *Store) PutActivation(rec platformconfig.ActivationRecord) error {
	ctx := context.Background()
	return s.withTenant(ctx, rec.Scope.TenantID, func(tx dbport.Tx) error {
		var currentMax int64
		row := tx.QueryRow(ctx, `
			SELECT activation_sequence FROM config_object_activation
			WHERE tenant_id = $1 AND cell_id = $2 AND kind = $3 AND object_id = $4
			ORDER BY activation_sequence DESC LIMIT 1`,
			rec.Scope.TenantID, rec.Scope.CellID, string(rec.Kind), rec.ID,
		)
		switch err := row.Scan(&currentMax); {
		case errors.Is(err, dbport.ErrNoRows):
			currentMax = 0
		case err != nil:
			return err
		}

		_, err := tx.Exec(ctx, `
			INSERT INTO config_object_activation (
				tenant_id, activation_id, cell_id, kind, object_id, revision,
				activation_sequence, activated_by, authority, reason, activated_at, object_digest
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
			rec.Scope.TenantID, uuid.New(), rec.Scope.CellID, string(rec.Kind), rec.ID, int64(rec.Revision),
			currentMax+1, rec.ActivatedBy, rec.Authority, rec.Reason, rec.ActivatedAt, rec.ObjectDigest,
		)
		return err
	})
}

func scanActivation(row interface {
	Scan(dest ...any) error
}, scope platformconfig.Scope, kind platformconfig.Kind, id string) (platformconfig.ActivationRecord, error) {
	var (
		revision                       int64
		activatedBy, authority, reason string
		activatedAt                    time.Time
		objectDigest                   string
	)
	if err := row.Scan(&revision, &activatedBy, &authority, &reason, &activatedAt, &objectDigest); err != nil {
		return platformconfig.ActivationRecord{}, err
	}
	return platformconfig.ActivationRecord{
		Scope: scope, Kind: kind, ID: id, Revision: uint32(revision),
		ActivatedBy: activatedBy, Authority: authority, Reason: reason,
		ActivatedAt: activatedAt, ObjectDigest: objectDigest,
	}, nil
}

// GetLatestActivation implements [platformconfig.Store]: the row with the
// highest activation_sequence for (scope, kind, id).
func (s *Store) GetLatestActivation(scope platformconfig.Scope, kind platformconfig.Kind, id string) (platformconfig.ActivationRecord, bool, error) {
	ctx := context.Background()
	var (
		rec   platformconfig.ActivationRecord
		found bool
	)
	err := s.withTenant(ctx, scope.TenantID, func(tx dbport.Tx) error {
		row := tx.QueryRow(ctx, `
			SELECT revision, activated_by, authority, reason, activated_at, object_digest
			FROM config_object_activation
			WHERE tenant_id = $1 AND cell_id = $2 AND kind = $3 AND object_id = $4
			ORDER BY activation_sequence DESC LIMIT 1`,
			scope.TenantID, scope.CellID, string(kind), id,
		)
		got, err := scanActivation(row, scope, kind, id)
		if errors.Is(err, dbport.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		rec, found = got, true
		return nil
	})
	if err != nil {
		return platformconfig.ActivationRecord{}, false, err
	}
	return rec, found, nil
}

// ListActivations implements [platformconfig.Store], oldest first — the
// full, permanent supersession history for (scope, kind, id).
func (s *Store) ListActivations(scope platformconfig.Scope, kind platformconfig.Kind, id string) ([]platformconfig.ActivationRecord, error) {
	ctx := context.Background()
	var out []platformconfig.ActivationRecord
	err := s.withTenant(ctx, scope.TenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT revision, activated_by, authority, reason, activated_at, object_digest
			FROM config_object_activation
			WHERE tenant_id = $1 AND cell_id = $2 AND kind = $3 AND object_id = $4
			ORDER BY activation_sequence ASC`,
			scope.TenantID, scope.CellID, string(kind), id,
		)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			rec, err := scanActivation(rows, scope, kind, id)
			if err != nil {
				return err
			}
			out = append(out, rec)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
