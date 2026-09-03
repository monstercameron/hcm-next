package effectfulpkg

import "testing"

// TestEffectfulPackageProbe exists only so
// compiler_test.go can name a real Test function that
// traceability.ScanTestNames finds, while ScanImports finds this package's
// forbidden import in effectful.go. It is never run as part of the module's
// own test suite (testdata directories are excluded from `go test ./...`).
func TestEffectfulPackageProbe(t *testing.T) {}
