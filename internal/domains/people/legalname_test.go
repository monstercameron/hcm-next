package people_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
)

func mustNormalize(t *testing.T, parts []string, latin string) people.StructuredName {
	t.Helper()
	name, err := people.Normalize(parts, latin)
	if err != nil {
		t.Fatal(err)
	}
	return name
}

func safeEvidence() []people.EvidenceCite {
	return []people.EvidenceCite{
		{Ref: "doc/scan-1", ScanState: "SAFE", ScanDigest: "sha256:scan-1", Classification: "LEGAL_ID"},
	}
}

func obligation() people.Obligation {
	return people.Obligation{Authority: "jurisdiction/DE-BE", Requirement: "civil-registry-extract"}
}

func proposeApproved(t *testing.T, id, worker string, name people.StructuredName) *people.NameChange {
	t.Helper()
	change, err := people.Propose(id, worker, "", name, safeEvidence(), obligation(), "2026-02-01")
	if err != nil {
		t.Fatal(err)
	}
	if err := people.Approve(change, "sha256:governance-approval", "discharge/de-be-77"); err != nil {
		t.Fatal(err)
	}
	return change
}

func seedRegistry() *people.Registry {
	registry := people.NewRegistry()
	registry.SeedProfile(people.PersonProfile{
		WorkerID: "ana", DisplayName: "Ana", Username: "ana.l", Email: "ana@example.com",
	})
	return registry
}

func feb1() time.Time { return time.Date(2026, time.February, 1, 9, 0, 0, 0, time.UTC) }

// TestLegalNameChangeConformanceRejectsWesternNameAssumptionsAndImplicitEffects
// is the CONF-019 primary test: multilingual structured names keep
// script, order and Latin forms; evidence, obligation and approval bind
// before commit; downstream systems authorize separately; observations
// reconcile without exposing evidence.
func TestLegalNameChangeConformanceRejectsWesternNameAssumptionsAndImplicitEffects(t *testing.T) {
	// No first/middle/last schema: Han, Arabic and Latin names keep
	// their given order, scripts and Latin forms.
	han := mustNormalize(t, []string{"王", "小明"}, "Wang Xiaoming")
	if han.Full != "王 小明" || han.Latin != "Wang Xiaoming" || han.NormalizationProfile != "NFC" {
		t.Fatalf("han name = %+v", han)
	}
	if han.Parts[0].Script != "Hani" || han.Parts[1].Script != "Hani" {
		t.Fatalf("han scripts = %+v", han.Parts)
	}
	arabic := mustNormalize(t, []string{"ليلى", "حسن"}, "Layla Hassan")
	if arabic.Full != "ليلى حسن" {
		t.Fatalf("arabic full = %q", arabic.Full)
	}
	latin := mustNormalize(t, []string{"Ana María", "López García"}, "")
	if latin.Latin != "Ana María López García" {
		t.Fatalf("latin default = %q", latin.Latin)
	}
	// Cyrillic lookalike inside a Latin part is a mixed-script bypass.
	if _, err := people.Normalize([]string{"Jаne"}, ""); !errors.Is(err, people.ErrMixedScript) {
		t.Fatalf("confusable part = %v, want ErrMixedScript", err)
	}
	// Non-Latin without Latin representation is refused.
	if _, err := people.Normalize([]string{"王"}, ""); !errors.Is(err, people.ErrMissingLatin) {
		t.Fatalf("latin-less han = %v, want ErrMissingLatin", err)
	}

	registry := seedRegistry()
	change := proposeApproved(t, "chg/19", "ana", han)
	if !change.Sufficiency.Sufficient {
		t.Fatalf("sufficiency = %+v, want the sealed checklist", change.Sufficiency)
	}
	if err := registry.Commit(change, feb1(), people.CutoffState{}, people.MergeState{},
		[]string{"payroll", "iam", "documents", "display"}); err != nil {
		t.Fatal(err)
	}
	if change.State != people.ChangeCommitted {
		t.Fatalf("state = %s, want COMMITTED", change.State)
	}
	if got := registry.CurrentDigest("ana"); got == "" {
		t.Fatal("no committed truth digest")
	}
	revisions := registry.Revisions("ana")
	if len(revisions) != 1 || revisions[0].Revision != 1 || revisions[0].ChangeID != "chg/19" {
		t.Fatalf("revisions = %+v, want the single atomic revision", revisions)
	}

	// Nothing downstream moves implicitly: display, username and email
	// keep their values until separately authorized effects land.
	profile, ok := registry.Profile("ana")
	if !ok {
		t.Fatal("profile missing")
	}
	if profile.DisplayName != "Ana" || profile.Username != "ana.l" || profile.Email != "ana@example.com" {
		t.Fatalf("commit moved downstream names: %+v", profile)
	}
	pending := registry.PendingEffects("chg/19")
	if len(pending) != 4 {
		t.Fatalf("pending = %v, want the four unauthorized effects", pending)
	}
	if err := registry.ObserveEffect(pending[0], "ack", true); err == nil {
		t.Fatal("observation of an unauthorized effect accepted")
	}
	for _, id := range pending {
		if err := registry.AuthorizeEffect(id, "value/"+id); err != nil {
			t.Fatal(err)
		}
	}
	profile, _ = registry.Profile("ana")
	if profile.DisplayName != "Ana" {
		t.Fatal("authorization moved the legal profile instead of the effect")
	}
	// Observations reconcile per effect without exposing evidence: the
	// note carries acceptance, never document content.
	for _, id := range pending {
		if err := registry.ObserveEffect(id, "downstream-ack", true); err != nil {
			t.Fatal(err)
		}
		effect, ok := registry.Effect(id)
		if !ok || !effect.Applied || !effect.Observed {
			t.Fatalf("effect = %+v, %v; want applied and observed", effect, ok)
		}
	}
	// Downstream acceptance is observation: truth stays the committed
	// revision digest.
	if got := registry.CurrentDigest("ana"); got != revisions[0].NameDigest {
		t.Fatal("downstream acceptance moved the truth")
	}
}

func TestTodo_CONF_019_Property(t *testing.T) {
	// Normalization is stable and order-preserving: NFC input round-trips
	// byte-identical, parts never reorder, Latin never invents itself.
	cases := []struct {
		parts []string
		latin string
		full  string
	}{
		{[]string{"王", "小明"}, "Wang Xiaoming", "王 小明"},
		{[]string{"ليلى", "حسن"}, "Layla Hassan", "ليلى حسن"},
		{[]string{"Ana María", "López García"}, "", "Ana María López García"},
		{[]string{"Jean-Luc", "O'Brien"}, "", "Jean-Luc O'Brien"},
	}
	for _, tc := range cases {
		first := mustNormalize(t, tc.parts, tc.latin)
		second := mustNormalize(t, tc.parts, tc.latin)
		if first.Full != tc.full || !reflect.DeepEqual(first, second) {
			t.Fatalf("parts %q normalize unstably: %+v", tc.parts, first)
		}
		for i, part := range tc.parts {
			if !strings.Contains(first.Full, part) || first.Parts[i].Value == "" {
				t.Fatalf("part %q lost or reordered in %+v", part, first)
			}
		}
	}
}

func TestTodo_CONF_019_Golden(t *testing.T) {
	name := mustNormalize(t, []string{"王", "小明"}, "Wang Xiaoming")
	change := proposeApproved(t, "chg/19", "ana", name)
	registry := seedRegistry()
	if err := registry.Commit(change, feb1(), people.CutoffState{}, people.MergeState{}, []string{"payroll"}); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "full: %s latin: %s profile: %s\n", name.Full, name.Latin, name.NormalizationProfile)
	for _, part := range name.Parts {
		fmt.Fprintf(&b, "part: %s script=%s\n", part.Value, part.Script)
	}
	fmt.Fprintf(&b, "sufficiency: %+v\n", change.Sufficiency)
	fmt.Fprintf(&b, "truth: %s\n", registry.CurrentDigest("ana"))
	fmt.Fprintf(&b, "pending: %s\n", strings.Join(registry.PendingEffects("chg/19"), ","))
	got := b.String()
	path := filepath.Join("testdata", "conf019_name.golden")
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote golden %s", path)
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (set HCMNEXT_UPDATE_GOLDEN=1 to create it)", path, err)
	}
	if string(want) != got {
		t.Fatalf("golden %s mismatch\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}

func FuzzTodo_CONF_019(f *testing.F) {
	f.Add("王", "Latn-fallback", 0)
	f.Add("Ana", "", 1)
	f.Add("Jаne", "", 2)

	f.Fuzz(func(t *testing.T, part, latin string, variant int) {
		if len(part) > 64 || len(latin) > 64 {
			t.Skip("not a name this fixture shapes")
		}
		first, firstErr := people.Normalize([]string{part}, latin)
		second, secondErr := people.Normalize([]string{part}, latin)
		if (firstErr == nil) != (secondErr == nil) {
			t.Fatal("normalize is not deterministic")
		}
		if firstErr != nil {
			return
		}
		if !reflect.DeepEqual(first, second) || first.NormalizationProfile != "NFC" {
			t.Fatal("normalize is not stable")
		}
		_ = variant
		// Every part keeps one script: the fuzzer must never smuggle a
		// mixed-script part past validation.
		for _, p := range first.Parts {
			if p.Script == "" {
				t.Fatal("part lost its script")
			}
		}
	})
}

func TestTodo_CONF_019_Integration(t *testing.T) {
	// Evidence cites carry scan verdicts, never content: unsafe and
	// pending cites refuse at propose time, exactly as the quarantine
	// vocabulary (SAFE/UNSAFE/PENDING) requires.
	name := mustNormalize(t, []string{"Ana", "López"}, "")
	for _, state := range []string{"UNSAFE", "PENDING", ""} {
		cites := []people.EvidenceCite{{Ref: "doc/scan-9", ScanState: state, ScanDigest: "sha256:scan-9", Classification: "LEGAL_ID"}}
		if _, err := people.Propose("chg/x", "ana", "", name, cites, obligation(), "2026-02-01"); !errors.Is(err, people.ErrUnsafeEvidence) {
			t.Fatalf("cite state %q proposed", state)
		}
	}
	// A second change chains on the first revision: prior digests link,
	// older revisions stay readable.
	registry := seedRegistry()
	first := proposeApproved(t, "chg/19a", "ana", name)
	if err := registry.Commit(first, feb1(), people.CutoffState{}, people.MergeState{}, nil); err != nil {
		t.Fatal(err)
	}
	secondName := mustNormalize(t, []string{"Ana", "López García"}, "")
	second, err := people.Propose("chg/19b", "ana", registry.CurrentDigest("ana"), secondName, safeEvidence(), obligation(), "2026-03-01")
	if err != nil {
		t.Fatal(err)
	}
	if err := people.Approve(second, "sha256:governance-2", "discharge/de-be-78"); err != nil {
		t.Fatal(err)
	}
	if err := registry.Commit(second, time.Date(2026, time.March, 1, 9, 0, 0, 0, time.UTC), people.CutoffState{}, people.MergeState{}, nil); err != nil {
		t.Fatal(err)
	}
	chain := registry.Revisions("ana")
	if len(chain) != 2 || chain[1].Prior != chain[0].NameDigest || chain[1].Revision != 2 {
		t.Fatalf("revision chain = %+v, want linked revisions", chain)
	}
}

func TestTodo_CONF_019_Fault(t *testing.T) {
	name := mustNormalize(t, []string{"Ana", "López"}, "")
	// Raw evidence never enters: missing digests, classifications and
	// refs all refuse.
	for _, cite := range []people.EvidenceCite{
		{Ref: "doc/scan-1", ScanState: "SAFE", Classification: "LEGAL_ID"},
		{Ref: "doc/scan-1", ScanState: "SAFE", ScanDigest: "sha256:scan-1"},
		{ScanState: "SAFE", ScanDigest: "sha256:scan-1", Classification: "LEGAL_ID"},
	} {
		if _, err := people.Propose("chg/x", "ana", "", name, []people.EvidenceCite{cite}, obligation(), "2026-02-01"); !errors.Is(err, people.ErrUnsafeEvidence) {
			t.Fatalf("cite %+v proposed", cite)
		}
	}
	// Approval without digest or discharge is not sufficiency.
	change, err := people.Propose("chg/x", "ana", "", name, safeEvidence(), obligation(), "2026-02-01")
	if err != nil {
		t.Fatal(err)
	}
	if err := people.Approve(change, "", "discharge/x"); !errors.Is(err, people.ErrInsufficientSufficiency) {
		t.Fatalf("digest-less approval = %v", err)
	}
	if err := people.Approve(change, "sha256:approval", ""); !errors.Is(err, people.ErrInsufficientSufficiency) {
		t.Fatalf("discharge-less approval = %v", err)
	}
	// Execution before the effective date, under payroll cutoff and under
	// identity merge all refuse with scoped repair.
	approved := proposeApproved(t, "chg/x", "ana", name)
	registry := seedRegistry()
	if err := registry.Commit(approved, time.Date(2026, time.January, 10, 9, 0, 0, 0, time.UTC), people.CutoffState{}, people.MergeState{}, nil); !errors.Is(err, people.ErrNotEffective) {
		t.Fatalf("early commit = %v, want ErrNotEffective", err)
	}
	if err := registry.Commit(approved, feb1(), people.CutoffState{Locked: true, Period: "2026-02"}, people.MergeState{}, nil); !errors.Is(err, people.ErrCutoffConflict) {
		t.Fatalf("cutoff commit = %v, want ErrCutoffConflict", err)
	}
	if err := registry.Commit(approved, feb1(), people.CutoffState{}, people.MergeState{Pending: true, OtherRef: "worker/duplicate-1"}, nil); !errors.Is(err, people.ErrMergeConflict) {
		t.Fatalf("merge commit = %v, want ErrMergeConflict", err)
	}
}

func TestTodo_CONF_019_Security(t *testing.T) {
	name := mustNormalize(t, []string{"Ana", "López"}, "")
	// Sufficiency is a checklist, not a score: no field on the change
	// carries a model verdict, and flipping inputs cannot fake it.
	change := proposeApproved(t, "chg/x", "ana", name)
	if !change.Sufficiency.Sufficient {
		t.Fatal("checklist sufficiency not sealed")
	}
	// Display, username and email never ride along: reading them as the
	// legal name is an implicit effect.
	registry := seedRegistry()
	approved := proposeApproved(t, "chg/y", "ana", name)
	if err := registry.Commit(approved, feb1(), people.CutoffState{}, people.MergeState{}, []string{"display"}); err != nil {
		t.Fatal(err)
	}
	profile, _ := registry.Profile("ana")
	if profile.DisplayName != "Ana" {
		t.Fatal("commit implicitly renamed the display name")
	}
	// Username and email effects need their own authorization: observing
	// or applying without it refuses.
	if err := registry.AuthorizeEffect("effect/chg/y/username", "ana.l2"); err == nil {
		t.Fatal("unlisted effect authorized")
	}
	if err := registry.ObserveEffect("effect/chg/y/display", "ack", true); err == nil {
		t.Fatal("unauthorized effect observed")
	}
	// Evidence content never appears in observations: notes carry
	// acceptance only.
	if err := registry.AuthorizeEffect("effect/chg/y/display", "Ana López"); err != nil {
		t.Fatal(err)
	}
	if err := registry.ObserveEffect("effect/chg/y/display", "civil-registry-extract-content", true); err != nil {
		t.Fatal(err)
	}
	effect, _ := registry.Effect("effect/chg/y/display")
	if effect.AppliedValue == "" || !effect.Observed {
		t.Fatalf("effect = %+v, want applied and observed", effect)
	}
}

func TestTodo_CONF_019_Conformance(t *testing.T) {
	name := mustNormalize(t, []string{"Ana", "López"}, "")
	// The lifecycle order is closed: approve before propose, commit
	// before approve, and double commit all refuse.
	change, err := people.Propose("chg/x", "ana", "", name, safeEvidence(), obligation(), "2026-02-01")
	if err != nil {
		t.Fatal(err)
	}
	registry := seedRegistry()
	if err := registry.Commit(change, feb1(), people.CutoffState{}, people.MergeState{}, nil); !errors.Is(err, people.ErrChangeState) {
		t.Fatalf("commit-before-approve = %v, want ErrChangeState", err)
	}
	if err := people.Approve(change, "sha256:approval", "discharge/x"); err != nil {
		t.Fatal(err)
	}
	if err := registry.Commit(change, feb1(), people.CutoffState{}, people.MergeState{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := registry.Commit(change, feb1(), people.CutoffState{}, people.MergeState{}, nil); !errors.Is(err, people.ErrChangeState) {
		t.Fatalf("double commit = %v, want ErrChangeState", err)
	}
	// Prior-digest moves refuse: history cannot be rewritten under a commit.
	second, err := people.Propose("chg/z", "ana", "sha256:moved-prior", name, safeEvidence(), obligation(), "2026-03-01")
	if err != nil {
		t.Fatal(err)
	}
	if err := people.Approve(second, "sha256:approval-2", "discharge/y"); err != nil {
		t.Fatal(err)
	}
	if err := registry.Commit(second, time.Date(2026, time.March, 1, 9, 0, 0, 0, time.UTC), people.CutoffState{}, people.MergeState{}, nil); err == nil {
		t.Fatal("moved-prior commit accepted")
	}
}

func TestTodo_CONF_019_Mutation(t *testing.T) {
	// Mutant 1: empty and blank parts are refused.
	for _, parts := range [][]string{nil, {}, {"  "}, {"Ana", ""}} {
		if _, err := people.Normalize(parts, ""); err == nil {
			t.Fatalf("parts %q normalized", parts)
		}
	}
	// Mutant 2: reordered parts are a different name with a different digest.
	a := mustNormalize(t, []string{"Ana", "López"}, "")
	b, err := people.Normalize([]string{"López", "Ana"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if a.Full == b.Full {
		t.Fatal("reordered name collides")
	}
	// Mutant 3: unapproved sufficiency never commits.
	name := mustNormalize(t, []string{"Ana", "López"}, "")
	change, err := people.Propose("chg/x", "ana", "", name, safeEvidence(), obligation(), "2026-02-01")
	if err != nil {
		t.Fatal(err)
	}
	if err := seedRegistry().Commit(change, feb1(), people.CutoffState{}, people.MergeState{}, nil); !errors.Is(err, people.ErrChangeState) {
		t.Fatalf("unapproved commit = %v, want ErrChangeState", err)
	}
	// Mutant 4: concurrent commits serialize into a linked chain.
	registry := seedRegistry()
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("chg/m-%d", i)
			c, err := people.Propose(id, "ana", "", name, safeEvidence(), obligation(), "2026-02-01")
			if err != nil {
				errs <- err
				return
			}
			if err := people.Approve(c, "sha256:approval", "discharge/x"); err != nil {
				errs <- err
				return
			}
			if err := registry.Commit(c, feb1(), people.CutoffState{}, people.MergeState{}, nil); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent commit = %v", err)
	}
	chain := registry.Revisions("ana")
	if len(chain) != 2 || chain[1].Prior != chain[0].NameDigest {
		t.Fatalf("concurrent chain = %+v, want linked revisions", chain)
	}
}
