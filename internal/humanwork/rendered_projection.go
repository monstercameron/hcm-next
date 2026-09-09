package humanwork

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// ProjectionSafety is the server's decision about whether the rendered view
// contains enough known, visible information for an ordinary approval.
type ProjectionSafety string

const (
	// SafetySafeToDecide is the only state in which a vote may be recorded.
	SafetySafeToDecide ProjectionSafety = "SAFE_TO_DECIDE"
	// SafetyMaterialUnknown means a material value was not known at render time.
	SafetyMaterialUnknown ProjectionSafety = "MATERIAL_UNKNOWN"
	// SafetyMaterialRedacted means a material value was withheld from the view.
	SafetyMaterialRedacted ProjectionSafety = "MATERIAL_REDACTED"

	// SafeToDecide is a concise compatibility spelling for SafetySafeToDecide.
	SafeToDecide = SafetySafeToDecide
	// MaterialUnknown is a concise compatibility spelling for
	// SafetyMaterialUnknown.
	MaterialUnknown = SafetyMaterialUnknown
	// MaterialRedacted is a concise compatibility spelling for
	// SafetyMaterialRedacted.
	MaterialRedacted = SafetyMaterialRedacted
)

// ProjectionField is a field the server included in the rendered view. Value
// is already the presentation-safe value; this type is not a raw data export.
type ProjectionField struct {
	Name  string
	Value string
}

// ProjectionInput is the server-owned semantic render input. A caller may
// request a render, but never supplies the resulting digest.
type ProjectionInput struct {
	RequirementID       string
	VisibleFields       []ProjectionField
	HiddenFieldManifest []string
	Warnings            []string
	Effects             []string
	UnknownFields       []string
	RedactedFields      []string
}

func (in ProjectionInput) validate() error {
	if in.RequirementID == "" {
		return errors.New("humanwork: rendered projection requirement id is required")
	}
	seen := make(map[string]bool, len(in.VisibleFields))
	for _, field := range in.VisibleFields {
		if field.Name == "" || seen[field.Name] {
			return fmt.Errorf("humanwork: rendered projection field %q is empty or duplicated", field.Name)
		}
		seen[field.Name] = true
	}
	return nil
}

// RenderedProjection is the immutable server-held receipt of one rendering.
// The digest covers visible fields, hidden-field manifest, warnings and
// effects, as well as the safety findings that decide whether voting is safe.
type RenderedProjection struct {
	RequirementID       string
	TaskVersion         uint64
	VisibleFields       []ProjectionField
	HiddenFieldManifest []string
	Warnings            []string
	Effects             []string
	UnknownFields       []string
	RedactedFields      []string
	Safety              ProjectionSafety
	Digest              string
}

// RenderProjection computes a canonical server-side projection. It always
// returns a non-safe result when material unknown or redacted fields exist.
func RenderProjection(in ProjectionInput, taskVersion uint64) (RenderedProjection, error) {
	if err := in.validate(); err != nil {
		return RenderedProjection{}, err
	}
	if taskVersion == 0 {
		return RenderedProjection{}, errors.New("humanwork: rendered projection task version must be positive")
	}
	unknown := sortedStrings(in.UnknownFields)
	redacted := sortedStrings(in.RedactedFields)
	safety := SafetySafeToDecide
	if len(unknown) > 0 {
		safety = SafetyMaterialUnknown
	} else if len(redacted) > 0 {
		safety = SafetyMaterialRedacted
	}
	visible := append([]ProjectionField(nil), in.VisibleFields...)
	sort.Slice(visible, func(i, j int) bool { return visible[i].Name < visible[j].Name })
	hidden := sortedStrings(in.HiddenFieldManifest)
	warnings := sortedStrings(in.Warnings)
	effects := sortedStrings(in.Effects)
	// The field values and list members are part of the preimage, in a stable
	// order. Build a second writer so the digest cannot ignore a visible value.
	writer := canonicalbytes.New("hcmnext.humanwork.RenderedProjection", 1).
		String("requirement_id", in.RequirementID).
		Int("task_version", int64(taskVersion))
	for _, field := range visible {
		writer.String("visible."+field.Name, field.Value)
	}
	writer.SortedStrings("hidden_field_manifest", hidden).
		SortedStrings("warnings", warnings).
		SortedStrings("effects", effects).
		SortedStrings("unknown_fields", unknown).
		SortedStrings("redacted_fields", redacted).
		String("safety", string(safety))
	raw, err := writer.Bytes()
	if err != nil {
		return RenderedProjection{}, err
	}
	return RenderedProjection{
		RequirementID: in.RequirementID, TaskVersion: taskVersion,
		VisibleFields: visible, HiddenFieldManifest: hidden,
		Warnings: warnings, Effects: effects, UnknownFields: unknown,
		RedactedFields: redacted, Safety: safety,
		Digest: canonicalbytes.Digest(raw),
	}, nil
}

// ProjectionStore is the server-held render record. Re-rendering a task
// replaces its projection and advances its version; it never accepts a
// client-provided version or digest.
type ProjectionStore struct {
	mu      sync.RWMutex
	current map[string]RenderedProjection
}

// NewProjectionStore returns an empty server-held projection store.
func NewProjectionStore() *ProjectionStore {
	return &ProjectionStore{current: make(map[string]RenderedProjection)}
}

// Render stores a new server rendering for taskID. The first render is task
// version one and each later render advances the version exactly once.
func (s *ProjectionStore) Render(taskID string, in ProjectionInput) (RenderedProjection, error) {
	if s == nil || taskID == "" {
		return RenderedProjection{}, errors.New("humanwork: projection store and task id are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	version := uint64(1)
	if previous, ok := s.current[taskID]; ok {
		version = previous.TaskVersion + 1
	}
	projection, err := RenderProjection(in, version)
	if err != nil {
		return RenderedProjection{}, err
	}
	s.current[taskID] = projection
	return cloneProjection(projection), nil
}

// Current returns the server-held projection for taskID.
func (s *ProjectionStore) Current(taskID string) (RenderedProjection, bool) {
	if s == nil || taskID == "" {
		return RenderedProjection{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.current[taskID]
	if !ok {
		return RenderedProjection{}, false
	}
	return cloneProjection(p), true
}

func cloneProjection(p RenderedProjection) RenderedProjection {
	p.VisibleFields = append([]ProjectionField(nil), p.VisibleFields...)
	p.HiddenFieldManifest = append([]string(nil), p.HiddenFieldManifest...)
	p.Warnings = append([]string(nil), p.Warnings...)
	p.Effects = append([]string(nil), p.Effects...)
	p.UnknownFields = append([]string(nil), p.UnknownFields...)
	p.RedactedFields = append([]string(nil), p.RedactedFields...)
	return p
}

func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
