package phaseone

import (
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/layout"
)

var allowedPhaseOneLayers = map[string]bool{
	"application":  true,
	"kernel":       true,
	"intent":       true,
	"capability":   true,
	"governance":   true,
	"engines":      true,
	"workflow":     true,
	"transaction":  true,
	"data":         true,
	"connectivity": true,
	"trust":        true,
	"operations":   true,
	"platform":     true,
	"transport":    true,
	"domains":      true,
}

func isPhaseOneRoot(m *layout.Manifest, name string) bool {
	for _, r := range m.InternalPackageRoots {
		if r.Name == name {
			return r.Phase == "P1A"
		}
	}
	return false
}

func DeferredRoots(m *layout.Manifest) []string {
	var out []string
	for _, r := range m.InternalPackageRoots {
		if r.Phase != "P1A" {
			out = append(out, "internal/"+r.Name)
		}
	}
	return out
}

func PhaseOneRoots(m *layout.Manifest) []string {
	var out []string
	for _, r := range m.InternalPackageRoots {
		if r.Phase == "P1A" {
			out = append(out, "internal/"+r.Name)
		}
	}
	return out
}

func IsDeferredImport(m *layout.Manifest, importPath string) bool {
	rel := strings.TrimPrefix(importPath, m.Module+"/")
	for _, d := range DeferredRoots(m) {
		if rel == d || strings.HasPrefix(rel, d+"/") {
			return true
		}
	}
	return false
}

type Violation struct {
	Importer string
	Imported string
	Reason   string
}

func CheckGraph(m *layout.Manifest, edges [][2]string) []Violation {
	var v []Violation
	for _, e := range edges {
		if IsDeferredImport(m, e[1]) {
			v = append(v, Violation{Importer: e[0], Imported: e[1], Reason: "deferred package in Phase 1 graph"})
		}
	}
	return v
}

func ValidateManifest(m *layout.Manifest) error {
	if len(PhaseOneRoots(m)) == 0 {
		return fmt.Errorf("phaseone: no P1A roots")
	}
	for _, r := range m.InternalPackageRoots {
		if r.Phase == "P1A" && !allowedPhaseOneLayers[r.Layer] {
			return fmt.Errorf("phaseone: root %q layer %q not allowed in Phase 1", r.Name, r.Layer)
		}
	}
	expected := map[string]bool{
		"kernel": true, "intent": true, "capability": true, "governance": true,
		"engines": true, "ledger": true, "data": true, "connectivity": true,
		"trust": true, "operations": true, "platform": true, "transport": true,
	}
	for _, r := range m.InternalPackageRoots {
		if r.Phase == "P1A" {
			delete(expected, r.Name)
		}
	}
	return nil
}

func Explain(m *layout.Manifest) map[string]string {
	out := make(map[string]string)
	for _, r := range m.InternalPackageRoots {
		if r.Phase == "P1A" {
			out["internal/"+r.Name] = r.Layer + ":" + r.Owner
		}
	}
	return out
}
