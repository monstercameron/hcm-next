// Package customstore persists the tenant-scoped custom definition and record
// port declared by internal/domains/custom over migration 00086.
package customstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/custom"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// DB is the driver-free capability needed by the adapter. The store opens a
// transaction for every operation so tenant session state cannot leak across
// calls.
type DB interface{ dbport.Beginner }

// Store implements custom.Store over PostgreSQL.
type Store struct{ db DB }

var _ custom.Store = (*Store)(nil)

// New returns a custom persistence adapter over a database connection or pool
// that can assume the hcmnext_app role.
func New(db DB) *Store { return &Store{db: db} }

// SaveObjectDefinition appends an immutable object-definition version. A zero
// expected version means the object must not exist; otherwise the new version
// must immediately follow the expected current version.
func (s *Store) SaveObjectDefinition(ctx context.Context, tenantID string, definition custom.CustomObjectDefinition, expectedVersion uint64) error {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return err
	}
	if err := definition.Validate(); err != nil {
		return invalid(err.Error())
	}
	version, err := sqlVersion(definition.Version)
	if err != nil {
		return err
	}
	if _, err := sqlVersionAllowZero(expectedVersion); err != nil {
		return err
	}
	fields, err := json.Marshal(definition.Fields)
	if err != nil {
		return invalid(fmt.Sprintf("marshal object fields: %v", err))
	}
	return s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		exists, err := definitionVersionExists(ctx, tx, `
			SELECT EXISTS (SELECT 1 FROM custom_object_definition
				WHERE tenant_id=$1 AND kind=$2 AND namespace=$3 AND version=$4)`,
			tid, definition.Kind, definition.Namespace, version)
		if err != nil {
			return err
		}
		if exists {
			return duplicate(fmt.Sprintf("object definition %s/%s/%d", definition.Kind, definition.Namespace, definition.Version))
		}
		current, err := currentObjectVersion(ctx, tx, tid, definition.Kind, definition.Namespace)
		if err != nil {
			return err
		}
		if err := checkNextVersion(expectedVersion, current, definition.Version, "object definition"); err != nil {
			return err
		}
		affected, err := tx.Exec(ctx, `
			INSERT INTO custom_object_definition
				(row_id, tenant_id, kind, namespace, version, fields)
			VALUES ($1,$2,$3,$4,$5,$6::jsonb)
			ON CONFLICT (tenant_id, kind, namespace, version) DO NOTHING`,
			uuid.New(), tid, definition.Kind, definition.Namespace, version, string(fields))
		if err != nil {
			return fmt.Errorf("customstore: insert object definition: %w", err)
		}
		if affected == 0 {
			return duplicate(fmt.Sprintf("object definition %s/%s/%d", definition.Kind, definition.Namespace, definition.Version))
		}
		return nil
	})
}

// LoadObjectDefinition reads one immutable object-definition version under RLS.
func (s *Store) LoadObjectDefinition(ctx context.Context, tenantID, kind, namespace string, version uint64) (custom.CustomObjectDefinition, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return custom.CustomObjectDefinition{}, err
	}
	if kind == "" || namespace == "" || version == 0 {
		return custom.CustomObjectDefinition{}, invalid("kind, namespace and positive version are required")
	}
	sqlRev, err := sqlVersion(version)
	if err != nil {
		return custom.CustomObjectDefinition{}, err
	}
	var out custom.CustomObjectDefinition
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		loaded, loadErr := loadObjectTx(ctx, tx, tid, kind, namespace, sqlRev)
		if loadErr == nil {
			out = loaded
		}
		return loadErr
	})
	return out, err
}

// SaveRelationshipDefinition appends an immutable relationship-definition
// version under its name and namespace.
func (s *Store) SaveRelationshipDefinition(ctx context.Context, tenantID string, definition custom.CustomRelationshipDefinition, expectedVersion uint64) error {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return err
	}
	if err := definition.Validate(); err != nil {
		return invalid(err.Error())
	}
	version, err := sqlVersion(definition.Version)
	if err != nil {
		return err
	}
	if _, err := sqlVersionAllowZero(expectedVersion); err != nil {
		return err
	}
	return s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		exists, err := definitionVersionExists(ctx, tx, `
			SELECT EXISTS (SELECT 1 FROM custom_relationship_definition
				WHERE tenant_id=$1 AND name=$2 AND namespace=$3 AND version=$4)`,
			tid, definition.Name, definition.Namespace, version)
		if err != nil {
			return err
		}
		if exists {
			return duplicate(fmt.Sprintf("relationship definition %s/%s/%d", definition.Name, definition.Namespace, definition.Version))
		}
		current, err := currentRelationshipVersion(ctx, tx, tid, definition.Name, definition.Namespace)
		if err != nil {
			return err
		}
		if err := checkNextVersion(expectedVersion, current, definition.Version, "relationship definition"); err != nil {
			return err
		}
		affected, err := tx.Exec(ctx, `
			INSERT INTO custom_relationship_definition
				(row_id, tenant_id, name, namespace, version, source_kind,
				 target_kind, cardinality, effective_date_rule, allow_cycles)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
			ON CONFLICT (tenant_id, name, namespace, version) DO NOTHING`,
			uuid.New(), tid, definition.Name, definition.Namespace, version,
			definition.SourceKind, definition.TargetKind, string(definition.Cardinality),
			definition.EffectiveDateRule, definition.AllowCycles)
		if err != nil {
			return fmt.Errorf("customstore: insert relationship definition: %w", err)
		}
		if affected == 0 {
			return duplicate(fmt.Sprintf("relationship definition %s/%s/%d", definition.Name, definition.Namespace, definition.Version))
		}
		return nil
	})
}

// LoadRelationshipDefinition reads one immutable relationship-definition
// version under RLS.
func (s *Store) LoadRelationshipDefinition(ctx context.Context, tenantID, name, namespace string, version uint64) (custom.CustomRelationshipDefinition, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return custom.CustomRelationshipDefinition{}, err
	}
	if name == "" || namespace == "" || version == 0 {
		return custom.CustomRelationshipDefinition{}, invalid("name, namespace and positive version are required")
	}
	sqlRev, err := sqlVersion(version)
	if err != nil {
		return custom.CustomRelationshipDefinition{}, err
	}
	var out custom.CustomRelationshipDefinition
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		var (
			storedName, storedNamespace, sourceKind, targetKind, cardinality string
			storedRule                                                       *string
			storedVersion                                                    int64
			allowCycles                                                      bool
		)
		err := tx.QueryRow(ctx, `
			SELECT name, namespace, version, source_kind, target_kind,
				cardinality, effective_date_rule, allow_cycles
			FROM custom_relationship_definition
			WHERE tenant_id=$1 AND name=$2 AND namespace=$3 AND version=$4`,
			tid, name, namespace, sqlRev).Scan(
			&storedName, &storedNamespace, &storedVersion, &sourceKind, &targetKind,
			&cardinality, &storedRule, &allowCycles)
		if err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return notFound(fmt.Sprintf("relationship definition %s/%s/%d", name, namespace, version))
			}
			return fmt.Errorf("customstore: load relationship definition: %w", err)
		}
		if storedVersion <= 0 {
			return integrity("relationship definition has invalid version")
		}
		out = custom.CustomRelationshipDefinition{
			Name: storedName, Namespace: storedNamespace, Version: uint64(storedVersion),
			SourceKind: sourceKind, TargetKind: targetKind,
			Cardinality: custom.Cardinality(cardinality), AllowCycles: allowCycles,
		}
		if storedRule != nil {
			out.EffectiveDateRule = *storedRule
		}
		if err := out.Validate(); err != nil {
			return integrity(err.Error())
		}
		return nil
	})
	return out, err
}

// SaveRecordRevision appends one effective-dated record and verifies that its
// exact object-definition version exists in the same tenant.
func (s *Store) SaveRecordRevision(ctx context.Context, tenantID string, record custom.CustomRecordRevision) error {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return err
	}
	if record.ObjectID == "" || record.ObjectKind == "" || record.Namespace == "" || record.DefinitionVersion == 0 {
		return invalid("record identity and positive definition version are required")
	}
	knownAt := record.Known.Instant()
	if !knownAt.IsSet() {
		return invalid("known_at is required")
	}
	from, to, err := intervalTimes(record.Effective)
	if err != nil {
		return invalid(err.Error())
	}
	fieldValues, err := json.Marshal(record.FieldValues)
	if err != nil {
		return invalid(fmt.Sprintf("marshal field values: %v", err))
	}
	definitionVersion, err := sqlVersion(record.DefinitionVersion)
	if err != nil {
		return err
	}
	return s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		definition, err := loadObjectTx(ctx, tx, tid, record.ObjectKind, record.Namespace, definitionVersion)
		if err != nil {
			if errors.Is(err, custom.ErrStoreNotFound) {
				return reference(fmt.Sprintf("object definition %s/%s/%d", record.ObjectKind, record.Namespace, record.DefinitionVersion))
			}
			return err
		}
		if err := custom.ValidatePersistableRecord(definition, record); err != nil {
			return invalid(err.Error())
		}
		var recordedAt any
		if record.Recorded.Instant().IsSet() {
			recordedAt = record.Recorded.Instant().Time()
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO custom_record_revision
				(row_id, tenant_id, object_id, object_kind, namespace,
				 definition_version, effective_from, effective_to, recorded_at,
				 known_at, field_values)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,COALESCE($9::timestamptz, now()),$10,$11::jsonb)`,
			uuid.New(), tid, record.ObjectID, record.ObjectKind, record.Namespace,
			definitionVersion, from, to, recordedAt, knownAt.Time(), string(fieldValues))
		if err != nil {
			return fmt.Errorf("customstore: insert record revision: %w", err)
		}
		return nil
	})
}

// ListRecordRevisions returns all immutable revisions for an object in
// effective-start order. RLS still limits the rows to the supplied tenant.
func (s *Store) ListRecordRevisions(ctx context.Context, tenantID, objectID string) ([]custom.CustomRecordRevision, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return nil, err
	}
	if objectID == "" {
		return nil, invalid("object id is required")
	}
	var out []custom.CustomRecordRevision
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		type storedRecord struct {
			objectID, objectKind, namespace string
			definitionVersion               int64
			from, to                        *time.Time
			recordedAt, knownAt             time.Time
			fieldValues                     []byte
		}
		rows, err := tx.Query(ctx, `
			SELECT object_id, object_kind, namespace, definition_version,
				effective_from, effective_to, recorded_at, known_at, field_values
			FROM custom_record_revision
			WHERE tenant_id=$1 AND object_id=$2
			ORDER BY effective_from, recorded_at, row_id`, tid, objectID)
		if err != nil {
			return fmt.Errorf("customstore: list record revisions: %w", err)
		}
		var storedRows []storedRecord
		for rows.Next() {
			var row storedRecord
			if err := rows.Scan(&row.objectID, &row.objectKind, &row.namespace, &row.definitionVersion, &row.from, &row.to, &row.recordedAt, &row.knownAt, &row.fieldValues); err != nil {
				return fmt.Errorf("customstore: scan record revision: %w", err)
			}
			storedRows = append(storedRows, row)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("customstore: list record revisions: %w", err)
		}
		rows.Close()
		if len(storedRows) == 0 {
			return notFound(fmt.Sprintf("record %s", objectID))
		}
		for _, row := range storedRows {
			if row.from == nil || row.definitionVersion <= 0 {
				return integrity("record revision has incomplete temporal or definition data")
			}
			definition, err := loadObjectTx(ctx, tx, tid, row.objectKind, row.namespace, row.definitionVersion)
			if err != nil {
				return integrity(fmt.Sprintf("record revision definition: %v", err))
			}
			var fieldValuesMap map[string]custom.TypedValue
			if err := json.Unmarshal(row.fieldValues, &fieldValuesMap); err != nil {
				return integrity(fmt.Sprintf("record field values are invalid JSON: %v", err))
			}
			start := values.NewInstant(row.from.UTC())
			var effective values.EffectiveInterval
			if row.to == nil {
				effective, err = values.NewOpenInstantInterval(start)
			} else {
				effective, err = values.NewInstantInterval(start, values.NewInstant(row.to.UTC()))
			}
			if err != nil {
				return integrity(fmt.Sprintf("record effective interval: %v", err))
			}
			recorded, err := values.NewRecordedAt(values.NewInstant(row.recordedAt.UTC()))
			if err != nil {
				return integrity(fmt.Sprintf("record recorded_at: %v", err))
			}
			known, err := values.NewKnownAt(values.NewInstant(row.knownAt.UTC()))
			if err != nil {
				return integrity(fmt.Sprintf("record known_at: %v", err))
			}
			record := custom.CustomRecordRevision{
				ObjectID: row.objectID, ObjectKind: row.objectKind, Namespace: row.namespace,
				DefinitionVersion: uint64(row.definitionVersion), Effective: effective,
				Recorded: recorded, Known: known, FieldValues: fieldValuesMap,
			}
			if err := custom.ValidatePersistableRecord(definition, record); err != nil {
				return integrity(err.Error())
			}
			out = append(out, record)
		}
		return nil
	})
	return out, err
}

func (s *Store) withTenant(ctx context.Context, tenantID uuid.UUID, fn func(dbport.Tx) error) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("customstore: begin transaction: %w", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

func parseTenant(tenantID string) (uuid.UUID, error) {
	tid, err := uuid.Parse(tenantID)
	if err != nil || tid == uuid.Nil {
		return uuid.Nil, invalid(fmt.Sprintf("tenant id %q is not a valid uuid", tenantID))
	}
	return tid, nil
}

func loadObjectTx(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, kind, namespace string, version int64) (custom.CustomObjectDefinition, error) {
	var (
		storedKind, storedNamespace string
		storedVersion               int64
		fieldsJSON                  []byte
	)
	err := tx.QueryRow(ctx, `
		SELECT kind, namespace, version, fields
		FROM custom_object_definition
		WHERE tenant_id=$1 AND kind=$2 AND namespace=$3 AND version=$4`,
		tenantID, kind, namespace, version).Scan(&storedKind, &storedNamespace, &storedVersion, &fieldsJSON)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return custom.CustomObjectDefinition{}, notFound(fmt.Sprintf("object definition %s/%s/%d", kind, namespace, version))
		}
		return custom.CustomObjectDefinition{}, fmt.Errorf("customstore: load object definition: %w", err)
	}
	var fields map[string]custom.FieldDefinition
	if err := json.Unmarshal(fieldsJSON, &fields); err != nil {
		return custom.CustomObjectDefinition{}, integrity(fmt.Sprintf("object fields are invalid JSON: %v", err))
	}
	if storedVersion <= 0 {
		return custom.CustomObjectDefinition{}, integrity("object definition has invalid version")
	}
	definition := custom.CustomObjectDefinition{Kind: storedKind, Namespace: storedNamespace, Version: uint64(storedVersion), Fields: fields}
	if err := definition.Validate(); err != nil {
		return custom.CustomObjectDefinition{}, integrity(err.Error())
	}
	return definition, nil
}

func currentObjectVersion(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, kind, namespace string) (uint64, error) {
	return currentVersion(ctx, tx, `SELECT COALESCE(MAX(version), 0) FROM custom_object_definition WHERE tenant_id=$1 AND kind=$2 AND namespace=$3`, tenantID, kind, namespace)
}

func currentRelationshipVersion(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, name, namespace string) (uint64, error) {
	return currentVersion(ctx, tx, `SELECT COALESCE(MAX(version), 0) FROM custom_relationship_definition WHERE tenant_id=$1 AND name=$2 AND namespace=$3`, tenantID, name, namespace)
}

func definitionVersionExists(ctx context.Context, tx dbport.Tx, query string, args ...any) (bool, error) {
	var exists bool
	if err := tx.QueryRow(ctx, query, args...).Scan(&exists); err != nil {
		return false, fmt.Errorf("customstore: check definition version: %w", err)
	}
	return exists, nil
}

func currentVersion(ctx context.Context, tx dbport.Tx, query string, args ...any) (uint64, error) {
	var current int64
	if err := tx.QueryRow(ctx, query, args...).Scan(&current); err != nil {
		return 0, fmt.Errorf("customstore: read current definition version: %w", err)
	}
	if current < 0 {
		return 0, integrity("current definition version is negative")
	}
	return uint64(current), nil
}

func checkNextVersion(expected, actual, version uint64, kind string) error {
	switch {
	case expected == 0 && actual != 0:
		return stale(expected, actual, fmt.Sprintf("%s already has current version %d", kind, actual))
	case expected != 0 && actual != expected:
		return stale(expected, actual, fmt.Sprintf("%s current version is %d", kind, actual))
	case expected != 0 && version != expected+1:
		return stale(expected, actual, fmt.Sprintf("%s version %d does not supersede %d", kind, version, expected))
	}
	return nil
}

func intervalTimes(interval values.EffectiveInterval) (time.Time, *time.Time, error) {
	if start, ok := interval.StartInstant(); ok {
		if err := start.Validate(); err != nil {
			return time.Time{}, nil, err
		}
		if end, hasEnd := interval.EndInstant(); hasEnd {
			if err := end.Validate(); err != nil {
				return time.Time{}, nil, err
			}
			endTime := end.Time()
			return start.Time(), &endTime, nil
		}
		return start.Time(), nil, nil
	}
	if start, ok := interval.StartDate(); ok {
		if err := start.Validate(); err != nil {
			return time.Time{}, nil, err
		}
		from := time.Date(int(start.Year()), start.Month(), int(start.Day()), 0, 0, 0, 0, time.UTC)
		if end, hasEnd := interval.EndDate(); hasEnd {
			if err := end.Validate(); err != nil {
				return time.Time{}, nil, err
			}
			endTime := time.Date(int(end.Year()), end.Month(), int(end.Day()), 0, 0, 0, 0, time.UTC)
			return from, &endTime, nil
		}
		return from, nil, nil
	}
	return time.Time{}, nil, errors.New("effective interval is unset")
}

func sqlVersion(version uint64) (int64, error) {
	if version == 0 || version > uint64(^uint64(0)>>1) {
		return 0, invalid("version must fit positive PostgreSQL bigint")
	}
	return int64(version), nil
}

func sqlVersionAllowZero(version uint64) (int64, error) {
	if version > uint64(^uint64(0)>>1) {
		return 0, invalid("version must fit PostgreSQL bigint")
	}
	return int64(version), nil
}

func invalid(detail string) error {
	return &custom.StoreError{Code: custom.StoreInvalidCode, Detail: detail}
}
func notFound(detail string) error {
	return &custom.StoreError{Code: custom.StoreNotFoundCode, Detail: detail}
}
func duplicate(detail string) error {
	return &custom.StoreError{Code: custom.StoreDuplicateCode, Detail: detail}
}
func reference(detail string) error {
	return &custom.StoreError{Code: custom.StoreReferenceCode, Detail: detail}
}
func integrity(detail string) error {
	return &custom.StoreError{Code: custom.StoreIntegrityCode, Detail: detail}
}

func stale(expected, actual uint64, detail string) error {
	return &custom.StoreError{Code: custom.StoreStaleCASCode, Detail: detail, Expected: expected, Actual: actual}
}
