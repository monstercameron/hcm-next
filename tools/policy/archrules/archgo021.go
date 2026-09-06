package archrules

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	// IntegrationRuleConnectivityTransformationSurface is the rule for the
	// narrow transformation surface visible to connectivity packages.
	IntegrationRuleConnectivityTransformationSurface = "connectivity-transformation-surface"
	// IntegrationRuleConnectivityTransformationVersion forbids connectivity
	// from reaching transformation versioning implementation details.
	IntegrationRuleConnectivityTransformationVersion = "connectivity-must-not-import-transformation-version"
	// IntegrationRuleMappingExecuteOnlyIRExecutor makes the executor package
	// the sole connectivity caller allowed to execute transformation IR.
	IntegrationRuleMappingExecuteOnlyIRExecutor = "mapping-execute-is-only-ir-executor"
	// IntegrationRuleTransformationConnectivitySurface makes adapters the
	// only transformation package allowed to depend on connectivity.
	IntegrationRuleTransformationConnectivitySurface = "transformation-connectivity-must-go-through-adapters"
	// IntegrationRuleSchemasnapshotMapping forbids schema snapshots from
	// depending on mapping semantics.
	IntegrationRuleSchemasnapshotMapping = "schemasnapshot-must-not-import-mapping"
	// IntegrationRuleMFTMapping forbids managed file transfer from depending
	// on mapping semantics.
	IntegrationRuleMFTMapping = "mft-must-not-import-mapping"
	// IntegrationRuleMFTTransformation forbids managed file transfer from
	// depending on transformation semantics.
	IntegrationRuleMFTTransformation = "mft-must-not-import-transformation"
)

// IntegrationBoundary describes the ARCH-GO-021 package roots. It is kept as
// data so fixture tests can exercise the checker without changing the real
// repository policy.
type IntegrationBoundary struct {
	Module                     string
	ConnectivityRoot           string
	TransformationRoot         string
	TransformationAdaptersRoot string
	TransformationIRRoot       string
	TransformationExecRoot     string
	TransformationLineageRoot  string
	TransformationVersionRoot  string
	MappingRoot                string
	MappingExecuteRoot         string
	SchemasnapshotRoot         string
	MFTRoot                    string
}

// DefaultIntegrationBoundary returns the ARCH-GO-021 boundary for the HCM
// Next module. Callers may replace Module when scanning a synthetic module.
func DefaultIntegrationBoundary(module string) IntegrationBoundary {
	return IntegrationBoundary{
		Module:                     module,
		ConnectivityRoot:           "internal/connectivity",
		TransformationRoot:         "internal/engines/transformation",
		TransformationAdaptersRoot: "internal/engines/transformation/adapters",
		TransformationIRRoot:       "internal/engines/transformation/ir",
		TransformationExecRoot:     "internal/engines/transformation/exec",
		TransformationLineageRoot:  "internal/engines/transformation/lineage",
		TransformationVersionRoot:  "internal/engines/transformation/version",
		MappingRoot:                "internal/connectivity/mapping",
		MappingExecuteRoot:         "internal/connectivity/mapping/execute",
		SchemasnapshotRoot:         "internal/connectivity/schemasnapshot",
		MFTRoot:                    "internal/connectivity/mft",
	}
}

// IntegrationAllowedEdge is one row in the reviewed allowed-edge table.
// A ** suffix means any descendant package; the table is a Golden-level
// description, while the checker uses exact root-prefix matching.
type IntegrationAllowedEdge struct {
	From   string
	To     string
	Detail string
}

// IntegrationAllowedEdges is the complete permitted cross-boundary table.
// Conditional rows state the extra ownership constraint in Detail.
var IntegrationAllowedEdges = []IntegrationAllowedEdge{
	{From: "internal/connectivity/**", To: "internal/engines/transformation/ir/**", Detail: "connectivity may consume IR contracts"},
	{From: "internal/connectivity/mapping/execute", To: "internal/engines/transformation/exec/**", Detail: "only mapping/execute may execute IR"},
	{From: "internal/connectivity/**", To: "internal/engines/transformation/lineage/**", Detail: "connectivity may consume lineage contracts"},
	{From: "internal/connectivity/**", To: "internal/engines/transformation/adapters/**", Detail: "connectivity may consume adapter contracts"},
	{From: "internal/engines/transformation/adapters/**", To: "internal/connectivity/**", Detail: "only transformation adapters may depend on connectivity"},
}

// IntegrationException is a narrowly pinned, reviewed exception to the
// allowed-edge table. Exceptions are exact importer/imported package pairs;
// they cannot silently widen to a subtree.
type IntegrationException struct {
	Importer  string
	Imported  string
	OwnerTodo string
	Reason    string
}

var integrationExceptions = []IntegrationException{
	{
		Importer:  "internal/connectivity/mapping/execute",
		Imported:  "internal/engines/transformation",
		OwnerTodo: "ARCH-GO-021",
		Reason:    "shared transformation scalar types remain a compatibility edge until the executor surface is narrowed",
	},
	{
		Importer:  "internal/connectivity/mapping/execute",
		Imported:  "internal/engines/transformation/taint",
		OwnerTodo: "ARCH-GO-021",
		Reason:    "the executor carries the shared taint envelope until that concern is exposed through the approved executor surface",
	},
}

// IntegrationExceptions returns a copy of the live exception list for
// Golden and integration checks.
func IntegrationExceptions() []IntegrationException {
	return append([]IntegrationException(nil), integrationExceptions...)
}

// IntegrationViolation is one file-level ARCH-GO-021 finding. File is
// module-relative when produced by ScanIntegrationTree, and is the fixture
// filename when produced by ScanIntegrationSource.
type IntegrationViolation struct {
	Rule     string
	File     string
	Line     int
	Importer string
	Imported string
	Detail   string
}

func (v IntegrationViolation) Error() string {
	location := v.File
	if v.Line > 0 {
		location = fmt.Sprintf("%s:%d", location, v.Line)
	}
	return fmt.Sprintf("ARCH-GO-021 violation: %s: %s imports %s (%s)", location, v.Importer, v.Imported, v.Rule)
}

// CheckIntegrationImport checks one direct module-relative import edge.
// It is intentionally independent of go list so callers can use it for
// synthetic AST fixtures as well as the real source tree.
func CheckIntegrationImport(b IntegrationBoundary, file, importerRel, importedRel string) []IntegrationViolation {
	importerRel = filepath.ToSlash(importerRel)
	importedRel = filepath.ToSlash(importedRel)
	var violations []IntegrationViolation
	add := func(rule, detail string) {
		violations = append(violations, IntegrationViolation{
			Rule: rule, File: filepath.ToSlash(file), Importer: importerRel, Imported: importedRel, Detail: detail,
		})
	}

	if UnderRoot(importerRel, b.ConnectivityRoot) && UnderRoot(importedRel, b.TransformationRoot) {
		switch {
		case UnderRoot(importedRel, b.TransformationVersionRoot):
			add(IntegrationRuleConnectivityTransformationVersion, "version internals are never a connectivity dependency")
		case !UnderAnyRoot(importedRel, []string{
			b.TransformationIRRoot,
			b.TransformationExecRoot,
			b.TransformationLineageRoot,
			b.TransformationAdaptersRoot,
		}):
			add(IntegrationRuleConnectivityTransformationSurface, "connectivity may use only ir, exec, lineage, or adapters")
		}
		if UnderRoot(importedRel, b.TransformationExecRoot) && !UnderRoot(importerRel, b.MappingExecuteRoot) {
			add(IntegrationRuleMappingExecuteOnlyIRExecutor, "only internal/connectivity/mapping/execute may import transformation/exec")
		}
	}

	if UnderRoot(importerRel, b.TransformationRoot) && UnderRoot(importedRel, b.ConnectivityRoot) && !UnderRoot(importerRel, b.TransformationAdaptersRoot) {
		add(IntegrationRuleTransformationConnectivitySurface, "transformation may depend on connectivity only from adapters")
	}
	if UnderRoot(importerRel, b.SchemasnapshotRoot) && UnderRoot(importedRel, b.MappingRoot) {
		add(IntegrationRuleSchemasnapshotMapping, "schemasnapshot owns shape capture, not mapping semantics")
	}
	if UnderRoot(importerRel, b.MFTRoot) {
		if UnderRoot(importedRel, b.MappingRoot) {
			add(IntegrationRuleMFTMapping, "mft imports neither mapping nor its subpackages")
		}
		if UnderRoot(importedRel, b.TransformationRoot) {
			add(IntegrationRuleMFTTransformation, "mft imports neither transformation nor its subpackages")
		}
	}

	return violations
}

// ScanIntegrationSource parses one Go source file's imports and checks every
// within-module edge. Test files are excluded by ScanIntegrationTree, but a
// direct caller can use this function for any fixture source.
func ScanIntegrationSource(b IntegrationBoundary, importerRel, filename, source string) ([]IntegrationViolation, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, source, parser.ImportsOnly)
	if err != nil {
		return nil, fmt.Errorf("archrules: parsing %s: %w", filename, err)
	}

	var violations []IntegrationViolation
	for _, spec := range file.Imports {
		importedPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return nil, fmt.Errorf("archrules: unquoting import in %s: %w", filename, err)
		}
		importedRel, ok := TrimModule(b.Module, importedPath)
		if !ok {
			continue
		}
		for _, violation := range CheckIntegrationImport(b, filename, importerRel, importedRel) {
			violation.Line = fset.Position(spec.Pos()).Line
			violations = append(violations, violation)
		}
	}
	return violations, nil
}

// ScanIntegrationTree parses all production Go files below connectivity and
// transformation and returns deterministic file-level findings.
func ScanIntegrationTree(root string, b IntegrationBoundary) ([]IntegrationViolation, error) {
	var violations []IntegrationViolation
	for _, packageRoot := range []string{b.ConnectivityRoot, b.TransformationRoot} {
		dir := filepath.Join(root, filepath.FromSlash(packageRoot))
		if _, err := os.Stat(dir); err != nil {
			return nil, fmt.Errorf("archrules: stat %s: %w", dir, err)
		}
		err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			relFile, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			relDir, err := filepath.Rel(root, filepath.Dir(path))
			if err != nil {
				return err
			}
			found, err := ScanIntegrationSource(b, filepath.ToSlash(relDir), filepath.ToSlash(relFile), string(data))
			if err != nil {
				return err
			}
			violations = append(violations, found...)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("archrules: walking %s: %w", dir, err)
		}
	}

	sort.Slice(violations, func(i, j int) bool {
		left, right := violations[i], violations[j]
		if left.File != right.File {
			return left.File < right.File
		}
		if left.Line != right.Line {
			return left.Line < right.Line
		}
		if left.Rule != right.Rule {
			return left.Rule < right.Rule
		}
		return left.Imported < right.Imported
	})
	return violations, nil
}

// IsIntegrationException reports whether an exact edge is covered by the
// reviewed live exception list. The source file is deliberately not part of
// the key: the exception is package-edge scoped, never a wildcard file or
// subtree exception.
func IsIntegrationException(importerRel, importedRel string) bool {
	for _, exception := range integrationExceptions {
		if exception.Importer == importerRel && exception.Imported == importedRel {
			return true
		}
	}
	return false
}
