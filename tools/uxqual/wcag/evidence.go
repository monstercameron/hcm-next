package wcag

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"
)

type Evidence struct {
	Todo      string     `yaml:"todo"`
	Standard  string     `yaml:"standard"`
	Artifact  string     `yaml:"artifact"`
	Scenarios []Scenario `yaml:"scenarios"`
	Waivers   []Waiver   `yaml:"waivers"`
}
type Scenario struct {
	ID     string `yaml:"id"`
	Kind   string `yaml:"kind"`
	Status string `yaml:"status"`
	Method string `yaml:"method"`
}
type Waiver struct {
	ID         string `yaml:"id"`
	Criterion  string `yaml:"criterion"`
	Owner      string `yaml:"owner"`
	Severity   string `yaml:"severity"`
	Workaround string `yaml:"workaround"`
	Expires    string `yaml:"expires"`
	ApprovedBy string `yaml:"approved_by"`
}

func LoadEvidence() (Evidence, error) {
	_, file, _, _ := runtime.Caller(0)
	p := filepath.Join(filepath.Dir(file), "..", "..", "..", "definitions", "ux", "wcag", "ux-003-evidence.yaml")
	b, err := os.ReadFile(p)
	if err != nil {
		return Evidence{}, err
	}
	var e Evidence
	if err := yaml.Unmarshal(b, &e); err != nil {
		return Evidence{}, err
	}
	return e, nil
}

func (e Evidence) Validate() error {
	if e.Todo != "UX-003" || e.Standard != "WCAG 2.2 AA" || e.Artifact == "" {
		return fmt.Errorf("invalid UX-003 evidence identity")
	}
	want := map[string]bool{"zoom-200": false, "reflow-400": false, "reduced-motion": false, "accessible-auth": false}
	for _, s := range e.Scenarios {
		if _, ok := want[s.ID]; !ok {
			return fmt.Errorf("unknown scenario %q", s.ID)
		}
		if s.Kind == "" || (s.Status != "PASS" && s.Status != "PENDING") || s.Method == "" {
			return fmt.Errorf("scenario %q lacks kind/status/method", s.ID)
		}
		want[s.ID] = true
	}
	for id, ok := range want {
		if !ok {
			return fmt.Errorf("missing named scenario %q", id)
		}
	}
	for _, w := range e.Waivers {
		if w.ID == "" || w.Criterion == "" || w.Owner == "" || w.Severity == "" || w.Workaround == "" || w.Expires == "" || w.ApprovedBy == "" {
			return fmt.Errorf("waiver %q is incomplete", w.ID)
		}
	}
	return nil
}

// ReleaseReady rejects structurally valid evidence until every named manual
// scenario has a recorded pass. PENDING is intentionally not a waiver.
func (e Evidence) ReleaseReady() error {
	if err := e.Validate(); err != nil {
		return err
	}
	for _, s := range e.Scenarios {
		if s.Status != "PASS" {
			return fmt.Errorf("scenario %q has not passed", s.ID)
		}
	}
	return nil
}
