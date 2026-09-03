// Package reportrender executes a published report under an explicit
// authorization and produces byte-for-byte reproducible views and exports.
package reportrender

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var (
	ErrUnauthorized = errors.New("reportrender: unauthorized")
	ErrInvalid      = errors.New("reportrender: invalid request")
	ErrIncomplete   = errors.New("reportrender: incomplete source")
)

const (
	Complete = "COMPLETE"
	Partial  = "PARTIAL"
	Stale    = "STALE"
	Unknown  = "UNKNOWN"
)

type Watermark struct {
	Source string `json:"source"`
	Value  string `json:"value"`
}
type Scope struct {
	Population string   `json:"population"`
	Fields     []string `json:"fields,omitempty"`
}
type Principal struct {
	ID      string `json:"id"`
	Tenant  string `json:"tenant"`
	Purpose string `json:"purpose"`
}

// Authorization is evaluated before rows are selected. A nil Allow means no
// caller is authorized; this fail-closed default prevents cached/admin leaks.
type Authorization struct {
	Principal Principal                   `json:"principal"`
	Scope     Scope                       `json:"scope"`
	Allow     func(Principal, Scope) bool `json:"-"`
}

type ReportDefinition struct {
	ID         string   `json:"id"`
	Version    uint32   `json:"version"`
	Title      string   `json:"title,omitempty"`
	Fields     []string `json:"fields"`
	Metrics    []string `json:"metrics,omitempty"`
	Population string   `json:"population"`
	Sort       []string `json:"sort,omitempty"`
	Formats    []string `json:"formats,omitempty"`
	Disclosure string   `json:"disclosure,omitempty"`
	Digest     string   `json:"digest"`
}
type Definition = ReportDefinition

type Registry struct{ definitions map[string]ReportDefinition }

func NewRegistry() *Registry { return &Registry{definitions: map[string]ReportDefinition{}} }
func (r *Registry) Register(d ReportDefinition) error {
	p, err := Publish(d)
	if err != nil {
		return err
	}
	k := p.ID + fmt.Sprintf("/%d", p.Version)
	if old, ok := r.definitions[k]; ok {
		if old.Digest != p.Digest {
			return ErrInvalid
		}
		return nil
	}
	r.definitions[k] = p
	return nil
}
func (r *Registry) Publish(d ReportDefinition) (ReportDefinition, error) {
	if err := r.Register(d); err != nil {
		return ReportDefinition{}, err
	}
	p, _ := Publish(d)
	return p, nil
}
func (r *Registry) Definition(id string, ref any) (ReportDefinition, bool) {
	for _, d := range r.definitions {
		if d.ID != id {
			continue
		}
		switch v := ref.(type) {
		case uint32:
			if d.Version == v {
				return d, true
			}
		case string:
			if d.Digest == v {
				return d, true
			}
		}
	}
	return ReportDefinition{}, false
}
func (r *Registry) Lookup(id, digest string) (ReportDefinition, bool) {
	for _, d := range r.definitions {
		if d.ID == id && d.Digest == digest {
			return d, true
		}
	}
	return ReportDefinition{}, false
}

func digestDefinition(d ReportDefinition) string {
	type content ReportDefinition
	d.Digest = ""
	b, _ := json.Marshal(content(d))
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func (d ReportDefinition) Validate() error {
	if d.ID == "" || d.Version == 0 || d.Population == "" || len(d.Fields) == 0 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, x := range append(append([]string{}, d.Fields...), d.Metrics...) {
		if x == "" || seen[x] {
			return ErrInvalid
		}
		seen[x] = true
	}
	return nil
}
func Publish(d ReportDefinition) (ReportDefinition, error) {
	if err := d.Validate(); err != nil {
		return ReportDefinition{}, err
	}
	d.Digest = digestDefinition(d)
	d.Fields = append([]string(nil), d.Fields...)
	d.Metrics = append([]string(nil), d.Metrics...)
	return d, nil
}

type Row map[string]string
type Source struct {
	Name      string    `json:"name"`
	Watermark Watermark `json:"watermark"`
	Complete  bool      `json:"complete"`
	Rows      []Row     `json:"rows"`
}
type ExecutionRequest struct {
	Definition    ReportDefinition `json:"definition"`
	Authorization Authorization    `json:"authorization"`
	Sources       []Source         `json:"sources"`
	Watermarks    []Watermark      `json:"watermarks,omitempty"`
}
type Plan = ExecutionRequest
type Result struct {
	Status           string      `json:"status"`
	DefinitionDigest string      `json:"definition_digest"`
	PrincipalID      string      `json:"principal_id"`
	Population       string      `json:"population"`
	Watermarks       []Watermark `json:"watermarks"`
	Rows             []Row       `json:"rows"`
	RowDigest        string      `json:"row_digest"`
	EvidenceDigest   string      `json:"evidence_digest"`
	WatermarkText    string      `json:"watermark_text"`
}
type Report = Result
type Rendered struct {
	Format    string
	Bytes     []byte
	Digest    string
	Watermark string
	Result    Result
}

func cloneRow(r Row) Row {
	n := Row{}
	for k, v := range r {
		n[k] = v
	}
	return n
}
func canonicalRows(rows []Row) []Row {
	out := make([]Row, len(rows))
	for i, r := range rows {
		out[i] = cloneRow(r)
	}
	return out
}
func digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func Execute(req ExecutionRequest) (Result, error) {
	if err := req.Definition.Validate(); err != nil {
		return Result{}, err
	}
	if req.Definition.Digest == "" {
		req.Definition.Digest = digestDefinition(req.Definition)
	}
	if req.Authorization.Principal.ID == "" || req.Authorization.Principal.Tenant == "" || req.Authorization.Principal.Purpose == "" || req.Authorization.Scope.Population == "" {
		return Result{}, ErrUnauthorized
	}
	if req.Authorization.Scope.Population != req.Definition.Population || req.Authorization.Allow == nil || !req.Authorization.Allow(req.Authorization.Principal, req.Authorization.Scope) {
		return Result{}, ErrUnauthorized
	}
	sources := append([]Source(nil), req.Sources...)
	sort.Slice(sources, func(i, j int) bool { return sources[i].Name < sources[j].Name })
	status := Complete
	var rows []Row
	var marks []Watermark
	for _, s := range sources {
		if s.Name == "" || s.Watermark.Source == "" || s.Watermark.Value == "" {
			status = Unknown
		}
		if !s.Complete && status == Complete {
			status = Partial
		}
		rows = append(rows, s.Rows...)
		marks = append(marks, s.Watermark)
	}
	sort.Slice(marks, func(i, j int) bool {
		if marks[i].Source == marks[j].Source {
			return marks[i].Value < marks[j].Value
		}
		return marks[i].Source < marks[j].Source
	})
	rows = canonicalRows(rows)
	rd := digest(struct {
		Rows []Row `json:"rows"`
	}{rows})
	r := Result{Status: status, DefinitionDigest: req.Definition.Digest, PrincipalID: req.Authorization.Principal.ID, Population: req.Definition.Population, Watermarks: marks, Rows: rows, RowDigest: rd}
	r.WatermarkText = watermark(marks)
	r.EvidenceDigest = digest(struct{ Definition, Principal, Scope, Rows, Watermarks any }{r.DefinitionDigest, r.PrincipalID, r.Population, r.Rows, r.Watermarks})
	return r, nil
}

func watermark(w []Watermark) string {
	a := make([]string, len(w))
	for i, x := range w {
		a[i] = x.Source + "=" + x.Value
	}
	return strings.Join(a, ",")
}
func (r Result) Render(format string) (Rendered, error) {
	format = strings.ToLower(format)
	if format != "json" && format != "csv" && format != "html" {
		return Rendered{}, ErrInvalid
	}
	var b []byte
	var err error
	switch format {
	case "json":
		b, err = json.Marshal(r)
	case "csv":
		b = []byte(csv(r))
	case "html":
		b = []byte(fmt.Sprintf("<div data-watermark=%q>CONFIDENTIAL — %s</div><pre>%s</pre>", r.WatermarkText, r.Status, mustJSON(r)))
	}
	if err != nil {
		return Rendered{}, err
	}
	return Rendered{Format: format, Bytes: b, Digest: digest(json.RawMessage(b)), Watermark: r.WatermarkText, Result: r}, nil
}
func (r Result) Export(format string) (Rendered, error) { return r.Render(format) }
func mustJSON(v any) string                             { b, _ := json.Marshal(v); return string(b) }
func csv(r Result) string {
	if len(r.Rows) == 0 {
		return "# watermark=" + r.WatermarkText + "\n"
	}
	keys := []string{}
	for k := range r.Rows[0] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("# watermark=" + r.WatermarkText + "\n")
	b.WriteString(strings.Join(keys, ","))
	b.WriteByte('\n')
	for _, row := range r.Rows {
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(strings.ReplaceAll(row[k], ",", ""))
		}
		b.WriteByte('\n')
	}
	return b.String()
}
