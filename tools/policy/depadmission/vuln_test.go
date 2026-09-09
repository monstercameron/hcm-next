package depadmission_test

import (
	"os"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/depadmission"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

func parseFixture(t *testing.T, name string) depadmission.ScanResult {
	t.Helper()
	f, err := os.Open(fixtureDir(t, "govulncheck", name))
	if err != nil {
		t.Fatalf("opening fixture %s: %v", name, err)
	}
	defer func() { _ = f.Close() }()
	result, err := depadmission.ParseGovulncheck(f)
	if err != nil {
		t.Fatalf("ParseGovulncheck(%s): %v", name, err)
	}
	return result
}

// TestGovulncheckAdmissionClassifiesReachableAndUnreachableFindings is the
// TOOL-024 primary test: given a `govulncheck -json` stream shaped exactly
// like the real tool's own wire format (a config message, interleaved osv
// and progress messages, and finding messages), it proves a finding whose
// trace shows an actual call into the vulnerable symbol is classified
// reachable, a module-only mention is classified unreachable, and neither
// is silently dropped from the parsed result.
func TestGovulncheckAdmissionClassifiesReachableAndUnreachableFindings(t *testing.T) {
	scan := parseFixture(t, "reachable_and_unreachable.json")

	if scan.Config == nil {
		t.Fatal("Config = nil, want the fixture's config message")
	}
	if scan.Config.ScannerName != "govulncheck" || scan.Config.DB != "https://vuln.go.dev" {
		t.Fatalf("Config = %+v, want scanner_name=govulncheck db=https://vuln.go.dev", scan.Config)
	}

	if len(scan.Findings) != 2 {
		t.Fatalf("len(Findings) = %d, want 2 (progress/osv messages must not produce findings)", len(scan.Findings))
	}

	reachable, unreachable := scan.Findings[0], scan.Findings[1]

	if !reachable.Reachable() {
		t.Errorf("finding %s: Reachable() = false, want true (trace ends in a named Function)", reachable.OSV)
	}
	if got := reachable.Module(); got != "example.com/vulnerable" {
		t.Errorf("reachable finding Module() = %q, want example.com/vulnerable", got)
	}
	if got := reachable.ModuleVersion(); got != "v1.2.3" {
		t.Errorf("reachable finding ModuleVersion() = %q, want v1.2.3", got)
	}

	if unreachable.Reachable() {
		t.Errorf("finding %s: Reachable() = true, want false (trace has no Function, module-only mention)", unreachable.OSV)
	}
	if got := unreachable.Module(); got != "example.com/unreachable-thing" {
		t.Errorf("unreachable finding Module() = %q, want example.com/unreachable-thing", got)
	}

	t.Run("RED: an unreachable finding is recorded, never silently ignored", func(t *testing.T) {
		policy := &depadmission.Policy{}
		disposition := depadmission.ClassifyFinding(policy, unreachable, time.Now())
		if disposition.Disposition != depadmission.FindingRecorded {
			t.Fatalf("unreachable finding disposition = %s, want RECORD (must still appear in the report)", disposition.Disposition)
		}
		if disposition.VulnerabilityID != "GO-2026-0002" {
			t.Fatalf("disposition dropped the vulnerability ID: %+v", disposition)
		}
	})

	t.Run("RED: a reachable finding without any exception blocks admission, it never silently passes", func(t *testing.T) {
		policy := &depadmission.Policy{}
		disposition := depadmission.ClassifyFinding(policy, reachable, time.Now())
		if disposition.Disposition != depadmission.FindingBlocked {
			t.Fatalf("reachable finding with no exception disposition = %s, want BLOCK", disposition.Disposition)
		}
	})
}

// TestTodo_TOOL_024_Security proves the exception mechanism itself is
// governed: a reachable finding is admitted only by a complete, unexpired,
// exact-version-bound exception; an incomplete, expired, or
// wrong-version exception must behave exactly as if no exception existed.
func TestTodo_TOOL_024_Security(t *testing.T) {
	scan := parseFixture(t, "reachable_and_unreachable.json")
	reachable := scan.Findings[0] // module=example.com/vulnerable version=v1.2.3 osv=GO-2026-0001
	now := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)

	complete := depadmission.VulnerabilityException{
		Module: "example.com/vulnerable", VulnerabilityID: "GO-2026-0001", ModuleVersion: "v1.2.3",
		Owner: "platform-foundation", Expiry: "2027-01-01", CompensatingControl: "not reachable from any HTTP entrypoint",
	}

	cases := []struct {
		name       string
		policy     *depadmission.Policy
		want       string
		wantReason bool
	}{
		{
			name:   "complete, unexpired, exact-version exception excepts the finding",
			policy: &depadmission.Policy{Vulnerability: depadmission.VulnerabilityPolicy{Exceptions: []depadmission.VulnerabilityException{complete}}},
			want:   depadmission.FindingExcepted,
		},
		{
			name: "RED: exception missing compensating_control is treated as absent",
			policy: &depadmission.Policy{Vulnerability: depadmission.VulnerabilityPolicy{Exceptions: []depadmission.VulnerabilityException{
				{Module: complete.Module, VulnerabilityID: complete.VulnerabilityID, ModuleVersion: complete.ModuleVersion, Owner: complete.Owner, Expiry: complete.Expiry},
			}}},
			want:       depadmission.FindingBlocked,
			wantReason: true,
		},
		{
			name: "RED: exception missing owner is treated as absent",
			policy: &depadmission.Policy{Vulnerability: depadmission.VulnerabilityPolicy{Exceptions: []depadmission.VulnerabilityException{
				{Module: complete.Module, VulnerabilityID: complete.VulnerabilityID, ModuleVersion: complete.ModuleVersion, Expiry: complete.Expiry, CompensatingControl: complete.CompensatingControl},
			}}},
			want:       depadmission.FindingBlocked,
			wantReason: true,
		},
		{
			name: "RED: expired exception is treated as absent",
			policy: &depadmission.Policy{Vulnerability: depadmission.VulnerabilityPolicy{Exceptions: []depadmission.VulnerabilityException{
				{Module: complete.Module, VulnerabilityID: complete.VulnerabilityID, ModuleVersion: complete.ModuleVersion, Owner: complete.Owner, Expiry: "2020-01-01", CompensatingControl: complete.CompensatingControl},
			}}},
			want:       depadmission.FindingBlocked,
			wantReason: true,
		},
		{
			name: "RED: exception digest-bound to a different module_version does not match",
			policy: &depadmission.Policy{Vulnerability: depadmission.VulnerabilityPolicy{Exceptions: []depadmission.VulnerabilityException{
				{Module: complete.Module, VulnerabilityID: complete.VulnerabilityID, ModuleVersion: "v1.2.2", Owner: complete.Owner, Expiry: complete.Expiry, CompensatingControl: complete.CompensatingControl},
			}}},
			want: depadmission.FindingBlocked,
		},
		{
			name:   "no exception at all blocks",
			policy: &depadmission.Policy{},
			want:   depadmission.FindingBlocked,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := depadmission.ClassifyFinding(tc.policy, reachable, now)
			if d.Disposition != tc.want {
				t.Fatalf("Disposition = %s, want %s (reason=%q)", d.Disposition, tc.want, d.Reason)
			}
			if tc.wantReason && d.Reason == "" {
				t.Fatal("BLOCK disposition carries no explanatory reason")
			}
			if tc.want == depadmission.FindingExcepted {
				if d.ExceptionOwner != complete.Owner || d.ExceptionExpiry != complete.Expiry {
					t.Fatalf("EXCEPTED disposition did not record owner/expiry: %+v", d)
				}
			}
		})
	}
}

// TestTodo_TOOL_024_Conformance checks this package's parser against the
// exact field names `govulncheck -json` uses on the wire (protocol_version,
// scanner_name, scanner_version, db, db_last_modified for config;
// module/version/package/function/receiver for a trace frame), and that
// missing scan-output evidence (RED: "scan output lacks tool/database/
// module-graph digests") is detected rather than silently accepted, plus
// that the module-graph digest this package computes itself is stable.
func TestTodo_TOOL_024_Conformance(t *testing.T) {
	scan := parseFixture(t, "reachable_and_unreachable.json")
	if scan.Config.ProtocolVersion != "v1.0.0" {
		t.Errorf("Config.ProtocolVersion = %q, want v1.0.0", scan.Config.ProtocolVersion)
	}
	if scan.Config.DBLastModified != "2026-08-01T00:00:00Z" {
		t.Errorf("Config.DBLastModified = %q, want 2026-08-01T00:00:00Z", scan.Config.DBLastModified)
	}
	if scan.Config.GoVersion != "go1.26.3" {
		t.Errorf("Config.GoVersion = %q, want go1.26.3", scan.Config.GoVersion)
	}

	t.Run("RED: a stream with no config message reports every identity field missing", func(t *testing.T) {
		missingScan := parseFixture(t, "missing_config.json")
		if missingScan.Config != nil {
			t.Fatalf("fixture unexpectedly carries a config message: %+v", missingScan.Config)
		}
		if len(missingScan.Findings) != 1 {
			t.Fatalf("len(Findings) = %d, want 1", len(missingScan.Findings))
		}
		missing := depadmission.ConfigMissingFields(missingScan.Config)
		want := []string{"scanner_name", "scanner_version", "db"}
		if len(missing) != len(want) {
			t.Fatalf("ConfigMissingFields(nil) = %v, want %v", missing, want)
		}
		for i, field := range want {
			if missing[i] != field {
				t.Fatalf("ConfigMissingFields(nil)[%d] = %q, want %q", i, missing[i], field)
			}
		}
	})

	t.Run("a fully-populated config reports nothing missing", func(t *testing.T) {
		if missing := depadmission.ConfigMissingFields(scan.Config); len(missing) != 0 {
			t.Fatalf("ConfigMissingFields(complete config) = %v, want none", missing)
		}
	})

	t.Run("ModuleGraphDigest is stable across repeated calls over the same go.sum", func(t *testing.T) {
		root := repopath.RootDir()
		first, err := depadmission.ModuleGraphDigest(root)
		if err != nil {
			t.Fatalf("ModuleGraphDigest: %v", err)
		}
		second, err := depadmission.ModuleGraphDigest(root)
		if err != nil {
			t.Fatalf("ModuleGraphDigest: %v", err)
		}
		if first != second {
			t.Fatalf("ModuleGraphDigest is not stable: %q vs %q", first, second)
		}
		if first == "" {
			t.Fatal("ModuleGraphDigest returned an empty digest")
		}
	})
}
