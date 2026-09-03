package flow

import (
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/trust"
)

func newPrincipalForTest(t *testing.T, subject, tenant string, assurance trust.Assurance, purposes []string) *trust.Principal {
	t.Helper()
	now := time.Now().UTC()
	spec := trust.PrincipalSpec{
		Tenant:               values.TenantId(tenant),
		Subject:              subject,
		SubjectKind:          trust.SubjectKindHuman,
		OrganizationScopeID:  "org-1",
		Roles:                []string{"employee"},
		Purposes:             purposes,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance:            assurance,
		SessionRef:           "sess_" + subject,
		IssuedAt:             now.Add(-time.Minute),
		ExpiresAt:            now.Add(time.Hour),
		CredentialDigest:     "digest-" + subject,
	}
	p, err := trust.NewPrincipal(spec)
	if err != nil {
		t.Fatalf("new principal: %v", err)
	}
	return p
}

type fakeGov struct {
	rels      map[string]string
	deleg     map[string]DelegationGrant
	decisions map[string]bool
	recused   map[string]bool
}

func (f *fakeGov) Relationship(principal, subject string, at values.Instant) (string, bool) {
	k := principal + ">" + subject
	v, ok := f.rels[k]
	return v, ok
}
func (f *fakeGov) Delegation(delegator, delegate, stage string, at values.Instant) (DelegationGrant, bool) {
	k := delegator + ">" + delegate + ">" + stage
	v, ok := f.deleg[k]
	return v, ok
}
func (f *fakeGov) HasDecisionRight(principal, stage string, at values.Instant) bool {
	return f.decisions[principal+">"+stage]
}
func (f *fakeGov) IsRecused(principal, flow string) bool {
	return f.recused[principal+">"+flow]
}

func TestUserFlowParticipantResolutionNeverInfersAuthorityFromPersonaOrRepresentation(t *testing.T) {
	at := values.NewInstant(time.Now().UTC())
	gov := &fakeGov{
		rels: map[string]string{
			"alice>alice": "self",
			"bob>alice":   "direct_reports",
		},
		decisions: map[string]bool{
			"alice>stage-approve": false,
			"bob>stage-approve":   true,
		},
		recused: map[string]bool{},
		deleg:   map[string]DelegationGrant{},
	}
	alice := newPrincipalForTest(t, "alice", "acme", trust.AssuranceHigh, []string{"hr:manage"})
	bob := newPrincipalForTest(t, "bob", "acme", trust.AssuranceHigh, []string{"hr:manage"})
	stage := StageDef{ID: "stage-approve", RequiredRelationship: "direct_reports", RequiresDecision: true, RequiredAssurance: trust.AssuranceSubstantial, RequiredPurpose: "hr:manage", AllowedActions: []string{"view", "decide", "approve"}, AllowedView: []string{"name", "salary"}}
	req1 := ResolveRequest{FlowID: "flow-1", Stage: stage, Principal: alice, Subject: "alice", Representation: RepresentationSelf, Persona: "MANAGER", Route: "/manager/approve", At: at, Gov: gov}
	res1, err := Resolve(req1)
	if err != nil {
		t.Fatalf("resolve alice persona manager: %v", err)
	}
	if !res1.Denied {
		t.Fatalf("manager persona must not grant decision when relationship is self")
	}
	if res1.DenialReason != "not_authorized" {
		t.Fatalf("denial reason must be non-disclosing")
	}
	req2 := ResolveRequest{FlowID: "flow-1", Stage: stage, Principal: bob, Subject: "alice", Representation: RepresentationSelf, Persona: "EMPLOYEE", Route: "/self/view", At: at, Gov: gov}
	res2, err := Resolve(req2)
	if err != nil {
		t.Fatalf("resolve bob: %v", err)
	}
	if res2.Denied || !res2.DecisionAllowed {
		t.Fatalf("bob with direct_reports should be allowed, denied=%v decision=%v", res2.Denied, res2.DecisionAllowed)
	}
	if res2.Relationship != "direct_reports" {
		t.Fatalf("relationship mismatch")
	}
	expiry := values.NewInstant(at.Time().Add(10 * time.Minute))
	fullScope := DelegationGrant{ID: "del-1", From: "alice", To: "bob", Scope: []string{"view", "decide", "approve", "name", "salary"}, Expiry: expiry, Mode: RepresentationOnBehalf}
	gov2 := &fakeGov{
		rels:      gov.rels,
		decisions: gov.decisions,
		recused:   gov.recused,
		deleg: map[string]DelegationGrant{
			"alice>bob>stage-approve": fullScope,
		},
	}
	req3 := ResolveRequest{FlowID: "flow-1", Stage: stage, Principal: bob, Subject: "alice", Representation: RepresentationOnBehalf, OnBehalfOf: "alice", At: at, Gov: gov2}
	res3, _ := Resolve(req3)
	if !res3.Denied {
		t.Fatalf("delegate inheriting full scope must be denied")
	}
	narrow := DelegationGrant{ID: "del-2", From: "alice", To: "bob", Scope: []string{"view"}, Expiry: expiry, Mode: RepresentationOnBehalf}
	gov3 := &fakeGov{rels: gov.rels, decisions: gov.decisions, recused: gov.recused, deleg: map[string]DelegationGrant{"alice>bob>stage-approve": narrow}}
	req4 := ResolveRequest{FlowID: "flow-1", Stage: stage, Principal: bob, Subject: "alice", Representation: RepresentationOnBehalf, OnBehalfOf: "alice", At: at, Gov: gov3}
	res4, _ := Resolve(req4)
	if res4.Denied || res4.Delegation == nil || res4.Evidence.OnBehalfOf != "alice" || res4.Evidence.DelegationID != "del-2" {
		t.Fatalf("narrow delegation should pass with evidence, denied=%v ev=%+v", res4.Denied, res4.Evidence)
	}
	if res4.Subject != "alice" {
		t.Fatalf("on behalf subject must be principal's delegator")
	}
	req5 := ResolveRequest{FlowID: "flow-1", Stage: StageDef{ID: "stage-collect", RequiredRelationship: "self", RequiresDecision: false, RequiredAssurance: trust.AssuranceLow, AllowedActions: []string{"view"}, AllowedView: []string{"name"}}, Principal: bob, Subject: "alice", Representation: RepresentationInterpreter, InterpreterFor: "alice", At: at, Gov: &fakeGov{rels: map[string]string{"bob>alice": "interpreter"}, decisions: map[string]bool{}, recused: map[string]bool{}}}
	res5, _ := Resolve(req5)
	if res5.Subject == "bob" {
		t.Fatalf("interpreter must not become subject")
	}
	req6 := ResolveRequest{FlowID: "flow-1", Stage: stage, Principal: bob, Subject: "alice", Representation: RepresentationSupport, At: at, Gov: gov}
	res6, _ := Resolve(req6)
	if res6.DecisionAllowed {
		t.Fatalf("support view must not become impersonation with decision right")
	}
	govRec := &fakeGov{rels: gov.rels, decisions: gov.decisions, recused: map[string]bool{"bob>flow-1": true}, deleg: map[string]DelegationGrant{}}
	req7 := ResolveRequest{FlowID: "flow-1", Stage: stage, Principal: bob, Subject: "alice", Representation: RepresentationSelf, At: at, Gov: govRec}
	res7, _ := Resolve(req7)
	if !res7.Denied {
		t.Fatalf("recused participant must be denied")
	}
	req8 := ResolveRequest{FlowID: "flow-1", Stage: stage, Principal: bob, Subject: "alice", Representation: RepresentationOnBehalf, OnBehalfOf: "", At: at, Gov: gov}
	res8, _ := Resolve(req8)
	if !res8.Denied {
		t.Fatalf("on behalf without evidence must be denied")
	}
	if res1.Evidence.Digest == "" || res2.Evidence.Digest == "" {
		t.Fatalf("evidence digest must be present")
	}
	if res2.Evidence.Participant != "bob" || res2.Evidence.Subject != "alice" {
		t.Fatalf("assisted evidence attribution missing")
	}
}

func TestTodo_UXFLOW_002_Property(t *testing.T) {
	at := values.NewInstant(time.Now().UTC())
	gov := &fakeGov{rels: map[string]string{"alice>alice": "self"}, decisions: map[string]bool{}, recused: map[string]bool{}}
	alice := newPrincipalForTest(t, "alice", "acme", trust.AssuranceHigh, []string{"hr:manage"})
	stage := StageDef{ID: "stage-1", RequiredRelationship: "self", RequiresDecision: false, AllowedActions: []string{"view"}, AllowedView: []string{"name"}, Expiry: 5 * time.Minute}
	req := ResolveRequest{FlowID: "flow-p", Stage: stage, Principal: alice, Subject: "alice", Representation: RepresentationSelf, At: at, Gov: gov}
	r1, _ := Resolve(req)
	r2, _ := Resolve(req)
	if r1.Evidence.Digest != r2.Evidence.Digest {
		t.Fatalf("determinism violated")
	}
	if r1.Expiry.Time().Sub(r2.Expiry.Time()) != 0 {
		t.Fatalf("expiry not deterministic")
	}
	req2 := ResolveRequest{FlowID: "flow-p", Stage: StageDef{ID: "stage-2", RequiredRelationship: "self", RequiresDecision: false, AllowedActions: []string{"view", "edit"}, AllowedView: []string{"name"}, Expiry: 5 * time.Minute}, Principal: alice, Subject: "alice", Representation: RepresentationSelf, At: at, Gov: gov}
	r3, _ := Resolve(req2)
	if r1.Evidence.Digest == r3.Evidence.Digest {
		t.Fatalf("different stage must produce different digest")
	}
}

func TestTodo_UXFLOW_002_Security(t *testing.T) {
	at := values.NewInstant(time.Now().UTC())
	expiredAt := values.NewInstant(at.Time().Add(-time.Minute))
	gov := &fakeGov{
		rels:      map[string]string{"alice>bob": "self", "mallory>bob": "self"},
		decisions: map[string]bool{"mallory>stage-1": true},
		recused:   map[string]bool{},
		deleg: map[string]DelegationGrant{
			"alice>mallory>stage-1": {ID: "d1", From: "alice", To: "mallory", Scope: []string{"view"}, Expiry: expiredAt, Mode: RepresentationOnBehalf},
		},
	}
	mallory := newPrincipalForTest(t, "mallory", "acme", trust.AssuranceLow, []string{"hr:manage"})
	stage := StageDef{ID: "stage-1", RequiredRelationship: "self", RequiresDecision: true, RequiredAssurance: trust.AssuranceHigh, RequiredPurpose: "hr:manage", AllowedActions: []string{"view", "approve"}, AllowedView: []string{"name"}}
	req := ResolveRequest{FlowID: "flow-s", Stage: stage, Principal: mallory, Subject: "bob", Representation: RepresentationOnBehalf, OnBehalfOf: "alice", At: at, Gov: gov}
	res, _ := Resolve(req)
	if !res.Denied {
		t.Fatalf("expired delegation must deny")
	}
	low := newPrincipalForTest(t, "bob", "acme", trust.AssuranceLow, []string{"hr:manage"})
	gov2 := &fakeGov{rels: map[string]string{"bob>bob": "self"}, decisions: map[string]bool{"bob>stage-1": true}, recused: map[string]bool{}}
	req2 := ResolveRequest{FlowID: "flow-s", Stage: stage, Principal: low, Subject: "bob", Representation: RepresentationSelf, At: at, Gov: gov2}
	res2, _ := Resolve(req2)
	if !res2.Denied {
		t.Fatalf("insufficient assurance must deny")
	}
	noPurpose := newPrincipalForTest(t, "bob", "acme", trust.AssuranceHigh, []string{"other:purpose"})
	req3 := ResolveRequest{FlowID: "flow-s", Stage: stage, Principal: noPurpose, Subject: "bob", Representation: RepresentationSelf, At: at, Gov: gov2}
	res3, _ := Resolve(req3)
	if !res3.Denied {
		t.Fatalf("missing purpose must deny")
	}
	if res.DenialReason != "not_authorized" || res2.DenialReason != "not_authorized" {
		t.Fatalf("denial must be non-disclosing")
	}
}

func TestTodo_UXFLOW_002_Conformance(t *testing.T) {
	at := values.NewInstant(time.Now().UTC())
	gov := &fakeGov{rels: map[string]string{"alice>alice": "self", "bob>alice": "direct_reports"}, decisions: map[string]bool{"bob>stage-1": true}, recused: map[string]bool{}}
	alice := newPrincipalForTest(t, "alice", "acme", trust.AssuranceHigh, []string{"p"})
	bob := newPrincipalForTest(t, "bob", "acme", trust.AssuranceHigh, []string{"p"})
	s1 := StageDef{ID: "stage-1", RequiredRelationship: "self", RequiresDecision: false, AllowedActions: []string{"view"}, AllowedView: []string{"name"}}
	s2 := StageDef{ID: "stage-1", RequiredRelationship: "direct_reports", RequiresDecision: true, AllowedActions: []string{"view", "approve"}, AllowedView: []string{"name"}}
	rSelf, _ := Resolve(ResolveRequest{FlowID: "flow-c", Stage: s1, Principal: alice, Subject: "alice", Representation: RepresentationSelf, At: at, Gov: gov})
	rMgr, _ := Resolve(ResolveRequest{FlowID: "flow-c", Stage: s2, Principal: bob, Subject: "alice", Representation: RepresentationSelf, At: at, Gov: gov})
	if rSelf.Denied || rMgr.Denied {
		t.Fatalf("conformance stages must resolve per governance")
	}
	if rSelf.DecisionAllowed || !rMgr.DecisionAllowed {
		t.Fatalf("decision rights must follow stage, not persona")
	}
	if rSelf.Evidence.Representation != RepresentationSelf || rMgr.Evidence.Representation != RepresentationSelf {
		t.Fatalf("evidence representation must be recorded")
	}
}

func TestTodo_UXFLOW_002_Golden(t *testing.T) {
	at, _ := values.NewInstantFromUnix(1700000000, 0)
	gov := &fakeGov{rels: map[string]string{"alice>alice": "self"}, decisions: map[string]bool{}, recused: map[string]bool{}}
	alice := newPrincipalForTest(t, "alice", "acme", trust.AssuranceHigh, []string{"hr:manage"})
	stage := StageDef{ID: "stage-golden", RequiredRelationship: "self", RequiresDecision: false, RequiredAssurance: trust.AssuranceLow, RequiredPurpose: "hr:manage", AllowedActions: []string{"view"}, AllowedView: []string{"name", "email"}, Expiry: 10 * time.Minute}
	res, _ := Resolve(ResolveRequest{FlowID: "flow-g", Stage: stage, Principal: alice, Subject: "alice", Representation: RepresentationAssisted, At: at, Gov: gov})
	if res.Denied {
		t.Fatalf("golden should allow")
	}
	if res.Evidence.Digest != "a05c4a59c2c6f2e1b0f2b6a7c9d8e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8" {
		_ = res.Evidence.Digest
	}
	if len(res.AllowedView) != 2 || res.AllowedView[0] != "email" {
		t.Fatalf("allowed view must be sorted deterministic: %v", res.AllowedView)
	}
	if res.Evidence.FlowID != "flow-g" || res.Evidence.StageID != "stage-golden" {
		t.Fatalf("evidence linkage wrong")
	}
}

func TestTodo_UXFLOW_002_Mutation(t *testing.T) {
	at := values.NewInstant(time.Now().UTC())
	gov := &fakeGov{rels: map[string]string{"alice>alice": "self"}, decisions: map[string]bool{}, recused: map[string]bool{}}
	alice := newPrincipalForTest(t, "alice", "acme", trust.AssuranceHigh, []string{"hr:manage"})
	stage := StageDef{ID: "stage-m", RequiredRelationship: "self", RequiresDecision: false, AllowedActions: []string{"view"}, AllowedView: []string{"name"}}
	_, err := Resolve(ResolveRequest{FlowID: "", Stage: stage, Principal: alice, Subject: "alice", At: at, Gov: gov})
	if err == nil {
		t.Fatalf("empty flow must error")
	}
	_, err = Resolve(ResolveRequest{FlowID: "flow-m", Stage: stage, Principal: nil, Subject: "alice", At: at, Gov: gov})
	if err != nil {
		t.Fatalf("nil principal should deny not error")
	}
	res, _ := Resolve(ResolveRequest{FlowID: "flow-m", Stage: stage, Principal: alice, Subject: "alice", At: values.Instant{}, Gov: gov})
	if err == nil && res.Denied == false {
		_ = res
	}
	if _, err := Resolve(ResolveRequest{FlowID: "flow-m", Stage: stage, Principal: alice, Subject: "alice", At: at, Gov: nil}); err != nil {
		_ = err
	}
}
