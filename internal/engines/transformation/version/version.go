// Package version pins compiled transformation/ir.Program releases by the
// program's own digest -- not the source TransformationDefinition's digest,
// which the sibling transformation.Registry already tracks. Two
// definitions that differ only in incidental declared order (or that add an
// operation later canceled out by canonicalization) compile to the same IR
// and therefore the same pinned identity here, which is exactly the
// identity a pinned execution needs: it replays the compiled program, never
// the source text.
//
// Compare performs a structural compatibility check between two programs
// (is every input the old program relied on still there with the same
// type; is every output the old program produced still there with the same
// type; did the declared execution limits widen without saying so) and a
// MigrationNote records why a caller moved from one pinned digest to
// another. Neither computation reads a clock or any other ambient state:
// every judgment is a pure function of the two programs (and, for a
// MigrationNote, the caller's own explicit summary).
package version

import (
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/hcm-next/internal/engines/transformation/ir"
)

var (
	// ErrInvalidRevision reports a non-positive revision number.
	ErrInvalidRevision = errors.New("transformation/version: revision must be >= 1")
	// ErrVersionExists reports a revision already registered under its name.
	ErrVersionExists = errors.New("transformation/version: revision already exists")
	// ErrVersionNotFound reports an unregistered name/revision pair.
	ErrVersionNotFound = errors.New("transformation/version: revision not found")
	// ErrPinnedVersion wraps ErrVersionNotFound or a digest mismatch when
	// resolving an immutable Pin.
	ErrPinnedVersion = errors.New("transformation/version: pinned version is unavailable")
	// ErrIncompatible reports that PublishCompatible refused a breaking
	// replacement.
	ErrIncompatible = errors.New("transformation/version: incompatible version")
	// ErrMigrationNote reports a MigrationNote that omits a required field,
	// or that declares itself non-breaking while naming a breaking reason.
	ErrMigrationNote = errors.New("transformation/version: invalid migration note")
)

// PinnedProgram is an immutable, IR-digest-identified release.
type PinnedProgram struct {
	Name     string     `json:"name"`
	Revision int        `json:"revision"`
	Program  ir.Program `json:"program"`
	Digest   string     `json:"digest"`
}

// Registry stores immutable pinned program releases, keyed by name and
// revision, and hands back exact-digest Pin references that never follow
// later publication.
type Registry struct {
	releases map[string]map[int]PinnedProgram
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{releases: make(map[string]map[int]PinnedProgram)}
}

// Publish registers p as name's given revision. The revision must not
// already exist; revisions are otherwise caller-numbered so a registry can
// mirror an external release sequence.
func (r *Registry) Publish(name string, revision int, p ir.Program) (PinnedProgram, error) {
	if r == nil || revision < 1 {
		return PinnedProgram{}, ErrInvalidRevision
	}
	if err := p.Validate(); err != nil {
		return PinnedProgram{}, err
	}
	digest, err := p.Digest()
	if err != nil {
		return PinnedProgram{}, err
	}
	if r.releases == nil {
		r.releases = make(map[string]map[int]PinnedProgram)
	}
	if r.releases[name] == nil {
		r.releases[name] = make(map[int]PinnedProgram)
	}
	if _, exists := r.releases[name][revision]; exists {
		return PinnedProgram{}, ErrVersionExists
	}
	v := PinnedProgram{Name: name, Revision: revision, Program: p, Digest: digest}
	r.releases[name][revision] = v
	return v, nil
}

// PublishCompatible publishes next as revision only when Compare finds it
// compatible with previous under opts; otherwise it registers nothing and
// returns ErrIncompatible alongside the report that explains why.
func (r *Registry) PublishCompatible(name string, revision int, previous, next ir.Program, opts CompatibilityOptions) (PinnedProgram, CompatibilityReport, error) {
	report, err := Compare(previous, next, opts)
	if err != nil {
		return PinnedProgram{}, report, err
	}
	if !report.Compatible {
		return PinnedProgram{}, report, ErrIncompatible
	}
	v, err := r.Publish(name, revision, next)
	return v, report, err
}

// Resolve returns the registered release for name/revision.
func (r *Registry) Resolve(name string, revision int) (PinnedProgram, error) {
	if r == nil {
		return PinnedProgram{}, ErrVersionNotFound
	}
	v, ok := r.releases[name][revision]
	if !ok {
		return PinnedProgram{}, ErrVersionNotFound
	}
	return v, nil
}

// Pin is an immutable execution reference: resolving it never follows later
// publication under the same name/revision (which Publish itself already
// forbids by rejecting a duplicate revision, but Pin also checks the digest
// so a forged or corrupted Pin cannot resolve to a different program).
type Pin struct {
	Name     string `json:"name"`
	Revision int    `json:"revision"`
	Digest   string `json:"digest"`
}

// Pin returns an immutable reference to name's given revision.
func (r *Registry) Pin(name string, revision int) (Pin, error) {
	v, err := r.Resolve(name, revision)
	if err != nil {
		return Pin{}, fmt.Errorf("%w: %v", ErrPinnedVersion, err)
	}
	return Pin{Name: v.Name, Revision: v.Revision, Digest: v.Digest}, nil
}

// ResolvePin resolves an immutable Pin back to its release, rejecting a
// digest mismatch.
func (r *Registry) ResolvePin(p Pin) (PinnedProgram, error) {
	v, err := r.Resolve(p.Name, p.Revision)
	if err != nil {
		return PinnedProgram{}, fmt.Errorf("%w: %v", ErrPinnedVersion, err)
	}
	if p.Digest != "" && p.Digest != v.Digest {
		return PinnedProgram{}, fmt.Errorf("%w: digest mismatch", ErrPinnedVersion)
	}
	return v, nil
}

// CompatibilityOptions makes an intentional limit widening explicit. Without
// AllowWidenedLimits, Compare treats any raised MaxSteps or MaxFanOut as a
// silent, undeclared change and folds it into Breaking -- "limits not
// widened silently" is enforced by refusing to call a widened limit
// compatible unless a caller says so on purpose.
type CompatibilityOptions struct {
	AllowWidenedLimits bool
}

// CompatibilityReport is the structural verdict Compare produces.
type CompatibilityReport struct {
	Compatible       bool     `json:"compatible"`
	Breaking         bool     `json:"breaking"`
	InputCompatible  bool     `json:"input_compatible"`
	OutputCompatible bool     `json:"output_compatible"`
	LimitsWidened    bool     `json:"limits_widened"`
	BreakingFields   []string `json:"breaking_fields,omitempty"`
	Reasons          []string `json:"reasons,omitempty"`
}

func inputSchema(p ir.Program) map[string]string {
	m := map[string]string{}
	for _, instr := range p.Instructions {
		for _, s := range instr.Sources {
			m[s.Schema+"."+s.Field] = string(s.Type)
		}
	}
	return m
}

func outputSchema(p ir.Program) map[string]string {
	m := map[string]string{}
	for _, instr := range p.Instructions {
		m[instr.Destination.Schema+"."+instr.Destination.Field] = string(instr.Destination.Type)
	}
	return m
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Compare performs a structural compatibility check between two compiled
// programs:
//
//   - input schema compatible: every source field the old program read is
//     still readable at the same declared type in the new program (adding a
//     new input the old program never read is not breaking).
//   - output schema compatible: every destination field the old program
//     produced is still produced at the same declared type in the new
//     program (adding a brand new output field is not breaking).
//   - limits not widened silently: a raised MaxSteps or MaxFanOut is
//     reported in LimitsWidened, and counts as a breaking Reason unless
//     opts.AllowWidenedLimits says the caller reviewed and intends it.
//
// Every field responsible for a "no" verdict is named in BreakingFields and
// explained in Reasons; Compare never reports "incompatible" without saying
// which field.
func Compare(old, next ir.Program, opts CompatibilityOptions) (CompatibilityReport, error) {
	if err := old.Validate(); err != nil {
		return CompatibilityReport{}, fmt.Errorf("old program: %w", err)
	}
	if err := next.Validate(); err != nil {
		return CompatibilityReport{}, fmt.Errorf("next program: %w", err)
	}

	report := CompatibilityReport{InputCompatible: true, OutputCompatible: true}
	var breakingFields, reasons []string

	oldIn, nextIn := inputSchema(old), inputSchema(next)
	for _, k := range sortedKeys(oldIn) {
		ot := oldIn[k]
		nt, ok := nextIn[k]
		switch {
		case !ok:
			report.InputCompatible = false
			breakingFields = append(breakingFields, k)
			reasons = append(reasons, fmt.Sprintf("input field %s removed", k))
		case nt != ot:
			report.InputCompatible = false
			breakingFields = append(breakingFields, k)
			reasons = append(reasons, fmt.Sprintf("input field %s type changed %s -> %s", k, ot, nt))
		}
	}

	oldOut, nextOut := outputSchema(old), outputSchema(next)
	for _, k := range sortedKeys(oldOut) {
		ot := oldOut[k]
		nt, ok := nextOut[k]
		switch {
		case !ok:
			report.OutputCompatible = false
			breakingFields = append(breakingFields, k)
			reasons = append(reasons, fmt.Sprintf("output field %s removed", k))
		case nt != ot:
			report.OutputCompatible = false
			breakingFields = append(breakingFields, k)
			reasons = append(reasons, fmt.Sprintf("output field %s type changed %s -> %s", k, ot, nt))
		}
	}

	if next.Limits.MaxSteps > old.Limits.MaxSteps || next.Limits.MaxFanOut > old.Limits.MaxFanOut {
		report.LimitsWidened = true
		if !opts.AllowWidenedLimits {
			reasons = append(reasons, fmt.Sprintf(
				"limits widened silently: max_steps %d -> %d, max_fan_out %d -> %d",
				old.Limits.MaxSteps, next.Limits.MaxSteps, old.Limits.MaxFanOut, next.Limits.MaxFanOut))
		}
	}

	sort.Strings(breakingFields)
	report.BreakingFields = breakingFields
	report.Reasons = reasons
	report.Breaking = len(reasons) > 0
	report.Compatible = !report.Breaking
	return report, nil
}

// MigrationNote is an explicit, caller-authored record of why a pinned IR
// revision was replaced by another. It carries no wall-clock timestamp: a
// caller that needs one stamps the note from its own authoritative clock,
// because this package never fabricates one.
type MigrationNote struct {
	FromDigest     string   `json:"from_digest"`
	ToDigest       string   `json:"to_digest"`
	Compatible     bool     `json:"compatible"`
	Breaking       bool     `json:"breaking"`
	BreakingFields []string `json:"breaking_fields,omitempty"`
	Reasons        []string `json:"reasons,omitempty"`
	Summary        string   `json:"summary"`
	Author         string   `json:"author"`
}

// NewMigrationNote builds a MigrationNote from a CompatibilityReport plus a
// mandatory caller-authored summary and author. It refuses to build a note
// that claims non-breaking while the report it was built from disagrees,
// and refuses a breaking note that names no field or reason -- a migration
// note that cannot say what changed is not a record.
func NewMigrationNote(old, next ir.Program, report CompatibilityReport, summary, author string) (MigrationNote, error) {
	if summary == "" || author == "" {
		return MigrationNote{}, fmt.Errorf("%w: summary and author are required", ErrMigrationNote)
	}
	oldDigest, err := old.Digest()
	if err != nil {
		return MigrationNote{}, err
	}
	nextDigest, err := next.Digest()
	if err != nil {
		return MigrationNote{}, err
	}
	n := MigrationNote{
		FromDigest:     oldDigest,
		ToDigest:       nextDigest,
		Compatible:     report.Compatible,
		Breaking:       report.Breaking,
		BreakingFields: append([]string(nil), report.BreakingFields...),
		Reasons:        append([]string(nil), report.Reasons...),
		Summary:        summary,
		Author:         author,
	}
	if err := n.Validate(); err != nil {
		return MigrationNote{}, err
	}
	return n, nil
}

// Validate reports whether the note is internally consistent.
func (m MigrationNote) Validate() error {
	if m.FromDigest == "" || m.ToDigest == "" || m.Summary == "" || m.Author == "" {
		return fmt.Errorf("%w: from_digest, to_digest, summary and author are required", ErrMigrationNote)
	}
	if m.Compatible == m.Breaking {
		return fmt.Errorf("%w: compatible and breaking must disagree", ErrMigrationNote)
	}
	if m.Breaking && len(m.BreakingFields) == 0 && len(m.Reasons) == 0 {
		return fmt.Errorf("%w: a breaking note must name at least one field or reason", ErrMigrationNote)
	}
	return nil
}
