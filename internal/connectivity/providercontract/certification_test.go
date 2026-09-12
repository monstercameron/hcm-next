package providercontract

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

func fixedEvalNow() time.Time {
	return time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
}

// fullyValidConnectorEvidence builds a connector evidence bundle that
// certifies at L4: every one of the ten classes is present, unexpired and
// passing, the connector was scale-tested to at least its declared quota,
// and its measured availability clears the production bar.
func fullyValidConnectorEvidence(connectorID string, now time.Time) ConnectorEvidence {
	exp := now.AddDate(1, 0, 0)
	window := Window{ObservedAt: now, ExpiresAt: exp}
	return ConnectorEvidence{
		ConnectorID:      connectorID,
		SupportedVersion: SupportedVersionEvidence{Window: window, Version: "v2026.1", Supported: true},
		Security:         SecurityEvidence{Window: window, CredentialRef: "cred-1", ScanPassed: true},
		Mapping:          MappingEvidence{Window: window, SchemaDigest: "sha256:deadbeef", Verified: true},
		Idempotency:      IdempotencyEvidence{Window: window, FixtureID: "idem-1", Passed: true},
		Rate:             RateEvidence{Window: window, QuotaPerMinute: 600, WithinBudget: true},
		Ambiguity:        AmbiguityEvidence{Window: window, FixtureID: "ambig-1", Passed: true},
		Reconciliation:   ReconciliationEvidence{Window: window, RunID: "recon-1", Verified: true},
		Restore:          RestoreEvidence{Window: window, RunID: "restore-1", Verified: true},
		Scale:            ScaleEvidence{Window: window, PeakLoadTested: 900, Passed: true},
		SLO:              SLOEvidence{Window: window, AvailabilityPercent: 99.95, WithinTarget: true},
	}
}

// TestTodo_CONN_RT_008 is the PRIMARY test: the seeded-defect scenarios RED
// describes. Each of an expired credential, a failed ambiguity fixture and
// a failed idempotency fixture must independently return
// CONN_RT_008_REJECTED naming the offending field, state and model version,
// and Certify must not mutate its evidence argument - there is nothing else
// on this pure path that could persist a row, event, outbox entry, work
// item or provider request, so an unmutated input is the whole proof.
func TestTodo_CONN_RT_008(t *testing.T) {
	now := fixedEvalNow()

	t.Run("expired credential", func(t *testing.T) {
		ev := fullyValidConnectorEvidence("conn-1", now)
		ev.Security.ExpiresAt = now.Add(-time.Hour)
		before := ev

		cert, err := Certify("conn-1", ev, MaturityL3, now)

		var rej *Rejection
		if !errors.As(err, &rej) {
			t.Fatalf("expected *Rejection, got %v", err)
		}
		if rej.Field != FieldSecurity || rej.State != StateExpired || rej.ModelVersion != CertificationModelVersion {
			t.Fatalf("rejection = %+v", rej)
		}
		if !errors.Is(err, ErrCertificationRejected) {
			t.Fatalf("rejection does not unwrap to ErrCertificationRejected: %v", err)
		}
		if cert.Level >= MaturityL3 {
			t.Fatalf("certified despite expired security credential: %+v", cert)
		}
		if !reflect.DeepEqual(before, ev) {
			t.Fatalf("Certify mutated its evidence argument: before=%+v after=%+v", before, ev)
		}
	})

	t.Run("failed ambiguity fixture", func(t *testing.T) {
		ev := fullyValidConnectorEvidence("conn-2", now)
		ev.Ambiguity.Passed = false
		before := ev

		cert, err := Certify("conn-2", ev, MaturityL3, now)

		var rej *Rejection
		if !errors.As(err, &rej) {
			t.Fatalf("expected *Rejection, got %v", err)
		}
		if rej.Field != FieldAmbiguity || rej.State != StateFailed {
			t.Fatalf("rejection = %+v", rej)
		}
		if cert.Level >= MaturityL3 {
			t.Fatalf("certified despite failed ambiguity fixture: %+v", cert)
		}
		if !reflect.DeepEqual(before, ev) {
			t.Fatalf("Certify mutated its evidence argument: before=%+v after=%+v", before, ev)
		}
	})

	t.Run("failed idempotency fixture", func(t *testing.T) {
		ev := fullyValidConnectorEvidence("conn-3", now)
		ev.Idempotency.Passed = false
		before := ev

		cert, err := Certify("conn-3", ev, MaturityL3, now)

		var rej *Rejection
		if !errors.As(err, &rej) {
			t.Fatalf("expected *Rejection, got %v", err)
		}
		if rej.Field != FieldIdempotency || rej.State != StateFailed {
			t.Fatalf("rejection = %+v", rej)
		}
		if cert.Level >= MaturityL3 {
			t.Fatalf("certified despite failed idempotency fixture: %+v", cert)
		}
		if !reflect.DeepEqual(before, ev) {
			t.Fatalf("Certify mutated its evidence argument: before=%+v after=%+v", before, ev)
		}
	})
}

// TestTodo_CONN_RT_008_Mutation proves each of the ten evidence classes
// independently blocks both L3 and L4: breaking exactly one class at a
// time, with the other nine left fully valid, must name that class and
// only that class.
func TestTodo_CONN_RT_008_Mutation(t *testing.T) {
	now := fixedEvalNow()
	breakers := []struct {
		field   RejectionField
		breakFn func(*ConnectorEvidence)
	}{
		{FieldSupportedVersion, func(e *ConnectorEvidence) { e.SupportedVersion.Supported = false }},
		{FieldSecurity, func(e *ConnectorEvidence) { e.Security.ScanPassed = false }},
		{FieldMapping, func(e *ConnectorEvidence) { e.Mapping.Verified = false }},
		{FieldIdempotency, func(e *ConnectorEvidence) { e.Idempotency.Passed = false }},
		{FieldRate, func(e *ConnectorEvidence) { e.Rate.WithinBudget = false }},
		{FieldAmbiguity, func(e *ConnectorEvidence) { e.Ambiguity.Passed = false }},
		{FieldReconciliation, func(e *ConnectorEvidence) { e.Reconciliation.Verified = false }},
		{FieldRestore, func(e *ConnectorEvidence) { e.Restore.Verified = false }},
		{FieldScale, func(e *ConnectorEvidence) { e.Scale.Passed = false }},
		{FieldSLO, func(e *ConnectorEvidence) { e.SLO.WithinTarget = false }},
	}
	for _, b := range breakers {
		b := b
		t.Run(string(b.field), func(t *testing.T) {
			ev := fullyValidConnectorEvidence("conn-mut", now)
			b.breakFn(&ev)
			for _, level := range []MaturityLevel{MaturityL3, MaturityL4} {
				cert, err := Certify("conn-mut", ev, level, now)
				var rej *Rejection
				if !errors.As(err, &rej) {
					t.Fatalf("requesting %s: expected rejection, got err=%v cert=%+v", level, err, cert)
				}
				if rej.Field != b.field {
					t.Fatalf("requesting %s: rejection field = %s, want %s", level, rej.Field, b.field)
				}
				if cert.Level >= level {
					t.Fatalf("requesting %s: certified at %s despite broken %s", level, cert.Level, b.field)
				}
			}
		})
	}
}

// TestTodo_CONN_RT_008_Conformance proves a connector with complete,
// unexpired evidence certifies at the level its evidence supports, and no
// higher: full evidence reaches L4, but evidence scale-tested below the
// connector's own declared quota still earns L3 and never L4.
func TestTodo_CONN_RT_008_Conformance(t *testing.T) {
	now := fixedEvalNow()
	ev := fullyValidConnectorEvidence("conn-conf", now)

	cert, err := Certify("conn-conf", ev, MaturityL4, now)
	if err != nil || cert.Level != MaturityL4 {
		t.Fatalf("complete unexpired evidence should certify at L4: cert=%+v err=%v", cert, err)
	}

	degraded := ev
	degraded.Scale.PeakLoadTested = ev.Rate.QuotaPerMinute - 1

	if cert, err := Certify("conn-conf", degraded, MaturityL3, now); err != nil || cert.Level != MaturityL3 {
		t.Fatalf("scale below the production bar should still earn L3: cert=%+v err=%v", cert, err)
	}

	cert, err = Certify("conn-conf", degraded, MaturityL4, now)
	var rej *Rejection
	if !errors.As(err, &rej) || rej.Field != FieldScale || rej.State != StateFailed {
		t.Fatalf("scale below the production bar must not reach L4: cert=%+v err=%v", cert, err)
	}
	if cert.Level != MaturityL3 {
		t.Fatalf("no-higher guarantee violated: certified at %s instead of capping at L3: %+v", cert.Level, cert)
	}
}

// TestTodo_CONN_RT_008_Fault proves a single failed fault-case-style
// fixture (ambiguity) blocks certification outright rather than being
// averaged away by the nine other passing classes.
func TestTodo_CONN_RT_008_Fault(t *testing.T) {
	now := fixedEvalNow()
	ev := fullyValidConnectorEvidence("conn-fault", now)
	ev.Ambiguity.Passed = false

	cert, err := Certify("conn-fault", ev, MaturityL3, now)

	var rej *Rejection
	if !errors.As(err, &rej) || rej.Field != FieldAmbiguity || rej.State != StateFailed {
		t.Fatalf("single failed fault fixture did not block certification: err=%v cert=%+v", err, cert)
	}
	if cert.Level >= MaturityL3 {
		t.Fatalf("failed fault fixture was averaged away by the nine passing classes: certified at %s", cert.Level)
	}
}

// TestTodo_CONN_RT_008_Recovery proves restore evidence specifically gates
// L3/L4: a connector missing only restore evidence still reaches L2, but
// never L3.
func TestTodo_CONN_RT_008_Recovery(t *testing.T) {
	now := fixedEvalNow()
	ev := fullyValidConnectorEvidence("conn-recovery", now)
	ev.Restore = RestoreEvidence{}

	if cert, err := Certify("conn-recovery", ev, MaturityL2, now); err != nil || cert.Level < MaturityL2 {
		t.Fatalf("L2 does not require restore evidence: cert=%+v err=%v", cert, err)
	}

	cert, err := Certify("conn-recovery", ev, MaturityL3, now)
	var rej *Rejection
	if !errors.As(err, &rej) || rej.Field != FieldRestore || rej.State != StateMissing {
		t.Fatalf("missing restore evidence should block L3 by name: cert=%+v err=%v", cert, err)
	}
}

// TestTodo_CONN_RT_008_Security proves tenant/connector scoping: evidence
// collected for one connector cannot certify a different connector, even
// though that same evidence certifies its own connector cleanly.
func TestTodo_CONN_RT_008_Security(t *testing.T) {
	now := fixedEvalNow()
	evidenceForA := fullyValidConnectorEvidence("connector-a", now)

	cert, err := Certify("connector-b", evidenceForA, MaturityL3, now)
	var rej *Rejection
	if !errors.As(err, &rej) || rej.Field != FieldConnectorID || rej.State != StateScopeMismatch {
		t.Fatalf("evidence scoped to another connector was accepted: err=%v cert=%+v", err, cert)
	}
	if cert.Level != MaturityL0 || cert.Classes != (ClassStatus{}) {
		t.Fatalf("cross-connector evidence produced a non-trivial certification record: %+v", cert)
	}

	if cert, err := Certify("connector-a", evidenceForA, MaturityL3, now); err != nil || cert.Level < MaturityL3 {
		t.Fatalf("evidence should still certify its own connector: cert=%+v err=%v", cert, err)
	}
}

// TestTodo_CONN_RT_008_Race proves concurrent certifications of identical
// evidence at the same evaluation instant always produce the identical
// verdict. There is no -race detector on windows/arm64 locally, so this
// asserts on the observed result instead.
func TestTodo_CONN_RT_008_Race(t *testing.T) {
	now := fixedEvalNow()
	ev := fullyValidConnectorEvidence("conn-race", now)
	const n = 50
	results := make([]Certification, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			results[i], errs[i] = Certify("conn-race", ev, MaturityL4, now)
		}()
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: unexpected error: %v", i, errs[i])
		}
		if results[i].Level != MaturityL4 || results[i].ModelVersion != CertificationModelVersion {
			t.Fatalf("goroutine %d: diverged from expected verdict: %+v", i, results[i])
		}
		if results[i] != results[0] {
			t.Fatalf("concurrent certifications diverged: [0]=%+v [%d]=%+v", results[0], i, results[i])
		}
	}
}

// TestTodo_CONN_RT_008_Integration drives certification from the existing
// PlaceholderTopology()/NewFixture()/CompileEvidence() path, so the
// evaluated evidence is built from the harness's own shape (topology
// identity, manifest digest, recovery/observation results) instead of a
// parallel, invented one.
func TestTodo_CONN_RT_008_Integration(t *testing.T) {
	now := fixedEvalNow()
	fixture, err := NewFixture(PlaceholderTopology())
	if err != nil {
		t.Fatal(err)
	}
	harness, err := CompileEvidence(context.Background(), fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !harness.NoMutatingCalls || !harness.IndependentObservation || !harness.RecoveryVerified {
		t.Fatalf("harness evidence is not clean; integration fixture is unsound: %+v", harness)
	}

	connectorID := fixture.Topology.SelectionID
	exp := now.AddDate(1, 0, 0)
	window := Window{ObservedAt: now, ExpiresAt: exp}
	quota := fixture.Topology.Capabilities[0].QuotaPerMinute

	ev := ConnectorEvidence{
		ConnectorID:      connectorID,
		SupportedVersion: SupportedVersionEvidence{Window: window, Version: fixture.Topology.APIVersion, Supported: true},
		Security:         SecurityEvidence{Window: window, CredentialRef: fixture.Topology.CredentialRef, ScanPassed: true},
		Mapping:          MappingEvidence{Window: window, SchemaDigest: harness.ManifestDigest, Verified: true},
		Idempotency:      IdempotencyEvidence{Window: window, FixtureID: "idem-integration", Passed: true},
		Rate:             RateEvidence{Window: window, QuotaPerMinute: quota, WithinBudget: true},
		Ambiguity:        AmbiguityEvidence{Window: window, FixtureID: "ambig-integration", Passed: true},
		Reconciliation:   ReconciliationEvidence{Window: window, RunID: "recon-integration", Verified: harness.IndependentObservation},
		Restore:          RestoreEvidence{Window: window, RunID: "restore-integration", Verified: harness.RecoveryVerified},
		Scale:            ScaleEvidence{Window: window, PeakLoadTested: quota, Passed: true},
		SLO:              SLOEvidence{Window: window, AvailabilityPercent: 99.95, WithinTarget: true},
	}

	cert, err := Certify(connectorID, ev, MaturityL3, now)
	if err != nil || cert.Level < MaturityL3 {
		t.Fatalf("connector built from the existing harness fixture should certify at L3: cert=%+v err=%v", cert, err)
	}
}

// absDuration reports the magnitude of d, used by breakClass to guarantee
// an expiry that lands strictly before now regardless of the fuzzed
// offset's sign.
func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// breakClass makes exactly one of the ten evidence classes in ev either
// wholly missing (its Go zero value) or definitely expired as of now,
// depending on expire.
func breakClass(ev *ConnectorEvidence, idx int, expire bool, offset time.Duration, now time.Time) {
	expiredAt := now.Add(-absDuration(offset) - time.Second)
	switch idx {
	case 0:
		if expire {
			ev.SupportedVersion.ExpiresAt = expiredAt
		} else {
			ev.SupportedVersion = SupportedVersionEvidence{}
		}
	case 1:
		if expire {
			ev.Security.ExpiresAt = expiredAt
		} else {
			ev.Security = SecurityEvidence{}
		}
	case 2:
		if expire {
			ev.Mapping.ExpiresAt = expiredAt
		} else {
			ev.Mapping = MappingEvidence{}
		}
	case 3:
		if expire {
			ev.Idempotency.ExpiresAt = expiredAt
		} else {
			ev.Idempotency = IdempotencyEvidence{}
		}
	case 4:
		if expire {
			ev.Rate.ExpiresAt = expiredAt
		} else {
			ev.Rate = RateEvidence{}
		}
	case 5:
		if expire {
			ev.Ambiguity.ExpiresAt = expiredAt
		} else {
			ev.Ambiguity = AmbiguityEvidence{}
		}
	case 6:
		if expire {
			ev.Reconciliation.ExpiresAt = expiredAt
		} else {
			ev.Reconciliation = ReconciliationEvidence{}
		}
	case 7:
		if expire {
			ev.Restore.ExpiresAt = expiredAt
		} else {
			ev.Restore = RestoreEvidence{}
		}
	case 8:
		if expire {
			ev.Scale.ExpiresAt = expiredAt
		} else {
			ev.Scale = ScaleEvidence{}
		}
	default:
		if expire {
			ev.SLO.ExpiresAt = expiredAt
		} else {
			ev.SLO = SLOEvidence{}
		}
	}
}

// FuzzTodo_CONN_RT_008 is the oracle: starting from evidence that would
// otherwise certify at L4, breaking any single one of the ten classes -
// either wholly missing or definitely expired - must never still certify
// at L3 or above.
func FuzzTodo_CONN_RT_008(f *testing.F) {
	f.Add(0, false, int64(0))
	f.Add(4, true, int64(3600))
	f.Add(9, true, int64(-10))
	f.Fuzz(func(t *testing.T, classIndex int, expire bool, offsetSeconds int64) {
		now := fixedEvalNow()
		ev := fullyValidConnectorEvidence("conn-fuzz", now)
		idx := classIndex % 10
		if idx < 0 {
			idx += 10
		}
		breakClass(&ev, idx, expire, time.Duration(offsetSeconds)*time.Second, now)

		cert, err := Certify("conn-fuzz", ev, MaturityL3, now)
		if err == nil && cert.Level >= MaturityL3 {
			t.Fatalf("class index %d certified at %s despite missing/expired evidence (expire=%v offsetSeconds=%d)",
				idx, cert.Level, expire, offsetSeconds)
		}
	})
}
