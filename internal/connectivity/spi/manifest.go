package spi

import (
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/hcm-next/internal/connectivity"
	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
)

// ObjectKind and Bounds are the adapter SPI's own names for the parent
// connectivity plane's closed object vocabulary and read envelope. They are
// aliases, not new types, so a generated client can pass a [connectivity.Page]
// or [connectivity.ObjectKind] value across the boundary without a
// conversion, and so this package can never quietly drift from the parent's
// definition of either.
type (
	ObjectKind = connectivity.ObjectKind
	Bounds     = connectivity.Bounds
)

// Operation is one capability an adapter may name in its manifest or in a
// write-capability declaration.
//
// Naming OpWrite here does not grant it: [AdapterManifest.Validate] rejects
// any manifest that declares a write capability as already active, and
// [DeclareWriteCapability] is the only path that ever evaluates a write
// request, independent of any adapter's own code.
type Operation string

// The operation kinds this SPI recognizes.
const (
	// OpRead retrieves a bounded page of external records. It changes nothing.
	OpRead Operation = "READ"
	// OpObserve retrieves a bounded set of external changes. It changes
	// nothing.
	OpObserve Operation = "OBSERVE"
	// OpWrite names a governed external mutation. No P1A manifest may
	// declare it active; it exists only as the operation a
	// [WriteCapabilityDeclaration] requests.
	OpWrite Operation = "WRITE"
)

// Valid reports whether o is one of the declared operation kinds.
func (o Operation) Valid() bool {
	switch o {
	case OpRead, OpObserve, OpWrite:
		return true
	default:
		return false
	}
}

// Capability is one object/operation/version triple an adapter serves.
type Capability struct {
	Object    ObjectKind
	Operation Operation
	Version   string
}

// Valid reports whether every field of c is populated with a declared value.
func (c Capability) Valid() bool {
	return c.Object.Valid() && c.Operation.Valid() && strings.TrimSpace(c.Version) != ""
}

// String renders the capability as "OBJECT:OPERATION@VERSION".
func (c Capability) String() string {
	return string(c.Object) + ":" + string(c.Operation) + "@" + c.Version
}

// AdapterManifest is the typed, digestable self-declaration an [Adapter]
// returns from Describe.
//
// It is deliberately not [connectivity.ConnectorDefinition]: a definition is
// the immutable, governed catalogue entry a tenant connection binds to
// (INTG-001); a manifest is what one running adapter build says about itself
// right now. A connection resolver compares the two; this package only
// carries the adapter's half.
type AdapterManifest struct {
	// AdapterID is the stable adapter implementation identity, e.g.
	// "workday.hcm.readonly".
	AdapterID string
	// Vendor and Product name the external system this adapter build serves.
	Vendor, Product string
	// Version is the adapter build's own version string, independent of the
	// governed [connectivity.ConnectorDefinition] version it may implement.
	Version string
	// Objects are the record families this adapter build knows.
	Objects []ObjectKind
	// Capabilities are the object/operation/version triples this build
	// serves. No entry may name [OpWrite]; see [AdapterManifest.Validate].
	Capabilities []Capability
	// Bounds is the read/observe envelope this adapter build honours.
	Bounds Bounds
	// GeneratedAt is when this manifest value was produced. It is not part of
	// the digest: two manifests with identical published surface digest
	// identically regardless of when Describe was called.
	GeneratedAt time.Time
}

// Validate reports every reason the manifest cannot be published, failing on
// the first so a caller fixes one problem at a time.
func (m AdapterManifest) Validate() error {
	const op = "spi.AdapterManifest.Validate"
	switch {
	case strings.TrimSpace(m.AdapterID) == "":
		return connectivity.Fail(op, connectivity.ErrInvalid, "manifest has no adapter id")
	case strings.TrimSpace(m.Vendor) == "":
		return connectivity.Fail(op, connectivity.ErrInvalid, "manifest has no vendor")
	case strings.TrimSpace(m.Product) == "":
		return connectivity.Fail(op, connectivity.ErrInvalid, "manifest has no product")
	case strings.TrimSpace(m.Version) == "":
		return connectivity.Fail(op, connectivity.ErrInvalid, "manifest has no version")
	case len(m.Objects) == 0:
		return connectivity.Fail(op, connectivity.ErrInvalid, "manifest declares no objects")
	case len(m.Capabilities) == 0:
		return connectivity.Fail(op, connectivity.ErrInvalid, "manifest declares no capabilities")
	case m.GeneratedAt.IsZero():
		return connectivity.Fail(op, connectivity.ErrInvalid, "manifest carries no generation time")
	}

	declared := make(map[ObjectKind]bool, len(m.Objects))
	for _, o := range m.Objects {
		if !o.Valid() {
			return connectivity.Fail(op, connectivity.ErrInvalid, "manifest declares unknown object %q", string(o))
		}
		if declared[o] {
			return connectivity.Fail(op, connectivity.ErrInvalid, "manifest declares object %q twice", string(o))
		}
		declared[o] = true
	}

	seen := make(map[Capability]bool, len(m.Capabilities))
	for _, c := range m.Capabilities {
		if !c.Valid() {
			return connectivity.Fail(op, connectivity.ErrInvalid, "manifest declares an incomplete capability %q", c.String())
		}
		// This is the structural half of the P1A write firewall: a manifest
		// cannot claim write is already active. Requesting it is a separate,
		// adapter-independent flow; see [DeclareWriteCapability].
		if c.Operation == OpWrite {
			return connectivity.Fail(op, connectivity.ErrUnsupported,
				"capability %q declares write, which this release never publishes as active; request it through DeclareWriteCapability instead", c.String())
		}
		if !declared[c.Object] {
			return connectivity.Fail(op, connectivity.ErrInvalid,
				"capability %q names object %q the manifest does not declare", c.String(), string(c.Object))
		}
		if seen[c] {
			return connectivity.Fail(op, connectivity.ErrInvalid, "manifest declares capability %q twice", c.String())
		}
		seen[c] = true
	}

	return m.Bounds.Validate()
}

// Supports reports whether the manifest publishes the capability.
func (m AdapterManifest) Supports(c Capability) bool {
	for _, have := range m.Capabilities {
		if have == c {
			return true
		}
	}
	return false
}

// CapabilityVersion returns the version an adapter build publishes for
// object/operation, and whether any such capability exists. It exists so a
// caller need not enumerate Capabilities by hand to find the one version a
// well-formed manifest publishes for a pair.
func (m AdapterManifest) CapabilityVersion(object ObjectKind, operation Operation) (string, bool) {
	for _, c := range m.Capabilities {
		if c.Object == object && c.Operation == operation {
			return c.Version, true
		}
	}
	return "", false
}

const (
	adapterManifestSchema  = "hcmnext.connectivity.spi.AdapterManifest"
	adapterManifestVersion = 1
)

// Canonical returns the manifest's canonical bytes. It first validates the
// manifest: a digest is never produced over a manifest that could not be
// published.
func (m AdapterManifest) Canonical() ([]byte, error) {
	const op = "spi.AdapterManifest.Canonical"
	if err := m.Validate(); err != nil {
		return nil, err
	}
	w := canonicalbytes.New(adapterManifestSchema, adapterManifestVersion).
		String("adapter_id", m.AdapterID).
		String("vendor", m.Vendor).
		String("product", m.Product).
		String("version", m.Version)
	w.SortedStrings("objects", objectStrings(m.Objects))
	w.SortedStrings("capabilities", capabilityStrings(m.Capabilities))
	w.Int("bounds.max_page_size", int64(m.Bounds.MaxPageSize)).
		Int("bounds.max_pages_per_run", int64(m.Bounds.MaxPagesPerRun)).
		Int("bounds.max_records_per_run", int64(m.Bounds.MaxRecordsPerRun)).
		Int("bounds.max_record_bytes", int64(m.Bounds.MaxRecordBytes)).
		Int("bounds.min_request_interval_ns", int64(m.Bounds.MinRequestInterval))
	raw, err := w.Bytes()
	if err != nil {
		return nil, connectivity.Fail(op, connectivity.ErrInvalid, "encode manifest: %v", err)
	}
	return raw, nil
}

// Digest returns the manifest's content digest. Two manifests with the same
// published surface digest identically on every machine.
func (m AdapterManifest) Digest() (string, error) {
	raw, err := m.Canonical()
	if err != nil {
		return "", err
	}
	return canonicalbytes.Digest(raw), nil
}

// SortedObjects returns the manifest's objects in a stable, deterministic
// order. Generated code and golden fixtures use this rather than range order.
func (m AdapterManifest) SortedObjects() []ObjectKind {
	out := append([]ObjectKind(nil), m.Objects...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// SortedCapabilities returns the manifest's capabilities in a stable,
// deterministic order.
func (m AdapterManifest) SortedCapabilities() []Capability {
	out := append([]Capability(nil), m.Capabilities...)
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

func objectStrings(objects []ObjectKind) []string {
	out := make([]string, len(objects))
	for i, o := range objects {
		out[i] = string(o)
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
