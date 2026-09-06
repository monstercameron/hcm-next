// Package spiconform is the conformance kit for [spi.Adapter]
// implementations.
//
// [Run] drives any adapter through the whole SPI contract: idempotent and
// bounded reads, deterministic snapshot digests, bounded observation, and the
// write-capability refusal shapes [spi.DeclareWriteCapability] produces. It
// takes a minimal [TB] rather than testing.TB so that this package's own
// tests can exercise Run's failure paths with a fake recorder; testing.TB's
// unexported method would otherwise forbid that.
//
// [MemoryAdapter] is a small in-memory reference [spi.Adapter]. It exists so
// this package's own tests, tools/gen/connectorsdk's golden tests, and a
// generated adapter skeleton's own test all have one canonical, dependency-
// free fixture to drive rather than each inventing a fake.
//
// This package imports "testing" and is never imported by production
// adapter code; only test files import it.
package spiconform
