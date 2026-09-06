// Package workload validates the provider-neutral runtime requirements for a
// digest-pinned Go workload. It emits no deployment side effects; a concrete
// adapter can consume the validated manifest later.
package workload

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Version reports the IAC-005 workload contract version.
func Version() int { return 1 }

// Explain describes the admission contract consumed by deployment adapters.
func Explain() string {
	return "IAC-005 v1: signed digest-pinned Go workloads with probes, budgets, identity, and graceful drain"
}

// Probe is a non-mutating health check.
type Probe struct {
	Path             string
	Port             int
	InitialDelaySecs int
	PeriodSecs       int
	TimeoutSecs      int
	FailureThreshold int
}

// Resources is the mandatory request/limit envelope for one container.
type Resources struct {
	CPURequest    string
	CPULimit      string
	MemoryRequest string
	MemoryLimit   string
}

// Container is one Go process in a workload manifest.
type Container struct {
	Name                   string
	Image                  string
	ImageDigest            string
	SignatureVerified      bool
	RunAsNonRoot           bool
	Privileged             bool
	ReadOnlyRootFilesystem bool
	ServiceAccount         string
	Permissions            []string
	Resources              Resources
	Liveness               Probe
	Readiness              Probe
}

// Manifest is the complete deployable workload contract.
type Manifest struct {
	Name                string
	Namespace           string
	CellID              string
	TenantID            string
	Replicas            int
	GracefulDrainSecs   int
	MaxUnavailable      int
	MinAvailable        int
	ServiceAccount      string
	ServiceAccountRoles []string
	Containers          []Container
}

// Violation is one deterministic manifest admission finding.
type Violation struct {
	Field  string
	Code   string
	Detail string
}

// Validate returns every missing or unsafe workload property.
func Validate(m Manifest) []Violation {
	var out []Violation
	need := func(field, code, detail string, missing bool) {
		if missing {
			out = append(out, Violation{Field: field, Code: code, Detail: detail})
		}
	}
	need("name", "MISSING_ID", "workload name is required", strings.TrimSpace(m.Name) == "")
	need("namespace", "MISSING_NAMESPACE", "workload namespace is required", strings.TrimSpace(m.Namespace) == "")
	need("cell_id", "MISSING_CELL", "workload cell id is required", strings.TrimSpace(m.CellID) == "")
	need("tenant_id", "MISSING_TENANT", "workload tenant id is required", strings.TrimSpace(m.TenantID) == "")
	need("replicas", "MISSING_REPLICAS", "workload must have at least one replica", m.Replicas <= 0)
	need("graceful_drain_secs", "MISSING_DRAIN", "workload must declare a positive graceful drain budget", m.GracefulDrainSecs <= 0)
	if m.MaxUnavailable < 0 || m.MaxUnavailable >= max(1, m.Replicas) {
		out = append(out, Violation{Field: "max_unavailable", Code: "INVALID_DISRUPTION_BUDGET", Detail: "max unavailable must be non-negative and below replica count"})
	}
	if m.MinAvailable <= 0 || m.MinAvailable > m.Replicas {
		out = append(out, Violation{Field: "min_available", Code: "INVALID_DISRUPTION_BUDGET", Detail: "min available must be positive and no greater than replicas"})
	}
	need("service_account", "MISSING_SERVICE_ACCOUNT", "workload must declare a least-privilege service account", strings.TrimSpace(m.ServiceAccount) == "")
	if len(m.ServiceAccountRoles) == 0 {
		out = append(out, Violation{Field: "service_account_roles", Code: "MISSING_LEAST_PRIVILEGE", Detail: "service account roles must be explicit, even when empty access is intended"})
	}
	for i, c := range m.Containers {
		prefix := fmt.Sprintf("containers[%d]", i)
		need(prefix+".name", "MISSING_CONTAINER_NAME", "container name is required", strings.TrimSpace(c.Name) == "")
		need(prefix+".image", "MISSING_IMAGE", "container image is required", strings.TrimSpace(c.Image) == "")
		if !validDigest(c.ImageDigest) {
			out = append(out, Violation{Field: prefix + ".image_digest", Code: "MUTABLE_IMAGE", Detail: "container image must be pinned by a 64-hex sha256 digest"})
		}
		if !c.SignatureVerified {
			out = append(out, Violation{Field: prefix + ".signature", Code: "UNVERIFIED_IMAGE", Detail: "container digest must be signature-verified"})
		}
		if !c.RunAsNonRoot {
			out = append(out, Violation{Field: prefix + ".run_as_non_root", Code: "ROOT_IDENTITY", Detail: "container must run as non-root"})
		}
		if c.Privileged {
			out = append(out, Violation{Field: prefix + ".privileged", Code: "PRIVILEGED_CONTAINER", Detail: "privileged containers are forbidden"})
		}
		need(prefix+".service_account", "MISSING_CONTAINER_SERVICE_ACCOUNT", "container service account must match the workload identity", strings.TrimSpace(c.ServiceAccount) == "" || c.ServiceAccount != m.ServiceAccount)
		if len(c.Permissions) != 0 {
			out = append(out, Violation{Field: prefix + ".permissions", Code: "EXCESS_PERMISSION", Detail: "container permissions must be empty; workload roles are centrally declared"})
		}
		validateResources(prefix+".resources", c.Resources, &out)
		validateProbe(prefix+".liveness", c.Liveness, &out)
		validateProbe(prefix+".readiness", c.Readiness, &out)
	}
	if len(m.Containers) == 0 {
		out = append(out, Violation{Field: "containers", Code: "MISSING_CONTAINERS", Detail: "workload must declare at least one container"})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Field != out[j].Field {
			return out[i].Field < out[j].Field
		}
		return out[i].Code < out[j].Code
	})
	return out
}

// Check returns a stable error for the first manifest violation.
func Check(m Manifest) error {
	if violations := Validate(m); len(violations) != 0 {
		v := violations[0]
		return fmt.Errorf("workload: %s %s: %s", v.Code, v.Field, v.Detail)
	}
	return nil
}

// CanonicalJSON returns a stable golden representation of a valid manifest.
func CanonicalJSON(m Manifest) ([]byte, error) {
	if err := Check(m); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("workload: encode manifest: %w", err)
	}
	return append(data, '\n'), nil
}

// Digest returns the content identity of the canonical manifest.
func Digest(m Manifest) (string, error) {
	data, err := CanonicalJSON(m)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func validateResources(field string, r Resources, out *[]Violation) {
	for name, value := range map[string]string{"cpu_request": r.CPURequest, "cpu_limit": r.CPULimit, "memory_request": r.MemoryRequest, "memory_limit": r.MemoryLimit} {
		if strings.TrimSpace(value) == "" {
			*out = append(*out, Violation{Field: field + "." + name, Code: "MISSING_RESOURCE_BUDGET", Detail: "container resource request and limit are mandatory"})
		}
	}
}

func validateProbe(field string, p Probe, out *[]Violation) {
	if strings.TrimSpace(p.Path) == "" || p.Port <= 0 || p.Port > 65535 || p.PeriodSecs <= 0 || p.TimeoutSecs <= 0 || p.FailureThreshold <= 0 {
		*out = append(*out, Violation{Field: field, Code: "MISSING_PROBE", Detail: "probe requires path, valid port, period, timeout, and failure threshold"})
	}
}

func validDigest(value string) bool {
	if !strings.HasPrefix(value, "sha256:") {
		return false
	}
	value = strings.TrimPrefix(value, "sha256:")
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return true
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
