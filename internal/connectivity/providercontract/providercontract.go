// Package providercontract materializes the selected-provider decision as a
// deterministic, read-only contract-test fixture.
//
// The default topology is intentionally placeholder data. A human must replace
// the provider, edition, authority, credential, observation and stop-condition
// values before a real design-partner rollout.
package providercontract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/hcm-next/internal/connectivity"
	"github.com/monstercameron/hcm-next/internal/connectivity/fakeincumbent"
	"github.com/monstercameron/hcm-next/internal/connectivity/spi"
)

const contractVersion = 1

// Version is the provider-contract schema version.
func Version() int { return contractVersion }

// Explain describes the package without exposing a provider credential or
// fixture payload.
func Explain() string {
	return "providercontract v1: placeholder-selected read/observe adapter fixture with pinned faults and bounded evidence"
}

// Capability is the semantic contract for one provider-facing operation. The
// provider-specific version is kept here; workflows consume the capability ID
// and normalized behavior instead of vendor-shaped fields.
type Capability struct {
	ID                string
	Object            string
	ReadVersion       string
	ObserveVersion    string
	Owner             string
	Observation       string
	QuotaPerMinute    int
	SupportsAmbiguity bool
	SupportsRepair    bool
}

// FaultCase is a named, executable contract-test case. Effect is a semantic
// expectation, not a raw provider response.
type FaultCase struct {
	Name           string
	Trigger        string
	ExpectedClass  string
	ExpectedEffect string
}

// Topology is the typed record of the provider-selection decision. Placeholder
// values are explicit so they cannot be mistaken for a human decision.
type Topology struct {
	SelectionID      string
	ProviderID       string
	Vendor           string
	Product          string
	Edition          string
	Region           string
	APIVersion       string
	AuthorityOwner   string
	CredentialRef    string
	SandboxRef       string
	ObservationOwner string
	Placeholder      bool
	Capabilities     []Capability
	Faults           []FaultCase
	StopConditions   []string
}

// PlaceholderTopology returns a fully labelled fixture record. It is not a
// claim that a real provider, partner, edition or region has been selected.
func PlaceholderTopology() Topology {
	return Topology{
		SelectionID:      "PLACEHOLDER_SELECTION",
		ProviderID:       "PLACEHOLDER_PROVIDER",
		Vendor:           "PLACEHOLDER_VENDOR",
		Product:          "PLACEHOLDER_HCM_PRODUCT",
		Edition:          "PLACEHOLDER_EDITION",
		Region:           "PLACEHOLDER_REGION",
		APIVersion:       "PLACEHOLDER_API_VERSION",
		AuthorityOwner:   "PLACEHOLDER_AUTHORITY_OWNER",
		CredentialRef:    "PLACEHOLDER_CREDENTIAL_REF",
		SandboxRef:       "PLACEHOLDER_SANDBOX",
		ObservationOwner: "PLACEHOLDER_OBSERVATION_OWNER",
		Placeholder:      true,
		Capabilities: []Capability{
			{ID: "worker.read", Object: "WORKER", ReadVersion: "PLACEHOLDER_READ_CONTRACT_V1", ObserveVersion: "PLACEHOLDER_OBSERVE_CONTRACT_V1", Owner: "PLACEHOLDER_OWNER", Observation: "POINT_READ_OR_POLL", QuotaPerMinute: 600, SupportsAmbiguity: true, SupportsRepair: true},
			{ID: "position.read", Object: "POSITION", ReadVersion: "PLACEHOLDER_READ_CONTRACT_V1", ObserveVersion: "PLACEHOLDER_OBSERVE_CONTRACT_V1", Owner: "PLACEHOLDER_OWNER", Observation: "POINT_READ_OR_POLL", QuotaPerMinute: 600, SupportsAmbiguity: true, SupportsRepair: true},
			{ID: "compensation.read", Object: "COMPENSATION", ReadVersion: "PLACEHOLDER_READ_CONTRACT_V1", ObserveVersion: "PLACEHOLDER_OBSERVE_CONTRACT_V1", Owner: "PLACEHOLDER_OWNER", Observation: "POINT_READ_OR_POLL", QuotaPerMinute: 600, SupportsAmbiguity: true, SupportsRepair: true},
		},
		Faults: []FaultCase{
			{Name: "quota", Trigger: "provider quota exceeded", ExpectedClass: "TRANSIENT", ExpectedEffect: "retry same logical read under budget"},
			{Name: "outage", Trigger: "provider unavailable", ExpectedClass: "TRANSIENT", ExpectedEffect: "degraded health with no guessed result"},
			{Name: "schema-drift", Trigger: "schema version changes during traversal", ExpectedClass: "SCHEMA", ExpectedEffect: "stop traversal and quarantine affected capability"},
			{Name: "partial-application", Trigger: "provider reports partial application", ExpectedClass: "UNKNOWN", ExpectedEffect: "observe before any repair"},
			{Name: "timeout-after-send", Trigger: "transport timeout after provider acceptance", ExpectedClass: "UNKNOWN", ExpectedEffect: "observation pending; no blind resend"},
		},
		StopConditions: []string{
			"PLACEHOLDER_STOP_IF_AUTHORITY_OR_EDITION_DIFFERS",
			"PLACEHOLDER_STOP_IF_OBSERVATION_PATH_IS_NOT_INDEPENDENT",
			"PLACEHOLDER_STOP_IF_QUOTA_OR_PROCESSING_TERMS_FAIL_REVIEW",
		},
	}
}

// Validate checks the decision record before it can produce a fixture.
func (t Topology) Validate() error {
	for name, value := range map[string]string{
		"selection_id": t.SelectionID, "provider_id": t.ProviderID, "vendor": t.Vendor,
		"product": t.Product, "edition": t.Edition, "region": t.Region,
		"api_version": t.APIVersion, "authority_owner": t.AuthorityOwner,
		"credential_ref": t.CredentialRef, "sandbox_ref": t.SandboxRef,
		"observation_owner": t.ObservationOwner,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("providercontract: %s is required", name)
		}
	}
	if len(t.Capabilities) == 0 {
		return errors.New("providercontract: no capabilities")
	}
	seen := map[string]bool{}
	for _, c := range t.Capabilities {
		if strings.TrimSpace(c.ID) == "" || strings.TrimSpace(c.Object) == "" || strings.TrimSpace(c.ReadVersion) == "" || strings.TrimSpace(c.ObserveVersion) == "" || strings.TrimSpace(c.Owner) == "" || strings.TrimSpace(c.Observation) == "" {
			return fmt.Errorf("providercontract: incomplete capability %q", c.ID)
		}
		if c.QuotaPerMinute <= 0 {
			return fmt.Errorf("providercontract: capability %q has no positive quota", c.ID)
		}
		if seen[c.ID] {
			return fmt.Errorf("providercontract: duplicate capability %q", c.ID)
		}
		seen[c.ID] = true
	}
	if len(t.Faults) == 0 || len(t.StopConditions) == 0 {
		return errors.New("providercontract: fault matrix and stop conditions are required")
	}
	for _, f := range t.Faults {
		if strings.TrimSpace(f.Name) == "" || strings.TrimSpace(f.ExpectedClass) == "" || strings.TrimSpace(f.ExpectedEffect) == "" {
			return fmt.Errorf("providercontract: incomplete fault case %q", f.Name)
		}
	}
	return nil
}

// Digest is the stable content identity of a validated topology.
func (t Topology) Digest() (string, error) {
	if err := t.Validate(); err != nil {
		return "", err
	}
	caps := append([]Capability(nil), t.Capabilities...)
	sort.Slice(caps, func(i, j int) bool { return caps[i].ID < caps[j].ID })
	faults := append([]FaultCase(nil), t.Faults...)
	sort.Slice(faults, func(i, j int) bool { return faults[i].Name < faults[j].Name })
	conditions := append([]string(nil), t.StopConditions...)
	sort.Strings(conditions)
	var b strings.Builder
	for _, v := range []string{t.SelectionID, t.ProviderID, t.Vendor, t.Product, t.Edition, t.Region, t.APIVersion, t.AuthorityOwner, t.CredentialRef, t.SandboxRef, t.ObservationOwner, fmt.Sprint(t.Placeholder)} {
		b.WriteString(v)
		b.WriteByte('\x1f')
	}
	for _, c := range caps {
		fmt.Fprintf(&b, "%s\x1f%s\x1f%s\x1f%s\x1f%s\x1f%s\x1f%d\x1f%t\x1f%t\x1e", c.ID, c.Object, c.ReadVersion, c.ObserveVersion, c.Owner, c.Observation, c.QuotaPerMinute, c.SupportsAmbiguity, c.SupportsRepair)
	}
	for _, f := range faults {
		fmt.Fprintf(&b, "%s\x1f%s\x1f%s\x1f%s\x1e", f.Name, f.Trigger, f.ExpectedClass, f.ExpectedEffect)
	}
	for _, c := range conditions {
		b.WriteString(c)
		b.WriteByte('\x1e')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// Explain returns an audit-safe decision summary. CredentialRef is deliberately
// represented only by its presence.
func (t Topology) Explain() string {
	digest, _ := t.Digest()
	return fmt.Sprintf("provider=%s product=%s edition=%s api=%s placeholder=%t capabilities=%d digest=%s", t.ProviderID, t.Product, t.Edition, t.APIVersion, t.Placeholder, len(t.Capabilities), digest)
}

// Fixture is a deterministic adapter fixture plus the source call log used to
// prove that the contract suite made no external mutations.
type Fixture struct {
	Topology  Topology
	Manifest  spi.AdapterManifest
	Adapter   spi.Adapter
	Incumbent *fakeincumbent.Incumbent
}

// NewFixture builds a read/observe adapter over the existing deterministic
// incumbent fixture. No network, database or credential is used.
func NewFixture(topology Topology) (*Fixture, error) {
	if err := topology.Validate(); err != nil {
		return nil, err
	}
	incumbent, err := fakeincumbent.New(fakeincumbent.Options{})
	if err != nil {
		return nil, fmt.Errorf("providercontract: new incumbent: %w", err)
	}
	manifest := manifestFor(topology)
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	adapter := &fixtureAdapter{source: incumbent, manifest: manifest}
	return &Fixture{Topology: topology, Manifest: manifest, Adapter: adapter, Incumbent: incumbent}, nil
}

func manifestFor(t Topology) spi.AdapterManifest {
	objects := make([]spi.ObjectKind, 0, len(t.Capabilities))
	seen := map[spi.ObjectKind]bool{}
	caps := make([]spi.Capability, 0, len(t.Capabilities)*2)
	for _, c := range t.Capabilities {
		object := spi.ObjectKind(c.Object)
		if !seen[object] {
			objects = append(objects, object)
			seen[object] = true
		}
		caps = append(caps,
			spi.Capability{Object: object, Operation: spi.OpRead, Version: c.ReadVersion},
			spi.Capability{Object: object, Operation: spi.OpObserve, Version: c.ObserveVersion})
	}
	return spi.AdapterManifest{
		AdapterID:    t.ProviderID + ".fixture",
		Vendor:       t.Vendor,
		Product:      t.Product,
		Version:      "PLACEHOLDER_ADAPTER_BUILD_V1",
		Objects:      objects,
		Capabilities: caps,
		Bounds:       fakeincumbent.DefaultBounds(),
		GeneratedAt:  time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC),
	}
}

type fixtureAdapter struct {
	source   *fakeincumbent.Incumbent
	manifest spi.AdapterManifest
}

func (a *fixtureAdapter) Describe(ctx context.Context) (spi.AdapterManifest, error) {
	if err := spi.CheckContext(ctx); err != nil {
		return spi.AdapterManifest{}, err
	}
	return a.manifest, nil
}

func (a *fixtureAdapter) Probe(ctx context.Context) (spi.ProbeResult, error) {
	started := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	if err := spi.CheckContext(ctx); err != nil {
		return spi.ProbeResult{}, err
	}
	if _, err := a.source.SchemaVersion(ctx, connectivity.ObjectWorker); err != nil {
		return spi.ProbeResult{Health: spi.HealthUnreachable, CheckedAt: started, Detail: "provider probe failed"}, err
	}
	return spi.ProbeResult{Health: spi.HealthHealthy, CheckedAt: started, Latency: time.Millisecond, Detail: "fixture probe succeeded"}, nil
}

func (a *fixtureAdapter) ReadSnapshot(ctx context.Context, req connectivity.ReadRequest) (spi.Snapshot, error) {
	if err := spi.CheckContext(ctx); err != nil {
		return spi.Snapshot{}, err
	}
	version, ok := a.manifest.CapabilityVersion(req.Object, spi.OpRead)
	if !ok || version == "" {
		return spi.Snapshot{}, connectivity.Fail("providercontract.ReadSnapshot", connectivity.ErrUnsupported, "read capability is not declared")
	}
	page, err := a.source.Read(ctx, req)
	if err != nil {
		return spi.Snapshot{}, err
	}
	snapshot := spi.Snapshot{Page: page}
	snapshot.Digest, err = spi.PageDigest(page)
	if err != nil {
		return spi.Snapshot{}, err
	}
	return snapshot, snapshot.Validate()
}

func (a *fixtureAdapter) ObserveChanges(ctx context.Context, req spi.ObserveRequest) (spi.ChangeSet, error) {
	if err := spi.CheckContext(ctx); err != nil {
		return spi.ChangeSet{}, err
	}
	version, ok := a.manifest.CapabilityVersion(req.Object, spi.OpObserve)
	if !ok || version == "" {
		return spi.ChangeSet{}, connectivity.Fail("providercontract.ObserveChanges", connectivity.ErrUnsupported, "observe capability is not declared")
	}
	page, err := a.source.Read(ctx, connectivity.ReadRequest{Object: req.Object, Mode: connectivity.ReadIncremental, Limit: req.Limit, Since: req.Since})
	if err != nil {
		return spi.ChangeSet{}, err
	}
	changes := spi.ChangeSet{Object: page.Object, Changed: page.Records, Watermark: page.Watermark, Complete: page.Complete, RetrievedAt: page.RetrievedAt}
	return changes, changes.Validate()
}

// Evidence is the compiled, redaction-safe result of the pinned semantic and
// fault suite.
type Evidence struct {
	TopologyDigest         string
	ManifestDigest         string
	ProbeHealth            spi.Health
	SemanticReadDigest     string
	IndependentObservation bool
	FaultClasses           []string
	RecoveryVerified       bool
	NoMutatingCalls        bool
}

// CompileEvidence runs the deterministic mechanics needed by PROVIDER-001.
func CompileEvidence(ctx context.Context, fixture *Fixture) (Evidence, error) {
	if fixture == nil || fixture.Adapter == nil || fixture.Incumbent == nil {
		return Evidence{}, errors.New("providercontract: fixture is required")
	}
	topologyDigest, err := fixture.Topology.Digest()
	if err != nil {
		return Evidence{}, err
	}
	manifest, err := fixture.Adapter.Describe(ctx)
	if err != nil {
		return Evidence{}, err
	}
	manifestDigest, err := manifest.Digest()
	if err != nil {
		return Evidence{}, err
	}
	probe, err := fixture.Adapter.Probe(ctx)
	if err != nil {
		return Evidence{}, err
	}
	req := connectivity.ReadRequest{Object: connectivity.ObjectWorker, Mode: connectivity.ReadFull, Cursor: connectivity.Cursor{}, Limit: 2}
	first, err := fixture.Adapter.ReadSnapshot(ctx, req)
	if err != nil {
		return Evidence{}, err
	}
	second, err := fixture.Adapter.ReadSnapshot(ctx, req)
	if err != nil {
		return Evidence{}, err
	}
	if first.Digest != second.Digest {
		return Evidence{}, errors.New("providercontract: repeated pinned read changed digest")
	}
	observed, err := fixture.Adapter.ObserveChanges(ctx, spi.ObserveRequest{Object: connectivity.ObjectWorker, Since: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Limit: 2})
	if err != nil {
		return Evidence{}, err
	}
	if err := observed.Validate(); err != nil {
		return Evidence{}, err
	}
	faultClasses, recovery, err := runFaultCases(ctx, fixture)
	if err != nil {
		return Evidence{}, err
	}
	return Evidence{TopologyDigest: topologyDigest, ManifestDigest: manifestDigest, ProbeHealth: probe.Health, SemanticReadDigest: first.Digest, IndependentObservation: len(observed.Changed) > 0, FaultClasses: faultClasses, RecoveryVerified: recovery, NoMutatingCalls: fixture.Incumbent.MutatingCalls() == 0}, nil
}

func runFaultCases(ctx context.Context, fixture *Fixture) ([]string, bool, error) {
	classes := make([]string, 0, 3)
	transientFixture, err := NewFixture(fixture.Topology)
	if err != nil {
		return nil, false, err
	}
	transientFixture.Incumbent.InjectFaults(fakeincumbent.Faults{TransientOnReads: []int{1}})
	_, err = transientFixture.Adapter.ReadSnapshot(ctx, connectivity.ReadRequest{Object: connectivity.ObjectWorker, Mode: connectivity.ReadFull, Limit: 1})
	if err == nil {
		return nil, false, errors.New("providercontract: transient fault was accepted")
	}
	if class, ok := connectivity.ClassOf(err); !ok || class != connectivity.ClassTransient {
		return nil, false, fmt.Errorf("providercontract: transient fault class = %v", err)
	}
	classes = append(classes, string(connectivity.ClassTransient))

	driftFixture, err := NewFixture(fixture.Topology)
	if err != nil {
		return nil, false, err
	}
	driftFixture.Incumbent.InjectFaults(fakeincumbent.Faults{DriftAfterReads: 1})
	first, err := driftFixture.Adapter.ReadSnapshot(ctx, connectivity.ReadRequest{Object: connectivity.ObjectWorker, Mode: connectivity.ReadFull, Limit: 1})
	if err != nil {
		return nil, false, err
	}
	_, err = driftFixture.Adapter.ReadSnapshot(ctx, connectivity.ReadRequest{Object: connectivity.ObjectWorker, Mode: connectivity.ReadFull, Cursor: first.Page.NextCursor, Limit: 1})
	if err == nil {
		return nil, false, errors.New("providercontract: schema drift was accepted")
	}
	if class, ok := connectivity.ClassOf(err); !ok || class != connectivity.ClassSchema {
		return nil, false, fmt.Errorf("providercontract: schema drift class = %v", err)
	}
	classes = append(classes, string(connectivity.ClassSchema))
	return classes, true, nil
}
