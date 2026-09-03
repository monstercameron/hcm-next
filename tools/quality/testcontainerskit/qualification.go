package testcontainerskit

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	// CandidateModule is deliberately only a string. The REJECT decision must
	// not add a module requirement merely to execute its qualification tests.
	CandidateModule = "github.com/testcontainers/testcontainers-go"
	ownedCleanup    = "owned-run-namespace"
)

var digestImage = regexp.MustCompile(`^[a-z0-9][a-z0-9./_-]*@sha256:[a-f0-9]{64}$`)

// EvidenceRow binds one test to the package that supplies its evidence.
type EvidenceRow struct {
	Test    string `yaml:"test"`
	Package string `yaml:"package"`
}

// Qualification is the parsed LIB-009 decision record.
type Qualification struct {
	Version                         int           `yaml:"version"`
	Todo                            string        `yaml:"todo"`
	Module                          string        `yaml:"module"`
	Role                            string        `yaml:"role"`
	Verdict                         string        `yaml:"verdict"`
	RuntimeDependencyGraphUnchanged bool          `yaml:"runtime_dependency_graph_unchanged"`
	Decision                        string        `yaml:"decision"`
	RemovalPath                     string        `yaml:"removal_path"`
	RuntimeProbe                    RuntimeProbe  `yaml:"runtime_probe"`
	Scope                           Scope         `yaml:"scope"`
	AdoptionRequirements            Requirements  `yaml:"adoption_requirements"`
	Evidence                        []EvidenceRow `yaml:"evidence"`
	Command                         string        `yaml:"command"`
}

// RuntimeProbe records the observed no-admission state. It is evidence from
// the reviewed environment, not a promise that a host daemon stays down.
type RuntimeProbe struct {
	ModuleInGoMod string `yaml:"module_in_go_mod"`
	ModuleInGoSum string `yaml:"module_in_go_sum"`
	DockerDaemon  string `yaml:"docker_daemon"`
}

// Scope names the decision's positive boundary and exclusions.
type Scope struct {
	Covers   []string `yaml:"covers"`
	Excludes []string `yaml:"excludes"`
}

// Requirements records the conditions a future ADOPT decision must prove.
type Requirements struct {
	Workloads           []string `yaml:"workloads"`
	ImagePin            string   `yaml:"image_pin"`
	Namespace           string   `yaml:"namespace"`
	Readiness           string   `yaml:"readiness"`
	Cleanup             string   `yaml:"cleanup"`
	FailurePreservation string   `yaml:"failure_preservation"`
}

// AdoptionPlan is an owned, dependency-free pre-admission contract. A future
// adapter must satisfy it before Testcontainers is added to go.mod; no
// Testcontainers type crosses this boundary.
type AdoptionPlan struct {
	TestOnly          bool
	ModuleVersion     string
	Images            map[string]string
	Namespace         string
	ReadinessTimeout  time.Duration
	CleanupTarget     string
	PreserveOnFailure bool
}

// LoadQualification reads a machine-verifiable decision record.
func LoadQualification(path string) (Qualification, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Qualification{}, fmt.Errorf("testcontainerskit: read qualification: %w", err)
	}
	var q Qualification
	if err := yaml.Unmarshal(data, &q); err != nil {
		return Qualification{}, fmt.Errorf("testcontainerskit: parse qualification: %w", err)
	}
	return q, nil
}

// ValidateAdoptionPlan enforces the minimum test-only environment mechanics
// that LIB-009 requires before a future decision may change from REJECT to
// ADOPT. It is deliberately side-effect free: this check never starts a
// container, pulls an image, or performs cleanup.
func ValidateAdoptionPlan(plan AdoptionPlan) error {
	if !plan.TestOnly {
		return errors.New("testcontainerskit: plan is not test-only")
	}
	if !pinnedVersion(plan.ModuleVersion) {
		return fmt.Errorf("testcontainerskit: module version %q is not a pinned release", plan.ModuleVersion)
	}
	if !validNamespace(plan.Namespace) {
		return fmt.Errorf("testcontainerskit: namespace %q is not an owned opaque test namespace", plan.Namespace)
	}
	if plan.ReadinessTimeout <= 0 || plan.ReadinessTimeout > 2*time.Minute {
		return fmt.Errorf("testcontainerskit: readiness timeout %s is not bounded", plan.ReadinessTimeout)
	}
	if plan.CleanupTarget != ownedCleanup {
		return fmt.Errorf("testcontainerskit: cleanup target %q is not confined to the owned run namespace", plan.CleanupTarget)
	}
	if !plan.PreserveOnFailure {
		return errors.New("testcontainerskit: failures must preserve diagnostic evidence")
	}
	for _, workload := range []string{"postgres", "s3", "smtp", "provider"} {
		image, ok := plan.Images[workload]
		if !ok || !digestImage.MatchString(image) {
			return fmt.Errorf("testcontainerskit: %s image %q is not digest-pinned", workload, image)
		}
	}
	if len(plan.Images) != 4 {
		return fmt.Errorf("testcontainerskit: image set has %d entries, want the four approved workloads", len(plan.Images))
	}
	return nil
}

// ReleaseGraph lists the resolved dependency graph of explicit release
// commands. It intentionally invokes Go rather than inspecting source text,
// so a future indirect import cannot bypass this check.
func ReleaseGraph(root string, targets ...string) ([]string, error) {
	if len(targets) == 0 {
		return nil, errors.New("testcontainerskit: release graph requires at least one target")
	}
	cmd := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}")
	cmd.Args = append(cmd.Args, targets...)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("testcontainerskit: go list release graph: %w\n%s", err, output)
	}
	seen := map[string]struct{}{}
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		name := strings.TrimSpace(scanner.Text())
		if name != "" {
			seen[name] = struct{}{}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("testcontainerskit: read release graph: %w", err)
	}
	graph := make([]string, 0, len(seen))
	for name := range seen {
		graph = append(graph, name)
	}
	sort.Strings(graph)
	return graph, nil
}

// FindRepoRoot walks upward from a caller-owned file or directory until it
// finds the root module. It keeps this package runnable from any test working
// directory without relying on an ambient current directory.
func FindRepoRoot(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("testcontainerskit: stat %s: %w", path, err)
	}
	dir := path
	if !info.IsDir() {
		dir = filepath.Dir(path)
	}
	for {
		if mod, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil && !mod.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("testcontainerskit: no go.mod above %s", path)
		}
		dir = parent
	}
}

func pinnedVersion(version string) bool {
	if !strings.HasPrefix(version, "v") || strings.Contains(strings.ToLower(version), "latest") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(version, "v"), ".")
	return len(parts) == 3 && parts[0] != "" && parts[1] != "" && parts[2] != ""
}

func validNamespace(namespace string) bool {
	if len(namespace) < 8 || len(namespace) > 96 || !strings.HasPrefix(namespace, "tc-") {
		return false
	}
	for i := 0; i < len(namespace); i++ {
		c := namespace[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			continue
		}
		return false
	}
	return namespace[len(namespace)-1] != '-'
}
