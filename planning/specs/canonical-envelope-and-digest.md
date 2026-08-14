# Canonical Envelope, Serialization, and Digest Contract

Approval binding, idempotency, hash chains, signed configuration, artifact
identity, and portable audit evidence require identical canonical bytes across
supported Go binaries. Ordinary Protobuf serialization is not assumed canonical.

## Canonical Digest Envelope

```text
CanonicalDigest
  profile_id
  profile_version
  schema_id
  schema_version
  algorithm_id
  scope_binding
  canonical_length
  canonical_bytes_ref?   // durable for high-value evidence
  digest
```

Profiles are separately versioned for:

```text
PROPOSAL
IDEMPOTENT_REQUEST
LEDGER_EVENT
CONFIG_BUNDLE
EVIDENCE_MANIFEST
ARTIFACT_REFERENCE
EXTERNAL_PAYLOAD_EVIDENCE
```

The Canonicalization Registry owns `CanonicalizationProfile`, golden vectors,
algorithm eligibility, migration, and retirement. Schema Registry owns semantic
field definitions. Cryptographic Custody owns algorithms/keys and signatures.

Capabilities are `canonicalize.encode|verify|explain`,
`canonicalization_profiles.read|validate|publish|deprecate`, and
`digest.verify|migrate`.

## Encoding Rules

The initial profile uses deterministic field-tag order over an explicitly
materialized canonical model, not language map iteration or JSON object order.

- Integers use minimal-width signed/unsigned canonical encoding declared by the
  field schema.
- Fixed decimals encode sign, unscaled integer, and declared scale; semantically
  equal values do not acquire different trailing-scale forms unless scale itself
  is material.
- Money encodes decimal plus uppercase ISO currency code.
- Instants encode UTC seconds/nanoseconds with normalized range. A local business
  time additionally encodes timezone ID, tzdb version, calendar version, and
  disambiguation for skipped/repeated local times.
- Strings use valid UTF-8 normalized to NFC unless a field explicitly declares
  verbatim bytes. Invalid UTF-8 is rejected; case and whitespace are never changed
  unless the schema declares a normalized semantic field.
- Ordered repeated fields retain order. Sets are sorted by their element's
  canonical bytes and reject duplicate canonical elements.
- Maps are converted to sorted key/value entries using canonical key bytes; map
  iteration order is immaterial.
- `ABSENT`, explicit `NULL`, and schema default are distinct when the schema says
  they are semantically distinct; otherwise the profile defines one normalized
  representation. Implementations cannot choose.
- Unknown Protobuf fields are rejected for material signed/hashed profiles unless
  the exact profile explicitly preserves their original canonical byte field.
- Enums encode stable numeric identity plus schema version, not display label.
- Binary content encodes length and bytes or a typed immutable artifact reference
  containing algorithm and content digest. Mutable URLs are never content identity.
- Nonmaterial display labels, UI layout, transient traces, and derived formatting
  are excluded only by an explicit profile field list.

Domain objects are first projected into a canonical profile message. Two different
schema versions cannot share a digest meaning merely because bytes happen to
match; schema and profile identity are bound into the envelope.

## Proposal Material Context

A Promotion proposal digest includes:

```text
intent and proposal revision
subjects and tenant/org/legal-entity scope
effective date/business-time context
typed planned domain writes and external effects
source baselines and expected stream sequences
position/budget reservations and expiry
source-authority fingerprint
AuthZ, legal, policy, entitlement and risk fingerprints
classification taxonomy/label versions, propagation watermarks and DLP decision
approved purpose, recipient/destination and residency decision
reference, band, FX, calendar and tzdb versions used
material attachment/artifact hashes
required approvals and separation constraints
side-effect and compensation/repair declarations
```

An approval records the full `CanonicalDigest`, not a naked hash. Any material
change produces a new proposal revision and invalidates approval according to
policy. Nonmaterial display rerendering may retain the digest only when the
profile proves those fields excluded.

Reclassification, DLP-policy revocation, destination/purpose change or incomplete
label propagation is material unless the profile explicitly proves otherwise. It
invalidates approval or blocks execution until re-evaluation.

## Shared Effective-Time Primitive

All domains use one canonical temporal boundary contract:

```text
EffectiveTimeRange
  kind: INSTANT | LOCAL_DATE
  start_inclusive
  end_exclusive?
  timezone?
  calendar_ref?
  disambiguation?
```

Intervals are half-open `[start,end)` and may be open-ended only by explicit
absence of `end_exclusive`. `LOCAL_DATE` requires the governing calendar and does
not silently convert to midnight UTC. An instant derived from local civil time
requires timezone/tzdb and overlap/gap disambiguation. Every schema that models
authority, employment, relationship, compensation, endpoint eligibility,
retention, conflict or reservation references this type rather than inventing
`valid_from/valid_to` endpoint behavior.

Canonical vectors cover adjacent ranges, zero/negative ranges, open ends,
precision truncation, DST gaps/folds and timezone/calendar revision effects.

## Lifecycle, Failure, Security, and Evidence

```text
DRAFT -> VALIDATED -> PUBLISHED -> ACTIVE -> DEPRECATED -> RETIRED
                         |
                      QUARANTINED
```

Profile author, cryptographic reviewer, publisher, and migration approver are
separate capabilities for high-risk profiles. Publication requires golden vectors
and cross-version conformance. Unknown profile/schema/algorithm, invalid Unicode,
ambiguous defaults, duplicate set members, unrepresentable decimal/time, or
unknown material fields fail closed.

Canonical bytes may contain sensitive data. Store them only when evidence value
requires it, encrypted and access-controlled by the source data classification;
otherwise retain protected source references sufficient to reconstruct them.
Digest values are not presumed non-sensitive because they may enable correlation
or guessing attacks.

Evidence records implementation/build version, input/source reference, profile
and schema, exact bytes or protected reconstruction reference, algorithm,
digest/signature, validation, migration, consumers, and mismatch incidents.

## Migration and Crypto Agility

A profile change creates a new version. Historical evidence continues verifying
with the historical profile. Where a new digest is needed, a `DigestMigration`
links old and new envelopes to the same authorized source object and records who,
why, when, implementation, comparison, and signature. It never overwrites the old
digest. Algorithm migration supports dual digests/signatures during a declared
transition.

## Phase 1 Acceptance

- Golden proposal, idempotent request, ledger event, config bundle, and evidence
  manifest vectors produce byte-identical output across every supported Go binary
  version and architecture.
- Map insertion order, locale, timezone environment, JSON rendering, and process
  restart do not change bytes.
- Absent/default/null, Unicode normalization, decimal scale, timestamp ambiguity,
  unknown fields, set order, and artifact-reference tests prove explicit behavior.
- A material proposal change invalidates approval; a declared nonmaterial display
  change does not.
- Historical hashes verify after schema and algorithm upgrades.

SchemaFlux may compile canonicalization plans and golden vectors from registered
schemas. The authoritative encoder/verifier is Go; grpcbridge and GWC never
recompute approval or ledger digests in browser code.
