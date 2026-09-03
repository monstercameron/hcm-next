// Package celqual is the backend-neutral CEL qualification contract.
// It deliberately does not import CEL-Go: a runtime dependency must first be
// admitted and then plugged into this small, owned contract.
package celqual

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

const CandidateModule = "github.com/google/cel-go"

// Qualification is the machine-readable pre-admission decision.  Keeping
// this record dependency-free is intentional: a candidate that is not yet
// admitted must not be needed to run the gate that rejects it.
type Qualification struct {
	Version         int           `yaml:"version"`
	Todo            string        `yaml:"todo"`
	Module          string        `yaml:"module"`
	Verdict         string        `yaml:"verdict"`
	BackendContract string        `yaml:"backend_contract"`
	Decision        string        `yaml:"decision"`
	Evidence        []EvidenceRow `yaml:"evidence"`
}

// EvidenceRow is one test declared in the qualification record that carries
// the LIB-005 matrix evidence for the owned boundary.
type EvidenceRow struct {
	Test    string `yaml:"test"`
	Package string `yaml:"package"`
}

// LoadQualification reads the architecture decision without importing CEL-Go.
func LoadQualification(path string) (Qualification, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Qualification{}, fmt.Errorf("celqual: read qualification: %w", err)
	}
	var q Qualification
	if err := yaml.Unmarshal(b, &q); err != nil {
		return Qualification{}, fmt.Errorf("celqual: parse qualification: %w", err)
	}
	return q, nil
}

func ValidateQualification(q Qualification) error {
	if q.Version != 1 || q.Todo != "LIB-005" || q.Module != CandidateModule {
		return errors.New("celqual: qualification identity drifted")
	}
	if q.Verdict != "NOT_ADMITTED" {
		return fmt.Errorf("celqual: candidate verdict %q is not NOT_ADMITTED", q.Verdict)
	}
	if q.BackendContract != "tools/quality/celqual" || !strings.Contains(q.Decision, "BLOCKED_PENDING_DEPENDENCY_ADMISSION") {
		return errors.New("celqual: decision does not preserve the dependency gate")
	}
	return nil
}

func ReleaseGraph(root string, targets ...string) ([]string, error) {
	if len(targets) == 0 {
		return nil, errors.New("celqual: release graph requires a target")
	}
	c := exec.Command("go", append([]string{"list", "-deps", "-f", "{{.ImportPath}}"}, targets...)...)
	c.Dir = root
	b, err := c.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("celqual: go list: %w\n%s", err, b)
	}
	seen := map[string]struct{}{}
	for _, s := range strings.Split(string(b), "\n") {
		if s = strings.TrimSpace(s); s != "" {
			seen[s] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out, nil
}

func FindRepoRoot(path string) (string, error) {
	i, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	d := path
	if !i.IsDir() {
		d = filepath.Dir(d)
	}
	for {
		if _, err = os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d, nil
		}
		p := filepath.Dir(d)
		if p == d {
			break
		}
		d = p
	}
	return "", errors.New("celqual: repository root not found")
}

type Contract struct {
	MaxNodes         int
	MaxCost          int
	AllowedVariables map[string]struct{}
}
type Program struct {
	Canonical string
	Nodes     int
	Cost      int
	Digest    string
}

var forbidden = regexp.MustCompile(`(?i)\b(now|timestamp|random|rand|uuid|http|https|fetch|read|write|reflect|type|proto|container)\s*\(`)
var token = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func (c Contract) Validate(source string) (Program, error) {
	if c.MaxNodes <= 0 || c.MaxCost <= 0 {
		return Program{}, fmt.Errorf("invalid positive limits")
	}
	s := strings.Join(strings.Fields(source), " ")
	if s == "" {
		return Program{}, fmt.Errorf("empty expression")
	}
	if len(s) > c.MaxCost*4 {
		return Program{}, fmt.Errorf("expression exceeds bounded size")
	}
	if forbidden.MatchString(s) {
		return Program{}, fmt.Errorf("ambient capability or reflection is forbidden")
	}
	if strings.ContainsAny(s, "{};[]") {
		return Program{}, fmt.Errorf("comprehensions, blocks, and indexing are not in bounded subset")
	}
	if strings.Contains(s, "?") || strings.Contains(s, "|") {
		return Program{}, fmt.Errorf("ambiguous or non-canonical operator")
	}
	parts := strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' })
	for _, p := range parts {
		if !token.MatchString(p) || p == "true" || p == "false" || p == "null" {
			continue
		}
		if _, ok := c.AllowedVariables[p]; !ok {
			return Program{}, fmt.Errorf("undeclared variable %q", p)
		}
	}
	nodes := len(parts)
	if nodes > c.MaxNodes {
		return Program{}, fmt.Errorf("node limit exceeded: %d > %d", nodes, c.MaxNodes)
	}
	cost := nodes + strings.Count(s, "&&") + strings.Count(s, "||")
	if cost > c.MaxCost {
		return Program{}, fmt.Errorf("cost limit exceeded: %d > %d", cost, c.MaxCost)
	}
	h := sha256.Sum256([]byte(s))
	return Program{Canonical: s, Nodes: nodes, Cost: cost, Digest: hex.EncodeToString(h[:])}, nil
}
func (p Program) Deterministic() bool { return p.Canonical != "" && p.Digest != "" }
