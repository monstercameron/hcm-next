// Package webdelivery validates the production frontend delivery manifest.
//
// The manifest (definitions/ux/production-frontend-delivery.yaml) describes the
// qualified delivery of the Promotion workspace rendering platform:
// GoWebComponents v5.0.1 compiled to WebAssembly, plus Go server-rendered
// HTML fallback, plus gRPC-over-WebSocket tunnel to canonical cell services.
//
// This package verifies:
//   - Every bundle's embed site exists in the source tree
//   - Build commands reference packages that exist
//   - CSP directives match the actual policy emitted by the workspace shell
//   - Size ceilings hold when assets are present
package webdelivery

import (
	"strings"

	"github.com/monstercameron/hcm-next/internal/humanwork/workspace"
)

// Manifest describes a production frontend delivery including bundles,
// routes, CSP, transport, and quality gates.
//
// It is intended to be unmarshaled from YAML and used by tests to verify
// the actual deployed system conforms to the documented contract.
type Manifest struct {
	Delivery struct {
		Name                string `yaml:"name"`
		Version             string `yaml:"version"`
		Title               string `yaml:"title"`
		Purpose             string `yaml:"purpose"`
		Authority           string `yaml:"authority"`
		RendererDecisionRef string `yaml:"renderer_decision_ref"`
	} `yaml:"delivery"`

	Bundles []struct {
		Name           string  `yaml:"name"`
		Description    string  `yaml:"description"`
		Purpose        string  `yaml:"purpose"`
		BuildCommand   string  `yaml:"build_command"`
		OutputPath     string  `yaml:"output_path"`
		EmbedSite      string  `yaml:"embed_site"`
		ContentType    string  `yaml:"content_type"`
		Runtime        string  `yaml:"runtime"`
		SizeBytes      int64   `yaml:"size_bytes"`
		SizeMiB        float64 `yaml:"size_mib"`
		SizeCeilingMiB float64 `yaml:"size_ceiling_mib"`
		Notes          string  `yaml:"notes"`
	} `yaml:"bundles"`

	Shell struct {
		Routes []struct {
			Path           string `yaml:"path"`
			Handler        string `yaml:"handler"`
			Method         string `yaml:"method"`
			Response       string `yaml:"response"`
			Authentication string `yaml:"authentication"`
			CacheControl   string `yaml:"cache_control"`
			Description    string `yaml:"description"`
		} `yaml:"routes"`
	} `yaml:"shell"`

	CSP struct {
		HeaderName        string   `yaml:"header_name"`
		IssuedBy          string   `yaml:"issued_by"`
		IssuedIn          string   `yaml:"issued_in"`
		BaseDirectives    []string `yaml:"base_directives"`
		DynamicDirectives []struct {
			Name        string `yaml:"name"`
			Value       string `yaml:"value"`
			Description string `yaml:"description"`
		} `yaml:"dynamic_directives"`
		Notes string `yaml:"notes"`
	} `yaml:"content_security_policy"`

	Transport struct {
		Tunnel struct {
			Path                    string `yaml:"path"`
			Protocol                string `yaml:"protocol"`
			AuthenticationScheme    string `yaml:"authentication_scheme"`
			AuthenticationPlacement string `yaml:"authentication_placement"`
			AuthenticationSource    string `yaml:"authentication_source"`
			TLSTermination          string `yaml:"tls_termination"`
			GrpcCredentials         string `yaml:"grpc_credentials"`
			Notes                   string `yaml:"notes"`
		} `yaml:"tunnel"`
	} `yaml:"transport"`

	ForbiddenRuntimes []struct {
		Name        string `yaml:"name"`
		Reason      string `yaml:"reason"`
		Enforcement string `yaml:"enforcement"`
	} `yaml:"forbidden_runtimes"`

	SizeBudget struct {
		JourneyWasmBytes          int64  `yaml:"journey_wasm_bytes"`
		JourneyWasmMiB            string `yaml:"journey_wasm_mib"`
		CeilingMiB                int64  `yaml:"ceiling_mib"`
		CeilingBytes              int64  `yaml:"ceiling_bytes"`
		CurrentUtilizationPercent string `yaml:"current_utilization_percent"`
		Notes                     string `yaml:"notes"`
	} `yaml:"size_budget"`

	QualityGates []struct {
		Name        string `yaml:"name"`
		Requirement string `yaml:"requirement"`
		Evidence    string `yaml:"evidence"`
	} `yaml:"quality_gates"`
}

// CSPDirectives reconstructs the full CSP header by concatenating base and
// dynamic directives with "; " separator. Dangerous values (unexamined from
// external input) must be sanitized by the caller before using the result
// in an HTTP header.
func (m *Manifest) CSPDirectives() string {
	directives := append([]string{}, m.CSP.BaseDirectives...)
	for _, dyn := range m.CSP.DynamicDirectives {
		directives = append(directives, dyn.Name+" "+dyn.Value)
	}
	return strings.Join(directives, "; ")
}

// WorkspaceCSP returns the Content-Security-Policy that the workspace shell
// (internal/humanwork/workspace.JourneyContentSecurityPolicy) emits for a
// given host. This is used by tests to verify the actual CSP structure.
func WorkspaceCSP(host string) string {
	return workspace.JourneyContentSecurityPolicy(host)
}

// parseCSPDirectives splits a CSP header into a map of directive_name -> directive_value.
// Multiple sources in a directive are joined with spaces.
func parseCSPDirectives(csp string) map[string]string {
	dirs := make(map[string]string)
	for _, part := range strings.Split(csp, "; ") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		parts := strings.SplitN(part, " ", 2)
		if len(parts) == 1 {
			dirs[parts[0]] = ""
		} else {
			dirs[parts[0]] = parts[1]
		}
	}
	return dirs
}
