package disposition_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/disposition"
)

// TestTodo_LEDGER_011_Golden pins the exact classification -> mechanism ->
// state boundary (LEDGER-011's REFACTOR clause: "payload encryption boundary
// is classification-driven") plus the closed vocabulary of PayloadState
// values, as bytes. A future change that reassigns which mechanism a
// classification uses, or that adds a state ReadView can silently return
// without every caller noticing, changes this file and must be a deliberate,
// reviewed edit -- not a side effect of an unrelated refactor.
func TestTodo_LEDGER_011_Golden(t *testing.T) {
	var b strings.Builder

	fmt.Fprintln(&b, "# LEDGER-011 classification -> mechanism -> state")
	for _, c := range []disposition.Classification{
		disposition.ClassificationStandard,
		disposition.ClassificationEncryptedAtRest,
	} {
		mechanism, state, err := disposition.Classify(c)
		if err != nil {
			t.Fatalf("Classify(%q): %v", c, err)
		}
		fmt.Fprintf(&b, "%s -> mechanism=%s state=%s\n", c, mechanism, state)
	}

	fmt.Fprintln(&b, "# unrecognized classifications are refused, never defaulted")
	for _, c := range []disposition.Classification{"", "standard", "ENCRYPTED", "TOP_SECRET"} {
		_, _, err := disposition.Classify(c)
		fmt.Fprintf(&b, "%q -> error=%v\n", c, err != nil)
	}

	fmt.Fprintln(&b, "# declared PayloadState vocabulary")
	for _, s := range []disposition.PayloadState{
		disposition.StatePresent,
		disposition.StateReferenced,
		disposition.StateHeld,
		disposition.StateRestricted,
		disposition.StatePayloadErased,
	} {
		fmt.Fprintf(&b, "%s\n", s)
	}

	got := b.String()
	goldenPath := filepath.Join("testdata", "golden_classification.txt")

	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v", goldenPath, err)
	}
	if got != string(want) {
		t.Fatalf("classification boundary drifted from testdata/golden_classification.txt:\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}

// TestTodo_LEDGER_011_PayloadStateVocabularyAgrees pins the one fact that
// justifies internal/data/ledger.PayloadState duplicating
// disposition.PayloadState's literals instead of importing them (importing
// would cycle: disposition already imports ledger): the four values the two
// types share -- present, referenced, restricted, erased -- must always be
// the same strings. ledger.Reader relies on this directly: it stores
// whatever ledger_payload_disposition.state says verbatim into
// EventRecord.PayloadState, and that column's CHECK constraint only ever
// admits the exact strings disposition.StateRestricted/StatePayloadErased
// name. If this test ever fails, the two vocabularies have drifted and
// ledger.Reader's PayloadState stops matching what disposition.ReadView
// reports for the same event.
func TestTodo_LEDGER_011_PayloadStateVocabularyAgrees(t *testing.T) {
	pairs := []struct {
		name        string
		ledger      ledger.PayloadState
		disposition disposition.PayloadState
	}{
		{"present", ledger.PayloadPresent, disposition.StatePresent},
		{"referenced", ledger.PayloadReferenced, disposition.StateReferenced},
		{"restricted", ledger.PayloadRestricted, disposition.StateRestricted},
		{"erased", ledger.PayloadErased, disposition.StatePayloadErased},
	}
	for _, p := range pairs {
		if string(p.ledger) != string(p.disposition) {
			t.Errorf("%s: ledger.PayloadState %q != disposition.PayloadState %q", p.name, p.ledger, p.disposition)
		}
	}
}
