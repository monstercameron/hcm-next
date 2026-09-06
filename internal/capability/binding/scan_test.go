package binding

import (
	"strings"
	"testing"
)

func TestScanHandlerSymbolsFindsFunctionsAndMethods(t *testing.T) {
	index, err := ScanHandlerSymbols(map[string]string{
		"internal/transport/admin/server.go": `package admin

func NewServer() *server { return nil }

func (s *server) GetWorkerState(ctx context.Context, req *R) (*S, error) { return nil, nil }

func (s server) valueReceiver() {}
`,
		"internal/intent/app/capabilities.go": `package app

func (h *domainHandlers) explainWorkerState(ctx context.Context, payload any) (any, error) { return nil, nil }
`,
	})
	if err != nil {
		t.Fatalf("ScanHandlerSymbols: %v", err)
	}

	for _, want := range []HandlerSymbol{
		{PackagePath: "internal/transport/admin", Name: "NewServer"},
		{PackagePath: "internal/transport/admin", Receiver: "*server", Name: "GetWorkerState"},
		{PackagePath: "internal/transport/admin", Receiver: "server", Name: "valueReceiver"},
		{PackagePath: "internal/intent/app", Receiver: "*domainHandlers", Name: "explainWorkerState"},
	} {
		if !index.Has(want) {
			t.Errorf("scan did not find %s; found %v", want.Ref(), index.Refs())
		}
	}
	if len(index) != 4 {
		t.Errorf("scan produced %d symbols, want 4: %v", len(index), index.Refs())
	}
}

// TestScanHandlerSymbolsDistinguishesPointerAndValueReceivers matters
// because a claim spells the receiver exactly as the source does; treating
// "*server" and "server" as the same symbol would let a claim "verify"
// against a method that is not the one it names.
func TestScanHandlerSymbolsDistinguishesPointerAndValueReceivers(t *testing.T) {
	index, err := ScanHandlerSymbols(map[string]string{
		"internal/p/a.go": "package p\n\nfunc (s server) Do() {}\n",
	})
	if err != nil {
		t.Fatalf("ScanHandlerSymbols: %v", err)
	}
	if index.Has(HandlerSymbol{PackagePath: "internal/p", Receiver: "*server", Name: "Do"}) {
		t.Error("a value-receiver method matched a pointer-receiver claim")
	}
	if !index.Has(HandlerSymbol{PackagePath: "internal/p", Receiver: "server", Name: "Do"}) {
		t.Error("the value-receiver method was not found")
	}
}

// TestScanHandlerSymbolsHandlesGenericReceivers: a generic receiver's type
// arguments are declaration-site names, not part of the symbol's identity.
func TestScanHandlerSymbolsHandlesGenericReceivers(t *testing.T) {
	index, err := ScanHandlerSymbols(map[string]string{
		"internal/p/a.go": "package p\n\nfunc (c *Cache[K, V]) Get(k K) V { var v V; return v }\n\nfunc (b Box[T]) Peek() {}\n",
	})
	if err != nil {
		t.Fatalf("ScanHandlerSymbols: %v", err)
	}
	if !index.Has(HandlerSymbol{PackagePath: "internal/p", Receiver: "*Cache", Name: "Get"}) {
		t.Errorf("generic pointer receiver not reduced to its base type: %v", index.Refs())
	}
	if !index.Has(HandlerSymbol{PackagePath: "internal/p", Receiver: "Box", Name: "Peek"}) {
		t.Errorf("generic value receiver not reduced to its base type: %v", index.Refs())
	}
}

// TestScanHandlerSymbolsSkipsTestsAndNonGoFiles: a handler that exists only
// in a test is not an implementation, and letting one satisfy a claim would
// be exactly the dangling binding BIND-001 forbids.
func TestScanHandlerSymbolsSkipsTestsAndNonGoFiles(t *testing.T) {
	index, err := ScanHandlerSymbols(map[string]string{
		"internal/p/a_test.go": "package p\n\nfunc (s *server) OnlyInATest() {}\n",
		"internal/p/README.md": "not go source at all",
		"internal/p/a.go":      "package p\n\nfunc Real() {}\n",
	})
	if err != nil {
		t.Fatalf("ScanHandlerSymbols: %v", err)
	}
	if index.Has(HandlerSymbol{PackagePath: "internal/p", Receiver: "*server", Name: "OnlyInATest"}) {
		t.Error("a test-only method was indexed as an implementation")
	}
	if len(index) != 1 || !index.Has(HandlerSymbol{PackagePath: "internal/p", Name: "Real"}) {
		t.Errorf("index = %v, want only internal/p.Real", index.Refs())
	}
}

// TestScanHandlerSymbolsFailsLoudlyOnUnparseableSource: an unreadable tree
// must not look like a clean one.
func TestScanHandlerSymbolsFailsLoudlyOnUnparseableSource(t *testing.T) {
	_, err := ScanHandlerSymbols(map[string]string{
		"internal/p/broken.go": "package p\n\nfunc ( {{{ not go",
	})
	if err == nil {
		t.Fatal("a file that does not parse produced no error; a broken tree would read as an empty one")
	}
	if !strings.Contains(err.Error(), "internal/p/broken.go") {
		t.Errorf("the parse error does not name the file: %v", err)
	}
}

// TestScanHandlerSymbolsNormalizesWindowsSeparators keeps the scan usable
// from a walk that produced backslash paths.
func TestScanHandlerSymbolsNormalizesWindowsSeparators(t *testing.T) {
	index, err := ScanHandlerSymbols(map[string]string{
		`internal\transport\admin\server.go`: "package admin\n\nfunc Ping() {}\n",
	})
	if err != nil {
		t.Fatalf("ScanHandlerSymbols: %v", err)
	}
	if !index.Has(HandlerSymbol{PackagePath: "internal/transport/admin", Name: "Ping"}) {
		t.Errorf("a backslash path did not normalize: %v", index.Refs())
	}
}

func TestScanHandlerSymbolsOnEmptyInput(t *testing.T) {
	index, err := ScanHandlerSymbols(nil)
	if err != nil {
		t.Fatalf("ScanHandlerSymbols(nil): %v", err)
	}
	if len(index) != 0 {
		t.Errorf("an empty input produced %d symbols", len(index))
	}
}
