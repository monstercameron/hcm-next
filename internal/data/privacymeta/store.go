// Package privacymeta is the tenant-scoped store for the privacy metadata
// migration 00032 creates (DB-015): versioned processing-purpose
// declarations, and the watermarked copy inventory that answers "where does
// this asset actually live" before any deletion, hold or transfer decision is
// made.
//
// The rule this package will not let a caller break, which the schema also
// enforces: a census that reports unknowns is never COMPLETE. A deletion or
// hold decision taken from a PARTIAL inventory is a decision taken with
// known blind spots, and the row says so rather than rounding up.
package privacymeta

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

var (
	// ErrNilTenant is returned when a row omits its tenant or its own id.
	ErrNilTenant = errors.New("privacymeta: tenant or row id is nil")
	// ErrMissingVersion is returned when a versioned row omits its version.
	ErrMissingVersion = errors.New("privacymeta: version missing or not positive")
	// ErrMissingDigest is returned when a required digest is absent.
	ErrMissingDigest = errors.New("privacymeta: content digest missing")
	// ErrMissingScope is returned when a row omits the scope its downstream
	// decisions depend on (the asset key, the store, the retention
	// schedule).
	ErrMissingScope = errors.New("privacymeta: scope missing")
	// ErrIncompleteCensus is returned when an inventory claims to be
	// complete while still reporting unknowns.
	ErrIncompleteCensus = errors.New("privacymeta: a census with unknowns is not COMPLETE")
	// ErrInvalidEnum is returned for a value outside its declared set.
	ErrInvalidEnum = errors.New("privacymeta: value outside its declared set")
	// ErrInvalidInterval is returned for a reversed or empty interval.
	ErrInvalidInterval = errors.New("privacymeta: time interval invalid")
)

// ErrDetail names the field behind one of the sentinels above.
type ErrDetail struct {
	Sentinel error
	Detail   string
}

func (e ErrDetail) Error() string { return fmt.Sprintf("%v: %s", e.Sentinel, e.Detail) }

func (e ErrDetail) Unwrap() error { return e.Sentinel }

func detail(sentinel error, format string, args ...any) error {
	return ErrDetail{Sentinel: sentinel, Detail: fmt.Sprintf(format, args...)}
}

// PrivacyTables is the exact set of base tables migration 00032 creates for
// the privacy family, sorted.
var PrivacyTables = []string{
	"data_copy",
	"data_copy_inventory",
	"processing_purpose_declaration",
}

// CopyTypes is the copy taxonomy the assurance model declares, duplicated by
// the schema's data_copy_type_allowed constraint.
var CopyTypes = []string{
	"AUTHORITATIVE", "PROJECTION", "SEARCH", "VECTOR", "CACHE",
	"TELEMETRY", "AGENT_MEMORY", "EXPORT", "BACKUP", "PROVIDER",
}

func ensureTenant(ctx context.Context, tx dbport.Execer, tenantID uuid.UUID) error {
	if tenantID == uuid.Nil {
		return ErrNilTenant
	}
	return tenancy.WithTenant(ctx, tx, tenantID)
}

func object(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}

func oneOf(value string, allowed ...string) bool {
	for _, a := range allowed {
		if value == a {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// processing_purpose_declaration
// ---------------------------------------------------------------------------

// ProcessingPurposeDeclaration is one versioned statement of what a tenant
// may process for a purpose, on what basis, and for how long.
type ProcessingPurposeDeclaration struct {
	TenantID             uuid.UUID       `json:"tenant_id"`
	DeclarationID        uuid.UUID       `json:"declaration_id"`
	PurposeKey           string          `json:"purpose_key"`
	DeclarationVersion   int64           `json:"declaration_version"`
	LawfulBasis          string          `json:"lawful_basis"`
	DataCategories       json.RawMessage `json:"data_categories"`
	AllowedOperations    json.RawMessage `json:"allowed_operations"`
	Recipients           json.RawMessage `json:"recipients"`
	RetentionScheduleKey string          `json:"retention_schedule_key"`
	TransferPolicyRef    string          `json:"transfer_policy_ref"`
	ContentDigest        string          `json:"content_digest"`
	EffectiveFrom        time.Time       `json:"effective_from"`
	EffectiveTo          *time.Time      `json:"effective_to"`
	Status               string          `json:"status"`
	CreatedAt            time.Time       `json:"created_at"`
}

func (d ProcessingPurposeDeclaration) Validate() error {
	if d.TenantID == uuid.Nil || d.DeclarationID == uuid.Nil {
		return ErrNilTenant
	}
	if d.DeclarationVersion < 1 {
		return detail(ErrMissingVersion, "processing_purpose_declaration.declaration_version")
	}
	if d.ContentDigest == "" {
		return detail(ErrMissingDigest, "processing_purpose_declaration.content_digest")
	}
	if d.PurposeKey == "" || d.RetentionScheduleKey == "" {
		return detail(ErrMissingScope, "processing_purpose_declaration purpose_key/retention_schedule_key")
	}
	if !oneOf(d.LawfulBasis, "CONSENT", "CONTRACT", "LEGAL_OBLIGATION", "VITAL_INTEREST", "PUBLIC_TASK", "LEGITIMATE_INTEREST") {
		return detail(ErrInvalidEnum, "processing_purpose_declaration.lawful_basis=%q", d.LawfulBasis)
	}
	if !oneOf(d.Status, "DRAFT", "ACTIVE", "SUPERSEDED", "WITHDRAWN") {
		return detail(ErrInvalidEnum, "processing_purpose_declaration.status=%q", d.Status)
	}
	if d.EffectiveTo != nil && !d.EffectiveTo.After(d.EffectiveFrom) {
		return detail(ErrInvalidInterval, "processing_purpose_declaration effective interval is not half-open")
	}
	return nil
}

func InsertProcessingPurposeDeclaration(ctx context.Context, tx dbport.Tx, d ProcessingPurposeDeclaration) error {
	if err := d.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, d.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO processing_purpose_declaration (tenant_id, declaration_id, purpose_key, declaration_version, lawful_basis, data_categories, allowed_operations, recipients, retention_schedule_key, transfer_policy_ref, content_digest, effective_from, effective_to, status, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		d.TenantID, d.DeclarationID, d.PurposeKey, d.DeclarationVersion, d.LawfulBasis, object(d.DataCategories), object(d.AllowedOperations), object(d.Recipients), d.RetentionScheduleKey, d.TransferPolicyRef, d.ContentDigest, d.EffectiveFrom, d.EffectiveTo, d.Status, d.CreatedAt)
	return err
}

func LoadProcessingPurposeDeclaration(ctx context.Context, q dbport.Querier, tenantID, declarationID uuid.UUID) (ProcessingPurposeDeclaration, error) {
	var d ProcessingPurposeDeclaration
	var categories, operations, recipients []byte
	var to *time.Time
	err := q.QueryRow(ctx, `SELECT tenant_id, declaration_id, purpose_key, declaration_version, lawful_basis, data_categories, allowed_operations, recipients, retention_schedule_key, transfer_policy_ref, content_digest, effective_from, effective_to, status, created_at FROM processing_purpose_declaration WHERE tenant_id=$1 AND declaration_id=$2`, tenantID, declarationID).
		Scan(&d.TenantID, &d.DeclarationID, &d.PurposeKey, &d.DeclarationVersion, &d.LawfulBasis, &categories, &operations, &recipients, &d.RetentionScheduleKey, &d.TransferPolicyRef, &d.ContentDigest, &d.EffectiveFrom, &to, &d.Status, &d.CreatedAt)
	if err != nil {
		return ProcessingPurposeDeclaration{}, err
	}
	d.DataCategories = categories
	d.AllowedOperations = operations
	d.Recipients = recipients
	d.EffectiveTo = to
	return d, nil
}

// ---------------------------------------------------------------------------
// data_copy_inventory and data_copy
// ---------------------------------------------------------------------------

// DataCopyInventory is one immutable, watermarked census of an asset's copies.
type DataCopyInventory struct {
	TenantID          uuid.UUID       `json:"tenant_id"`
	InventoryID       uuid.UUID       `json:"inventory_id"`
	CanonicalAssetKey string          `json:"canonical_asset_key"`
	AsOf              time.Time       `json:"as_of"`
	SourceWatermark   string          `json:"source_watermark"`
	ExpectedSources   json.RawMessage `json:"expected_sources"`
	SourceWatermarks  json.RawMessage `json:"source_watermarks"`
	Completeness      string          `json:"completeness"`
	UnknownCount      int32           `json:"unknown_count"`
	ContentDigest     string          `json:"content_digest"`
	CreatedAt         time.Time       `json:"created_at"`
}

func (i DataCopyInventory) Validate() error {
	if i.TenantID == uuid.Nil || i.InventoryID == uuid.Nil {
		return ErrNilTenant
	}
	if i.CanonicalAssetKey == "" {
		return detail(ErrMissingScope, "data_copy_inventory.canonical_asset_key")
	}
	if i.SourceWatermark == "" {
		return detail(ErrMissingScope, "data_copy_inventory.source_watermark")
	}
	var expected []string
	if len(i.ExpectedSources) == 0 || json.Unmarshal(i.ExpectedSources, &expected) != nil || len(expected) == 0 {
		return detail(ErrMissingScope, "data_copy_inventory.expected_sources")
	}
	var watermarks map[string]string
	if len(i.SourceWatermarks) == 0 || json.Unmarshal(i.SourceWatermarks, &watermarks) != nil || len(watermarks) == 0 {
		return detail(ErrMissingScope, "data_copy_inventory.source_watermarks")
	}
	seen := make(map[string]bool, len(expected))
	for _, source := range expected {
		if source == "" || seen[source] || watermarks[source] == "" {
			return detail(ErrIncompleteCensus, "expected discovery source %q has no unique watermark", source)
		}
		seen[source] = true
	}
	if len(watermarks) != len(seen) {
		return detail(ErrIncompleteCensus, "source watermark set does not match expected discovery sources")
	}
	if i.ContentDigest == "" {
		return detail(ErrMissingDigest, "data_copy_inventory.content_digest")
	}
	if !oneOf(i.Completeness, "COMPLETE", "PARTIAL", "UNKNOWN") {
		return detail(ErrInvalidEnum, "data_copy_inventory.completeness=%q", i.Completeness)
	}
	if i.UnknownCount < 0 {
		return detail(ErrInvalidEnum, "data_copy_inventory.unknown_count=%d", i.UnknownCount)
	}
	if i.Completeness == "COMPLETE" && i.UnknownCount != 0 {
		return detail(ErrIncompleteCensus, "%d unknown copies", i.UnknownCount)
	}
	return nil
}

func InsertDataCopyInventory(ctx context.Context, tx dbport.Tx, i DataCopyInventory) error {
	if err := i.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, i.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO data_copy_inventory (tenant_id, inventory_id, canonical_asset_key, as_of, source_watermark, expected_sources, source_watermarks, completeness, unknown_count, content_digest, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		i.TenantID, i.InventoryID, i.CanonicalAssetKey, i.AsOf, i.SourceWatermark, i.ExpectedSources, i.SourceWatermarks, i.Completeness, i.UnknownCount, i.ContentDigest, i.CreatedAt)
	return err
}

func LoadDataCopyInventory(ctx context.Context, q dbport.Querier, tenantID, inventoryID uuid.UUID) (DataCopyInventory, error) {
	var i DataCopyInventory
	var expected, watermarks []byte
	err := q.QueryRow(ctx, `SELECT tenant_id, inventory_id, canonical_asset_key, as_of, source_watermark, expected_sources, source_watermarks, completeness, unknown_count, content_digest, created_at FROM data_copy_inventory WHERE tenant_id=$1 AND inventory_id=$2`, tenantID, inventoryID).
		Scan(&i.TenantID, &i.InventoryID, &i.CanonicalAssetKey, &i.AsOf, &i.SourceWatermark, &expected, &watermarks, &i.Completeness, &i.UnknownCount, &i.ContentDigest, &i.CreatedAt)
	if err != nil {
		return DataCopyInventory{}, err
	}
	i.ExpectedSources, i.SourceWatermarks = expected, watermarks
	return i, nil
}

// DataCopy is one located copy of a canonical asset, with the retention and
// hold state that governs whether it can be deleted.
type DataCopy struct {
	TenantID             uuid.UUID       `json:"tenant_id"`
	CopyID               uuid.UUID       `json:"copy_id"`
	InventoryID          uuid.UUID       `json:"inventory_id"`
	CanonicalAssetKey    string          `json:"canonical_asset_key"`
	CopyType             string          `json:"copy_type"`
	StoreRef             string          `json:"store_ref"`
	DiscoverySource      string          `json:"discovery_source"`
	SubjectRef           string          `json:"subject_ref"`
	DataCategory         string          `json:"data_category"`
	ProcessorRef         string          `json:"processor_ref"`
	Region               string          `json:"region"`
	FieldScope           json.RawMessage `json:"field_scope"`
	EncryptionKeyRef     string          `json:"encryption_key_ref"`
	RetentionScheduleKey string          `json:"retention_schedule_key"`
	HoldState            string          `json:"hold_state"`
	DeletionCapability   string          `json:"deletion_capability"`
	RestorePolicy        string          `json:"restore_policy"`
	LastVerifiedAt       *time.Time      `json:"last_verified_at"`
	CreatedAt            time.Time       `json:"created_at"`
}

func (c DataCopy) Validate() error {
	if c.TenantID == uuid.Nil || c.CopyID == uuid.Nil || c.InventoryID == uuid.Nil {
		return ErrNilTenant
	}
	if c.CanonicalAssetKey == "" || c.StoreRef == "" || c.DiscoverySource == "" || c.SubjectRef == "" || c.DataCategory == "" || c.ProcessorRef == "" || c.Region == "" || c.EncryptionKeyRef == "" || c.RetentionScheduleKey == "" || c.RestorePolicy == "" {
		return detail(ErrMissingScope, "data_copy asset/store/discovery source/subject/category/processor/location/key/retention/restore metadata")
	}
	if !oneOf(c.CopyType, CopyTypes...) {
		return detail(ErrInvalidEnum, "data_copy.copy_type=%q", c.CopyType)
	}
	if !oneOf(c.HoldState, "NONE", "HELD", "RELEASED") {
		return detail(ErrInvalidEnum, "data_copy.hold_state=%q", c.HoldState)
	}
	if !oneOf(c.DeletionCapability, "DELETE", "ANONYMIZE", "TOMBSTONE", "NONE") {
		return detail(ErrInvalidEnum, "data_copy.deletion_capability=%q", c.DeletionCapability)
	}
	if c.CopyType == "PROVIDER" && c.ProcessorRef == "" {
		return detail(ErrMissingScope, "data_copy of type PROVIDER without a processor_ref")
	}
	return nil
}

func InsertDataCopy(ctx context.Context, tx dbport.Tx, c DataCopy) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, c.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO data_copy (tenant_id, copy_id, inventory_id, canonical_asset_key, copy_type, store_ref, discovery_source, subject_ref, data_category, processor_ref, region, field_scope, encryption_key_ref, retention_schedule_key, hold_state, deletion_capability, restore_policy, last_verified_at, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`,
		c.TenantID, c.CopyID, c.InventoryID, c.CanonicalAssetKey, c.CopyType, c.StoreRef, c.DiscoverySource, c.SubjectRef, c.DataCategory, c.ProcessorRef, c.Region, object(c.FieldScope), c.EncryptionKeyRef, c.RetentionScheduleKey, c.HoldState, c.DeletionCapability, c.RestorePolicy, c.LastVerifiedAt, c.CreatedAt)
	return err
}

func LoadDataCopy(ctx context.Context, q dbport.Querier, tenantID, copyID uuid.UUID) (DataCopy, error) {
	var c DataCopy
	var scope []byte
	var verified *time.Time
	err := q.QueryRow(ctx, `SELECT tenant_id, copy_id, inventory_id, canonical_asset_key, copy_type, store_ref, discovery_source, subject_ref, data_category, processor_ref, region, field_scope, encryption_key_ref, retention_schedule_key, hold_state, deletion_capability, restore_policy, last_verified_at, created_at FROM data_copy WHERE tenant_id=$1 AND copy_id=$2`, tenantID, copyID).
		Scan(&c.TenantID, &c.CopyID, &c.InventoryID, &c.CanonicalAssetKey, &c.CopyType, &c.StoreRef, &c.DiscoverySource, &c.SubjectRef, &c.DataCategory, &c.ProcessorRef, &c.Region, &scope, &c.EncryptionKeyRef, &c.RetentionScheduleKey, &c.HoldState, &c.DeletionCapability, &c.RestorePolicy, &verified, &c.CreatedAt)
	if err != nil {
		return DataCopy{}, err
	}
	c.FieldScope = scope
	c.LastVerifiedAt = verified
	return c, nil
}

// ListCopyTypes returns the copy types recorded under one inventory, sorted.
// A deletion plan reads this to know which stores it must actually reach.
func ListCopyTypes(ctx context.Context, q dbport.Querier, tenantID, inventoryID uuid.UUID) ([]string, error) {
	rows, err := q.Query(ctx, `SELECT copy_type FROM data_copy WHERE tenant_id=$1 AND inventory_id=$2`, tenantID, inventoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var kind string
		if err := rows.Scan(&kind); err != nil {
			return nil, err
		}
		out = append(out, kind)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}
