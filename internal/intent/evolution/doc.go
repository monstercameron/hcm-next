// Package evolution governs how a live intent definition version may change
// without ever mutating a published version in place.
//
// Two disjoint paths are legal, and there is no third:
//
//   - [CompatibilityCheck] classifies a successor version against its
//     predecessor. The only COMPATIBLE difference is a newly added optional
//     input; a compatible successor binds a live instance directly, with no
//     further ceremony.
//   - Every other named difference — a required input added (including an
//     existing input that became required), a required input removed, an
//     input's declared kind changed, the effect class raised, or the
//     approval requirement removed — is INCOMPATIBLE. An incompatible
//     successor may reach a live instance only through a
//     [SupersessionRecord]: an immutable, digested governance artifact that
//     names a reason, an author, an approver distinct from the author, an
//     effective instant, and a [LiveInstancePolicy] deciding what happens to
//     instances already live under the superseded version.
//
// [DefinitionDigest] and [RefuseInPlaceEdit] prove, rather than merely
// assert, that the third path — editing a published version's content while
// keeping its reference — is refused: a candidate that claims the same
// (intent_type_id, version) as a published definition but carries different
// content is not a new version and is never accepted as one, regardless of
// what [CompatibilityCheck] would have said about its content had it been
// published as a genuinely new version.
//
// This package makes no governance decision by itself and touches no store:
// it computes reports and mints records that a caller with real authority —
// the intent registry, a change-management workflow — records and acts on.
// The live-instance compatibility question it answers for intent definitions
// is the same shape [internal/workflow/migrationpreview] answers for
// compiled workflow versions.
package evolution
