package evidence

import (
	"context"
	"errors"
	"math/rand"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	evidencev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/evidence/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/cryptoagility"
	kernelevidence "github.com/monstercameron/human-capital-management-suite/internal/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/operations"
)

// TestTodo_EP_EVID_001_Property proves order independence: shuffling the
// dimension-fact input order, and shuffling the purpose policy's own
// allowed-field order, never changes the assembled receipt's digest or the
// redaction outcome. evidence.Assemble is documented to key by name rather
// than input position; this pins that from this package's own call site
// rather than trusting the comment.
func TestTodo_EP_EVID_001_Property(t *testing.T) {
	base := fullLineage(testTenant, "intent-property", "", "")
	receiptA, err := assembleReceipt(base)
	if err != nil {
		t.Fatalf("assembleReceipt: %v", err)
	}

	shuffled := base
	shuffled.Dimensions = append([]DimensionFact(nil), base.Dimensions...)
	rand.New(rand.NewSource(42)).Shuffle(len(shuffled.Dimensions), func(i, j int) {
		shuffled.Dimensions[i], shuffled.Dimensions[j] = shuffled.Dimensions[j], shuffled.Dimensions[i]
	})
	receiptB, err := assembleReceipt(shuffled)
	if err != nil {
		t.Fatalf("assembleReceipt (shuffled): %v", err)
	}
	if receiptA.Digest != receiptB.Digest {
		t.Fatalf("receipt digest depends on dimension input order: %s vs %s", receiptA.Digest, receiptB.Digest)
	}

	allowed := []string{"intent", "request", "proposal", "approvals", "transaction-heads", "reconciliation"}
	shuffledAllowed := append([]string(nil), allowed...)
	rand.New(rand.NewSource(7)).Shuffle(len(shuffledAllowed), func(i, j int) {
		shuffledAllowed[i], shuffledAllowed[j] = shuffledAllowed[j], shuffledAllowed[i]
	})
	r1, redactedNames1, err := redactReceipt(receiptA, allowed, testPurpose)
	if err != nil {
		t.Fatalf("redactReceipt: %v", err)
	}
	r2, redactedNames2, err := redactReceipt(receiptA, shuffledAllowed, testPurpose)
	if err != nil {
		t.Fatalf("redactReceipt (shuffled allow list): %v", err)
	}
	if r1.Digest != r2.Digest {
		t.Fatalf("redaction outcome depends on allow-list order: %s vs %s", r1.Digest, r2.Digest)
	}
	if len(redactedNames1) != len(redactedNames2) {
		t.Fatalf("redacted name count depends on allow-list order: %d vs %d", len(redactedNames1), len(redactedNames2))
	}
}

// TestTodo_EP_EVID_001_Golden pins exact bytes: the assembled receipt digest
// for a fixed, fully-named lineage, and the exact neutralized/preserved
// bytes EXPORT-001's two profiles produce for one formula-injection payload
// on every dangerous leading character clause 3 names.
func TestTodo_EP_EVID_001_Golden(t *testing.T) {
	snap := fullLineage(testTenant, "intent-golden", "", "")
	receipt, err := assembleReceipt(snap)
	if err != nil {
		t.Fatalf("assembleReceipt: %v", err)
	}
	const wantDigest = "sha256:0b0d0168fdb5dd9f3fac4645a480d4b32f1330707d2490caf96455487add8d93"
	if receipt.Digest != wantDigest {
		t.Fatalf("receipt digest = %s, want pinned %s (if this changed intentionally, update the golden literal)", receipt.Digest, wantDigest)
	}

	h := newHarness(t)
	leading := map[string]string{
		"snapshot":         "=formula",
		"simulation":       "+formula",
		"effects":          "-formula",
		"domain-revisions": "@formula",
		"repair":           "\tformula",
		"obligations":      "\rformula",
	}
	seed := snap
	for name, note := range leading {
		seed = withDimension(seed, name, kernelevidence.StatusPresent, "sha256:"+name+"-digest", note)
	}
	_, content := exportOnce(t, h, "intent-golden", testPurpose, "idem-golden-1", seed)
	for name, note := range leading {
		if got := humanNoteFor(t, content.HumanArtifact, name); got != "'"+note {
			t.Fatalf("golden human cell for %s = %q, want %q", name, got, "'"+note)
		}
		if got := machineNoteFor(t, content.MachineArtifact, name); got != note {
			t.Fatalf("golden machine value for %s = %q, want exact %q", name, got, note)
		}
	}
}

// TestTodo_EP_EVID_001_Race proves that many concurrent ExportIntentEvidence
// calls under the same idempotency key create exactly one operation, never
// a race-created duplicate.
func TestTodo_EP_EVID_001_Race(t *testing.T) {
	h := newHarness(t)
	h.dispatch.sync = true
	seed := fullLineage(testTenant, "intent-race", "", "")
	h.lineage.Seed(testTenant, "intent-race", seed)

	const n = 32
	ids := make([]string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := &evidencev1.ExportIntentEvidenceRequest{IdempotencyKey: "idem-race", IntentId: "intent-race", Purpose: testPurpose}
			ctx := admittedContext(t, testSubject, []string{testPurpose}, req, ExportIntentEvidenceProcedure)
			resp, err := h.server.ExportIntentEvidence(ctx, req)
			if err != nil {
				t.Errorf("concurrent ExportIntentEvidence: %v", err)
				return
			}
			ids[i] = resp.GetOperation().GetOperationId()
		}(i)
	}
	wg.Wait()

	first := ids[0]
	if first == "" {
		t.Fatal("no operation id observed")
	}
	for i, id := range ids {
		if id != first {
			t.Fatalf("goroutine %d observed operation id %s, want %s (every concurrent caller must resolve to one export)", i, id, first)
		}
	}
	if creates, _ := h.ops.Writes(); creates != 1 {
		t.Fatalf("concurrent idempotent exports created %d operations, want exactly 1", creates)
	}
}

// TestTodo_EP_EVID_001_Integration reaches a real gRPC transport: a bufconn
// server with the production trusted-context interceptor
// (grpcserver.UnaryInterceptor) hosting both EvidenceService and the shared
// OperationsService (EP-OPS-001) over the same operation journal, dialed by
// a real grpc.ClientConn. It proves ExportIntentEvidence's Operation is
// readable end to end through the one shared long-running-operation
// surface, not just through this package's own direct method calls.
func TestTodo_EP_EVID_001_Integration(t *testing.T) {
	h := newHarness(t)
	h.dispatch.sync = true

	lis := bufconn.Listen(1024 * 1024)
	t.Cleanup(func() { _ = lis.Close() })

	cfg := transport.Config{
		Verifier: trustFixtureVerifier(t),
		Now:      func() time.Time { return time.Unix(10, 0).UTC() },
	}
	grpcSrv := grpc.NewServer(grpc.UnaryInterceptor(grpcserver.UnaryInterceptor(cfg)))
	if err := Register(grpcSrv, Dependencies{
		Receipts: h.receipts, Lineage: h.lineage, Purposes: h.purposes,
		Operations: h.ops, Artifacts: h.artifacts, Idempotency: h.idem,
		PackageKey: h.packKey, Signer: h.signerKey, SigningPolicy: h.policy,
		Dispatcher: h.dispatch, Clock: func() time.Time { return h.now },
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	operations.Register(grpcSrv, operations.Dependencies{Store: h.ops})
	go func() { _ = grpcSrv.Serve(lis) }()
	t.Cleanup(grpcSrv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	evidenceClient := evidencev1.NewEvidenceServiceClient(conn)
	opsClient := evidencev1.NewOperationsServiceClient(conn)

	seed := fullLineage(testTenant, "intent-integration", "", "")
	h.lineage.Seed(testTenant, "intent-integration", seed)

	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer fixture")
	exportResp, err := evidenceClient.ExportIntentEvidence(ctx, &evidencev1.ExportIntentEvidenceRequest{
		IdempotencyKey: "idem-integration-1", IntentId: "intent-integration", Purpose: testPurpose,
	})
	if err != nil {
		t.Fatalf("ExportIntentEvidence over real gRPC: %v", err)
	}
	opID := exportResp.GetOperation().GetOperationId()
	if opID == "" {
		t.Fatal("no operation id returned over real gRPC")
	}

	opResp, err := opsClient.GetOperation(ctx, &evidencev1.GetOperationRequest{OperationId: opID})
	if err != nil {
		t.Fatalf("GetOperation over real gRPC: %v", err)
	}
	if opResp.GetOperation().GetState() != evidencev1.OperationState_OPERATION_STATE_SUCCEEDED {
		t.Fatalf("operation state over real gRPC = %s, want SUCCEEDED", opResp.GetOperation().GetState())
	}
	if opResp.GetOperation().GetResult().GetCanonicalDigest().GetCanonicalBytesArtifactRef() == "" {
		t.Fatal("succeeded operation carries no artifact reference")
	}
}

// TestTodo_EP_EVID_001_Fault proves a downstream failure at any stage of
// the async export fails the operation closed -- terminal FAILED with a
// typed error -- rather than leaving it stuck PENDING/RUNNING or reporting
// a false SUCCEEDED.
func TestTodo_EP_EVID_001_Fault(t *testing.T) {
	t.Run("UnknownIntentFailsClosed", func(t *testing.T) {
		h := newHarness(t)
		req := &evidencev1.ExportIntentEvidenceRequest{IdempotencyKey: "idem-fault-1", IntentId: "does-not-exist", Purpose: testPurpose}
		ctx := admittedContext(t, testSubject, []string{testPurpose}, req, ExportIntentEvidenceProcedure)
		resp, err := h.server.ExportIntentEvidence(ctx, req)
		if err != nil {
			t.Fatalf("ExportIntentEvidence: %v", err)
		}
		h.dispatch.Release(t)
		rec, err := h.ops.Get(context.Background(), testTenant, resp.GetOperation().GetOperationId())
		if err != nil {
			t.Fatal(err)
		}
		if string(rec.State) != "FAILED" {
			t.Fatalf("state = %s, want FAILED", rec.State)
		}
		if rec.Error.GetReasonRef() != reasonNotFound {
			t.Fatalf("reason = %s, want %s", rec.Error.GetReasonRef(), reasonNotFound)
		}
	})

	t.Run("ArtifactSinkFailureFailsClosed", func(t *testing.T) {
		h := newHarness(t)
		seed := fullLineage(testTenant, "intent-artifact-fault", "", "")
		h.lineage.Seed(testTenant, "intent-artifact-fault", seed)
		req := &evidencev1.ExportIntentEvidenceRequest{IdempotencyKey: "idem-fault-2", IntentId: "intent-artifact-fault", Purpose: testPurpose}
		ctx := admittedContext(t, testSubject, []string{testPurpose}, req, ExportIntentEvidenceProcedure)
		resp, err := h.server.ExportIntentEvidence(ctx, req)
		if err != nil {
			t.Fatalf("ExportIntentEvidence: %v", err)
		}
		opID := resp.GetOperation().GetOperationId()
		artifactID := deterministicArtifactID(opID)
		// Pre-occupy the artifact id so the sink's own duplicate guard fires
		// -- a genuine downstream failure, not a fabricated error.
		if _, err := h.artifacts.Put(context.Background(), testTenant, artifactID, []byte("occupied")); err != nil {
			t.Fatal(err)
		}
		h.dispatch.Release(t)
		rec, err := h.ops.Get(context.Background(), testTenant, opID)
		if err != nil {
			t.Fatal(err)
		}
		if string(rec.State) != "FAILED" {
			t.Fatalf("state = %s, want FAILED", rec.State)
		}
	})

	t.Run("UnconfiguredPurposePolicyFailsClosed", func(t *testing.T) {
		h := newHarness(t)
		seed := fullLineage(testTenant, "intent-purpose-fault", "", "")
		h.lineage.Seed(testTenant, "intent-purpose-fault", seed)
		// AuthorizesPurpose succeeds (the principal claims this purpose) but
		// no reviewed field-visibility policy exists for it: a gap between
		// "the caller may declare this purpose" and "we know what it means"
		// must refuse, never default to allow-all or allow-nothing.
		req := &evidencev1.ExportIntentEvidenceRequest{IdempotencyKey: "idem-fault-3", IntentId: "intent-purpose-fault", Purpose: "unconfigured_purpose"}
		ctx := admittedContext(t, testSubject, []string{"unconfigured_purpose"}, req, ExportIntentEvidenceProcedure)
		_, err := h.server.ExportIntentEvidence(ctx, req)
		var owned *envelope.Error
		if !errors.As(err, &owned) || owned.Code() != envelope.CodePermissionDenied {
			t.Fatalf("expected a PERMISSION_DENIED refusal for an unconfigured purpose before any operation is created, got: %v", err)
		}
		if creates, _ := h.ops.Writes(); creates != 0 {
			t.Fatalf("an unconfigured purpose must create zero operations, got %d", creates)
		}
	})
}

// TestTodo_EP_EVID_001_Security proves the tamper-detection clause: a
// flipped byte anywhere in the sealed ciphertext, and a stripped signature,
// both fail OpenAndVerify -- the verifier does not return nil on everything.
func TestTodo_EP_EVID_001_Security(t *testing.T) {
	h := newHarness(t)
	seed := fullLineage(testTenant, "intent-tamper", "", "")
	_, content := exportOnce(t, h, "intent-tamper", testPurpose, "idem-tamper-1", seed)
	_ = content

	// Rebuild a fresh, valid package deterministically so this test can
	// tamper with its own copy without depending on exportOnce's internal
	// artifact plumbing.
	original, sealed := sealFixturePackage(t, h)
	if _, err := OpenAndVerify(sealed, h.packKey, h.verifyKey, h.policy, h.now); err != nil {
		t.Fatalf("a genuine, untampered package failed to verify: %v", err)
	}

	t.Run("FlippedCiphertextByteFailsVerification", func(t *testing.T) {
		tampered := sealed
		tampered.Envelope.Ciphertext = append([]byte(nil), sealed.Envelope.Ciphertext...)
		tampered.Envelope.Ciphertext[0] ^= 0xFF
		if _, err := OpenAndVerify(tampered, h.packKey, h.verifyKey, h.policy, h.now); !errors.Is(err, ErrPackageTampered) {
			t.Fatalf("a flipped ciphertext byte verified successfully (or with the wrong error): %v", err)
		}
	})

	t.Run("StrippedSignatureFailsVerification", func(t *testing.T) {
		tampered := sealed
		tampered.Signature = cryptoagility.Signature{}
		if _, err := OpenAndVerify(tampered, h.packKey, h.verifyKey, h.policy, h.now); err == nil {
			t.Fatal("a package with its signature stripped verified successfully")
		}
	})

	t.Run("WrongKeyFailsVerification", func(t *testing.T) {
		if _, err := OpenAndVerify(sealed, PackageKey{}, h.verifyKey, h.policy, h.now); !errors.Is(err, ErrPackageTampered) {
			t.Fatalf("decrypting with the wrong symmetric key verified successfully (or with the wrong error): %v", err)
		}
	})
	_ = original
}

// TestTodo_EP_EVID_001_Conformance proves both transports this todo wires
// are actually reachable (gRPC registration and the Connect/grpcbridge HTTP
// handler), and that composition fails closed when any dependency is
// missing rather than serving a half-configured cell.
func TestTodo_EP_EVID_001_Conformance(t *testing.T) {
	h := newHarness(t)
	deps := Dependencies{
		Receipts: h.receipts, Lineage: h.lineage, Purposes: h.purposes,
		Operations: h.ops, Artifacts: h.artifacts, Idempotency: h.idem,
		PackageKey: h.packKey, Signer: h.signerKey, SigningPolicy: h.policy,
	}

	grpcSrv := grpc.NewServer()
	if err := Register(grpcSrv, deps); err != nil {
		t.Fatalf("Register: %v", err)
	}
	info := grpcSrv.GetServiceInfo()
	svc, ok := info["hcmnext.evidence.v1.EvidenceService"]
	if !ok {
		t.Fatal("EvidenceService is not registered on the gRPC server")
	}
	methods := map[string]bool{}
	for _, m := range svc.Methods {
		methods[m.Name] = true
	}
	if !methods["GetExecutionReceipt"] || !methods["ExportIntentEvidence"] {
		t.Fatalf("expected methods missing from service info: %+v", methods)
	}

	handler, err := NewHandler(deps)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	if handler == nil {
		t.Fatal("NewHandler returned a nil handler")
	}

	each := func(name string, mutate func(*Dependencies)) {
		t.Run(name, func(t *testing.T) {
			d := deps
			mutate(&d)
			if _, err := newServer(d); !errors.Is(err, ErrNotConfigured) {
				t.Fatalf("expected ErrNotConfigured for missing %s, got %v", name, err)
			}
		})
	}
	each("Receipts", func(d *Dependencies) { d.Receipts = nil })
	each("Lineage", func(d *Dependencies) { d.Lineage = nil })
	each("Purposes", func(d *Dependencies) { d.Purposes = nil })
	each("Operations", func(d *Dependencies) { d.Operations = nil })
	each("Artifacts", func(d *Dependencies) { d.Artifacts = nil })
	each("Idempotency", func(d *Dependencies) { d.Idempotency = nil })
	each("Signer", func(d *Dependencies) { d.Signer = cryptoagility.Key{} })
}

// TestTodo_EP_EVID_001_Mutation proves the six-hop lineage-completeness
// check is actually exercised: a lineage missing one of the six named hops
// (by omission, or by an explicit non-PRESENT status) fails the export,
// exactly like the zero-hop case -- a five-of-six lineage is not "close
// enough".
func TestTodo_EP_EVID_001_Mutation(t *testing.T) {
	for _, hop := range RequiredLineageHops {
		hop := hop
		t.Run("Omitted_"+hop, func(t *testing.T) {
			h := newHarness(t)
			seed := withoutDimension(fullLineage(testTenant, "intent-mut-omit-"+hop, "", ""), hop)
			// evidence.Assemble itself refuses an omitted dimension; prove
			// this reaches the operation as a FAILED terminal state, not a
			// silently-accepted five-of-six export.
			req := &evidencev1.ExportIntentEvidenceRequest{IdempotencyKey: "idem-mut-omit", IntentId: "intent-mut-omit-" + hop, Purpose: testPurpose}
			h.lineage.Seed(testTenant, "intent-mut-omit-"+hop, seed)
			ctx := admittedContext(t, testSubject, []string{testPurpose}, req, ExportIntentEvidenceProcedure)
			resp, err := h.server.ExportIntentEvidence(ctx, req)
			if err != nil {
				t.Fatalf("ExportIntentEvidence: %v", err)
			}
			h.dispatch.Release(t)
			rec, err := h.ops.Get(context.Background(), testTenant, resp.GetOperation().GetOperationId())
			if err != nil {
				t.Fatal(err)
			}
			if string(rec.State) != "FAILED" {
				t.Fatalf("hop %s: state = %s, want FAILED (an incomplete lineage must never succeed)", hop, rec.State)
			}
		})

		t.Run("NotPresent_"+hop, func(t *testing.T) {
			h := newHarness(t)
			seed := withDimension(fullLineage(testTenant, "intent-mut-absent-"+hop, "", ""), hop, kernelevidence.StatusAbsent, "", "not reached this run")
			h.lineage.Seed(testTenant, "intent-mut-absent-"+hop, seed)
			req := &evidencev1.ExportIntentEvidenceRequest{IdempotencyKey: "idem-mut-absent", IntentId: "intent-mut-absent-" + hop, Purpose: testPurpose}
			ctx := admittedContext(t, testSubject, []string{testPurpose}, req, ExportIntentEvidenceProcedure)
			resp, err := h.server.ExportIntentEvidence(ctx, req)
			if err != nil {
				t.Fatalf("ExportIntentEvidence: %v", err)
			}
			h.dispatch.Release(t)
			rec, err := h.ops.Get(context.Background(), testTenant, resp.GetOperation().GetOperationId())
			if err != nil {
				t.Fatal(err)
			}
			if string(rec.State) != "FAILED" {
				t.Fatalf("hop %s ABSENT: state = %s, want FAILED (a five-of-six-present lineage must never succeed)", hop, rec.State)
			}
			if rec.Error.GetReasonRef() != reasonLineageIncomplete {
				t.Fatalf("hop %s ABSENT: reason = %s, want %s", hop, rec.Error.GetReasonRef(), reasonLineageIncomplete)
			}
		})
	}

	// Control: the same fixture with all six hops PRESENT must succeed, so
	// the failures above are proven to be about the missing hop specifically
	// and not some unrelated fixture defect.
	t.Run("AllSixHopsPresentSucceeds", func(t *testing.T) {
		h := newHarness(t)
		seed := fullLineage(testTenant, "intent-mut-control", "", "")
		rec, _ := exportOnce(t, h, "intent-mut-control", testPurpose, "idem-mut-control", seed)
		if string(rec.State) != "SUCCEEDED" {
			t.Fatalf("control fixture state = %s, want SUCCEEDED", rec.State)
		}
	})
}

// -- shared matrix helpers --

func humanNoteFor(t *testing.T, csvBytes []byte, dimension string) string {
	t.Helper()
	return csvCell(t, csvBytes, dimension, "note")
}

func machineNoteFor(t *testing.T, jsonBytes []byte, dimension string) string {
	t.Helper()
	return jsonFieldValue(t, jsonBytes, dimension, "note")
}

func sealFixturePackage(t *testing.T, h *harness) (Content, Package) {
	t.Helper()
	content := Content{
		Manifest: Manifest{
			ContractVersion: ContractVersion, Tenant: testTenant, IntentRef: "intent-fixture",
			RequestorSubject: testSubject, IdempotencyKey: "idem-fixture", Purpose: testPurpose,
			Format: "test-fixture/v1", AllowedFields: []string{"status"}, IssuedAt: h.now, ExpiresAt: h.now.Add(time.Hour),
		},
		HumanArtifact:   []byte("id,status\nintent,PRESENT\n"),
		MachineArtifact: []byte(`{"records":[]}`),
	}
	content.Manifest.HumanContentDigest = digest(content.HumanArtifact)
	content.Manifest.MachineContentDigest = digest(content.MachineArtifact)
	sealedContent, pkg, err := SealAndSign(content, h.packKey, h.signerKey, h.policy)
	if err != nil {
		t.Fatalf("SealAndSign: %v", err)
	}
	return sealedContent, pkg
}
