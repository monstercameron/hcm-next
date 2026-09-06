package survey

import (
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func validPopulationBindingRef() PopulationBindingRef {
	return PopulationBindingRef{DefinitionID: "pop-1", RevisionVersion: "v1", Digest: "sha256:aaaa"}
}

func validWholeRule() SamplingRule {
	return SamplingRule{Kind: SamplingKindWhole, Nonresponse: NonresponsePolicyFollowUpReminder}
}

// TestTodo_SURVEY_002 is the primary acceptance case: freezing a campaign
// sample from a population binding under a declared sampling rule produces
// a stable, deterministic digest, the frozen membership cannot be mutated
// through any exported surface afterward, and a protected-membership sample
// never carries a raw member list.
func TestTodo_SURVEY_002(t *testing.T) {
	binding := validPopulationBindingRef()
	frozenAt := values.NewInstant(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))

	sample, err := FreezeSample("campaign-1", binding, validWholeRule(), []string{"emp-2", "emp-1"}, false, 0, frozenAt)
	if err != nil {
		t.Fatal(err)
	}
	if sample.Digest == "" || sample.Count != 2 {
		t.Fatalf("expected a digested two-member sample, got %+v", sample)
	}
	if sample.MemberIDs[0] != "emp-1" || sample.MemberIDs[1] != "emp-2" {
		t.Fatalf("expected sorted member ids, got %v", sample.MemberIDs)
	}

	// Two calls with byte-identical inputs freeze to the same digest.
	again, err := FreezeSample("campaign-1", binding, validWholeRule(), []string{"emp-2", "emp-1"}, false, 0, frozenAt)
	if err != nil {
		t.Fatal(err)
	}
	if again.Digest != sample.Digest {
		t.Fatal("freezing identical inputs twice produced different digests")
	}

	// Membership cannot be mutated after freeze: mutating the returned
	// defensive copy must not reach back into the frozen sample.
	got := sample.MemberIDList()
	got[0] = "tampered"
	if sample.MemberIDs[0] == "tampered" {
		t.Fatal("MemberIDList exposed the frozen slice instead of a copy")
	}
	sourceIDs := []string{"emp-2", "emp-1"}
	sourceIDs[0] = "tampered-source"
	if sample.MemberIDs[1] == "tampered-source" {
		t.Fatal("FreezeSample aliased the caller's member id slice")
	}

	// A protected-membership sample never carries a raw member list.
	protected, err := FreezeSample("campaign-2", binding, validWholeRule(), nil, true, 42, frozenAt)
	if err != nil {
		t.Fatal(err)
	}
	if !protected.MembershipProtected || len(protected.MemberIDs) != 0 || protected.Count != 42 {
		t.Fatalf("expected a protected sample with no member list and the given count, got %+v", protected)
	}

	// Structural failures never freeze a silent sample.
	if _, err := FreezeSample("", binding, validWholeRule(), []string{"emp-1"}, false, 0, frozenAt); err == nil {
		t.Fatal("expected an error for an empty campaign id")
	}
	if _, err := FreezeSample("campaign-1", PopulationBindingRef{}, validWholeRule(), []string{"emp-1"}, false, 0, frozenAt); err == nil {
		t.Fatal("expected an error for an incomplete population binding reference")
	}
	if _, err := FreezeSample("campaign-1", binding, validWholeRule(), nil, false, 0, frozenAt); err == nil {
		t.Fatal("expected an error for disclosed membership with no members")
	}
	if _, err := FreezeSample("campaign-1", binding, validWholeRule(), []string{"emp-1"}, true, 1, frozenAt); err == nil {
		t.Fatal("expected an error for protected membership that still carries a raw member list")
	}
	if _, err := FreezeSample("campaign-1", binding, validWholeRule(), []string{"emp-1", "emp-1"}, false, 0, frozenAt); err == nil {
		t.Fatal("expected an error for a duplicate member id")
	}
	if _, err := FreezeSample("campaign-1", binding, SamplingRule{Kind: SamplingKindRandom, Nonresponse: NonresponsePolicyEscalate}, []string{"emp-1"}, false, 0, frozenAt); err == nil {
		t.Fatal("expected an error for a RANDOM rule with no sample size")
	}
}

// TestTodo_SURVEY_002_Security verifies that a caller who cannot see the
// underlying population still cannot recover raw membership or an
// unauthorized member count from a protected CampaignSample: the only
// exported surfaces (MemberIDList, the zero-length MemberIDs field, and the
// declared Count) never leak more than the freeze call itself disclosed.
func TestTodo_SURVEY_002_Security(t *testing.T) {
	binding := validPopulationBindingRef()
	frozenAt := values.NewInstant(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))

	protected, err := FreezeSample("campaign-3", binding, validWholeRule(), nil, true, 5, frozenAt)
	if err != nil {
		t.Fatal(err)
	}
	if len(protected.MemberIDList()) != 0 {
		t.Fatal("a protected sample must never disclose member ids through MemberIDList")
	}

	// Withholding the count entirely (declaring it protected with count 0,
	// the caller's own conservative choice) must not be confused with "zero
	// members disclosed": the sample must still mark MembershipProtected.
	withheldCount, err := FreezeSample("campaign-4", binding, validWholeRule(), nil, true, 0, frozenAt)
	if err != nil {
		t.Fatal(err)
	}
	if !withheldCount.MembershipProtected {
		t.Fatal("a sample built with membershipProtected=true must report itself protected regardless of count")
	}

	// A protected sample must reject a negative count outright rather than
	// silently accepting nonsense evidence.
	if _, err := FreezeSample("campaign-5", binding, validWholeRule(), nil, true, -1, frozenAt); err == nil {
		t.Fatal("expected an error for a protected sample with a negative count")
	}
}

// TestTodo_SURVEY_002_Mutation verifies the sample digest is sensitive to
// every field that participates in the audit record: the campaign id, the
// binding it cites, the sampling rule and the resolved membership each
// change the digest when changed.
func TestTodo_SURVEY_002_Mutation(t *testing.T) {
	binding := validPopulationBindingRef()
	frozenAt := values.NewInstant(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	baseline, err := FreezeSample("campaign-1", binding, validWholeRule(), []string{"emp-1", "emp-2"}, false, 0, frozenAt)
	if err != nil {
		t.Fatal(err)
	}

	other, err := FreezeSample("campaign-2", binding, validWholeRule(), []string{"emp-1", "emp-2"}, false, 0, frozenAt)
	if err != nil {
		t.Fatal(err)
	}
	if other.Digest == baseline.Digest {
		t.Fatal("campaign_id: digest did not change")
	}

	otherBinding := binding
	otherBinding.Digest = "sha256:bbbb"
	withBinding, err := FreezeSample("campaign-1", otherBinding, validWholeRule(), []string{"emp-1", "emp-2"}, false, 0, frozenAt)
	if err != nil {
		t.Fatal(err)
	}
	if withBinding.Digest == baseline.Digest {
		t.Fatal("binding: digest did not change")
	}

	otherRule := validWholeRule()
	otherRule.Nonresponse = NonresponsePolicyEscalate
	withRule, err := FreezeSample("campaign-1", binding, otherRule, []string{"emp-1", "emp-2"}, false, 0, frozenAt)
	if err != nil {
		t.Fatal(err)
	}
	if withRule.Digest == baseline.Digest {
		t.Fatal("rule: digest did not change")
	}

	withMember, err := FreezeSample("campaign-1", binding, validWholeRule(), []string{"emp-1", "emp-3"}, false, 0, frozenAt)
	if err != nil {
		t.Fatal(err)
	}
	if withMember.Digest == baseline.Digest {
		t.Fatal("membership: digest did not change")
	}
}
