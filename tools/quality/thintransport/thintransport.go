// Package thintransport checks the source boundary around transport adapters.
//
// The check intentionally operates on Go source and imports only.  It does not
// load packages, run generated code, or inspect runtime behaviour.  Transport
// is allowed to depend on generated contracts and application/intent APIs, but
// must not acquire business ownership or persistence/provider implementations.
package thintransport

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Finding identifies one thin-transport source violation.
type Finding struct {
	Code   string
	Path   string
	Line   int
	Detail string
}

func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: %s (%s)", f.Path, f.Line, f.Detail, f.Code)
}

// Check scans production Go files in the transport adapter roles. root is the
// repository root, or a fixture root containing internal/transport. Test files
// and generated files are deliberately excluded.
func Check(root string) []Finding {
	var files []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "vendor/") || strings.Contains(rel, "/gen/") || strings.HasPrefix(rel, "gen/") || strings.HasSuffix(rel, ".gen.go") {
			return nil
		}
		if transportRole(rel) {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	var out []Finding
	for _, path := range files {
		out = append(out, inspectFile(root, path)...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Code < out[j].Code
	})
	return out
}

// CheckFile checks one source file, useful to policy and mutation tests.
func CheckFile(root, path string) []Finding { return inspectFile(root, path) }

func transportRole(rel string) bool {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] != "internal" || parts[i+1] != "transport" {
			continue
		}
		if i+2 >= len(parts) {
			return false
		}
		switch parts[i+2] {
		case "grpc", "grpcbridge", "middleware", // canonical roles
			"grpcserver", "edge", "otelmw": // selected fallback implementations
			return true
		}
	}
	return false
}

func inspectFile(root, path string) []Finding {
	rel, _ := filepath.Rel(root, path)
	rel = filepath.ToSlash(rel)
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return []Finding{{Code: "parse-error", Path: rel, Detail: err.Error()}}
	}
	var out []Finding
	for _, imp := range f.Imports {
		ip := strings.Trim(imp.Path.Value, "\"")
		if code, ok := forbiddenImport(ip); ok {
			p := fset.Position(imp.Pos())
			out = append(out, Finding{Code: code, Path: rel, Line: p.Line, Detail: "transport imports forbidden business/persistence implementation " + ip})
		}
	}
	return out
}

func forbiddenImport(path string) (string, bool) {
	if strings.Contains(path, "/internal/data/") || strings.HasSuffix(path, "/internal/data") ||
		strings.Contains(path, "/internal/storage/") || strings.Contains(path, "/internal/persistence/") {
		return "persistence-import", true
	}
	if strings.HasPrefix(path, "github.com/jackc/pgx") || path == "database/sql" ||
		strings.Contains(path, "/go-redis") || strings.Contains(path, "/minio-go") ||
		strings.Contains(path, "/aws-sdk-") {
		return "provider-import", true
	}
	if strings.Contains(path, "/internal/domains/") || strings.Contains(path, "/internal/domain/") ||
		strings.Contains(path, "/internal/repositories/") || strings.Contains(path, "/internal/providers/") {
		return "business-import", true
	}
	return "", false
}
