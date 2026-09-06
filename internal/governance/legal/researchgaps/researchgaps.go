// Package researchgaps records employment-law topics that must be researched
// before a rule pack may claim coverage. It does not implement scheduling.
package researchgaps

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/hcm-next/internal/governance/legal"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

//go:embed testdata/predictive-scheduling.yaml
var fixtureFS embed.FS

const schemaVersion = 1

// ReviewFlag is the review state of a finding, not a claim that the law
// exists. A missing finding is always unreviewed until the research corpus is
// expanded.
type ReviewFlag string

const (
	ReviewUnreviewed ReviewFlag = "UNREVIEWED"
	ReviewReviewed   ReviewFlag = "REVIEWED"
)

// FindingStatus is the matrix value a later research pass must record.
type FindingStatus string

const (
	StatusResearchMissing FindingStatus = "RESEARCH_MISSING"
	StatusYes             FindingStatus = "Y"
	StatusLocal           FindingStatus = "L"
	StatusFederal         FindingStatus = "F"
	StatusUncertain       FindingStatus = "?"
)

// Citation identifies the statute or ordinance that the next research pass
// must confirm. It is a lead, not an executable legal rule.
type Citation struct {
	SourceFile string `yaml:"source_file" json:"source_file"`
	Section    string `yaml:"section" json:"section"`
	Authority  string `yaml:"authority" json:"authority"`
}

// Entry is one known predictive-scheduling or fair-workweek jurisdiction.
type Entry struct {
	ID             string             `yaml:"id" json:"id"`
	Name           string             `yaml:"name" json:"name"`
	Jurisdiction   legal.Jurisdiction `yaml:"jurisdiction" json:"jurisdiction"`
	Status         FindingStatus      `yaml:"status" json:"status"`
	Review         ReviewFlag         `yaml:"review" json:"review"`
	ResearchFile   string             `yaml:"research_file" json:"research_file"`
	Citation       Citation           `yaml:"citation" json:"citation"`
	RequiredFields []string           `yaml:"required_pack_fields" json:"required_pack_fields"`
}

// PublicationEvidence is supplied by the authoring pipeline when it tries
// to publish a pack for a gap jurisdiction.
type PublicationEvidence struct {
	JurisdictionID   string
	ResearchFile     string
	ReviewedResearch bool
}

// Registry is a versioned, effective-dated, known-at, digested gap schema.
type Registry struct {
	SchemaVersion int
	Version       int
	Window        legal.EffectiveWindow
	KnownAt       values.KnownAt
	Entries       []Entry
	Digest        string
}

// Errors are typed and field-oriented so authoring can refuse a bad row or a
// premature pack without guessing what the author intended.
var (
	ErrValidation    = errors.New("researchgaps: VALIDATION_FAILED")
	ErrField         = errors.New("researchgaps: invalid field")
	ErrUnknownEntry  = errors.New("researchgaps: jurisdiction is not registered")
	ErrPackBlocked   = errors.New("researchgaps: PACK_PUBLICATION_BLOCKED")
	ErrEffectiveDate = errors.New("researchgaps: effective date is outside registry window")
	ErrKnownAt       = errors.New("researchgaps: known-at is earlier than registry knowledge")
)

// FieldError names the rejected field.
type FieldError struct {
	Field string
	Cause error
}

func (e *FieldError) Error() string {
	return fmt.Sprintf("researchgaps: field %s: %v", e.Field, e.Cause)
}
func (e *FieldError) Unwrap() error { return e.Cause }

func fieldError(field string, cause error) error {
	return fmt.Errorf("%w: %w: %w", ErrValidation, ErrField, &FieldError{Field: field, Cause: cause})
}

func validFindingStatus(s FindingStatus) bool {
	return s == StatusResearchMissing || s == StatusYes || s == StatusLocal || s == StatusFederal || s == StatusUncertain
}

func validReview(r ReviewFlag) bool { return r == ReviewReviewed || r == ReviewUnreviewed }

// Validate rejects a row that could be mistaken for researched law without a
// source, required pack shape, or review flag.
func (r Registry) Validate() error {
	if r.SchemaVersion != schemaVersion {
		return fieldError("schema_version", fmt.Errorf("want %d, got %d", schemaVersion, r.SchemaVersion))
	}
	if r.Version <= 0 {
		return fieldError("version", errors.New("must be positive"))
	}
	if err := r.Window.Validate(); err != nil {
		return fieldError("effective_window", err)
	}
	if err := r.KnownAt.Instant().Validate(); err != nil {
		return fieldError("known_at", err)
	}
	if len(r.Entries) == 0 {
		return fieldError("entries", errors.New("must contain at least one jurisdiction"))
	}
	seen := map[string]struct{}{}
	for i, entry := range r.Entries {
		prefix := fmt.Sprintf("entries[%d]", i)
		if entry.ID == "" {
			return fieldError(prefix+".id", errors.New("is required"))
		}
		if _, exists := seen[entry.ID]; exists {
			return fieldError(prefix+".id", errors.New("is duplicated"))
		}
		seen[entry.ID] = struct{}{}
		if entry.Name == "" {
			return fieldError(prefix+".name", errors.New("is required"))
		}
		if err := entry.Jurisdiction.Validate(); err != nil {
			return fieldError(prefix+".jurisdiction", err)
		}
		if !validFindingStatus(entry.Status) {
			return fieldError(prefix+".status", fmt.Errorf("unknown value %q", entry.Status))
		}
		if !validReview(entry.Review) {
			return fieldError(prefix+".review", fmt.Errorf("unknown value %q", entry.Review))
		}
		if entry.ResearchFile == "" {
			return fieldError(prefix+".research_file", errors.New("is required"))
		}
		if len(entry.RequiredFields) == 0 {
			return fieldError(prefix+".required_pack_fields", errors.New("must describe the future pack shape"))
		}
		if entry.Status == StatusResearchMissing && entry.Review == ReviewReviewed {
			return fieldError(prefix+".review", errors.New("RESEARCH_MISSING cannot be marked REVIEWED"))
		}
		if err := validateCitation(entry.Citation, prefix+".citation"); err != nil {
			return err
		}
	}
	return nil
}

func validateCitation(c Citation, field string) error {
	if c.SourceFile == "" {
		return fieldError(field+".source_file", errors.New("is required"))
	}
	if c.Section == "" {
		return fieldError(field+".section", errors.New("is required"))
	}
	if c.Authority == "" {
		return fieldError(field+".authority", errors.New("is required"))
	}
	return nil
}

// ComputeDigest returns the digest of content, excluding Digest itself.
func (r Registry) ComputeDigest() string {
	payload := struct {
		SchemaVersion int     `json:"schema_version"`
		Version       int     `json:"version"`
		Start         string  `json:"effective_start"`
		End           string  `json:"effective_end"`
		HasEnd        bool    `json:"effective_has_end"`
		KnownAt       string  `json:"known_at"`
		Entries       []Entry `json:"entries"`
	}{r.SchemaVersion, r.Version, r.Window.Start.String(), r.Window.End.String(), r.Window.HasEnd, r.KnownAt.String(), r.Entries}
	b, _ := json.Marshal(payload)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Explain renders a deterministic one-line coverage summary.
func (r Registry) Explain() string {
	return fmt.Sprintf("researchgaps schema=%d version=%d window=%s known_at=%s entries=%d digest=%s", r.SchemaVersion, r.Version, r.Window, r.KnownAt, len(r.Entries), r.Digest)
}

type yamlRegistry struct {
	SchemaVersion  int     `yaml:"schema_version"`
	Version        int     `yaml:"version"`
	EffectiveStart string  `yaml:"effective_start"`
	EffectiveEnd   string  `yaml:"effective_end"`
	KnownAt        string  `yaml:"known_at"`
	Entries        []Entry `yaml:"entries"`
}

func parseScalar(text string) string {
	text = strings.TrimSpace(text)
	if len(text) >= 2 && text[0] == '"' && text[len(text)-1] == '"' {
		if value, err := strconv.Unquote(text); err == nil {
			return value
		}
	}
	return strings.Trim(text, "'")
}

func splitInline(text string) []string {
	var out []string
	start := 0
	quoted := false
	for i, r := range text {
		switch r {
		case '"':
			quoted = !quoted
		case ',':
			if !quoted {
				out = append(out, strings.TrimSpace(text[start:i]))
				start = i + 1
			}
		}
	}
	out = append(out, strings.TrimSpace(text[start:]))
	return out
}

func inlineMap(text string) (map[string]string, error) {
	text = strings.TrimSpace(text)
	if len(text) < 2 || text[0] != '{' || text[len(text)-1] != '}' {
		return nil, errors.New("expected inline map")
	}
	out := map[string]string{}
	for _, part := range splitInline(text[1 : len(text)-1]) {
		key, value, ok := strings.Cut(part, ":")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("invalid inline map member %q", part)
		}
		out[strings.TrimSpace(key)] = parseScalar(value)
	}
	return out, nil
}

func inlineList(text string) ([]string, error) {
	text = strings.TrimSpace(text)
	if len(text) < 2 || text[0] != '[' || text[len(text)-1] != ']' {
		return nil, errors.New("expected inline list")
	}
	if strings.TrimSpace(text[1:len(text)-1]) == "" {
		return nil, nil
	}
	parts := splitInline(text[1 : len(text)-1])
	out := make([]string, len(parts))
	for i, part := range parts {
		out[i] = parseScalar(part)
	}
	return out, nil
}

func parseKeyValue(text string) (string, string, error) {
	key, value, ok := strings.Cut(strings.TrimSpace(text), ":")
	if !ok || strings.TrimSpace(key) == "" {
		return "", "", errors.New("expected key/value")
	}
	return strings.TrimSpace(key), strings.TrimSpace(value), nil
}

// parseFixtureYAML reads the small checked-in YAML schema without a semantic
// third-party dependency. The fixture uses scalars, block sequence items, and
// inline maps/lists only; unknown members are rejected.
func parseFixtureYAML(data []byte) (yamlRegistry, error) {
	var out yamlRegistry
	section := ""
	var entry *Entry
	flush := func() {
		if entry != nil {
			out.Entries = append(out.Entries, *entry)
			entry = nil
		}
	}
	for lineNumber, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent == 0 {
			flush()
			key, value, err := parseKeyValue(trimmed)
			if err != nil {
				return yamlRegistry{}, fmt.Errorf("line %d: %w", lineNumber+1, err)
			}
			if value == "" {
				section = key
				continue
			}
			switch key {
			case "schema_version":
				_, err = fmt.Sscanf(value, "%d", &out.SchemaVersion)
			case "version":
				_, err = fmt.Sscanf(value, "%d", &out.Version)
			case "effective_start":
				out.EffectiveStart = parseScalar(value)
			case "effective_end":
				out.EffectiveEnd = parseScalar(value)
			case "known_at":
				out.KnownAt = parseScalar(value)
			default:
				err = fmt.Errorf("unknown top-level field %q", key)
			}
			if err != nil {
				return yamlRegistry{}, fmt.Errorf("line %d: %w", lineNumber+1, err)
			}
			continue
		}
		if strings.HasPrefix(trimmed, "- ") {
			flush()
			if section != "entries" {
				return yamlRegistry{}, fmt.Errorf("line %d: unknown sequence %q", lineNumber+1, section)
			}
			entry = &Entry{}
			if err := applyEntryField(entry, strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))); err != nil {
				return yamlRegistry{}, fmt.Errorf("line %d: %w", lineNumber+1, err)
			}
			continue
		}
		if section != "entries" || entry == nil {
			return yamlRegistry{}, fmt.Errorf("line %d: field has no entry", lineNumber+1)
		}
		key, value, err := parseKeyValue(trimmed)
		if err != nil {
			return yamlRegistry{}, fmt.Errorf("line %d: %w", lineNumber+1, err)
		}
		if err := applyEntryField(entry, key+": "+value); err != nil {
			return yamlRegistry{}, fmt.Errorf("line %d: %w", lineNumber+1, err)
		}
	}
	flush()
	return out, nil
}

func applyCitation(c *Citation, text string) error {
	members, err := inlineMap(text)
	if err != nil {
		return err
	}
	c.SourceFile, c.Section, c.Authority = members["source_file"], members["section"], members["authority"]
	return nil
}

func applyEntryField(entry *Entry, text string) error {
	key, value, err := parseKeyValue(text)
	if err != nil {
		return err
	}
	switch key {
	case "id":
		entry.ID = parseScalar(value)
	case "name":
		entry.Name = parseScalar(value)
	case "jurisdiction":
		members, err := inlineMap(value)
		if err != nil {
			return err
		}
		entry.Jurisdiction = legal.Jurisdiction{Country: members["country"], State: members["state"], Locality: members["locality"]}
	case "status":
		entry.Status = FindingStatus(parseScalar(value))
	case "review":
		entry.Review = ReviewFlag(parseScalar(value))
	case "research_file":
		entry.ResearchFile = parseScalar(value)
	case "citation":
		return applyCitation(&entry.Citation, value)
	case "required_pack_fields":
		entry.RequiredFields, err = inlineList(value)
	default:
		return fmt.Errorf("unknown entry field %q", key)
	}
	return err
}

func parseKnownAt(s string) (values.KnownAt, error) {
	instant, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return values.KnownAt{}, fmt.Errorf("%w: known_at: %v", ErrValidation, err)
	}
	return values.NewKnownAt(values.NewInstant(instant))
}

// Load parses and validates a YAML gap registry, then computes its digest.
func Load(data []byte) (Registry, error) {
	wire, err := parseFixtureYAML(data)
	if err != nil {
		return Registry{}, fmt.Errorf("%w: yaml: %v", ErrValidation, err)
	}
	start, err := values.ParseLocalDate(wire.EffectiveStart)
	if err != nil {
		return Registry{}, fieldError("effective_start", err)
	}
	var window legal.EffectiveWindow
	if wire.EffectiveEnd == "" {
		window, err = legal.NewOpenEffectiveWindow(start)
	} else {
		end, endErr := values.ParseLocalDate(wire.EffectiveEnd)
		if endErr != nil {
			return Registry{}, fieldError("effective_end", endErr)
		}
		window, err = legal.NewClosedEffectiveWindow(start, end)
	}
	if err != nil {
		return Registry{}, fieldError("effective_window", err)
	}
	knownAt, err := parseKnownAt(wire.KnownAt)
	if err != nil {
		return Registry{}, fieldError("known_at", err)
	}
	r := Registry{SchemaVersion: wire.SchemaVersion, Version: wire.Version, Window: window, KnownAt: knownAt, Entries: wire.Entries}
	if err := r.Validate(); err != nil {
		return Registry{}, err
	}
	r.Digest = r.ComputeDigest()
	return r, nil
}

// LoadFixture loads the checked-in research-gap fixture.
func LoadFixture() (Registry, error) {
	data, err := fixtureFS.ReadFile("testdata/predictive-scheduling.yaml")
	if err != nil {
		return Registry{}, fmt.Errorf("researchgaps: fixture: %w", err)
	}
	return Load(data)
}

// Lookup returns a defensive copy of a known gap row.
func (r Registry) Lookup(id string, effectiveDate values.LocalDate, knownAt values.KnownAt) (Entry, error) {
	if err := r.Validate(); err != nil {
		return Entry{}, err
	}
	if effectiveDate.Validate() != nil || !r.Window.Contains(effectiveDate) {
		return Entry{}, ErrEffectiveDate
	}
	if knownAt.Instant().Validate() != nil || knownAt.Instant().Before(r.KnownAt.Instant()) {
		return Entry{}, ErrKnownAt
	}
	for _, entry := range r.Entries {
		if entry.ID == id {
			entry.RequiredFields = append([]string(nil), entry.RequiredFields...)
			return entry, nil
		}
	}
	return Entry{}, fmt.Errorf("%w: %s", ErrUnknownEntry, id)
}

// ValidatePublication fails closed while a known gap is still present or the
// publishing evidence does not carry a reviewed research file.
func (r Registry) ValidatePublication(evidence PublicationEvidence, effectiveDate values.LocalDate, knownAt values.KnownAt) error {
	entry, err := r.Lookup(evidence.JurisdictionID, effectiveDate, knownAt)
	if err != nil {
		return err
	}
	if entry.Status == StatusResearchMissing {
		return fmt.Errorf("%w: %s status=%s", ErrPackBlocked, entry.ID, entry.Status)
	}
	if entry.Review != ReviewReviewed || !evidence.ReviewedResearch {
		return fmt.Errorf("%w: %s research review is not REVIEWED", ErrPackBlocked, entry.ID)
	}
	if evidence.ResearchFile != entry.ResearchFile {
		return fmt.Errorf("%w: research_file expected %s, got %s", ErrPackBlocked, entry.ResearchFile, evidence.ResearchFile)
	}
	return nil
}
