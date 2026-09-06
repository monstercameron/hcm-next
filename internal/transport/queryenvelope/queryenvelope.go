// Package queryenvelope defines ALIGN-017's authorized product-query
// envelope. It adapts the landed repository authorization scope into a
// bounded transport contract without reading storage or carrying row values.
package queryenvelope

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/trust/authz"
)

const schemaVersion = 1

// Version identifies this transport contract.
func Version() int { return schemaVersion }

var (
	// ErrInvalid is returned when an envelope would be ambiguous or would
	// bypass the authorization scope.
	ErrInvalid = errors.New("queryenvelope: invalid authorized product-query envelope")
)

// FreshnessState is the safe, bounded freshness vocabulary a product response
// may expose without copying projection payloads into the envelope.
type FreshnessState string

const (
	FreshnessCurrent     FreshnessState = "CURRENT"
	FreshnessStale       FreshnessState = "STALE"
	FreshnessUnavailable FreshnessState = "UNAVAILABLE"
	FreshnessUnknown     FreshnessState = "UNKNOWN"
)

func (s FreshnessState) valid() bool {
	switch s {
	case FreshnessCurrent, FreshnessStale, FreshnessUnavailable, FreshnessUnknown:
		return true
	default:
		return false
	}
}

// Freshness identifies the source snapshot and bounded serving state. It
// contains no row values and is safe to carry through SSR, RPC, or export
// adapters.
type Freshness struct {
	State         FreshnessState `json:"state"`
	SourceVersion string         `json:"source_version"`
	Watermark     string         `json:"watermark"`
}

func (f Freshness) validate() error {
	if !f.State.valid() {
		return fmt.Errorf("%w: freshness state %q is not recognized", ErrInvalid, f.State)
	}
	if !safeToken(f.SourceVersion) {
		return fmt.Errorf("%w: freshness source version is empty or unsafe", ErrInvalid)
	}
	if !safeToken(f.Watermark) {
		return fmt.Errorf("%w: freshness watermark is empty or unsafe", ErrInvalid)
	}
	return nil
}

// FieldDisposition is the authorized result for one requested field. A
// denied or withheld field has no value in this contract; an adapter must
// omit it or use its own governed redaction representation.
type FieldDisposition struct {
	Field       authz.FieldID `json:"field"`
	Effect      authz.Effect  `json:"effect"`
	RuleID      string        `json:"rule_id,omitempty"`
	Obligations []string      `json:"obligations,omitempty"`
}

// Request is the transport-independent input used to construct an envelope.
// Scope is the only authority-bearing input; callers cannot provide subjects
// or field dispositions separately and thereby widen it.
type Request struct {
	SliceID      string
	SliceVersion int
	QueryID      string
	Resource     string
	ReadAt       values.Instant
	MaxRows      int
	Scope        authz.RepositoryScope
	Freshness    Freshness
}

// Envelope is the authorized query contract passed from the trusted backend
// to a product-query adapter. It contains explicit authorized subjects only;
// there is no wildcard or table-shaped query form.
type Envelope struct {
	SchemaVersion int                `json:"schema_version"`
	SliceID       string             `json:"slice_id"`
	SliceVersion  int                `json:"slice_version"`
	QueryID       string             `json:"query_id"`
	Resource      string             `json:"resource"`
	Tenant        values.TenantId    `json:"tenant_id"`
	Purpose       string             `json:"purpose"`
	ReadAt        values.Instant     `json:"read_at"`
	MaxRows       int                `json:"max_rows"`
	Subjects      []values.EntityRef `json:"subjects"`
	Fields        []FieldDisposition `json:"fields"`
	PolicyVersion string             `json:"policy_version"`
	EvidenceID    string             `json:"evidence_id"`
	Freshness     Freshness          `json:"freshness"`
}

// New constructs and validates an envelope from a repository authorization
// scope. A denied but valid scope produces a safe zero-subject envelope; this
// lets callers return the same shape without turning authorization denial into
// an existence oracle.
func New(req Request) (Envelope, error) {
	if err := validateRequest(req); err != nil {
		return Envelope{}, err
	}
	if err := req.Scope.Validate(); err != nil {
		return Envelope{}, fmt.Errorf("%w: repository scope: %v", ErrInvalid, err)
	}
	if req.Scope.EvaluatedAt().Compare(req.ReadAt) != 0 {
		return Envelope{}, fmt.Errorf("%w: read instant differs from authorization instant", ErrInvalid)
	}

	subjects := req.Scope.AllowedSubjects()
	fields := make([]FieldDisposition, 0, len(req.Scope.Fields()))
	if req.Scope.Effect() == authz.EffectAllow {
		for field, ruling := range req.Scope.Fields() {
			fields = append(fields, FieldDisposition{
				Field: field, Effect: ruling.Effect, RuleID: ruling.RuleID,
				Obligations: append([]string(nil), ruling.Obligations...),
			})
		}
	} else {
		// Uniformly suppress field metadata when the subject/tenant scope was
		// denied. The caller already knows its requested shape, but the
		// response must not reveal which field rules would have applied.
		subjects = nil
	}
	sort.Slice(fields, func(i, j int) bool { return string(fields[i].Field) < string(fields[j].Field) })
	envelope := Envelope{
		SchemaVersion: schemaVersion,
		SliceID:       req.SliceID,
		SliceVersion:  req.SliceVersion,
		QueryID:       req.QueryID,
		Resource:      req.Resource,
		Tenant:        req.Scope.Tenant(),
		Purpose:       req.Scope.Purpose(),
		ReadAt:        req.ReadAt,
		MaxRows:       req.MaxRows,
		Subjects:      subjects,
		Fields:        fields,
		PolicyVersion: req.Scope.PolicyVersion(),
		EvidenceID:    req.Scope.EvidenceID(),
		Freshness:     req.Freshness,
	}
	if err := envelope.Validate(); err != nil {
		return Envelope{}, err
	}
	return envelope, nil
}

// Validate checks the immutable envelope after construction. It is also safe
// for transport adapters to call before forwarding a decoded envelope.
func (e Envelope) Validate() error {
	if e.SchemaVersion != schemaVersion {
		return fmt.Errorf("%w: schema version %d, want %d", ErrInvalid, e.SchemaVersion, schemaVersion)
	}
	if err := validateRequest(Request{
		SliceID: e.SliceID, SliceVersion: e.SliceVersion, QueryID: e.QueryID,
		Resource: e.Resource, ReadAt: e.ReadAt, MaxRows: e.MaxRows, Freshness: e.Freshness,
	}); err != nil {
		return err
	}
	if err := e.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %v", ErrInvalid, err)
	}
	if !safeToken(e.Purpose) || !safeToken(e.PolicyVersion) || !safeToken(e.EvidenceID) {
		return fmt.Errorf("%w: authorization evidence is incomplete", ErrInvalid)
	}
	seenSubjects := make(map[string]bool, len(e.Subjects))
	if len(e.Subjects) > e.MaxRows {
		return fmt.Errorf("%w: %d authorized subjects exceed max_rows %d", ErrInvalid, len(e.Subjects), e.MaxRows)
	}
	for _, subject := range e.Subjects {
		if err := subject.Validate(); err != nil {
			return fmt.Errorf("%w: subject: %v", ErrInvalid, err)
		}
		if subject.Tenant != e.Tenant {
			return fmt.Errorf("%w: subject crosses tenant boundary", ErrInvalid)
		}
		if seenSubjects[subject.String()] {
			return fmt.Errorf("%w: duplicate authorized subject", ErrInvalid)
		}
		seenSubjects[subject.String()] = true
	}
	seenFields := make(map[authz.FieldID]bool, len(e.Fields))
	for _, field := range e.Fields {
		if field.Field == "" || !field.Effect.Valid() || field.RuleID == "" {
			return fmt.Errorf("%w: field disposition is incomplete", ErrInvalid)
		}
		if seenFields[field.Field] {
			return fmt.Errorf("%w: duplicate field disposition %q", ErrInvalid, field.Field)
		}
		seenFields[field.Field] = true
		if field.Effect == authz.EffectRedacted && len(field.Obligations) == 0 {
			return fmt.Errorf("%w: redacted field %q has no obligation", ErrInvalid, field.Field)
		}
	}
	return nil
}

// Authorized reports whether the envelope contains an allowed scope with at
// least one authorized subject. An allowed zero-row result is valid but is not
// an authorization grant for a specific product row.
func (e Envelope) Authorized() bool { return e.Validate() == nil && len(e.Subjects) > 0 }

// AllowedSubjects returns a defensive copy of the explicit authorized rows.
func (e Envelope) AllowedSubjects() []values.EntityRef {
	return append([]values.EntityRef(nil), e.Subjects...)
}

// Explain returns a bounded, redaction-safe summary. It names no tenant,
// subject, field value, or storage relation.
func (e Envelope) Explain() string {
	return fmt.Sprintf("authorized product query v%d %s@%d with %d subject(s), %d field disposition(s), freshness=%s", e.SchemaVersion, e.SliceID, e.SliceVersion, len(e.Subjects), len(e.Fields), e.Freshness.State)
}

// Digest returns a stable identity for the observable envelope metadata.
func (e Envelope) Digest() string {
	h := sha256.New()
	write := func(label, value string) { fmt.Fprintf(h, "%s=%d:%s;", label, len(value), value) }
	write("schema", fmt.Sprint(e.SchemaVersion))
	write("slice", e.SliceID)
	write("slice_version", fmt.Sprint(e.SliceVersion))
	write("query", e.QueryID)
	write("resource", e.Resource)
	write("tenant", e.Tenant.String())
	write("purpose", e.Purpose)
	write("read_at", e.ReadAt.String())
	write("max_rows", fmt.Sprint(e.MaxRows))
	for _, subject := range e.Subjects {
		write("subject", subject.String())
	}
	for _, field := range e.Fields {
		write("field", string(field.Field))
		write("effect", field.Effect.String())
		write("rule", field.RuleID)
	}
	write("policy", e.PolicyVersion)
	write("evidence", e.EvidenceID)
	write("freshness", string(e.Freshness.State))
	write("source", e.Freshness.SourceVersion)
	write("watermark", e.Freshness.Watermark)
	return hex.EncodeToString(h.Sum(nil))
}

func validateRequest(req Request) error {
	if !safeToken(req.SliceID) || req.SliceVersion < 1 || !safeToken(req.QueryID) || !safeToken(req.Resource) {
		return fmt.Errorf("%w: slice, query, or resource identity is invalid", ErrInvalid)
	}
	if err := req.ReadAt.Validate(); err != nil {
		return fmt.Errorf("%w: read instant: %v", ErrInvalid, err)
	}
	if req.MaxRows < 1 || req.MaxRows > 1000 {
		return fmt.Errorf("%w: max_rows %d is outside [1,1000]", ErrInvalid, req.MaxRows)
	}
	if err := req.Freshness.validate(); err != nil {
		return err
	}
	return nil
}

func safeToken(value string) bool {
	if value == "" || len(value) > 256 || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r > 0x7e || r == ' ' {
			return false
		}
	}
	return true
}
