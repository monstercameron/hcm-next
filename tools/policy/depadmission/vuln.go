package depadmission

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Frame is one entry in a Finding's call trace, matching `govulncheck
// -json`'s own wire shape. The deepest frame (index 0) names the
// vulnerable symbol itself; later frames walk outward toward an entry
// point. A frame with an empty Function is a module- or package-level
// mention with no traced call into it.
type Frame struct {
	Module   string `json:"module"`
	Version  string `json:"version,omitempty"`
	Package  string `json:"package,omitempty"`
	Function string `json:"function,omitempty"`
	Receiver string `json:"receiver,omitempty"`
}

// Finding is one vulnerability finding, matching `govulncheck -json`'s
// "finding" message.
type Finding struct {
	OSV          string   `json:"osv"`
	FixedVersion string   `json:"fixed_version,omitempty"`
	Trace        []*Frame `json:"trace"`
}

// Reachable reports whether f's trace shows an actual call chain into the
// vulnerable symbol (any frame naming a Function), as opposed to a
// module-only mention (import present, nothing observed calling it). This
// is the "symbol-aware" distinction TOOL-024 adds on top of TOOL-019's
// module-level policy.
func (f Finding) Reachable() bool {
	for _, frame := range f.Trace {
		if frame != nil && frame.Function != "" {
			return true
		}
	}
	return false
}

// Module returns the vulnerable module's path, taken from the deepest
// trace frame. It returns "" for a malformed finding with no trace, which
// the caller must treat as evidence corruption, not as an admissible
// finding.
func (f Finding) Module() string {
	if len(f.Trace) == 0 || f.Trace[0] == nil {
		return ""
	}
	return f.Trace[0].Module
}

// ModuleVersion returns the vulnerable module's version at the deepest
// trace frame, for matching against a digest-bound exception.
func (f Finding) ModuleVersion() string {
	if len(f.Trace) == 0 || f.Trace[0] == nil {
		return ""
	}
	return f.Trace[0].Version
}

// Config is `govulncheck -json`'s "config" message: the scanner and
// database identity a scan ran against. TOOL-024's RED clause requires
// this evidence to be present in the scan output; a report built from a
// stream that never sent one is scan-output evidence, not a clean pass.
type Config struct {
	ProtocolVersion string `json:"protocol_version,omitempty"`
	ScannerName     string `json:"scanner_name,omitempty"`
	ScannerVersion  string `json:"scanner_version,omitempty"`
	DB              string `json:"db,omitempty"`
	DBLastModified  string `json:"db_last_modified,omitempty"`
	GoVersion       string `json:"go_version,omitempty"`
}

// message mirrors one line of `govulncheck -json`'s output stream: a
// sequence of objects, each populating exactly one of the known fields.
// osv and progress messages carry information this package does not need
// (the full OSV database entry, and human-readable progress text), so they
// are decoded as raw JSON only to advance the stream correctly.
type message struct {
	Config   *Config         `json:"config,omitempty"`
	Progress json.RawMessage `json:"progress,omitempty"`
	OSV      json.RawMessage `json:"osv,omitempty"`
	Finding  *Finding        `json:"finding,omitempty"`
}

// ScanResult is what ParseGovulncheck extracts from one `govulncheck
// -json` stream.
type ScanResult struct {
	// Config is the scanner/database identity the stream declared, or nil
	// if the stream never sent a config message.
	Config *Config
	// Findings is every finding message, in stream order. Unreachable
	// findings are included: TOOL-024's RED clause forbids silently
	// dropping them.
	Findings []Finding
}

// ParseGovulncheck decodes a `govulncheck -json` output stream. The real
// tool emits a sequence of complete JSON objects with no enclosing array
// (the same convention as `go list -json`), so this reads with a
// json.Decoder loop rather than unmarshaling a single array.
func ParseGovulncheck(r io.Reader) (ScanResult, error) {
	var result ScanResult
	decoder := json.NewDecoder(r)
	for decoder.More() {
		var msg message
		if err := decoder.Decode(&msg); err != nil {
			return ScanResult{}, fmt.Errorf("depadmission: decoding govulncheck output: %w", err)
		}
		if msg.Config != nil {
			result.Config = msg.Config
		}
		if msg.Finding != nil {
			result.Findings = append(result.Findings, *msg.Finding)
		}
	}
	return result, nil
}

// ParseGovulncheckFile reads and parses path as a `govulncheck -json`
// stream.
func ParseGovulncheckFile(path string) (ScanResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return ScanResult{}, fmt.Errorf("depadmission: opening govulncheck evidence %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	return ParseGovulncheck(f)
}

// ConfigMissingFields reports which of the fields TOOL-024's evidence
// contract requires are absent from cfg. A nil cfg (no config message in
// the stream at all) reports every field missing.
func ConfigMissingFields(cfg *Config) []string {
	var missing []string
	if cfg == nil {
		return []string{"scanner_name", "scanner_version", "db"}
	}
	if cfg.ScannerName == "" {
		missing = append(missing, "scanner_name")
	}
	if cfg.ScannerVersion == "" {
		missing = append(missing, "scanner_version")
	}
	if cfg.DB == "" {
		missing = append(missing, "db")
	}
	return missing
}

// ModuleGraphDigest returns a stable digest of root/go.sum: the "module
// graph digest" component of the scan evidence contract, computed by this
// package itself rather than trusted from the scanner, so that policy
// evaluation is always joined to the exact dependency set it ran over.
func ModuleGraphDigest(root string) (string, error) {
	data, err := os.ReadFile(root + string(os.PathSeparator) + "go.sum")
	if err != nil {
		return "", fmt.Errorf("depadmission: reading go.sum: %w", err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
