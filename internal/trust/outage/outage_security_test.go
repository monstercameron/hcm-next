package outage

import (
	"errors"
	"testing"
	"time"
)

func TestPolicyAndRequestValidation_RejectEveryMalformedBoundary(t *testing.T) {
	base := testPolicy()
	for _, mutate := range []func(*Policy){
		func(p *Policy) { p.MaxJWKSStaleness = 0 },
		func(p *Policy) { p.MaxClockSkew = -time.Second },
		func(p *Policy) { p.MaxCachedSessionAge = 0 },
	} {
		p := base
		mutate(&p)
		if err := p.validate(); !errors.Is(err, ErrInvalidPolicy) {
			t.Fatalf("policy validation = %v", err)
		}
	}
	for _, class := range []Class{ClassNewSession, ClassExistingRead, ClassExistingWrite, ClassPrivileged} {
		if !class.valid() {
			t.Errorf("known class %q invalid", class)
		}
	}
	if Class("UNKNOWN").valid() {
		t.Fatal("unknown class valid")
	}
	decision := Evaluate(base, healthyHealth(), Request{Class: ClassExistingRead, SessionAge: -time.Second}, baseNow)
	if decision.Outcome != OutcomeDeny || decision.Reason != ReasonInvalidRequest || !decision.EvaluatedAt.Equal(baseNow) {
		t.Fatalf("negative session age decision = %+v", decision)
	}
	decision = Evaluate(base, healthyHealth(), Request{Class: ClassNewSession}, baseNow.In(time.FixedZone("west", -5*60*60)))
	if !decision.EvaluatedAt.Equal(baseNow.UTC()) {
		t.Fatalf("decision time = %v", decision.EvaluatedAt)
	}
}

func TestHealthAndEmergencyValidity_FailClosedOnEachSignal(t *testing.T) {
	p := testPolicy()
	cases := []Health{
		{IdPReachable: false, JWKSAge: time.Minute, ClockSkew: time.Second},
		{IdPReachable: true, JWKSAge: -time.Second, ClockSkew: time.Second},
		{IdPReachable: true, JWKSAge: p.MaxJWKSStaleness + time.Nanosecond, ClockSkew: time.Second},
		{IdPReachable: true, JWKSAge: time.Minute, ClockSkew: p.MaxClockSkew + time.Nanosecond},
	}
	for i, health := range cases {
		if got := healthyAt(p, health); got {
			t.Fatalf("health case %d unexpectedly healthy", i)
		}
	}
	if !healthyAt(p, Health{IdPReachable: true, JWKSAge: p.MaxJWKSStaleness, ClockSkew: -p.MaxClockSkew}) {
		t.Fatal("health exactly at inclusive bounds was unhealthy")
	}
	valid := &EmergencyGrant{Approved: true, StepUpSatisfied: true, TicketRef: "INC-1", ExpiresAt: baseNow.Add(time.Minute)}
	if !valid.validAt(baseNow) {
		t.Fatal("valid emergency grant rejected")
	}
	for _, grant := range []*EmergencyGrant{
		nil,
		{Approved: false, StepUpSatisfied: true, TicketRef: "INC-1", ExpiresAt: baseNow.Add(time.Minute)},
		{Approved: true, StepUpSatisfied: false, TicketRef: "INC-1", ExpiresAt: baseNow.Add(time.Minute)},
		{Approved: true, StepUpSatisfied: true, TicketRef: " ", ExpiresAt: baseNow.Add(time.Minute)},
		{Approved: true, StepUpSatisfied: true, TicketRef: "INC-1", ExpiresAt: baseNow},
	} {
		if grant.validAt(baseNow) {
			t.Fatalf("invalid emergency grant accepted: %+v", grant)
		}
	}
}

func TestEvaluate_HealthyAndDegradedClassDecisionsAreImmutableValues(t *testing.T) {
	p := testPolicy()
	for _, class := range []Class{ClassNewSession, ClassExistingRead, ClassExistingWrite, ClassPrivileged} {
		got := Evaluate(p, healthyHealth(), Request{Class: class, SessionAge: time.Hour}, baseNow)
		if got.Outcome != OutcomePermit || got.Reason != ReasonHealthy || !got.Healthy || got.Class != class {
			t.Fatalf("healthy %s = %+v", class, got)
		}
	}
	read := Evaluate(p, staleHealth(), Request{Class: ClassExistingRead, SessionAge: p.MaxCachedSessionAge + time.Nanosecond}, baseNow)
	write := Evaluate(p, staleHealth(), Request{Class: ClassExistingWrite, SessionAge: p.MaxCachedSessionAge}, baseNow)
	if read.Outcome != OutcomeDeny || write.Outcome != OutcomePermitReadOnly || read.Healthy || write.Healthy {
		t.Fatalf("degraded decisions = %+v / %+v", read, write)
	}
	uses := []CachedUse{{SessionRef: "s", Outcome: OutcomeDeny}, {SessionRef: "p", Outcome: OutcomePermit}}
	results := Reconcile(uses)
	results[0].SessionRef = "tampered"
	if uses[0].SessionRef != "s" || results[1].Action != ReconcileNoActionNeeded {
		t.Fatalf("reconciliation mutated input or wrong default: %+v", results)
	}
}
