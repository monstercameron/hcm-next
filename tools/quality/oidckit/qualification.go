// Package oidckit contains the dependency-free admission checks for LIB-010.
// It deliberately does not import either candidate library: qualification is
// a pre-admission gate and must remain runnable while the candidates are absent.
package oidckit

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	OIDCModule   = "github.com/coreos/go-oidc"
	OAuth2Module = "golang.org/x/oauth2"
)

type EvidenceRow struct {
	Test    string `yaml:"test"`
	Package string `yaml:"package"`
}

// Qualification is the reviewed, machine-readable LIB-010 decision.
type Qualification struct {
	Version                         int           `yaml:"version"`
	Todo                            string        `yaml:"todo"`
	Role                            string        `yaml:"role"`
	OIDCModule                      string        `yaml:"oidc_module"`
	OAuth2Module                    string        `yaml:"oauth2_module"`
	Verdict                         string        `yaml:"verdict"`
	Decision                        string        `yaml:"decision"`
	RuntimeDependencyGraphUnchanged bool          `yaml:"runtime_dependency_graph_unchanged"`
	Scope                           Scope         `yaml:"scope"`
	Requirements                    Requirements  `yaml:"requirements"`
	Evidence                        []EvidenceRow `yaml:"evidence"`
	Command                         string        `yaml:"command"`
}
type Scope struct {
	Covers   []string `yaml:"covers"`
	Excludes []string `yaml:"excludes"`
}
type Requirements struct {
	Issuer        string   `yaml:"issuer"`
	Audience      string   `yaml:"audience"`
	Algorithms    []string `yaml:"algorithms"`
	PKCE          string   `yaml:"pkce"`
	StateNonce    string   `yaml:"state_nonce"`
	KeyRefresh    string   `yaml:"key_refresh"`
	Normalization string   `yaml:"normalization"`
}

func LoadQualification(path string) (Qualification, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Qualification{}, fmt.Errorf("oidckit: read qualification: %w", err)
	}
	var q Qualification
	if err := yaml.Unmarshal(b, &q); err != nil {
		return Qualification{}, fmt.Errorf("oidckit: parse qualification: %w", err)
	}
	return q, nil
}

// ValidateAdapterPlan checks the owned boundary a future ADOPT decision must
// satisfy. No provider value or library type may cross this boundary.
type AdapterPlan struct {
	TestOnly                                                     bool
	OIDCVersion, OAuth2Version                                   string
	Issuer, Audience                                             string
	Algorithms                                                   []string
	RequirePKCE, RequireState, RequireNonce, VerifyIssuer        bool
	RefreshOnKeyMiss, NormalizeClaims, NoAuthorizationFromClaims bool
}

func ValidateAdapterPlan(p AdapterPlan) error {
	if !p.TestOnly {
		return errors.New("oidckit: adapter plan must be test-only")
	}
	if !pinned(p.OIDCVersion) || !pinned(p.OAuth2Version) {
		return errors.New("oidckit: both module versions must be pinned")
	}
	if p.Issuer == "" || p.Audience == "" {
		return errors.New("oidckit: issuer and audience are required")
	}
	if !p.VerifyIssuer {
		return errors.New("oidckit: issuer verification is required")
	}
	if !p.RequirePKCE || !p.RequireState || !p.RequireNonce {
		return errors.New("oidckit: PKCE, state and nonce are required")
	}
	if !p.RefreshOnKeyMiss {
		return errors.New("oidckit: key refresh on miss is required")
	}
	if !p.NormalizeClaims || !p.NoAuthorizationFromClaims {
		return errors.New("oidckit: claims must normalize into an owned result, never authorization")
	}
	if len(p.Algorithms) == 0 {
		return errors.New("oidckit: explicit algorithm allow-list is required")
	}
	for _, a := range p.Algorithms {
		if a != "RS256" && a != "ES256" && a != "EdDSA" {
			return fmt.Errorf("oidckit: algorithm %q is not approved", a)
		}
	}
	return nil
}
func pinned(v string) bool {
	return strings.HasPrefix(v, "v") && len(v) > 2 && !strings.ContainsAny(v, "latest* ")
}

// ReleaseGraph returns the sorted transitive import graph for release targets.
func ReleaseGraph(root string, targets ...string) ([]string, error) {
	if len(targets) == 0 {
		return nil, errors.New("oidckit: release graph requires a target")
	}
	c := exec.Command("go", append([]string{"list", "-deps", "-f", "{{.ImportPath}}"}, targets...)...)
	c.Dir = root
	b, err := c.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("oidckit: go list: %w\n%s", err, b)
	}
	set := map[string]bool{}
	for _, s := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(s) != "" {
			set[strings.TrimSpace(s)] = true
		}
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out, nil
}
func FindRepoRoot(path string) (string, error) {
	i, e := os.Stat(path)
	if e != nil {
		return "", e
	}
	d := path
	if !i.IsDir() {
		d = filepath.Dir(path)
	}
	for {
		if _, e = os.Stat(filepath.Join(d, "go.mod")); e == nil {
			return d, nil
		}
		n := filepath.Dir(d)
		if n == d {
			break
		}
		d = n
	}
	return "", errors.New("oidckit: repository root not found")
}
