package connectivity

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
)

// Version is a connector definition version. Definition versions are ordered
// and immutable: publishing a change means publishing a new version, never
// editing a published one.
type Version struct {
	Major uint32
	Minor uint32
	Patch uint32
}

// ParseVersion parses "major.minor.patch".
func ParseVersion(s string) (Version, error) {
	const op = "connectivity.ParseVersion"
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return Version{}, newError(op, ErrInvalid, "version %q is not major.minor.patch", s)
	}
	var out [3]uint32
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 10, 32)
		if err != nil {
			return Version{}, newError(op, ErrInvalid, "version %q has a non-numeric component", s)
		}
		out[i] = uint32(n)
	}
	return Version{Major: out[0], Minor: out[1], Patch: out[2]}, nil
}

func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// IsZero reports whether v is the unset version. 0.0.0 is never publishable.
func (v Version) IsZero() bool { return v == Version{} }

// Compare orders two versions.
func (v Version) Compare(o Version) int {
	switch {
	case v.Major != o.Major:
		return cmpUint(v.Major, o.Major)
	case v.Minor != o.Minor:
		return cmpUint(v.Minor, o.Minor)
	default:
		return cmpUint(v.Patch, o.Patch)
	}
}

func cmpUint(a, b uint32) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// Operation is a semantic operation class a connector may declare. P1A
// publishes only OperationRead; the others exist so that a P1B definition can
// declare them without a schema change here.
type Operation string

// The operation classes.
const (
	// OperationRead retrieves external records. It changes nothing.
	OperationRead Operation = "READ"
	// OperationWrite applies a governed change. No P1A connector declares it.
	OperationWrite Operation = "WRITE"
	// OperationEvent subscribes to a provider change feed.
	OperationEvent Operation = "EVENT"
)

// Valid reports whether o is a declared operation class.
func (o Operation) Valid() bool {
	switch o {
	case OperationRead, OperationWrite, OperationEvent:
		return true
	default:
		return false
	}
}

// Capability is one object/operation pair a connector version supports. It is
// the unit a connection may narrow but never broaden.
type Capability struct {
	Object    ObjectKind
	Operation Operation
}

// String renders the capability as "OBJECT:OPERATION".
func (c Capability) String() string { return string(c.Object) + ":" + string(c.Operation) }

// Valid reports whether both halves are declared values.
func (c Capability) Valid() bool { return c.Object.Valid() && c.Operation.Valid() }

// ReadCapabilities is the P1A capability set over the given objects.
func ReadCapabilities(objects ...ObjectKind) []Capability {
	caps := make([]Capability, 0, len(objects))
	for _, o := range objects {
		caps = append(caps, Capability{Object: o, Operation: OperationRead})
	}
	return caps
}

// AuthMode names how a connection authenticates. The definition declares which
// modes it supports; the connection picks one.
type AuthMode string

// The authentication modes P1A recognizes.
const (
	AuthOAuth2ClientCredentials AuthMode = "OAUTH2_CLIENT_CREDENTIALS"
	AuthAPIKey                  AuthMode = "API_KEY"
	AuthMTLS                    AuthMode = "MTLS"
	AuthSignedAssertion         AuthMode = "SIGNED_ASSERTION"
)

// Valid reports whether m is a declared auth mode.
func (m AuthMode) Valid() bool {
	switch m {
	case AuthOAuth2ClientCredentials, AuthAPIKey, AuthMTLS, AuthSignedAssertion:
		return true
	default:
		return false
	}
}

// Maturity is the support level a definition version claims. It is part of the
// published surface because "we can read workers" and "we will support you
// reading workers in production" are different statements.
type Maturity string

// The maturity levels.
const (
	MaturityExperimental Maturity = "EXPERIMENTAL"
	MaturityPreview      Maturity = "PREVIEW"
	MaturityCertified    Maturity = "CERTIFIED"
)

// Valid reports whether m is a declared maturity level.
func (m Maturity) Valid() bool {
	switch m {
	case MaturityExperimental, MaturityPreview, MaturityCertified:
		return true
	default:
		return false
	}
}

// SchemaRef binds one object to the external schema contract a definition
// version reads it under. INTG-004 and INTG-005 attach snapshots and diffs to
// these refs; this package only carries them.
type SchemaRef struct {
	Object     ObjectKind
	SchemaID   string
	SchemaVer  string
	Descriptor string
}

// PaginationContract states how the connector paginates. Declaring it is what
// lets a caller reason about restart safety without reading provider docs.
type PaginationContract struct {
	// Style is the pagination mechanism, e.g. "KEYSET".
	Style string
	// SortKeyField names the provider field the sort key is taken from.
	SortKeyField string
	// TieBreakField names the field that breaks equal sort keys.
	TieBreakField string
	// StableUnderSnapshot states that a pinned snapshot yields a stable order.
	StableUnderSnapshot bool
	// TokenTTL is how long a continuation token stays valid. Zero means the
	// provider declares no expiry.
	TokenTTL time.Duration
}

// Validate reports whether the pagination contract is complete.
func (p PaginationContract) Validate() error {
	const op = "connectivity.PaginationContract.Validate"
	switch {
	case strings.TrimSpace(p.Style) == "":
		return newError(op, ErrInvalid, "pagination contract has no style")
	case strings.TrimSpace(p.SortKeyField) == "":
		return newError(op, ErrInvalid, "pagination contract has no sort key field")
	case strings.TrimSpace(p.TieBreakField) == "":
		return newError(op, ErrInvalid, "pagination contract has no tie-break field")
	case p.TokenTTL < 0:
		return newError(op, ErrInvalid, "pagination token TTL cannot be negative")
	}
	return nil
}

// RateContract states the provider's own limits. It is separate from [Bounds]:
// Bounds is what we will ask for, RateContract is what they will serve.
type RateContract struct {
	RequestsPerMinute int
	ConcurrentReads   int
	BurstRequests     int
}

// Validate reports whether the rate contract is complete.
func (r RateContract) Validate() error {
	const op = "connectivity.RateContract.Validate"
	switch {
	case r.RequestsPerMinute <= 0:
		return newError(op, ErrInvalid, "rate contract has no requests-per-minute")
	case r.ConcurrentReads <= 0:
		return newError(op, ErrInvalid, "rate contract has no concurrency limit")
	case r.BurstRequests < 0:
		return newError(op, ErrInvalid, "rate contract burst cannot be negative")
	}
	return nil
}

// IdempotencyContract states how a repeated request is recognized by the
// provider. A read connector still declares it, because a re-read must be
// provably the same question.
type IdempotencyContract struct {
	// KeyField names the header or parameter carrying the idempotency key, or
	// is empty when the provider has none.
	KeyField string
	// RetentionWindow is how long the provider remembers a key.
	RetentionWindow time.Duration
	// ReadsAreIdempotent states that an identical read repeated within the
	// snapshot returns identical records.
	ReadsAreIdempotent bool
}

// Validate reports whether the idempotency contract is publishable.
func (i IdempotencyContract) Validate() error {
	const op = "connectivity.IdempotencyContract.Validate"
	if !i.ReadsAreIdempotent {
		return newError(op, ErrInvalid,
			"a read connector must declare its reads idempotent within a snapshot")
	}
	if i.RetentionWindow < 0 {
		return newError(op, ErrInvalid, "idempotency retention window cannot be negative")
	}
	return nil
}

// ObservationContract states what an observation from this connector can
// honestly claim: whether the provider timestamps records, and how stale a
// read may be before it must be reported stale rather than fresh.
type ObservationContract struct {
	// WatermarkField names the provider field carrying the change watermark.
	WatermarkField string
	// FreshnessBudget is how old a watermark may be before an observation is
	// reported stale.
	FreshnessBudget time.Duration
	// SupportsCompleteness states that the provider can say whether a
	// traversal saw everything.
	SupportsCompleteness bool
}

// Validate reports whether the observation contract is complete.
func (o ObservationContract) Validate() error {
	const op = "connectivity.ObservationContract.Validate"
	switch {
	case strings.TrimSpace(o.WatermarkField) == "":
		return newError(op, ErrInvalid, "observation contract has no watermark field")
	case o.FreshnessBudget <= 0:
		return newError(op, ErrInvalid, "observation contract has no freshness budget")
	}
	return nil
}

// ReconciliationContract states what can be compared back against the source
// after the fact. INTG-010 consumes it; publication only requires it exist.
type ReconciliationContract struct {
	// KeyFields are the fields that identify a record across systems.
	KeyFields []string
	// SupportsPointRead states that a single record can be re-read by key,
	// which is what makes a targeted reconciliation possible.
	SupportsPointRead bool
	// ComparableFields are the fields whose values may be compared.
	ComparableFields []string
}

// Validate reports whether the reconciliation contract is complete.
func (r ReconciliationContract) Validate() error {
	const op = "connectivity.ReconciliationContract.Validate"
	switch {
	case len(r.KeyFields) == 0:
		return newError(op, ErrInvalid, "reconciliation contract has no key fields")
	case len(r.ComparableFields) == 0:
		return newError(op, ErrInvalid, "reconciliation contract has no comparable fields")
	}
	return nil
}

// HealthContract states how the connector is probed without mutation.
type HealthContract struct {
	// ProbeObject is the object a read-only health probe touches.
	ProbeObject ObjectKind
	// Interval is how often a healthy connection is re-probed.
	Interval time.Duration
	// DegradedAfterFailures is the consecutive failure count that moves a
	// connection to DEGRADED.
	DegradedAfterFailures int
}

// Validate reports whether the health contract is complete.
func (h HealthContract) Validate() error {
	const op = "connectivity.HealthContract.Validate"
	switch {
	case !h.ProbeObject.Valid():
		return newError(op, ErrInvalid, "health contract has no probe object")
	case h.Interval <= 0:
		return newError(op, ErrInvalid, "health contract has no probe interval")
	case h.DegradedAfterFailures <= 0:
		return newError(op, ErrInvalid, "health contract has no degradation threshold")
	}
	return nil
}

// ConnectorDefinition is the immutable, versioned statement of what one named
// connector version can do.
//
// Every contract on it is required at publication. That is deliberate: a
// definition missing its pagination or observation semantics reads as "we do
// not know how this connector behaves", and an unknown behaviour must not be
// publishable as a supported surface.
type ConnectorDefinition struct {
	// ConnectorID is the stable definition identity, e.g. "workday.hcm".
	ConnectorID string
	// Vendor and Product name the external system.
	Vendor  string
	Product string
	// Version is this immutable version.
	Version Version
	// Maturity is the support level claimed.
	Maturity Maturity
	// Objects are the record families this version knows.
	Objects []ObjectKind
	// Capabilities are the object/operation pairs served.
	Capabilities []Capability
	// AuthModes are the authentication modes a connection may pick.
	AuthModes []AuthMode
	// ReadModes are the traversal modes supported.
	ReadModes []ReadMode
	// WriteModes are the write surfaces declared. P1A definitions declare
	// none; the field exists so a P1B definition need not change this type.
	WriteModes []string
	// EventModes are the change-feed surfaces declared.
	EventModes []string
	// SchemaRefs binds each object to its external schema contract.
	SchemaRefs []SchemaRef
	// Bounds is the read envelope this version honours.
	Bounds Bounds
	// Pagination, Rate, Idempotency, Observation, Reconciliation and Health
	// are the behavioural contracts publication requires.
	Pagination     PaginationContract
	Rate           RateContract
	Idempotency    IdempotencyContract
	Observation    ObservationContract
	Reconciliation ReconciliationContract
	Health         HealthContract
}

const (
	definitionSchema  = "hcmnext.connectivity.ConnectorDefinition"
	definitionVersion = 1
)

// Validate reports every reason the definition cannot be published. It fails
// on the first, because a caller fixing a definition fixes one field at a time
// and a list of twenty complaints is not more useful than the first one.
func (d ConnectorDefinition) Validate() error {
	const op = "connectivity.ConnectorDefinition.Validate"
	switch {
	case strings.TrimSpace(d.ConnectorID) == "":
		return newError(op, ErrInvalid, "definition has no connector id")
	case strings.TrimSpace(d.Vendor) == "":
		return newError(op, ErrInvalid, "definition has no vendor")
	case strings.TrimSpace(d.Product) == "":
		return newError(op, ErrInvalid, "definition has no product")
	case d.Version.IsZero():
		return newError(op, ErrInvalid, "definition has no version")
	case !d.Maturity.Valid():
		return newError(op, ErrInvalid, "definition has no maturity level")
	case len(d.Objects) == 0:
		return newError(op, ErrInvalid, "definition declares no objects")
	case len(d.Capabilities) == 0:
		return newError(op, ErrInvalid, "definition declares no capabilities")
	case len(d.AuthModes) == 0:
		return newError(op, ErrInvalid, "definition declares no auth modes")
	case len(d.ReadModes) == 0:
		return newError(op, ErrInvalid, "definition declares no read modes")
	// A read-only connector declares an empty write surface, not an absent
	// one. Nil means the author never said, and "we do not know whether this
	// connector can write" is not publishable.
	case d.WriteModes == nil:
		return newError(op, ErrInvalid,
			"definition does not declare a write surface; a read-only connector declares an empty one")
	case d.EventModes == nil:
		return newError(op, ErrInvalid,
			"definition does not declare an event surface; a connector without one declares an empty list")
	case len(d.SchemaRefs) == 0:
		return newError(op, ErrInvalid, "definition declares no schema refs")
	}

	declared := make(map[ObjectKind]bool, len(d.Objects))
	for _, o := range d.Objects {
		if !o.Valid() {
			return newError(op, ErrInvalid, "definition declares unknown object %q", string(o))
		}
		if declared[o] {
			return newError(op, ErrInvalid, "definition declares object %q twice", string(o))
		}
		declared[o] = true
	}
	seenCap := make(map[Capability]bool, len(d.Capabilities))
	for _, c := range d.Capabilities {
		if !c.Valid() {
			return newError(op, ErrInvalid, "definition declares unknown capability %q", c.String())
		}
		if !declared[c.Object] {
			return newError(op, ErrInvalid,
				"capability %q names object %q the definition does not declare", c.String(), string(c.Object))
		}
		if seenCap[c] {
			return newError(op, ErrInvalid, "definition declares capability %q twice", c.String())
		}
		seenCap[c] = true
		if c.Operation == OperationWrite && len(d.WriteModes) == 0 {
			return newError(op, ErrInvalid,
				"capability %q declares a write but the definition publishes no write surface", c.String())
		}
		if c.Operation == OperationEvent && len(d.EventModes) == 0 {
			return newError(op, ErrInvalid,
				"capability %q declares events but the definition publishes no event surface", c.String())
		}
	}
	for _, m := range d.AuthModes {
		if !m.Valid() {
			return newError(op, ErrInvalid, "definition declares unknown auth mode %q", string(m))
		}
	}
	for _, m := range d.ReadModes {
		if !m.Valid() {
			return newError(op, ErrInvalid, "definition declares unknown read mode %q", string(m))
		}
	}
	for _, s := range d.SchemaRefs {
		switch {
		case !declared[s.Object]:
			return newError(op, ErrInvalid,
				"schema ref names object %q the definition does not declare", string(s.Object))
		case strings.TrimSpace(s.SchemaID) == "":
			return newError(op, ErrInvalid, "schema ref for %q has no schema id", string(s.Object))
		case strings.TrimSpace(s.SchemaVer) == "":
			return newError(op, ErrInvalid, "schema ref for %q has no schema version", string(s.Object))
		}
	}
	for _, o := range d.Objects {
		if !d.hasSchemaRef(o) {
			return newError(op, ErrInvalid, "object %q has no schema ref", string(o))
		}
	}
	if err := d.Bounds.Validate(); err != nil {
		return err
	}
	for _, validate := range []func() error{
		d.Pagination.Validate, d.Rate.Validate, d.Idempotency.Validate,
		d.Observation.Validate, d.Reconciliation.Validate, d.Health.Validate,
	} {
		if err := validate(); err != nil {
			return err
		}
	}
	if !declared[d.Health.ProbeObject] {
		return newError(op, ErrInvalid,
			"health probe object %q is not declared", string(d.Health.ProbeObject))
	}
	return nil
}

func (d ConnectorDefinition) hasSchemaRef(o ObjectKind) bool {
	for _, s := range d.SchemaRefs {
		if s.Object == o {
			return true
		}
	}
	return false
}

// Supports reports whether the definition version publishes the capability.
func (d ConnectorDefinition) Supports(c Capability) bool {
	for _, have := range d.Capabilities {
		if have == c {
			return true
		}
	}
	return false
}

// SchemaRefFor returns the schema ref bound to object.
func (d ConnectorDefinition) SchemaRefFor(object ObjectKind) (SchemaRef, bool) {
	for _, s := range d.SchemaRefs {
		if s.Object == object {
			return s, true
		}
	}
	return SchemaRef{}, false
}

// Digest returns the content digest of the definition's published surface.
//
// Two definitions with the same meaning digest identically on every machine,
// which is what makes "this version already exists, unchanged" distinguishable
// from "someone edited a published version".
func (d ConnectorDefinition) Digest() (string, error) {
	raw, err := d.canonical()
	if err != nil {
		return "", err
	}
	return canonicalbytes.Digest(raw), nil
}

func (d ConnectorDefinition) canonical() ([]byte, error) {
	const op = "connectivity.ConnectorDefinition.Digest"
	w := canonicalbytes.New(definitionSchema, definitionVersion).
		String("connector_id", d.ConnectorID).
		String("vendor", d.Vendor).
		String("product", d.Product).
		String("version", d.Version.String()).
		String("maturity", string(d.Maturity))

	w.SortedStrings("objects", stringsOf(d.Objects))
	w.SortedStrings("capabilities", capabilityStrings(d.Capabilities))
	w.SortedStrings("auth_modes", stringsOf(d.AuthModes))
	w.SortedStrings("read_modes", stringsOf(d.ReadModes))
	w.SortedStrings("write_modes", append([]string(nil), d.WriteModes...))
	w.SortedStrings("event_modes", append([]string(nil), d.EventModes...))
	w.SortedStrings("schema_refs", schemaRefStrings(d.SchemaRefs))

	w.Int("bounds.max_page_size", int64(d.Bounds.MaxPageSize)).
		Int("bounds.max_pages_per_run", int64(d.Bounds.MaxPagesPerRun)).
		Int("bounds.max_records_per_run", int64(d.Bounds.MaxRecordsPerRun)).
		Int("bounds.max_record_bytes", int64(d.Bounds.MaxRecordBytes)).
		Int("bounds.min_request_interval_ns", int64(d.Bounds.MinRequestInterval))

	w.String("pagination.style", d.Pagination.Style).
		String("pagination.sort_key_field", d.Pagination.SortKeyField).
		String("pagination.tie_break_field", d.Pagination.TieBreakField).
		Bool("pagination.stable_under_snapshot", d.Pagination.StableUnderSnapshot).
		Int("pagination.token_ttl_ns", int64(d.Pagination.TokenTTL))

	w.Int("rate.requests_per_minute", int64(d.Rate.RequestsPerMinute)).
		Int("rate.concurrent_reads", int64(d.Rate.ConcurrentReads)).
		Int("rate.burst_requests", int64(d.Rate.BurstRequests))

	w.String("idempotency.key_field", d.Idempotency.KeyField).
		Int("idempotency.retention_ns", int64(d.Idempotency.RetentionWindow)).
		Bool("idempotency.reads_idempotent", d.Idempotency.ReadsAreIdempotent)

	w.String("observation.watermark_field", d.Observation.WatermarkField).
		Int("observation.freshness_budget_ns", int64(d.Observation.FreshnessBudget)).
		Bool("observation.supports_completeness", d.Observation.SupportsCompleteness)

	w.SortedStrings("reconciliation.key_fields", d.Reconciliation.KeyFields)
	w.SortedStrings("reconciliation.comparable_fields", d.Reconciliation.ComparableFields)
	w.Bool("reconciliation.supports_point_read", d.Reconciliation.SupportsPointRead)

	w.String("health.probe_object", string(d.Health.ProbeObject)).
		Int("health.interval_ns", int64(d.Health.Interval)).
		Int("health.degraded_after_failures", int64(d.Health.DegradedAfterFailures))

	raw, err := w.Bytes()
	if err != nil {
		return nil, newError(op, ErrInvalid, "encode definition: %v", err)
	}
	return raw, nil
}

// stringsOf converts any string-kinded slice to []string.
func stringsOf[T ~string](in []T) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = string(v)
	}
	return out
}

func capabilityStrings(caps []Capability) []string {
	out := make([]string, len(caps))
	for i, c := range caps {
		out[i] = c.String()
	}
	return out
}

func schemaRefStrings(refs []SchemaRef) []string {
	out := make([]string, len(refs))
	for i, r := range refs {
		out[i] = string(r.Object) + "|" + r.SchemaID + "|" + r.SchemaVer + "|" + r.Descriptor
	}
	return out
}

// PublicationMeta is the governance context of a publication. Publication time
// is supplied rather than read from the wall clock so that publication is a
// pure function of its inputs and a golden vector stays golden.
type PublicationMeta struct {
	PublishedBy string
	PublishedAt time.Time
}

// Publication is one immutable published definition version.
type Publication struct {
	Definition  ConnectorDefinition
	Digest      string
	PublishedBy string
	PublishedAt time.Time
}

// Registry is the immutable versioned connector definition registry.
//
// It answers exactly one question authoritatively: given a connector id and a
// version, what surface is supported. A published version is never mutated;
// re-publishing byte-identical content is idempotent, and re-publishing
// different content under a published version is [ErrImmutable].
//
// A Registry is safe for concurrent use.
type Registry struct {
	mu       sync.RWMutex
	byKey    map[definitionKey]Publication
	versions map[string][]Version
}

type definitionKey struct {
	connectorID string
	version     Version
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		byKey:    make(map[definitionKey]Publication),
		versions: make(map[string][]Version),
	}
}

// Publish validates and records a definition version.
//
// Publishing the same version with byte-identical content twice succeeds and
// returns the existing publication: a redeploy that republishes its catalogue
// must not fail, but it also must not be able to change history.
func (r *Registry) Publish(def ConnectorDefinition, meta PublicationMeta) (Publication, error) {
	const op = "connectivity.Registry.Publish"
	if err := def.Validate(); err != nil {
		return Publication{}, err
	}
	switch {
	case strings.TrimSpace(meta.PublishedBy) == "":
		return Publication{}, newError(op, ErrInvalid, "publication has no publisher")
	case meta.PublishedAt.IsZero():
		return Publication{}, newError(op, ErrInvalid, "publication has no publication time")
	}
	digest, err := def.Digest()
	if err != nil {
		return Publication{}, err
	}

	key := definitionKey{connectorID: def.ConnectorID, version: def.Version}
	pub := Publication{
		Definition:  def,
		Digest:      digest,
		PublishedBy: meta.PublishedBy,
		PublishedAt: meta.PublishedAt.UTC(),
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.byKey[key]; ok {
		if existing.Digest != digest {
			return Publication{}, newError(op, ErrImmutable,
				"connector %s version %s is published with digest %s; refusing to replace it with %s",
				def.ConnectorID, def.Version, existing.Digest, digest)
		}
		return existing, nil
	}
	r.byKey[key] = pub
	r.versions[def.ConnectorID] = insertVersion(r.versions[def.ConnectorID], def.Version)
	return pub, nil
}

// Resolve returns the exact published version.
func (r *Registry) Resolve(connectorID string, version Version) (Publication, error) {
	const op = "connectivity.Registry.Resolve"
	r.mu.RLock()
	defer r.mu.RUnlock()
	pub, ok := r.byKey[definitionKey{connectorID: connectorID, version: version}]
	if !ok {
		return Publication{}, newError(op, ErrNotFound,
			"connector %s has no published version %s", connectorID, version)
	}
	return pub, nil
}

// Latest returns the highest published version of a connector.
func (r *Registry) Latest(connectorID string) (Publication, error) {
	const op = "connectivity.Registry.Latest"
	r.mu.RLock()
	defer r.mu.RUnlock()
	versions := r.versions[connectorID]
	if len(versions) == 0 {
		return Publication{}, newError(op, ErrNotFound, "connector %s has no published version", connectorID)
	}
	return r.byKey[definitionKey{connectorID: connectorID, version: versions[len(versions)-1]}], nil
}

// Versions returns every published version of a connector, ascending.
func (r *Registry) Versions(connectorID string) []Version {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]Version(nil), r.versions[connectorID]...)
}

// ConnectorIDs returns every published connector id, ascending.
func (r *Registry) ConnectorIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.versions))
	for id := range r.versions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func insertVersion(versions []Version, v Version) []Version {
	idx := sort.Search(len(versions), func(i int) bool { return versions[i].Compare(v) >= 0 })
	versions = append(versions, Version{})
	copy(versions[idx+1:], versions[idx:])
	versions[idx] = v
	return versions
}
