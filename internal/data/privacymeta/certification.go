package privacymeta

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

var (
	ErrCertificationBlocked = errors.New("privacymeta: copy inventory certification blocked")
	ErrStaleCopy            = errors.New("privacymeta: copy verification is stale")
)

type CopyInventoryCertificate struct {
	TenantID    uuid.UUID
	InventoryID uuid.UUID
	IssuedAt    time.Time
	CopyCount   int
	Digest      string
}

func CertifyCopyInventory(ctx context.Context, q dbport.Querier, tenantID, inventoryID uuid.UUID, asOf time.Time, maxAge time.Duration) (CopyInventoryCertificate, error) {
	if q == nil || tenantID == uuid.Nil || inventoryID == uuid.Nil {
		return CopyInventoryCertificate{}, fmt.Errorf("%w: tenant-scoped store is required", ErrCertificationBlocked)
	}
	// Inventory evidence and its members must come from one PostgreSQL statement
	// snapshot. Two queries under READ COMMITTED can otherwise certify an
	// inventory header and a copy set that never existed together.
	rows, err := q.Query(ctx, `SELECT
		i.tenant_id, i.inventory_id, i.canonical_asset_key, i.as_of, i.source_watermark,
		i.expected_sources, i.source_watermarks, i.completeness, i.unknown_count, i.content_digest, i.created_at,
		c.copy_id, c.copy_type, c.store_ref, c.discovery_source, c.subject_ref, c.data_category,
		c.processor_ref, c.region, c.field_scope, c.encryption_key_ref, c.retention_schedule_key,
		c.hold_state, c.deletion_capability, c.restore_policy, c.last_verified_at, c.created_at
		FROM data_copy_inventory i
		JOIN data_copy c ON c.tenant_id=i.tenant_id AND c.inventory_id=i.inventory_id
		WHERE i.tenant_id=$1 AND i.inventory_id=$2
		ORDER BY c.copy_type, c.store_ref, c.copy_id`, tenantID, inventoryID)
	if err != nil {
		return CopyInventoryCertificate{}, fmt.Errorf("%w: list copies: %v", ErrCertificationBlocked, err)
	}
	defer rows.Close()
	var inventory DataCopyInventory
	var copies []DataCopy
	for rows.Next() {
		var copy DataCopy
		var expected, watermarks, scope []byte
		if err := rows.Scan(
			&inventory.TenantID, &inventory.InventoryID, &inventory.CanonicalAssetKey, &inventory.AsOf, &inventory.SourceWatermark,
			&expected, &watermarks, &inventory.Completeness, &inventory.UnknownCount, &inventory.ContentDigest, &inventory.CreatedAt,
			&copy.CopyID, &copy.CopyType, &copy.StoreRef, &copy.DiscoverySource, &copy.SubjectRef, &copy.DataCategory,
			&copy.ProcessorRef, &copy.Region, &scope, &copy.EncryptionKeyRef, &copy.RetentionScheduleKey,
			&copy.HoldState, &copy.DeletionCapability, &copy.RestorePolicy, &copy.LastVerifiedAt, &copy.CreatedAt,
		); err != nil {
			return CopyInventoryCertificate{}, fmt.Errorf("%w: scan copy: %v", ErrCertificationBlocked, err)
		}
		inventory.ExpectedSources, inventory.SourceWatermarks = expected, watermarks
		copy.TenantID, copy.InventoryID, copy.CanonicalAssetKey, copy.FieldScope = inventory.TenantID, inventory.InventoryID, inventory.CanonicalAssetKey, scope
		copies = append(copies, copy)
	}
	if err := rows.Err(); err != nil {
		return CopyInventoryCertificate{}, fmt.Errorf("%w: list copies: %v", ErrCertificationBlocked, err)
	}
	if len(copies) == 0 {
		return CopyInventoryCertificate{}, fmt.Errorf("%w: inventory contains no discovered copies", ErrCertificationBlocked)
	}
	return certifyLoadedCopyInventory(inventory, copies, asOf, maxAge)
}

func certifyLoadedCopyInventory(inventory DataCopyInventory, copies []DataCopy, asOf time.Time, maxAge time.Duration) (CopyInventoryCertificate, error) {
	if err := inventory.Validate(); err != nil {
		return CopyInventoryCertificate{}, fmt.Errorf("%w: inventory: %v", ErrCertificationBlocked, err)
	}
	if inventory.Completeness != "COMPLETE" || inventory.UnknownCount != 0 {
		return CopyInventoryCertificate{}, fmt.Errorf("%w: inventory is not complete", ErrCertificationBlocked)
	}
	if asOf.IsZero() || maxAge <= 0 {
		return CopyInventoryCertificate{}, fmt.Errorf("%w: certification time and freshness window are required", ErrCertificationBlocked)
	}
	if inventory.AsOf.After(asOf) || asOf.Sub(inventory.AsOf) > maxAge {
		return CopyInventoryCertificate{}, fmt.Errorf("%w: inventory watermark", ErrStaleCopy)
	}
	var expected []string
	var watermarks map[string]string
	if json.Unmarshal(inventory.ExpectedSources, &expected) != nil || json.Unmarshal(inventory.SourceWatermarks, &watermarks) != nil {
		return CopyInventoryCertificate{}, fmt.Errorf("%w: invalid discovery coverage", ErrCertificationBlocked)
	}
	expectedSet := make(map[string]bool, len(expected))
	for _, source := range expected {
		expectedSet[source] = true
	}
	discovered := make(map[string]bool, len(expected))
	seen := make(map[string]bool, len(copies))
	for _, copy := range copies {
		if err := copy.Validate(); err != nil {
			return CopyInventoryCertificate{}, fmt.Errorf("%w: copy: %v", ErrCertificationBlocked, err)
		}
		if copy.TenantID != inventory.TenantID || copy.InventoryID != inventory.InventoryID || copy.CanonicalAssetKey != inventory.CanonicalAssetKey {
			return CopyInventoryCertificate{}, fmt.Errorf("%w: copy is outside inventory scope", ErrCertificationBlocked)
		}
		if seen[copy.CopyID.String()] {
			return CopyInventoryCertificate{}, fmt.Errorf("%w: duplicate copy %s", ErrCertificationBlocked, copy.CopyID)
		}
		seen[copy.CopyID.String()] = true
		if !expectedSet[copy.DiscoverySource] {
			return CopyInventoryCertificate{}, fmt.Errorf("%w: copy %s came from unexpected discovery source %q", ErrCertificationBlocked, copy.CopyID, copy.DiscoverySource)
		}
		discovered[copy.DiscoverySource] = true
		if copy.DeletionCapability == "NONE" {
			return CopyInventoryCertificate{}, fmt.Errorf("%w: copy %s lacks required lifecycle metadata", ErrCertificationBlocked, copy.CopyID)
		}
		if copy.LastVerifiedAt == nil || copy.LastVerifiedAt.After(asOf) || asOf.Sub(*copy.LastVerifiedAt) > maxAge {
			return CopyInventoryCertificate{}, fmt.Errorf("%w: copy %s", ErrStaleCopy, copy.CopyID)
		}
		var scope map[string]any
		if len(copy.FieldScope) == 0 || json.Unmarshal(copy.FieldScope, &scope) != nil {
			return CopyInventoryCertificate{}, fmt.Errorf("%w: copy %s field scope is not metadata", ErrCertificationBlocked, copy.CopyID)
		}
		if len(scope) == 0 {
			return CopyInventoryCertificate{}, fmt.Errorf("%w: copy %s field scope is empty", ErrCertificationBlocked, copy.CopyID)
		}
	}
	for _, source := range expected {
		if !discovered[source] || strings.TrimSpace(watermarks[source]) == "" {
			return CopyInventoryCertificate{}, fmt.Errorf("%w: expected discovery source %q is not covered", ErrCertificationBlocked, source)
		}
	}
	sort.Strings(expected)
	canonicalExpectedBytes, err := json.Marshal(expected)
	if err != nil {
		return CopyInventoryCertificate{}, fmt.Errorf("%w: expected sources: %v", ErrCertificationBlocked, err)
	}
	canonicalExpected := string(canonicalExpectedBytes)
	canonicalWatermarks, err := canonicalJSON(inventory.SourceWatermarks)
	if err != nil {
		return CopyInventoryCertificate{}, fmt.Errorf("%w: source watermarks: %v", ErrCertificationBlocked, err)
	}
	w := canonicalbytes.New("hcmnext.privacymeta.CopyInventoryCertificate", 2).String("tenant_id", inventory.TenantID.String()).String("inventory_id", inventory.InventoryID.String()).String("asset", inventory.CanonicalAssetKey).String("watermark", inventory.SourceWatermark).String("expected_sources", canonicalExpected).String("source_watermarks", canonicalWatermarks).String("inventory_digest", inventory.ContentDigest).String("inventory_as_of", inventory.AsOf.UTC().Format(time.RFC3339Nano)).String("issued_at", asOf.UTC().Format(time.RFC3339Nano)).Int("max_age_ns", maxAge.Nanoseconds()).Int("copies", int64(len(copies)))
	for _, copy := range copies {
		canonicalScope, err := canonicalJSON(copy.FieldScope)
		if err != nil {
			return CopyInventoryCertificate{}, fmt.Errorf("%w: copy %s field scope: %v", ErrCertificationBlocked, copy.CopyID, err)
		}
		w.String("copy_id", copy.CopyID.String()).String("copy_type", copy.CopyType).String("store_ref", copy.StoreRef).String("discovery_source", copy.DiscoverySource).String("subject", copy.SubjectRef).String("category", copy.DataCategory).String("processor", copy.ProcessorRef).String("region", copy.Region).String("field_scope", canonicalScope).String("key", copy.EncryptionKeyRef).String("retention", copy.RetentionScheduleKey).String("hold", copy.HoldState).String("deletion", copy.DeletionCapability).String("restore", copy.RestorePolicy).String("verified_at", copy.LastVerifiedAt.UTC().Format(time.RFC3339Nano))
	}
	digest, err := w.Digest()
	if err != nil {
		return CopyInventoryCertificate{}, fmt.Errorf("%w: certificate digest: %v", ErrCertificationBlocked, err)
	}
	return CopyInventoryCertificate{TenantID: inventory.TenantID, InventoryID: inventory.InventoryID, IssuedAt: asOf.UTC(), CopyCount: len(copies), Digest: digest}, nil
}

func canonicalJSON(raw json.RawMessage) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := decodeCanonicalJSONValue(decoder)
	if err != nil {
		return "", err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return "", errors.New("trailing JSON value")
		}
		return "", err
	}
	encoded, err := json.Marshal(value)
	return string(encoded), err
}

func decodeCanonicalJSONValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return token, nil
	}
	switch delimiter {
	case '{':
		object := make(map[string]any)
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, errors.New("JSON object key is not a string")
			}
			if _, duplicate := object[key]; duplicate {
				return nil, fmt.Errorf("duplicate JSON object key %q", key)
			}
			value, err := decodeCanonicalJSONValue(decoder)
			if err != nil {
				return nil, err
			}
			object[key] = value
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return nil, errors.New("unterminated JSON object")
		}
		return object, nil
	case '[':
		var array []any
		for decoder.More() {
			value, err := decodeCanonicalJSONValue(decoder)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return nil, errors.New("unterminated JSON array")
		}
		return array, nil
	default:
		return nil, fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
}
