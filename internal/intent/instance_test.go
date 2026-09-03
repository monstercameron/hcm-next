package intent_test

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/intent/lifecycle"
	"github.com/monstercameron/hcm-next/internal/kernel/digest"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// TestTodo_INTENT_002 is the PRIMARY test for typed IntentInstance envelopes.
//
// RED: a missing tenant, organization scope, purpose, initiator, definition,
// idempotency key, correlation id, classification, retention class or control
// snapshot is rejected, and so is an untyped payload.
//
// GREEN: a valid typed request creates an instance with a UUIDv7 identifier, a
// canonical request digest and immutable creation evidence.
func TestTodo_INTENT_002(t *testing.T) {
	reg := mustRegistry(t)
	d := mustDigester(t)
	def := mustResolve(t, reg, "hcmnext.people.promote_worker/v1")

	t.Run("RED", func(t *testing.T) {
		cases := []struct {
			name   string
			break_ func(*intent.InstanceSpec)
			cause  error
		}{
			{"missing tenant", func(s *intent.InstanceSpec) { s.Tenant = "" }, intent.ErrInvalidInstance},
			{"missing organization scope", func(s *intent.InstanceSpec) { s.OrganizationScopeID = "" }, intent.ErrInvalidInstance},
			{"missing purpose", func(s *intent.InstanceSpec) { s.Purpose = "" }, intent.ErrInvalidInstance},
			{"missing initiator id", func(s *intent.InstanceSpec) { s.Initiator.PrincipalID = "" }, intent.ErrInvalidInstance},
			{"missing identity assurance", func(s *intent.InstanceSpec) { s.Initiator.IdentityAssuranceRef = "" }, intent.ErrInvalidInstance},
			{"missing idempotency key", func(s *intent.InstanceSpec) { s.IdempotencyKey = "" }, intent.ErrInvalidInstance},
			{"missing correlation id", func(s *intent.InstanceSpec) { s.CorrelationID = "" }, intent.ErrInvalidInstance},
			{"missing trace id", func(s *intent.InstanceSpec) { s.TraceID = "" }, intent.ErrInvalidInstance},
			{"missing classification", func(s *intent.InstanceSpec) { s.Classification = "" }, intent.ErrInvalidInstance},
			{"missing retention class", func(s *intent.InstanceSpec) { s.RetentionClass = "" }, intent.ErrInvalidInstance},
			{"missing subjects", func(s *intent.InstanceSpec) { s.Subjects = nil }, intent.ErrInvalidInstance},
			{"missing control snapshots", func(s *intent.InstanceSpec) { s.ControlSnapshots = intent.ControlSnapshots{} }, intent.ErrInvalidInstance},
			{"partial control snapshots", func(s *intent.InstanceSpec) { s.ControlSnapshots.DLPDecisionDigest = "" }, intent.ErrInvalidInstance},
			{"missing source authority snapshot", func(s *intent.InstanceSpec) { s.SourceAuthoritySnapshotDigest = "" }, intent.ErrInvalidInstance},
			{"untyped payload: no bytes", func(s *intent.InstanceSpec) { s.Request.WireBytes = nil }, intent.ErrUntypedPayload},
			{"untyped payload: no schema", func(s *intent.InstanceSpec) { s.Request.Schema = intent.SchemaRef{} }, intent.ErrUntypedPayload},
			{"untyped payload: no protobuf message name", func(s *intent.InstanceSpec) {
				s.Request.Schema.ProtobufFullName = ""
			}, intent.ErrUntypedPayload},
			{"payload for the wrong schema", func(s *intent.InstanceSpec) {
				s.Request.Schema = schemaOf("hcmnext.rewards.v1.ChangeBasePayRequest")
			}, intent.ErrUntypedPayload},
			{"initiator kind the definition forbids", func(s *intent.InstanceSpec) {
				s.Initiator.Kind = intent.InitiatorSchedule
			}, intent.ErrInitiatorNotAllowed},
			{"execution mode the definition forbids", func(s *intent.InstanceSpec) {
				s.ExecutionMode = intent.ModeExecute
			}, intent.ErrModeNotAllowed},
			{"subject kind the definition forbids", func(s *intent.InstanceSpec) {
				s.Subjects[0].Kind = "LEARNING_ENROLLMENT"
			}, intent.ErrInvalidInstance},
			{"duplicate subject", func(s *intent.InstanceSpec) {
				s.Subjects = append(s.Subjects, s.Subjects[0])
			}, intent.ErrInvalidInstance},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				spec := promoteSpec()
				tc.break_(&spec)
				_, _, err := intent.NewInstance(spec, def, d, countingIDs("aaaaaaaa"), fixedClock())
				if !errors.Is(err, tc.cause) {
					t.Fatalf("invalid envelope accepted, or wrong cause: err=%v want %v", err, tc.cause)
				}
			})
		}
	})

	t.Run("GREEN", func(t *testing.T) {
		inst, evidence, err := intent.NewInstance(promoteSpec(), def, d, nil, fixedClock())
		if err != nil {
			t.Fatalf("create instance: %v", err)
		}
		// UUIDv7: version nibble 7 and RFC 4122 variant.
		if len(inst.IntentID) != 36 {
			t.Fatalf("intent id %q is not a UUID", inst.IntentID)
		}
		if inst.IntentID[14] != '7' {
			t.Fatalf("intent id %q is not version 7", inst.IntentID)
		}
		if v := inst.IntentID[19]; v != '8' && v != '9' && v != 'a' && v != 'b' {
			t.Fatalf("intent id %q has variant nibble %q", inst.IntentID, string(v))
		}
		if inst.CanonicalRequestDigest.Digest == "" {
			t.Fatalf("instance carries no canonical request digest")
		}
		if got, want := inst.CanonicalRequestDigest.ProfileID, digest.ProfileIdempotentRequest; got != want {
			t.Fatalf("request digest minted under profile %q, want %q", got, want)
		}
		if err := d.VerifyRequestDigest(inst); err != nil {
			t.Fatalf("the minted request digest does not verify: %v", err)
		}
		if inst.Lifecycle.Request != lifecycle.RequestDraft ||
			inst.Lifecycle.Execution != lifecycle.ExecutionNotPlanned {
			t.Fatalf("a new instance starts at %v", inst.Lifecycle)
		}
		if inst.InstanceVersion != 1 {
			t.Fatalf("instance version = %d", inst.InstanceVersion)
		}
		// Creation evidence is a record of the creation, not a second copy of
		// the envelope: it carries the identity, digest and lifecycle a later
		// auditor needs and nothing that could drift.
		if evidence.IntentID != inst.IntentID ||
			evidence.RequestDigest != inst.CanonicalRequestDigest ||
			evidence.Definition != def.Ref ||
			evidence.CreatedAt != inst.CreatedAt ||
			evidence.Lifecycle != inst.Lifecycle {
			t.Fatalf("creation evidence does not describe the instance it came from")
		}
	})

	t.Run("the digest is idempotent over the request, not the envelope", func(t *testing.T) {
		spec := promoteSpec()
		first, _, err := intent.NewInstance(spec, def, d, countingIDs("aaaaaaaa"), fixedClock())
		if err != nil {
			t.Fatalf("first: %v", err)
		}
		// A different intent id, correlation id and trace id describe the same
		// request: the idempotent-request digest must not move.
		spec.CorrelationID = "corr:2"
		spec.TraceID = "trace:2"
		second, _, err := intent.NewInstance(spec, def, d, countingIDs("bbbbbbbb"), fixedClock())
		if err != nil {
			t.Fatalf("second: %v", err)
		}
		if first.IntentID == second.IntentID {
			t.Fatalf("two instances share an id")
		}
		if first.CanonicalRequestDigest.Digest != second.CanonicalRequestDigest.Digest {
			t.Fatalf("correlation and trace ids moved the idempotent-request digest")
		}

		// A different idempotency key is a different request.
		spec.IdempotencyKey = "idem:promote:9001:2026-11-01"
		third, _, err := intent.NewInstance(spec, def, d, countingIDs("cccccccc"), fixedClock())
		if err != nil {
			t.Fatalf("third: %v", err)
		}
		if third.CanonicalRequestDigest.Digest == first.CanonicalRequestDigest.Digest {
			t.Fatalf("a different idempotency key produced the same digest")
		}
	})
}

// TestTodo_INTENT_002_Golden pins the canonical request digest of a fixed
// envelope. A canonicalization change that silently moves the digest — and so
// silently breaks idempotency for every stored request — fails here.
func TestTodo_INTENT_002_Golden(t *testing.T) {
	reg := mustRegistry(t)
	d := mustDigester(t)
	def := mustResolve(t, reg, "hcmnext.people.promote_worker/v1")
	inst, evidence, err := intent.NewInstance(promoteSpec(), def, d,
		countingIDs("01234567"), fixedClock())
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	goldenJSON(t, "intent_002_envelope.json", struct {
		IntentID  string
		Digest    digest.Reference
		Lifecycle string
		Evidence  intent.CreationEvidence
	}{
		IntentID:  inst.IntentID,
		Digest:    inst.CanonicalRequestDigest,
		Lifecycle: inst.Lifecycle.String(),
		Evidence:  evidence,
	})
}

// TestTodo_INTENT_002_Race creates envelopes concurrently and asserts that the
// digest for one request is the same in every goroutine and that ids never
// collide. Determinism across processes is the whole point of a canonical
// digest; determinism across goroutines is the cheapest way to notice when it
// is lost.
func TestTodo_INTENT_002_Race(t *testing.T) {
	reg := mustRegistry(t)
	d := mustDigester(t)
	def := mustResolve(t, reg, "hcmnext.people.promote_worker/v1")

	const n = 24
	digests := make([]string, n)
	ids := make([]string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			inst, _, err := intent.NewInstance(promoteSpec(), def, d, nil, fixedClock())
			if err != nil {
				t.Errorf("goroutine %d: %v", i, err)
				return
			}
			digests[i] = inst.CanonicalRequestDigest.Digest
			ids[i] = inst.IntentID
		}(i)
	}
	wg.Wait()

	seen := map[string]bool{}
	for i := 0; i < n; i++ {
		if digests[i] != digests[0] {
			t.Fatalf("goroutine %d produced a different digest for the same request", i)
		}
		if seen[ids[i]] {
			t.Fatalf("intent id %q was minted twice", ids[i])
		}
		seen[ids[i]] = true
	}
}

// TestTodo_INTENT_002_Security proves the envelope cannot be talked into
// widening its own authority: the principal is taken as a derived value and
// never read from the payload, agent initiation grants nothing extra, and a
// definition's initiator and mode lists are the ceiling.
func TestTodo_INTENT_002_Security(t *testing.T) {
	reg := mustRegistry(t)
	d := mustDigester(t)

	t.Run("agent initiation does not increase authority", func(t *testing.T) {
		def := mustResolve(t, reg, "hcmnext.work.approve_proposal/v1")
		spec := promoteSpec()
		spec.Subjects = []intent.SubjectReference{
			{Kind: "PROPOSAL", SubjectID: "proposal:1", AuthorityDomain: "HUMAN_WORK"},
		}
		spec.Request.Schema = schemaOf("hcmnext.work.v1.ApproveProposalRequest")
		spec.ExecutionMode = intent.ModeExecute
		spec.Initiator.Kind = intent.InitiatorAgent
		_, _, err := intent.NewInstance(spec, def, d, nil, fixedClock())
		if !errors.Is(err, intent.ErrInitiatorNotAllowed) {
			t.Fatalf("an agent invoked a human-only definition: %v", err)
		}
	})

	t.Run("a P1A definition cannot be driven into EXECUTE", func(t *testing.T) {
		def := mustResolve(t, reg, "hcmnext.people.promote_worker/v1")
		if def.AllowsMode(intent.ModeExecute) {
			t.Fatalf("%s allows EXECUTE in P1A", def.Ref)
		}
		spec := promoteSpec()
		spec.ExecutionMode = intent.ModeExecute
		if _, _, err := intent.NewInstance(spec, def, d, nil, fixedClock()); !errors.Is(err, intent.ErrModeNotAllowed) {
			t.Fatalf("a P1A definition accepted EXECUTE: %v", err)
		}
	})

	t.Run("the principal is a taken value, never a payload field", func(t *testing.T) {
		def := mustResolve(t, reg, "hcmnext.people.promote_worker/v1")
		spec := promoteSpec()
		// A payload claiming to be from someone else changes nothing: the
		// kernel reads the principal it was handed.
		spec.Request.WireBytes = []byte("principal_id=root")
		inst, _, err := intent.NewInstance(spec, def, d, nil, fixedClock())
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if inst.Initiator != principal() {
			t.Fatalf("initiator was taken from somewhere other than the supplied value: %+v",
				inst.Initiator)
		}
	})

	t.Run("an envelope validated against the wrong definition is rejected", func(t *testing.T) {
		def := mustResolve(t, reg, "hcmnext.people.promote_worker/v1")
		other := mustResolve(t, reg, "hcmnext.operations.detect_drift/v1")
		inst, _, err := intent.NewInstance(promoteSpec(), def, d, nil, fixedClock())
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if err := inst.Validate(other); !errors.Is(err, intent.ErrInvalidInstance) {
			t.Fatalf("an envelope validated cleanly against a different definition: %v", err)
		}
	})
}

// TestTodo_INTENT_002_Mutation perturbs one field of a valid envelope at a time
// and requires the digest to move for every material field and to stay put for
// every non-material one. A digest that ignores a material field would let an
// approval survive a change it was meant to catch.
func TestTodo_INTENT_002_Mutation(t *testing.T) {
	reg := mustRegistry(t)
	d := mustDigester(t)
	def := mustResolve(t, reg, "hcmnext.people.promote_worker/v1")

	baseline, _, err := intent.NewInstance(promoteSpec(), def, d, countingIDs("aaaaaaaa"), fixedClock())
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}

	material := []struct {
		name   string
		mutate func(*intent.InstanceSpec)
	}{
		{"organization scope", func(s *intent.InstanceSpec) { s.OrganizationScopeID = "org:acme-eu:sales" }},
		{"purpose", func(s *intent.InstanceSpec) { s.Purpose = "promotion.off_cycle" }},
		{"subject set", func(s *intent.InstanceSpec) { s.Subjects = s.Subjects[:1] }},
		{"requested effective time", func(s *intent.InstanceSpec) {
			at := values.NewInstant(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
			s.RequestedEffectiveAt = &at
		}},
		{"request payload bytes", func(s *intent.InstanceSpec) {
			s.Request.WireBytes = []byte{0x0a, 0x05, 'o', 't', 'h', 'e', 'r'}
		}},
		{"idempotency key", func(s *intent.InstanceSpec) { s.IdempotencyKey = "idem:other" }},
		{"execution mode", func(s *intent.InstanceSpec) { s.ExecutionMode = intent.ModeShadow }},
	}
	for _, m := range material {
		t.Run("material: "+m.name, func(t *testing.T) {
			spec := promoteSpec()
			m.mutate(&spec)
			got, _, err := intent.NewInstance(spec, def, d, countingIDs("bbbbbbbb"), fixedClock())
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			if got.CanonicalRequestDigest.Digest == baseline.CanonicalRequestDigest.Digest {
				t.Fatalf("changing the %s left the request digest unchanged", m.name)
			}
		})
	}

	nonMaterial := []struct {
		name   string
		mutate func(*intent.InstanceSpec)
	}{
		{"correlation id", func(s *intent.InstanceSpec) { s.CorrelationID = "corr:other" }},
		{"trace id", func(s *intent.InstanceSpec) { s.TraceID = "trace:other" }},
		{"control snapshots", func(s *intent.InstanceSpec) {
			s.ControlSnapshots.PolicyBundleDigest = "policy-bundle-2"
			s.ControlSnapshots.ClassificationTaxonomyDigest = "taxonomy-2"
		}},
		{"risk context digest", func(s *intent.InstanceSpec) { s.RiskContextDigest = "risk-2" }},
	}
	for _, m := range nonMaterial {
		t.Run("non-material: "+m.name, func(t *testing.T) {
			spec := promoteSpec()
			m.mutate(&spec)
			got, _, err := intent.NewInstance(spec, def, d, countingIDs("cccccccc"), fixedClock())
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			if got.CanonicalRequestDigest.Digest != baseline.CanonicalRequestDigest.Digest {
				t.Fatalf("changing the %s moved the request digest; it is revalidated context, "+
					"not material", m.name)
			}
		})
	}
}

// FuzzTodo_INTENT_002 fuzzes the envelope's required-field validation. Any spec
// the kernel accepts must carry every required field and produce a verifiable
// digest; any spec it rejects must fail with a typed cause.
func FuzzTodo_INTENT_002(f *testing.F) {
	f.Add("acme-eu", "org:1", "purpose", "idem", "corr", "trace", "CONF", "RET", uint8(1))
	f.Add("", "", "", "", "", "", "", "", uint8(0))
	f.Add("ACME", "org:1", "purpose", "idem", "corr", "trace", "CONF", "RET", uint8(2))
	f.Fuzz(func(t *testing.T,
		tenant, org, purpose, idem, corr, trace, classification, retention string, mode uint8,
	) {
		reg, err := registryOnce()
		if err != nil {
			t.Fatalf("registry: %v", err)
		}
		d, err := digesterOnce()
		if err != nil {
			t.Fatalf("digester: %v", err)
		}
		def, err := reg.ResolveText("hcmnext.people.promote_worker/v1")
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		spec := promoteSpec()
		spec.Tenant = values.TenantId(tenant)
		spec.OrganizationScopeID = org
		spec.Purpose = purpose
		spec.IdempotencyKey = idem
		spec.CorrelationID = corr
		spec.TraceID = trace
		spec.Classification = classification
		spec.RetentionClass = retention
		spec.ExecutionMode = intent.Mode(mode)

		inst, evidence, err := intent.NewInstance(spec, def, d, nil, fixedClock())
		if err != nil {
			var typed *intent.Error
			if !errors.As(err, &typed) {
				t.Fatalf("rejection %v is not a typed intent error", err)
			}
			return
		}
		for name, value := range map[string]string{
			"organization scope": inst.OrganizationScopeID,
			"purpose":            inst.Purpose,
			"idempotency key":    inst.IdempotencyKey,
			"correlation id":     inst.CorrelationID,
			"trace id":           inst.TraceID,
			"classification":     inst.Classification,
			"retention class":    inst.RetentionClass,
		} {
			if value == "" {
				t.Fatalf("accepted an envelope with an empty %s", name)
			}
		}
		if err := inst.Tenant.Validate(); err != nil {
			t.Fatalf("accepted an envelope with an invalid tenant %q: %v", inst.Tenant, err)
		}
		if !inst.ExecutionMode.Valid() || !def.AllowsMode(inst.ExecutionMode) {
			t.Fatalf("accepted execution mode %s", inst.ExecutionMode)
		}
		if inst.CanonicalRequestDigest.Digest == "" {
			t.Fatalf("accepted an envelope with no request digest")
		}
		if evidence.IntentID != inst.IntentID {
			t.Fatalf("creation evidence describes a different instance")
		}
		if err := d.VerifyRequestDigest(inst); err != nil {
			t.Fatalf("minted a digest that does not verify: %v", err)
		}
		if !strings.HasPrefix(inst.CanonicalRequestDigest.ProfileID, "hcmnext.") {
			t.Fatalf("digest minted under profile %q", inst.CanonicalRequestDigest.ProfileID)
		}
	})
}
