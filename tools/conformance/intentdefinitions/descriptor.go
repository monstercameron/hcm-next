// Package intentdefinitions contains the finite, reviewable conformance
// descriptors for the initial BusinessIntent draft-contract slice.
package intentdefinitions

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Descriptor is the conformance contract for one immutable definition.
// Slices and maps are deliberately data-only: no executable policy can hide
// in a descriptor.
type Descriptor struct {
	IntentTypeID      string           `json:"intent_type_id"`
	Version           int              `json:"version"`
	DisplayName       string           `json:"display_name"`
	Family            string           `json:"family"`
	Entities          []string         `json:"entities"`
	Properties        []string         `json:"properties"`
	Reads             []string         `json:"reads"`
	Writes            []string         `json:"writes"`
	Effects           []string         `json:"effects"`
	Authority         []string         `json:"authority"`
	Time              []string         `json:"time"`
	Lifecycle         Lifecycle        `json:"lifecycle"`
	Evidence          []string         `json:"evidence"`
	NegativePolicy    []NegativePolicy `json:"negative_policy"`
	Scenario          Scenario         `json:"scenario"`
	SideEffectProfile string           `json:"side_effect_profile"`
	ConformanceOnly   bool             `json:"conformance_only,omitempty"`
	SourceFile        string           `json:"source_file"`
}

type Lifecycle struct {
	Request     string `json:"request"`
	Execution   string `json:"execution"`
	Business    string `json:"business"`
	Consistency string `json:"consistency"`
	Obligation  string `json:"obligation"`
}
type NegativePolicy struct {
	Condition   string `json:"condition"`
	Disposition string `json:"disposition"`
}
type Scenario struct {
	Name  string `json:"name"`
	Given string `json:"given"`
	When  string `json:"when"`
	Then  string `json:"then"`
}

var expectedIDs = []string{
	"hcmnext.people.change_manager", "hcmnext.people.explain_worker_state", "hcmnext.people.promote_worker",
	"hcmnext.rewards.change_base_pay", "hcmnext.rewards.simulate_compensation", "hcmnext.rewards.evaluate_pay_band_position",
	"hcmnext.rewards.reserve_compensation_budget", "hcmnext.rewards.release_compensation_budget",
	"hcmnext.work.approve_proposal", "hcmnext.work.reject_proposal", "hcmnext.intelligence.explain_transaction",
	"hcmnext.operations.detect_drift", "hcmnext.operations.create_repair_plan", "hcmnext.operations.simulate_repair",
}

func ExpectedIDs() []string { return append([]string(nil), expectedIDs...) }

func (d Descriptor) key() string { return fmt.Sprintf("%s/v%d", d.IntentTypeID, d.Version) }

// Validate enforces finite coverage and all mandatory conformance dimensions.
func Validate(ds []Descriptor) error {
	if len(ds) != len(expectedIDs) {
		return fmt.Errorf("descriptor count=%d, want %d", len(ds), len(expectedIDs))
	}
	expected := map[string]bool{}
	for _, id := range expectedIDs {
		expected[id] = true
	}
	seen := map[string]bool{}
	for _, d := range ds {
		if !expected[d.IntentTypeID] {
			return fmt.Errorf("undrafted intent %q", d.IntentTypeID)
		}
		if d.Version != 1 || !strings.HasPrefix(d.IntentTypeID, "hcmnext.") {
			return fmt.Errorf("invalid identity %q", d.key())
		}
		if seen[d.key()] {
			return fmt.Errorf("duplicate descriptor %s", d.key())
		}
		seen[d.key()] = true
		for n, v := range map[string][]string{"entities": d.Entities, "properties": d.Properties, "reads": d.Reads, "writes": d.Writes, "effects": d.Effects, "authority": d.Authority, "time": d.Time, "evidence": d.Evidence} {
			if len(v) == 0 {
				return fmt.Errorf("%s: missing %s", d.key(), n)
			}
		}
		if d.Family == "" || d.SideEffectProfile == "" || d.Lifecycle.Request == "" || d.Lifecycle.Execution == "" || d.Lifecycle.Business == "" || d.Lifecycle.Consistency == "" || d.Lifecycle.Obligation == "" {
			return fmt.Errorf("%s: incomplete family/effect/lifecycle", d.key())
		}
		if d.Family == "ANALYTICAL_REQUEST" && d.SideEffectProfile != "READ_ONLY" || d.Family == "CALCULATION_REQUEST" && d.SideEffectProfile != "PURE" {
			return fmt.Errorf("%s: family %s cannot use side effect %s", d.key(), d.Family, d.SideEffectProfile)
		}
		if (d.Family == "ANALYTICAL_REQUEST" || d.Family == "CALCULATION_REQUEST") && len(d.Writes) != 1 || (d.Family == "ANALYTICAL_REQUEST" || d.Family == "CALCULATION_REQUEST") && d.Writes[0] != "none" {
			return fmt.Errorf("%s: read/calculation descriptor declares writes", d.key())
		}
		if len(d.NegativePolicy) == 0 || d.Scenario.Name == "" || d.Scenario.Given == "" || d.Scenario.When == "" || d.Scenario.Then == "" {
			return fmt.Errorf("%s: missing negative policy or scenario", d.key())
		}
		if d.ConformanceOnly && d.IntentTypeID != "hcmnext.people.change_manager" {
			return fmt.Errorf("%s: only change_manager may be conformance-only", d.key())
		}
	}
	for _, id := range expectedIDs {
		if !seen[id+"/v1"] {
			return fmt.Errorf("missing descriptor %s/v1", id)
		}
	}
	return nil
}

func Digest(ds []Descriptor) (string, error) {
	cpy := append([]Descriptor(nil), ds...)
	sort.Slice(cpy, func(i, j int) bool { return cpy[i].key() < cpy[j].key() })
	b, err := json.Marshal(cpy)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
