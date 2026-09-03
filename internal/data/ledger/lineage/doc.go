// Package lineage implements LEDGER-005: append-only correction and
// supersession lineage over internal/data/ledger.
//
// # One mechanism, two names
//
// The five assertion classes (internal/data/ledger/append.go) already define
// CORRECTION as "a later governed assertion corrects, completes, or
// supersedes an earlier assertion" - correction and supersession are the
// same mechanism under two business names, not two schemas. Replacing a
// proposal or intent revision with its successor is recorded exactly the
// way correcting a mistaken fact is: a new CORRECTION event whose Corrects
// field names the (stream, sequence) it replaces. This package's lineage
// queries therefore work uniformly over both: an "ancestor" is whatever a
// node corrects (or supersedes) and a "descendant" is whatever corrects (or
// supersedes) it, regardless of which business story motivated the append.
//
// migrations/00005_ledger.sql (LEDGER-001/LEDGER-004, frozen for this work)
// already carries what lineage needs: ledger_event.corrects_stream_key and
// corrects_sequence, a CHECK constraint requiring exactly the CORRECTION
// class to carry them, and the append-only trigger and REVOKEd UPDATE/DELETE
// privileges that make ledger_event immutable once written. This package
// adds no column and no table: it is a read path (ancestor, descendant and
// effective-current queries) over data internal/data/ledger.Append already
// writes, plus a pre-append guard ([ValidateCorrectionTarget]) that refuses
// a correction request before it is ever submitted to Append, rather than
// discovering a dangling or cross-tenant reference only when a later query
// walks it.
//
// The reason a correction or supersession carries for replacing its target
// is not a ledger-level field: it lives in the CORRECTION event's own typed
// payload, exactly like every other business fact a ledger event carries
// (internal/data/ledger.AppendRequest.Payload, keyed by SchemaRef). The
// ledger records that a correction happened, who authored it and what it
// targets; what the domain-specific reason means is the payload schema's
// business, not the ledger's.
//
// # Why lineage cannot be forged by mutation
//
// This package exposes no update or delete of any kind - there is no method
// on any type here that could rewrite or remove a ledger_event row, and the
// only way to append is [Append], which forwards to
// internal/data/ledger.Append after validating a correction's target. That
// structural absence is deliberate: LEDGER-005 requires proving that an
// attempt to mutate a recorded assertion is refused at the adapter API, not
// merely that this package happens not to call UPDATE or DELETE today. The
// package's tests additionally prove the underlying refusal directly against
// PostgreSQL - an UPDATE or DELETE issued straight at ledger_event through
// the raw connection is rejected by the append-only trigger, and the row is
// byte-for-byte unchanged afterward.
package lineage
