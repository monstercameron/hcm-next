package modelgen

import (
	"fmt"
	"sort"
	"strings"
)

// Import paths the generated package may need, named once so every emitter
// spells them identically.
const (
	valuesImportPath    = "github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	lifecycleImportPath = "github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
)

// fieldKind classifies how a generated struct field is declared and
// validated. It exists so [renderValidateBody] never has to pattern-match a
// Go type string at render time: [Build] resolves the kind once, up front.
type fieldKind int

// Field kinds. Each one is a distinct, closed case in every switch this
// package writes over fieldKind — there is no default case that silently
// treats an unrecognized kind as "no validation", which is what would let an
// invalid constraint through (MSRC-007 RED).
const (
	kindString fieldKind = iota
	kindStateID
	kindUint32
	kindStringSlice
	kindDecimal
	kindInterval
	kindInstant
	kindKnownAt
	kindRecordedAt
	kindDimensions
)

// typeInfo is what [typeTable] resolves one PropertyDefinition.GoType string
// to: the Go type spelling to emit, the import path it needs (empty for a
// predeclared type), and the validation kind.
type typeInfo struct {
	goType  string
	kind    fieldKind
	imports []string
}

// typeTable is the closed set of GoType strings this generator accepts. It
// is deliberately closed: [Build] rejects any PropertyDefinition.GoType not
// listed here rather than emitting `any` or `map[string]any` for it — see the
// package doc's MSRC-007 RED discussion. Extending generation to a new Go
// type is a source change to this table, never an inferred fallback.
//
// values.KnownAt and values.RecordedAt are listed even though no property in
// [github.com/monstercameron/human-capital-management-suite/internal/intent/model.Catalog] uses
// them yet: MSRC-007's GREEN clause names "known-at invariants through
// internal/kernel/values" as a validator this generator must support, and
// [TestBuildSyntheticKnownAtAndRecordedAt] exercises both against a synthetic
// registry so the path is real, not aspirational, dead code.
var typeTable = map[string]typeInfo{
	"string":   {goType: "string", kind: kindString},
	"uint32":   {goType: "uint32", kind: kindUint32},
	"[]string": {goType: "[]string", kind: kindStringSlice},
	"values.Decimal": {
		goType: "values.Decimal", kind: kindDecimal, imports: []string{valuesImportPath},
	},
	"values.EffectiveInterval": {
		goType: "values.EffectiveInterval", kind: kindInterval, imports: []string{valuesImportPath},
	},
	"values.Instant": {
		goType: "values.Instant", kind: kindInstant, imports: []string{valuesImportPath},
	},
	"values.KnownAt": {
		goType: "values.KnownAt", kind: kindKnownAt, imports: []string{valuesImportPath},
	},
	"values.RecordedAt": {
		goType: "values.RecordedAt", kind: kindRecordedAt, imports: []string{valuesImportPath},
	},
	"lifecycle.StateID": {
		goType: "lifecycle.StateID", kind: kindStateID, imports: []string{lifecycleImportPath},
	},
	"lifecycle.Dimensions": {
		goType: "lifecycle.Dimensions", kind: kindDimensions, imports: []string{lifecycleImportPath},
	},
}

// resolveGoType looks up goType in [typeTable]. It is the one place
// generation fails closed on an unrecognized modeled type.
func resolveGoType(goType string) (typeInfo, error) {
	info, ok := typeTable[goType]
	if !ok {
		return typeInfo{}, fmt.Errorf(
			"modelgen: %q is not a recognized generated Go type; add it to typeTable or fix the source property (never falls back to map[string]any or any)",
			goType)
	}
	return info, nil
}

// pascalCase converts a snake_case (optionally dotted) identifier such as
// "manager_relationship" or "effective_interval" into a Go exported field
// name such as "ManagerRelationship" or "EffectiveInterval".
func pascalCase(s string) string {
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == '_' || r == '.' })
	var b strings.Builder
	for _, f := range fields {
		if f == "" {
			continue
		}
		b.WriteString(strings.ToUpper(f[:1]))
		b.WriteString(f[1:])
	}
	return b.String()
}

// sortedKeys returns m's keys sorted, for deterministic iteration over any
// map this package builds.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
