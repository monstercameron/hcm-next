package todogovernance

import (
	"fmt"
	"strings"
)

const (
	CodeMissingIntentContext = "MISSING_INTENT_CONTEXT"
	CodeUnknownRole          = "UNKNOWN_ROLE"
	CodeUnknownSet           = "UNKNOWN_SET"
	CodeMissingSets          = "MISSING_SETS"
	CodeInvalidDirect        = "INVALID_DIRECT"
	CodeEmptyWhy             = "EMPTY_WHY"
)

// declaredRoles is the INTENT CONTEXT ROLE vocabulary from
// "### BusinessIntent context required by every todo" in planning/todos.md.
var declaredRoles = map[string]bool{
	"DIRECT":         true,
	"COMPOSITE":      true,
	"EMITTER":        true,
	"EXPOSURE":       true,
	"DOMAIN_SUPPORT": true,
	"CONFORMANCE":    true,
	"GOVERNANCE":     true,
	"SUBSTRATE":      true,
}

// declaredSets is the SETS domain vocabulary from the same section, plus
// their union "BI.ALL".
var declaredSets = map[string]bool{
	"BI.PEOPLE": true, "BI.WORKFORCE": true, "BI.REWARDS": true, "BI.PAYROLL": true,
	"BI.REGULATORY": true, "BI.RECRUITING": true, "BI.LIFECYCLE": true, "BI.ACCESS": true,
	"BI.TALENT": true, "BI.EXPERIENCE": true, "BI.CASES": true, "BI.MOBILITY": true,
	"BI.PRIVACY": true, "BI.DOCUMENTS": true, "BI.WORK": true, "BI.INTEGRATION": true,
	"BI.DATAOPS": true, "BI.SECURITY": true, "BI.ANALYTICS": true, "BI.INTELLIGENCE": true,
	"BI.OPERATIONS": true, "BI.COMMERCIAL": true, "BI.TENANT": true, "BI.TRIGGERS": true,
	"BI.ALL": true,
}

// ValidateIntentContext runs the GOV-025 checks against each record's
// INTENT CONTEXT field: ROLE must be one of the eight declared roles, SETS
// must be a non-empty comma-separated list drawn from the declared BI.*
// domains (or their union BI.ALL), DIRECT must be either "none" or a
// BusinessIntent name from the catalog (comma-separated names are checked
// individually), and WHY must be non-empty. catalogDefinitions is the list
// returned by LoadCatalogDefinitions/ParseCatalogDefinitions.
func ValidateIntentContext(records []Record, catalogDefinitions []string) []Finding {
	catalog := make(map[string]bool, len(catalogDefinitions))
	catalogBase := make(map[string]bool, len(catalogDefinitions))
	for _, ref := range catalogDefinitions {
		catalog[ref] = true
		if base, _, ok := strings.Cut(ref, "/"); ok {
			catalogBase[base] = true
		}
	}

	var findings []Finding
	for _, rec := range records {
		t := rec.Todo
		if rec.IntentContextRaw == "" {
			findings = append(findings, Finding{
				TodoID: t.ID, Rule: "GOV-025", Code: CodeMissingIntentContext, Line: t.Line,
				Reason: "todo has no INTENT CONTEXT field",
			})
			continue
		}

		fields := rec.IntentContext
		if role := fields["ROLE"]; role == "" || !declaredRoles[role] {
			findings = append(findings, Finding{
				TodoID: t.ID, Rule: "GOV-025", Code: CodeUnknownRole, Line: t.Line, Detail: role,
				Reason: fmt.Sprintf("ROLE %q is not one of the declared roles", role),
			})
		}

		sets, hasSets := fields["SETS"]
		if !hasSets || strings.TrimSpace(sets) == "" {
			findings = append(findings, Finding{
				TodoID: t.ID, Rule: "GOV-025", Code: CodeMissingSets, Line: t.Line,
				Reason: "INTENT CONTEXT has no SETS field",
			})
		} else {
			for _, tok := range strings.Split(sets, ",") {
				tok = strings.TrimSpace(tok)
				if tok == "" {
					continue
				}
				if !declaredSets[tok] {
					findings = append(findings, Finding{
						TodoID: t.ID, Rule: "GOV-025", Code: CodeUnknownSet, Line: t.Line, Detail: tok,
						Reason: fmt.Sprintf("SETS entry %q is not a declared BI.* domain", tok),
					})
				}
			}
		}

		if direct, hasDirect := fields["DIRECT"]; hasDirect {
			for _, tok := range strings.Split(direct, ",") {
				tok = strings.TrimSpace(tok)
				if tok == "" || tok == "none" {
					continue
				}
				if !catalog[tok] && !catalogBase[tok] {
					findings = append(findings, Finding{
						TodoID: t.ID, Rule: "GOV-025", Code: CodeInvalidDirect, Line: t.Line, Detail: tok,
						Reason: fmt.Sprintf("DIRECT %q is neither \"none\" nor a BusinessIntent name from the catalog", tok),
					})
				}
			}
		}

		if why := strings.TrimSpace(fields["WHY"]); why == "" {
			findings = append(findings, Finding{
				TodoID: t.ID, Rule: "GOV-025", Code: CodeEmptyWhy, Line: t.Line,
				Reason: "INTENT CONTEXT has no WHY field or WHY is empty",
			})
		}
	}

	sortFindings(findings)
	return findings
}
