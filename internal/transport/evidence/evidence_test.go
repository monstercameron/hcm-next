package evidence

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"

	evidencev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/evidence/v1"
	kernelevidence "github.com/monstercameron/human-capital-management-suite/internal/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/operations"
	"google.golang.org/protobuf/proto"
)

// exportOnce is the shared, direct-call export flow every subtest below
// composes: seed a full lineage, call ExportIntentEvidence, release the
// gated dispatcher, and return the completed operation record and the
// opened, verified package content.
func exportOnce(t *testing.T, h *harness, intentID, purpose, idempotencyKey string, seed LineageSnapshot) (operations.Record, Content) {
	t.Helper()
	h.lineage.Seed(testTenant, intentID, seed)
	req := &evidencev1.ExportIntentEvidenceRequest{
		IdempotencyKey: idempotencyKey,
		IntentId:       intentID,
		Purpose:        purpose,
	}
	ctx := admittedContext(t, testSubject, []string{purpose}, req, ExportIntentEvidenceProcedure)
	resp, err := h.server.ExportIntentEvidence(ctx, req)
	if err != nil {
		t.Fatalf("ExportIntentEvidence: %v", err)
	}
	opID := resp.GetOperation().GetOperationId()
	if opID == "" {
		t.Fatal("ExportIntentEvidence returned no operation id")
	}
	h.dispatch.Release(t)

	rec, err := h.ops.Get(context.Background(), testTenant, opID)
	if err != nil {
		t.Fatalf("ops.Get: %v", err)
	}
	if string(rec.State) != "SUCCEEDED" {
		t.Fatalf("operation state = %s, want SUCCEEDED (error=%v)", rec.State, rec.Error)
	}
	ref := rec.Result.GetCanonicalDigest().GetCanonicalBytesArtifactRef()
	raw, err := h.artifacts.Get(context.Background(), testTenant, extractArtifactID(ref))
	if err != nil {
		t.Fatalf("artifacts.Get: %v", err)
	}
	pkg, err := UnmarshalPackage(raw)
	if err != nil {
		t.Fatalf("UnmarshalPackage: %v", err)
	}
	content, err := OpenAndVerify(pkg, h.packKey, h.verifyKey, h.policy, h.now)
	if err != nil {
		t.Fatalf("OpenAndVerify: %v", err)
	}
	return rec, content
}

// decodePackage fetches, unseals and verifies the package one completed
// export operation points at.
func decodePackage(t *testing.T, h *harness, rec operations.Record) Content {
	t.Helper()
	ref := rec.Result.GetCanonicalDigest().GetCanonicalBytesArtifactRef()
	raw, err := h.artifacts.Get(context.Background(), testTenant, extractArtifactID(ref))
	if err != nil {
		t.Fatalf("artifacts.Get: %v", err)
	}
	pkg, err := UnmarshalPackage(raw)
	if err != nil {
		t.Fatalf("UnmarshalPackage: %v", err)
	}
	content, err := OpenAndVerify(pkg, h.packKey, h.verifyKey, h.policy, h.now)
	if err != nil {
		t.Fatalf("OpenAndVerify: %v", err)
	}
	return content
}

// driftingLineageSource returns different content on every read and counts
// its calls. It exists so the ambient-data clause is proven by a test that
// can actually fail: an export that reads its source more than once, or that
// builds any part of its output from a later read, is caught here.
type driftingLineageSource struct {
	mu    sync.Mutex
	base  LineageSnapshot
	calls int
}

func (d *driftingLineageSource) markerFor(generation int) string {
	return fmt.Sprintf("generation-%d", generation)
}

func (d *driftingLineageSource) Calls() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

// Snapshot hands back the base snapshot with this read's generation stamped
// into every dimension digest, so content from two different reads is never
// interchangeable.
func (d *driftingLineageSource) Snapshot(ctx context.Context, tenant, intentID string) (LineageSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return LineageSnapshot{}, err
	}
	d.mu.Lock()
	d.calls++
	generation := d.calls
	d.mu.Unlock()

	out := d.base
	out.Dimensions = make([]DimensionFact, len(d.base.Dimensions))
	copy(out.Dimensions, d.base.Dimensions)
	marker := d.markerFor(generation)
	for i := range out.Dimensions {
		out.Dimensions[i].Digest = out.Dimensions[i].Digest + "-" + marker
	}
	return out, nil
}

// extractArtifactID pulls the id portion back off the mem:// reference
// MemoryArtifactSink.Put returns.
func extractArtifactID(ref string) string {
	parts := strings.Split(ref, "/")
	return parts[len(parts)-1]
}

func isNotFound(t *testing.T, err error) bool {
	t.Helper()
	var owned *envelope.Error
	if !errors.As(err, &owned) {
		return false
	}
	return owned.Code() == envelope.CodeNotFound
}

// TestEvidenceEndpointsEnforcePurposeRedactionManifestAndOperationSemantics
// is EP-EVID-001's PRIMARY test: it proves, in one place, that
// GetExecutionReceipt is purpose- and tenant-scoped and non-disclosing, that
// ExportIntentEvidence binds and redacts its manifest by purpose, that
// EXPORT-001's formula-injection defense is actually composed into the
// human artifact while the machine artifact stays exact, that the export's
// Operation is one idempotent asynchronous resource, that an export writes
// to nothing but its own two targets, and that its receipt is built from
// frozen lineage rather than whatever the world looks like when the export
// runs.
func TestEvidenceEndpointsEnforcePurposeRedactionManifestAndOperationSemantics(t *testing.T) {
	t.Run("ReceiptPurposeAndTenantNonDisclosure", func(t *testing.T) {
		h := newHarness(t)
		h.receipts.Seed(Receipt{
			ReceiptID: "receipt-1", TenantID: testTenant, Purpose: testPurpose,
			IssuedAt: h.now, IntentType: "hcmnext.pay/adjust_pay_rate", IntentVersion: "1",
			Mode: "SIMULATE", RequestState: "SIMULATED", ExecutionState: "NOT_STARTED",
			InputsDigest: "sha256:in", ResultDigest: "sha256:out",
		})

		newReq := func() *evidencev1.GetExecutionReceiptRequest {
			return &evidencev1.GetExecutionReceiptRequest{ReceiptId: "receipt-1"}
		}

		// Visible: right tenant, right purpose. Every call below builds its
		// own fresh request message: transport.Admit overwrites a message's
		// trusted Scope field in place, so reusing one message object across
		// admission calls with different principals would leak the previous
		// call's resolved purpose into the next one.
		visibleReq := newReq()
		ctx := admittedContext(t, testSubject, []string{testPurpose}, visibleReq, GetExecutionReceiptProcedure)
		res, err := h.server.GetExecutionReceipt(ctx, visibleReq)
		if err != nil {
			t.Fatalf("visible receipt read failed: %v", err)
		}
		if res.GetExecutionReceipt().GetReceiptId() != "receipt-1" {
			t.Fatalf("unexpected receipt id: %+v", res)
		}
		if res.GetExecutionReceipt().GetReceipt().GetIntentType() != "hcmnext.pay/adjust_pay_rate" {
			t.Fatalf("receipt content did not round-trip: %+v", res.GetExecutionReceipt().GetReceipt())
		}

		// Not visible: caller not authorized for the receipt's purpose.
		otherPurposeReq := newReq()
		otherPurposeCtx := admittedContext(t, testSubject, []string{"unrelated_purpose"}, otherPurposeReq, GetExecutionReceiptProcedure)
		if _, err := h.server.GetExecutionReceipt(otherPurposeCtx, otherPurposeReq); !isNotFound(t, err) {
			t.Fatalf("purpose-mismatched read did not refuse as NOT_FOUND: %v", err)
		}

		// Not visible: caller in a different tenant sees the same NOT_FOUND,
		// never a distinguishing error that would leak existence.
		crossTenantReq := newReq()
		crossTenantCtx := admittedContextForTenant(t, "other-tenant", testSubject, []string{testPurpose}, crossTenantReq, GetExecutionReceiptProcedure)
		if _, err := h.server.GetExecutionReceipt(crossTenantCtx, crossTenantReq); !isNotFound(t, err) {
			t.Fatalf("cross-tenant read did not refuse as NOT_FOUND: %v", err)
		}

		// Unknown receipt id: the exact same NOT_FOUND, not a different code
		// that would let a caller tell "wrong scope" apart from "no such id".
		missing := &evidencev1.GetExecutionReceiptRequest{ReceiptId: "does-not-exist"}
		missingCtx := admittedContext(t, testSubject, []string{testPurpose}, missing, GetExecutionReceiptProcedure)
		if _, err := h.server.GetExecutionReceipt(missingCtx, missing); !isNotFound(t, err) {
			t.Fatalf("missing receipt did not refuse as NOT_FOUND: %v", err)
		}
	})

	t.Run("ExportBindsManifestAndRedactsByPurpose", func(t *testing.T) {
		h := newHarness(t)
		seed := fullLineage(testTenant, "intent-redact", "", "")
		_, content := exportOnce(t, h, "intent-redact", "redacted_view", "idem-redact-1", seed)

		m := content.Manifest
		if m.Tenant != testTenant || m.IntentRef != "intent-redact" || m.Purpose != "redacted_view" {
			t.Fatalf("manifest did not bind tenant/intent/purpose: %+v", m)
		}
		if m.RequestorSubject != testSubject || m.IdempotencyKey != "idem-redact-1" {
			t.Fatalf("manifest did not bind requestor/idempotency watermark: %+v", m)
		}
		if !m.ExpiresAt.After(m.IssuedAt) {
			t.Fatalf("manifest expiry does not follow issuance: issued=%v expires=%v", m.IssuedAt, m.ExpiresAt)
		}
		if m.Format == "" || m.ContractVersion == "" {
			t.Fatalf("manifest does not bind a declared format: %+v", m)
		}
		wantAllowed := []string{"intent", "request", "proposal", "approvals", "transaction-heads", "reconciliation"}
		sort.Strings(wantAllowed)
		gotAllowed := append([]string(nil), m.AllowedFields...)
		sort.Strings(gotAllowed)
		if fmt.Sprint(gotAllowed) != fmt.Sprint(wantAllowed) {
			t.Fatalf("allowed fields = %v, want %v", gotAllowed, wantAllowed)
		}
		allowSet := map[string]bool{}
		for _, n := range wantAllowed {
			allowSet[n] = true
		}
		var wantRedacted []string
		for _, n := range kernelevidence.Dimensions {
			if !allowSet[n] {
				wantRedacted = append(wantRedacted, n)
			}
		}
		sort.Strings(wantRedacted)
		gotRedacted := append([]string(nil), m.RedactedFields...)
		sort.Strings(gotRedacted)
		if fmt.Sprint(gotRedacted) != fmt.Sprint(wantRedacted) {
			t.Fatalf("redacted fields = %v, want %v", gotRedacted, wantRedacted)
		}
		if m.RedactionReason == "" {
			t.Fatal("redaction applied with no cited reason")
		}
	})

	t.Run("FormulaInjectionNeutralizedInHumanArtifactPreservedInMachineArtifact", func(t *testing.T) {
		h := newHarness(t)
		seed := fullLineage(testTenant, "intent-formula", "", "")
		payloads := map[string]string{
			"snapshot":         "=1+1",
			"simulation":       "+SUM(A1:A9)",
			"effects":          "-2+3",
			"domain-revisions": "@SUM(1,2)",
			"repair":           "\tsneaky",
			"obligations":      "\rsneaky",
		}
		for name, note := range payloads {
			seed = withDimension(seed, name, kernelevidence.StatusPresent, "sha256:"+name+"-digest", note)
		}
		_, content := exportOnce(t, h, "intent-formula", testPurpose, "idem-formula-1", seed)

		rows, err := csv.NewReader(bytes.NewReader(content.HumanArtifact)).ReadAll()
		if err != nil {
			t.Fatalf("parse human CSV: %v", err)
		}
		header := rows[0]
		noteCol := -1
		for i, col := range header {
			if col == "note" {
				noteCol = i
			}
		}
		if noteCol < 0 {
			t.Fatalf("human CSV has no note column: %v", header)
		}
		byID := map[string]string{}
		for _, row := range rows[1:] {
			byID[row[0]] = row[noteCol]
		}
		for name, note := range payloads {
			got, ok := byID[name]
			if !ok {
				t.Fatalf("human CSV missing row for %s", name)
			}
			if !strings.HasPrefix(got, "'") || got[1:] != note {
				t.Fatalf("dimension %s note = %q, want inert %q", name, got, "'"+note)
			}
		}

		var machine struct {
			Records []struct {
				ID     string `json:"id"`
				Fields []struct {
					Name  string `json:"name"`
					Value string `json:"value"`
				} `json:"fields"`
			} `json:"records"`
		}
		if err := json.Unmarshal(content.MachineArtifact, &machine); err != nil {
			t.Fatalf("parse machine JSON: %v", err)
		}
		machineNotes := map[string]string{}
		for _, rec := range machine.Records {
			for _, f := range rec.Fields {
				if f.Name == "note" {
					machineNotes[rec.ID] = f.Value
				}
			}
		}
		for name, note := range payloads {
			if machineNotes[name] != note {
				t.Fatalf("machine artifact altered dimension %s note: got %q, want exact %q", name, machineNotes[name], note)
			}
		}
	})

	t.Run("OperationSemanticsIdempotentAndAsync", func(t *testing.T) {
		h := newHarness(t)
		seed := fullLineage(testTenant, "intent-idem", "", "")
		h.lineage.Seed(testTenant, "intent-idem", seed)
		req := &evidencev1.ExportIntentEvidenceRequest{IdempotencyKey: "idem-shared", IntentId: "intent-idem", Purpose: testPurpose}
		ctx := admittedContext(t, testSubject, []string{testPurpose}, req, ExportIntentEvidenceProcedure)

		first, err := h.server.ExportIntentEvidence(ctx, req)
		if err != nil {
			t.Fatalf("first ExportIntentEvidence: %v", err)
		}
		// GREEN: the request path must not have built the archive
		// synchronously. Before the dispatcher is released, the operation is
		// still PENDING or RUNNING, never SUCCEEDED.
		pending, err := h.ops.Get(context.Background(), testTenant, first.GetOperation().GetOperationId())
		if err != nil {
			t.Fatalf("ops.Get before release: %v", err)
		}
		if string(pending.State) == "SUCCEEDED" {
			t.Fatal("ExportIntentEvidence built the archive synchronously in the request path")
		}

		second, err := h.server.ExportIntentEvidence(ctx, req)
		if err != nil {
			t.Fatalf("second (replayed) ExportIntentEvidence: %v", err)
		}
		if second.GetOperation().GetOperationId() != first.GetOperation().GetOperationId() {
			t.Fatalf("idempotent replay produced a different operation: %s vs %s", second.GetOperation().GetOperationId(), first.GetOperation().GetOperationId())
		}
		if creates, _ := h.ops.Writes(); creates != 1 {
			t.Fatalf("idempotent replay created %d operations, want exactly 1", creates)
		}

		h.dispatch.Release(t)
		final, err := h.ops.Get(context.Background(), testTenant, first.GetOperation().GetOperationId())
		if err != nil {
			t.Fatal(err)
		}
		if string(final.State) != "SUCCEEDED" {
			t.Fatalf("final state = %s, want SUCCEEDED", final.State)
		}
	})

	t.Run("ZeroSourceMutation", func(t *testing.T) {
		h := newHarness(t)
		seed := fullLineage(testTenant, "intent-mutation-count", "", "")
		exportOnce(t, h, "intent-mutation-count", testPurpose, "idem-mutation-1", seed)

		// The zero-source-mutation proof: an export writes to exactly two
		// named targets -- the operation journal (one create, two updates:
		// MarkRunning then Complete) and the artifact sink (one put) -- and
		// nothing else. LineageSource exposes no write method at all, so
		// there is no third target this call could reach even by accident;
		// this asserts the two reachable targets were touched exactly the
		// expected number of times, not merely "the export succeeded".
		creates, updates := h.ops.Writes()
		if creates != 1 {
			t.Fatalf("operation creates = %d, want 1", creates)
		}
		if updates != 2 {
			t.Fatalf("operation updates = %d, want 2 (MarkRunning, Complete)", updates)
		}
		if got := h.artifacts.Puts(); got != 1 {
			t.Fatalf("artifact puts = %d, want 1", got)
		}
	})

	t.Run("AmbientLineageIgnoresCurrentStateMutation", func(t *testing.T) {
		// A receipt describes what happened at execution time. The failure this
		// guards against is an implementation that re-reads its source while
		// building the export, so that a world moving underneath it produces an
		// internally inconsistent artifact -- a manifest digest derived from one
		// read and artifact bodies derived from another.
		//
		// Asserting that mutating a bucket the source never reads changes
		// nothing would be tautological, so instead the source itself drifts:
		// every call returns different content. The export must therefore read
		// exactly once, and every part of its output must come from that one
		// read.
		drift := &driftingLineageSource{base: fullLineage(testTenant, "intent-drift", "", "")}
		h := newHarness(t, withLineageSource(drift))

		req := &evidencev1.ExportIntentEvidenceRequest{
			IdempotencyKey: "idem-drift-1", IntentId: "intent-drift", Purpose: testPurpose,
		}
		ctx := admittedContext(t, testSubject, []string{testPurpose}, req, ExportIntentEvidenceProcedure)
		resp, err := h.server.ExportIntentEvidence(ctx, req)
		if err != nil {
			t.Fatalf("ExportIntentEvidence: %v", err)
		}
		h.dispatch.Release(t)

		rec, err := h.ops.Get(context.Background(), testTenant, resp.GetOperation().GetOperationId())
		if err != nil {
			t.Fatalf("ops.Get: %v", err)
		}
		if string(rec.State) != "SUCCEEDED" {
			t.Fatalf("operation state = %s, want SUCCEEDED (error=%v)", rec.State, rec.Error)
		}

		if got := drift.Calls(); got != 1 {
			t.Fatalf("lineage source read %d times during one export, want exactly 1: every part of the "+
				"export must derive from a single frozen read, or the artifact can disagree with its own manifest", got)
		}

		// And the content that landed is generation 1's, not a later read's.
		content := decodePackage(t, h, rec)
		marker := drift.markerFor(1)
		if !strings.Contains(string(content.MachineArtifact), marker) {
			t.Fatalf("machine artifact does not carry the first read's marker %q", marker)
		}
		if strings.Contains(string(content.MachineArtifact), drift.markerFor(2)) {
			t.Fatal("machine artifact carries a second read's marker: the export re-read its source mid-run")
		}
	})
}

// admittedContextForTenant is admittedContext for a caller in a different
// tenant than the fixture record under test, used to prove cross-tenant
// non-disclosure.
func admittedContextForTenant(t *testing.T, tenant, subject string, purposes []string, message proto.Message, method string) context.Context {
	t.Helper()
	return admittedContextTenant(t, tenant, subject, purposes, message, method)
}
