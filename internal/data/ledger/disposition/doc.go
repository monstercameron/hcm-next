// Package disposition applies retention, legal holds and crypto-erasure to
// one ledger event's inline payload without ever letting a caller mistake a
// destroyed payload for an ordinary one (LEDGER-011).
//
// # The bug this package exists to close
//
// internal/data/ledger.EventRecord.Payload is nil both for an event that
// references its content by ArtifactRef (never carried inline bytes) and,
// were a payload ever zeroed in place, for one whose payload had been
// destroyed. A caller reading the raw field cannot tell those two apart, and
// a nil slice reads as "nothing to see here" rather than "this was
// destroyed under a retention order" -- exactly backwards for a compliance
// action that must be impossible to read past.
//
// [View] is the fix: every accessor in this package returns one, and its
// State is always one of five declared values ([StatePresent],
// [StateReferenced], [StateHeld], [StateRestricted],
// [StatePayloadErased]). The zero value "" belongs to none of them, so a
// caller that forgets to switch on State gets a value that cannot be
// mistaken for "present and fine".
//
// # Why ledger_event itself is never written
//
// ledger_event (migration 00005) is append-only by construction: a BEFORE
// UPDATE OR DELETE trigger forbids mutation, UPDATE/DELETE are revoked from
// PUBLIC, and the application role is granted only SELECT/INSERT on it and
// its hash partitions. [Erase] does not attempt to weaken any of that. It
// records disposition as its own append-only fact in a new table,
// ledger_payload_disposition (migration 00285), naming the exact event it
// disposes and carrying that event's own digest and digest algorithm as
// evidence that the chronology-bearing columns were read, never altered.
// [ReadView] is what actually withholds the payload: once a disposition row
// names an event, it never returns EventRecord.Payload for that event again,
// regardless of what ledger_event still physically stores.
//
// # An honest boundary
//
// That last sentence is a real limitation and this package does not hide
// it: the literal bytes in ledger_event.payload are not zeroed, overwritten
// or physically unrecoverable by anything in this package, for either
// disposition mechanism. For [ClassificationStandard] (mechanism
// [MechanismOverwrite], state [StatePayloadErased]) that is a direct
// consequence of ledger_event's own append-only guarantee -- a real
// byte-level overwrite is not achievable without abandoning that guarantee,
// which is out of this todo's roots. For [ClassificationEncryptedAtRest]
// (mechanism [MechanismCryptoErasure], state [StateRestricted]) it also
// reflects that this codebase's ledger append path
// (internal/data/ledger.Appender.Append) never encrypts an inline payload
// before writing it: there is no real encryption key protecting the bytes
// already on disk, so there is no real key for Erase to destroy. Erase
// records a symbolic key reference and its destruction time as governance
// evidence of the classification decision, but that record proves a policy
// choice was made, not that the underlying bytes became cryptographically
// unrecoverable. A production system that needs the latter guarantee would
// need the ledger append path itself to encrypt ENCRYPTED_AT_REST-classified
// payloads before commit, so that destroying the key has real teeth; that
// change is outside this package's roots and is not made here.
//
// What this package does guarantee, and proves against a real PostgreSQL
// instance and the real internal/data/ledger/hashchain verifier, is: (1) a
// caller can always tell an erased payload apart from one that never had
// inline bytes; (2) disposition never perturbs an event's sequence,
// correlation, three times, digest or digest algorithm, and the hash chain
// over the stream verifies identically before and after; and (3) an event
// under an active legal hold cannot be erased, and the hold's own evidence
// is untouched by the refused attempt.
package disposition
