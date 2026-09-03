// Package tenancy is the single Go entry point for establishing the
// PostgreSQL session-local tenant context that migration 00008's row level
// security policies key on (owner: data plane; phase: P1A; DB-017).
//
// # Why a session setting, not a WHERE clause
//
// Every tenant-scoped table already carries tenant_id and every repository is
// expected to filter on it, but a repository that forgets is a bug that
// silently returns another tenant's rows. Migration 00008 makes tenant
// isolation a property of the database connection instead: every
// tenant-scoped table has a row level security policy that compares its own
// tenant_id column against the app.tenant_id session setting, enforced for
// the hcmnext_app role regardless of what SQL a caller happens to run.
// Repository-level filtering remains defense in depth, not the only boundary
// (DB-017's own REFACTOR clause).
//
// # Why per transaction, not per connection
//
// WithTenant calls PostgreSQL's set_config with is_local = true, which scopes
// the setting to the current transaction: it is visible to every statement
// that transaction runs and is discarded automatically when the transaction
// commits or rolls back. A pooled connection handed to the next, unrelated
// request therefore starts with no tenant selected -- fail closed by
// construction, never a stale tenant left over from whoever used the
// connection before.
package tenancy

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
)

// SessionSetting is the PostgreSQL custom run-time parameter every
// tenant-scoped row level security policy created by migration 00008 reads.
// It needs no extension and no shared_preload_libraries entry: PostgreSQL
// accepts SET/set_config on any dotted "placeholder" name that is not one of
// its own built-in parameters.
const SessionSetting = "app.tenant_id"

// AppRole is the least-privilege PostgreSQL role migration 00008 creates. It
// carries NOSUPERUSER and NOBYPASSRLS and owns no table, so row level
// security applies to it unconditionally. It is NOLOGIN: a session reaches it
// with "SET ROLE hcmnext_app" from an already-authenticated identity, never by
// direct password authentication.
const AppRole = "hcmnext_app"

// Execer is the minimal capability WithTenant needs: one parameterised
// statement inside the caller's own transaction. A [dbport.Tx] and a
// [dbport.Conn] both satisfy it, and so does anything else that runs a
// parameterised statement through the port.
type Execer = dbport.Execer

// WithTenant scopes tx to tenantID for every tenant-scoped table's row level
// security policy, for the lifetime of tx only. Call it once, as the first
// statement of a transaction that will touch tenant-scoped data; every
// statement that transaction runs afterward is confined to tenantID's rows,
// and the confinement disappears with the transaction.
//
// tenantID must not be the nil UUID. A nil tenant would set app.tenant_id to
// the all-zero UUID, which is indistinguishable at the SQL level from a real
// (if catastrophically unlikely) tenant identifier, and would therefore fail
// open into "whatever tenant happens to have that id" rather than failing
// loudly at the call site that forgot to supply a real one. Rejecting it here
// means the only way to see zero rows from a forgotten tenant scope is to
// never call WithTenant at all, which is exactly the fail-closed behavior
// migration 00008's policies are written to produce.
func WithTenant(ctx context.Context, tx Execer, tenantID uuid.UUID) error {
	if tenantID == uuid.Nil {
		return fmt.Errorf("tenancy: tenant id must not be the nil UUID")
	}
	// set_config(name, value, is_local) is used instead of a literal
	// "SET LOCAL app.tenant_id = ..." statement because SET does not accept a
	// bind parameter: the value would have to be interpolated into the SQL
	// text by hand, which is exactly the kind of string-built statement this
	// package exists to avoid. set_config is an ordinary function call and
	// takes tenantID as a normal, safely bound argument.
	if _, err := tx.Exec(ctx, `SELECT set_config($1, $2, true)`, SessionSetting, tenantID.String()); err != nil {
		return fmt.Errorf("tenancy: set %s: %w", SessionSetting, err)
	}
	return nil
}
