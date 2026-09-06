package tenancy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/domains/tenant"
)

// BootstrapReceipt is one durably recorded, applied bootstrap manifest
// revision (TENANT-002 evidence), read back from migrations/00029's
// tenant_bootstrap_receipt.
type BootstrapReceipt struct {
	ReceiptID  uuid.UUID
	TenantID   uuid.UUID
	ManifestID string
	Revision   uint64
	Digest     string
	AppliedAt  time.Time
}

// LatestBootstrap returns the most recently applied manifest revision for
// tenantID, or nil when the tenant has never been bootstrapped through this
// ledger. It is q's own read, independent of any particular manifest
// identity: a tenant may only ever have one manifest identity's history
// applied against it (see [tenant.ResolveBootstrap]), so the single latest
// row -- highest revision -- is always the whole answer.
func LatestBootstrap(ctx context.Context, q dbport.Querier, tenantID uuid.UUID) (*tenant.BootstrapRecord, error) {
	if tenantID == uuid.Nil {
		return nil, fmt.Errorf("tenancy: latest bootstrap needs a tenant id")
	}
	var rec tenant.BootstrapRecord
	var revision int64
	err := q.QueryRow(ctx, `
		SELECT manifest_id, revision, digest, applied_at
		FROM tenant_bootstrap_receipt
		WHERE tenant_id = $1
		ORDER BY revision DESC, applied_at DESC
		LIMIT 1`, tenantID).Scan(&rec.ManifestID, &revision, &rec.Digest, &rec.AppliedAt)
	if errors.Is(err, dbport.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("tenancy: read latest bootstrap receipt for %s: %w", tenantID, err)
	}
	rec.Revision = uint64(revision)
	return &rec, nil
}

// receiptAt returns the digest already recorded for tenantID under exactly
// (manifestID, revision), and whether one exists.
func receiptAt(ctx context.Context, q dbport.Querier, tenantID uuid.UUID, manifestID string, revision uint64) (string, bool, error) {
	var digest string
	err := q.QueryRow(ctx, `
		SELECT digest FROM tenant_bootstrap_receipt
		WHERE tenant_id = $1 AND manifest_id = $2 AND revision = $3`,
		tenantID, manifestID, int64(revision)).Scan(&digest)
	if errors.Is(err, dbport.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("tenancy: read bootstrap receipt %s revision %d for %s: %w", manifestID, revision, tenantID, err)
	}
	return digest, true, nil
}

// ListBootstrapReceipts returns every applied bootstrap receipt for tenantID,
// oldest revision first: the full evidence trail TENANT-002 requires.
func ListBootstrapReceipts(ctx context.Context, q dbport.Querier, tenantID uuid.UUID) ([]BootstrapReceipt, error) {
	if tenantID == uuid.Nil {
		return nil, fmt.Errorf("tenancy: list bootstrap receipts needs a tenant id")
	}
	rows, err := q.Query(ctx, `
		SELECT receipt_id, tenant_id, manifest_id, revision, digest, applied_at
		FROM tenant_bootstrap_receipt
		WHERE tenant_id = $1
		ORDER BY revision ASC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("tenancy: list bootstrap receipts for %s: %w", tenantID, err)
	}
	defer rows.Close()

	var out []BootstrapReceipt
	for rows.Next() {
		var r BootstrapReceipt
		var revision int64
		if err := rows.Scan(&r.ReceiptID, &r.TenantID, &r.ManifestID, &revision, &r.Digest, &r.AppliedAt); err != nil {
			return nil, fmt.Errorf("tenancy: scan bootstrap receipt for %s: %w", tenantID, err)
		}
		r.Revision = uint64(revision)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tenancy: list bootstrap receipts for %s: %w", tenantID, err)
	}
	return out, nil
}

// Bootstrap resolves manifest against tenantID's currently applied receipt
// (via [LatestBootstrap]) and, only when the outcome is
// [tenant.BootstrapApply], records a new receipt inside tx. A NOOP or
// REJECTED outcome writes nothing: [tenant.ResolveBootstrap]'s decision is
// authoritative, and this function never second-guesses it.
//
// Bootstrap never touches the `tenant` registration row itself, or any
// identity/admin/key/placement/product/policy/schema/recovery-contact/audit/
// health plane: those are separate, undelivered contracts (TOOL-014,
// TRUST-019 and friends). A caller wanting one pilot tenant fully bootstrapped
// composes this with whatever registers the tenant's own row -- in P1A,
// internal/intent/app/pgstore.Store.Bootstrap, which already makes that
// registration idempotent by tenant id (`ON CONFLICT (tenant_id) DO
// NOTHING`). pgstore.Store.Bootstrap has no concept of a manifest revision to
// guard against, though; a caller that wants "the same manifest content is
// refused unless declared as a new revision" enforced end to end runs this
// function -- typically before or alongside pgstore.Store.Bootstrap, in a
// transaction it commits together with (or immediately following) the tenant
// row registration -- rather than relying on pgstore.Store.Bootstrap alone.
//
// Concurrency: two callers racing to apply the identical manifest can both
// read the same "not yet applied" state before either commits. The insert
// below is an INSERT ... ON CONFLICT (tenant_id, manifest_id, revision) DO
// NOTHING, the same idempotent-by-identity technique
// internal/data/artifacts.PutStream uses for its own content-id race: the
// loser re-reads whatever the winner actually recorded at that exact
// (manifest, revision) and reports NOOP when it matches (nothing lost) or
// REJECTED when it does not (a genuine same-revision conflict), rather than
// surfacing the raw unique-constraint violation.
func Bootstrap(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, manifest tenant.BootstrapManifest) (tenant.BootstrapOutcome, error) {
	if tenantID == uuid.Nil {
		return tenant.BootstrapOutcome{}, fmt.Errorf("tenancy: bootstrap needs a tenant id")
	}

	current, err := LatestBootstrap(ctx, tx, tenantID)
	if err != nil {
		return tenant.BootstrapOutcome{}, err
	}
	outcome, err := tenant.ResolveBootstrap(current, manifest)
	if err != nil {
		return outcome, err
	}
	if outcome.Decision != tenant.BootstrapApply {
		return outcome, nil
	}

	payload, err := json.Marshal(manifest)
	if err != nil {
		return tenant.BootstrapOutcome{}, fmt.Errorf("tenancy: encode bootstrap manifest %s: %w", manifest.ManifestID, err)
	}

	affected, err := tx.Exec(ctx, `
		INSERT INTO tenant_bootstrap_receipt
			(tenant_id, receipt_id, manifest_id, revision, digest, decision, manifest)
		VALUES ($1, $2, $3, $4, $5, 'APPLIED', $6)
		ON CONFLICT (tenant_id, manifest_id, revision) DO NOTHING`,
		tenantID, uuid.New(), manifest.ManifestID, int64(manifest.Revision), outcome.Digest, payload)
	if err != nil {
		return tenant.BootstrapOutcome{}, fmt.Errorf("tenancy: record bootstrap receipt for %s: %w", tenantID, err)
	}
	if affected == 1 {
		return outcome, nil
	}

	// Someone else concurrently recorded this exact (tenant, manifest,
	// revision) first. Resolve against what is now actually on file instead
	// of trusting the read this call started with.
	racedDigest, found, err := receiptAt(ctx, tx, tenantID, manifest.ManifestID, manifest.Revision)
	if err != nil {
		return tenant.BootstrapOutcome{}, err
	}
	if !found {
		return tenant.BootstrapOutcome{}, fmt.Errorf(
			"tenancy: bootstrap insert for %s revision %d reported a conflict but no receipt is on file",
			manifest.ManifestID, manifest.Revision)
	}
	if strings.EqualFold(racedDigest, outcome.Digest) {
		return tenant.BootstrapOutcome{Decision: tenant.BootstrapNoop, Digest: outcome.Digest}, nil
	}
	rejectErr := fmt.Errorf(
		"%w: a concurrent apply recorded manifest %q revision %d with digest %s before this one committed",
		tenant.ErrBootstrapRejected, manifest.ManifestID, manifest.Revision, racedDigest)
	return tenant.BootstrapOutcome{Decision: tenant.BootstrapRejected, Digest: outcome.Digest, Reason: rejectErr.Error()}, rejectErr
}
