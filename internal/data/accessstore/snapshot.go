package accessstore

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/domains/access"
	"github.com/monstercameron/hcm-next/internal/domains/evidence"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

type identityRow struct {
	id              string
	rowID           uuid.UUID
	subject, system string
}
type accountRow struct {
	id       string
	rowID    uuid.UUID
	identity identityRow
}
type entitlementRow struct {
	id     string
	rowID  uuid.UUID
	system string
}

func loadSnapshot(ctx context.Context, ex Executor, tenantID uuid.UUID, tenant values.TenantId) (access.Graph, error) {
	identities, identityRows, err := loadIdentities(ctx, ex, tenantID, tenant)
	if err != nil {
		return access.Graph{}, err
	}
	accounts, accountRows, err := loadAccounts(ctx, ex, tenantID, tenant, identityRows)
	if err != nil {
		return access.Graph{}, err
	}
	entitlements, entitlementRows, err := loadEntitlements(ctx, ex, tenantID, tenant)
	if err != nil {
		return access.Graph{}, err
	}
	expected, err := loadExpected(ctx, ex, tenantID, tenant, identityRows, accountRows, entitlementRows)
	if err != nil {
		return access.Graph{}, err
	}
	graph := access.Graph{Tenant: tenant, Identities: identities, Accounts: accounts, Entitlements: entitlements, Expected: expected}
	if err := graph.Validate(); err != nil {
		return access.Graph{}, fmt.Errorf("accessstore: validate snapshot: %w", err)
	}
	return graph, nil
}

func loadIdentities(ctx context.Context, ex Executor, tenantID uuid.UUID, tenant values.TenantId) ([]access.WorkforceIdentity, map[uuid.UUID]identityRow, error) {
	rows, err := ex.Query(ctx, `SELECT DISTINCT ON (identity_id) row_id,identity_id,subject,system,worker_ref,revision,authority_class,effective_from,effective_to,known_at,lifecycle,digest FROM workforce_identity WHERE tenant_id=$1 ORDER BY identity_id,revision DESC`, tenantID)
	if err != nil {
		return nil, nil, fmt.Errorf("accessstore: load workforce identities: %w", err)
	}
	defer rows.Close()
	out := make([]access.WorkforceIdentity, 0)
	refs := make(map[uuid.UUID]identityRow)
	for rows.Next() {
		var rowID, worker uuid.UUID
		var id, subject, system, authority, lifecycle, storedDigest string
		var revision int64
		var from, known time.Time
		var to *time.Time
		if err := rows.Scan(&rowID, &id, &subject, &system, &worker, &revision, &authority, &from, &to, &known, &lifecycle, &storedDigest); err != nil {
			return nil, nil, err
		}
		record, err := rehydrateIdentity(tenant, id, subject, system, worker, revision, authority, from, to, known, lifecycle, storedDigest)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, record)
		refs[rowID] = identityRow{id: id, rowID: rowID, subject: subject, system: system}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return out, refs, nil
}

func loadAccounts(ctx context.Context, ex Executor, tenantID uuid.UUID, tenant values.TenantId, identities map[uuid.UUID]identityRow) ([]access.AccountLink, map[uuid.UUID]accountRow, error) {
	rows, err := ex.Query(ctx, `SELECT DISTINCT ON (a.link_id) a.row_id,a.link_id,a.workforce_identity_ref,a.source,a.application,a.account_id,a.revision,a.effective_from,a.effective_to,a.digest FROM account_link a WHERE a.tenant_id=$1 ORDER BY a.link_id,a.revision DESC`, tenantID)
	if err != nil {
		return nil, nil, fmt.Errorf("accessstore: load account links: %w", err)
	}
	defer rows.Close()
	out := make([]access.AccountLink, 0)
	refs := make(map[uuid.UUID]accountRow)
	for rows.Next() {
		var rowID, identityRef uuid.UUID
		var id, source, application, accountID, storedDigest string
		var revision int64
		var from time.Time
		var to *time.Time
		if err := rows.Scan(&rowID, &id, &identityRef, &source, &application, &accountID, &revision, &from, &to, &storedDigest); err != nil {
			return nil, nil, err
		}
		identity, ok := identities[identityRef]
		if !ok {
			return nil, nil, fmt.Errorf("%w: account link %s identity", access.ErrUnownedReference, id)
		}
		record, err := rehydrateAccount(tenant, id, identity, source, application, accountID, revision, from, to, storedDigest)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, record)
		refs[rowID] = accountRow{id: id, rowID: rowID, identity: identity}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return out, refs, nil
}

func loadEntitlements(ctx context.Context, ex Executor, tenantID uuid.UUID, tenant values.TenantId) ([]access.EntitlementDefinition, map[uuid.UUID]entitlementRow, error) {
	rows, err := ex.Query(ctx, `SELECT DISTINCT ON (definition_id) row_id,definition_id,application,code,version,risk_class,owner,revision,effective_from,effective_to,digest FROM entitlement_definition WHERE tenant_id=$1 ORDER BY definition_id,revision DESC`, tenantID)
	if err != nil {
		return nil, nil, fmt.Errorf("accessstore: load entitlement definitions: %w", err)
	}
	defer rows.Close()
	out := make([]access.EntitlementDefinition, 0)
	refs := make(map[uuid.UUID]entitlementRow)
	for rows.Next() {
		var rowID uuid.UUID
		var id, application, code, version, risk, owner, storedDigest string
		var revision int64
		var from time.Time
		var to *time.Time
		if err := rows.Scan(&rowID, &id, &application, &code, &version, &risk, &owner, &revision, &from, &to, &storedDigest); err != nil {
			return nil, nil, err
		}
		record, err := rehydrateEntitlement(tenant, id, application, code, version, risk, owner, revision, from, to, storedDigest)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, record)
		refs[rowID] = entitlementRow{id: id, rowID: rowID, system: application}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return out, refs, nil
}

func loadExpected(ctx context.Context, ex Executor, tenantID uuid.UUID, tenant values.TenantId, identities map[uuid.UUID]identityRow, accounts map[uuid.UUID]accountRow, entitlements map[uuid.UUID]entitlementRow) ([]access.ExpectedEntitlement, error) {
	rows, err := ex.Query(ctx, `SELECT DISTINCT ON (expected_id) row_id,expected_id,workforce_identity_ref,account_link_ref,entitlement_ref,employment_ref,position_ref,policy_ref,revision,effective_from,effective_to,digest FROM expected_entitlement WHERE tenant_id=$1 ORDER BY expected_id,revision DESC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("accessstore: load expected entitlements: %w", err)
	}
	defer rows.Close()
	out := make([]access.ExpectedEntitlement, 0)
	for rows.Next() {
		var rowID, identityRef, entitlementRef uuid.UUID
		var accountRef, employmentRef, positionRef, policyRef *uuid.UUID
		var id, storedDigest string
		var revision int64
		var from time.Time
		var to *time.Time
		if err := rows.Scan(&rowID, &id, &identityRef, &accountRef, &entitlementRef, &employmentRef, &positionRef, &policyRef, &revision, &from, &to, &storedDigest); err != nil {
			return nil, err
		}
		identity, ok := identities[identityRef]
		if !ok {
			return nil, fmt.Errorf("%w: expected entitlement %s identity", access.ErrUnownedReference, id)
		}
		entitlement, ok := entitlements[entitlementRef]
		if !ok {
			return nil, fmt.Errorf("%w: expected entitlement %s definition", access.ErrUnownedReference, id)
		}
		accountID := ""
		if accountRef != nil {
			account, ok := accounts[*accountRef]
			if !ok {
				return nil, fmt.Errorf("%w: expected entitlement %s account", access.ErrUnownedReference, id)
			}
			accountID = account.id
		}
		record, err := rehydrateExpected(tenant, id, identity, accountID, entitlement, employmentRef, positionRef, policyRef, revision, from, to, storedDigest)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func rehydrateIdentity(tenant values.TenantId, id, subject, system string, worker uuid.UUID, revision int64, authority string, from time.Time, to *time.Time, known time.Time, lifecycle, storedDigest string) (access.WorkforceIdentity, error) {
	rev, err := values.NewSequenceRevision("access.workforce_identity."+id, uint64(revision))
	if err != nil {
		return access.WorkforceIdentity{}, err
	}
	effective, err := interval(from, to)
	if err != nil {
		return access.WorkforceIdentity{}, err
	}
	knownAt, prov, err := metadata(known, storedDigest)
	if err != nil {
		return access.WorkforceIdentity{}, err
	}
	return access.WorkforceIdentity{ID: id, Tenant: tenant, Subject: subject, System: system, WorkerRef: values.EntityRef{Tenant: tenant, Kind: "worker", Id: worker.String()}, Revision: rev, Authority: access.AuthorityClass(authority), Effective: effective, KnownAt: knownAt, Provenance: prov, Lifecycle: access.Lifecycle(lifecycle)}, nil
}

func rehydrateAccount(tenant values.TenantId, id string, identity identityRow, source, application, accountID string, revision int64, from time.Time, to *time.Time, storedDigest string) (access.AccountLink, error) {
	rev, err := values.NewSequenceRevision("access.account_link."+id, uint64(revision))
	if err != nil {
		return access.AccountLink{}, err
	}
	effective, err := interval(from, to)
	if err != nil {
		return access.AccountLink{}, err
	}
	known, prov, err := metadata(from, storedDigest)
	if err != nil {
		return access.AccountLink{}, err
	}
	return access.AccountLink{ID: id, Tenant: tenant, Subject: identity.subject, System: application, Source: source, WorkforceIdentityID: identity.id, Application: application, AccountID: accountID, Revision: rev, Authority: access.AuthorityNative, Effective: effective, KnownAt: known, Provenance: prov, Lifecycle: access.LifecycleActive}, nil
}

func rehydrateEntitlement(tenant values.TenantId, id, application, code, version, risk, owner string, revision int64, from time.Time, to *time.Time, storedDigest string) (access.EntitlementDefinition, error) {
	rev, err := values.NewSequenceRevision("access.entitlement_definition."+id, uint64(revision))
	if err != nil {
		return access.EntitlementDefinition{}, err
	}
	effective, err := interval(from, to)
	if err != nil {
		return access.EntitlementDefinition{}, err
	}
	known, prov, err := metadata(from, storedDigest)
	if err != nil {
		return access.EntitlementDefinition{}, err
	}
	return access.EntitlementDefinition{ID: id, Tenant: tenant, Subject: id, System: application, Application: application, Code: code, Version: version, RiskClass: access.RiskClass(risk), Owner: owner, Revision: rev, Authority: access.AuthorityNative, Effective: effective, KnownAt: known, Provenance: prov, Lifecycle: access.LifecycleActive}, nil
}

func rehydrateExpected(tenant values.TenantId, id string, identity identityRow, accountID string, entitlement entitlementRow, employment, position, policy *uuid.UUID, revision int64, from time.Time, to *time.Time, storedDigest string) (access.ExpectedEntitlement, error) {
	rev, err := values.NewSequenceRevision("access.expected_entitlement."+id, uint64(revision))
	if err != nil {
		return access.ExpectedEntitlement{}, err
	}
	effective, err := interval(from, to)
	if err != nil {
		return access.ExpectedEntitlement{}, err
	}
	known, prov, err := metadata(from, storedDigest)
	if err != nil {
		return access.ExpectedEntitlement{}, err
	}
	return access.ExpectedEntitlement{ID: id, Tenant: tenant, Subject: identity.subject, System: entitlement.system, WorkforceIdentityID: identity.id, AccountLinkID: accountID, EntitlementID: entitlement.id, EmploymentRef: uuidText(employment), PositionRef: uuidText(position), PolicyRef: uuidText(policy), Revision: rev, Authority: access.AuthorityNative, Effective: effective, KnownAt: known, Provenance: prov, Lifecycle: access.LifecycleActive}, nil
}

func uuidText(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}
func interval(from time.Time, to *time.Time) (values.EffectiveInterval, error) {
	start := values.NewInstant(from.UTC())
	if to == nil {
		return values.NewOpenInstantInterval(start)
	}
	return values.NewInstantInterval(start, values.NewInstant(to.UTC()))
}
func metadata(at time.Time, digest string) (values.KnownAt, evidence.Provenance, error) {
	instant := values.NewInstant(at.UTC())
	known, err := values.NewKnownAt(instant)
	if err != nil {
		return values.KnownAt{}, evidence.Provenance{}, err
	}
	recorded, err := values.NewRecordedAt(instant)
	if err != nil {
		return values.KnownAt{}, evidence.Provenance{}, err
	}
	return known, evidence.Provenance{Source: "accessstore", EvidenceRef: digest, RecordedAt: recorded}, nil
}
