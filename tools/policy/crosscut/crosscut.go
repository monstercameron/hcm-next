package crosscut

import (
	"strings"
)

var crossCuttingRoots = []string{
	"internal/trust",
	"internal/platform/telemetry",
	"internal/platform/bootstrap",
	"internal/operations",
}

var forbiddenTargets = []string{
	"internal/domains",
	"internal/data/postgres",
	"internal/data/ledger",
	"internal/connectivity",
}

func isCrossCutting(importerRel string) bool {
	for _, r := range crossCuttingRoots {
		if importerRel == r || strings.HasPrefix(importerRel, r+"/") {
			return true
		}
	}
	return false
}

func isForbiddenTarget(importedRel string) bool {
	for _, f := range forbiddenTargets {
		if importedRel == f || strings.HasPrefix(importedRel, f+"/") {
			return true
		}
		if strings.Contains(f, "*") {
			parts := strings.Split(f, "/")
			impParts := strings.Split(importedRel, "/")
			if len(impParts) < len(parts) {
				continue
			}
			match := true
			for i, p := range parts {
				if p == "*" {
					continue
				}
				if p != impParts[i] {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
	}
	return false
}

type Violation struct {
	Importer string
	Imported string
	Rule     string
}

func CheckEdge(module, importer, imported string) *Violation {
	prefix := module + "/"
	if !strings.HasPrefix(importer, prefix) || !strings.HasPrefix(imported, prefix) {
		return nil
	}
	impRel := strings.TrimPrefix(importer, prefix)
	tgtRel := strings.TrimPrefix(imported, prefix)
	if !isCrossCutting(impRel) {
		return nil
	}
	if isForbiddenTarget(tgtRel) {
		return &Violation{Importer: importer, Imported: imported, Rule: "crosscut-must-not-import-domain"}
	}
	if tgtRel == "internal/intelligence" || strings.HasPrefix(tgtRel, "internal/intelligence/") {
		return &Violation{Importer: importer, Imported: imported, Rule: "crosscut-must-not-require-intelligence"}
	}
	if tgtRel == "internal/agent" || strings.HasPrefix(tgtRel, "internal/agent/") {
		return &Violation{Importer: importer, Imported: imported, Rule: "crosscut-must-not-require-agent"}
	}
	return nil
}

func CheckGraph(module string, edges [][2]string) []Violation {
	var out []Violation
	for _, e := range edges {
		if v := CheckEdge(module, e[0], e[1]); v != nil {
			out = append(out, *v)
		}
	}
	return out
}

func AllowedPorts() []string {
	return []string{
		"internal/capability",
		"internal/ledger",
		"internal/data",
		"internal/platform/telemetry",
	}
}
