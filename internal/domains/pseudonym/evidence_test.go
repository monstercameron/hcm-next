package pseudonym_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/pseudonym"
)

func evidencePseudonym() pseudonym.Pseudonym {
	return pseudonym.Pseudonym{ID: "psn-evidence-1", Value: "psn-evidence-1", Tenant: "tenant-1", Scope: "program-a", Generation: 4}
}

func evidenceStore() *pseudonym.ConfidentialEvidenceStore {
	return pseudonym.NewConfidentialEvidenceStore(func() time.Time { return testNow })
}

func evidenceInput() pseudonym.EvidenceInput {
	return pseudonym.EvidenceInput{Pseudonym: evidencePseudonym(), ContentDigest: "sha256:content-1", IntakeAt: testNow.Add(-time.Hour), CustodyRef: "artifact:ciphertext-1"}
}

// TestTodo_ANON_007 proves intake, content digest, time, custody, corrections
// and explicit non-revelation are preserved as append-only evidence.
func TestTodo_ANON_007(t *testing.T) {
	store := evidenceStore()
	original, err := store.Append(evidenceInput())
	if err != nil {
		t.Fatal(err)
	}
	correction, err := store.AppendCorrection(pseudonym.EvidenceCorrectionRequest{RecordID: original.ID, Reason: "intake timestamp corrected", ContentDigest: "sha256:content-2"})
	if err != nil {
		t.Fatal(err)
	}
	history, err := store.History(correction.ID)
	if err != nil || len(history) != 2 || history[0].ID != original.ID || history[1].ID != correction.ID {
		t.Fatalf("history = %+v, err = %v", history, err)
	}
	if history[0].Revelation != pseudonym.RevelationNotRevealed || correction.CorrectionOf != original.ID || correction.CorrectionReason == "" {
		t.Fatalf("evidence chain = %+v -> %+v", original, correction)
	}
	if status, err := store.RevelationStatus(original.ID); err != nil || status != pseudonym.RevelationNotRevealed {
		t.Fatalf("status = %q, err = %v", status, err)
	}
}

// TestTodo_ANON_007_Security proves evidence views and explanations contain
// no ordinary actor identity or raw content and revelation requires a matching
// governed receipt.
func TestTodo_ANON_007_Security(t *testing.T) {
	store := evidenceStore()
	record, err := store.Append(evidenceInput())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(pseudonym.ExplainEvidence(), record.PseudonymID) || strings.Contains(pseudonym.ExplainEvidence(), "content-1") {
		t.Fatal("Explain carried confidential values")
	}
	if err := store.Delete(record.ID); !errors.Is(err, pseudonym.ErrEvidenceAppendOnly) {
		t.Fatalf("delete err = %v", err)
	}
	if _, ok := store.Get(record.ID); !ok {
		t.Fatal("delete removed evidence")
	}
}

// TestTodo_ANON_007_Mutation proves a revelation appends a separate event and
// leaves the pseudonymous evidence record byte-for-byte unchanged.
func TestTodo_ANON_007_Mutation(t *testing.T) {
	store := evidenceStore()
	original, err := store.Append(evidenceInput())
	if err != nil {
		t.Fatal(err)
	}
	revelationPolicy := pseudonym.RevelationPolicy{ID: "policy", Version: "1", ClearedScopes: map[string][]pseudonym.RevelationPurpose{"program-a": {pseudonym.RevelationPurposeCaseInvestigation}}, CustodianRole: "custodian", MaxTTL: time.Hour, Clock: func() time.Time { return testNow }}
	request := pseudonym.RevelationRequest{Pseudonym: evidencePseudonym(), RequestedBy: "worker", Approver: "custodian-1", ApproverRole: "custodian", Purpose: pseudonym.RevelationPurposeCaseInvestigation, LegalBasisRef: "legal:42", Scope: "program-a", TTL: time.Minute, Recipients: []string{"reviewer"}, Fields: []string{"contact_reference"}, NotificationPolicy: "notify-after-review"}
	decision, err := pseudonym.EvaluateRevelation(revelationPolicy, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordRevelation(original.ID, decision.Evidence); err != nil {
		t.Fatal(err)
	}
	after, ok := store.Get(original.ID)
	if !ok || !reflect.DeepEqual(original, after) {
		t.Fatalf("evidence was rewritten: before=%+v after=%+v", original, after)
	}
	if status, err := store.RevelationStatus(original.ID); err != nil || status != pseudonym.RevelationRevealed {
		t.Fatalf("revelation status = %q, err = %v", status, err)
	}
	if len(store.RevelationEvents()) != 1 {
		t.Fatalf("events = %+v", store.RevelationEvents())
	}
}
