// Package tenantstore persists tenant placement, provisioning evidence and
// government-authorization profile revisions. It never starts or commits a
// transaction; callers must scope the supplied executor with tenancy.WithTenant.
package tenantstore

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/domains/tenant"
	"github.com/monstercameron/hcm-next/internal/domains/tenant/govauth"
)

type Executor interface {
	dbport.Execer
	dbport.Querier
}

type Error = tenant.StoreError
type ErrorCode = tenant.StoreCode

const (
	CodeInvalid   = tenant.StoreCodeInvalid
	CodeDuplicate = tenant.StoreCodeDuplicate
	CodeNotFound  = tenant.StoreCodeNotFound
	CodeStaleCAS  = tenant.StoreCodeStaleCAS
)

var (
	ErrInvalid   = tenant.ErrInvalid
	ErrDuplicate = tenant.ErrDuplicate
	ErrNotFound  = tenant.ErrNotFound
	ErrStaleCAS  = tenant.ErrStaleCAS
)

// Store is bound to a caller-owned connection or transaction.
type Store struct {
	ex Executor
}

// New binds the store to ex. ex must already have the desired tenant scope
// when it is an hcmnext_app connection or transaction.
func New(ex Executor) Store { return Store{ex: ex} }

// SavePlacement inserts the first placement when expectedEpoch is zero, or
// advances the current placement only when expectedEpoch matches and the new
// epoch is greater. The database trigger is the final monotonicity backstop.
func (s Store) SavePlacement(ctx context.Context, tenantRef string, placement tenant.Placement, expectedEpoch uint64) error {
	tenantID, err := s.ready(tenantRef)
	if err != nil {
		return err
	}
	if err := placement.Validate(); err != nil {
		return err
	}
	signature, err := hex.DecodeString(placement.Signature)
	if err != nil || len(signature) != 32 {
		return invalid("placement signature must be a 32-byte hexadecimal signature")
	}
	if expectedEpoch == 0 {
		affected, err := s.ex.Exec(ctx, `
			INSERT INTO tenant_placement (
				tenant_id, cell, region, residency_profile, isolation_tier,
				epoch, signature)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (tenant_id) DO NOTHING`,
			tenantID, placement.Cell, placement.Region, placement.ResidencyProfile,
			placement.IsolationTier, int64(placement.Epoch), signature)
		if err != nil {
			return fmt.Errorf("tenantstore: insert placement for %s: %w", tenantID, err)
		}
		if affected == 0 {
			return duplicate("tenant placement already exists")
		}
		return nil
	}

	affected, err := s.ex.Exec(ctx, `
		UPDATE tenant_placement
		SET cell = $3, region = $4, residency_profile = $5, isolation_tier = $6,
			epoch = $7, signature = $8, updated_at = now()
		WHERE tenant_id = $1 AND epoch = $2 AND $7 > epoch`,
		tenantID, int64(expectedEpoch), placement.Cell, placement.Region,
		placement.ResidencyProfile, placement.IsolationTier, int64(placement.Epoch), signature)
	if err != nil {
		return fmt.Errorf("tenantstore: advance placement for %s: %w", tenantID, err)
	}
	if affected == 0 {
		return stale("placement epoch does not match the current fenced epoch")
	}
	return nil
}

// LoadPlacement returns the current placement for tenantID.
func (s Store) LoadPlacement(ctx context.Context, tenantRef string) (tenant.Placement, error) {
	tenantID, err := s.ready(tenantRef)
	if err != nil {
		return tenant.Placement{}, err
	}
	var (
		placement tenant.Placement
		signature []byte
		epoch     int64
	)
	err = s.ex.QueryRow(ctx, `
		SELECT cell, region, residency_profile, isolation_tier, epoch, signature
		FROM tenant_placement
		WHERE tenant_id = $1`, tenantID).Scan(
		&placement.Cell, &placement.Region, &placement.ResidencyProfile,
		&placement.IsolationTier, &epoch, &signature)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return tenant.Placement{}, notFound("tenant placement does not exist")
		}
		return tenant.Placement{}, fmt.Errorf("tenantstore: load placement for %s: %w", tenantID, err)
	}
	placement.Epoch = uint64(epoch)
	placement.Tenant = tenantID.String()
	placement.Signature = hex.EncodeToString(signature)
	return placement, nil
}

// AppendProvisioningEvent records one immutable provisioning fact at sequence.
func (s Store) AppendProvisioningEvent(ctx context.Context, tenantRef string, event tenant.ProvisioningEvent, sequence uint64) error {
	tenantID, err := s.ready(tenantRef)
	if err != nil {
		return err
	}
	if event.Tenant == "" || sequence == 0 {
		return invalid("event tenant and positive event sequence are required")
	}
	affected, err := s.ex.Exec(ctx, `
		INSERT INTO tenant_provisioning_event (
			row_id, tenant_id, kind, plane, verifier_principal, reason, at, event_sequence)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), $7, $8)
		ON CONFLICT DO NOTHING`,
		uuid.New(), tenantID, string(event.Kind), string(event.Plane),
		event.VerifierPrincipal, event.Reason, event.At.UTC(), int64(sequence))
	if err != nil {
		return fmt.Errorf("tenantstore: append provisioning event for %s: %w", tenantID, err)
	}
	if affected == 0 {
		return duplicate("provisioning event sequence already exists")
	}
	return nil
}

// ListProvisioningEvents returns the append-only evidence in sequence order.
func (s Store) ListProvisioningEvents(ctx context.Context, tenantRef string) ([]tenant.ProvisioningEventRecord, error) {
	tenantID, err := s.ready(tenantRef)
	if err != nil {
		return nil, err
	}
	rows, err := s.ex.Query(ctx, `
		SELECT tenant_id, event_sequence, kind, plane,
			COALESCE(verifier_principal, ''), COALESCE(reason, ''), at
		FROM tenant_provisioning_event
		WHERE tenant_id = $1
		ORDER BY event_sequence`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("tenantstore: list provisioning events for %s: %w", tenantID, err)
	}
	defer rows.Close()
	var result []tenant.ProvisioningEventRecord
	for rows.Next() {
		var (
			record   tenant.ProvisioningEventRecord
			sequence int64
			kind     string
			plane    string
		)
		if err := rows.Scan(&record.TenantID, &sequence, &kind, &plane,
			&record.Event.VerifierPrincipal, &record.Event.Reason, &record.Event.At); err != nil {
			return nil, fmt.Errorf("tenantstore: scan provisioning event for %s: %w", tenantID, err)
		}
		record.Sequence = uint64(sequence)
		record.Event.Tenant = tenantID.String()
		record.Event.Kind = tenant.ProvisioningEventKind(kind)
		record.Event.Plane = tenant.Plane(plane)
		record.Event.At = record.Event.At.UTC()
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tenantstore: list provisioning events for %s: %w", tenantID, err)
	}
	return result, nil
}

// SaveGovernmentAuthorizationProfile inserts one sealed immutable revision.
func (s Store) SaveGovernmentAuthorizationProfile(ctx context.Context, tenantRef string, profile govauth.GovernmentAuthorizationProfile) error {
	tenantID, err := s.ready(tenantRef)
	if err != nil {
		return err
	}
	sealed, err := sealProfile(tenantRef, profile)
	if err != nil {
		return err
	}
	programs, err := json.Marshal(sealed.ApplicablePrograms)
	if err != nil {
		return fmt.Errorf("tenantstore: encode profile programs: %w", err)
	}
	controls, err := json.Marshal(sealed.InheritedControls)
	if err != nil {
		return fmt.Errorf("tenantstore: encode profile controls: %w", err)
	}
	evidence, err := json.Marshal(sealed.Evidence)
	if err != nil {
		return fmt.Errorf("tenantstore: encode profile evidence: %w", err)
	}
	fedramp, err := json.Marshal(sealed.FedRAMP)
	if err != nil {
		return fmt.Errorf("tenantstore: encode profile FedRAMP: %w", err)
	}
	cms, err := json.Marshal(sealed.CMS)
	if err != nil {
		return fmt.Errorf("tenantstore: encode profile CMS: %w", err)
	}
	answers, err := json.Marshal(sealed.ProcurementAnswers)
	if err != nil {
		return fmt.Errorf("tenantstore: encode profile procurement answers: %w", err)
	}
	affected, err := s.ex.Exec(ctx, `
		INSERT INTO government_authorization_profile (
			row_id, tenant_id, schema_version, revision, integration_id,
			applicable_programs, system_boundary, inherited_controls, evidence,
			assessor_status, review_date, fedramp, cms, procurement_answers, revision_digest)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8::jsonb, $9::jsonb,
			$10, $11, $12::jsonb, $13::jsonb, $14::jsonb, $15)
		ON CONFLICT DO NOTHING`,
		uuid.New(), tenantID, strconv.Itoa(sealed.SchemaVersion), int64(sealed.Revision),
		sealed.IntegrationID, string(programs), jsonString(sealed.SystemBoundary), string(controls),
		string(evidence), string(sealed.AssessorStatus), sealed.ReviewDate.UTC(), string(fedramp),
		string(cms), string(answers), sealed.RevisionDigest)
	if err != nil {
		return fmt.Errorf("tenantstore: insert government authorization profile for %s: %w", tenantID, err)
	}
	if affected == 0 {
		return duplicate("government authorization profile revision already exists")
	}
	return nil
}

// LoadGovernmentAuthorizationProfile loads one exact immutable revision.
func (s Store) LoadGovernmentAuthorizationProfile(ctx context.Context, tenantRef string, integrationID string, revision uint64) (govauth.GovernmentAuthorizationProfile, error) {
	tenantID, err := s.ready(tenantRef)
	if err != nil {
		return govauth.GovernmentAuthorizationProfile{}, err
	}
	if strings.TrimSpace(integrationID) == "" || revision == 0 {
		return govauth.GovernmentAuthorizationProfile{}, invalid("integration id and positive revision are required")
	}
	return s.loadProfile(ctx, tenantID, integrationID, revision, false)
}

// LatestGovernmentAuthorizationProfile loads the highest stored revision for
// one tenant/integration.
func (s Store) LatestGovernmentAuthorizationProfile(ctx context.Context, tenantRef string, integrationID string) (govauth.GovernmentAuthorizationProfile, error) {
	tenantID, err := s.ready(tenantRef)
	if err != nil {
		return govauth.GovernmentAuthorizationProfile{}, err
	}
	if strings.TrimSpace(integrationID) == "" {
		return govauth.GovernmentAuthorizationProfile{}, invalid("integration id is required")
	}
	return s.loadProfile(ctx, tenantID, integrationID, 0, true)
}

func (s Store) loadProfile(ctx context.Context, tenantID uuid.UUID, integrationID string, revision uint64, latest bool) (govauth.GovernmentAuthorizationProfile, error) {
	query := `
		SELECT schema_version, revision, integration_id,
			COALESCE(applicable_programs::text, 'null'), COALESCE(system_boundary::text, 'null'),
			COALESCE(inherited_controls::text, 'null'), COALESCE(evidence::text, 'null'),
			assessor_status, review_date, COALESCE(fedramp::text, 'null'),
			COALESCE(cms::text, 'null'), COALESCE(procurement_answers::text, 'null'), revision_digest
		FROM government_authorization_profile
		WHERE tenant_id = $1 AND integration_id = $2`
	args := []any{tenantID, integrationID}
	if latest {
		query += ` ORDER BY revision DESC LIMIT 1`
	} else {
		query += ` AND revision = $3`
		args = append(args, int64(revision))
	}
	var (
		schemaVersion                          string
		storedRevision                         int64
		storedIntegration                      string
		programs, boundary, controls, evidence string
		status                                 govauth.AssessorStatus
		reviewDate                             time.Time
		fedramp, cms, answers                  string
		digest                                 string
	)
	if err := s.ex.QueryRow(ctx, query, args...).Scan(
		&schemaVersion, &storedRevision, &storedIntegration, &programs, &boundary,
		&controls, &evidence, &status, &reviewDate, &fedramp, &cms, &answers, &digest); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return govauth.GovernmentAuthorizationProfile{}, notFound("government authorization profile revision does not exist")
		}
		return govauth.GovernmentAuthorizationProfile{}, fmt.Errorf("tenantstore: load government authorization profile for %s: %w", tenantID, err)
	}
	schema, err := strconv.Atoi(schemaVersion)
	if err != nil {
		return govauth.GovernmentAuthorizationProfile{}, fmt.Errorf("tenantstore: decode profile schema version: %w", err)
	}
	return decodeProfile(schema, storedRevision, storedIntegration, programs, boundary,
		controls, evidence, status, reviewDate, fedramp, cms, answers, digest, tenantID)
}

func decodeProfile(schema int, revision int64, integrationID, programs, boundary, controls, evidence string,
	status govauth.AssessorStatus, reviewDate time.Time, fedramp, cms, answers, digest string, tenantID uuid.UUID,
) (govauth.GovernmentAuthorizationProfile, error) {
	if reviewDate.IsZero() {
		return govauth.GovernmentAuthorizationProfile{}, invalid("profile review date is invalid")
	}
	var profile govauth.GovernmentAuthorizationProfile
	profile.SchemaVersion = schema
	profile.Revision = uint64(revision)
	profile.TenantID = tenantID.String()
	profile.IntegrationID = integrationID
	profile.AssessorStatus = status
	profile.ReviewDate = reviewDate.UTC()
	if err := json.Unmarshal([]byte(programs), &profile.ApplicablePrograms); err != nil {
		return govauth.GovernmentAuthorizationProfile{}, fmt.Errorf("tenantstore: decode profile programs: %w", err)
	}
	if err := json.Unmarshal([]byte(boundary), &profile.SystemBoundary); err != nil {
		return govauth.GovernmentAuthorizationProfile{}, fmt.Errorf("tenantstore: decode profile system boundary: %w", err)
	}
	if err := json.Unmarshal([]byte(controls), &profile.InheritedControls); err != nil {
		return govauth.GovernmentAuthorizationProfile{}, fmt.Errorf("tenantstore: decode profile controls: %w", err)
	}
	if err := json.Unmarshal([]byte(evidence), &profile.Evidence); err != nil {
		return govauth.GovernmentAuthorizationProfile{}, fmt.Errorf("tenantstore: decode profile evidence: %w", err)
	}
	if err := json.Unmarshal([]byte(fedramp), &profile.FedRAMP); err != nil {
		return govauth.GovernmentAuthorizationProfile{}, fmt.Errorf("tenantstore: decode profile FedRAMP: %w", err)
	}
	if err := json.Unmarshal([]byte(cms), &profile.CMS); err != nil {
		return govauth.GovernmentAuthorizationProfile{}, fmt.Errorf("tenantstore: decode profile CMS: %w", err)
	}
	if err := json.Unmarshal([]byte(answers), &profile.ProcurementAnswers); err != nil {
		return govauth.GovernmentAuthorizationProfile{}, fmt.Errorf("tenantstore: decode profile procurement answers: %w", err)
	}
	profile.RevisionDigest = digest
	sealed, err := govauth.NewGovernmentAuthorizationProfile(profile)
	if err != nil {
		return govauth.GovernmentAuthorizationProfile{}, fmt.Errorf("tenantstore: validate loaded profile: %w", err)
	}
	return sealed, nil
}

func (s Store) ready(tenantRef string) (uuid.UUID, error) {
	if s.ex == nil {
		return uuid.Nil, invalid("store has no executor")
	}
	if strings.TrimSpace(tenantRef) == "" {
		return uuid.Nil, invalid("tenant id is required")
	}
	tenantID, err := uuid.Parse(tenantRef)
	if err != nil || tenantID == uuid.Nil {
		return uuid.Nil, invalid("tenant id must be a non-nil UUID")
	}
	return tenantID, nil
}

func sealProfile(tenantRef string, profile govauth.GovernmentAuthorizationProfile) (govauth.GovernmentAuthorizationProfile, error) {
	if profile.TenantID != tenantRef {
		return govauth.GovernmentAuthorizationProfile{}, invalid("profile tenant_id must match the storage tenant")
	}
	if profile.Revision == 0 {
		profile.Revision = profile.Version
	}
	if profile.IntegrationID == "" {
		profile.IntegrationID = profile.IntegrationRef
	}
	if len(profile.ApplicablePrograms) == 0 {
		profile.ApplicablePrograms = append([]govauth.Program(nil), profile.Programs...)
	}
	if len(profile.InheritedControls) == 0 {
		profile.InheritedControls = append([]govauth.ControlInheritanceReference(nil), profile.InheritedControlReferences...)
	}
	if len(profile.Evidence) == 0 {
		profile.Evidence = append([]govauth.EvidencePointer(nil), profile.EvidenceCatalog...)
	}
	if profile.FedRAMP == nil {
		profile.FedRAMP = profile.FedRAMPPath
	}
	if profile.CMS == nil {
		profile.CMS = profile.MARSEResources
	}
	if !profile.ReviewDate.IsZero() && (profile.ReviewDate.Hour() != 0 || profile.ReviewDate.Minute() != 0 || profile.ReviewDate.Second() != 0 || profile.ReviewDate.Nanosecond() != 0) {
		return govauth.GovernmentAuthorizationProfile{}, invalid("profile review_date must be midnight because storage preserves a date")
	}
	sealed, err := govauth.NewGovernmentAuthorizationProfile(profile)
	if err != nil {
		return govauth.GovernmentAuthorizationProfile{}, err
	}
	return sealed, nil
}

func jsonString(value string) string {
	b, _ := json.Marshal(value)
	return string(b)
}

func invalid(detail string) error   { return &tenant.StoreError{Code: CodeInvalid, Detail: detail} }
func duplicate(detail string) error { return &tenant.StoreError{Code: CodeDuplicate, Detail: detail} }
func notFound(detail string) error  { return &tenant.StoreError{Code: CodeNotFound, Detail: detail} }
func stale(detail string) error     { return &tenant.StoreError{Code: CodeStaleCAS, Detail: detail} }

// CodeOf exposes the stable code without requiring callers to import the
// domain package that owns the repository port.
func CodeOf(err error) ErrorCode { return tenant.CodeOf(err) }

var _ tenant.Repository = Store{}
