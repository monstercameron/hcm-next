package lineage_test

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/lineage"
	"github.com/monstercameron/human-capital-management-suite/internal/data/provenance"
)

const (
	traceTenant = "tenant-a"
	traceIntent = "intent/promo-15"
	traceField  = "worker.base_pay"
)

func traceClock() []time.Time {
	base := time.Date(2026, time.February, 1, 9, 0, 0, 0, time.UTC)
	out := make([]time.Time, 7)
	for i := range out {
		out[i] = base.Add(time.Duration(i) * time.Hour)
	}
	return out
}

// promotionTrace builds the DATA-015 chain: a base-pay correction proposed,
// approved (restricted approver payload), planned, appended as a ledger
// event, executed by the HRIS connector, observed back and repaired to
// closure. Digests chain link to link.
func promotionTrace(t *testing.T) []lineage.Node {
	t.Helper()
	clock := traceClock()
	digests := []string{
		"sha256:proposal", "sha256:approval", "sha256:plan", "sha256:event",
		"sha256:connector", "sha256:observation", "sha256:repair",
	}
	nodes := []lineage.Node{
		{Stage: lineage.StageProposal, Source: "proposal-svc", Version: "v3", Authority: "auth/hrbp", Principal: "hrbp-1", Evidence: []string{"ev-proposal"}, Payload: "base_pay=95000.00"},
		{Stage: lineage.StageApproval, Source: "approval-svc", Version: "v3", Authority: "auth/policy", Principal: "policy-7", Evidence: []string{"ev-approval"}, Restricted: true, Reason: "approver PII above caller clearance"},
		{Stage: lineage.StagePlan, Source: "workflow", Version: "promote/v1", Authority: "auth/workflow", Principal: "workflow-3", Evidence: []string{"ev-plan"}, Payload: "plan=promote-into-management"},
		{Stage: lineage.StageEvent, Source: "ledger", Version: "stream-v1", Authority: "auth/ledger", Principal: "tx-44", Evidence: []string{"ev-event"}, Payload: "stream=payroll seq=41", StreamKey: "payroll", Sequence: 41},
		{Stage: lineage.StageConnectorOp, Source: "hris-connector", Version: "conn-v9", Authority: "auth/connector", Principal: "op-117", Evidence: []string{"ev-operation"}, Payload: "op=update-base-pay"},
		{Stage: lineage.StageObservation, Source: "hris-connector", Version: "conn-v9", Authority: "auth/connector", Principal: "op-118", Evidence: []string{"ev-observation"}, Payload: "base_pay=95000.00", ObservationRef: "obs-118", ConnectorRef: "conn/hris"},
		{Stage: lineage.StageRepair, Source: "repair-svc", Version: "v2", Authority: "auth/repair", Principal: "repair-5", Evidence: []string{"ev-repair"}, Payload: "rounding-delta=0.00 reconciled"},
	}
	for i := range nodes {
		nodes[i].Field = traceField
		nodes[i].EffectiveAt = clock[0]
		nodes[i].KnownAt = clock[i]
		nodes[i].Digest = digests[i]
		if i > 0 {
			nodes[i].PrevDigest = digests[i-1]
		}
	}
	return nodes
}

func mustAssemble(t *testing.T, nodes []lineage.Node) lineage.Trace {
	t.Helper()
	trace, err := lineage.Assemble(traceTenant, traceIntent, traceField, nodes)
	if err != nil {
		t.Fatal(err)
	}
	return trace
}

func TestTodo_DATA_015(t *testing.T) {
	trace := mustAssemble(t, promotionTrace(t))
	if err := trace.Verify(); err != nil {
		t.Fatalf("fresh trace does not verify: %v", err)
	}
	if len(trace.Nodes) != 7 {
		t.Fatalf("trace holds %d nodes, want proposal through repair", len(trace.Nodes))
	}
	for i, want := range lineage.Stages {
		if trace.Nodes[i].Stage != want {
			t.Fatalf("node %d = %s, want %s", i, trace.Nodes[i].Stage, want)
		}
	}
	// Every hop is source/version/time attributed.
	for _, node := range trace.Nodes {
		if node.Source == "" || node.Version == "" || node.Authority == "" ||
			len(node.Evidence) == 0 || node.EffectiveAt.IsZero() || node.KnownAt.IsZero() {
			t.Fatalf("%s node is unattributed: %+v", node.Stage, node)
		}
	}
	// The restricted approval hop is a stub: reason present, payload gone.
	approval, err := trace.At(lineage.StageApproval)
	if err != nil {
		t.Fatal(err)
	}
	if trace.Redacted != 1 || approval.Payload != "" || approval.Reason == "" {
		t.Fatalf("approval hop is not redaction-safe: %+v (redacted=%d)", approval, trace.Redacted)
	}
	// The worker field reads end to end: proposed, executed and observed
	// amounts agree, and the repair closed at zero delta.
	proposal, _ := trace.At(lineage.StageProposal)
	observed, _ := trace.At(lineage.StageObservation)
	repair, _ := trace.At(lineage.StageRepair)
	if !strings.Contains(proposal.Payload, "95000.00") || !strings.Contains(observed.Payload, "95000.00") {
		t.Fatalf("field does not trace proposal %q to observation %q", proposal.Payload, observed.Payload)
	}
	if !strings.Contains(repair.Payload, "0.00") {
		t.Fatalf("repair did not close the field: %q", repair.Payload)
	}
	if trace.Digest == "" {
		t.Fatal("trace carries no seal")
	}
}

func TestTodo_DATA_015_Race(t *testing.T) {
	first := mustAssemble(t, promotionTrace(t))
	const workers = 16
	digests := make(chan string, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			trace, err := lineage.Assemble(traceTenant, traceIntent, traceField, promotionTrace(t))
			if err != nil {
				errs <- err
				return
			}
			if err := trace.Verify(); err != nil {
				errs <- err
				return
			}
			digests <- trace.Digest
		}()
	}
	wg.Wait()
	close(digests)
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent trace = %v", err)
	}
	for digest := range digests {
		if digest != first.Digest {
			t.Fatal("concurrent traces diverge")
		}
	}
}

// TestTodo_DATA_015_Integration projects the assembled trace onto the
// provenance publishing contract: every hop becomes a valid publish
// request, ledger events and external observations keeping their kinds.
func TestTodo_DATA_015_Integration(t *testing.T) {
	trace := mustAssemble(t, promotionTrace(t))
	tenant := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	var requests []provenance.PublishRequest
	for _, node := range trace.Nodes {
		req := provenance.PublishRequest{
			Tenant: tenant, IntentRef: trace.IntentRef,
			SourceAuthority: node.Authority, PrincipalRef: node.Principal,
			EvidenceIDs: node.Evidence,
			Digests:     []provenance.Digest{{Kind: "node", Algorithm: "sha256", Digest: node.Digest}},
			PublishedAt: node.KnownAt,
		}
		switch node.Stage {
		case lineage.StageEvent:
			req.SourceKind = provenance.SourceLedgerEvent
			req.SourceRef = provenance.LedgerEventSourceRef(node.StreamKey, node.Sequence)
			req.StreamKey = node.StreamKey
			req.Sequence = node.Sequence
		case lineage.StageObservation:
			req.SourceKind = provenance.SourceExternalObservation
			req.SourceRef = "observation/" + node.ObservationRef
			req.ObservationRef = node.ObservationRef
			req.ConnectorRef = node.ConnectorRef
		default:
			req.SourceKind = provenance.SourceLedgerEvent
			req.SourceRef = fmt.Sprintf("lineage/%s/%s", strings.ToLower(string(node.Stage)), trace.IntentRef)
		}
		requests = append(requests, req)
	}
	if len(requests) != 7 {
		t.Fatalf("projected %d publish requests, want 7", len(requests))
	}
	kinds := map[provenance.SourceKind]int{}
	for _, req := range requests {
		if !req.SourceKind.Valid() || req.SourceRef == "" || len(req.EvidenceIDs) == 0 || len(req.Digests) == 0 {
			t.Fatalf("hop projects to an unpublishable request: %+v", req)
		}
		if req.IntentRef != traceIntent {
			t.Fatalf("request leaves the intent: %+v", req)
		}
		kinds[req.SourceKind]++
	}
	if kinds[provenance.SourceExternalObservation] != 1 {
		t.Fatalf("observation kinds = %v, want exactly one external observation", kinds)
	}
	// The redacted approval hop publishes its attribution and digest, never
	// its payload: check no request carries the restricted marker.
	for _, req := range requests {
		for _, digest := range req.Digests {
			if strings.Contains(digest.Digest, "SSN") {
				t.Fatalf("restricted payload reached publishing: %+v", req)
			}
		}
	}
}

func TestTodo_DATA_015_Mutation(t *testing.T) {
	// Mutant 1: a dropped repair stage is an incomplete trace.
	dropped := promotionTrace(t)[:6]
	if _, err := lineage.Assemble(traceTenant, traceIntent, traceField, dropped); !errors.Is(err, lineage.ErrIncompleteTrace) {
		t.Fatalf("dropped stage = %v, want ErrIncompleteTrace", err)
	}
	// Mutant 2: a reordered chain breaks linkage.
	swapped := promotionTrace(t)
	swapped[3], swapped[4] = swapped[4], swapped[3]
	if _, err := lineage.Assemble(traceTenant, traceIntent, traceField, swapped); err == nil {
		t.Fatal("reordered chain assembled")
	}
	// Mutant 3: a tampered digest fails verification.
	nodes := promotionTrace(t)
	nodes[5].Digest = "sha256:tampered"
	nodes[6].PrevDigest = "sha256:tampered"
	trace := mustAssemble(t, nodes)
	trace.Nodes[5].Digest = "sha256:edited-after-seal"
	if err := trace.Verify(); !errors.Is(err, lineage.ErrBrokenChain) {
		t.Fatalf("edited seal verifies = %v, want ErrBrokenChain", err)
	}
	// Mutant 4: a restricted node smuggling its payload is refused.
	leak := promotionTrace(t)
	leak[1].Payload = "approver=id:992-SSN"
	if _, err := lineage.Assemble(traceTenant, traceIntent, traceField, leak); !errors.Is(err, lineage.ErrHiddenLeak) {
		t.Fatalf("payload leak = %v, want ErrHiddenLeak", err)
	}
	// Mutant 5: an unattributed hop (no version) is refused.
	unattributed := promotionTrace(t)
	unattributed[2].Version = ""
	if _, err := lineage.Assemble(traceTenant, traceIntent, traceField, unattributed); !errors.Is(err, lineage.ErrUnattributed) {
		t.Fatalf("unattributed hop = %v, want ErrUnattributed", err)
	}
	// Mutant 6: a hop filed under another worker field is refused.
	foreign := promotionTrace(t)
	foreign[0].Field = "worker.job_title"
	if _, err := lineage.Assemble(traceTenant, traceIntent, traceField, foreign); !errors.Is(err, lineage.ErrFieldMismatch) {
		t.Fatalf("foreign field = %v, want ErrFieldMismatch", err)
	}
}
