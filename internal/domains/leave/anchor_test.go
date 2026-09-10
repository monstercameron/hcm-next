package leave_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/leave"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func anchorProcess(t *testing.T) leave.ProcessRequest {
	t.Helper()
	p, err := leave.Bind(request(t), leave.TrustedContext{TenantID: values.TenantId("tenant-a"), OrganizationScopeID: "org:1", PrincipalID: "principal:1"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func anchorCounts(s *leave.AnchorStore) map[leave.AnchorCategory]int {
	return s.CountByCategory()
}

func TestTodo_LEAVE_016(t *testing.T) {
	s := leave.NewAnchorStore()
	p := anchorProcess(t)
	record, created, err := s.Invoke(p)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !created {
		t.Fatal("first invocation did not create the anchor")
	}
	// The anchor persists the intent as CHANGE_REQUEST with its child
	// bindings, the requested revision and the LeaveRequested provenance.
	if record.Family != intent.FamilyChangeRequest || len(record.Children) != 8 {
		t.Fatalf("not a child-bound CHANGE_REQUEST: %+v", record)
	}
	if record.Revision != 1 || record.State != leave.AnchorRequested {
		t.Fatalf("not a requested revision 1: %+v", record)
	}
	if record.IntentID == "" || record.RequestID == "" || record.EventID == "" {
		t.Fatalf("anchor identities missing: %+v", record)
	}
	if record.Tenant != "tenant-a" || record.Principal != "principal:1" || record.OrgScope != "org:1" {
		t.Fatalf("provenance lost: %+v", record)
	}
	if record.WorkerID != "worker:abc-123" || len(record.Evidence) != 1 {
		t.Fatalf("request correlation lost: %+v", record)
	}
	if err := record.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// A duplicate invocation replays the stored anchor: one intent, one
	// request, one chronology entry — never two.
	again, created, err := s.Invoke(p)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if created || again.Digest != record.Digest || again.IntentID != record.IntentID || again.RequestID != record.RequestID || again.EventID != record.EventID {
		t.Fatalf("duplicate forked the anchor: %+v", again)
	}
	counts := anchorCounts(s)
	if counts[leave.AnchorWriteIntent] != 1 || counts[leave.AnchorWriteRequest] != 1 || counts[leave.AnchorWriteChronology] != 1 {
		t.Fatalf("anchor writes = %v", counts)
	}
	// Anchoring changes no workforce state and dispatches no effect.
	for _, category := range []leave.AnchorCategory{leave.AnchorWriteEmployment, leave.AnchorWriteAvailable, leave.AnchorWriteSchedule, leave.AnchorWriteBalance, leave.AnchorWriteEffect} {
		if counts[category] != 0 {
			t.Fatalf("anchor mutated %s", category)
		}
	}
	// RED: unbound and misbound processes refuse.
	if _, _, err := s.Invoke(leave.ProcessRequest{}); err == nil {
		t.Fatal("empty process anchored")
	}
	forged := p
	forged.Family = intent.FamilyCalculationRequest
	if _, _, err := s.Invoke(forged); err == nil {
		t.Fatal("non-CHANGE_REQUEST family anchored")
	}
	// RED: the same client request id with different content conflicts
	// instead of forking a second anchor.
	r := request(t)
	r.Reason = "changed reason"
	clash, err := leave.Bind(r, leave.TrustedContext{TenantID: values.TenantId("tenant-a"), OrganizationScopeID: "org:1", PrincipalID: "principal:1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Invoke(clash); !errors.Is(err, leave.ErrStaleAnchor) {
		t.Fatalf("conflicting reuse error = %v", err)
	}
}

func TestTodo_LEAVE_016_Property(t *testing.T) {
	p := anchorProcess(t)
	// Malformed processes fail closed across the whole binding surface.
	cases := map[string]func(*leave.ProcessRequest){
		"definition": func(q *leave.ProcessRequest) { q.DefinitionType = "hcmnext.other" },
		"version":    func(q *leave.ProcessRequest) { q.DefinitionVersion++ },
		"family":     func(q *leave.ProcessRequest) { q.Family = intent.FamilyAnalyticalRequest },
		"digest":     func(q *leave.ProcessRequest) { q.CanonicalDigest = "" },
		"children":   func(q *leave.ProcessRequest) { q.ChildKinds = q.ChildKinds[:4] },
		"tenant":     func(q *leave.ProcessRequest) { q.Context.TenantID = values.TenantId("") },
		"worker":     func(q *leave.ProcessRequest) { q.Request.WorkerID = "worker-999" },
	}
	for name, mutate := range cases {
		q := p
		mutate(&q)
		if _, _, err := leave.NewAnchorStore().Invoke(q); err == nil {
			t.Fatalf("%s process anchored", name)
		}
	}
	// Evidence order never forks the anchor: canonicalization sorts refs.
	r := request(t)
	r.EvidenceRefs = []string{"evidence:b", "evidence:a"}
	shuffled, err := leave.Bind(r, leave.TrustedContext{TenantID: values.TenantId("tenant-a"), OrganizationScopeID: "org:1", PrincipalID: "principal:1"})
	if err != nil {
		t.Fatal(err)
	}
	r.EvidenceRefs = []string{"evidence:a", "evidence:b"}
	ordered, err := leave.Bind(r, leave.TrustedContext{TenantID: values.TenantId("tenant-a"), OrganizationScopeID: "org:1", PrincipalID: "principal:1"})
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := leave.NewAnchorStore().Invoke(shuffled)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := leave.NewAnchorStore().Invoke(ordered)
	if err != nil {
		t.Fatal(err)
	}
	if first.IntentID != second.IntentID || first.Digest != second.Digest {
		t.Fatal("evidence order forked the anchor")
	}
	// Anchoring is deterministic across stores: identities derive from the
	// request digest, never from fresh randomness.
	other, _, err := leave.NewAnchorStore().Invoke(p)
	if err != nil {
		t.Fatal(err)
	}
	mine, _, err := leave.NewAnchorStore().Invoke(p)
	if err != nil {
		t.Fatal(err)
	}
	if mine.IntentID != other.IntentID || mine.RequestID != other.RequestID || mine.EventID != other.EventID {
		t.Fatal("anchor identities are not deterministic")
	}
}

func TestTodo_LEAVE_016_Race(t *testing.T) {
	s := leave.NewAnchorStore()
	p := anchorProcess(t)
	const workers = 8
	type outcome struct {
		record  leave.AnchorRecord
		created bool
		err     error
	}
	out := make([]outcome, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out[i].record, out[i].created, out[i].err = s.Invoke(p)
		}(i)
	}
	wg.Wait()
	creations := 0
	for i, o := range out {
		if o.err != nil {
			t.Fatalf("racer %d: %v", i, o.err)
		}
		if o.created {
			creations++
		}
		if o.record.Digest != out[0].record.Digest || o.record.IntentID != out[0].record.IntentID {
			t.Fatalf("racer %d diverged: %+v", i, o.record)
		}
	}
	if creations != 1 {
		t.Fatalf("creations = %d, want exactly 1", creations)
	}
	counts := anchorCounts(s)
	if counts[leave.AnchorWriteIntent] != 1 || counts[leave.AnchorWriteRequest] != 1 || counts[leave.AnchorWriteChronology] != 1 {
		t.Fatalf("raced writes = %v", counts)
	}
}

func TestTodo_LEAVE_016_Integration(t *testing.T) {
	s := leave.NewAnchorStore()
	// The full seam: raw request, trusted bind, idempotent invoke, read-back.
	p := anchorProcess(t)
	record, created, err := s.Invoke(p)
	if err != nil || !created {
		t.Fatalf("Invoke = %v, %v, %v", record, created, err)
	}
	stored, ok := s.Get(p.CanonicalDigest)
	if !ok {
		t.Fatal("anchor not readable after invoke")
	}
	if stored.Digest != record.Digest || stored.Family != intent.FamilyChangeRequest || len(stored.Children) != 8 {
		t.Fatalf("stored=%+v", stored)
	}
	if stored.Revision != 1 || stored.State != leave.AnchorRequested {
		t.Fatalf("stored=%+v", stored)
	}
	if stored.Event != leave.AnchorEvent {
		t.Fatalf("event=%q, want LeaveRequested", stored.Event)
	}
	// Mutating the Invoke-returned record never changes the stored anchor.
	record.Children[0] = "mutated"
	record.Evidence[0] = "evidence:mutated"
	fresh, ok := s.Get(p.CanonicalDigest)
	if !ok {
		t.Fatal("anchor vanished")
	}
	if fresh.Children[0] == "mutated" || fresh.Evidence[0] == "evidence:mutated" {
		t.Fatalf("invoke return aliased the store: %+v", fresh)
	}
	// The write log holds exactly the three anchor writes, in order.
	writes := s.Writes()
	if len(writes) != 3 || writes[0].Category != leave.AnchorWriteIntent || writes[1].Category != leave.AnchorWriteRequest || writes[2].Category != leave.AnchorWriteChronology {
		t.Fatalf("writes=%+v", writes)
	}
	if writes[0].Ref != record.IntentID || writes[1].Ref != record.RequestID || writes[2].Ref != record.EventID {
		t.Fatalf("writes=%+v", writes)
	}
	// Stored anchors are frozen snapshots: mutating a read never changes
	// the repository.
	stored.State = "CLOSED"
	stored.Children[0] = "mutated"
	again, ok := s.Get(p.CanonicalDigest)
	if !ok {
		t.Fatal("anchor vanished")
	}
	if again.State != leave.AnchorRequested || again.Children[0] == "mutated" {
		t.Fatalf("stored anchor mutated: %+v", again)
	}
	if err := again.Verify(); err != nil {
		t.Fatalf("Verify after hostile read: %v", err)
	}
}

func TestTodo_LEAVE_016_Mutation(t *testing.T) {
	p := anchorProcess(t)
	base, _, err := leave.NewAnchorStore().Invoke(p)
	if err != nil {
		t.Fatal(err)
	}
	// Any material mutation re-identifies the anchor: worker, mode, reason
	// and evidence each mint a fresh intent, request and event.
	variants := map[string]leave.RequestLeave{}
	r := request(t)
	worker := r
	worker.WorkerID = "worker:xyz-789"
	worker.ClientRequestID = "client-worker"
	variants["worker"] = worker
	mode := r
	mode.Mode = leave.ModeIntermittent
	mode.ClientRequestID = "client-mode"
	variants["mode"] = mode
	reason := r
	reason.Reason = "another reason"
	reason.ClientRequestID = "client-reason"
	variants["reason"] = reason
	evidence := r
	evidence.EvidenceRefs = []string{"evidence:other-2"}
	evidence.ClientRequestID = "client-evidence"
	variants["evidence"] = evidence
	for name, variant := range variants {
		q, err := leave.Bind(variant, leave.TrustedContext{TenantID: values.TenantId("tenant-a"), OrganizationScopeID: "org:1", PrincipalID: "principal:1"})
		if err != nil {
			t.Fatal(err)
		}
		mutated, created, err := leave.NewAnchorStore().Invoke(q)
		if err != nil || !created {
			t.Fatalf("%s: Invoke = %v, %v", name, mutated, err)
		}
		if mutated.IntentID == base.IntentID || mutated.RequestID == base.RequestID || mutated.EventID == base.EventID || mutated.Digest == base.Digest {
			t.Fatalf("%s mutation kept the anchor identity", name)
		}
	}
	// A forged anchor never verifies.
	forged := base
	forged.State = "CLOSED"
	if err := forged.Verify(); err == nil {
		t.Fatal("forged anchor verified")
	}
}
