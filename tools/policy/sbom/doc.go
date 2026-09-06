// Package sbom: rationale and known deviations (TOOL-017).
//
// # Why not `go list -m -json all`
//
// TOOL-017's brief describes generating the BOM "from `go list -m -json
// all` and `go version -m` style data". Running `go list -m -json all`
// against this repository's actual go.mod fails outright:
//
//	$ go list -m -json all
//	go: malformed module path "agenthub": missing dot in first path element
//
// This is the same failure mode tools/policy/depadmission/license.go and
// tools/policy/depmanifest/gomod.go already document and route around:
// `go list -m ... all` walks the *entire* transitive module graph,
// including the go.mod files of modules this repository never imports a
// package from, and a single malformed go.mod anywhere in that wider graph
// fails the whole call. This repository's own dependency tree currently
// demonstrates exactly that failure.
//
// This package therefore reads the same "go list -m -json"-shaped data
// (module path, resolved version, indirect flag) directly out of go.mod's
// require block via golang.org/x/mod/modfile (ParseRequires) — already a
// pinned dependency of this module, so this introduces no new one. Go's
// module graph pruning (1.17+) keeps that require block to exactly the
// modules this module's own build depends on, already resolved to the
// single version minimal version selection picked; that is the same
// information `go list -m -json all` would report for each of them, without
// needing to load the wider graph at all.
//
// Component content hashes (the "go version -m style data" — the per-module
// hash a built binary's module list carries) come from go.sum (ParseGoSum),
// which is exactly where `go version -m`'s own hashes and go.sum's originate
// (both are the module's h1 content hash; go.sum simply already has it
// without needing a built binary to extract it from).
//
// # Dependency graph
//
// `go mod graph` (unlike `go list -m ... all`) succeeds unconditionally
// against this repository, because it only ever prints require edges
// already recorded in go.mod files already sitting in the local module
// cache — it never has to resolve or validate a transitively-reachable
// module's own package tree. Its output is pre-MVS-selection, though: the
// same module path can appear at several different versions across
// different edges, because it records every version any go.mod in the
// graph asked for, not only the one MVS finally selected. Generate keeps
// only the edges whose both endpoints match go.mod's own selected version
// for that path (see ModGraph's doc comment), so Document.Dependencies
// reflects the graph as actually built, not every version MVS considered
// and discarded along the way.
//
// # Output path
//
// This package's generator does not write into definitions/ itself (that
// directory is out of this lane's file roots). cmd/sbomgen takes an
// explicit -out flag; the orchestrator is expected to run it with
// `-out definitions/supply-chain/sbom.cdx.json` (see cmd/sbomgen's doc
// comment for the exact invocation) and move/commit the result from
// wherever -out points, or point -out at that path directly if this
// lane's writes are later permitted there.
package sbom
