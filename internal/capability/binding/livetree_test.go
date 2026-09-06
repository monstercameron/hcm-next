package binding

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file is the only place in this package that touches the filesystem.
// BIND-001 asks for a handler symbol "verified to exist by AST scan of
// internal/transport"; the scan itself is pure ([ScanHandlerSymbols]) so the
// package stays kernel-pure, and the reading lives here, in a test.

// handlerScanRoots are the module-relative roots walked to prove a claimed
// handler symbol exists.
//
// internal/transport is the root BIND-001 names: it holds the typed RPC
// methods bound to the generated service descriptors. internal/intent/app is
// the second root, and omitting it would make the scan lie: that package's
// domainHandlers is where internal/capability's BOOTSTRAP table is
// republished with real handlers bound to it (see
// internal/intent/app/capabilities.go newCapabilityRegistry), so it — not
// internal/transport — is where a capability's own typed implementation
// currently lives.
var handlerScanRoots = []string{
	"internal/transport",
	"internal/intent/app",
}

// repoRoot walks up from the test's working directory to the directory
// holding go.mod, so the scan finds the live tree regardless of where the
// test binary was started.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found walking up from the test working directory")
		}
		dir = parent
	}
}

// readSources walks roots under the repository root and returns every
// non-test .go file keyed by its module-relative, slash-separated path,
// which is exactly the key [ScanHandlerSymbols] derives a package path from.
func readSources(t *testing.T, roots []string) map[string]string {
	t.Helper()
	root := repoRoot(t)
	out := map[string]string{}
	for _, rel := range roots {
		base := filepath.Join(root, filepath.FromSlash(rel))
		walkErr := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			relPath, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			out[filepath.ToSlash(relPath)] = string(data)
			return nil
		})
		if walkErr != nil {
			t.Fatalf("walking %s: %v", rel, walkErr)
		}
	}
	if len(out) == 0 {
		t.Fatalf("scanned %v and found no Go source; the scan would vacuously accept every claim", roots)
	}
	return out
}

// liveHandlerIndex is the handler index built from the live tree.
func liveHandlerIndex(t *testing.T) HandlerIndex {
	t.Helper()
	index, err := ScanHandlerSymbols(readSources(t, handlerScanRoots))
	if err != nil {
		t.Fatalf("ScanHandlerSymbols over the live tree: %v", err)
	}
	return index
}

// liveTable is the binding table over the real registry, the real generated
// descriptors, the reviewed claims and the real model binding, with handler
// symbols verified against the live tree.
func liveTable(t *testing.T) Table {
	t.Helper()
	table, err := Build(liveHandlerIndex(t))
	if err != nil {
		t.Fatalf("Build over the live tree: %v", err)
	}
	if !table.SymbolsChecked {
		t.Fatalf("Build reported SymbolsChecked=false with a non-nil handler index")
	}
	return table
}

// TestLiveTreeScanFindsTheHandlersItMustFind guards the scan itself: if the
// walk or the parse quietly produced an empty or partial index, every
// "symbol exists" check in this package would pass vacuously.
func TestLiveTreeScanFindsTheHandlersItMustFind(t *testing.T) {
	index := liveHandlerIndex(t)

	mustFind := []HandlerSymbol{
		appHandler("explainWorkerState"),
		appHandler("promoteWorker"),
		appHandler("simulateCompensation"),
		appHandler("evaluatePayBandPosition"),
		appHandler("detectDrift"),
		appHandler("createRepairPlan"),
		appHandler("simulateRepair"),
		appHandler("explainTransaction"),
		appRegistryHandler("ListCapabilities"),
		appRegistryHandler("GetCapability"),
		{PackagePath: "internal/transport/admin", Receiver: "*server", Name: "GetWorkerState"},
		{PackagePath: "internal/transport/admin", Receiver: "*server", Name: "ExplainTransaction"},
		{PackagePath: "internal/transport/journey", Receiver: "*server", Name: "ProposePromotion"},
		{PackagePath: "internal/transport/grpcserver", Receiver: "*intentService", Name: "SimulateIntent"},
	}
	for _, sym := range mustFind {
		if !index.Has(sym) {
			t.Errorf("the live-tree scan did not find %s, which exists in the tree; the scan is not proving anything", sym.Ref())
		}
	}

	// A symbol that does not exist must not be found, or "verified" means
	// nothing.
	absent := HandlerSymbol{PackagePath: "internal/transport/admin", Receiver: "*server", Name: "NoSuchMethodExists"}
	if index.Has(absent) {
		t.Errorf("the scan reported %s exists; it does not", absent.Ref())
	}
}
