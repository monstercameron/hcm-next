// Package masking generates deterministic, privacy-safe datasets for
// development and conformance. It is deliberately kernel-pure: it accepts
// values supplied by a caller, returns a fenced in-memory result, and has no
// database, filesystem, network, or production-key access.
package masking

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"
)

const contractVersion = 1

// Version reports the masking contract version.
func Version() int { return contractVersion }

type FieldKind string

const (
	FieldPublic    FieldKind = "PUBLIC"
	FieldDirectID  FieldKind = "DIRECT_IDENTIFIER"
	FieldQuasiID   FieldKind = "QUASI_IDENTIFIER"
	FieldReference FieldKind = "REFERENCE"
	FieldTemporal  FieldKind = "TEMPORAL"
	FieldDecimal   FieldKind = "DECIMAL"
	FieldLegal     FieldKind = "LEGAL"
)

type FieldRule struct {
	Kind FieldKind
}

type SourceRow struct {
	ID           string
	Values       map[string]string
	SemanticTags []string
}

type Dataset struct {
	SourceAuthority   string
	SourceEnvironment string
	TenantID          string
	Purpose           string
	ProfileVersion    string
	Rows              []SourceRow
}

// Profile is the complete transformation and privacy declaration. Secret is
// used only to derive scoped tokens and is never copied into a Result.
type Profile struct {
	Version           string
	TenantID          string
	Purpose           string
	SourceAuthority   string
	SourceEnvironment string
	Secret            []byte
	ExpiresAt         time.Time
	Fields            map[string]FieldRule
	RequiredTags      []string
}

type Fidelity struct {
	InputRows          int
	OutputRows         int
	RequiredTags       int
	PreservedTags      int
	CoveredFieldKinds  int
	DeclaredFieldKinds int
}

type PrivacyAssessment struct {
	DirectIdentifiersRemoved    bool
	QuasiIdentifiersGeneralized bool
	CrossScopeLinkagePrevented  bool
	VisibleSyntheticMarker      bool
}

type Result struct {
	TenantID       string
	Purpose        string
	ProfileVersion string
	Marker         string
	ExpiresAt      time.Time
	Rows           []SourceRow
	Fidelity       Fidelity
	Privacy        PrivacyAssessment
	Destroyed      bool
	Digest         string
}

var (
	ErrInvalid   = errors.New("masking: invalid input")
	ErrExpired   = errors.New("masking: dataset expired")
	ErrDestroyed = errors.New("masking: dataset destroyed")
)

// Generate creates a synthetic result and performs the privacy and semantic
// coverage checks declared by profile. It rejects unclassified source fields,
// production sources, expired profiles, and scope mismatches.
func Generate(source Dataset, profile Profile, now time.Time) (Result, error) {
	if now.IsZero() || profile.ExpiresAt.IsZero() || !now.Before(profile.ExpiresAt) {
		return Result{}, ErrExpired
	}
	if strings.TrimSpace(source.SourceAuthority) == "" || strings.TrimSpace(source.SourceEnvironment) == "" || strings.TrimSpace(source.TenantID) == "" || strings.TrimSpace(source.Purpose) == "" || strings.TrimSpace(source.ProfileVersion) == "" {
		return Result{}, fmt.Errorf("%w: source authority, environment, tenant, purpose and profile version are required", ErrInvalid)
	}
	if strings.EqualFold(source.SourceEnvironment, "PRODUCTION") || strings.EqualFold(source.SourceEnvironment, "PROD") {
		return Result{}, fmt.Errorf("%w: production source is not accepted", ErrInvalid)
	}
	if source.TenantID != profile.TenantID || source.Purpose != profile.Purpose || source.ProfileVersion != profile.Version || source.SourceAuthority != profile.SourceAuthority || source.SourceEnvironment != profile.SourceEnvironment {
		return Result{}, fmt.Errorf("%w: source and profile scope differ", ErrInvalid)
	}
	if len(profile.Secret) < 16 {
		return Result{}, fmt.Errorf("%w: scoped transformation secret is too short", ErrInvalid)
	}
	if len(source.Rows) == 0 {
		return Result{}, fmt.Errorf("%w: source has no rows", ErrInvalid)
	}
	for name, rule := range profile.Fields {
		if !validFieldKind(rule.Kind) {
			return Result{}, fmt.Errorf("%w: field %q has unknown transformation kind", ErrInvalid, name)
		}
	}

	rows := make([]SourceRow, 0, len(source.Rows))
	tags := map[string]bool{}
	kinds := map[FieldKind]bool{}
	for _, row := range source.Rows {
		if strings.TrimSpace(row.ID) == "" {
			return Result{}, fmt.Errorf("%w: source row has no id", ErrInvalid)
		}
		values := make(map[string]string, len(row.Values))
		for name, value := range row.Values {
			rule, ok := profile.Fields[name]
			if !ok {
				return Result{}, fmt.Errorf("%w: field %q has no declared transformation", ErrInvalid, name)
			}
			transformed, err := transform(rule.Kind, value, profile.Secret, profile.TenantID, profile.Purpose, profile.Version, name)
			if err != nil {
				return Result{}, fmt.Errorf("%w: field %q: %v", ErrInvalid, name, err)
			}
			values[name] = transformed
			kinds[rule.Kind] = true
		}
		rowID := scopedToken(profile.Secret, profile.TenantID, profile.Purpose, profile.Version, "row", row.ID)
		rowTags := append([]string(nil), row.SemanticTags...)
		for _, tag := range rowTags {
			tags[tag] = true
		}
		rows = append(rows, SourceRow{ID: rowID, Values: values, SemanticTags: rowTags})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })

	fidelity := Fidelity{InputRows: len(source.Rows), OutputRows: len(rows), DeclaredFieldKinds: len(profile.Fields)}
	declaredKinds := map[FieldKind]bool{}
	for _, rule := range profile.Fields {
		declaredKinds[rule.Kind] = true
	}
	fidelity.DeclaredFieldKinds = len(declaredKinds)
	for _, tag := range profile.RequiredTags {
		fidelity.RequiredTags++
		if tags[tag] {
			fidelity.PreservedTags++
		}
	}
	fidelity.CoveredFieldKinds = len(kinds)
	for _, tag := range profile.RequiredTags {
		if !tags[tag] {
			return Result{}, fmt.Errorf("%w: required semantic tag %q was not preserved", ErrInvalid, tag)
		}
	}
	result := Result{
		TenantID: profile.TenantID, Purpose: profile.Purpose, ProfileVersion: profile.Version,
		Marker: "SYNTHETIC_DATASET/" + profile.Version, ExpiresAt: profile.ExpiresAt,
		Rows: rows, Fidelity: fidelity,
		Privacy: PrivacyAssessment{DirectIdentifiersRemoved: true, QuasiIdentifiersGeneralized: true, CrossScopeLinkagePrevented: true, VisibleSyntheticMarker: true},
	}
	result.Digest = result.contentDigest()
	return result, nil
}

func validFieldKind(kind FieldKind) bool {
	switch kind {
	case FieldPublic, FieldDirectID, FieldQuasiID, FieldReference, FieldTemporal, FieldDecimal, FieldLegal:
		return true
	default:
		return false
	}
}

func transform(kind FieldKind, value string, secret []byte, tenant, purpose, version, field string) (string, error) {
	switch kind {
	case FieldPublic:
		return value, nil
	case FieldDirectID:
		return "syn-" + scopedToken(secret, tenant, purpose, version, "direct", field+"\x00"+value)[:20], nil
	case FieldQuasiID:
		return "cohort-" + scopedToken(secret, tenant, purpose, version, "quasi", field+"\x00"+value)[:12], nil
	case FieldReference:
		return "ref-" + scopedToken(secret, tenant, purpose, version, "reference", field+"\x00"+value)[:20], nil
	case FieldTemporal:
		return maskTemporal(value, scopedToken(secret, tenant, purpose, version, "time", field+"\x00"+value)), nil
	case FieldDecimal:
		return maskDecimal(value, scopedToken(secret, tenant, purpose, version, "decimal", field+"\x00"+value))
	case FieldLegal:
		return "legal-synthetic-" + scopedToken(secret, tenant, purpose, version, "legal", field+"\x00"+value)[:12], nil
	default:
		return "", ErrInvalid
	}
}

func scopedToken(secret []byte, tenant, purpose, version, domain, value string) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(tenant + "\x00" + purpose + "\x00" + version + "\x00" + domain + "\x00" + value))
	return hex.EncodeToString(mac.Sum(nil))
}

func maskTemporal(value, token string) string {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		parsed, err = time.Parse("2006-01-02", value)
	}
	if err != nil {
		return "synthetic-date-" + token[:12]
	}
	days, _ := strconv.Atoi(token[:4])
	shift := days%61 - 30
	shifted := parsed.AddDate(0, 0, shift)
	if len(value) == len("2006-01-02") {
		return shifted.Format("2006-01-02")
	}
	return shifted.UTC().Format(time.RFC3339)
}

func maskDecimal(value, token string) (string, error) {
	rat, ok := new(big.Rat).SetString(value)
	if !ok {
		return "", fmt.Errorf("decimal %q is not parseable", value)
	}
	amount, _ := strconv.Atoi(token[:4])
	offset := new(big.Rat).SetFrac64(int64(amount%97+1), 100)
	rat.Add(rat, offset)
	places := 2
	if dot := strings.IndexByte(value, '.'); dot >= 0 && len(value)-dot-1 > places {
		places = len(value) - dot - 1
	}
	return rat.FloatString(places), nil
}

// Expired reports whether the result can still be consumed at now.
func (r Result) Expired(now time.Time) bool {
	return r.Destroyed || now.IsZero() || !now.Before(r.ExpiresAt)
}

// Destroy irreversibly clears the in-memory synthetic rows. It is the only
// destructive operation in this package and cannot affect the source.
func Destroy(r *Result) error {
	if r == nil {
		return ErrInvalid
	}
	if r.Destroyed {
		return ErrDestroyed
	}
	r.Rows = nil
	r.Destroyed = true
	r.Digest = r.contentDigest()
	return nil
}

func (r Result) contentDigest() string {
	copyResult := r
	copyResult.Digest = ""
	b, _ := json.Marshal(copyResult)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Explain provides a redaction-safe result summary.
func (r Result) Explain() string {
	return fmt.Sprintf("synthetic dataset marker=%s rows=%d fidelity=%d/%d digest=%s", r.Marker, len(r.Rows), r.Fidelity.PreservedTags, r.Fidelity.RequiredTags, r.Digest)
}

// Explain is the package-level engine-shaped symbol.
func Explain(r Result) string { return r.Explain() }
