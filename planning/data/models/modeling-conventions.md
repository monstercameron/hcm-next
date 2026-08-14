# Modeling Conventions and Shared Value Objects

## Universal entity envelope

Every aggregate, revision, relationship, control artifact and material evidence
record explicitly carries the applicable subset of:

```text
EntityEnvelope
  entity_id
  entity_kind
  tenant_id
  owning_organization_scope_ref
  cell_id
  placement_epoch
  lifecycle_state
  revision
  effective_interval
  recorded_at
  known_at
  source_authority_ref
  authority_epoch
  schema_ref
  classification_ref
  purpose_constraints[]
  retention_policy_ref
  legal_hold_keys[]
  provenance_ref
  correction_of_ref?
  supersedes_ref?
  created_by_principal_ref
  created_by_transaction_ref
```

Not every value object repeats this envelope. The owning aggregate/revision binds
its values. Cross-tenant identity is never inferred from an ID alone.

## Temporal primitives

```text
EffectiveInterval
  start_inclusive
  end_exclusive?
  semantic_kind = INSTANT | LOCAL_DATE | PAY_PERIOD | REPORTING_PERIOD
  timezone_id?
  tzdb_version?
  disambiguation = REJECT_GAP | EARLIER | LATER | EXPLICIT_OFFSET

TemporalPoint
  effective_at
  known_at
  recorded_at
  observed_at?
  received_at?
  policy_as_of

Deadline
  trigger_ref
  clock_start_basis
  due_expression
  authoritative_timezone
  tzdb_version
  business_calendar_ref/version
  cutoff_time?
  extension/tolling/grace refs[]
  resolved_due_at
  satisfaction_mode
```

Half-open intervals are canonical. Runtime timestamps never substitute for
business-effective or legally applicable time.

## Identity and references

```text
EntityRef
  tenant_id
  entity_kind
  entity_id
  revision_or_as_of?

ExternalObjectRef
  tenant_id
  connection_id
  external_account_id
  connector_version
  object_type
  external_id
  effective_interval

NaturalKeyClaim
  namespace
  normalized_value_hash
  issuer/source
  assurance
  effective_interval
```

External identifiers are scoped and may be reused. They never become canonical
IDs without an effective-dated linkage decision.

## Money, quantity and rate

```text
Money
  amount_decimal
  currency_code
  rounding_policy_ref

Rate
  amount_decimal
  currency_code?
  unit
  frequency
  basis
  denominator?

Quantity
  value_decimal
  unit_code

Percentage
  value_decimal
  scale
  rounding_policy_ref

ExchangeRateObservation
  source
  pair
  rate_decimal
  rate_type
  effective_at
  observed_at
  source_version
```

Material financial values never use binary floating point.

## Address, location and jurisdiction

```text
PostalAddress
  lines[]
  locality
  administrative_area
  postal_code
  country_code
  script
  normalized_address_ref?
  validation_status

GeoPoint
  latitude_decimal
  longitude_decimal
  accuracy_meters?
  source
  observed_at

LocationRef
  location_id
  address_revision_ref
  timezone_id
  jurisdiction_boundary_refs[]

JurisdictionAssertion
  role
  domain
  jurisdiction_ref
  activity_segment_ref?
  effective_interval
  source_authority_ref
  evidence_refs[]
  confidence
  status
```

Locale and jurisdiction remain distinct.

## Names and communication values

```text
LocalizedText
  language_tag
  script
  direction
  text
  translation_source_ref?
  approved_equivalence_ref?

StructuredName
  given_names[]
  middle_names[]
  family_names[]
  prefixes[]
  suffixes[]
  local_full_name
  latin_full_name?
  script
  ordering_rule

ContactEndpointValue
  channel
  normalized_address_or_provider_ref
  verification_state
  business_or_personal
  allowed_purposes[]
  classification_limit
```

## Classification, authority and provenance

```text
DataClassification
  class = PUBLIC | INTERNAL | CONFIDENTIAL | RESTRICTED | HIGHLY_RESTRICTED
  categories[]
  compartment_refs[]
  residency_constraints[]

AuthorityBinding
  resource/field scope
  effective_interval
  mastering_mode = LOCAL_MASTER | EXTERNAL_MASTER | SHARED_FIELD
  domain_fact_owner
  permitted_writer
  policy_fingerprint
  activation_epoch
  promotion_or_merge_rule

ProvenanceLink
  source_ref
  relation = ASSERTED_BY | DERIVED_FROM | OBSERVED_FROM | CORRECTS |
             SUPERSEDES | DECIDED_BY | PRODUCED_BY | SENT_TO
  field_paths[]
  transformation_ref?
  confidence?
```

An observation may be authoritative evidence of what a source reported without
being authoritative for the underlying business fact.

## State and correction rules

- Mutable business meaning is represented by immutable revisions/facts plus an
  aggregate lifecycle, not silent in-place history edits.
- Corrections append new assertions and preserve the original statement/event.
- Merge and separation never erase identity lineage.
- Deletion/disposition may remove payloads while retaining minimized evidence or
  tombstones under a separate retention rule.
- Projections, search, analytics, vectors and caches are reconstructable and do
  not acquire authority through replication.
