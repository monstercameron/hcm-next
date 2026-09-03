package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
)

var (
	ErrNotFound       = errors.New("store: not found")
	ErrScopeRequired  = errors.New("store: scope required")
	ErrTenantRequired = errors.New("store: tenant required")
	ErrCellRequired   = errors.New("store: cell required")
	ErrEncryption     = errors.New("store: encryption boundary")
)

const (
	EncryptionPlatformManaged = "PLATFORM_MANAGED"
	EncryptionFieldLevel      = "FIELD_LEVEL"
)

type Scope struct {
	TenantID        uuid.UUID
	CellID          string
	EncryptionClass string
	IsMaintenance   bool
}

func (s Scope) Validate() error {
	if s.TenantID == uuid.Nil {
		return ErrTenantRequired
	}
	if strings.TrimSpace(s.CellID) == "" {
		return ErrCellRequired
	}
	if s.EncryptionClass != EncryptionPlatformManaged && s.EncryptionClass != EncryptionFieldLevel {
		return fmt.Errorf("%w: %s", ErrEncryption, s.EncryptionClass)
	}
	return nil
}

type Adapter struct {
	db    dbport.Conn
	table string
}

func NewAdapter(db dbport.Conn, table string) *Adapter {
	return &Adapter{db: db, table: table}
}

func (a *Adapter) ensureTable(ctx context.Context) error {
	_, err := a.db.Exec(ctx, fmt.Sprintf(`create table if not exists %s (tenant_id uuid not null, cell_id text not null, k text not null, v text not null, primary key (tenant_id, cell_id, k))`, a.table))
	return err
}

func (a *Adapter) validateKey(scope Scope, key string) error {
	if !strings.HasPrefix(key, scope.CellID+":") {
		return fmt.Errorf("%w: key prefix mismatch", ErrScopeRequired)
	}
	if strings.Contains(key, ":tenant:") {
		return fmt.Errorf("%w: forged prefix", ErrScopeRequired)
	}
	return nil
}

func (a *Adapter) Put(ctx context.Context, scope Scope, key, value string) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if err := a.validateKey(scope, key); err != nil {
		return err
	}
	if err := a.ensureTable(ctx); err != nil {
		return err
	}
	needsTx := scope.IsMaintenance
	if needsTx {
		b, ok := a.db.(dbport.Beginner)
		if ok {
			tx, err := b.Begin(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if err := tenancy.WithTenant(ctx, tx, scope.TenantID); err != nil {
				return err
			}
			_, err = tx.Exec(ctx, fmt.Sprintf(`insert into %s (tenant_id, cell_id, k, v) values ($1,$2,$3,$4) on conflict (tenant_id, cell_id, k) do update set v=$4`, a.table), scope.TenantID.String(), scope.CellID, key, value)
			if err != nil {
				return err
			}
			return tx.Commit(ctx)
		}
	}
	_, err := a.db.Exec(ctx, fmt.Sprintf(`insert into %s (tenant_id, cell_id, k, v) values ($1,$2,$3,$4) on conflict (tenant_id, cell_id, k) do update set v=$4`, a.table), scope.TenantID.String(), scope.CellID, key, value)
	return err
}

func (a *Adapter) Get(ctx context.Context, scope Scope, key string) (string, error) {
	if err := scope.Validate(); err != nil {
		return "", ErrNotFound
	}
	if err := a.validateKey(scope, key); err != nil {
		return "", ErrNotFound
	}
	if err := a.ensureTable(ctx); err != nil {
		return "", err
	}
	var v string
	err := a.db.QueryRow(ctx, fmt.Sprintf(`select v from %s where tenant_id=$1 and cell_id=$2 and k=$3`, a.table), scope.TenantID.String(), scope.CellID, key).Scan(&v)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	return v, nil
}

func (a *Adapter) List(ctx context.Context, scope Scope, prefix string) ([]string, error) {
	if err := scope.Validate(); err != nil {
		return nil, ErrNotFound
	}
	if prefix != "" && !strings.HasPrefix(prefix, scope.CellID+":") {
		return nil, ErrNotFound
	}
	if err := a.ensureTable(ctx); err != nil {
		return nil, err
	}
	like := scope.CellID + ":%"
	if prefix != "" {
		like = prefix + "%"
	}
	rows, err := a.db.Query(ctx, fmt.Sprintf(`select k from %s where tenant_id=$1 and cell_id=$2 and k like $3 order by k`, a.table), scope.TenantID.String(), scope.CellID, like)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (a *Adapter) Delete(ctx context.Context, scope Scope, key string) error {
	if err := scope.Validate(); err != nil {
		return ErrNotFound
	}
	if err := a.validateKey(scope, key); err != nil {
		return ErrNotFound
	}
	if err := a.ensureTable(ctx); err != nil {
		return err
	}
	_, err := a.db.Exec(ctx, fmt.Sprintf(`delete from %s where tenant_id=$1 and cell_id=$2 and k=$3`, a.table), scope.TenantID.String(), scope.CellID, key)
	return err
}
