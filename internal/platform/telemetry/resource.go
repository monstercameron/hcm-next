package telemetry

import (
	"errors"
	"fmt"

	"github.com/monstercameron/hcm-next/internal/platform/buildinfo"
)

// ResourceSchemaVersion tags Resource's field set. Any change to a required
// field, its meaning or its wire name requires bumping this version;
// historical resources keep validating under their own version rather than
// being silently reinterpreted.
const ResourceSchemaVersion = 1

// TenantClass buckets tenant scope for telemetry. It is a bounded
// operational bucket, never a tenant id, workspace name or org name: a
// resource that carries an actual tenant identity here is exactly the
// OBS-001 RED defect this package rejects (see Resource.Validate).
type TenantClass string

// Published tenant classes. The zero value, TenantClassUnspecified, is legal
// on Resource because TenantClass is optional (not every emitting process is
// tenant-scoped), but it is never a legal value to explicitly select.
const (
	TenantClassUnspecified TenantClass = ""
	TenantClassInternal    TenantClass = "internal"
	TenantClassTrial       TenantClass = "trial"
	TenantClassStandard    TenantClass = "standard"
	TenantClassEnterprise  TenantClass = "enterprise"
)

func (c TenantClass) valid() bool {
	switch c {
	case TenantClassUnspecified, TenantClassInternal, TenantClassTrial, TenantClassStandard, TenantClassEnterprise:
		return true
	default:
		return false
	}
}

// ProcessRole names the runtime role of the process emitting telemetry.
type ProcessRole string

// Published process roles.
const (
	ProcessRoleUnspecified ProcessRole = ""
	ProcessRoleAPI         ProcessRole = "api"
	ProcessRoleWorker      ProcessRole = "worker"
	ProcessRoleScheduler   ProcessRole = "scheduler"
	ProcessRoleMigrator    ProcessRole = "migrator"
	ProcessRoleOperator    ProcessRole = "operator"
)

func (r ProcessRole) valid() bool {
	switch r {
	case ProcessRoleAPI, ProcessRoleWorker, ProcessRoleScheduler, ProcessRoleMigrator, ProcessRoleOperator:
		return true
	default:
		return false
	}
}

// Resource-field validation errors. All are matchable with errors.Is and are
// also reported as a RejectionError naming the exact offending field (see
// Resource.Validate).
var (
	ErrResourceSchemaVersion = errors.New("telemetry: resource schema version is missing or unknown")
	ErrResourceField         = errors.New("telemetry: resource field is missing or invalid")
)

// Resource identifies the process instance that produced a telemetry
// signal. It is service-and-build identity, never business or tenant
// identity (structured-logging-and-opentelemetry.md envelope fields
// service.name/service.version/service.instance.id/deployment.environment/
// cell.id/region/process.role/build.digest).
type Resource struct {
	SchemaVersion     int
	ServiceName       string
	ServiceVersion    string
	ServiceInstanceID string
	Environment       string
	CellID            string
	Region            string
	ProcessRole       ProcessRole
	BuildDigest       string
	TenantClass       TenantClass
}

// NewResourceFromBuild builds a Resource, taking ServiceVersion and
// BuildDigest from the running binary's own build identity (OBS-001:
// "version from buildinfo") rather than letting a caller assert an
// arbitrary version string.
func NewResourceFromBuild(build buildinfo.Info, serviceName, serviceInstanceID, environment, cellID, region string, role ProcessRole, tenantClass TenantClass) Resource {
	version := build.Revision
	if version == "" {
		version = "unknown"
	}
	digest := build.Revision
	if build.Modified {
		digest += "+modified"
	}
	if digest == "" {
		digest = "unknown"
	}
	return Resource{
		SchemaVersion:     ResourceSchemaVersion,
		ServiceName:       serviceName,
		ServiceVersion:    version,
		ServiceInstanceID: serviceInstanceID,
		Environment:       environment,
		CellID:            cellID,
		Region:            region,
		ProcessRole:       role,
		BuildDigest:       digest,
		TenantClass:       tenantClass,
	}
}

// Validate reports the first missing or invalid required field. Region is
// optional (a single-region cell may leave it empty); TenantClass is
// optional but, when set, must be one of the published buckets.
func (r Resource) Validate() error {
	if r.SchemaVersion != ResourceSchemaVersion {
		return newRejection("OBS_001_REJECTED", "resource.schema_version", "unknown_version", r.SchemaVersion, ErrResourceSchemaVersion)
	}
	type field struct {
		name  string
		value string
	}
	required := []field{
		{"resource.service_name", r.ServiceName},
		{"resource.service_version", r.ServiceVersion},
		{"resource.service_instance_id", r.ServiceInstanceID},
		{"resource.environment", r.Environment},
		{"resource.cell_id", r.CellID},
		{"resource.build_digest", r.BuildDigest},
	}
	for _, f := range required {
		if f.value == "" {
			return newRejection("OBS_001_REJECTED", f.name, "missing", r.SchemaVersion,
				fmt.Errorf("%w: %s", ErrResourceField, f.name))
		}
	}
	if !r.ProcessRole.valid() || r.ProcessRole == ProcessRoleUnspecified {
		return newRejection("OBS_001_REJECTED", "resource.process_role", "missing_or_unknown", r.SchemaVersion,
			fmt.Errorf("%w: resource.process_role", ErrResourceField))
	}
	if !r.TenantClass.valid() {
		return newRejection("OBS_001_REJECTED", "resource.tenant_class", "unknown", r.SchemaVersion,
			fmt.Errorf("%w: resource.tenant_class", ErrResourceField))
	}
	return nil
}
