package proofing

import (
	"bytes"
	"encoding/hex"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func providerObservation(t *testing.T, e WorkAuthorizationEvidence, provider, version string, state ProviderState) ProviderObservation {
	t.Helper()
	o, err := NewProviderObservation(ProviderObservation{Tenant: e.Subject.Tenant, Subject: e.Subject, ProviderRef: provider, Version: version, State: state, EvidenceDigest: e.EvidenceDigest, EvidenceRevisionDigest: e.CanonicalDigest})
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func conformRequest(t *testing.T, e WorkAuthorizationEvidence, observations ...ProviderObservation) WorkAuthorizationConformanceRequest {
	t.Helper()
	return WorkAuthorizationConformanceRequest{Evidence: e, Observations: observations, AsOf: proofingDate(t, 2026, time.February, 1), ResultID: "result-1"}
}

func TestWorkAuthorizationConformanceBlocksExpiredUnknownAndAmbiguousProviderState(t *testing.T) {
	e := proofingAuthorization(t)
	verified := providerObservation(t, e, "registry", "2026-09", ProviderVerified)
	got, err := ConformWorkAuthorization(conformRequest(t, e, verified))
	if err != nil || got.Decision != DecisionAuthorized || len(got.ProviderBindingDigests) != 1 || got.ProviderBindingDigests[0] != verified.CanonicalDigest {
		t.Fatalf("authorized=%+v err=%v", got, err)
	}
	for _, tc := range []struct {
		name  string
		state ProviderState
		asOf  values.LocalDate
		want  AuthorizationDecision
		cause error
	}{
		{"expired evidence", ProviderVerified, proofingDate(t, 2027, time.January, 1), DecisionExpired, ErrAuthorizationExpired},
		{"expired provider", ProviderExpired, proofingDate(t, 2026, time.February, 1), DecisionExpired, ErrAuthorizationExpired},
		{"unknown", ProviderUnknown, proofingDate(t, 2026, time.February, 1), DecisionUnknown, ErrAuthorizationUnknown},
		{"ambiguous", ProviderAmbiguous, proofingDate(t, 2026, time.February, 1), DecisionUnknown, ErrProviderAmbiguous},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := conformRequest(t, e, providerObservation(t, e, "registry", "v1", tc.state))
			req.AsOf = tc.asOf
			result, err := ConformWorkAuthorization(req)
			if result.Decision != tc.want || !errors.Is(err, ErrConformanceRejected) || !errors.Is(err, tc.cause) {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
	missing, err := ConformWorkAuthorization(conformRequest(t, e))
	if missing.Decision != DecisionUnknown || !errors.Is(err, ErrAuthorizationUnknown) {
		t.Fatalf("missing observation certified: %+v %v", missing, err)
	}
}

func TestWorkAuthorizationConformanceEnforcesEffectiveDateBoundaries(t *testing.T) {
	e := proofingAuthorization(t)
	o := providerObservation(t, e, "registry", "v1", ProviderVerified)
	for _, tc := range []struct {
		name     string
		asOf     values.LocalDate
		decision AuthorizationDecision
		cause    error
	}{
		{"day before", proofingDate(t, 2025, time.December, 31), DecisionUnknown, ErrAuthorizationNotEffective},
		{"exact start", e.ValidFrom, DecisionAuthorized, nil},
		{"exact end", e.ValidUntil, DecisionExpired, ErrAuthorizationExpired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := conformRequest(t, e, o)
			req.AsOf = tc.asOf
			result, err := ConformWorkAuthorization(req)
			if result.Decision != tc.decision {
				t.Fatalf("decision=%s want=%s err=%v", result.Decision, tc.decision, err)
			}
			if tc.cause == nil && err != nil {
				t.Fatalf("unexpected boundary error: %v", err)
			}
			if tc.cause != nil && (!errors.Is(err, ErrConformanceRejected) || !errors.Is(err, tc.cause)) {
				t.Fatalf("boundary error=%v want=%v", err, tc.cause)
			}
		})
	}
}

func TestTodo_PROOF_002_Property(t *testing.T) {
	e, old := proofingAuthorization(t), proofingAuthorization(t)
	for _, tc := range []struct{ digest, source string }{{e.EvidenceDigest, "fresh-source"}, {"sha256:fresh", e.SourceRef}} {
		if _, err := e.Renew(proofingDate(t, 2027, time.January, 1), proofingDate(t, 2028, time.January, 1), proofingDate(t, 2027, time.October, 1), tc.digest, tc.source); !errors.Is(err, ErrRevisionLineage) {
			t.Fatalf("non-fresh renewal accepted: %v", err)
		}
	}
	if e != old {
		t.Fatal("failed renewal mutated prior evidence")
	}
}

func TestTodo_PROOF_002_Golden(t *testing.T) {
	e := proofingAuthorization(t)
	o := providerObservation(t, e, "registry", "v1", ProviderVerified)
	want := "0724736368656d612c68636d6e6578742e646f6d61696e732e70726f6f66696e672e50726f76696465724f62736572766174696f6e0f24736368656d615f76657273696f6e01020674656e616e740874656e616e742d31077375626a6563743c657265663a76313a74656e616e742d313a776f726b65723a30303030303030302d303030302d343030302d383030302d3030303030303030303030310c70726f76696465725f7265660872656769737472790776657273696f6e0276310573746174650856455249464945440f65766964656e63655f646967657374127368613235363a776f726b2d7065726d69741865766964656e63655f7265766973696f6e5f646967657374477368613235363a37646465383037633036353864653634643937373865393163633436326266363264393637323063646132363136636536303832643936646533636332356536"
	if got := hex.EncodeToString(o.body()); got != want {
		t.Fatalf("provider binding golden drift\n got %s\nwant %s", got, want)
	}
}

func TestTodo_PROOF_002_Race(t *testing.T) {
	e := proofingAuthorization(t)
	o := providerObservation(t, e, "registry", "v1", ProviderVerified)
	req := conformRequest(t, e, o)
	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := ConformWorkAuthorization(req)
			if err != nil || result.Decision != DecisionAuthorized || result.ProviderBindingDigests[0] != o.CanonicalDigest {
				errs <- errors.New("non-deterministic conformance")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestTodo_PROOF_002_Fault(t *testing.T) {
	result, err := ConformWorkAuthorization(conformRequest(t, proofingAuthorization(t)))
	if result.Decision != DecisionUnknown || !errors.Is(err, ErrAuthorizationUnknown) {
		t.Fatalf("missing provider state did not fail closed: %+v %v", result, err)
	}
}

func TestTodo_PROOF_002_Security(t *testing.T) {
	e := proofingAuthorization(t)
	for _, mutate := range []func(*ProviderObservation){func(o *ProviderObservation) { o.Tenant = values.TenantId("other") }, func(o *ProviderObservation) { o.Subject.Id = "other-worker" }, func(o *ProviderObservation) { o.Version = "v2" }, func(o *ProviderObservation) { o.EvidenceRevisionDigest = "sha256:other" }} {
		o := providerObservation(t, e, "registry", "v1", ProviderVerified)
		mutate(&o)
		if _, err := ConformWorkAuthorization(conformRequest(t, e, o)); !errors.Is(err, ErrProviderBinding) {
			t.Fatalf("rebound provider receipt accepted: %v", err)
		}
	}
}

func TestTodo_PROOF_002_Conformance(t *testing.T) {
	e := proofingAuthorization(t)
	left := providerObservation(t, e, "registry-a", "v1", ProviderVerified)
	right := providerObservation(t, e, "registry-b", "v4", ProviderRejected)
	result, err := ConformWorkAuthorization(conformRequest(t, e, left, right))
	if result.Decision != DecisionUnknown || result.ProviderState != ProviderAmbiguous || len(result.ProviderBindingDigests) != 2 || !errors.Is(err, ErrProviderAmbiguous) {
		t.Fatalf("contradictory observations certified: %+v %v", result, err)
	}
}

func TestWorkAuthorizationCorrectionDoesNotRewriteHistoricalEligibility(t *testing.T) {
	e := proofingAuthorization(t)
	original, err := ConformWorkAuthorization(conformRequest(t, e, providerObservation(t, e, "registry", "v1", ProviderVerified)))
	if err != nil || original.Decision != DecisionAuthorized {
		t.Fatalf("original result=%+v err=%v", original, err)
	}
	originalBytes := append([]byte(nil), original.WorkAuthorizationResult.Canonical()...)
	corrected, err := e.Successor(WorkAuthorizationEvidence{
		EvidenceID: e.EvidenceID, Subject: e.Subject, Revision: 2, SupersedesRevision: 1,
		DocumentClass: e.DocumentClass, VerificationMethod: e.VerificationMethod,
		Jurisdiction: e.Jurisdiction, Category: e.Category,
		ValidFrom: proofingDate(t, 2026, time.March, 1), ValidUntil: e.ValidUntil,
		ReverificationDue: e.ReverificationDue, EvidenceDigest: "sha256:corrected", SourceRef: "correction-source-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	correctedResult, correctedErr := ConformWorkAuthorization(conformRequest(t, corrected, providerObservation(t, corrected, "registry", "v2", ProviderVerified)))
	if correctedResult.Decision != DecisionUnknown || !errors.Is(correctedErr, ErrAuthorizationNotEffective) {
		t.Fatalf("corrected result=%+v err=%v", correctedResult, correctedErr)
	}
	if original.Decision != DecisionAuthorized || original.EvidenceRevisionDigest != e.CanonicalDigest || !bytes.Equal(original.WorkAuthorizationResult.Canonical(), originalBytes) {
		t.Fatal("correction rewrote the historical eligibility result")
	}
}

func TestTodo_PROOF_002_Mutation(t *testing.T) {
	e := proofingAuthorization(t)
	originalObservation := providerObservation(t, e, "registry", "v1", ProviderVerified)
	historical, historicalErr := ConformWorkAuthorization(conformRequest(t, e, originalObservation))
	if historicalErr != nil {
		t.Fatal(historicalErr)
	}
	historicalBytes := append([]byte(nil), historical.WorkAuthorizationResult.Canonical()...)
	oldCanonical := append([]byte(nil), e.Canonical()...)
	child, err := e.Renew(proofingDate(t, 2027, time.January, 1), proofingDate(t, 2028, time.January, 1), proofingDate(t, 2027, time.October, 1), "sha256:new", "source-2")
	if err != nil || e.Revision != 1 || child.Revision != 2 || child.SupersedesRevision != e.Revision || bytes.Equal(e.Canonical(), child.Canonical()) || !bytes.Equal(e.Canonical(), oldCanonical) || !bytes.Equal(historical.WorkAuthorizationResult.Canonical(), historicalBytes) || historical.EvidenceRevisionDigest != e.CanonicalDigest {
		t.Fatalf("renewal did not append cleanly: old=%+v child=%+v err=%v", e, child, err)
	}
}
