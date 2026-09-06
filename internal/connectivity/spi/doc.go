// Package spi defines the provider-neutral connector adapter SPI: the
// interface a connector implementation compiles against and the generated
// SDK in tools/gen/connectorsdk targets.
//
// Semantic owner: connectivity. Phase: P1A.
//
// # The read-only claim is structural here too
//
// This package sits beside the parent [connectivity] package's own
// [connectivity.Connector], which the parent's doc comment describes as
// carrying "no method that could mutate an external system, and no method
// [that] returns a handle that could." [Adapter] makes the identical claim
// for the pluggable, generated-SDK-facing surface: it has exactly four
// methods - Describe, Probe, ReadSnapshot, ObserveChanges - and none of them
// can request, apply, or acknowledge an external change.
//
// Write is not merely absent from the interface; it is a separate, adapter-
// independent request family ([WriteCapabilityDeclaration]) that only this
// package's own [DeclareWriteCapability] evaluates. No adapter implementation
// can grant itself write capability, because no adapter method exists that
// this package would ever consult to decide that question. In this release
// [DeclaredAmendments] is empty, so every declaration is refused; the type
// exists so a later phase can publish a signed amendment without changing
// this contract's shape.
//
// # What this package owns
//
//   - [AdapterManifest]: the typed, digestable self-declaration an adapter
//     returns from Describe. Two manifests with the same meaning digest
//     identically, which is what lets a caller detect a silently changed
//     adapter build.
//   - [Adapter]: the four-method read/observe SPI.
//   - [WriteCapabilityDeclaration], [WriteCapabilityDecision] and
//     [DeclareWriteCapability]: the typed refusal path for write capability.
//   - [Snapshot] and [PageDigest]: the deterministic digest over a
//     [connectivity.Page] that lets a conformance kit prove a read is
//     idempotent without comparing full record sets by hand.
//   - [ObserveRequest] and [ChangeSet]: the bounded change-observation
//     request/response pair ObserveChanges uses.
//
// The conformance kit that drives any [Adapter] implementation through this
// contract lives in the spiconform subpackage, not here, so that an adapter
// implementation never has to import "testing" to satisfy this package.
//
// # Dependency direction
//
// This package adapts the parent [connectivity] package's read-only
// vocabulary (ObjectKind, Bounds, Cursor, Page, Record, ReadRequest) for
// adapter authors; it does not import internal/connectivity/writeadapters or
// internal/transport/clients/providerwrite, and never will while this
// release's manifest forbids both.
package spi
