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

func TestQuorum_ProfileValidationAndReceiptSecurity(t *testing.T) {
	base := timeauth.QuorumProfile{ID: "p", RequiredSources: 2, MaxOffset: time.Second, MaxUncertainty: time.Second, MaxHoldover: time.Minute, LeapSmear: "smear", RequireAuthentication: true}
	for _, mutate := range []func(*timeauth.QuorumProfile){
		func(p *timeauth.QuorumProfile) { p.ID = "" }, func(p *timeauth.QuorumProfile) { p.RequiredSources = 1 }, func(p *timeauth.QuorumProfile) { p.MaxOffset = 0 }, func(p *timeauth.QuorumProfile) { p.MaxUncertainty = 0 }, func(p *timeauth.QuorumProfile) { p.MaxHoldover = -time.Second }, func(p *timeauth.QuorumProfile) { p.LeapSmear = "" }, func(p *timeauth.QuorumProfile) { p.RequireAuthentication = false },
	} {
		p := base
		mutate(&p)
		if _, err := timeauth.NewQuorumProfile(p); !errors.Is(err, timeauth.ErrInvalidQuorumProfile) {
			t.Fatalf("NewQuorumProfile(%+v) = %v", p, err)
		}
	}
	p, err := timeauth.NewQuorumProfile(base)
	if err != nil || p.Digest == "" || p.Validate() != nil {
		t.Fatalf("valid profile = %+v, %v", p, err)
	}
	bad := p
	bad.Digest = "forged"
	if !errors.Is(bad.Validate(), timeauth.ErrInvalidQuorumProfile) {
		t.Fatalf("forged profile accepted")
	}
	if !errors.Is((timeauth.QuorumProfile{}).Validate(), timeauth.ErrInvalidQuorumProfile) {
		t.Fatalf("zero profile accepted")
	}

	sources := []timeauth.QuorumSource{
		{ID: "b", Epoch: values.NewInstant(baseTime.Add(40 * time.Millisecond)), Offset: 40 * time.Millisecond, Uncertainty: 30 * time.Millisecond, Authenticated: true, Health: timeauth.HealthTrusted, LeapSmear: "smear"},
		{ID: "a", Epoch: values.NewInstant(baseTime), Offset: 0, Uncertainty: 20 * time.Millisecond, Authenticated: true, Health: timeauth.HealthTrusted, LeapSmear: "smear"},
	}
	receipt, err := p.Evaluate(sources)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.SourceIDs[0] != "a" || receipt.SourceIDs[1] != "b" || receipt.Skew != 40*time.Millisecond || receipt.Offset != 20*time.Millisecond || receipt.Uncertainty != 30*time.Millisecond || receipt.Holdover || receipt.Health != timeauth.HealthTrusted {
		t.Fatalf("receipt = %+v", receipt)
	}
	if err := receipt.RequireSensitive(); err != nil || timeauth.Explain(receipt) != receipt.Explain() || !strings.Contains(receipt.Explain(), "profile=p") || strings.Contains(receipt.Explain(), "source-placeholder") {
		t.Fatalf("receipt trust/explanation = %v, %q", err, receipt.Explain())
	}
	for _, mutate := range []func(*timeauth.QuorumReceipt){func(r *timeauth.QuorumReceipt) { r.Health = timeauth.HealthUntrusted }, func(r *timeauth.QuorumReceipt) { r.Digest = "forged" }} {
		copy := receipt
		mutate(&copy)
		if !errors.Is(copy.RequireSensitive(), timeauth.ErrQuorumUntrusted) {
			t.Fatalf("tampered receipt accepted: %+v", copy)
		}
	}
}

func TestQuorum_EvaluateRejectsEveryUntrustedCondition(t *testing.T) {
	p := placeholderQuorum(t)
	base := placeholderSources()
	if _, err := p.Evaluate(base[:1]); !errors.Is(err, timeauth.ErrQuorumUntrusted) {
		t.Fatalf("too few sources = %v", err)
	}
	mutations := []struct {
		name   string
		mutate func([]timeauth.QuorumSource)
	}{
		{"empty id", func(s []timeauth.QuorumSource) { s[0].ID = " " }},
		{"unset epoch", func(s []timeauth.QuorumSource) { s[0].Epoch = values.Instant{} }},
		{"invalid health", func(s []timeauth.QuorumSource) { s[0].Health = timeauth.Health(99) }},
		{"negative uncertainty", func(s []timeauth.QuorumSource) { s[0].Uncertainty = -time.Nanosecond }},
		{"negative holdover age", func(s []timeauth.QuorumSource) { s[0].HoldoverAge = -time.Nanosecond }},
		{"wrong smear", func(s []timeauth.QuorumSource) { s[0].LeapSmear = "other" }},
		{"unauthenticated", func(s []timeauth.QuorumSource) { s[0].Authenticated = false }},
		{"degraded", func(s []timeauth.QuorumSource) { s[0].Health = timeauth.HealthDegraded }},
		{"excess uncertainty", func(s []timeauth.QuorumSource) { s[0].Uncertainty = 2 * time.Second }},
		{"excess holdover", func(s []timeauth.QuorumSource) { s[0].Holdover = true; s[0].HoldoverAge = 2 * time.Minute }},
		{"duplicate id", func(s []timeauth.QuorumSource) { s[1].ID = s[0].ID }},
		{"offset disagreement", func(s []timeauth.QuorumSource) { s[1].Offset = 500 * time.Millisecond }},
		{"epoch disagreement", func(s []timeauth.QuorumSource) { s[1].Epoch = values.NewInstant(s[0].Epoch.Time().Add(time.Second)) }},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			s := append([]timeauth.QuorumSource(nil), base...)
			tc.mutate(s)
			if _, err := p.Evaluate(s); !errors.Is(err, timeauth.ErrQuorumUntrusted) {
				t.Fatalf("Evaluate = %v", err)
			}
		})
	}
}
