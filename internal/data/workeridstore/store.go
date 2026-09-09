// Package workeridstore persists organization worker-number policies and
// atomically reserves their globally unique output.
package workeridstore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/workerids"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type Store struct {
	db     dbport.Beginner
	tenant func(values.TenantId) uuid.UUID
}

func New(db dbport.Beginner, tenant func(values.TenantId) uuid.UUID) *Store {
	return &Store{db: db, tenant: tenant}
}

const columns = `version, prefix, suffix, separator, sequence_digits, start_at, next_sequence, increment_by, zero_pad, year_format, include_unit_code, check_digit, excluded_ranges`

func (s *Store) Load(ctx context.Context, tenant values.TenantId, organization string) (workerids.Policy, error) {
	p := workerids.DefaultPolicy()
	p.OrganizationScopeID = strings.TrimSpace(organization)
	if p.OrganizationScopeID == "" {
		return workerids.Policy{}, workerids.ErrInvalid
	}
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		err := scanPolicy(tx.QueryRow(ctx, `SELECT `+columns+` FROM organization_worker_id_policy WHERE tenant_id=$1 AND organization_scope_id=$2`, tenantID, p.OrganizationScopeID), &p)
		if errors.Is(err, dbport.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("workeridstore: load policy: %w", err)
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM worker_id_reservation WHERE tenant_id=$1 AND organization_scope_id=$2`, tenantID, p.OrganizationScopeID).Scan(&p.IssuedCount)
	})
	return workerids.Normalize(p), err
}

func (s *Store) Save(ctx context.Context, tenant values.TenantId, organization, actor string, p workerids.Policy) (workerids.Policy, error) {
	organization, actor = strings.TrimSpace(organization), strings.TrimSpace(actor)
	p = workerids.Normalize(p)
	p.OrganizationScopeID = organization
	if organization == "" || actor == "" {
		return workerids.Policy{}, workerids.ErrInvalid
	}
	if err := workerids.Validate(p); err != nil {
		return workerids.Policy{}, err
	}
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		if p.Version == 0 {
			p.NextSequence = p.StartAt
			affected, err := tx.Exec(ctx, `INSERT INTO organization_worker_id_policy (tenant_id,organization_scope_id,version,prefix,suffix,separator,sequence_digits,start_at,next_sequence,increment_by,zero_pad,year_format,include_unit_code,check_digit,excluded_ranges,updated_by) VALUES ($1,$2,1,$3,$4,$5,$6,$7,$7,$8,$9,$10,$11,$12,$13,$14) ON CONFLICT DO NOTHING`, tenantID, organization, p.Prefix, p.Suffix, p.Separator, p.SequenceDigits, p.StartAt, p.IncrementBy, p.ZeroPad, p.YearFormat, p.IncludeUnitCode, p.CheckDigit, p.ExcludedRanges, actor)
			if err != nil {
				return fmt.Errorf("workeridstore: insert policy: %w", err)
			}
			if affected != 1 {
				return workerids.ErrVersionConflict
			}
			p.Version = 1
			return nil
		}
		var next int64
		if err := tx.QueryRow(ctx, `SELECT next_sequence FROM organization_worker_id_policy WHERE tenant_id=$1 AND organization_scope_id=$2 AND version=$3 FOR UPDATE`, tenantID, organization, p.Version).Scan(&next); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return workerids.ErrVersionConflict
			}
			return err
		}
		p.NextSequence = next
		if err := workerids.Validate(p); err != nil {
			return err
		}
		affected, err := tx.Exec(ctx, `UPDATE organization_worker_id_policy SET version=version+1,prefix=$4,suffix=$5,separator=$6,sequence_digits=$7,start_at=$8,increment_by=$9,zero_pad=$10,year_format=$11,include_unit_code=$12,check_digit=$13,excluded_ranges=$14,updated_by=$15,updated_at=clock_timestamp() WHERE tenant_id=$1 AND organization_scope_id=$2 AND version=$3`, tenantID, organization, p.Version, p.Prefix, p.Suffix, p.Separator, p.SequenceDigits, p.StartAt, p.IncrementBy, p.ZeroPad, p.YearFormat, p.IncludeUnitCode, p.CheckDigit, p.ExcludedRanges, actor)
		if err != nil {
			return fmt.Errorf("workeridstore: update policy: %w", err)
		}
		if affected != 1 {
			return workerids.ErrVersionConflict
		}
		p.Version++
		return nil
	})
	return p, err
}

func (s *Store) Reserve(ctx context.Context, tenant values.TenantId, organization, actor string, fc workerids.FormatContext) (string, error) {
	organization, actor = strings.TrimSpace(organization), strings.TrimSpace(actor)
	if organization == "" || actor == "" {
		return "", workerids.ErrInvalid
	}
	var reserved string
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		d := workerids.DefaultPolicy()
		_, err := tx.Exec(ctx, `INSERT INTO organization_worker_id_policy (tenant_id,organization_scope_id,updated_by) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, tenantID, organization, actor)
		if err != nil {
			return err
		}
		p := d
		p.OrganizationScopeID = organization
		if err := scanPolicy(tx.QueryRow(ctx, `SELECT `+columns+` FROM organization_worker_id_policy WHERE tenant_id=$1 AND organization_scope_id=$2 FOR UPDATE`, tenantID, organization), &p); err != nil {
			return err
		}
		for attempts := 0; attempts < 10000; attempts++ {
			sequence := p.NextSequence
			p.NextSequence += p.IncrementBy
			candidate, formatErr := workerids.Format(p, sequence, fc)
			if formatErr != nil {
				return formatErr
			}
			if workerids.IsExcluded(p, sequence) {
				continue
			}
			affected, insertErr := tx.Exec(ctx, `INSERT INTO worker_id_reservation (tenant_id,organization_scope_id,worker_number,sequence_value,reserved_by) VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, tenantID, organization, candidate, sequence, actor)
			if insertErr != nil {
				return insertErr
			}
			if affected == 1 {
				reserved = candidate
				break
			}
		}
		if reserved == "" {
			return workerids.ErrExhausted
		}
		_, err = tx.Exec(ctx, `UPDATE organization_worker_id_policy SET next_sequence=$3,updated_at=clock_timestamp() WHERE tenant_id=$1 AND organization_scope_id=$2`, tenantID, organization, p.NextSequence)
		return err
	})
	return reserved, err
}

func scanPolicy(row dbport.Row, p *workerids.Policy) error {
	return row.Scan(&p.Version, &p.Prefix, &p.Suffix, &p.Separator, &p.SequenceDigits, &p.StartAt, &p.NextSequence, &p.IncrementBy, &p.ZeroPad, &p.YearFormat, &p.IncludeUnitCode, &p.CheckDigit, &p.ExcludedRanges)
}

func (s *Store) withTenant(ctx context.Context, tenant values.TenantId, fn func(dbport.Tx, uuid.UUID) error) error {
	if s == nil || s.db == nil || s.tenant == nil {
		return workerids.ErrUnavailable
	}
	tenantID := s.tenant(tenant)
	if tenantID == uuid.Nil {
		return workerids.ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = tenancy.WithTenant(ctx, tx, tenantID); err == nil {
		err = fn(tx, tenantID)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

var _ workerids.Store = (*Store)(nil)
