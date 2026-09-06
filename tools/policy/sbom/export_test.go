package sbom

// BuildDocumentForTest exposes the exec-free document-assembly core to the
// external sbom_test package, so golden/shape tests can exercise it without
// depending on this repository's own live go.mod/go.sum/`go mod graph`
// output. This file compiles only for `go test` and adds nothing to the
// package's real public API.
var BuildDocumentForTest = buildDocument
