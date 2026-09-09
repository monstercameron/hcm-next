// Package providerdrift compares a signed provider contract with a fresh
// observation and applies a fail-closed dispatch fence until compatibility is
// reviewed.
package providerdrift

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/providercontract"
)

const contractVersion = 1

// Version is the provider-drift contract version.
func Version() int { return contractVersion }

// Explain describes the package without provider payloads.
func Explain() string {
	return "providerdrift v1: typed contract diff, dispatch quarantine, in-flight observation fence and reviewed resume"
}

// State is the only state a drift evaluator may publish.
type State string

const (
	StateHealthy     State = "HEALTHY"
	StateUnknown     State = "UNKNOWN"
	StateQuarantined State = "QUARANTINED"
	StateResumed     State = "RESUMED"
)

// Contract is the signed or observed provider surface. Maps are semantic
// registries: a missing observed key is a drift/unknown signal, never a value
// to silently inherit from the signed copy.
type Contract struct {
	ProviderID         string
	AdapterID          string
	AdapterVersion     string
	ManifestDigest     string
	SchemaVersions     map[string]string
	CapabilityVersions map[string]string
	Permissions        map[string]bool
	QuotaPerMinute     int
	ErrorClasses       []string
	WebhookVersion     string
}

// FromTopology creates the signed contract portion of PROVIDER-002.
func FromTopology(t providercontract.Topology, manifestDigest string) (Contract, error) {
	if err := t.Validate(); err != nil {
		return Contract{}, err
	}
	contract := Contract{
		ProviderID: t.ProviderID, AdapterID: t.ProviderID + ".fixture", AdapterVersion: "PLACEHOLDER_ADAPTER_BUILD_V1", ManifestDigest: manifestDigest,
		SchemaVersions: map[string]string{}, CapabilityVersions: map[string]string{}, Permissions: map[string]bool{}, QuotaPerMinute: 0,
		ErrorClasses: []string{"TRANSIENT", "SCHEMA", "UNKNOWN"}, WebhookVersion: "PLACEHOLDER_WEBHOOK_CONTRACT_V1",
	}
	for _, c := range t.Capabilities {
		contract.SchemaVersions[c.Object] = c.ReadVersion
		contract.CapabilityVersions[c.ID] = c.ReadVersion + "+" + c.ObserveVersion
		contract.Permissions[c.ID] = true
		if contract.QuotaPerMinute == 0 || c.QuotaPerMinute < contract.QuotaPerMinute {
			contract.QuotaPerMinute = c.QuotaPerMinute
		}
	}
	return contract, contract.Validate()
}

// Validate checks that the contract has enough data to support a meaningful
// comparison. It intentionally accepts placeholder values.
func (c Contract) Validate() error {
	for name, value := range map[string]string{"provider_id": c.ProviderID, "adapter_id": c.AdapterID, "adapter_version": c.AdapterVersion, "manifest_digest": c.ManifestDigest, "webhook_version": c.WebhookVersion} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("providerdrift: %s is required", name)
		}
	}
	if len(c.SchemaVersions) == 0 || len(c.CapabilityVersions) == 0 || len(c.Permissions) == 0 || c.QuotaPerMinute <= 0 || len(c.ErrorClasses) == 0 {
		return errors.New("providerdrift: contract has incomplete registries")
	}
	return nil
}

// Change describes one typed contract difference and its affected semantic
// capabilities.
type Change struct {
	Kind     string
	Path     string
	Before   string
	After    string
	Affected []string
}

// Report is the evaluator output. NewDispatchAllowed is false for both
// QUARANTINED and UNKNOWN. InFlightDisposition preserves ambiguity instead
// of hiding it behind a provider-health result.
type Report struct {
	Expected             Contract
	Observed             Contract
	State                State
	NewDispatchAllowed   bool
	InFlightDisposition  string
	Changes              []Change
	AffectedCapabilities []string
	Digest               string
}

// Compare evaluates a signed contract against a probe observation.
func Compare(expected, observed Contract) (Report, error) {
	if err := expected.Validate(); err != nil {
		return Report{}, fmt.Errorf("expected contract: %w", err)
	}
	if err := observed.Validate(); err != nil {
		return Report{}, fmt.Errorf("observed contract: %w", err)
	}
	changes := make([]Change, 0)
	add := func(kind, path, before, after string, affected ...string) {
		changes = append(changes, Change{Kind: kind, Path: path, Before: before, After: after, Affected: uniqueSorted(affected)})
	}
	if expected.ProviderID != observed.ProviderID {
		add("IDENTITY", "provider_id", expected.ProviderID, observed.ProviderID)
	}
	if expected.AdapterID != observed.AdapterID {
		add("IDENTITY", "adapter_id", expected.AdapterID, observed.AdapterID)
	}
	if expected.AdapterVersion != observed.AdapterVersion {
		add("VERSION", "adapter_version", expected.AdapterVersion, observed.AdapterVersion)
	}
	if expected.ManifestDigest != observed.ManifestDigest {
		add("MANIFEST", "manifest_digest", expected.ManifestDigest, observed.ManifestDigest)
	}
	compareStringMap(expected.SchemaVersions, observed.SchemaVersions, "schema", &changes)
	compareStringMap(expected.CapabilityVersions, observed.CapabilityVersions, "capability", &changes)
	compareBoolMap(expected.Permissions, observed.Permissions, &changes)
	if expected.QuotaPerMinute != observed.QuotaPerMinute {
		add("QUOTA", "quota_per_minute", fmt.Sprint(expected.QuotaPerMinute), fmt.Sprint(observed.QuotaPerMinute), keys(expected.CapabilityVersions)...)
	}
	if !equalStrings(expected.ErrorClasses, observed.ErrorClasses) {
		add("ERROR", "error_classes", strings.Join(expected.ErrorClasses, ","), strings.Join(observed.ErrorClasses, ","), keys(expected.CapabilityVersions)...)
	}
	if expected.WebhookVersion != observed.WebhookVersion {
		add("WEBHOOK", "webhook_version", expected.WebhookVersion, observed.WebhookVersion, keys(expected.CapabilityVersions)...)
	}

	affected := make([]string, 0)
	unknown := false
	for _, change := range changes {
		affected = append(affected, change.Affected...)
		if change.Before == "<unknown>" || change.After == "<unknown>" {
			unknown = true
		}
	}
	state := StateHealthy
	if unknown {
		state = StateUnknown
	} else if len(changes) > 0 {
		state = StateQuarantined
	}
	report := Report{Expected: expected, Observed: observed, State: state, NewDispatchAllowed: state == StateHealthy, InFlightDisposition: "NONE", Changes: changes, AffectedCapabilities: uniqueSorted(affected)}
	if state != StateHealthy {
		report.InFlightDisposition = "PRESERVE_OPERATION_ID_AND_REQUIRE_OBSERVATION"
	}
	report.Digest = reportDigest(report)
	return report, nil
}

func compareStringMap(expected, observed map[string]string, kind string, changes *[]Change) {
	for _, key := range unionKeys(expected, observed) {
		before, beforeOK := expected[key]
		after, afterOK := observed[key]
		if !beforeOK {
			before = "<unknown>"
		}
		if !afterOK {
			after = "<unknown>"
		}
		if before != after {
			affected := []string{key}
			if kind == "schema" {
				affected = nil
			}
			*changes = append(*changes, Change{Kind: strings.ToUpper(kind), Path: kind + "." + key, Before: before, After: after, Affected: affected})
		}
	}
}

func compareBoolMap(expected, observed map[string]bool, changes *[]Change) {
	for _, key := range unionBoolKeys(expected, observed) {
		before, beforeOK := expected[key]
		after, afterOK := observed[key]
		beforeText, afterText := fmt.Sprint(before), fmt.Sprint(after)
		if !beforeOK {
			beforeText = "<unknown>"
		}
		if !afterOK {
			afterText = "<unknown>"
		}
		if beforeText != afterText {
			*changes = append(*changes, Change{Kind: "PERMISSION", Path: "permission." + key, Before: beforeText, After: afterText, Affected: []string{key}})
		}
	}
}

// Review is the human-controlled compatibility decision. The evaluator never
// invents these values; compatible review and re-selection are both explicit.
type Review struct {
	ReviewID       string
	Compatible     bool
	Reselect       bool
	ProviderID     string
	AdapterVersion string
	ManifestDigest string
	ConfigDigest   string
	ApprovedAt     time.Time
}

// Reconcile applies a reviewed decision to a quarantined report. An absent,
// stale or incompatible review leaves the fence in place.
func Reconcile(report Report, review Review) Report {
	if report.State == StateHealthy {
		return report
	}
	if strings.TrimSpace(review.ReviewID) == "" || review.ApprovedAt.IsZero() || strings.TrimSpace(review.ConfigDigest) == "" {
		return report
	}
	if review.Reselect {
		report.State = StateResumed
		report.NewDispatchAllowed = true
		report.InFlightDisposition = "RESELECTED_WITH_EXPLICIT_REVIEW"
		report.Digest = reportDigest(report)
		return report
	}
	if !review.Compatible || review.ProviderID != report.Observed.ProviderID || review.AdapterVersion != report.Observed.AdapterVersion || review.ManifestDigest != report.Observed.ManifestDigest {
		return report
	}
	report.State = StateResumed
	report.NewDispatchAllowed = true
	report.InFlightDisposition = "RESUMED_AFTER_COMPATIBLE_ADAPTER_ROLLOUT"
	report.Digest = reportDigest(report)
	return report
}

// Explain returns bounded drift evidence without raw provider responses.
func (r Report) Explain() string {
	return fmt.Sprintf("state=%s new_dispatch=%t changes=%d affected=%d in_flight=%s digest=%s", r.State, r.NewDispatchAllowed, len(r.Changes), len(r.AffectedCapabilities), r.InFlightDisposition, r.Digest)
}

func reportDigest(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s|%t|%s|", r.State, r.NewDispatchAllowed, r.InFlightDisposition)
	for _, c := range r.Changes {
		fmt.Fprintf(&b, "%s|%s|%s|%s|%s;", c.Kind, c.Path, c.Before, c.After, strings.Join(c.Affected, ","))
	}
	for _, a := range r.AffectedCapabilities {
		b.WriteString(a)
		b.WriteByte(',')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func unionKeys(a, b map[string]string) []string {
	set := map[string]bool{}
	for k := range a {
		set[k] = true
	}
	for k := range b {
		set[k] = true
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
func unionBoolKeys(a, b map[string]bool) []string {
	set := map[string]bool{}
	for k := range a {
		set[k] = true
	}
	for k := range b {
		set[k] = true
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
func uniqueSorted(in []string) []string {
	set := map[string]bool{}
	for _, s := range in {
		if s != "" {
			set[s] = true
		}
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
func equalStrings(a, b []string) bool {
	aa, bb := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(aa)
	sort.Strings(bb)
	if len(aa) != len(bb) {
		return false
	}
	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}
	return true
}
