// Package productquery owns the semantic transport contract for authorized
// product projections. It is deliberately independent of protobuf, a
// database, and a renderer: gRPC, HTTP, SSR, and enhanced browser surfaces
// receive the same envelope after the same authorization decision.
//
// A query is evaluated over repository-provided candidates. The repository is
// responsible for applying its tenant predicate; this package applies the
// same trusted authorization inputs again before anything becomes a product
// response. An unauthorized subject is omitted rather than represented by a
// distinguishable error or count.
package productquery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

const contractVersion = 1

// Version identifies the semantic product-query contract.
func Version() int { return contractVersion }

// Explain returns a stable, value-free description of the transport boundary.
func Explain() string {
	return "authorized product queries share one tenant-scoped envelope, field disposition, projection freshness, and surface-independent digest; invalidations carry only bounded authorized references and versions"
}

// Limits are intentionally fixed contract bounds. They are not request
// fields, so a caller cannot turn a query or invalidation into an unbounded
// data or fan-out channel.
const (
	MaxCandidates         = 100
	MaxFields             = 64
	MaxInvalidationItems  = 64
	MaxProjectionNameSize = 128
	MaxInvalidationBytes  = 4096
)

// ValueState distinguishes semantic absence from an authorization decision.
// A missing candidate value is never interpreted as an empty business value.
type ValueState string

const (
	ValuePresent     ValueState = "PRESENT"
	ValueAbsent      ValueState = "ABSENT"
	ValueUnknown     ValueState = "UNKNOWN"
	ValueUnavailable ValueState = "UNAVAILABLE"
	ValueRedacted    ValueState = "REDACTED"
)

func (s ValueState) valid() bool {
	switch s {
	case ValuePresent, ValueAbsent, ValueUnknown, ValueUnavailable, ValueRedacted:
		return true
	default:
		return false
	}
}

// Cell is a repository value before authorization projection. Value is only
// copied when State is ValuePresent and the field ruling allows it.
type Cell struct {
	State ValueState `json:"state"`
	Value string     `json:"value,omitempty"`
}

func (c Cell) validate() error {
	if !c.State.valid() {
		return fmt.Errorf("productquery: invalid cell state %q", c.State)
	}
	if c.State != ValuePresent && c.Value != "" {
		return errors.New("productquery: non-present cell carries a value")
	}
	return nil
}

// Candidate is one repository result considered for an authorized product
// query. It contains no authority of its own: Relationships, Sharing, and
// ResourceOrg are facts supplied by the owning repository and evaluated by
// internal/trust/authz.
type Candidate struct {
	Subject       values.EntityRef
	ResourceOrg   authz.OrgUnitRef
	Relationships []authz.RelationshipFact
	Sharing       []authz.SharingGrant
	Fields        map[authz.FieldID]Cell
}

func (c Candidate) validate() error {
	if err := c.Subject.Validate(); err != nil {
		return fmt.Errorf("productquery: candidate subject: %w", err)
	}
	return nil
}

// Projection identifies the derived source behind an envelope. Watermark is
// the applied source position; SourceSequence is the latest source position
// observed while the response was built.
type Projection struct {
	Name              string        `json:"name"`
	DefinitionVersion string        `json:"definition_version"`
	SchemaVersion     string        `json:"schema_version"`
	SourceSequence    uint64        `json:"source_sequence"`
	Watermark         uint64        `json:"watermark"`
	ObservedAt        time.Time     `json:"observed_at"`
	MaxAge            time.Duration `json:"max_age"`
}

// Freshness is the response-level reliability state of the projection.
type Freshness string

const (
	FreshnessUnknown Freshness = "UNKNOWN"
	FreshnessCurrent Freshness = "CURRENT"
	FreshnessStale   Freshness = "STALE"
)

func (p Projection) validate() error {
	if !boundedToken(p.Name, MaxProjectionNameSize) {
		return errors.New("productquery: projection name is empty or unbounded")
	}
	if !boundedToken(p.DefinitionVersion, MaxProjectionNameSize) || !boundedToken(p.SchemaVersion, MaxProjectionNameSize) {
		return errors.New("productquery: projection versions are empty or unbounded")
	}
	if p.Watermark > p.SourceSequence {
		return errors.New("productquery: projection watermark is ahead of source sequence")
	}
	if p.ObservedAt.IsZero() || p.MaxAge <= 0 {
		return errors.New("productquery: projection freshness metadata is incomplete")
	}
	return nil
}

// Validate reports whether projection metadata is complete and bounded.
func (p Projection) Validate() error { return p.validate() }

func (p Projection) freshnessAt(now time.Time) Freshness {
	if now.IsZero() || p.ObservedAt.IsZero() || p.MaxAge <= 0 {
		return FreshnessUnknown
	}
	if now.Before(p.ObservedAt) || now.Sub(p.ObservedAt) <= p.MaxAge {
		return FreshnessCurrent
	}
	return FreshnessStale
}

// Request is the canonical input shared by every product-query adapter.
// Tenant is intentionally absent: it is derived from Principal and checked
// against each candidate's tenant-scoped reference.
type Request struct {
	Principal   *trust.Principal
	Purpose     string
	EffectiveAt values.Instant
	Fields      []authz.FieldID
	OrgEdges    []authz.OrgEdge
	Candidates  []Candidate
	Projection  Projection
	ObservedNow time.Time
}

func (r Request) validate() error {
	if r.Principal == nil {
		return errors.New("productquery: principal is required")
	}
	if r.Purpose != "" && !boundedToken(r.Purpose, MaxProjectionNameSize) {
		return errors.New("productquery: purpose is unbounded")
	}
	if r.Purpose == "" && r.Principal.DefaultPurpose() == "" {
		return errors.New("productquery: purpose is required")
	}
	if err := r.EffectiveAt.Validate(); err != nil {
		return fmt.Errorf("productquery: effective time: %w", err)
	}
	if len(r.Fields) == 0 || len(r.Fields) > MaxFields {
		return fmt.Errorf("productquery: fields must contain 1..%d entries", MaxFields)
	}
	seenFields := make(map[authz.FieldID]struct{}, len(r.Fields))
	for _, field := range r.Fields {
		if field == "" {
			return errors.New("productquery: empty requested field")
		}
		if _, ok := seenFields[field]; ok {
			return fmt.Errorf("productquery: duplicate requested field %q", field)
		}
		seenFields[field] = struct{}{}
	}
	if len(r.Candidates) > MaxCandidates {
		return fmt.Errorf("productquery: candidate count exceeds %d", MaxCandidates)
	}
	if err := r.Projection.validate(); err != nil {
		return err
	}
	for i, candidate := range r.Candidates {
		if err := candidate.validate(); err != nil {
			return fmt.Errorf("productquery: candidate %d: %w", i, err)
		}
	}
	seenSubjects := make(map[string]struct{}, len(r.Candidates))
	for _, candidate := range r.Candidates {
		key := candidate.Subject.String()
		if _, ok := seenSubjects[key]; ok {
			return fmt.Errorf("productquery: duplicate candidate subject %q", key)
		}
		seenSubjects[key] = struct{}{}
	}
	return nil
}

// Field is one safe field value in a product row. Denied fields are omitted;
// a redacted ruling is represented without the source value.
type Field struct {
	ID          authz.FieldID `json:"id"`
	Disposition authz.Effect  `json:"disposition"`
	State       ValueState    `json:"state"`
	Value       string        `json:"value,omitempty"`
}

// Row is an authorized, bounded product row. Subject is safe because it is
// present only after the subject-level decision allowed disclosure.
type Row struct {
	Subject values.EntityRef `json:"subject"`
	Fields  []Field          `json:"fields"`
}

// Envelope is the canonical semantic response. Adapters may serialize it as
// protobuf, JSON, or SSR data, but must not add surface-specific authority or
// business values to it.
type Envelope struct {
	ContractVersion int             `json:"contract_version"`
	Tenant          values.TenantId `json:"tenant"`
	Purpose         string          `json:"purpose"`
	PolicyVersions  []string        `json:"policy_versions"`
	Projection      Projection      `json:"projection"`
	Freshness       Freshness       `json:"freshness"`
	Rows            []Row           `json:"rows"`
}

// Validate checks the safe output shape. It is useful at adapter boundaries
// before a protobuf, JSON, or SSR serializer is allowed to copy the envelope.
func (e Envelope) Validate() error {
	if e.ContractVersion != contractVersion {
		return fmt.Errorf("productquery: unsupported envelope version %d", e.ContractVersion)
	}
	if err := e.Tenant.Validate(); err != nil {
		return fmt.Errorf("productquery: envelope tenant: %w", err)
	}
	if !boundedToken(e.Purpose, MaxProjectionNameSize) {
		return errors.New("productquery: envelope purpose is empty or unbounded")
	}
	if err := e.Projection.validate(); err != nil {
		return err
	}
	if e.Freshness != FreshnessUnknown && e.Freshness != FreshnessCurrent && e.Freshness != FreshnessStale {
		return fmt.Errorf("productquery: invalid freshness %q", e.Freshness)
	}
	if len(e.Rows) > MaxCandidates {
		return fmt.Errorf("productquery: envelope row count exceeds %d", MaxCandidates)
	}
	seen := make(map[string]struct{}, len(e.Rows))
	for _, row := range e.Rows {
		if err := row.Subject.Validate(); err != nil || row.Subject.Tenant != e.Tenant {
			return errors.New("productquery: envelope row is outside its tenant")
		}
		key := row.Subject.String()
		if _, ok := seen[key]; ok {
			return fmt.Errorf("productquery: duplicate envelope row %q", key)
		}
		seen[key] = struct{}{}
		if len(row.Fields) > MaxFields {
			return fmt.Errorf("productquery: envelope field count exceeds %d", MaxFields)
		}
		for _, field := range row.Fields {
			if field.ID == "" || (field.Disposition != authz.EffectAllow && field.Disposition != authz.EffectRedacted) || !field.State.valid() {
				return errors.New("productquery: envelope contains an invalid field")
			}
			if field.Disposition == authz.EffectRedacted && field.Value != "" {
				return errors.New("productquery: redacted field carries a value")
			}
		}
	}
	return nil
}

// Project evaluates the trusted authorization contract and produces one
// surface-independent envelope. Unauthorized subjects are dropped uniformly,
// so absence from the result does not distinguish a denied record from a
// record that was not returned by the repository.
func Project(req Request) (Envelope, error) {
	if err := req.validate(); err != nil {
		return Envelope{}, err
	}
	fields := append([]authz.FieldID(nil), req.Fields...)
	sort.Slice(fields, func(i, j int) bool { return fields[i] < fields[j] })
	candidates := append([]Candidate(nil), req.Candidates...)
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Subject.String() < candidates[j].Subject.String() })

	principalOrg := authz.OrgUnitRef{}
	if scope := req.Principal.OrganizationScopeID(); scope != "" {
		principalOrg = authz.OrgUnitRef{Tenant: req.Principal.Tenant(), ID: scope}
	}

	envelope := Envelope{
		ContractVersion: contractVersion,
		Tenant:          req.Principal.Tenant(),
		Purpose:         req.Purpose,
		Projection:      req.Projection,
		Freshness:       req.Projection.freshnessAt(req.ObservedNow),
		PolicyVersions:  []string{authz.PolicyVersion},
	}
	if envelope.Purpose == "" {
		envelope.Purpose = req.Principal.DefaultPurpose()
	}
	for _, candidate := range candidates {
		decision, err := authz.Enforce(authz.Request{
			Principal:     req.Principal,
			Purpose:       req.Purpose,
			EffectiveAt:   req.EffectiveAt,
			Subject:       candidate.Subject,
			PrincipalOrg:  principalOrg,
			ResourceOrg:   candidate.ResourceOrg,
			OrgEdges:      req.OrgEdges,
			Sharing:       candidate.Sharing,
			Relationships: candidate.Relationships,
			Fields:        fields,
		})
		if err != nil {
			return Envelope{}, fmt.Errorf("productquery: authorize %s: %w", candidate.Subject.String(), err)
		}
		if !decision.SubjectDisclosable {
			continue
		}
		if envelope.Purpose == "" {
			envelope.Purpose = decision.Purpose
		}
		envelope.PolicyVersions = appendUnique(envelope.PolicyVersions, decision.PolicyVersions...)
		row := Row{Subject: candidate.Subject}
		for _, fieldID := range fields {
			ruling := decision.Fields[fieldID]
			field, ok, fieldErr := projectField(fieldID, ruling, candidate.Fields[fieldID])
			if fieldErr != nil {
				return Envelope{}, fmt.Errorf("productquery: project %s.%s: %w", candidate.Subject.String(), fieldID, fieldErr)
			}
			if ok {
				row.Fields = append(row.Fields, field)
			}
		}
		envelope.Rows = append(envelope.Rows, row)
	}
	sort.Strings(envelope.PolicyVersions)
	return envelope, nil
}

// Build is an explicit transport-oriented alias for Project.
func Build(req Request) (Envelope, error) { return Project(req) }

func projectField(id authz.FieldID, ruling authz.FieldRuling, cell Cell) (Field, bool, error) {
	switch ruling.Effect {
	case authz.EffectAllow:
		if cell.State == "" {
			cell.State = ValueUnknown
		}
		if err := cell.validate(); err != nil {
			return Field{}, false, err
		}
		return Field{ID: id, Disposition: ruling.Effect, State: cell.State, Value: cell.Value}, true, nil
	case authz.EffectRedacted:
		return Field{ID: id, Disposition: ruling.Effect, State: ValueRedacted}, true, nil
	default:
		return Field{}, false, nil
	}
}

// CanonicalBytes is the cross-surface semantic encoding. It contains no
// transport kind, caller request id, or server-local timing value.
func (e Envelope) CanonicalBytes() ([]byte, error) {
	copyEnvelope := e
	copyEnvelope.PolicyVersions = append([]string(nil), e.PolicyVersions...)
	if copyEnvelope.PolicyVersions == nil {
		copyEnvelope.PolicyVersions = []string{}
	}
	sort.Strings(copyEnvelope.PolicyVersions)
	copyEnvelope.Rows = cloneRows(e.Rows)
	if copyEnvelope.Rows == nil {
		copyEnvelope.Rows = []Row{}
	}
	sort.SliceStable(copyEnvelope.Rows, func(i, j int) bool {
		return copyEnvelope.Rows[i].Subject.String() < copyEnvelope.Rows[j].Subject.String()
	})
	for i := range copyEnvelope.Rows {
		if copyEnvelope.Rows[i].Fields == nil {
			copyEnvelope.Rows[i].Fields = []Field{}
		}
		sort.SliceStable(copyEnvelope.Rows[i].Fields, func(left, right int) bool {
			return copyEnvelope.Rows[i].Fields[left].ID < copyEnvelope.Rows[i].Fields[right].ID
		})
	}
	return json.Marshal(copyEnvelope)
}

// Digest identifies the semantic envelope and is shared across all adapters.
func (e Envelope) Digest() string {
	b, err := e.CanonicalBytes()
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func cloneRows(rows []Row) []Row {
	out := make([]Row, len(rows))
	for i, row := range rows {
		out[i] = row
		out[i].Fields = append([]Field(nil), row.Fields...)
	}
	return out
}

func appendUnique(dst []string, values ...string) []string {
	seen := make(map[string]struct{}, len(dst)+len(values))
	for _, value := range dst {
		seen[value] = struct{}{}
	}
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; !ok {
			dst = append(dst, value)
			seen[value] = struct{}{}
		}
	}
	return dst
}

func boundedToken(value string, max int) bool {
	if value == "" || len(value) > max || strings.TrimSpace(value) != value {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 0x21 || value[i] > 0x7e {
			return false
		}
	}
	return true
}

// InvalidationTarget is an authorized source change before transport
// projection. Decision is retained only for this in-process check and is
// never placed on the wire.
type InvalidationTarget struct {
	Subject       values.EntityRef
	Revision      uint64
	Decision      authz.Decision
	ResourceOrg   authz.OrgUnitRef
	Relationships []authz.RelationshipFact
	Sharing       []authz.SharingGrant
}

// InvalidationRequest is the bounded input to EmitInvalidation.
type InvalidationRequest struct {
	Principal      *trust.Principal
	Purpose        string
	EffectiveAt    values.Instant
	Fields         []authz.FieldID
	OrgEdges       []authz.OrgEdge
	Projection     string
	SourceSequence uint64
	Watermark      uint64
	Targets        []InvalidationTarget
}

// InvalidationItem is the only per-subject data an invalidation may carry:
// an already-authorized opaque reference and its new revision.
type InvalidationItem struct {
	Subject  values.EntityRef `json:"subject"`
	Revision uint64           `json:"revision"`
}

// InvalidationMessage is a bounded hint. It is never authoritative data; the
// receiver must refetch the corresponding projection through Project.
type InvalidationMessage struct {
	ContractVersion int                `json:"contract_version"`
	Tenant          values.TenantId    `json:"tenant"`
	Projection      string             `json:"projection"`
	SourceSequence  uint64             `json:"source_sequence"`
	Watermark       uint64             `json:"watermark"`
	Items           []InvalidationItem `json:"items"`
}

// Validate checks the wire-safe shape of an invalidation after authorization
// projection. It does not and cannot authorize a target by itself.
func (m InvalidationMessage) Validate() error {
	if m.ContractVersion != contractVersion {
		return fmt.Errorf("productquery: unsupported invalidation version %d", m.ContractVersion)
	}
	if err := m.Tenant.Validate(); err != nil {
		return fmt.Errorf("productquery: invalidation tenant: %w", err)
	}
	if !boundedToken(m.Projection, MaxProjectionNameSize) {
		return errors.New("productquery: invalidation projection is empty or unbounded")
	}
	if m.Watermark > m.SourceSequence || len(m.Items) == 0 || len(m.Items) > MaxInvalidationItems {
		return errors.New("productquery: invalidation sequence or item bound is invalid")
	}
	previous := ""
	for _, item := range m.Items {
		if err := item.Subject.Validate(); err != nil || item.Subject.Tenant != m.Tenant || item.Revision == 0 {
			return errors.New("productquery: invalidation item is invalid")
		}
		key := item.Subject.String()
		if previous != "" && key <= previous {
			return errors.New("productquery: invalidation items are not unique and sorted")
		}
		previous = key
	}
	b, err := m.CanonicalBytes()
	if err != nil {
		return err
	}
	if len(b) > MaxInvalidationBytes {
		return fmt.Errorf("productquery: invalidation message exceeds %d bytes", MaxInvalidationBytes)
	}
	return nil
}

// EmitInvalidation emits one message when at least one target is authorized.
// Unauthorized targets are omitted without an error or count, preventing a
// mixed-authorization batch from becoming an existence oracle. The bool is
// false when no authorized item remains.
func EmitInvalidation(req InvalidationRequest) (InvalidationMessage, bool, error) {
	if req.Principal == nil {
		return InvalidationMessage{}, false, errors.New("productquery: invalidation principal is required")
	}
	if !boundedToken(req.Projection, MaxProjectionNameSize) {
		return InvalidationMessage{}, false, errors.New("productquery: invalidation projection is empty or unbounded")
	}
	if req.Watermark > req.SourceSequence {
		return InvalidationMessage{}, false, errors.New("productquery: invalidation watermark is ahead of source sequence")
	}
	if len(req.Targets) > MaxInvalidationItems {
		return InvalidationMessage{}, false, fmt.Errorf("productquery: invalidation item count exceeds %d", MaxInvalidationItems)
	}

	items := make(map[string]InvalidationItem, len(req.Targets))
	for i, target := range req.Targets {
		if err := target.Subject.Validate(); err != nil {
			return InvalidationMessage{}, false, fmt.Errorf("productquery: invalidation target %d: %w", i, err)
		}
		if target.Revision == 0 {
			return InvalidationMessage{}, false, fmt.Errorf("productquery: invalidation target %d has no revision", i)
		}
		if target.Subject.Tenant != req.Principal.Tenant() {
			continue
		}
		principalOrg := authz.OrgUnitRef{}
		if scope := req.Principal.OrganizationScopeID(); scope != "" {
			principalOrg = authz.OrgUnitRef{Tenant: req.Principal.Tenant(), ID: scope}
		}
		fresh, err := authz.Enforce(authz.Request{
			Principal:     req.Principal,
			Purpose:       req.Purpose,
			EffectiveAt:   req.EffectiveAt,
			Subject:       target.Subject,
			PrincipalOrg:  principalOrg,
			ResourceOrg:   target.ResourceOrg,
			OrgEdges:      req.OrgEdges,
			Sharing:       target.Sharing,
			Relationships: target.Relationships,
			Fields:        req.Fields,
		})
		if err != nil || !fresh.SubjectDisclosable || !reflect.DeepEqual(fresh, target.Decision) {
			continue
		}
		key := target.Subject.String()
		if previous, ok := items[key]; !ok || target.Revision > previous.Revision {
			items[key] = InvalidationItem{Subject: target.Subject, Revision: target.Revision}
		}
	}
	if len(items) == 0 {
		return InvalidationMessage{}, false, nil
	}
	message := InvalidationMessage{
		ContractVersion: contractVersion,
		Tenant:          req.Principal.Tenant(),
		Projection:      req.Projection,
		SourceSequence:  req.SourceSequence,
		Watermark:       req.Watermark,
		Items:           make([]InvalidationItem, 0, len(items)),
	}
	for _, item := range items {
		message.Items = append(message.Items, item)
	}
	sort.Slice(message.Items, func(i, j int) bool { return message.Items[i].Subject.String() < message.Items[j].Subject.String() })
	if err := message.Validate(); err != nil {
		return InvalidationMessage{}, false, err
	}
	return message, true, nil
}

// BuildInvalidation is an explicit alias for EmitInvalidation.
func BuildInvalidation(req InvalidationRequest) (InvalidationMessage, bool, error) {
	return EmitInvalidation(req)
}

// Digest returns the stable semantic digest of an invalidation hint.
func (m InvalidationMessage) Digest() string {
	if len(m.Items) == 0 {
		return ""
	}
	b, err := m.CanonicalBytes()
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// CanonicalBytes returns the stable wire-independent encoding of an
// invalidation hint.
func (m InvalidationMessage) CanonicalBytes() ([]byte, error) {
	copyMessage := m
	copyMessage.Items = append([]InvalidationItem(nil), m.Items...)
	if copyMessage.Items == nil {
		copyMessage.Items = []InvalidationItem{}
	}
	sort.SliceStable(copyMessage.Items, func(i, j int) bool {
		return copyMessage.Items[i].Subject.String() < copyMessage.Items[j].Subject.String()
	})
	return json.Marshal(copyMessage)
}
