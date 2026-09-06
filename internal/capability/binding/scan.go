package binding

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"sort"
	"strings"
)

// ScanHandlerSymbols parses Go sources and returns every package-level
// function and method declared in them, keyed by [HandlerSymbol.Ref]. This
// is how a claimed handler symbol is proved to exist rather than merely
// spelled plausibly: a rename, a move or a typo turns the claim into a
// [GapMissingHandlerSymbol] on the next run.
//
// sources is keyed by module-relative file path ("internal/transport/admin/
// server.go"); the value is that file's source text. The package path of a
// symbol is the file's directory, so the caller never has to state it
// twice. Keeping the file reading outside this function is what lets this
// package stay kernel-pure: the tests walk the live tree and hand the bytes
// in.
//
// Files whose name ends in "_test.go" are skipped: a handler that exists
// only in a test is not an implementation, and letting one satisfy a claim
// would be exactly the dangling binding BIND-001 forbids.
//
// A file that does not parse is an error naming that file, never a silently
// smaller index — an unreadable tree must not look like a clean one.
func ScanHandlerSymbols(sources map[string]string) (HandlerIndex, error) {
	index := HandlerIndex{}
	for _, name := range sortedKeys(sources) {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		pkgPath := path.Dir(path.Clean(strings.ReplaceAll(name, "\\", "/")))
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, sources[name], parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("binding: parsing %s: %w", name, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			sym := HandlerSymbol{PackagePath: pkgPath, Name: fn.Name.Name, Receiver: receiverName(fn)}
			index[sym.Ref()] = true
		}
	}
	return index, nil
}

// receiverName renders a method's receiver type the way the source spells
// it ("*domainHandlers", "server"), or "" for a package-level function. A
// generic receiver ("*Cache[K,V]") renders by its base type name, because
// the type arguments are declaration-site names, not part of the symbol's
// identity.
func receiverName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	return renderReceiverType(fn.Recv.List[0].Type)
}

func renderReceiverType(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return "*" + renderReceiverType(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return renderReceiverType(t.X)
	case *ast.IndexListExpr:
		return renderReceiverType(t.X)
	default:
		return ""
	}
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
