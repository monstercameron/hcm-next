package appointment

import (
	"fmt"
	"strings"
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
	ID            string
	Version       string
	Name          string
	Kind          ResourceKind
	Qualification string
	PrivacyClass  string
	Capacity      int
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
		return reject("resource.capacity", "DRAFT", r.Version, "must be positive")
	}
	return nil
}

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
