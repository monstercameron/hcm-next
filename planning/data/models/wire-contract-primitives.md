# Wire Contract Primitives

These are the canonical conceptual wire types for later Protobuf, generated Go,
and SchemaFlux schemas. Prose shorthands such as `resource/field scope` must
compile to these explicit types; they are not literal union fields.

```text
EntityId
  entity_type_id, tenant partition, opaque ULID-or-UUID bytes
  canonical text encoding/version

RevisionToken
  aggregate/entity ref, monotonic revision or opaque CAS bytes
  source authority/epoch, issued-at

EntityRef
  entity_type_id, entity_id
  oneof selector = EXACT_REVISION | EFFECTIVE_AS_OF | KNOWN_AT | CURRENT
  tenant/org scope, authority expectation

ResourceKey
  resource_type, tenant/org/partition, canonical key bytes
  ordering class/version

FieldPath
  entity_type_id, stable numeric field-number path[]
  optional repeated/map element selectors
  schema release/version

CanonicalDigest
  algorithm = SHA_256 initially, canonicalization profile/version
  digest bytes, content/schema type

PresenceValue
  state = PRESENT | ABSENT | NULL | UNKNOWN | REDACTED |
          NOT_APPLICABLE | STALE | UNAVAILABLE
  oneof typed value | reason/authority/evidence ref

Scope
  tenant/org/legal-entity/population/resource/field selectors
  purpose/channel/jurisdiction/effective interval

AuthoritySourceRef
  authority source ID, epoch, source role
  expected binding/version/fence

StreamHeadRef
  stream ID, sequence, revision token, last event hash
  fencing epoch/observed-at

FixedDecimal
  signed unscaled two's-complement integer bytes
  int32 scale, declared max precision
  no NaN/infinity/negative zero
  overflow is an error; rounding requires explicit RoundingMode

RoundingMode
  HALF_EVEN | HALF_UP | HALF_AWAY_FROM_ZERO | FLOOR | CEILING |
  TOWARD_ZERO | AWAY_FROM_ZERO | EXACT_REQUIRED

Money
  FixedDecimal amount, ISO-4217 currency
  declared currency minor-unit profile/version

Percentage
  FixedDecimal fraction where 1.0 = 100 percent
  allowed range/scale/rounding profile

Quantity
  FixedDecimal value, unit code/version
  dimension/conversion profile

Rate
  numerator Money-or-Quantity, denominator Quantity-or-TimeUnit
  basis/calendar/rounding profile

TemporalInstant
  UTC seconds + nanos
  trusted-time proof ref?, uncertainty duration
  source clock/timezone/tzdb version where supplied

TemporalInterval
  start inclusive, end exclusive or unbounded
  basis = EFFECTIVE | RECORDED | KNOWN | OBSERVED | RECEIVED

BitemporalCoordinate
  effective interval, known-at, recorded-at
  observed/received times optional and separately tagged
  source-clock/trusted-time proof refs

LifecycleStatus
  lifecycle profile/version/state ID, transition sequence

TypedArtifactRef
  artifact ID/revision, schema/media type
  digest/classification/encryption/residency refs
```

## Canonical serialization

```text
CanonicalizationProfile
  profile_id, version
  Protobuf deterministic field-order/unknown-field rules
  presence/default/oneof/map/repeated ordering rules
  Unicode normalization and string encoding rules
  FixedDecimal normalization rules
  timestamp precision and timezone exclusion rules
  artifact/reference encoding rules
  digest algorithm/effective interval/status
```

Every digest-bearing entity names one canonicalization profile. JSON text,
database row order, locale-formatted numbers, wall-clock timezone strings, or
Go map iteration order are never digest inputs directly.

## Temporal invariants

```text
recorded_at is assigned by trusted platform receipt/commit time.
received_at may precede or follow source occurred_at and preserves uncertainty.
known_at is the earliest time the asserted fact was available to the stated
authority; it cannot be after recorded_at without an explicit future-knowledge
claim and validation.
effective intervals are start-inclusive/end-exclusive.
Overlapping authoritative revisions for an exclusive property are forbidden
unless the property definition declares multi-valued composition.
Corrections append a new assertion and never move recorded_at backward.
Source clock values never establish legal ordering without clock provenance.
```

## Go generation constraints

```text
No map[string]any or untyped JSON at domain boundaries.
Every optional scalar uses explicit Protobuf presence.
Every variant uses oneof or a tagged message, never slash-delimited prose.
Enums reserve zero for UNSPECIFIED and preserve unknown numeric values.
Removed field numbers and enum values remain reserved.
Money/quantity arithmetic uses generated FixedDecimal helpers, never float64.
SchemaFlux validation owns cross-field constraints and canonicalization profiles.
grpcbridge exposes the same Protobuf presence and error semantics over HTTP.
```
