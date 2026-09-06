// Package export owns the two output profiles for governed data export.
// HUMAN_SPREADSHEET is a presentation format and may transform dangerous
// cells; MACHINE_DATA is a typed, non-executable interchange format and never
// applies spreadsheet escaping to source values.
package export

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const ContractVersion = "hcmnext.export-profiles/v1"

type Profile string

const (
	HumanSpreadsheet Profile = "HUMAN_SPREADSHEET"
	MachineData      Profile = "MACHINE_DATA"
)

var (
	ErrInvalidRequest = errors.New("export: invalid request")
	ErrInvalidProfile = errors.New("export: invalid profile")
	ErrUnauthorized   = errors.New("export: field is outside the authorized scope")
	ErrTampered       = errors.New("export: artifact digest mismatch")
)

// Field is a source value. Value is always retained as a string at this
// boundary; machine JSON preserves the exact bytes represented by the UTF-8
// string, while human rendering is the only layer allowed to transform it.
type Field struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Record struct {
	ID     string
	Fields []Field
}

type Request struct {
	TenantID       string
	SubjectID      string
	Scope          string
	Purpose        string
	SchemaDigest   string
	Classification string
	DLPPolicyRef   string
	AllowedFields  []string
	ExpiresAt      time.Time
	Profile        Profile
	Locale         string
	Records        []Record
}

type Manifest struct {
	ContractVersion       string
	Profile               Profile
	TransformationProfile string
	TenantID              string
	SubjectID             string
	Scope                 string
	Purpose               string
	SchemaDigest          string
	Classification        string
	DLPPolicyRef          string
	AllowedFields         []string
	Locale                string
	Delimiter             rune
	RecordCount           int
	Warning               string
	ContentDigest         string
	ManifestDigest        string
	ExpiresAt             time.Time
}

type Artifact struct {
	Manifest Manifest
	Content  []byte
	Warnings []string
}

type Plan struct {
	request  Request
	manifest Manifest
}

func NewPlan(req Request, now time.Time) (Plan, error) {
	if err := validate(req, now); err != nil {
		return Plan{}, err
	}
	fields := append([]string(nil), req.AllowedFields...)
	delimiter, err := delimiterFor(req.Profile, req.Locale)
	if err != nil {
		return Plan{}, err
	}
	transformation := "exact-typed-json/v1"
	warning := ""
	if req.Profile == HumanSpreadsheet {
		transformation = "csv-inert-formula-cells/v1"
		warning = "human spreadsheet profile may prefix formula/control-leading cells with an apostrophe"
	}
	return Plan{request: cloneRequest(req), manifest: Manifest{
		ContractVersion: ContractVersion, Profile: req.Profile, TransformationProfile: transformation,
		TenantID: req.TenantID, SubjectID: req.SubjectID, Scope: req.Scope, Purpose: req.Purpose,
		SchemaDigest: req.SchemaDigest, Classification: req.Classification, DLPPolicyRef: req.DLPPolicyRef,
		AllowedFields: fields, Locale: req.Locale, Delimiter: delimiter, RecordCount: len(req.Records),
		Warning: warning, ExpiresAt: req.ExpiresAt,
	}}, nil
}

// Export plans and renders one immutable artifact. It does not read a source,
// mutate a source, or apply presentation escaping to the request records.
func Export(req Request, now time.Time) (Artifact, error) {
	plan, err := NewPlan(req, now)
	if err != nil {
		return Artifact{}, err
	}
	return plan.Render()
}

func (p Plan) Render() (Artifact, error) {
	var content []byte
	var warnings []string
	var err error
	switch p.manifest.Profile {
	case HumanSpreadsheet:
		content, warnings, err = renderHuman(p.request, p.manifest)
	case MachineData:
		content, err = renderMachine(p.request, p.manifest)
	default:
		return Artifact{}, ErrInvalidProfile
	}
	if err != nil {
		return Artifact{}, err
	}
	p.manifest.ContentDigest = digest(content)
	p.manifest.ManifestDigest = digestManifest(p.manifest)
	return Artifact{Manifest: p.manifest, Content: content, Warnings: warnings}, nil
}

func (a Artifact) Verify() error {
	if a.Manifest.ContentDigest != digest(a.Content) || a.Manifest.ManifestDigest == "" || a.Manifest.ManifestDigest != digestManifest(a.Manifest) {
		return ErrTampered
	}
	return nil
}

func (a Artifact) Explain() string {
	return fmt.Sprintf("profile=%s transformation=%s records=%d classification=%s digest=%s", a.Manifest.Profile, a.Manifest.TransformationProfile, a.Manifest.RecordCount, a.Manifest.Classification, a.Manifest.ContentDigest)
}

func Explain(a Artifact) string { return a.Explain() }

func RenderHumanSpreadsheet(req Request, now time.Time) (Artifact, error) {
	req.Profile = HumanSpreadsheet
	return Export(req, now)
}

func RenderMachineData(req Request, now time.Time) (Artifact, error) {
	req.Profile = MachineData
	return Export(req, now)
}

func renderHuman(req Request, manifest Manifest) ([]byte, []string, error) {
	var b strings.Builder
	w := csv.NewWriter(&b)
	w.Comma = manifest.Delimiter
	header := append([]string{"id"}, req.AllowedFields...)
	if err := w.Write(header); err != nil {
		return nil, nil, err
	}
	rows := sortedRecords(req.Records)
	transformed := false
	for _, record := range rows {
		values := mapFields(record.Fields)
		row := make([]string, 0, len(header))
		id := record.ID
		if formulaLeading(id) {
			id = "'" + id
			transformed = true
		}
		row = append(row, id)
		for _, name := range req.AllowedFields {
			value := values[name]
			if formulaLeading(value) {
				value = "'" + value
				transformed = true
			}
			row = append(row, value)
		}
		if err := w.Write(row); err != nil {
			return nil, nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, nil, err
	}
	warnings := []string{}
	if transformed {
		warnings = append(warnings, "formula/control-leading cells were prefixed with an apostrophe for spreadsheet safety")
	}
	return []byte(b.String()), warnings, nil
}

type machineDocument struct {
	SchemaVersion int             `json:"schema_version"`
	Profile       Profile         `json:"profile"`
	Records       []machineRecord `json:"records"`
}

type machineRecord struct {
	ID     string  `json:"id"`
	Fields []Field `json:"fields"`
}

func renderMachine(req Request, manifest Manifest) ([]byte, error) {
	rows := sortedRecords(req.Records)
	doc := machineDocument{SchemaVersion: 1, Profile: manifest.Profile, Records: make([]machineRecord, 0, len(rows))}
	for _, record := range rows {
		values := mapFields(record.Fields)
		fields := make([]Field, 0, len(req.AllowedFields))
		for _, name := range req.AllowedFields {
			fields = append(fields, Field{Name: name, Value: values[name]})
		}
		doc.Records = append(doc.Records, machineRecord{ID: record.ID, Fields: fields})
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func validate(req Request, now time.Time) error {
	for name, value := range map[string]string{"tenant": req.TenantID, "subject": req.SubjectID, "scope": req.Scope, "purpose": req.Purpose, "schema_digest": req.SchemaDigest, "classification": req.Classification, "dlp_policy": req.DLPPolicyRef} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidRequest, name)
		}
	}
	if !req.ExpiresAt.After(now) || req.ExpiresAt.Sub(now) > 7*24*time.Hour {
		return fmt.Errorf("%w: expiry must be in the future and within seven days", ErrInvalidRequest)
	}
	if req.Profile != HumanSpreadsheet && req.Profile != MachineData {
		return ErrInvalidProfile
	}
	if _, err := delimiterFor(req.Profile, req.Locale); err != nil {
		return err
	}
	if len(req.AllowedFields) == 0 || len(req.Records) == 0 {
		return fmt.Errorf("%w: fields and records are required", ErrInvalidRequest)
	}
	allowed := make(map[string]struct{}, len(req.AllowedFields))
	for _, field := range req.AllowedFields {
		if strings.TrimSpace(field) == "" || !utf8.ValidString(field) {
			return fmt.Errorf("%w: invalid allowed field", ErrInvalidRequest)
		}
		if _, ok := allowed[field]; ok {
			return fmt.Errorf("%w: duplicate allowed field %q", ErrInvalidRequest, field)
		}
		allowed[field] = struct{}{}
	}
	for _, record := range req.Records {
		if strings.TrimSpace(record.ID) == "" || !utf8.ValidString(record.ID) {
			return fmt.Errorf("%w: record id is required", ErrInvalidRequest)
		}
		seen := make(map[string]struct{}, len(record.Fields))
		for _, field := range record.Fields {
			if _, ok := allowed[field.Name]; !ok {
				return fmt.Errorf("%w: %s", ErrUnauthorized, field.Name)
			}
			if _, ok := seen[field.Name]; ok {
				return fmt.Errorf("%w: duplicate record field %q", ErrInvalidRequest, field.Name)
			}
			seen[field.Name] = struct{}{}
			if !utf8.ValidString(field.Value) {
				return fmt.Errorf("%w: field %q is not UTF-8", ErrInvalidRequest, field.Name)
			}
		}
	}
	return nil
}

func delimiterFor(profile Profile, locale string) (rune, error) {
	if profile == MachineData {
		return ',', nil
	}
	switch locale {
	case "", "en-US", "en-GB", "en-CA":
		return ',', nil
	case "de-DE", "fr-FR", "it-IT", "es-ES":
		return ';', nil
	default:
		return 0, fmt.Errorf("%w: unsupported spreadsheet locale %q", ErrInvalidRequest, locale)
	}
}

func formulaLeading(value string) bool {
	if value == "" {
		return false
	}
	switch []rune(value)[0] {
	case '=', '+', '-', '@', '\t', '\r', '\n', '＝', '＋', '－', '＠':
		return true
	default:
		return false
	}
}

func sortedRecords(records []Record) []Record {
	out := cloneRecords(records)
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func mapFields(fields []Field) map[string]string {
	out := make(map[string]string, len(fields))
	for _, field := range fields {
		out[field.Name] = field.Value
	}
	return out
}

func cloneRequest(req Request) Request {
	req.AllowedFields = append([]string(nil), req.AllowedFields...)
	req.Records = cloneRecords(req.Records)
	return req
}

func cloneRecords(records []Record) []Record {
	out := make([]Record, len(records))
	for i, record := range records {
		out[i] = Record{ID: record.ID, Fields: append([]Field(nil), record.Fields...)}
	}
	return out
}

func digest(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func digestManifest(manifest Manifest) string {
	copyManifest := manifest
	copyManifest.ManifestDigest = ""
	b, _ := json.Marshal(copyManifest)
	return digest(b)
}
