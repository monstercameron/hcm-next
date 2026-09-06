package modeling

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func validDecision() Decision {
	return Decision{
		ID: "contact-ownership", Kind: QuestionOwnership,
		AffectedIntents:   []string{"hcmnext.people.update_contact"},
		AffectedWorkflows: []string{"people.contact_information_update/v1"},
		AccountableOwner:  "people-domain", DecisionDeadline: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		SafeDefault:  DefaultRouteHuman,
		Alternatives: []Alternative{{ID: "person", Description: "person-owned", Consequence: "employment-specific work facts need a relationship"}},
		Consequences: []string{"work and personal facts remain independently scoped"}, Authority: "model-review-board",
		SourceEvidence: []string{"workflow-data-register#unresolved-modeling-decisions"}, InvalidationTrigger: "country policy revision",
		LinkedTodo: "WF-DISC-003", LinkedTests: []string{"TestWorkflowModelingDecisionRejectsImplicitOrUnsafeDefault"},
		Status: StatusOpenOwned, Revision: 1,
	}
}

func TestWorkflowModelingDecisionRejectsImplicitOrUnsafeDefault(t *testing.T) {
	d := validDecision()
	if err := d.Validate(); err != nil {
		t.Fatalf("valid decision: %v", err)
	}
	d.SafeDefault = ""
	if !errors.Is(d.Validate(), ErrUnsafeDefault) {
		t.Fatalf("missing default error = %v", d.Validate())
	}
	d = validDecision()
	d.AffectedWorkflows = nil
	if d.Validate() == nil {
		t.Fatal("missing affected workflow accepted")
	}
}

func TestTodo_WF_DISC_003_Property(t *testing.T) {
	d := validDecision()
	if got, err := d.SafeBoundary(); err != nil || got.Write || got.Action != DefaultRouteHuman {
		t.Fatalf("boundary = %#v, %v", got, err)
	}
	if err := ValidateRegistry([]Decision{d, d}); !errors.Is(err, ErrDuplicateID) {
		t.Fatalf("duplicate error = %v", err)
	}
}

func TestTodo_WF_DISC_003_Golden(t *testing.T) {
	d := validDecision()
	if got := d.Digest(); len(got) != 64 {
		t.Fatalf("digest = %q", got)
	}
	if d.Digest() != d.Digest() {
		t.Fatal("digest is not deterministic")
	}
	if !strings.Contains(d.Explain(), "safe-default=ROUTE_HUMAN") {
		t.Fatalf("explain = %q", d.Explain())
	}
}

func TestTodo_WF_DISC_003_Security(t *testing.T) {
	d := validDecision()
	d.SourceEvidence = []string{"secret-evidence-payload"}
	if strings.Contains(d.Explain(), "secret-evidence-payload") {
		t.Fatal("explain disclosed evidence payload")
	}
}

func TestTodo_WF_DISC_003_Conformance(t *testing.T) {
	for _, status := range []Status{StatusOpenOwned, StatusDecided, StatusDeferred} {
		d := validDecision()
		d.Status = status
		if err := d.Validate(); err != nil {
			t.Fatalf("status %s: %v", status, err)
		}
	}
}

func TestTodo_WF_DISC_003_Mutation(t *testing.T) {
	d := validDecision()
	before := d.Digest()
	d.AffectedIntents[0] = "changed"
	if before == d.Digest() {
		t.Fatal("material mutation retained the old digest")
	}
}
