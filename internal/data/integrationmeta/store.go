package integrationmeta

import (
	"context"
	"encoding/json"
	"regexp"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
)

// credentialRefPattern mirrors the schema's
// connector_connection_credential_is_reference constraint.
var credentialRefPattern = regexp.MustCompile(`^(vault|kms|secretref|keyring)://\S+$`)

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

// ---------------------------------------------------------------------------
// external_system
// ---------------------------------------------------------------------------

// ExternalSystem is one external product a tenant integrates with.
type ExternalSystem struct {
	TenantID           uuid.UUID `json:"tenant_id"`
	SystemID           uuid.UUID `json:"system_id"`
	SystemKey          string    `json:"system_key"`
	Vendor             string    `json:"vendor"`
	Product            string    `json:"product"`
	Environment        string    `json:"environment"`
	ResidencyRegion    string    `json:"residency_region"`
	DataClassification string    `json:"data_classification"`
	OwnerPrincipalRef  string    `json:"owner_principal_ref"`
	Lifecycle          string    `json:"lifecycle"`
	Health             string    `json:"health"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func (s ExternalSystem) Validate() error {
	if s.TenantID == uuid.Nil || s.SystemID == uuid.Nil {
		return ErrNilTenant
	}
	if s.SystemKey == "" {
		return detail(ErrMissingCorrelation, "external_system.system_key")
	}
	if s.DataClassification == "" {
		return detail(ErrMissingClassification, "external_system.data_classification")
	}
	if !oneOf(s.Environment, "SANDBOX", "TEST", "STAGING", "PRODUCTION") {
		return detail(ErrInvalidEnum, "external_system.environment=%q", s.Environment)
	}
	return nil
}

func InsertExternalSystem(ctx context.Context, tx dbport.Tx, s ExternalSystem) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, s.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO external_system (tenant_id, system_id, system_key, vendor, product, environment, residency_region, data_classification, owner_principal_ref, lifecycle, health, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		s.TenantID, s.SystemID, s.SystemKey, s.Vendor, s.Product, s.Environment, s.ResidencyRegion, s.DataClassification, s.OwnerPrincipalRef, s.Lifecycle, s.Health, s.CreatedAt, s.UpdatedAt)
	return err
}

func LoadExternalSystem(ctx context.Context, q dbport.Querier, tenantID, systemID uuid.UUID) (ExternalSystem, error) {
	var s ExternalSystem
	err := q.QueryRow(ctx, `SELECT tenant_id, system_id, system_key, vendor, product, environment, residency_region, data_classification, owner_principal_ref, lifecycle, health, created_at, updated_at FROM external_system WHERE tenant_id=$1 AND system_id=$2`, tenantID, systemID).
		Scan(&s.TenantID, &s.SystemID, &s.SystemKey, &s.Vendor, &s.Product, &s.Environment, &s.ResidencyRegion, &s.DataClassification, &s.OwnerPrincipalRef, &s.Lifecycle, &s.Health, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return ExternalSystem{}, err
	}
	return s, nil
}

// ---------------------------------------------------------------------------
// connector_definition
// ---------------------------------------------------------------------------

// ConnectorDefinition is one versioned connector contract.
type ConnectorDefinition struct {
	TenantID             uuid.UUID       `json:"tenant_id"`
	ConnectorID          uuid.UUID       `json:"connector_id"`
	ConnectorKey         string          `json:"connector_key"`
	ConnectorVersion     int64           `json:"connector_version"`
	Vendor               string          `json:"vendor"`
	SupportedObjects     json.RawMessage `json:"supported_objects"`
	Capabilities         json.RawMessage `json:"capabilities"`
	AuthMode             string          `json:"auth_mode"`
	WriteMode            string          `json:"write_mode"`
	IdempotencySemantics string          `json:"idempotency_semantics"`
	ObservationSemantics string          `json:"observation_semantics"`
	DescriptorDigest     string          `json:"descriptor_digest"`
	Status               string          `json:"status"`
	CreatedAt            time.Time       `json:"created_at"`
}

func (c ConnectorDefinition) Validate() error {
	if c.TenantID == uuid.Nil || c.ConnectorID == uuid.Nil {
		return ErrNilTenant
	}
	if c.ConnectorVersion < 1 {
		return detail(ErrMissingVersion, "connector_definition.connector_version")
	}
	if c.DescriptorDigest == "" {
		return detail(ErrMissingDigest, "connector_definition.descriptor_digest")
	}
	if !oneOf(c.AuthMode, "OAUTH2", "API_KEY", "MTLS", "SAML", "BASIC", "NONE") {
		return detail(ErrInvalidEnum, "connector_definition.auth_mode=%q", c.AuthMode)
	}
	if !oneOf(c.WriteMode, "NONE", "IDEMPOTENT", "AT_LEAST_ONCE", "TRANSACTIONAL") {
		return detail(ErrInvalidEnum, "connector_definition.write_mode=%q", c.WriteMode)
	}
	return nil
}

func InsertConnectorDefinition(ctx context.Context, tx dbport.Tx, c ConnectorDefinition) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, c.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO connector_definition (tenant_id, connector_id, connector_key, connector_version, vendor, supported_objects, capabilities, auth_mode, write_mode, idempotency_semantics, observation_semantics, descriptor_digest, status, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		c.TenantID, c.ConnectorID, c.ConnectorKey, c.ConnectorVersion, c.Vendor, object(c.SupportedObjects), object(c.Capabilities), c.AuthMode, c.WriteMode, c.IdempotencySemantics, c.ObservationSemantics, c.DescriptorDigest, c.Status, c.CreatedAt)
	return err
}

func LoadConnectorDefinition(ctx context.Context, q dbport.Querier, tenantID, connectorID uuid.UUID) (ConnectorDefinition, error) {
	var c ConnectorDefinition
	var objects, caps []byte
	err := q.QueryRow(ctx, `SELECT tenant_id, connector_id, connector_key, connector_version, vendor, supported_objects, capabilities, auth_mode, write_mode, idempotency_semantics, observation_semantics, descriptor_digest, status, created_at FROM connector_definition WHERE tenant_id=$1 AND connector_id=$2`, tenantID, connectorID).
		Scan(&c.TenantID, &c.ConnectorID, &c.ConnectorKey, &c.ConnectorVersion, &c.Vendor, &objects, &caps, &c.AuthMode, &c.WriteMode, &c.IdempotencySemantics, &c.ObservationSemantics, &c.DescriptorDigest, &c.Status, &c.CreatedAt)
	if err != nil {
		return ConnectorDefinition{}, err
	}
	c.SupportedObjects = objects
	c.Capabilities = caps
	return c, nil
}

// ---------------------------------------------------------------------------
// connector_connection
// ---------------------------------------------------------------------------

// ConnectorConnection binds a connector to an external system for one tenant.
// CredentialRef names a governed secret store entry; the secret itself never
// reaches this struct.
type ConnectorConnection struct {
	TenantID        uuid.UUID       `json:"tenant_id"`
	ConnectionID    uuid.UUID       `json:"connection_id"`
	SystemID        uuid.UUID       `json:"system_id"`
	ConnectorID     uuid.UUID       `json:"connector_id"`
	OrgScopeID      *uuid.UUID      `json:"org_scope_id"`
	Environment     string          `json:"environment"`
	Endpoint        string          `json:"endpoint"`
	CredentialRef   string          `json:"credential_ref"`
	ResidencyRegion string          `json:"residency_region"`
	Configuration   json.RawMessage `json:"configuration"`
	Generation      int64           `json:"generation"`
	Lifecycle       string          `json:"lifecycle"`
	Health          string          `json:"health"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// Validate refuses a raw secret in either place it could hide: the credential
// reference itself, and the configuration object beside it.
func (c ConnectorConnection) Validate() error {
	if c.TenantID == uuid.Nil || c.ConnectionID == uuid.Nil {
		return ErrNilTenant
	}
	if !credentialRefPattern.MatchString(c.CredentialRef) {
		return detail(ErrRawSecret, "connector_connection.credential_ref=%q is not a vault://, kms://, secretref:// or keyring:// reference", c.CredentialRef)
	}
	if c.Generation < 1 {
		return detail(ErrMissingVersion, "connector_connection.generation")
	}
	if !oneOf(c.Lifecycle, "DRAFT", "VALIDATING", "READY", "ACTIVE", "DEGRADED", "SUSPENDED", "REVOKED", "QUARANTINED") {
		return detail(ErrInvalidEnum, "connector_connection.lifecycle=%q", c.Lifecycle)
	}
	if len(c.Configuration) > 0 {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(c.Configuration, &fields); err != nil {
			return detail(ErrInvalidEnum, "connector_connection.configuration is not a JSON object: %v", err)
		}
		for _, key := range secretKeys {
			if _, found := fields[key]; found {
				return detail(ErrRawSecret, "connector_connection.configuration carries key %q", key)
			}
		}
	}
	return nil
}

func InsertConnectorConnection(ctx context.Context, tx dbport.Tx, c ConnectorConnection) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, c.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO connector_connection (tenant_id, connection_id, system_id, connector_id, org_scope_id, environment, endpoint, credential_ref, residency_region, configuration, generation, lifecycle, health, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		c.TenantID, c.ConnectionID, c.SystemID, c.ConnectorID, c.OrgScopeID, c.Environment, c.Endpoint, c.CredentialRef, c.ResidencyRegion, object(c.Configuration), c.Generation, c.Lifecycle, c.Health, c.CreatedAt, c.UpdatedAt)
	return err
}

func LoadConnectorConnection(ctx context.Context, q dbport.Querier, tenantID, connectionID uuid.UUID) (ConnectorConnection, error) {
	var c ConnectorConnection
	var org *uuid.UUID
	var cfg []byte
	err := q.QueryRow(ctx, `SELECT tenant_id, connection_id, system_id, connector_id, org_scope_id, environment, endpoint, credential_ref, residency_region, configuration, generation, lifecycle, health, created_at, updated_at FROM connector_connection WHERE tenant_id=$1 AND connection_id=$2`, tenantID, connectionID).
		Scan(&c.TenantID, &c.ConnectionID, &c.SystemID, &c.ConnectorID, &org, &c.Environment, &c.Endpoint, &c.CredentialRef, &c.ResidencyRegion, &cfg, &c.Generation, &c.Lifecycle, &c.Health, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return ConnectorConnection{}, err
	}
	c.OrgScopeID = org
	c.Configuration = cfg
	return c, nil
}

// ---------------------------------------------------------------------------
// mapping_profile and mapping_execution
// ---------------------------------------------------------------------------

// MappingProfile is one versioned, published field-mapping contract.
type MappingProfile struct {
	TenantID             uuid.UUID       `json:"tenant_id"`
	MappingID            uuid.UUID       `json:"mapping_id"`
	MappingKey           string          `json:"mapping_key"`
	MappingVersion       int64           `json:"mapping_version"`
	ConnectionID         *uuid.UUID      `json:"connection_id"`
	SourceSchemaKey      string          `json:"source_schema_key"`
	DestinationSchemaKey string          `json:"destination_schema_key"`
	TransformLanguage    string          `json:"transform_language"`
	TransformVersion     int64           `json:"transform_version"`
	FieldRules           json.RawMessage `json:"field_rules"`
	LossinessPolicy      string          `json:"lossiness_policy"`
	ContentDigest        string          `json:"content_digest"`
	PublicationState     string          `json:"publication_state"`
	EffectiveFrom        time.Time       `json:"effective_from"`
	EffectiveTo          *time.Time      `json:"effective_to"`
	CreatedAt            time.Time       `json:"created_at"`
}

func (m MappingProfile) Validate() error {
	if m.TenantID == uuid.Nil || m.MappingID == uuid.Nil {
		return ErrNilTenant
	}
	if m.MappingVersion < 1 || m.TransformVersion < 1 {
		return detail(ErrMissingVersion, "mapping_profile.mapping_version/transform_version")
	}
	if m.ContentDigest == "" {
		return detail(ErrMissingDigest, "mapping_profile.content_digest")
	}
	if m.EffectiveTo != nil && !m.EffectiveTo.After(m.EffectiveFrom) {
		return detail(ErrInvalidInterval, "mapping_profile effective interval is not half-open")
	}
	if !oneOf(m.LossinessPolicy, "LOSSLESS", "LOSSY_DECLARED", "LOSSY_REJECTED") {
		return detail(ErrInvalidEnum, "mapping_profile.lossiness_policy=%q", m.LossinessPolicy)
	}
	return nil
}

func InsertMappingProfile(ctx context.Context, tx dbport.Tx, m MappingProfile) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, m.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO mapping_profile (tenant_id, mapping_id, mapping_key, mapping_version, connection_id, source_schema_key, destination_schema_key, transform_language, transform_version, field_rules, lossiness_policy, content_digest, publication_state, effective_from, effective_to, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		m.TenantID, m.MappingID, m.MappingKey, m.MappingVersion, m.ConnectionID, m.SourceSchemaKey, m.DestinationSchemaKey, m.TransformLanguage, m.TransformVersion, object(m.FieldRules), m.LossinessPolicy, m.ContentDigest, m.PublicationState, m.EffectiveFrom, m.EffectiveTo, m.CreatedAt)
	return err
}

func LoadMappingProfile(ctx context.Context, q dbport.Querier, tenantID, mappingID uuid.UUID) (MappingProfile, error) {
	var m MappingProfile
	var conn *uuid.UUID
	var to *time.Time
	var rules []byte
	err := q.QueryRow(ctx, `SELECT tenant_id, mapping_id, mapping_key, mapping_version, connection_id, source_schema_key, destination_schema_key, transform_language, transform_version, field_rules, lossiness_policy, content_digest, publication_state, effective_from, effective_to, created_at FROM mapping_profile WHERE tenant_id=$1 AND mapping_id=$2`, tenantID, mappingID).
		Scan(&m.TenantID, &m.MappingID, &m.MappingKey, &m.MappingVersion, &conn, &m.SourceSchemaKey, &m.DestinationSchemaKey, &m.TransformLanguage, &m.TransformVersion, &rules, &m.LossinessPolicy, &m.ContentDigest, &m.PublicationState, &m.EffectiveFrom, &to, &m.CreatedAt)
	if err != nil {
		return MappingProfile{}, err
	}
	m.ConnectionID = conn
	m.EffectiveTo = to
	m.FieldRules = rules
	return m, nil
}

// MappingExecution is the immutable record of applying one mapping version to
// one receipt item.
type MappingExecution struct {
	TenantID            uuid.UUID       `json:"tenant_id"`
	ExecutionID         uuid.UUID       `json:"execution_id"`
	MappingID           uuid.UUID       `json:"mapping_id"`
	MappingVersion      int64           `json:"mapping_version"`
	ItemID              uuid.UUID       `json:"item_id"`
	InputDigest         string          `json:"input_digest"`
	OutputDigest        string          `json:"output_digest"`
	FieldResults        json.RawMessage `json:"field_results"`
	PresenceDiagnostics json.RawMessage `json:"presence_diagnostics"`
	Lossiness           string          `json:"lossiness"`
	Status              string          `json:"status"`
	ExecutedAt          time.Time       `json:"executed_at"`
}

func (m MappingExecution) Validate() error {
	if m.TenantID == uuid.Nil || m.ExecutionID == uuid.Nil || m.MappingID == uuid.Nil || m.ItemID == uuid.Nil {
		return ErrNilTenant
	}
	if m.MappingVersion < 1 {
		return detail(ErrMissingVersion, "mapping_execution.mapping_version")
	}
	if m.InputDigest == "" || m.OutputDigest == "" {
		return detail(ErrMissingDigest, "mapping_execution input/output digest")
	}
	if !oneOf(m.Status, "APPLIED", "QUARANTINED", "FAILED") {
		return detail(ErrInvalidEnum, "mapping_execution.status=%q", m.Status)
	}
	return nil
}

func InsertMappingExecution(ctx context.Context, tx dbport.Tx, m MappingExecution) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, m.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO mapping_execution (tenant_id, execution_id, mapping_id, mapping_version, item_id, input_digest, output_digest, field_results, presence_diagnostics, lossiness, status, executed_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		m.TenantID, m.ExecutionID, m.MappingID, m.MappingVersion, m.ItemID, m.InputDigest, m.OutputDigest, object(m.FieldResults), object(m.PresenceDiagnostics), m.Lossiness, m.Status, m.ExecutedAt)
	return err
}

func LoadMappingExecution(ctx context.Context, q dbport.Querier, tenantID, executionID uuid.UUID) (MappingExecution, error) {
	var m MappingExecution
	var results, presence []byte
	err := q.QueryRow(ctx, `SELECT tenant_id, execution_id, mapping_id, mapping_version, item_id, input_digest, output_digest, field_results, presence_diagnostics, lossiness, status, executed_at FROM mapping_execution WHERE tenant_id=$1 AND execution_id=$2`, tenantID, executionID).
		Scan(&m.TenantID, &m.ExecutionID, &m.MappingID, &m.MappingVersion, &m.ItemID, &m.InputDigest, &m.OutputDigest, &results, &presence, &m.Lossiness, &m.Status, &m.ExecutedAt)
	if err != nil {
		return MappingExecution{}, err
	}
	m.FieldResults = results
	m.PresenceDiagnostics = presence
	return m, nil
}

// ---------------------------------------------------------------------------
// integration_receipt and integration_receipt_item
// ---------------------------------------------------------------------------

// IntegrationReceipt is the immutable record of one authenticated inbound
// payload. DedupeKey is unique per connection.
type IntegrationReceipt struct {
	TenantID            uuid.UUID `json:"tenant_id"`
	ReceiptID           uuid.UUID `json:"receipt_id"`
	ConnectionID        uuid.UUID `json:"connection_id"`
	ProviderEventID     string    `json:"provider_event_id"`
	DedupeKey           string    `json:"dedupe_key"`
	CorrelationKey      string    `json:"correlation_key"`
	TrustProfileVersion int64     `json:"trust_profile_version"`
	AuthResult          string    `json:"auth_result"`
	SignatureResult     string    `json:"signature_result"`
	ReplayDisposition   string    `json:"replay_disposition"`
	RawArtifactDigest   string    `json:"raw_artifact_digest"`
	SchemaKey           string    `json:"schema_key"`
	ParserVersion       int64     `json:"parser_version"`
	Classification      string    `json:"classification"`
	ReceivedAt          time.Time `json:"received_at"`
	TrustedAt           time.Time `json:"trusted_at"`
	Disposition         string    `json:"disposition"`
}

func (r IntegrationReceipt) Validate() error {
	if r.TenantID == uuid.Nil || r.ReceiptID == uuid.Nil || r.ConnectionID == uuid.Nil {
		return ErrNilTenant
	}
	if r.DedupeKey == "" || r.CorrelationKey == "" {
		return detail(ErrMissingCorrelation, "integration_receipt dedupe_key/correlation_key")
	}
	if r.TrustProfileVersion < 1 || r.ParserVersion < 1 {
		return detail(ErrMissingVersion, "integration_receipt trust_profile_version/parser_version")
	}
	if r.RawArtifactDigest == "" {
		return detail(ErrMissingDigest, "integration_receipt.raw_artifact_digest")
	}
	if r.Classification == "" {
		return detail(ErrMissingClassification, "integration_receipt.classification")
	}
	if !oneOf(r.Disposition, "ACCEPTED", "QUARANTINED", "REJECTED") {
		return detail(ErrInvalidEnum, "integration_receipt.disposition=%q", r.Disposition)
	}
	if r.Disposition == "ACCEPTED" && (r.AuthResult != "AUTHENTICATED" || r.SignatureResult != "VALID") {
		return detail(ErrInvalidEnum, "integration_receipt accepted while auth=%q signature=%q", r.AuthResult, r.SignatureResult)
	}
	if r.TrustedAt.Before(r.ReceivedAt) {
		return detail(ErrInvalidInterval, "integration_receipt.trusted_at precedes received_at")
	}
	return nil
}

func InsertIntegrationReceipt(ctx context.Context, tx dbport.Tx, r IntegrationReceipt) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, r.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO integration_receipt (tenant_id, receipt_id, connection_id, provider_event_id, dedupe_key, correlation_key, trust_profile_version, auth_result, signature_result, replay_disposition, raw_artifact_digest, schema_key, parser_version, classification, received_at, trusted_at, disposition) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		r.TenantID, r.ReceiptID, r.ConnectionID, r.ProviderEventID, r.DedupeKey, r.CorrelationKey, r.TrustProfileVersion, r.AuthResult, r.SignatureResult, r.ReplayDisposition, r.RawArtifactDigest, r.SchemaKey, r.ParserVersion, r.Classification, r.ReceivedAt, r.TrustedAt, r.Disposition)
	return err
}

func LoadIntegrationReceipt(ctx context.Context, q dbport.Querier, tenantID, receiptID uuid.UUID) (IntegrationReceipt, error) {
	var r IntegrationReceipt
	err := q.QueryRow(ctx, `SELECT tenant_id, receipt_id, connection_id, provider_event_id, dedupe_key, correlation_key, trust_profile_version, auth_result, signature_result, replay_disposition, raw_artifact_digest, schema_key, parser_version, classification, received_at, trusted_at, disposition FROM integration_receipt WHERE tenant_id=$1 AND receipt_id=$2`, tenantID, receiptID).
		Scan(&r.TenantID, &r.ReceiptID, &r.ConnectionID, &r.ProviderEventID, &r.DedupeKey, &r.CorrelationKey, &r.TrustProfileVersion, &r.AuthResult, &r.SignatureResult, &r.ReplayDisposition, &r.RawArtifactDigest, &r.SchemaKey, &r.ParserVersion, &r.Classification, &r.ReceivedAt, &r.TrustedAt, &r.Disposition)
	if err != nil {
		return IntegrationReceipt{}, err
	}
	return r, nil
}

// IntegrationReceiptItem is one addressable element of a receipt payload.
type IntegrationReceiptItem struct {
	TenantID       uuid.UUID  `json:"tenant_id"`
	ItemID         uuid.UUID  `json:"item_id"`
	ReceiptID      uuid.UUID  `json:"receipt_id"`
	ItemIndex      int32      `json:"item_index"`
	ItemPath       string     `json:"item_path"`
	PayloadDigest  string     `json:"payload_digest"`
	SchemaResult   string     `json:"schema_result"`
	CorrelationKey string     `json:"correlation_key"`
	MappingID      *uuid.UUID `json:"mapping_id"`
	Disposition    string     `json:"disposition"`
	ErrorReason    string     `json:"error_reason"`
	RecordedAt     time.Time  `json:"recorded_at"`
}

func (i IntegrationReceiptItem) Validate() error {
	if i.TenantID == uuid.Nil || i.ItemID == uuid.Nil || i.ReceiptID == uuid.Nil {
		return ErrNilTenant
	}
	if i.ItemIndex < 0 {
		return detail(ErrInvalidEnum, "integration_receipt_item.item_index=%d", i.ItemIndex)
	}
	if i.CorrelationKey == "" {
		return detail(ErrMissingCorrelation, "integration_receipt_item.correlation_key")
	}
	if i.PayloadDigest == "" {
		return detail(ErrMissingDigest, "integration_receipt_item.payload_digest")
	}
	if !oneOf(i.Disposition, "MAPPED", "QUARANTINED", "REJECTED", "DEFERRED") {
		return detail(ErrInvalidEnum, "integration_receipt_item.disposition=%q", i.Disposition)
	}
	if i.Disposition == "REJECTED" && i.ErrorReason == "" {
		return detail(ErrInvalidEnum, "integration_receipt_item rejected without a reason")
	}
	return nil
}

func InsertIntegrationReceiptItem(ctx context.Context, tx dbport.Tx, i IntegrationReceiptItem) error {
	if err := i.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, i.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO integration_receipt_item (tenant_id, item_id, receipt_id, item_index, item_path, payload_digest, schema_result, correlation_key, mapping_id, disposition, error_reason, recorded_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		i.TenantID, i.ItemID, i.ReceiptID, i.ItemIndex, i.ItemPath, i.PayloadDigest, i.SchemaResult, i.CorrelationKey, i.MappingID, i.Disposition, i.ErrorReason, i.RecordedAt)
	return err
}

func LoadIntegrationReceiptItem(ctx context.Context, q dbport.Querier, tenantID, itemID uuid.UUID) (IntegrationReceiptItem, error) {
	var i IntegrationReceiptItem
	var mapping *uuid.UUID
	err := q.QueryRow(ctx, `SELECT tenant_id, item_id, receipt_id, item_index, item_path, payload_digest, schema_result, correlation_key, mapping_id, disposition, error_reason, recorded_at FROM integration_receipt_item WHERE tenant_id=$1 AND item_id=$2`, tenantID, itemID).
		Scan(&i.TenantID, &i.ItemID, &i.ReceiptID, &i.ItemIndex, &i.ItemPath, &i.PayloadDigest, &i.SchemaResult, &i.CorrelationKey, &mapping, &i.Disposition, &i.ErrorReason, &i.RecordedAt)
	if err != nil {
		return IntegrationReceiptItem{}, err
	}
	i.MappingID = mapping
	return i, nil
}

// ---------------------------------------------------------------------------
// connector_operation, its attempts and its observations
// ---------------------------------------------------------------------------

// ConnectorOperation is one node of the outbound effect graph.
// CompletionState only reaches COMPLETE through [CompleteOperation].
type ConnectorOperation struct {
	TenantID                uuid.UUID  `json:"tenant_id"`
	OperationID             uuid.UUID  `json:"operation_id"`
	ConnectionID            uuid.UUID  `json:"connection_id"`
	CausalPredecessorID     *uuid.UUID `json:"causal_predecessor_id"`
	SequenceNo              int64      `json:"sequence_no"`
	EffectRef               string     `json:"effect_ref"`
	WorkflowRef             string     `json:"workflow_ref"`
	SemanticOperation       string     `json:"semantic_operation"`
	ResourceKey             string     `json:"resource_key"`
	MappingID               *uuid.UUID `json:"mapping_id"`
	ExpectedExternalVersion *string    `json:"expected_external_version"`
	CanonicalPayloadDigest  string     `json:"canonical_payload_digest"`
	MappedPayloadDigest     string     `json:"mapped_payload_digest"`
	IdempotencyKey          string     `json:"idempotency_key"`
	FenceToken              int64      `json:"fence_token"`
	AuthorityDigest         string     `json:"authority_digest"`
	Classification          string     `json:"classification"`
	DeadlineAt              time.Time  `json:"deadline_at"`
	State                   string     `json:"state"`
	CompletionState         string     `json:"completion_state"`
	ObservationID           *uuid.UUID `json:"observation_id"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
}

func (o ConnectorOperation) Validate() error {
	if o.TenantID == uuid.Nil || o.OperationID == uuid.Nil || o.ConnectionID == uuid.Nil {
		return ErrNilTenant
	}
	if o.IdempotencyKey == "" {
		return detail(ErrMissingCorrelation, "connector_operation.idempotency_key")
	}
	if o.CanonicalPayloadDigest == "" || o.MappedPayloadDigest == "" || o.AuthorityDigest == "" {
		return detail(ErrMissingDigest, "connector_operation payload/authority digest")
	}
	if o.Classification == "" {
		return detail(ErrMissingClassification, "connector_operation.classification")
	}
	if o.DeadlineAt.IsZero() {
		return detail(ErrMissingDeadline, "connector_operation.deadline_at")
	}
	if o.FenceToken < 1 || o.SequenceNo < 1 {
		return detail(ErrMissingVersion, "connector_operation fence_token/sequence_no")
	}
	if !oneOf(o.State, "PLANNED", "SUBMITTED", "PROVIDER_ACCEPTED", "OBSERVED", "RECONCILED", "FAILED", "AMBIGUOUS") {
		return detail(ErrInvalidEnum, "connector_operation.state=%q", o.State)
	}
	if !oneOf(o.CompletionState, "PENDING", "COMPLETE", "ABANDONED") {
		return detail(ErrInvalidEnum, "connector_operation.completion_state=%q", o.CompletionState)
	}
	if o.CompletionState == "COMPLETE" && o.ObservationID == nil {
		return ErrProviderAcceptanceNotCompletion
	}
	if o.CausalPredecessorID != nil && *o.CausalPredecessorID == o.OperationID {
		return detail(ErrInvalidEnum, "connector_operation is its own causal predecessor")
	}
	return nil
}

func InsertConnectorOperation(ctx context.Context, tx dbport.Tx, o ConnectorOperation) error {
	if err := o.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, o.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO connector_operation (tenant_id, operation_id, connection_id, causal_predecessor_id, sequence_no, effect_ref, workflow_ref, semantic_operation, resource_key, mapping_id, expected_external_version, canonical_payload_digest, mapped_payload_digest, idempotency_key, fence_token, authority_digest, classification, deadline_at, state, completion_state, observation_id, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)`,
		o.TenantID, o.OperationID, o.ConnectionID, o.CausalPredecessorID, o.SequenceNo, o.EffectRef, o.WorkflowRef, o.SemanticOperation, o.ResourceKey, o.MappingID, o.ExpectedExternalVersion, o.CanonicalPayloadDigest, o.MappedPayloadDigest, o.IdempotencyKey, o.FenceToken, o.AuthorityDigest, o.Classification, o.DeadlineAt, o.State, o.CompletionState, o.ObservationID, o.CreatedAt, o.UpdatedAt)
	return err
}

func LoadConnectorOperation(ctx context.Context, q dbport.Querier, tenantID, operationID uuid.UUID) (ConnectorOperation, error) {
	var o ConnectorOperation
	var pred, mapping, obs *uuid.UUID
	var expected *string
	err := q.QueryRow(ctx, `SELECT tenant_id, operation_id, connection_id, causal_predecessor_id, sequence_no, effect_ref, workflow_ref, semantic_operation, resource_key, mapping_id, expected_external_version, canonical_payload_digest, mapped_payload_digest, idempotency_key, fence_token, authority_digest, classification, deadline_at, state, completion_state, observation_id, created_at, updated_at FROM connector_operation WHERE tenant_id=$1 AND operation_id=$2`, tenantID, operationID).
		Scan(&o.TenantID, &o.OperationID, &o.ConnectionID, &pred, &o.SequenceNo, &o.EffectRef, &o.WorkflowRef, &o.SemanticOperation, &o.ResourceKey, &mapping, &expected, &o.CanonicalPayloadDigest, &o.MappedPayloadDigest, &o.IdempotencyKey, &o.FenceToken, &o.AuthorityDigest, &o.Classification, &o.DeadlineAt, &o.State, &o.CompletionState, &obs, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return ConnectorOperation{}, err
	}
	o.CausalPredecessorID = pred
	o.MappingID = mapping
	o.ObservationID = obs
	o.ExpectedExternalVersion = expected
	return o, nil
}

// ConnectorOperationAttempt is one immutable provider round trip.
type ConnectorOperationAttempt struct {
	TenantID          uuid.UUID  `json:"tenant_id"`
	AttemptID         uuid.UUID  `json:"attempt_id"`
	OperationID       uuid.UUID  `json:"operation_id"`
	AttemptNumber     int32      `json:"attempt_number"`
	RequestDigest     string     `json:"request_digest"`
	ResponseDigest    *string    `json:"response_digest"`
	ProviderRequestID string     `json:"provider_request_id"`
	FenceToken        int64      `json:"fence_token"`
	AttemptedAt       time.Time  `json:"attempted_at"`
	ReceivedAt        *time.Time `json:"received_at"`
	ProviderResult    string     `json:"provider_result"`
	RetryDisposition  string     `json:"retry_disposition"`
}

func (a ConnectorOperationAttempt) Validate() error {
	if a.TenantID == uuid.Nil || a.AttemptID == uuid.Nil || a.OperationID == uuid.Nil {
		return ErrNilTenant
	}
	if a.AttemptNumber < 1 {
		return detail(ErrMissingVersion, "connector_operation_attempt.attempt_number")
	}
	if a.RequestDigest == "" {
		return detail(ErrMissingDigest, "connector_operation_attempt.request_digest")
	}
	if !oneOf(a.ProviderResult, "SUCCESS", "FAILURE", "PARTIAL", "UNKNOWN", "AMBIGUOUS", "PENDING") {
		return detail(ErrInvalidEnum, "connector_operation_attempt.provider_result=%q", a.ProviderResult)
	}
	if !oneOf(a.RetryDisposition, "NONE", "RETRY", "DEAD_LETTER", "OBSERVATION_REQUIRED", "REPAIR_REQUIRED") {
		return detail(ErrInvalidEnum, "connector_operation_attempt.retry_disposition=%q", a.RetryDisposition)
	}
	if oneOf(a.ProviderResult, "AMBIGUOUS", "UNKNOWN") && !oneOf(a.RetryDisposition, "OBSERVATION_REQUIRED", "REPAIR_REQUIRED", "DEAD_LETTER") {
		return detail(ErrProviderAcceptanceNotCompletion, "%s provider result routed to %s instead of observation or repair", a.ProviderResult, a.RetryDisposition)
	}
	if a.ReceivedAt != nil && a.ReceivedAt.Before(a.AttemptedAt) {
		return detail(ErrInvalidInterval, "connector_operation_attempt.received_at precedes attempted_at")
	}
	return nil
}

func InsertConnectorOperationAttempt(ctx context.Context, tx dbport.Tx, a ConnectorOperationAttempt) error {
	if err := a.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, a.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO connector_operation_attempt (tenant_id, attempt_id, operation_id, attempt_number, request_digest, response_digest, provider_request_id, fence_token, attempted_at, received_at, provider_result, retry_disposition) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		a.TenantID, a.AttemptID, a.OperationID, a.AttemptNumber, a.RequestDigest, a.ResponseDigest, a.ProviderRequestID, a.FenceToken, a.AttemptedAt, a.ReceivedAt, a.ProviderResult, a.RetryDisposition)
	return err
}

func LoadConnectorOperationAttempt(ctx context.Context, q dbport.Querier, tenantID, attemptID uuid.UUID) (ConnectorOperationAttempt, error) {
	var a ConnectorOperationAttempt
	var response *string
	var received *time.Time
	err := q.QueryRow(ctx, `SELECT tenant_id, attempt_id, operation_id, attempt_number, request_digest, response_digest, provider_request_id, fence_token, attempted_at, received_at, provider_result, retry_disposition FROM connector_operation_attempt WHERE tenant_id=$1 AND attempt_id=$2`, tenantID, attemptID).
		Scan(&a.TenantID, &a.AttemptID, &a.OperationID, &a.AttemptNumber, &a.RequestDigest, &response, &a.ProviderRequestID, &a.FenceToken, &a.AttemptedAt, &received, &a.ProviderResult, &a.RetryDisposition)
	if err != nil {
		return ConnectorOperationAttempt{}, err
	}
	a.ResponseDigest = response
	a.ReceivedAt = received
	return a, nil
}

// ExternalOperationObservation is what the external system was actually
// observed to hold after an operation, and how fresh that reading is.
type ExternalOperationObservation struct {
	TenantID          uuid.UUID       `json:"tenant_id"`
	ObservationID     uuid.UUID       `json:"observation_id"`
	OperationID       uuid.UUID       `json:"operation_id"`
	ResourceKey       string          `json:"resource_key"`
	ObservedState     json.RawMessage `json:"observed_state"`
	ObservedDigest    string          `json:"observed_digest"`
	ProviderVersion   string          `json:"provider_version"`
	Watermark         string          `json:"watermark"`
	Completeness      string          `json:"completeness"`
	Authority         string          `json:"authority"`
	ObservedAt        time.Time       `json:"observed_at"`
	ReceivedAt        time.Time       `json:"received_at"`
	FreshnessDeadline time.Time       `json:"freshness_deadline"`
}

func (o ExternalOperationObservation) Validate() error {
	if o.TenantID == uuid.Nil || o.ObservationID == uuid.Nil || o.OperationID == uuid.Nil {
		return ErrNilTenant
	}
	if o.ObservedDigest == "" {
		return detail(ErrMissingDigest, "external_operation_observation.observed_digest")
	}
	if !oneOf(o.Completeness, "CURRENT", "REVISION", "UNKNOWN", "INCOMPLETE", "STALE") {
		return detail(ErrInvalidEnum, "external_operation_observation.completeness=%q", o.Completeness)
	}
	if !oneOf(o.Authority, "AUTHORITATIVE", "ADVISORY", "UNTRUSTED") {
		return detail(ErrInvalidEnum, "external_operation_observation.authority=%q", o.Authority)
	}
	if o.FreshnessDeadline.IsZero() || !o.FreshnessDeadline.After(o.ObservedAt) {
		return detail(ErrMissingDeadline, "external_operation_observation.freshness_deadline")
	}
	if o.ReceivedAt.Before(o.ObservedAt) {
		return detail(ErrInvalidInterval, "external_operation_observation.received_at precedes observed_at")
	}
	return nil
}

func InsertExternalOperationObservation(ctx context.Context, tx dbport.Tx, o ExternalOperationObservation) error {
	if err := o.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, o.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO external_operation_observation (tenant_id, observation_id, operation_id, resource_key, observed_state, observed_digest, provider_version, watermark, completeness, authority, observed_at, received_at, freshness_deadline) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		o.TenantID, o.ObservationID, o.OperationID, o.ResourceKey, object(o.ObservedState), o.ObservedDigest, o.ProviderVersion, o.Watermark, o.Completeness, o.Authority, o.ObservedAt, o.ReceivedAt, o.FreshnessDeadline)
	return err
}

func LoadExternalOperationObservation(ctx context.Context, q dbport.Querier, tenantID, observationID uuid.UUID) (ExternalOperationObservation, error) {
	var o ExternalOperationObservation
	var state []byte
	err := q.QueryRow(ctx, `SELECT tenant_id, observation_id, operation_id, resource_key, observed_state, observed_digest, provider_version, watermark, completeness, authority, observed_at, received_at, freshness_deadline FROM external_operation_observation WHERE tenant_id=$1 AND observation_id=$2`, tenantID, observationID).
		Scan(&o.TenantID, &o.ObservationID, &o.OperationID, &o.ResourceKey, &state, &o.ObservedDigest, &o.ProviderVersion, &o.Watermark, &o.Completeness, &o.Authority, &o.ObservedAt, &o.ReceivedAt, &o.FreshnessDeadline)
	if err != nil {
		return ExternalOperationObservation{}, err
	}
	o.ObservedState = state
	return o, nil
}

// CompleteOperation is the only path to business completion. It refuses a nil
// observation outright, and its UPDATE names the observation so the schema's
// connector_operation_completion_requires_observation constraint would refuse
// it too if this check were ever removed.
func CompleteOperation(ctx context.Context, tx dbport.Tx, tenantID, operationID, observationID uuid.UUID, at time.Time) error {
	if tenantID == uuid.Nil || operationID == uuid.Nil {
		return ErrNilTenant
	}
	if observationID == uuid.Nil {
		return ErrProviderAcceptanceNotCompletion
	}
	if err := ensureTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	// The observation must belong to this operation: an observation of some
	// other operation is not evidence of this one.
	var owner uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT operation_id FROM external_operation_observation WHERE tenant_id=$1 AND observation_id=$2`, tenantID, observationID).Scan(&owner); err != nil {
		return err
	}
	if owner != operationID {
		return detail(ErrProviderAcceptanceNotCompletion, "observation %s belongs to operation %s", observationID, owner)
	}
	affected, err := tx.Exec(ctx, `UPDATE connector_operation SET completion_state='COMPLETE', observation_id=$3, state='RECONCILED', updated_at=$4 WHERE tenant_id=$1 AND operation_id=$2`, tenantID, operationID, observationID, at)
	if err != nil {
		return err
	}
	if affected == 0 {
		return dbport.ErrNoRows
	}
	return nil
}

// ---------------------------------------------------------------------------
// reconciliation_job and reconciliation_result
// ---------------------------------------------------------------------------

// ReconciliationJob is one expected-versus-observed sweep over a connection.
type ReconciliationJob struct {
	TenantID     uuid.UUID       `json:"tenant_id"`
	JobID        uuid.UUID       `json:"job_id"`
	ConnectionID uuid.UUID       `json:"connection_id"`
	ObjectKey    string          `json:"object_key"`
	Direction    string          `json:"direction"`
	Mode         string          `json:"mode"`
	CursorToken  string          `json:"cursor_token"`
	Watermark    string          `json:"watermark"`
	Counts       json.RawMessage `json:"counts"`
	StartedAt    time.Time       `json:"started_at"`
	CompletedAt  *time.Time      `json:"completed_at"`
	Status       string          `json:"status"`
}

func (j ReconciliationJob) Validate() error {
	if j.TenantID == uuid.Nil || j.JobID == uuid.Nil || j.ConnectionID == uuid.Nil {
		return ErrNilTenant
	}
	if !oneOf(j.Direction, "INBOUND", "OUTBOUND", "BIDIRECTIONAL") {
		return detail(ErrInvalidEnum, "reconciliation_job.direction=%q", j.Direction)
	}
	if !oneOf(j.Mode, "FULL", "DELTA", "SPOT") {
		return detail(ErrInvalidEnum, "reconciliation_job.mode=%q", j.Mode)
	}
	if !oneOf(j.Status, "RUNNING", "COMPLETED", "FAILED", "ABORTED") {
		return detail(ErrInvalidEnum, "reconciliation_job.status=%q", j.Status)
	}
	if j.Status != "RUNNING" && j.CompletedAt == nil {
		return detail(ErrInvalidInterval, "reconciliation_job in terminal status %s without completed_at", j.Status)
	}
	return nil
}

func InsertReconciliationJob(ctx context.Context, tx dbport.Tx, j ReconciliationJob) error {
	if err := j.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, j.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO reconciliation_job (tenant_id, job_id, connection_id, object_key, direction, mode, cursor_token, watermark, counts, started_at, completed_at, status) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		j.TenantID, j.JobID, j.ConnectionID, j.ObjectKey, j.Direction, j.Mode, j.CursorToken, j.Watermark, object(j.Counts), j.StartedAt, j.CompletedAt, j.Status)
	return err
}

func LoadReconciliationJob(ctx context.Context, q dbport.Querier, tenantID, jobID uuid.UUID) (ReconciliationJob, error) {
	var j ReconciliationJob
	var counts []byte
	var completed *time.Time
	err := q.QueryRow(ctx, `SELECT tenant_id, job_id, connection_id, object_key, direction, mode, cursor_token, watermark, counts, started_at, completed_at, status FROM reconciliation_job WHERE tenant_id=$1 AND job_id=$2`, tenantID, jobID).
		Scan(&j.TenantID, &j.JobID, &j.ConnectionID, &j.ObjectKey, &j.Direction, &j.Mode, &j.CursorToken, &j.Watermark, &counts, &j.StartedAt, &completed, &j.Status)
	if err != nil {
		return ReconciliationJob{}, err
	}
	j.Counts = counts
	j.CompletedAt = completed
	return j, nil
}

// ReconciliationResult is one immutable per-resource comparison outcome.
type ReconciliationResult struct {
	TenantID         uuid.UUID `json:"tenant_id"`
	ResultID         uuid.UUID `json:"result_id"`
	JobID            uuid.UUID `json:"job_id"`
	ResourceKey      string    `json:"resource_key"`
	ExpectedDigest   string    `json:"expected_digest"`
	ObservedDigest   *string   `json:"observed_digest"`
	ComparisonResult string    `json:"comparison_result"`
	DiscrepancyCount int32     `json:"discrepancy_count"`
	Severity         string    `json:"severity"`
	RepairPlanRef    string    `json:"repair_plan_ref"`
	RecordedAt       time.Time `json:"recorded_at"`
}

func (r ReconciliationResult) Validate() error {
	if r.TenantID == uuid.Nil || r.ResultID == uuid.Nil || r.JobID == uuid.Nil {
		return ErrNilTenant
	}
	if r.ExpectedDigest == "" {
		return detail(ErrMissingDigest, "reconciliation_result.expected_digest")
	}
	if !oneOf(r.ComparisonResult, "PASS", "FAIL", "PARTIAL", "UNKNOWN") {
		return detail(ErrInvalidEnum, "reconciliation_result.comparison_result=%q", r.ComparisonResult)
	}
	if r.DiscrepancyCount < 0 {
		return detail(ErrInvalidEnum, "reconciliation_result.discrepancy_count=%d", r.DiscrepancyCount)
	}
	if r.ComparisonResult == "PASS" && r.DiscrepancyCount != 0 {
		return detail(ErrInvalidEnum, "reconciliation_result PASS with %d discrepancies", r.DiscrepancyCount)
	}
	if r.ComparisonResult == "FAIL" && r.DiscrepancyCount == 0 {
		return detail(ErrInvalidEnum, "reconciliation_result FAIL with no discrepancy")
	}
	return nil
}

func InsertReconciliationResult(ctx context.Context, tx dbport.Tx, r ReconciliationResult) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, r.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO reconciliation_result (tenant_id, result_id, job_id, resource_key, expected_digest, observed_digest, comparison_result, discrepancy_count, severity, repair_plan_ref, recorded_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		r.TenantID, r.ResultID, r.JobID, r.ResourceKey, r.ExpectedDigest, r.ObservedDigest, r.ComparisonResult, r.DiscrepancyCount, r.Severity, r.RepairPlanRef, r.RecordedAt)
	return err
}

func LoadReconciliationResult(ctx context.Context, q dbport.Querier, tenantID, resultID uuid.UUID) (ReconciliationResult, error) {
	var r ReconciliationResult
	var observed *string
	err := q.QueryRow(ctx, `SELECT tenant_id, result_id, job_id, resource_key, expected_digest, observed_digest, comparison_result, discrepancy_count, severity, repair_plan_ref, recorded_at FROM reconciliation_result WHERE tenant_id=$1 AND result_id=$2`, tenantID, resultID).
		Scan(&r.TenantID, &r.ResultID, &r.JobID, &r.ResourceKey, &r.ExpectedDigest, &observed, &r.ComparisonResult, &r.DiscrepancyCount, &r.Severity, &r.RepairPlanRef, &r.RecordedAt)
	if err != nil {
		return ReconciliationResult{}, err
	}
	r.ObservedDigest = observed
	return r, nil
}

func oneOf(value string, allowed ...string) bool {
	for _, a := range allowed {
		if value == a {
			return true
		}
	}
	return false
}
