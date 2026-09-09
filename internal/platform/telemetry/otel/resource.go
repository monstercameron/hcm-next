package otel

import (
	"go.opentelemetry.io/otel/attribute"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
)

// Resource attribute keys. These mirror the exact field names
// planning/specs/structured-logging-and-opentelemetry.md's envelope lists
// under "service.name/service.version/service.instance.id/
// deployment.environment/cell.id/region/process.role/build.digest" — the
// OTel-conventional dotted spelling, which is deliberately distinct from
// internal/platform/telemetry's own snake_case attribute allow-list keys
// (cell_id, tenant_class, ...): those govern span/metric/log *attributes*
// through the Evaluator; these govern the process *Resource* identity, which
// telemetry.Resource.Validate already checks field-by-field and which never
// passes through the attribute allow-list (a Resource is process identity,
// not caller-supplied content).
const (
	resourceKeyServiceName       = "service.name"
	resourceKeyServiceVersion    = "service.version"
	resourceKeyServiceInstanceID = "service.instance.id"
	resourceKeyEnvironment       = "deployment.environment"
	resourceKeyCellID            = "cell.id"
	resourceKeyRegion            = "region"
	resourceKeyProcessRole       = "process.role"
	resourceKeyBuildDigest       = "build.digest"
	resourceKeyTenantClass       = "tenant.class"
)

// buildResource validates res and converts it into an OTel SDK Resource. It
// never merges in sdkresource.Default() (host name, OS, PID, ...): the
// Resource this package exports is exactly and only the fields
// telemetry.Resource.Validate already certified, so a caller can reason
// about resource cardinality from the Go struct alone rather than from
// whatever a given OTel SDK version's default detectors happen to add.
//
// buildResource never reaches the allow-list/Evaluator: it is fed
// caller-constructed process identity (telemetry.NewResourceFromBuild),
// already validated field-by-field by Resource.Validate, not caller-supplied
// span/metric content.
func buildResource(res telemetry.Resource) (*sdkresource.Resource, error) {
	if err := res.Validate(); err != nil {
		return nil, err
	}

	attrs := []attribute.KeyValue{
		attribute.String(resourceKeyServiceName, res.ServiceName),
		attribute.String(resourceKeyServiceVersion, res.ServiceVersion),
		attribute.String(resourceKeyServiceInstanceID, res.ServiceInstanceID),
		attribute.String(resourceKeyEnvironment, res.Environment),
		attribute.String(resourceKeyCellID, res.CellID),
		attribute.String(resourceKeyProcessRole, string(res.ProcessRole)),
		attribute.String(resourceKeyBuildDigest, res.BuildDigest),
	}
	if res.Region != "" {
		attrs = append(attrs, attribute.String(resourceKeyRegion, res.Region))
	}
	if res.TenantClass != telemetry.TenantClassUnspecified {
		attrs = append(attrs, attribute.String(resourceKeyTenantClass, string(res.TenantClass)))
	}

	return sdkresource.NewSchemaless(attrs...), nil
}
