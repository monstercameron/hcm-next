// Package workflowdesignjoin joins each drafted BusinessIntent definition
// to exactly one workflow design disposition (WF-DISC-006): definitions
// resolve by stable semantic identity (definition_ref with version) to
// the design records owned by tools/planning/workflowdesign, display
// names are never join keys, and the denominator is always the accepted
// definition list, never the record corpus. It is kernel-pure: pure
// snapshot compilation plus text emission, no database, network or
// mutable global state.
package workflowdesignjoin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowdesign"
)

// Join finding codes. MISSING_DESIGN marks an accepted definition with no
// record; DUPLICATE_DESIGN marks a definition claimed twice;
// ALIAS_RECORD marks a record with no definition ref (display names must
// never be join keys); UNKNOWN_DEFINITION marks a record bound outside
// the accepted list, counted nowhere.
const (
	MissingDesign     = "MISSING_DESIGN"
	DuplicateDesign   = "DUPLICATE_DESIGN"
	AliasRecord       = "ALIAS_RECORD"
	UnknownDefinition = "UNKNOWN_DEFINITION"
	InvalidDefinition = "INVALID_DEFINITION"
)

// Binding is one accepted definition joined to its design record's
// display intent.
type Binding struct {
	Definition string `json:"definition"`
	Intent     string `json:"intent"`
}

// Join is the compiled one-to-one join over accepted definitions.
type Join struct {
	Matched  int       `json:"matched"`
	Total    int       `json:"total"`
	Bindings []Binding `json:"bindings"`
	Digest   string    `json:"digest"`
}

// Complete reports whether every accepted definition binds exactly one
// record. Callers must also require zero findings: a complete count with
// alias or unknown records is not a green join.
func (j Join) Complete() bool { return j.Matched == j.Total }

// Finding is one source-exact join diagnostic.
type Finding struct {
	Definition string `json:"definition,omitempty"`
	Intent     string `json:"intent,omitempty"`
	Code       string `json:"code"`
	Detail     string `json:"detail"`
}

// MarshalJoin renders a join with its findings as canonical JSON.
func MarshalJoin(join Join, findings []Finding) ([]byte, error) {
	ordered := append([]Finding(nil), findings...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Definition != ordered[j].Definition {
			return ordered[i].Definition < ordered[j].Definition
		}
		if ordered[i].Code != ordered[j].Code {
			return ordered[i].Code < ordered[j].Code
		}
		return ordered[i].Detail < ordered[j].Detail
	})
	rendered, err := json.MarshalIndent(struct {
		Join     Join      `json:"join"`
		Findings []Finding `json:"findings,omitempty"`
	}{Join: join, Findings: ordered}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(rendered, '\n'), nil
}

// JoinRecords compiles the one-to-one join of accepted definition refs
// to design records. Records bind by definition ref only; the accepted
// list is the only denominator.
func JoinRecords(accepted []string, records []workflowdesign.DesignRecord) (Join, []Finding) {
	var findings []Finding
	acceptedSet := make(map[string]bool, len(accepted))
	for _, id := range accepted {
		acceptedSet[id] = true
	}
	byDefinition := make(map[string][]workflowdesign.DesignRecord)
	for _, record := range records {
		definition := strings.TrimSpace(record.Definition)
		switch {
		case definition == "":
			findings = append(findings, Finding{Intent: record.Intent, Code: AliasRecord, Detail: "record carries no definition ref; display names must never be join keys"})
		case !workflowdesign.ValidDefinition(definition):
			findings = append(findings, Finding{Intent: record.Intent, Code: InvalidDefinition, Detail: "definition " + definition + " is not a versioned definition_ref"})
		case !acceptedSet[definition]:
			findings = append(findings, Finding{Definition: definition, Intent: record.Intent, Code: UnknownDefinition, Detail: "record binds outside the accepted definitions; it joins nothing and counts nowhere"})
		default:
			byDefinition[definition] = append(byDefinition[definition], record)
		}
	}
	join := Join{Total: len(accepted)}
	for _, id := range accepted {
		claimants := byDefinition[id]
		switch len(claimants) {
		case 0:
			findings = append(findings, Finding{Definition: id, Code: MissingDesign, Detail: "accepted definition has no design record"})
		case 1:
			join.Bindings = append(join.Bindings, Binding{Definition: id, Intent: claimants[0].Intent})
			join.Matched++
		default:
			// A duplicated definition binds nothing: the join is
			// one-to-one or it is not a join.
			for _, extra := range claimants {
				findings = append(findings, Finding{Definition: id, Intent: extra.Intent, Code: DuplicateDesign, Detail: "accepted definition has more than one design record"})
			}
		}
	}
	sort.Slice(join.Bindings, func(i, j int) bool { return join.Bindings[i].Definition < join.Bindings[j].Definition })
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Definition != findings[j].Definition {
			return findings[i].Definition < findings[j].Definition
		}
		if findings[i].Code != findings[j].Code {
			return findings[i].Code < findings[j].Code
		}
		return findings[i].Detail < findings[j].Detail
	})
	join.Digest = digestJoin(join.Bindings)
	return join, findings
}

func digestJoin(bindings []Binding) string {
	var lines []string
	for _, binding := range bindings {
		lines = append(lines, binding.Definition+"\x00"+binding.Intent)
	}
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
