// Command buildfixture is a TOOL-016 fixture: a minimal, dependency-free
// main package (stdlib only, no module context required to build it) used
// by tools/quality/buildverify's tests to prove the deterministic-build
// checker actually distinguishes a reproducible build from a
// nondeterministic one.
//
// `go build` embeds each compilation's absolute source directory in the
// resulting binary's build-ID/debug information unless -trimpath is
// passed; that is the entire reason -trimpath exists. Building this exact
// file from two different temporary directories therefore produces two
// different digests without -trimpath (RED) and identical digests with it
// (GREEN), which is exactly the property TOOL-016 requires a deterministic
// build checker to detect.
package main

import "fmt"

func main() {
	fmt.Println("buildverify fixture")
}
