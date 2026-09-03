package eastwest

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Dependency struct {
	Consumer    string `yaml:"consumer"`
	Dependency  string `yaml:"dependency"`
	Owner       string `yaml:"owner"`
	Criticality string `yaml:"criticality"`
	TenantScope string `yaml:"tenant_scope"`
	CellScope   string `yaml:"cell_scope"`
	WorkloadID  string `yaml:"workload_identity"`
	NetworkPath string `yaml:"network_path"`
	Protocol    string `yaml:"protocol"`
	SchemaRange string `yaml:"schema_range"`
	Timeout     string `yaml:"timeout"`
	Staleness   string `yaml:"staleness"`
	Fallback    string `yaml:"fallback"`
	SLO         string `yaml:"slo"`
	Version     string `yaml:"version"`
	EvidenceRef string `yaml:"evidence_ref"`
	VerifiedAt  string `yaml:"verified_at"`
	ExpiresAt   string `yaml:"expires_at"`
}

type Manifest struct {
	Version      int          `yaml:"version"`
	Module       string       `yaml:"module"`
	EffectiveAt  string       `yaml:"effective_at"`
	Dependencies []Dependency `yaml:"dependencies"`
}

func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (m *Manifest) Validate() error {
	if m.Module == "" {
		return fmt.Errorf("module required")
	}
	for i, d := range m.Dependencies {
		if d.Consumer == "" || d.Dependency == "" || d.WorkloadID == "" {
			return fmt.Errorf("dependency %d missing required fields", i)
		}
		if d.Version == "" {
			return fmt.Errorf("dependency %d missing version", i)
		}
		if d.VerifiedAt == "" || d.ExpiresAt == "" {
			return fmt.Errorf("dependency %d missing validity", i)
		}
		if _, err := time.Parse(time.RFC3339, d.VerifiedAt); err != nil {
			return fmt.Errorf("dependency %d bad verified_at", i)
		}
		if _, err := time.Parse(time.RFC3339, d.ExpiresAt); err != nil {
			return fmt.Errorf("dependency %d bad expires_at", i)
		}
	}
	return nil
}

type Policy struct {
	entries map[string]Dependency
	digest  string
}

func Compile(m *Manifest) (*Policy, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	entries := make(map[string]Dependency)
	keys := make([]string, 0, len(m.Dependencies))
	for _, d := range m.Dependencies {
		k := d.Consumer + "->" + d.Dependency
		entries[k] = d
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		d := entries[k]
		fmt.Fprintf(h, "%s|%s|%s|%s|%s|%s|%s;", d.Consumer, d.Dependency, d.WorkloadID, d.CellScope, d.Version, d.VerifiedAt, d.ExpiresAt)
	}
	digest := hex.EncodeToString(h.Sum(nil))
	return &Policy{entries: entries, digest: digest}, nil
}

func (p *Policy) Digest() string { return p.digest }

func (p *Policy) Allows(callerWorkload, target, method, cell string, now time.Time) error {
	if callerWorkload == "" || target == "" || method == "" {
		return fmt.Errorf("missing identity")
	}
	k := callerWorkload + "->" + target
	d, ok := p.entries[k]
	if !ok {
		return fmt.Errorf("unknown workload")
	}
	if d.WorkloadID != callerWorkload {
		return fmt.Errorf("unauthorized workload")
	}
	if d.CellScope == "cell-local" && cell == "" {
		return fmt.Errorf("wrong cell")
	}
	if d.CellScope == "cell-local" && cell != "cell-local" {
		return fmt.Errorf("wrong cell")
	}
	verified, _ := time.Parse(time.RFC3339, d.VerifiedAt)
	expires, _ := time.Parse(time.RFC3339, d.ExpiresAt)
	if now.Before(verified) || !now.Before(expires) {
		return fmt.Errorf("stale endpoint")
	}
	if strings.Contains(target, "postgres-store") && method != "READ" && method != "WRITE" && method != "MIGRATE" {
		return fmt.Errorf("unauthorized method")
	}
	return nil
}

func SignRequest(payload, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func VerifyRequest(payload, sig, secret string) bool {
	exp := SignRequest(payload, secret)
	return hmac.Equal([]byte(exp), []byte(sig))
}
