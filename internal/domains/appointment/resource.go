package appointment

import (
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ResourceKind classifies an appointment resource. A resource may be a
// person, a place, equipment, or a governed capability.
type ResourceKind string

const (
	ResourcePerson     ResourceKind = "PERSON"
	ResourceLocation   ResourceKind = "LOCATION"
	ResourceEquipment  ResourceKind = "EQUIPMENT"
	ResourceCapability ResourceKind = "CAPABILITY"
)

// ResourceType is the versioned contract required to reserve a resource.
type ResourceType struct {
	ID              string
	Version         string
	Revision        uint64
	Name            string
	Kind            ResourceKind
	Qualification   string
	PrivacyClass    string
	Capacity        int
	CapacityMode    CapacityMode
	CapacityLimit   int64
	CanonicalDigest string
}

func (r ResourceType) Validate() error {
	for _, v := range []struct{ field, value string }{{"resource.id", r.ID}, {"resource.version", r.Version}, {"resource.name", r.Name}, {"resource.qualification", r.Qualification}, {"resource.privacy_class", r.PrivacyClass}} {
		if strings.TrimSpace(v.value) == "" {
			return reject(v.field, "DRAFT", r.Version, "is required")
		}
	}
	switch r.Kind {
	case ResourcePerson, ResourceLocation, ResourceEquipment, ResourceCapability:
	default:
		return reject("resource.kind", "DRAFT", r.Version, "must be PERSON, LOCATION, EQUIPMENT, or CAPABILITY")
	}
	if r.Capacity < 1 {
		if r.CapacityMode == "" {
			return reject("resource.capacity", "DRAFT", r.Version, "must be positive")
		}
	}
	if r.CapacityMode != "" {
		switch r.CapacityMode {
		case CapacityExclusive:
			if r.CapacityLimit != 0 && r.CapacityLimit != 1 {
				return reject("resource.capacity_limit", "DRAFT", r.Version, "EXCLUSIVE capacity must be one")
			}
		case CapacityShared:
			if r.CapacityLimit <= 0 {
				return reject("resource.capacity_limit", "DRAFT", r.Version, "SHARED capacity limit must be positive")
			}
		case CapacityUnlimited:
			if r.CapacityLimit != 0 {
				return reject("resource.capacity_limit", "DRAFT", r.Version, "UNLIMITED capacity has no limit")
			}
		default:
			return reject("resource.capacity_mode", "DRAFT", r.Version, "is not declared")
		}
	}
	if r.CanonicalDigest != "" && r.CanonicalDigest != r.computedDigest() {
		return reject("resource.canonical_digest", "DRAFT", r.Version, "does not match the canonical resource bytes")
	}
	return nil
}

// Canonical returns a stable resource definition encoding.
func (r ResourceType) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.canonicalBody()
}

func (r ResourceType) canonicalBody() []byte {
	mode, limit := r.capacity()
	w := canonicalbytes.New("hcmnext.domains.appointment.ResourceType", schemaVersion).
		String("id", r.ID).String("version", r.Version).Int("revision", int64(r.Revision)).
		String("name", r.Name).String("kind", string(r.Kind)).String("qualification", r.Qualification).
		String("privacy_class", r.PrivacyClass).String("capacity_mode", string(mode)).Int("capacity_limit", limit)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (r ResourceType) capacity() (CapacityMode, int64) {
	if r.CapacityMode != "" {
		return r.CapacityMode, r.CapacityLimit
	}
	return CapacityShared, int64(r.Capacity)
}

// CapacitySemantics returns the closed capacity rule carried by this type.
func (r ResourceType) CapacitySemantics() (CapacityMode, int64, error) {
	if err := r.Validate(); err != nil {
		return "", 0, err
	}
	mode, limit := r.capacity()
	return mode, limit, nil
}

func (r ResourceType) computedDigest() string {
	b := r.canonicalBody()
	if b == nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

// NewResourceType copies and digests a versioned resource type.
func NewResourceType(r ResourceType) (ResourceType, error) {
	if r.Revision == 0 {
		r.Revision = 1
	}
	if r.CapacityMode == "" {
		r.CapacityMode = CapacityShared
		r.CapacityLimit = int64(r.Capacity)
	}
	r.CanonicalDigest = ""
	r.CanonicalDigest = r.computedDigest()
	if err := r.Validate(); err != nil {
		return ResourceType{}, err
	}
	return r, nil
}

// Digest returns the digest of the validated immutable resource definition.
func (r ResourceType) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.computedDigest(), nil
}

// ResourceTypeRef identifies a declared resource type without embedding its
// mutable availability facts.
type ResourceTypeRef = values.EntityRef

// ResourceTypes validates a complete, uniquely identified resource contract.
func ResourceTypes(resources []ResourceType) error {
	if len(resources) == 0 {
		return reject("resources", "DRAFT", "", "at least one resource type is required")
	}
	seen := map[string]struct{}{}
	for i, r := range resources {
		if err := r.Validate(); err != nil {
			if x, ok := err.(*Rejection); ok {
				x.Field = fmt.Sprintf("resources[%d].%s", i, strings.TrimPrefix(x.Field, "resource."))
			}
			return err
		}
		key := strings.ToLower(strings.TrimSpace(r.ID))
		if _, ok := seen[key]; ok {
			return reject(fmt.Sprintf("resources[%d].id", i), "DRAFT", r.Version, "duplicates another resource type")
		}
		seen[key] = struct{}{}
	}
	return nil
}
