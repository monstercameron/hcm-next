// Package testhygiene detects test-suite reliability hazards and enforces
// explicit quarantine records for skipped tests.
//
// The checker is intentionally kernel-pure: it uses go/parser for static
// inspection and the Go tool for repeated package execution. It never starts
// a database, container, network service, or third-party test framework.
package testhygiene
