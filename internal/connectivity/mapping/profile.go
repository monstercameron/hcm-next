package mapping

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation/ir"
)

const IdentityTransformation = "identity"

var (
	ErrInvalidProfileVersion   = errors.New("mapping: invalid profile version")
	ErrUnknownTargetField      = errors.New("mapping: unknown target field")
	ErrDuplicateMapping        = errors.New("mapping: duplicate field mapping")
	ErrClassificationDowngrade = errors.New("mapping: classification downgrade")
	ErrInvalidTransformation   = errors.New("mapping: invalid transformation IR reference")
)

// Classification is the minimum classification that a mapped value retains.
// The order is deliberately explicit so a compiler can refuse a weakening of
// source or target classification without consulting ambient policy.
type Classification string

const (
	ClassificationPublic          Classification = "PUBLIC"
	ClassificationInternal        Classification = "INTERNAL"
	ClassificationPII             Classification = "PII"
	ClassificationCompensation    Classification = "COMPENSATION"
	ClassificationBank            Classification = "BANK"
	ClassificationMedical         Classification = "MEDICAL"
	ClassificationImmigration     Classification = "IMMIGRATION"
	ClassificationCase            Classification = "CASE"
	ClassificationSpecialCategory Classification = "SPECIAL_CATEGORY"

	ClassPublic          = ClassificationPublic
	ClassInternal        = ClassificationInternal
	ClassPII             = ClassificationPII
	ClassCompensation    = ClassificationCompensation
	ClassBank            = ClassificationBank
	ClassMedical         = ClassificationMedical
	ClassImmigration     = ClassificationImmigration
	ClassCase            = ClassificationCase
	ClassSpecialCategory = ClassificationSpecialCategory
)

var classificationRanks = map[Classification]int{
	ClassificationPublic: 0, ClassificationInternal: 1, ClassificationPII: 2,
	ClassificationCompensation: 3, ClassificationBank: 3, ClassificationMedical: 3,
	ClassificationImmigration: 3, ClassificationCase: 3, ClassificationSpecialCategory: 4,
}

func (c Classification) Valid() bool { _, ok := classificationRanks[c]; return ok }

// TargetField is a field admitted by the target entity schema. A target field
// without a classification still participates in unknown-field checking.
type TargetField struct {
	Name           string
	Classification Classification
}

// FieldMapping is one source-to-target mapping. Exactly one transformation
// reference is required: either the literal identity token or a sha256 digest
// produced by internal/engines/transformation/ir.
type FieldMapping struct {
	SourceField            string
	TargetField            string
	TransformationIRDigest string
	// IRDigest, TransformDigest, and TransformationDigest are compatibility
	// spellings for callers integrating different manifest serializers. The
	// compiler normalizes them to TransformationIRDigest.
	IRDigest             string
	TransformDigest      string
	TransformationDigest string
	TransformationRef    string
	Identity             bool
	Required             bool
	Optional             bool
	Classification       Classification
	SourceClassification Classification
	TargetClassification Classification
}

// MappingProfile is the source form compiled by Compile. TargetFields may be
// []TargetField, []string, map[string]Classification, or map[string]string;
// accepting the map forms keeps schema descriptors convenient while the
// compiler canonicalizes all of them to the same ordered representation.
type MappingProfile struct {
	MappingID            string
	Version              any
	ProfileVersion       any
	SourceSystemRef      string
	TargetEntity         string
	Mappings             []FieldMapping
	FieldMappings        []FieldMapping
	Fields               []FieldMapping
	TargetFields         any
	Classification       Classification
	SourceClassification Classification
}

// MappingProfileVersion contains only private mutable state. Accessors
// return deep copies, so changing the input profile or an accessor result
// cannot alter a compiled version or its digest.
type MappingProfileVersion struct {
	profile      MappingProfile
	mappings     []FieldMapping
	targetFields []TargetField
	digest       string
}

// Compile deterministically compiles a profile against its declared target
// fields. An optional schema argument may supply target fields when the
// profile's TargetFields is nil; it accepts the same forms as TargetFields.
func Compile(profile MappingProfile, targetSchema ...any) (MappingProfileVersion, error) {
	normalized, err := normalizeProfile(profile, targetSchema...)
	if err != nil {
		return MappingProfileVersion{}, err
	}
	canon, err := json.Marshal(normalized)
	if err != nil {
		return MappingProfileVersion{}, fmt.Errorf("%w: canonical profile: %v", ErrInvalidProfileVersion, err)
	}
	sum := sha256.Sum256(canon)
	storedProfile := cloneProfile(profile)
	storedProfile.Version = normalized.Version
	storedProfile.ProfileVersion = nil
	compiled := MappingProfileVersion{
		profile:      storedProfile,
		mappings:     append([]FieldMapping(nil), normalized.Mappings...),
		targetFields: append([]TargetField(nil), normalized.TargetFields...),
		digest:       "sha256:" + hex.EncodeToString(sum[:]),
	}
	compiled.profile.Mappings = append([]FieldMapping(nil), normalized.Mappings...)
	compiled.profile.FieldMappings = nil
	compiled.profile.Fields = nil
	compiled.profile.TargetFields = append([]TargetField(nil), normalized.TargetFields...)
	return compiled, nil
}

// CompileProfile is a descriptive alias for Compile.
func CompileProfile(profile MappingProfile, targetSchema ...any) (MappingProfileVersion, error) {
	return Compile(profile, targetSchema...)
}

func (v MappingProfileVersion) Digest() string { return v.digest }
func (v MappingProfileVersion) Canonical() []byte {
	profile := v.profile
	version, _, _ := profileVersion(profile.Version)
	profile.Mappings = append([]FieldMapping(nil), v.mappings...)
	profile.FieldMappings = nil
	profile.Fields = nil
	profile.TargetFields = append([]TargetField(nil), v.targetFields...)
	normalized := canonicalProfile{
		MappingID: profile.MappingID, Version: version, SourceSystemRef: profile.SourceSystemRef,
		TargetEntity: profile.TargetEntity, Mappings: append([]FieldMapping(nil), v.mappings...),
		TargetFields: append([]TargetField(nil), v.targetFields...), Classification: profile.Classification,
		SourceClassification: profile.SourceClassification,
	}
	b, _ := json.Marshal(normalized)
	return b
}

func (v MappingProfileVersion) Profile() MappingProfile {
	p := cloneProfile(v.profile)
	p.Mappings = append([]FieldMapping(nil), v.mappings...)
	p.FieldMappings = nil
	p.Fields = nil
	p.TargetFields = append([]TargetField(nil), v.targetFields...)
	return p
}

func (v MappingProfileVersion) Mappings() []FieldMapping {
	return append([]FieldMapping(nil), v.mappings...)
}

func (v MappingProfileVersion) Explain() string {
	return fmt.Sprintf("mapping profile %s@%d from %s to %s (%d fields; %s)", v.profile.MappingID, v.profile.Version, v.profile.SourceSystemRef, v.profile.TargetEntity, len(v.mappings), v.digest)
}

type canonicalProfile struct {
	MappingID            string
	Version              int
	SourceSystemRef      string
	TargetEntity         string
	Mappings             []FieldMapping
	TargetFields         []TargetField
	Classification       Classification
	SourceClassification Classification
}

func normalizeProfile(p MappingProfile, targetSchema ...any) (canonicalProfile, error) {
	version, versionSet, versionErr := profileVersion(p.Version)
	if versionErr != nil {
		return canonicalProfile{}, versionErr
	}
	if p.ProfileVersion != nil {
		profileVersionValue, profileVersionSet, err := profileVersion(p.ProfileVersion)
		if err != nil {
			return canonicalProfile{}, err
		}
		if versionSet && profileVersionSet && version != profileVersionValue {
			return canonicalProfile{}, fmt.Errorf("%w: Version and ProfileVersion disagree", ErrInvalidProfileVersion)
		}
		version, versionSet = profileVersionValue, profileVersionSet
	}
	if p.MappingID == "" || !versionSet || version < 1 || strings.TrimSpace(p.SourceSystemRef) == "" || strings.TrimSpace(p.TargetEntity) == "" {
		return canonicalProfile{}, fmt.Errorf("%w: mapping id, positive version, source system ref, and target entity are required", ErrInvalidProfileVersion)
	}
	if p.Classification != "" && !p.Classification.Valid() {
		return canonicalProfile{}, fmt.Errorf("%w: unknown profile classification %q", ErrInvalidProfileVersion, p.Classification)
	}
	if p.SourceClassification != "" && !p.SourceClassification.Valid() {
		return canonicalProfile{}, fmt.Errorf("%w: unknown source classification %q", ErrInvalidProfileVersion, p.SourceClassification)
	}
	mappings := append([]FieldMapping(nil), p.Mappings...)
	if len(p.FieldMappings) > 0 {
		mappings = append(mappings, p.FieldMappings...)
	}
	if len(p.Fields) > 0 {
		mappings = append(mappings, p.Fields...)
	}
	if len(mappings) == 0 {
		return canonicalProfile{}, fmt.Errorf("%w: at least one field mapping is required", ErrInvalidProfileVersion)
	}
	targets, err := targetFields(p.TargetFields, targetSchema...)
	if err != nil {
		return canonicalProfile{}, err
	}
	if len(targets) == 0 {
		return canonicalProfile{}, fmt.Errorf("%w: target fields are required to resolve mappings", ErrInvalidProfileVersion)
	}
	targetByName := make(map[string]TargetField, len(targets))
	for _, target := range targets {
		if target.Name == "" || target.Classification != "" && !target.Classification.Valid() {
			return canonicalProfile{}, fmt.Errorf("%w: invalid target field %q", ErrInvalidProfileVersion, target.Name)
		}
		if _, exists := targetByName[target.Name]; exists {
			return canonicalProfile{}, fmt.Errorf("%w: target field %q is declared twice", ErrInvalidProfileVersion, target.Name)
		}
		targetByName[target.Name] = target
	}
	seenTargets := make(map[string]bool, len(mappings))
	for i := range mappings {
		m := &mappings[i]
		if strings.TrimSpace(m.SourceField) == "" || strings.TrimSpace(m.TargetField) == "" {
			return canonicalProfile{}, fmt.Errorf("%w: mapping %d needs source and target fields", ErrInvalidProfileVersion, i)
		}
		if _, ok := targetByName[m.TargetField]; !ok {
			return canonicalProfile{}, fmt.Errorf("%w: %q", ErrUnknownTargetField, m.TargetField)
		}
		if seenTargets[m.TargetField] {
			return canonicalProfile{}, fmt.Errorf("%w: %q", ErrDuplicateMapping, m.TargetField)
		}
		seenTargets[m.TargetField] = true
		if m.Required && m.Optional {
			return canonicalProfile{}, fmt.Errorf("%w: mapping %q cannot be both required and optional", ErrInvalidProfileVersion, m.TargetField)
		}
		m.Optional = !m.Required
		if !m.Classification.Valid() {
			return canonicalProfile{}, fmt.Errorf("%w: mapping %q has no valid classification", ErrInvalidProfileVersion, m.TargetField)
		}
		if target := targetByName[m.TargetField].Classification; target != "" && classificationRanks[m.Classification] < classificationRanks[target] {
			return canonicalProfile{}, fmt.Errorf("%w: %q lowers target classification %s to %s", ErrClassificationDowngrade, m.TargetField, target, m.Classification)
		}
		if source := m.SourceClassification; source != "" {
			if !source.Valid() {
				return canonicalProfile{}, fmt.Errorf("%w: unknown source classification %q", ErrInvalidProfileVersion, source)
			}
			if classificationRanks[m.Classification] < classificationRanks[source] {
				return canonicalProfile{}, fmt.Errorf("%w: %q lowers %s to %s", ErrClassificationDowngrade, m.TargetField, source, m.Classification)
			}
		}
		for _, source := range []Classification{p.SourceClassification, p.Classification} {
			if source != "" && classificationRanks[m.Classification] < classificationRanks[source] {
				return canonicalProfile{}, fmt.Errorf("%w: %q lowers %s to %s", ErrClassificationDowngrade, m.TargetField, source, m.Classification)
			}
		}
		if target := m.TargetClassification; target != "" {
			if !target.Valid() {
				return canonicalProfile{}, fmt.Errorf("%w: unknown target classification %q", ErrInvalidProfileVersion, target)
			}
			if classificationRanks[m.Classification] < classificationRanks[target] {
				return canonicalProfile{}, fmt.Errorf("%w: %q lowers target classification %s to %s", ErrClassificationDowngrade, m.TargetField, target, m.Classification)
			}
		}
		declared := []string{m.TransformationIRDigest, m.IRDigest, m.TransformDigest, m.TransformationDigest, m.TransformationRef}
		transform := ""
		for _, candidate := range declared {
			if candidate == "" {
				continue
			}
			if transform != "" && transform != candidate {
				return canonicalProfile{}, fmt.Errorf("%w: mapping %q has conflicting transformation references", ErrInvalidTransformation, m.TargetField)
			}
			transform = candidate
		}
		if m.Identity {
			if transform != "" && transform != IdentityTransformation {
				return canonicalProfile{}, fmt.Errorf("%w: mapping %q marks identity and a different transform", ErrInvalidTransformation, m.TargetField)
			}
			transform = IdentityTransformation
		}
		if !validTransformRef(transform) {
			return canonicalProfile{}, fmt.Errorf("%w: mapping %q must name identity or a transformation IR digest", ErrInvalidTransformation, m.TargetField)
		}
		m.TransformationIRDigest = transform
		m.IRDigest, m.TransformDigest, m.TransformationDigest, m.TransformationRef, m.Identity = "", "", "", "", transform == IdentityTransformation
		m.TargetClassification = ""
	}
	sort.Slice(mappings, func(i, j int) bool {
		if mappings[i].TargetField != mappings[j].TargetField {
			return mappings[i].TargetField < mappings[j].TargetField
		}
		return mappings[i].SourceField < mappings[j].SourceField
	})
	sort.Slice(targets, func(i, j int) bool { return targets[i].Name < targets[j].Name })
	return canonicalProfile{MappingID: p.MappingID, Version: version, SourceSystemRef: p.SourceSystemRef, TargetEntity: p.TargetEntity, Mappings: mappings, TargetFields: targets, Classification: p.Classification, SourceClassification: p.SourceClassification}, nil
}

func profileVersion(value any) (int, bool, error) {
	switch v := value.(type) {
	case int:
		return v, true, nil
	case int8:
		return int(v), true, nil
	case int16:
		return int(v), true, nil
	case int32:
		return int(v), true, nil
	case int64:
		return int(v), true, nil
	case uint:
		return int(v), true, nil
	case uint8:
		return int(v), true, nil
	case uint16:
		return int(v), true, nil
	case uint32:
		return int(v), true, nil
	case uint64:
		return int(v), true, nil
	case string:
		s := strings.TrimSpace(v)
		if len(s) > 0 && (s[0] == 'v' || s[0] == 'V') {
			s = s[1:]
		}
		if s == "" || !allDigits(s) {
			return 0, false, fmt.Errorf("%w: version %q is not a positive integer or vN", ErrInvalidProfileVersion, v)
		}
		var parsed int
		if _, err := fmt.Sscanf(s, "%d", &parsed); err != nil {
			return 0, false, fmt.Errorf("%w: version %q", ErrInvalidProfileVersion, v)
		}
		return parsed, true, nil
	case nil:
		return 0, false, nil
	default:
		return 0, false, fmt.Errorf("%w: unsupported version type %T", ErrInvalidProfileVersion, value)
	}
}

func allDigits(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func validTransformRef(ref string) bool {
	if ref == IdentityTransformation {
		return true
	}
	if !strings.HasPrefix(ref, ir.DigestAlgorithm+":") || len(ref) != len(ir.DigestAlgorithm)+1+64 {
		return false
	}
	for _, r := range ref[len(ir.DigestAlgorithm)+1:] {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func targetFields(value any, schema ...any) ([]TargetField, error) {
	if value == nil && len(schema) > 0 {
		if len(schema) != 1 {
			return nil, fmt.Errorf("%w: only one target schema may be supplied", ErrInvalidProfileVersion)
		}
		value = schema[0]
	}
	switch fields := value.(type) {
	case []TargetField:
		return append([]TargetField(nil), fields...), nil
	case []string:
		out := make([]TargetField, len(fields))
		for i, name := range fields {
			out[i] = TargetField{Name: name}
		}
		return out, nil
	case map[string]Classification:
		out := make([]TargetField, 0, len(fields))
		for name, class := range fields {
			out = append(out, TargetField{Name: name, Classification: class})
		}
		return out, nil
	case map[string]string:
		out := make([]TargetField, 0, len(fields))
		for name, class := range fields {
			out = append(out, TargetField{Name: name, Classification: Classification(class)})
		}
		return out, nil
	case nil:
		return nil, nil
	default:
		return nil, fmt.Errorf("%w: unsupported target field schema %T", ErrInvalidProfileVersion, value)
	}
}

func cloneProfile(p MappingProfile) MappingProfile {
	p.Mappings = append([]FieldMapping(nil), p.Mappings...)
	p.FieldMappings = append([]FieldMapping(nil), p.FieldMappings...)
	p.Fields = append([]FieldMapping(nil), p.Fields...)
	switch fields := p.TargetFields.(type) {
	case []TargetField:
		p.TargetFields = append([]TargetField(nil), fields...)
	case []string:
		p.TargetFields = append([]string(nil), fields...)
	case map[string]Classification:
		p.TargetFields = cloneClassificationMap(fields)
	case map[string]string:
		out := make(map[string]string, len(fields))
		for k, v := range fields {
			out[k] = v
		}
		p.TargetFields = out
	}
	return p
}

func cloneClassificationMap(in map[string]Classification) map[string]Classification {
	out := make(map[string]Classification, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
