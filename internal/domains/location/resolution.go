package location

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
)

// ResolutionStatus is the epistemic state of address resolution. It describes
// the evidence returned by this package; it is never a legal conclusion.
type ResolutionStatus string

const (
	ResolutionResolved  ResolutionStatus = "RESOLVED"
	ResolutionAmbiguous ResolutionStatus = "AMBIGUOUS"
	ResolutionPartial   ResolutionStatus = "PARTIAL"
	ResolutionUnknown   ResolutionStatus = "UNKNOWN"

	Resolved          = ResolutionResolved
	Ambiguous         = ResolutionAmbiguous
	Partial           = ResolutionPartial
	UnknownResolution = ResolutionUnknown
)

func (s ResolutionStatus) String() string { return string(s) }

func (s ResolutionStatus) Valid() bool {
	switch s {
	case ResolutionResolved, ResolutionAmbiguous, ResolutionPartial, ResolutionUnknown:
		return true
	default:
		return false
	}
}

var (
	ErrInvalidResolutionDataset = errors.New("location: invalid resolution dataset")
	ErrInvalidResolutionRequest = errors.New("location: invalid resolution request")
)

// ResolutionCandidate is one ranked, descriptive candidate. Release is the
// exact dataset release that supplied the candidate, and TZDBVersion is set
// for timezone candidates. A candidate is not an assertion that the value is
// legally applicable.
type ResolutionCandidate struct {
	Value       string
	Rank        int
	Confidence  Confidence
	Source      string
	Release     string
	TZDBVersion string
}

func (c ResolutionCandidate) Validate() error {
	if strings.TrimSpace(c.Value) == "" || c.Rank < 1 || !c.Confidence.Valid() {
		return fmt.Errorf("%w: candidate needs value, positive rank and declared confidence", ErrInvalidResolutionDataset)
	}
	if strings.TrimSpace(c.Source) == "" || strings.TrimSpace(c.Release) == "" {
		return fmt.Errorf("%w: candidate needs source and release", ErrInvalidResolutionDataset)
	}
	return nil
}

func (c ResolutionCandidate) canonical() []byte {
	w := canonicalbytes.New("hcmnext.domains.location.ResolutionCandidate", schemaVersion).
		String("value", c.Value).
		Int("rank", int64(c.Rank)).
		String("confidence", string(c.Confidence)).
		String("source", c.Source).
		String("release", c.Release).
		String("tzdb_version", c.TZDBVersion)
	b, _ := w.Bytes()
	return b
}

// LocalityRule declares a candidate locality in a pinned reference release.
// An empty selector is a wildcard. PostalPrefix is matched against the
// normalized postal code and never changes the submitted postal evidence.
type LocalityRule struct {
	CountryCode     string
	SubdivisionCode string
	PostalPrefix    string
	Locality        string
	Release         string
	Confidence      Confidence
}

// TimezoneRule declares a timezone candidate in a pinned tzdb release. This
// package deliberately does not call time.LoadLocation: host tzdb state and
// network/provider behavior must not affect a resolution result.
type TimezoneRule struct {
	CountryCode     string
	SubdivisionCode string
	PostalPrefix    string
	Locality        string
	Timezone        string
	TimezoneID      string
	Release         string
	TZDBVersion     string
	Confidence      Confidence
}

func (r LocalityRule) normalized(datasetRelease string) (LocalityRule, error) {
	r.CountryCode = normalizeCode(r.CountryCode)
	r.SubdivisionCode = normalizeCode(r.SubdivisionCode)
	r.PostalPrefix = normalizeComponent(r.PostalPrefix)
	r.Locality = normalizeComponent(r.Locality)
	if r.Release == "" {
		r.Release = datasetRelease
	}
	if r.Confidence == "" {
		r.Confidence = ConfidenceMedium
	}
	if !validCode(r.CountryCode, 2) || r.Locality == "" || r.Release == "" || !r.Confidence.Valid() {
		return LocalityRule{}, fmt.Errorf("%w: locality rule is incomplete", ErrInvalidResolutionDataset)
	}
	return r, nil
}

func (r TimezoneRule) normalized(datasetRelease, datasetTZDB string) (TimezoneRule, error) {
	r.CountryCode = normalizeCode(r.CountryCode)
	r.SubdivisionCode = normalizeCode(r.SubdivisionCode)
	r.PostalPrefix = normalizeComponent(r.PostalPrefix)
	r.Locality = normalizeComponent(r.Locality)
	if r.Timezone == "" {
		r.Timezone = r.TimezoneID
	}
	if r.Release == "" {
		r.Release = datasetRelease
	}
	if r.TZDBVersion == "" {
		r.TZDBVersion = datasetTZDB
	}
	if r.Confidence == "" {
		r.Confidence = ConfidenceHigh
	}
	if !validCode(r.CountryCode, 2) || r.Timezone == "" || r.Release == "" || r.TZDBVersion == "" || !r.Confidence.Valid() {
		return TimezoneRule{}, fmt.Errorf("%w: timezone rule is incomplete", ErrInvalidResolutionDataset)
	}
	return r, nil
}

// ResolutionDataset is the complete, pinned input to a Resolver. Jurisdiction
// candidates are read only from JurisdictionTable; no provider or tenant
// default is consulted.
type ResolutionDataset struct {
	DatasetID               string
	Version                 string
	Release                 string
	TZDBVersion             string
	TimezoneDatabaseVersion string
	LocalityRules           []LocalityRule
	TimezoneRules           []TimezoneRule
	JurisdictionTable       JurisdictionTable
}

func (d ResolutionDataset) release() string {
	if strings.TrimSpace(d.Release) != "" {
		return strings.TrimSpace(d.Release)
	}
	return strings.TrimSpace(d.Version)
}

func (d ResolutionDataset) tzdbVersion() string {
	if strings.TrimSpace(d.TZDBVersion) != "" {
		return strings.TrimSpace(d.TZDBVersion)
	}
	return strings.TrimSpace(d.TimezoneDatabaseVersion)
}

func (d ResolutionDataset) normalized() (ResolutionDataset, error) {
	d.Release = d.release()
	d.Version = d.Release
	d.TZDBVersion = d.tzdbVersion()
	if d.Release == "" || d.TZDBVersion == "" {
		return ResolutionDataset{}, fmt.Errorf("%w: release and tzdb version are required", ErrInvalidResolutionDataset)
	}
	if d.JurisdictionTable.TableID != "" {
		table, err := NewJurisdictionTable(d.JurisdictionTable)
		if err != nil {
			return ResolutionDataset{}, err
		}
		d.JurisdictionTable = table
	}
	localityRules := append([]LocalityRule(nil), d.LocalityRules...)
	d.LocalityRules = make([]LocalityRule, len(localityRules))
	for i, rule := range localityRules {
		normalized, err := rule.normalized(d.Release)
		if err != nil {
			return ResolutionDataset{}, err
		}
		d.LocalityRules[i] = normalized
	}
	timezoneRules := append([]TimezoneRule(nil), d.TimezoneRules...)
	d.TimezoneRules = make([]TimezoneRule, len(timezoneRules))
	for i, rule := range timezoneRules {
		normalized, err := rule.normalized(d.Release, d.TZDBVersion)
		if err != nil {
			return ResolutionDataset{}, err
		}
		d.TimezoneRules[i] = normalized
	}
	return d, nil
}

// Validate checks that the dataset is pinned and that every declared rule is
// usable without consulting ambient host data.
func (d ResolutionDataset) Validate() error {
	_, err := d.normalized()
	return err
}

// ResolutionRequest supplies address evidence. OriginalAddress is retained in
// the result before normalization, so postal normalization never destroys the
// source evidence. Dataset is copied and validated by ResolveAddress.
type ResolutionRequest struct {
	Address Address
	Dataset ResolutionDataset
}

// LocationResolution is a pure resolution result. It contains ranked
// candidates and provenance, not a legal or tax decision.
type LocationResolution struct {
	OriginalAddress        Address
	Address                Address
	NormalizedAddress      Address
	LocalityCandidates     []ResolutionCandidate
	TimezoneCandidates     []ResolutionCandidate
	JurisdictionCandidates []ResolutionCandidate
	Status                 ResolutionStatus
	DatasetVersion         string
	TZDBVersion            string
	CanonicalDigest        string
}

func (r LocationResolution) Validate() error {
	if !r.Status.Valid() || r.DatasetVersion == "" || r.TZDBVersion == "" || r.CanonicalDigest == "" {
		return fmt.Errorf("%w: incomplete resolution", ErrInvalidResolutionRequest)
	}
	for _, candidates := range [][]ResolutionCandidate{r.LocalityCandidates, r.TimezoneCandidates, r.JurisdictionCandidates} {
		for _, candidate := range candidates {
			if err := candidate.Validate(); err != nil {
				return err
			}
		}
	}
	return nil
}

type Resolver struct{ dataset ResolutionDataset }

func NewResolver(dataset ResolutionDataset) (Resolver, error) {
	dataset, err := dataset.normalized()
	if err != nil {
		return Resolver{}, err
	}
	return Resolver{dataset: dataset}, nil
}

func ResolveAddress(req ResolutionRequest) (LocationResolution, error) {
	resolver, err := NewResolver(req.Dataset)
	if err != nil {
		return LocationResolution{}, err
	}
	return resolver.Resolve(req.Address)
}

// Resolve normalizes the address and ranks only candidates supported by the
// pinned dataset. Incomplete address evidence returns PARTIAL or UNKNOWN with
// nil error; malformed dataset configuration is the error case.
func (r Resolver) Resolve(input Address) (LocationResolution, error) {
	if r.dataset.Release == "" || r.dataset.TZDBVersion == "" {
		return LocationResolution{}, fmt.Errorf("%w: resolver has no pinned dataset", ErrInvalidResolutionDataset)
	}
	normalized := input.normalized()
	if err := normalized.Validate(); err == nil {
		normalized.CanonicalDigest = normalized.computedDigest()
	}
	out := LocationResolution{
		OriginalAddress:   cloneAddress(input),
		Address:           normalized,
		NormalizedAddress: normalized,
		DatasetVersion:    r.dataset.Release,
		TZDBVersion:       r.dataset.TZDBVersion,
	}
	out.LocalityCandidates = r.localities(normalized)
	out.TimezoneCandidates = r.timezones(normalized)
	out.JurisdictionCandidates = r.jurisdictions(normalized)
	out.Status = resolutionStatus(out.LocalityCandidates, out.TimezoneCandidates, out.JurisdictionCandidates)
	out.CanonicalDigest = canonicalbytes.Digest(out.canonical())
	return out, nil
}

func cloneAddress(a Address) Address {
	a.Lines = append([]string(nil), a.Lines...)
	return a
}

func (r Resolver) localities(a Address) []ResolutionCandidate {
	var candidates []ResolutionCandidate
	if locality := normalizeComponent(a.Locality); locality != "" {
		candidates = append(candidates, ResolutionCandidate{Value: locality, Rank: 1, Confidence: ConfidenceLow, Source: "address-input", Release: r.dataset.Release})
	}
	for _, rule := range r.dataset.LocalityRules {
		if !matchesSelector(rule.CountryCode, rule.SubdivisionCode, rule.PostalPrefix, rule.Locality, a) {
			continue
		}
		candidates = append(candidates, ResolutionCandidate{Value: rule.Locality, Rank: selectorRank(rule.CountryCode, rule.SubdivisionCode, rule.PostalPrefix, rule.Locality, a), Confidence: rule.Confidence, Source: "locality-dataset", Release: rule.Release})
	}
	return dedupeCandidates(candidates)
}

func (r Resolver) timezones(a Address) []ResolutionCandidate {
	var candidates []ResolutionCandidate
	for _, rule := range r.dataset.TimezoneRules {
		if !matchesSelector(rule.CountryCode, rule.SubdivisionCode, rule.PostalPrefix, rule.Locality, a) {
			continue
		}
		candidates = append(candidates, ResolutionCandidate{Value: rule.Timezone, Rank: selectorRank(rule.CountryCode, rule.SubdivisionCode, rule.PostalPrefix, rule.Locality, a), Confidence: rule.Confidence, Source: "tzdb-dataset", Release: rule.Release, TZDBVersion: rule.TZDBVersion})
	}
	return dedupeCandidates(candidates)
}

func (r Resolver) jurisdictions(a Address) []ResolutionCandidate {
	table := r.dataset.JurisdictionTable
	if table.TableID == "" {
		return nil
	}
	var candidates []ResolutionCandidate
	for _, candidate := range table.Rules {
		rule := candidate.normalized()
		if rule.CountryCode != a.CountryCode || (rule.SubdivisionCode != "" && rule.SubdivisionCode != a.SubdivisionCode) || (rule.Locality != "" && rule.Locality != a.Locality) {
			continue
		}
		specificity := 0
		if rule.SubdivisionCode != "" {
			specificity++
		}
		if rule.Locality != "" {
			specificity++
		}
		value := strings.Join([]string{rule.CountryCode, rule.SubdivisionCode, rule.Locality}, "|")
		confidence := ConfidenceMedium
		if specificity == 2 {
			confidence = ConfidenceHigh
		}
		candidates = append(candidates, ResolutionCandidate{Value: value, Rank: 3 - specificity, Confidence: confidence, Source: "jurisdiction-table", Release: table.Version})
	}
	return dedupeCandidates(candidates)
}

func matchesSelector(country, subdivision, postalPrefix, locality string, a Address) bool {
	return country == a.CountryCode &&
		(subdivision == "" || subdivision == a.SubdivisionCode) &&
		(postalPrefix == "" || strings.HasPrefix(a.PostalCode, postalPrefix)) &&
		(locality == "" || locality == a.Locality)
}

func selectorRank(country, subdivision, postalPrefix, locality string, a Address) int {
	specificity := 0
	if country != "" {
		specificity++
	}
	if subdivision != "" {
		specificity++
	}
	if postalPrefix != "" {
		specificity++
	}
	if locality != "" {
		specificity++
	}
	return 5 - specificity
}

func dedupeCandidates(in []ResolutionCandidate) []ResolutionCandidate {
	byValue := make(map[string]ResolutionCandidate, len(in))
	for _, candidate := range in {
		if prior, ok := byValue[candidate.Value]; !ok || candidate.Rank < prior.Rank || (candidate.Rank == prior.Rank && string(candidate.Confidence) > string(prior.Confidence)) {
			byValue[candidate.Value] = candidate
		}
	}
	out := make([]ResolutionCandidate, 0, len(byValue))
	for _, candidate := range byValue {
		out = append(out, candidate)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Rank != out[j].Rank {
			return out[i].Rank < out[j].Rank
		}
		return out[i].Value < out[j].Value
	})
	return out
}

func topCandidateCount(candidates []ResolutionCandidate) int {
	if len(candidates) == 0 {
		return 0
	}
	top := candidates[0].Rank
	count := 0
	for _, candidate := range candidates {
		if candidate.Rank == top {
			count++
		}
	}
	return count
}

func resolutionStatus(sets ...[]ResolutionCandidate) ResolutionStatus {
	any := false
	for _, candidates := range sets {
		if len(candidates) == 0 {
			continue
		}
		any = true
		if topCandidateCount(candidates) > 1 {
			return ResolutionAmbiguous
		}
	}
	if !any {
		return ResolutionUnknown
	}
	for _, candidates := range sets {
		if len(candidates) == 0 {
			return ResolutionPartial
		}
	}
	return ResolutionResolved
}

func (r LocationResolution) canonical() []byte {
	w := canonicalbytes.New("hcmnext.domains.location.LocationResolution", schemaVersion).
		String("status", string(r.Status)).Value("address", r.NormalizedAddress).
		String("dataset_version", r.DatasetVersion).String("tzdb_version", r.TZDBVersion).
		Count("locality_candidate", len(r.LocalityCandidates))
	for _, candidate := range r.LocalityCandidates {
		w.Field("locality_candidate", candidate.canonical())
	}
	w.Count("timezone_candidate", len(r.TimezoneCandidates))
	for _, candidate := range r.TimezoneCandidates {
		w.Field("timezone_candidate", candidate.canonical())
	}
	w.Count("jurisdiction_candidate", len(r.JurisdictionCandidates))
	for _, candidate := range r.JurisdictionCandidates {
		w.Field("jurisdiction_candidate", candidate.canonical())
	}
	b, _ := w.Bytes()
	return b
}

// ResolutionExplanation is safe for operational logs and omits source lines,
// postal codes and candidate values.
type ResolutionExplanation struct {
	Status                     ResolutionStatus
	LocalityCandidateCount     int
	TimezoneCandidateCount     int
	JurisdictionCandidateCount int
	DatasetVersion             string
	TZDBVersion                string
	Digest                     string
}

func (r LocationResolution) Explain() (ResolutionExplanation, error) {
	if err := r.Validate(); err != nil {
		return ResolutionExplanation{}, err
	}
	return ResolutionExplanation{Status: r.Status, LocalityCandidateCount: len(r.LocalityCandidates), TimezoneCandidateCount: len(r.TimezoneCandidates), JurisdictionCandidateCount: len(r.JurisdictionCandidates), DatasetVersion: r.DatasetVersion, TZDBVersion: r.TZDBVersion, Digest: r.CanonicalDigest}, nil
}

func ExplainResolution(r LocationResolution) (ResolutionExplanation, error) { return r.Explain() }
