package recovery

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// RestoreStatus describes whether a restored tenant may leave the recovery
// fence. A receipt is useful even when conformance is incomplete: it records
// exactly what was restored while keeping the service fenced.
type RestoreStatus string

const (
	RestoreFenced RestoreStatus = "FENCED"
	RestoreReady  RestoreStatus = "READY"
)

// RestoreConformance is the independent acceptance set for a tenant restore.
// Every check must pass before a restore can be marked ready.
type RestoreConformance struct {
	LedgerHeadsValid   bool
	ForeignKeysValid   bool
	RuntimeLeasesValid bool
	HoldsApplied       bool
	DeletionsApplied   bool
	HashesValid        bool
}

func (c RestoreConformance) passed() bool {
	return c.LedgerHeadsValid && c.ForeignKeysValid && c.RuntimeLeasesValid && c.HoldsApplied && c.DeletionsApplied && c.HashesValid
}

// RestoredRow is a tenant-owned, already verified row in the isolated
// restore. Restricted and deleted rows must never be supplied as active data.
type RestoredRow struct {
	ID       string
	TenantID string
	Plane    string
	Digest   string
}

// TenantRestoreRequest is the pure input to the restore acceptance gate. It
// contains references and digests only; the actual bytes remain in the
// restore adapter that produced the request.
type TenantRestoreRequest struct {
	TenantID               string
	RecoveryPoint          time.Time
	RestoredAt             time.Time
	Rows                   []RestoredRow
	DeletedIDs             []string
	HeldIDs                []string
	LedgerHead             string
	MigrationJournalDigest string
	RuntimeLeaseEpoch      uint64
	Conformance            RestoreConformance
	Isolated               bool
	ProductionEffects      bool
}

// RestoreReceipt is the audit-safe result of tenant restore validation.
type RestoreReceipt struct {
	Status                 RestoreStatus
	Fenced                 bool
	TenantID               string
	RecoveryPoint          time.Time
	RestoredAt             time.Time
	RPO                    time.Duration
	RowCount               int
	HeldCount              int
	DeletedCount           int
	DataDigest             string
	ManifestDigest         string
	LedgerHead             string
	MigrationJournalDigest string
	RuntimeLeaseEpoch      uint64
	Conformance            RestoreConformance
}

var (
	ErrInvalidTenantRestore = errors.New("recovery: invalid tenant restore")
	ErrCrossTenantRestore   = errors.New("recovery: restore contains a foreign tenant")
	ErrRestrictedRestore    = errors.New("recovery: restore would resurrect restricted data")
)

// RestoreTenant validates an isolated tenant restore and produces an exact,
// deterministic receipt. Failed conformance is not an error because the
// receipt is the evidence that the service remains fenced.
func RestoreTenant(req TenantRestoreRequest) (RestoreReceipt, error) {
	if !validTenant(req.TenantID) || req.RecoveryPoint.IsZero() || req.RestoredAt.IsZero() || req.RestoredAt.Before(req.RecoveryPoint) {
		return RestoreReceipt{}, fmt.Errorf("%w: tenant and recovery times are required", ErrInvalidTenantRestore)
	}
	if !req.Isolated {
		return RestoreReceipt{}, fmt.Errorf("%w: restore must target an isolated environment", ErrInvalidTenantRestore)
	}
	if req.ProductionEffects {
		return RestoreReceipt{}, fmt.Errorf("%w: production effects are forbidden", ErrInvalidTenantRestore)
	}
	if len(req.Rows) == 0 || strings.TrimSpace(req.LedgerHead) == "" || strings.TrimSpace(req.MigrationJournalDigest) == "" || req.RuntimeLeaseEpoch == 0 {
		return RestoreReceipt{}, fmt.Errorf("%w: rows, ledger head, migration journal and lease epoch are required", ErrInvalidTenantRestore)
	}
	deleted, err := manifestIDs(req.DeletedIDs, "deleted")
	if err != nil {
		return RestoreReceipt{}, err
	}
	held, err := manifestIDs(req.HeldIDs, "held")
	if err != nil {
		return RestoreReceipt{}, err
	}
	rows := append([]RestoredRow(nil), req.Rows...)
	seenRows := make(map[string]bool, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.ID) == "" || strings.TrimSpace(row.Plane) == "" || strings.TrimSpace(row.Digest) == "" {
			return RestoreReceipt{}, fmt.Errorf("%w: every restored row needs id, plane and digest", ErrInvalidTenantRestore)
		}
		if row.TenantID != req.TenantID {
			return RestoreReceipt{}, fmt.Errorf("%w: row %q belongs to %q", ErrCrossTenantRestore, row.ID, row.TenantID)
		}
		if deleted[row.ID] {
			return RestoreReceipt{}, fmt.Errorf("%w: deleted row %q is present", ErrRestrictedRestore, row.ID)
		}
		if seenRows[row.ID] {
			return RestoreReceipt{}, fmt.Errorf("%w: duplicate row %q", ErrInvalidTenantRestore, row.ID)
		}
		seenRows[row.ID] = true
	}
	for id := range held {
		if !seenRows[id] {
			return RestoreReceipt{}, fmt.Errorf("%w: hold %q has no restored row", ErrInvalidTenantRestore, id)
		}
	}

	dataDigest := digestRestoreRows(rows, req.LedgerHead)
	manifestDigest := digestManifests(req.DeletedIDs, req.HeldIDs)
	status := RestoreFenced
	if req.Conformance.passed() {
		status = RestoreReady
	}
	return RestoreReceipt{
		Status:                 status,
		Fenced:                 status != RestoreReady,
		TenantID:               req.TenantID,
		RecoveryPoint:          req.RecoveryPoint.UTC(),
		RestoredAt:             req.RestoredAt.UTC(),
		RPO:                    req.RestoredAt.Sub(req.RecoveryPoint),
		RowCount:               len(rows),
		HeldCount:              len(held),
		DeletedCount:           len(deleted),
		DataDigest:             dataDigest,
		ManifestDigest:         manifestDigest,
		LedgerHead:             req.LedgerHead,
		MigrationJournalDigest: req.MigrationJournalDigest,
		RuntimeLeaseEpoch:      req.RuntimeLeaseEpoch,
		Conformance:            req.Conformance,
	}, nil
}

// ExplainRestore returns bounded evidence without row identifiers or payloads.
func ExplainRestore(receipt RestoreReceipt) string {
	return fmt.Sprintf("tenant restore status=%s fenced=%t rows=%d held=%d deleted=%d rpo=%s data=%s manifest=%s", receipt.Status, receipt.Fenced, receipt.RowCount, receipt.HeldCount, receipt.DeletedCount, receipt.RPO, receipt.DataDigest, receipt.ManifestDigest)
}

func manifestIDs(ids []string, kind string) (map[string]bool, error) {
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		if strings.TrimSpace(id) == "" || out[id] {
			return nil, fmt.Errorf("%w: invalid or duplicate %s manifest id", ErrInvalidTenantRestore, kind)
		}
		out[id] = true
	}
	return out, nil
}

func digestRestoreRows(rows []RestoredRow, ledgerHead string) string {
	values := make([]string, 0, len(rows))
	for _, row := range rows {
		values = append(values, strings.Join([]string{row.TenantID, row.Plane, row.ID, row.Digest}, "\x00"))
	}
	sort.Strings(values)
	data := strings.Join(append(values, ledgerHead), "\x00")
	sum := sha256.Sum256([]byte(data))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func digestManifests(deleted, held []string) string {
	deletes := append([]string(nil), deleted...)
	holds := append([]string(nil), held...)
	sort.Strings(deletes)
	sort.Strings(holds)
	sum := sha256.Sum256([]byte("deleted=" + strings.Join(deletes, ",") + "\x00held=" + strings.Join(holds, ",")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
