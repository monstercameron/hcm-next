// Package deferredimports guards Phase 1's explicit non-goals (GOV-009):
// Gate A/B production code must never take a mandatory dependency on
// Kafka, ClickHouse, OpenSearch, vector infrastructure, full billing,
// payroll calculation, or omnichannel modules (execution-plan.md's
// Explicit Non-Goals). A bounded interface/port may exist; only an actual
// import in the production dependency graph is a violation.
//
// REFACTOR note: the forbidden-import set here should be driven from the
// implementation-depth registry (GOV-005) once OUT-OF-PHASE subsystems are
// enumerated there, instead of being maintained as a second hardcoded list.
package deferredimports

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// ForbiddenImport is one deferred-subsystem import path fragment: any
// import path containing it (as a substring) is prohibited in a Gate A/B
// production dependency graph.
type ForbiddenImport struct {
	Subsystem string
	Fragment  string
}

// Forbidden is the exhaustive, curated set of import-path fragments for
// the subsystems execution-plan.md's Explicit Non-Goals names as never
// mandatory in Phase 1.
var Forbidden = []ForbiddenImport{
	{Subsystem: "Kafka", Fragment: "segmentio/kafka-go"},
	{Subsystem: "Kafka", Fragment: "confluentinc/confluent-kafka-go"},
	{Subsystem: "Kafka", Fragment: "IBM/sarama"},
	{Subsystem: "Kafka", Fragment: "Shopify/sarama"},
	{Subsystem: "ClickHouse", Fragment: "clickhouse-go"},
	{Subsystem: "OpenSearch", Fragment: "opensearch-project/opensearch-go"},
	{Subsystem: "OpenSearch", Fragment: "elastic/go-elasticsearch"},
	{Subsystem: "Vector infrastructure", Fragment: "pgvector"},
	{Subsystem: "Vector infrastructure", Fragment: "milvus-io"},
	{Subsystem: "Vector infrastructure", Fragment: "weaviate"},
	{Subsystem: "Vector infrastructure", Fragment: "pinecone"},
	{Subsystem: "Vector infrastructure", Fragment: "qdrant"},
	{Subsystem: "Full billing", Fragment: "internal/billing/full"},
	{Subsystem: "Payroll calculation", Fragment: "internal/domains/payroll/calculation"},
	{Subsystem: "Payroll calculation", Fragment: "internal/payroll/calculation"},
	{Subsystem: "Omnichannel", Fragment: "internal/omnichannel"},
	{Subsystem: "Omnichannel", Fragment: "internal/messaging/omnichannel"},
}

// MatchForbidden reports the ForbiddenImport whose fragment occurs in
// importPath, if any.
func MatchForbidden(importPath string) (ForbiddenImport, bool) {
	for _, f := range Forbidden {
		if strings.Contains(importPath, f.Fragment) {
			return f, true
		}
	}
	return ForbiddenImport{}, false
}

// Violation names one forbidden import found in one production source file.
type Violation struct {
	File      string
	Import    string
	Subsystem string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s: imports %q (%s is a Phase 1 non-goal and must never be a mandatory dependency)", v.File, v.Import, v.Subsystem)
}

// ScanDir walks root (recursively) for non-test .go files and returns a
// Violation for every forbidden import found. Only import declarations are
// parsed (parser.ImportsOnly), so this never requires the packages to
// build.
func ScanDir(root string) ([]Violation, error) {
	var violations []Violation
	fset := token.NewFileSet()

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case "testdata", ".git", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		for _, imp := range file.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if f, ok := MatchForbidden(importPath); ok {
				violations = append(violations, Violation{File: path, Import: importPath, Subsystem: f.Subsystem})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return violations, nil
}
