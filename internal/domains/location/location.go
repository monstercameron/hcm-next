// Package location owns canonical postal addresses, effective-dated work
// locations and worksites. It resolves only against a declared jurisdiction
// table; it does not infer a legal jurisdiction from a geocoder or tenant
// default.
package location

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const schemaVersion = 1

// Version reports this package's immutable vocabulary version.
func Version() int { return schemaVersion }

var (
	ErrInvalidAddress           = errors.New("location: invalid address")
	ErrInvalidWorkLocation      = errors.New("location: invalid work location revision")
	ErrInvalidWorksite          = errors.New("location: invalid worksite revision")
	ErrInvalidJurisdictionTable = errors.New("location: invalid jurisdiction table")
	ErrJurisdictionUnknown      = errors.New("location: jurisdiction is not declared for worksite")
	ErrJurisdictionConflict     = errors.New("location: jurisdiction table has conflicting matches")
	ErrWorksiteNotFound         = errors.New("location: worksite not found")
)

// FieldError identifies the exact field that made a value invalid.
type FieldError struct {
	Domain error
	Field  string
	Reason string
}

func (e *FieldError) Error() string {
	return fmt.Sprintf("%s: field=%s: %s", e.Domain, e.Field, e.Reason)
}
func (e *FieldError) Unwrap() error { return e.Domain }
func fieldError(domain error, field, reason string) error {
	return &FieldError{Domain: domain, Field: field, Reason: reason}
}

func normalizeComponent(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}
func normalizeCode(s string) string { return strings.ToUpper(normalizeComponent(s)) }
func validCode(s string, length int) bool {
	if len(s) != length {
		return false
	}
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}
func validSubdivision(s string) bool {
	if s == "" {
		return true
	}
	for _, r := range s {
		if !(unicode.IsUpper(r) || unicode.IsDigit(r) || r == '-') {
			return false
		}
	}
	return true
}

// Address is the canonical, provider-independent postal address value. Lines
// is the normalized representation; Line1 and Line2 are retained as ergonomic
// aliases for callers building ordinary two-line addresses.
type Address struct {
	Lines              []string
	Line1              string
	Line2              string
	Locality           string
	AdministrativeArea string
	PostalCode         string
	CountryCode        string
	SubdivisionCode    string
	CanonicalDigest    string
}

func (a Address) normalized() Address {
	out := a
	out.Lines = append([]string(nil), a.Lines...)
	if len(out.Lines) == 0 {
		for _, line := range []string{a.Line1, a.Line2} {
			if n := normalizeComponent(line); n != "" {
				out.Lines = append(out.Lines, n)
			}
		}
	} else {
		for i, line := range out.Lines {
			out.Lines[i] = normalizeComponent(line)
		}
	}
	if len(out.Lines) > 0 {
		out.Line1 = out.Lines[0]
	}
	if len(out.Lines) > 1 {
		out.Line2 = out.Lines[1]
	}
	if len(out.Lines) > 2 {
		out.Line2 = strings.Join(out.Lines[1:], " | ")
	}
	out.Locality = normalizeComponent(a.Locality)
	out.AdministrativeArea = normalizeComponent(a.AdministrativeArea)
	out.PostalCode = normalizeComponent(a.PostalCode)
	out.CountryCode = normalizeCode(a.CountryCode)
	out.SubdivisionCode = normalizeCode(a.SubdivisionCode)
	return out
}
func (a Address) Validate() error {
	n := a.normalized()
	if len(n.Lines) == 0 {
		return fieldError(ErrInvalidAddress, "lines", "at least one normalized line is required")
	}
	if strings.TrimSpace(n.Locality) == "" {
		return fieldError(ErrInvalidAddress, "locality", "is required")
	}
	if !validCode(n.CountryCode, 2) {
		return fieldError(ErrInvalidAddress, "country_code", "must be a two-letter uppercase code")
	}
	if !validSubdivision(n.SubdivisionCode) {
		return fieldError(ErrInvalidAddress, "subdivision_code", "contains an invalid character")
	}
	if a.CanonicalDigest != "" && a.CanonicalDigest != a.computedDigest() {
		return fieldError(ErrInvalidAddress, "canonical_digest", "does not match address bytes")
	}
	return nil
}
func (a Address) body() []byte {
	n := a.normalized()
	w := canonicalbytes.New("hcmnext.domains.location.Address", schemaVersion).Count("line", len(n.Lines))
	for _, line := range n.Lines {
		w.String("line", line)
	}
	w.String("locality", n.Locality).String("administrative_area", n.AdministrativeArea).String("postal_code", n.PostalCode).String("country_code", n.CountryCode).String("subdivision_code", n.SubdivisionCode)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (a Address) computedDigest() string { return canonicalbytes.Digest(a.body()) }
func (a Address) Canonical() []byte {
	if a.Validate() != nil {
		return nil
	}
	return a.body()
}
func (a Address) Digest() (string, error) {
	if err := a.Validate(); err != nil {
		return "", err
	}
	return a.computedDigest(), nil
}
func (a Address) Identity() (string, error) { return a.Digest() }

// NewAddress normalizes a postal address and records its digest identity.
func NewAddress(a Address) (Address, error) {
	a = a.normalized()
	a.CanonicalDigest = a.computedDigest()
	if err := a.Validate(); err != nil {
		return Address{}, err
	}
	a.Lines = append([]string(nil), a.Lines...)
	return a, nil
}

// Confidence is a closed vocabulary for source quality. It is descriptive and
// never upgrades a candidate into a legal conclusion.
type Confidence string

const (
	ConfidenceUnknown       Confidence = "UNKNOWN"
	ConfidenceLow           Confidence = "LOW"
	ConfidenceMedium        Confidence = "MEDIUM"
	ConfidenceHigh          Confidence = "HIGH"
	ConfidenceAuthoritative Confidence = "AUTHORITATIVE"
	Unknown                            = ConfidenceUnknown
	Low                                = ConfidenceLow
	Medium                             = ConfidenceMedium
	High                               = ConfidenceHigh
)

func (c Confidence) Valid() bool {
	switch c {
	case ConfidenceUnknown, ConfidenceLow, ConfidenceMedium, ConfidenceHigh, ConfidenceAuthoritative:
		return true
	}
	return false
}

func normalizedCandidates(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		n := normalizeComponent(s)
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// WorkLocation is a logical location revision, distinct from a home address
// or a worksite and carrying its own correction lineage.
type WorkLocationRevision struct {
	LocationID         string
	WorkLocationID     string
	Revision           uint64
	ParentRevision     uint64
	ParentDigest       string
	Address            Address
	LocalityCandidates []string
	TimezoneCandidates []string
	TimeZoneCandidates []string
	SourceAuthority    string
	Confidence         Confidence
	Effective          values.EffectiveInterval
	KnownAt            values.KnownAt
	CanonicalDigest    string
}
type WorkLocation = WorkLocationRevision

func (w WorkLocationRevision) id() string {
	if strings.TrimSpace(w.WorkLocationID) != "" {
		return w.WorkLocationID
	}
	return w.LocationID
}
func (w WorkLocationRevision) candidates() []string {
	if len(w.TimezoneCandidates) > 0 {
		return normalizedCandidates(w.TimezoneCandidates)
	}
	return normalizedCandidates(w.TimeZoneCandidates)
}
func (w WorkLocationRevision) Validate() error {
	if strings.TrimSpace(w.id()) == "" {
		return fieldError(ErrInvalidWorkLocation, "location_id", "is required")
	}
	if w.Revision == 0 {
		return fieldError(ErrInvalidWorkLocation, "revision", "must be positive")
	}
	if w.Revision > 1 && (w.ParentRevision == 0 || w.ParentRevision >= w.Revision || strings.TrimSpace(w.ParentDigest) == "") {
		return fieldError(ErrInvalidWorkLocation, "lineage", "successor requires parent revision and digest")
	}
	if err := w.Address.Validate(); err != nil {
		return fieldError(ErrInvalidWorkLocation, "address", err.Error())
	}
	if err := w.Effective.Validate(); err != nil {
		return fieldError(ErrInvalidWorkLocation, "effective", err.Error())
	}
	if w.KnownAt.Canonical() == nil {
		return fieldError(ErrInvalidWorkLocation, "known_at", "is required")
	}
	if strings.TrimSpace(w.SourceAuthority) == "" {
		return fieldError(ErrInvalidWorkLocation, "source_authority", "is required")
	}
	if !w.Confidence.Valid() {
		return fieldError(ErrInvalidWorkLocation, "confidence", "is not declared")
	}
	if w.CanonicalDigest != "" && w.CanonicalDigest != w.computedDigest() {
		return fieldError(ErrInvalidWorkLocation, "canonical_digest", "does not match location bytes")
	}
	return nil
}
func (w WorkLocationRevision) body() []byte {
	c := w.candidates()
	x := canonicalbytes.New("hcmnext.domains.location.WorkLocationRevision", schemaVersion).String("location_id", w.id()).Int("revision", int64(w.Revision)).Int("parent_revision", int64(w.ParentRevision)).String("parent_digest", w.ParentDigest).Value("address", w.Address).Count("locality_candidate", len(normalizedCandidates(w.LocalityCandidates)))
	for _, v := range normalizedCandidates(w.LocalityCandidates) {
		x.String("locality_candidate", v)
	}
	x.Count("timezone_candidate", len(c))
	for _, v := range c {
		x.String("timezone_candidate", v)
	}
	x.String("source_authority", w.SourceAuthority).String("confidence", string(w.Confidence)).Value("effective", w.Effective).Value("known_at", w.KnownAt)
	b, err := x.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (w WorkLocationRevision) computedDigest() string { return canonicalbytes.Digest(w.body()) }
func (w WorkLocationRevision) Canonical() []byte {
	if w.Validate() != nil {
		return nil
	}
	return w.body()
}
func (w WorkLocationRevision) Digest() (string, error) {
	if err := w.Validate(); err != nil {
		return "", err
	}
	return w.computedDigest(), nil
}
func NewWorkLocationRevision(w WorkLocationRevision) (WorkLocationRevision, error) {
	w.LocalityCandidates = normalizedCandidates(w.LocalityCandidates)
	w.TimezoneCandidates = w.candidates()
	w.CanonicalDigest = w.computedDigest()
	if err := w.Validate(); err != nil {
		return WorkLocationRevision{}, err
	}
	return w, nil
}
func NewWorkLocation(w WorkLocationRevision) (WorkLocationRevision, error) {
	return NewWorkLocationRevision(w)
}
func (w WorkLocationRevision) Successor(next WorkLocationRevision) (WorkLocationRevision, error) {
	if err := w.Validate(); err != nil {
		return WorkLocationRevision{}, err
	}
	next.LocationID, next.WorkLocationID = w.LocationID, w.id()
	next.Revision, next.ParentRevision, next.ParentDigest = w.Revision+1, w.Revision, w.CanonicalDigest
	return NewWorkLocationRevision(next)
}

// Worksite is the physical operating site that may be resolved by a declared
// table. A worksite is not a tax residence or a mailing endpoint.
type WorksiteRevision struct {
	WorksiteID         string
	ID                 string
	Revision           uint64
	ParentRevision     uint64
	ParentDigest       string
	WorkLocationRef    string
	Name               string
	Address            Address
	LocalityCandidates []string
	TimezoneCandidates []string
	TimeZoneCandidates []string
	SourceAuthority    string
	Confidence         Confidence
	Effective          values.EffectiveInterval
	KnownAt            values.KnownAt
	CanonicalDigest    string
}
type Worksite = WorksiteRevision

func (w WorksiteRevision) id() string {
	if strings.TrimSpace(w.WorksiteID) != "" {
		return w.WorksiteID
	}
	return w.ID
}
func (w WorksiteRevision) candidates() []string {
	if len(w.TimezoneCandidates) > 0 {
		return normalizedCandidates(w.TimezoneCandidates)
	}
	return normalizedCandidates(w.TimeZoneCandidates)
}
func (w WorksiteRevision) Validate() error {
	if strings.TrimSpace(w.id()) == "" {
		return fieldError(ErrInvalidWorksite, "worksite_id", "is required")
	}
	if w.Revision == 0 {
		return fieldError(ErrInvalidWorksite, "revision", "must be positive")
	}
	if w.Revision > 1 && (w.ParentRevision == 0 || w.ParentRevision >= w.Revision || strings.TrimSpace(w.ParentDigest) == "") {
		return fieldError(ErrInvalidWorksite, "lineage", "successor requires parent revision and digest")
	}
	if strings.TrimSpace(w.WorkLocationRef) == "" {
		return fieldError(ErrInvalidWorksite, "work_location_ref", "is required")
	}
	if strings.TrimSpace(w.Name) == "" {
		return fieldError(ErrInvalidWorksite, "name", "is required")
	}
	if err := w.Address.Validate(); err != nil {
		return fieldError(ErrInvalidWorksite, "address", err.Error())
	}
	if err := w.Effective.Validate(); err != nil {
		return fieldError(ErrInvalidWorksite, "effective", err.Error())
	}
	if w.KnownAt.Canonical() == nil {
		return fieldError(ErrInvalidWorksite, "known_at", "is required")
	}
	if strings.TrimSpace(w.SourceAuthority) == "" {
		return fieldError(ErrInvalidWorksite, "source_authority", "is required")
	}
	if !w.Confidence.Valid() {
		return fieldError(ErrInvalidWorksite, "confidence", "is not declared")
	}
	if w.CanonicalDigest != "" && w.CanonicalDigest != w.computedDigest() {
		return fieldError(ErrInvalidWorksite, "canonical_digest", "does not match worksite bytes")
	}
	return nil
}
func (w WorksiteRevision) body() []byte {
	c := w.candidates()
	x := canonicalbytes.New("hcmnext.domains.location.WorksiteRevision", schemaVersion).String("worksite_id", w.id()).Int("revision", int64(w.Revision)).Int("parent_revision", int64(w.ParentRevision)).String("parent_digest", w.ParentDigest).String("work_location_ref", w.WorkLocationRef).String("name", normalizeComponent(w.Name)).Value("address", w.Address).Count("locality_candidate", len(normalizedCandidates(w.LocalityCandidates)))
	for _, v := range normalizedCandidates(w.LocalityCandidates) {
		x.String("locality_candidate", v)
	}
	x.Count("timezone_candidate", len(c))
	for _, v := range c {
		x.String("timezone_candidate", v)
	}
	x.String("source_authority", w.SourceAuthority).String("confidence", string(w.Confidence)).Value("effective", w.Effective).Value("known_at", w.KnownAt)
	b, err := x.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (w WorksiteRevision) computedDigest() string { return canonicalbytes.Digest(w.body()) }
func (w WorksiteRevision) Canonical() []byte {
	if w.Validate() != nil {
		return nil
	}
	return w.body()
}
func (w WorksiteRevision) Digest() (string, error) {
	if err := w.Validate(); err != nil {
		return "", err
	}
	return w.computedDigest(), nil
}
func NewWorksiteRevision(w WorksiteRevision) (WorksiteRevision, error) {
	w.Name = normalizeComponent(w.Name)
	w.LocalityCandidates = normalizedCandidates(w.LocalityCandidates)
	w.TimezoneCandidates = w.candidates()
	w.CanonicalDigest = w.computedDigest()
	if err := w.Validate(); err != nil {
		return WorksiteRevision{}, err
	}
	return w, nil
}
func NewWorksite(w WorksiteRevision) (WorksiteRevision, error) { return NewWorksiteRevision(w) }
func (w WorksiteRevision) Successor(next WorksiteRevision) (WorksiteRevision, error) {
	if err := w.Validate(); err != nil {
		return WorksiteRevision{}, err
	}
	next.WorksiteID, next.ID = w.WorksiteID, w.id()
	next.Revision, next.ParentRevision, next.ParentDigest = w.Revision+1, w.Revision, w.CanonicalDigest
	return NewWorksiteRevision(next)
}

type JurisdictionSet struct {
	Federal string
	State   string
	Local   string
}
type JurisdictionRule struct {
	CountryCode     string
	SubdivisionCode string
	Locality        string
	FederalTax      string
	StateTax        string
	LocalTax        string
	FederalLabor    string
	StateLabor      string
	LocalLabor      string
}

func (r JurisdictionRule) normalized() JurisdictionRule {
	r.CountryCode = normalizeCode(r.CountryCode)
	r.SubdivisionCode = normalizeCode(r.SubdivisionCode)
	r.Locality = normalizeComponent(r.Locality)
	return r
}
func (r JurisdictionRule) Validate() error {
	r = r.normalized()
	if !validCode(r.CountryCode, 2) {
		return fieldError(ErrInvalidJurisdictionTable, "country_code", "must be a two-letter uppercase code")
	}
	if !validSubdivision(r.SubdivisionCode) {
		return fieldError(ErrInvalidJurisdictionTable, "subdivision_code", "is invalid")
	}
	for field, v := range map[string]string{"federal_tax": r.FederalTax, "state_tax": r.StateTax, "local_tax": r.LocalTax, "federal_labor": r.FederalLabor, "state_labor": r.StateLabor, "local_labor": r.LocalLabor} {
		if strings.TrimSpace(v) == "" {
			return fieldError(ErrInvalidJurisdictionTable, field, "is required")
		}
	}
	return nil
}
func (r JurisdictionRule) Canonical() []byte {
	r = r.normalized()
	if r.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.location.JurisdictionRule", schemaVersion).String("country", r.CountryCode).String("subdivision", r.SubdivisionCode).String("locality", r.Locality).String("federal_tax", r.FederalTax).String("state_tax", r.StateTax).String("local_tax", r.LocalTax).String("federal_labor", r.FederalLabor).String("state_labor", r.StateLabor).String("local_labor", r.LocalLabor).Bytes()
	if err != nil {
		return nil
	}
	return b
}

type JurisdictionTable struct {
	TableID         string
	Version         string
	Rules           []JurisdictionRule
	CanonicalDigest string
}

func (t JurisdictionTable) Validate() error {
	if strings.TrimSpace(t.TableID) == "" {
		return fieldError(ErrInvalidJurisdictionTable, "table_id", "is required")
	}
	if strings.TrimSpace(t.Version) == "" {
		return fieldError(ErrInvalidJurisdictionTable, "version", "is required")
	}
	if len(t.Rules) == 0 {
		return fieldError(ErrInvalidJurisdictionTable, "rules", "at least one is required")
	}
	seen := map[string]bool{}
	for i, r := range t.Rules {
		if err := r.Validate(); err != nil {
			return fieldError(ErrInvalidJurisdictionTable, fmt.Sprintf("rules[%d]", i), err.Error())
		}
		r = r.normalized()
		key := r.CountryCode + "\x00" + r.SubdivisionCode + "\x00" + r.Locality
		if seen[key] {
			return fieldError(ErrInvalidJurisdictionTable, "rules", "duplicate selector")
		}
		seen[key] = true
	}
	if t.CanonicalDigest != "" && t.CanonicalDigest != t.computedDigest() {
		return fieldError(ErrInvalidJurisdictionTable, "canonical_digest", "does not match table bytes")
	}
	return nil
}
func (t JurisdictionTable) body() []byte {
	rules := append([]JurisdictionRule(nil), t.Rules...)
	sort.Slice(rules, func(i, j int) bool { return string(rules[i].Canonical()) < string(rules[j].Canonical()) })
	w := canonicalbytes.New("hcmnext.domains.location.JurisdictionTable", schemaVersion).String("table_id", t.TableID).String("version", t.Version).Count("rule", len(rules))
	for _, r := range rules {
		w.Value("rule", r)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (t JurisdictionTable) computedDigest() string { return canonicalbytes.Digest(t.body()) }
func (t JurisdictionTable) Canonical() []byte {
	if t.Validate() != nil {
		return nil
	}
	return t.body()
}
func (t JurisdictionTable) Digest() (string, error) {
	if err := t.Validate(); err != nil {
		return "", err
	}
	return t.computedDigest(), nil
}
func NewJurisdictionTable(t JurisdictionTable) (JurisdictionTable, error) {
	for i := range t.Rules {
		t.Rules[i] = t.Rules[i].normalized()
	}
	t.Rules = append([]JurisdictionRule(nil), t.Rules...)
	t.CanonicalDigest = t.computedDigest()
	if err := t.Validate(); err != nil {
		return JurisdictionTable{}, err
	}
	return t, nil
}

type JurisdictionResolution struct {
	WorksiteID      string
	TableID         string
	TableVersion    string
	Tax             JurisdictionSet
	Labor           JurisdictionSet
	FederalTax      string
	StateTax        string
	LocalTax        string
	FederalLabor    string
	StateLabor      string
	LocalLabor      string
	CanonicalDigest string
}

func (r JurisdictionResolution) Validate() error {
	if strings.TrimSpace(r.WorksiteID) == "" {
		return fieldError(ErrJurisdictionUnknown, "worksite_id", "is required")
	}
	if strings.TrimSpace(r.TableID) == "" || strings.TrimSpace(r.TableVersion) == "" {
		return fieldError(ErrJurisdictionUnknown, "table", "identity is required")
	}
	for field, v := range map[string]string{"federal_tax": r.FederalTax, "state_tax": r.StateTax, "local_tax": r.LocalTax, "federal_labor": r.FederalLabor, "state_labor": r.StateLabor, "local_labor": r.LocalLabor} {
		if v == "" {
			return fieldError(ErrJurisdictionUnknown, field, "is not declared")
		}
	}
	return nil
}
func (r JurisdictionResolution) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.location.JurisdictionResolution", schemaVersion).String("worksite_id", r.WorksiteID).String("table_id", r.TableID).String("table_version", r.TableVersion).String("federal_tax", r.FederalTax).String("state_tax", r.StateTax).String("local_tax", r.LocalTax).String("federal_labor", r.FederalLabor).String("state_labor", r.StateLabor).String("local_labor", r.LocalLabor).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// JurisdictionResolver only selects a row declared in Table. A row with more
// specific subdivision/locality selectors wins; equal-specificity conflicts
// are refused rather than resolved by map or insertion order.
type JurisdictionResolver struct{ Table JurisdictionTable }

func NewJurisdictionResolver(t JurisdictionTable) (JurisdictionResolver, error) {
	if err := t.Validate(); err != nil {
		return JurisdictionResolver{}, err
	}
	return JurisdictionResolver{Table: t}, nil
}
func (r JurisdictionResolver) Resolve(w WorksiteRevision) (JurisdictionResolution, error) {
	if err := w.Validate(); err != nil {
		return JurisdictionResolution{}, err
	}
	if err := r.Table.Validate(); err != nil {
		return JurisdictionResolution{}, err
	}
	address := w.Address.normalized()
	country := address.CountryCode
	subdivision := address.SubdivisionCode
	locality := address.Locality
	best := -1
	var chosen JurisdictionRule
	for _, candidate := range r.Table.Rules {
		rule := candidate.normalized()
		if rule.CountryCode != country || (rule.SubdivisionCode != "" && rule.SubdivisionCode != subdivision) || (rule.Locality != "" && rule.Locality != locality) {
			continue
		}
		specificity := 0
		if rule.SubdivisionCode != "" {
			specificity++
		}
		if rule.Locality != "" {
			specificity++
		}
		if specificity > best {
			best = specificity
			chosen = rule
		} else if specificity == best && string(rule.Canonical()) != string(chosen.Canonical()) {
			return JurisdictionResolution{}, ErrJurisdictionConflict
		}
	}
	if best < 0 {
		return JurisdictionResolution{}, ErrJurisdictionUnknown
	}
	out := JurisdictionResolution{WorksiteID: w.id(), TableID: r.Table.TableID, TableVersion: r.Table.Version, FederalTax: chosen.FederalTax, StateTax: chosen.StateTax, LocalTax: chosen.LocalTax, FederalLabor: chosen.FederalLabor, StateLabor: chosen.StateLabor, LocalLabor: chosen.LocalLabor}
	out.Tax = JurisdictionSet{Federal: out.FederalTax, State: out.StateTax, Local: out.LocalTax}
	out.Labor = JurisdictionSet{Federal: out.FederalLabor, State: out.StateLabor, Local: out.LocalLabor}
	out.CanonicalDigest = canonicalbytes.Digest(out.Canonical())
	return out, nil
}
func ResolveWorksite(t JurisdictionTable, w WorksiteRevision) (JurisdictionResolution, error) {
	r, err := NewJurisdictionResolver(t)
	if err != nil {
		return JurisdictionResolution{}, err
	}
	return r.Resolve(w)
}

// WorksiteReader and InMemoryWorksiteStore are deliberately small adapter
// seams for tests and future providers; no database is implied.
type WorksiteReader interface {
	Worksite(context.Context, string, uint64) (WorksiteRevision, error)
}
type InMemoryWorksiteStore struct {
	mu    sync.RWMutex
	items map[string]WorksiteRevision
}

func NewInMemoryWorksiteStore() *InMemoryWorksiteStore {
	return &InMemoryWorksiteStore{items: make(map[string]WorksiteRevision)}
}
func (s *InMemoryWorksiteStore) Put(ctx context.Context, w WorksiteRevision) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := w.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items == nil {
		s.items = make(map[string]WorksiteRevision)
	}
	s.items[w.id()+fmt.Sprintf("\x00%d", w.Revision)] = w
	return nil
}
func (s *InMemoryWorksiteStore) Worksite(ctx context.Context, id string, revision uint64) (WorksiteRevision, error) {
	if err := ctx.Err(); err != nil {
		return WorksiteRevision{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	w, ok := s.items[id+fmt.Sprintf("\x00%d", revision)]
	if !ok {
		return WorksiteRevision{}, ErrWorksiteNotFound
	}
	w.LocalityCandidates = append([]string(nil), w.LocalityCandidates...)
	w.TimezoneCandidates = append([]string(nil), w.TimezoneCandidates...)
	return w, nil
}

// Explain returns audit-safe identity facts. It never repeats address lines,
// postal codes, worker-like references, source payloads, or evidence values.
type Explanation struct {
	Revision               uint64
	Confidence             Confidence
	LocalityCandidateCount int
	TimezoneCandidateCount int
	HasAddress             bool
	Digest                 string
}

func (w WorkLocationRevision) Explain() (Explanation, error) {
	if err := w.Validate(); err != nil {
		return Explanation{}, err
	}
	return Explanation{Revision: w.Revision, Confidence: w.Confidence, LocalityCandidateCount: len(w.LocalityCandidates), TimezoneCandidateCount: len(w.candidates()), HasAddress: true, Digest: w.CanonicalDigest}, nil
}
func (w WorksiteRevision) Explain() (Explanation, error) {
	if err := w.Validate(); err != nil {
		return Explanation{}, err
	}
	return Explanation{Revision: w.Revision, Confidence: w.Confidence, LocalityCandidateCount: len(w.LocalityCandidates), TimezoneCandidateCount: len(w.candidates()), HasAddress: true, Digest: w.CanonicalDigest}, nil
}
func Explain(w WorksiteRevision) (Explanation, error) { return w.Explain() }
