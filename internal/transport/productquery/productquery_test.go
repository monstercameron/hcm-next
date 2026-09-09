package productquery_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/productquery"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

var (
	testNow       = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	testEffective = values.NewInstant(testNow)
)

func principal(t *testing.T, roles []string, purposes []string) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant:               values.TenantId("acme"),
		Subject:              "operator-1",
		SubjectKind:          trust.SubjectKindHuman,
		Roles:                roles,
		Purposes:             purposes,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance:            trust.AssuranceHigh,
		SessionRef:           "session-1",
		IssuedAt:             testNow.Add(-time.Hour),
		ExpiresAt:            testNow.Add(time.Hour),
		CredentialDigest:     "credential-digest-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func subject(tenant, id string) values.EntityRef {
	return values.EntityRef{Tenant: values.TenantId(tenant), Kind: values.Kind("worker"), Id: id}
}

func projection(observed time.Time) productquery.Projection {
	return productquery.Projection{
		Name:              "worker_summary",
		DefinitionVersion: "worker-summary.v3",
		SchemaVersion:     "schema.v5",
		SourceSequence:    12,
		Watermark:         10,
		ObservedAt:        observed,
		MaxAge:            5 * time.Minute,
	}
}

func request(p *trust.Principal, candidates []productquery.Candidate) productquery.Request {
	return productquery.Request{
		Principal:   p,
		EffectiveAt: testEffective,
		Fields:      []authz.FieldID{authz.FieldBaseSalary, authz.FieldWorkerNumber},
		Candidates:  candidates,
		Projection:  projection(testNow.Add(-time.Minute)),
		ObservedNow: testNow,
	}
}

func allowedCandidate(id string) productquery.Candidate {
	return productquery.Candidate{
		Subject: subject("acme", id),
		Fields: map[authz.FieldID]productquery.Cell{
			authz.FieldWorkerNumber: {State: productquery.ValuePresent, Value: "W-" + id},
			authz.FieldBaseSalary:   {State: productquery.ValuePresent, Value: "125000.00"},
		},
	}
}

func authorizedDecision(t *testing.T, p *trust.Principal, s values.EntityRef) authz.Decision {
	t.Helper()
	d, err := authz.Enforce(authz.Request{
		Principal:   p,
		Purpose:     authz.PurposeCompensationReview,
		EffectiveAt: testEffective,
		Subject:     s,
		Fields:      []authz.FieldID{authz.FieldWorkerNumber},
	})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestTodo_ALIGN_022(t *testing.T) {
	p := principal(t, []string{string(authz.RoleCompAdmin)}, []string{authz.PurposeCompensationReview})
	r := request(p, []productquery.Candidate{
		allowedCandidate("00000000-0000-4000-8000-000000000001"),
		{Subject: subject("other", "00000000-0000-4000-8000-000000000002"), Fields: map[authz.FieldID]productquery.Cell{
			authz.FieldWorkerNumber: {State: productquery.ValueState("MALFORMED"), Value: "MUST-NOT-LEAK"},
		}},
	})
	envelope, err := productquery.Project(r)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.ContractVersion != productquery.Version() || envelope.Tenant != values.TenantId("acme") {
		t.Fatalf("envelope identity = %+v", envelope)
	}
	if envelope.Freshness != productquery.FreshnessCurrent || len(envelope.Rows) != 1 {
		t.Fatalf("authorized projection = %+v", envelope)
	}
	if envelope.Purpose != authz.PurposeCompensationReview || envelope.Digest() == "" {
		t.Fatalf("semantic metadata = %+v digest=%q", envelope, envelope.Digest())
	}
}

func TestTodo_ALIGN_022_Property(t *testing.T) {
	p := principal(t, []string{string(authz.RoleCompAdmin)}, []string{authz.PurposeCompensationReview})
	first := request(p, []productquery.Candidate{allowedCandidate("00000000-0000-4000-8000-000000000001")})
	second := first
	second.Fields = []authz.FieldID{authz.FieldWorkerNumber, authz.FieldBaseSalary}
	second.Candidates = []productquery.Candidate{first.Candidates[0]}
	// The two requests name the same fields in a different order.
	first.Fields = []authz.FieldID{authz.FieldBaseSalary, authz.FieldWorkerNumber}
	one, err := productquery.Project(first)
	if err != nil {
		t.Fatal(err)
	}
	two, err := productquery.Project(second)
	if err != nil {
		t.Fatal(err)
	}
	if one.Digest() != two.Digest() {
		t.Fatalf("field-order changed semantic digest: %s != %s", one.Digest(), two.Digest())
	}
}

func TestTodo_ALIGN_022_Golden(t *testing.T) {
	p := principal(t, []string{string(authz.RoleCompAdmin)}, []string{authz.PurposeCompensationReview})
	envelope, err := productquery.Project(request(p, []productquery.Candidate{allowedCandidate("00000000-0000-4000-8000-000000000001")}))
	if err != nil {
		t.Fatal(err)
	}
	if len(envelope.Rows) != 1 || len(envelope.Rows[0].Fields) != 2 {
		t.Fatalf("golden row = %+v", envelope.Rows)
	}
	if envelope.Rows[0].Fields[0].ID != authz.FieldBaseSalary || envelope.Rows[0].Fields[1].ID != authz.FieldWorkerNumber {
		t.Fatalf("fields are not canonicalized: %+v", envelope.Rows[0].Fields)
	}
	if envelope.Rows[0].Fields[0].Value != "125000.00" {
		t.Fatalf("authorized value changed: %+v", envelope.Rows[0].Fields[0])
	}
}

func TestTodo_ALIGN_022_Security(t *testing.T) {
	p := principal(t, []string{string(authz.RoleCompAdmin)}, []string{authz.PurposeCompensationReview})
	r := request(p, []productquery.Candidate{
		allowedCandidate("00000000-0000-4000-8000-000000000001"),
		{Subject: subject("other", "00000000-0000-4000-8000-000000000002"), Fields: map[authz.FieldID]productquery.Cell{
			authz.FieldWorkerNumber: {State: productquery.ValuePresent, Value: "MUST-NOT-LEAK"},
		}},
	})
	envelope, err := productquery.Project(r)
	if err != nil {
		t.Fatal(err)
	}
	b, err := envelope.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "MUST-NOT-LEAK") || strings.Contains(string(b), "00000000-0000-4000-8000-000000000002") {
		t.Fatalf("unauthorized candidate reached the envelope: %s", b)
	}
}

func TestTodo_ALIGN_022_Integration(t *testing.T) {
	p := principal(t, []string{string(authz.RoleCompAdmin)}, []string{authz.PurposeCompensationReview})
	r := request(p, []productquery.Candidate{allowedCandidate("00000000-0000-4000-8000-000000000001")})
	grpcEnvelope, err := productquery.Build(r)
	if err != nil {
		t.Fatal(err)
	}
	httpEnvelope, err := productquery.Project(r)
	if err != nil {
		t.Fatal(err)
	}
	grpcBytes, _ := grpcEnvelope.CanonicalBytes()
	httpBytes, _ := httpEnvelope.CanonicalBytes()
	if !reflect.DeepEqual(grpcBytes, httpBytes) || grpcEnvelope.Digest() != httpEnvelope.Digest() {
		t.Fatal("transport adapters did not produce one semantic envelope")
	}
}

func TestTodo_ALIGN_022_Fault(t *testing.T) {
	p := principal(t, []string{string(authz.RoleCompAdmin)}, []string{authz.PurposeCompensationReview})
	r := request(p, []productquery.Candidate{allowedCandidate("00000000-0000-4000-8000-000000000001")})
	r.Projection = projection(testNow.Add(-6 * time.Minute))
	envelope, err := productquery.Project(r)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Freshness != productquery.FreshnessStale {
		t.Fatalf("stale projection freshness = %s", envelope.Freshness)
	}
	r.Candidates[0].Fields[authz.FieldWorkerNumber] = productquery.Cell{State: productquery.ValueState("BROKEN"), Value: "x"}
	if _, err := productquery.Project(r); err == nil {
		t.Fatal("malformed source cell was accepted")
	}
}

func TestTodo_ALIGN_022_Conformance(t *testing.T) {
	if productquery.Version() != 1 || productquery.Explain() == "" {
		t.Fatalf("contract shape is incomplete: version=%d explain=%q", productquery.Version(), productquery.Explain())
	}
	if productquery.MaxCandidates <= 0 || productquery.MaxFields <= 0 {
		t.Fatal("query bounds are not positive")
	}
}

func TestTodo_ALIGN_023(t *testing.T) {
	p := principal(t, []string{string(authz.RoleCompAdmin)}, []string{authz.PurposeCompensationReview})
	authorized := subject("acme", "00000000-0000-4000-8000-000000000001")
	denied := subject("other", "00000000-0000-4000-8000-000000000002")
	message, emit, err := productquery.EmitInvalidation(productquery.InvalidationRequest{
		Principal: p, Purpose: authz.PurposeCompensationReview, EffectiveAt: testEffective, Fields: []authz.FieldID{authz.FieldWorkerNumber}, Projection: "worker_summary", SourceSequence: 12, Watermark: 10,
		Targets: []productquery.InvalidationTarget{
			{Subject: authorized, Revision: 4, Decision: authorizedDecision(t, p, authorized)},
			{Subject: denied, Revision: 9},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !emit || len(message.Items) != 1 || message.Items[0].Subject != authorized || message.Items[0].Revision != 4 {
		t.Fatalf("bounded authorized message = %+v emitted=%v", message, emit)
	}
}

func TestTodo_ALIGN_023_Property(t *testing.T) {
	p := principal(t, []string{string(authz.RoleCompAdmin)}, []string{authz.PurposeCompensationReview})
	a := subject("acme", "00000000-0000-4000-8000-000000000001")
	b := subject("acme", "00000000-0000-4000-8000-000000000002")
	base := productquery.InvalidationRequest{Principal: p, Purpose: authz.PurposeCompensationReview, EffectiveAt: testEffective, Fields: []authz.FieldID{authz.FieldWorkerNumber}, Projection: "worker_summary", SourceSequence: 12, Watermark: 10,
		Targets: []productquery.InvalidationTarget{{Subject: b, Revision: 2, Decision: authorizedDecision(t, p, b)}, {Subject: a, Revision: 3, Decision: authorizedDecision(t, p, a)}}}
	permuted := base
	permuted.Targets = []productquery.InvalidationTarget{base.Targets[1], base.Targets[0]}
	one, emitOne, err := productquery.EmitInvalidation(base)
	if err != nil {
		t.Fatal(err)
	}
	two, emitTwo, err := productquery.EmitInvalidation(permuted)
	if err != nil {
		t.Fatal(err)
	}
	if !emitOne || !emitTwo || one.Digest() != two.Digest() {
		t.Fatalf("target order changed invalidation digest: %s != %s", one.Digest(), two.Digest())
	}
}

func TestTodo_ALIGN_023_Golden(t *testing.T) {
	p := principal(t, []string{string(authz.RoleCompAdmin)}, []string{authz.PurposeCompensationReview})
	s := subject("acme", "00000000-0000-4000-8000-000000000001")
	message, emit, err := productquery.EmitInvalidation(productquery.InvalidationRequest{
		Principal: p, Purpose: authz.PurposeCompensationReview, EffectiveAt: testEffective, Fields: []authz.FieldID{authz.FieldWorkerNumber}, Projection: "worker_summary", SourceSequence: 7, Watermark: 7,
		Targets: []productquery.InvalidationTarget{{Subject: s, Revision: 1, Decision: authorizedDecision(t, p, s)}, {Subject: s, Revision: 4, Decision: authorizedDecision(t, p, s)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !emit || len(message.Items) != 1 || message.Items[0].Revision != 4 {
		t.Fatalf("duplicate target reduction = %+v emitted=%v", message, emit)
	}
}

func TestTodo_ALIGN_023_Security(t *testing.T) {
	p := principal(t, []string{string(authz.RoleCompAdmin)}, []string{authz.PurposeCompensationReview})
	message, emit, err := productquery.EmitInvalidation(productquery.InvalidationRequest{
		Principal: p, Purpose: authz.PurposeCompensationReview, EffectiveAt: testEffective, Fields: []authz.FieldID{authz.FieldWorkerNumber}, Projection: "worker_summary", SourceSequence: 1, Watermark: 1,
		Targets: []productquery.InvalidationTarget{{Subject: subject("other", "00000000-0000-4000-8000-000000000002"), Revision: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if emit || len(message.Items) != 0 || message.Digest() != "" {
		t.Fatalf("unauthorized-only batch produced a distinguishable message: %+v emitted=%v digest=%q", message, emit, message.Digest())
	}
}

func TestTodo_ALIGN_023_Integration(t *testing.T) {
	p := principal(t, []string{string(authz.RoleCompAdmin)}, []string{authz.PurposeCompensationReview})
	s := subject("acme", "00000000-0000-4000-8000-000000000001")
	r := productquery.InvalidationRequest{Principal: p, Purpose: authz.PurposeCompensationReview, EffectiveAt: testEffective, Fields: []authz.FieldID{authz.FieldWorkerNumber}, Projection: "worker_summary", SourceSequence: 3, Watermark: 2,
		Targets: []productquery.InvalidationTarget{{Subject: s, Revision: 8, Decision: authorizedDecision(t, p, s)}}}
	one, emitOne, err := productquery.EmitInvalidation(r)
	if err != nil {
		t.Fatal(err)
	}
	two, emitTwo, err := productquery.BuildInvalidation(r)
	if err != nil {
		t.Fatal(err)
	}
	if !emitOne || !emitTwo || one.Digest() != two.Digest() {
		t.Fatalf("invalidation adapter mismatch")
	}
}

func TestTodo_ALIGN_023_Fault(t *testing.T) {
	p := principal(t, []string{string(authz.RoleCompAdmin)}, []string{authz.PurposeCompensationReview})
	s := subject("acme", "00000000-0000-4000-8000-000000000001")
	r := productquery.InvalidationRequest{Principal: p, Purpose: authz.PurposeCompensationReview, EffectiveAt: testEffective, Fields: []authz.FieldID{authz.FieldWorkerNumber}, Projection: "worker_summary", SourceSequence: 1, Watermark: 1,
		Targets: []productquery.InvalidationTarget{{Subject: s, Revision: 1, Decision: authorizedDecision(t, p, s)}}}
	r.Watermark = 2
	if _, _, err := productquery.EmitInvalidation(r); err == nil {
		t.Fatal("watermark ahead of source was accepted")
	}
	r.Watermark = 1
	r.Targets[0].Revision = 0
	if _, _, err := productquery.EmitInvalidation(r); err == nil {
		t.Fatal("zero invalidation revision was accepted")
	}
	tooMany := make([]productquery.InvalidationTarget, productquery.MaxInvalidationItems+1)
	for i := range tooMany {
		tooMany[i] = productquery.InvalidationTarget{Subject: s, Revision: uint64(i + 1), Decision: authorizedDecision(t, p, s)}
	}
	r.Targets = tooMany
	if _, _, err := productquery.EmitInvalidation(r); err == nil {
		t.Fatal("unbounded invalidation batch was accepted")
	}
}

func TestTodo_ALIGN_023_Conformance(t *testing.T) {
	if productquery.MaxInvalidationItems <= 0 || productquery.MaxInvalidationBytes <= 0 {
		t.Fatal("invalidation bounds are not positive")
	}
	if productquery.Explain() == "" {
		t.Fatal("contract explanation is empty")
	}
}

func TestProductQueryContractProjectionAndEnvelopeValidation(t *testing.T) {
	if productquery.Version() != 1 || productquery.Explain() == "" {
		t.Fatalf("contract metadata version=%d explain=%q", productquery.Version(), productquery.Explain())
	}
	validProjection := projection(testNow.Add(-time.Minute))
	if err := validProjection.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*productquery.Projection)
	}{
		{"empty name", func(p *productquery.Projection) { p.Name = "" }},
		{"unbounded version", func(p *productquery.Projection) {
			p.SchemaVersion = strings.Repeat("x", productquery.MaxProjectionNameSize+1)
		}},
		{"watermark ahead", func(p *productquery.Projection) { p.Watermark = p.SourceSequence + 1 }},
		{"missing observed time", func(p *productquery.Projection) { p.ObservedAt = time.Time{} }},
		{"missing max age", func(p *productquery.Projection) { p.MaxAge = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := validProjection
			tc.mutate(&p)
			if err := p.Validate(); err == nil {
				t.Fatal("invalid projection accepted")
			}
		})
	}
	p := principal(t, []string{string(authz.RoleCompAdmin)}, []string{authz.PurposeCompensationReview})
	envelope, err := productquery.Project(request(p, []productquery.Candidate{allowedCandidate("00000000-0000-4000-8000-000000000001")}))
	if err != nil || envelope.Validate() != nil {
		t.Fatalf("valid envelope=%+v err=%v validation=%v", envelope, err, envelope.Validate())
	}
	for _, tc := range []struct {
		name   string
		mutate func(*productquery.Envelope)
	}{
		{"bad version", func(e *productquery.Envelope) { e.ContractVersion = 0 }},
		{"bad tenant", func(e *productquery.Envelope) { e.Tenant = "" }},
		{"bad purpose", func(e *productquery.Envelope) { e.Purpose = " " }},
		{"bad freshness", func(e *productquery.Envelope) { e.Freshness = productquery.Freshness("BROKEN") }},
		{"foreign row", func(e *productquery.Envelope) { e.Rows[0].Subject.Tenant = "other" }},
		{"duplicate row", func(e *productquery.Envelope) { e.Rows = append(e.Rows, e.Rows[0]) }},
		{"invalid field", func(e *productquery.Envelope) { e.Rows[0].Fields[0].Disposition = authz.Effect(99) }},
		{"redacted value", func(e *productquery.Envelope) {
			e.Rows[0].Fields[0].Disposition = authz.EffectRedacted
			e.Rows[0].Fields[0].Value = "secret"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copyEnvelope := envelope
			copyEnvelope.Rows = append([]productquery.Row(nil), envelope.Rows...)
			copyEnvelope.Rows[0].Fields = append([]productquery.Field(nil), envelope.Rows[0].Fields...)
			tc.mutate(&copyEnvelope)
			if err := copyEnvelope.Validate(); err == nil {
				t.Fatal("invalid envelope accepted")
			}
		})
	}
}

func TestProductQueryProjectRejectsMalformedRequests(t *testing.T) {
	p := principal(t, []string{string(authz.RoleCompAdmin)}, []string{authz.PurposeCompensationReview})
	for _, tc := range []struct {
		name   string
		mutate func(*productquery.Request)
	}{
		{"nil principal", func(r *productquery.Request) { r.Principal = nil }},
		{"unbounded purpose", func(r *productquery.Request) { r.Purpose = strings.Repeat("x", productquery.MaxProjectionNameSize+1) }},
		{"invalid effective time", func(r *productquery.Request) { r.EffectiveAt = values.Instant{} }},
		{"no fields", func(r *productquery.Request) { r.Fields = nil }},
		{"duplicate field", func(r *productquery.Request) {
			r.Fields = []authz.FieldID{authz.FieldWorkerNumber, authz.FieldWorkerNumber}
		}},
		{"invalid candidate", func(r *productquery.Request) { r.Candidates[0].Subject = values.EntityRef{} }},
		{"duplicate candidate", func(r *productquery.Request) { r.Candidates = append(r.Candidates, r.Candidates[0]) }},
		{"invalid projection", func(r *productquery.Request) { r.Projection.MaxAge = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := request(p, []productquery.Candidate{allowedCandidate("00000000-0000-4000-8000-000000000001")})
			tc.mutate(&r)
			if _, err := productquery.Project(r); err == nil {
				t.Fatal("malformed request accepted")
			}
		})
	}
}

func TestProductQueryInvalidationRejectsForgedDecision(t *testing.T) {
	p := principal(t, []string{string(authz.RoleCompAdmin)}, []string{authz.PurposeCompensationReview})
	s := subject("acme", "00000000-0000-4000-8000-000000000001")
	forged := authorizedDecision(t, p, s)
	forged.MatchedRules = []string{"forged-rule"}
	message, emit, err := productquery.EmitInvalidation(productquery.InvalidationRequest{
		Principal: p, Purpose: authz.PurposeCompensationReview, EffectiveAt: testEffective, Fields: []authz.FieldID{authz.FieldWorkerNumber}, Projection: "worker_summary", SourceSequence: 2, Watermark: 2,
		Targets: []productquery.InvalidationTarget{{Subject: s, Revision: 1, Decision: forged}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if emit || len(message.Items) != 0 || message.Digest() != "" {
		t.Fatalf("forged decision was emitted: %+v emit=%v", message, emit)
	}
}

func TestProductQueryInvalidationInputAndMessageValidation(t *testing.T) {
	p := principal(t, []string{string(authz.RoleCompAdmin)}, []string{authz.PurposeCompensationReview})
	s := subject("acme", "00000000-0000-4000-8000-000000000001")
	valid := productquery.InvalidationMessage{ContractVersion: 1, Tenant: "acme", Projection: "worker_summary", SourceSequence: 2, Watermark: 1, Items: []productquery.InvalidationItem{{Subject: s, Revision: 1}}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*productquery.InvalidationMessage)
	}{
		{"bad version", func(m *productquery.InvalidationMessage) { m.ContractVersion = 0 }},
		{"bad tenant", func(m *productquery.InvalidationMessage) { m.Tenant = "" }},
		{"bad projection", func(m *productquery.InvalidationMessage) { m.Projection = "" }},
		{"ahead watermark", func(m *productquery.InvalidationMessage) { m.Watermark = 3 }},
		{"empty items", func(m *productquery.InvalidationMessage) { m.Items = nil }},
		{"zero revision", func(m *productquery.InvalidationMessage) { m.Items[0].Revision = 0 }},
		{"foreign item", func(m *productquery.InvalidationMessage) { m.Items[0].Subject.Tenant = "other" }},
		{"unsorted duplicate", func(m *productquery.InvalidationMessage) { m.Items = append(m.Items, m.Items[0]) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := valid
			m.Items = append([]productquery.InvalidationItem(nil), valid.Items...)
			tc.mutate(&m)
			if err := m.Validate(); err == nil {
				t.Fatal("invalid message accepted")
			}
		})
	}
	for _, tc := range []struct {
		name   string
		mutate func(*productquery.InvalidationRequest)
	}{
		{"nil principal", func(r *productquery.InvalidationRequest) { r.Principal = nil }},
		{"bad projection", func(r *productquery.InvalidationRequest) { r.Projection = "" }},
		{"ahead watermark", func(r *productquery.InvalidationRequest) { r.Watermark = 3 }},
		{"too many targets", func(r *productquery.InvalidationRequest) {
			r.Targets = make([]productquery.InvalidationTarget, productquery.MaxInvalidationItems+1)
		}},
		{"bad target subject", func(r *productquery.InvalidationRequest) { r.Targets[0].Subject = values.EntityRef{} }},
		{"zero revision", func(r *productquery.InvalidationRequest) { r.Targets[0].Revision = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := productquery.InvalidationRequest{Principal: p, Purpose: authz.PurposeCompensationReview, EffectiveAt: testEffective, Fields: []authz.FieldID{authz.FieldWorkerNumber}, Projection: "worker_summary", SourceSequence: 2, Watermark: 1, Targets: []productquery.InvalidationTarget{{Subject: s, Revision: 1, Decision: authorizedDecision(t, p, s)}}}
			tc.mutate(&r)
			if _, _, err := productquery.EmitInvalidation(r); err == nil {
				t.Fatal("invalid invalidation request accepted")
			}
		})
	}
}

func TestProductQueryCanonicalBytesAndDigestAreStable(t *testing.T) {
	p := principal(t, []string{string(authz.RoleCompAdmin)}, []string{authz.PurposeCompensationReview})
	e, err := productquery.Project(request(p, []productquery.Candidate{allowedCandidate("00000000-0000-4000-8000-000000000001")}))
	if err != nil {
		t.Fatal(err)
	}
	before := e.Rows[0].Fields[0].ID
	one, err := e.CanonicalBytes()
	if err != nil || len(one) == 0 || e.Digest() == "" {
		t.Fatalf("canonical=%s digest=%q err=%v", one, e.Digest(), err)
	}
	if e.Rows[0].Fields[0].ID != before {
		t.Fatal("CanonicalBytes mutated the envelope")
	}
	copyEnvelope := e
	copyEnvelope.PolicyVersions = append([]string(nil), e.PolicyVersions...)
	copyEnvelope.Rows = append([]productquery.Row(nil), e.Rows...)
	copyEnvelope.Rows[0].Fields = append([]productquery.Field(nil), e.Rows[0].Fields...)
	if copyEnvelope.Digest() != e.Digest() {
		t.Fatal("equivalent envelope changed digest")
	}
	invalid := productquery.InvalidationMessage{Tenant: "acme", Projection: "worker_summary", Items: nil}
	bytes, err := invalid.CanonicalBytes()
	if invalid.Digest() != "" || err != nil || len(bytes) == 0 {
		t.Fatal("empty invalidation digest behavior changed")
	}
}
