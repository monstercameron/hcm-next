package inventory

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestProcessingActivity_Validate_AllRequiredAndPolicyBranches(t *testing.T) {
	base := validInventory().Activities[0]
	cases := []struct {
		name   string
		mutate func(ProcessingActivity) ProcessingActivity
	}{
		{"id", func(a ProcessingActivity) ProcessingActivity { a.ID = ""; return a }},
		{"version", func(a ProcessingActivity) ProcessingActivity { a.Version = ""; return a }},
		{"controller", func(a ProcessingActivity) ProcessingActivity { a.Controller = ""; return a }},
		{"processor", func(a ProcessingActivity) ProcessingActivity { a.Processor = ""; return a }},
		{"purpose", func(a ProcessingActivity) ProcessingActivity { a.Purpose = ""; return a }},
		{"lawful basis", func(a ProcessingActivity) ProcessingActivity { a.LawfulBasis = ""; return a }},
		{"retention", func(a ProcessingActivity) ProcessingActivity { a.Retention = ""; return a }},
		{"data subjects", func(a ProcessingActivity) ProcessingActivity { a.DataSubjects = nil; return a }},
		{"data categories", func(a ProcessingActivity) ProcessingActivity { a.DataCategories = nil; return a }},
		{"systems", func(a ProcessingActivity) ProcessingActivity { a.Systems = nil; return a }},
		{"recipients", func(a ProcessingActivity) ProcessingActivity { a.Recipients = nil; return a }},
		{"regions", func(a ProcessingActivity) ProcessingActivity { a.Regions = nil; return a }},
		{"security controls", func(a ProcessingActivity) ProcessingActivity { a.SecurityControls = nil; return a }},
		{"empty list item", func(a ProcessingActivity) ProcessingActivity { a.Systems = []string{" "}; return a }},
		{"unsupported status", func(a ProcessingActivity) ProcessingActivity { a.Status = "UNKNOWN"; return a }},
		{"approved no dpia", func(a ProcessingActivity) ProcessingActivity { a.DPIARef = ""; return a }},
		{"unsupported obligation", func(a ProcessingActivity) ProcessingActivity { a.Obligations[0].Kind = "OTHER"; return a }},
		{"duplicate obligation", func(a ProcessingActivity) ProcessingActivity { a.Obligations[1].Kind = a.Obligations[0].Kind; return a }},
		{"obligation jurisdiction", func(a ProcessingActivity) ProcessingActivity { a.Obligations[0].Jurisdiction = ""; return a }},
		{"obligation release", func(a ProcessingActivity) ProcessingActivity { a.Obligations[0].RulePackRelease = ""; return a }},
		{"obligation deadline", func(a ProcessingActivity) ProcessingActivity { a.Obligations[0].DeadlineHours = 0; return a }},
		{"obligation interval", func(a ProcessingActivity) ProcessingActivity {
			a.Obligations[0].EffectiveTo = a.Obligations[0].EffectiveFrom
			return a
		}},
		{"approved missing obligations", func(a ProcessingActivity) ProcessingActivity { a.Obligations = nil; return a }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := validInventory().Activities[0]
			if err := tc.mutate(candidate).Validate(); !errors.Is(err, ErrInvalid) {
				t.Fatalf("Validate() = %v, want ErrInvalid", err)
			}
		})
	}
	for _, status := range []Status{StatusDraft, StatusApproved, StatusRetired} {
		a := base
		a.Status = status
		if err := a.Validate(); err != nil {
			t.Errorf("status %s rejected: %v", status, err)
		}
	}
	to := base
	to.Obligations[0].EffectiveTo = to.Obligations[0].EffectiveFrom.Add(time.Hour)
	if err := to.Validate(); err != nil {
		t.Fatalf("valid bounded obligation rejected: %v", err)
	}
}

func TestProcessingDataFlow_Validate_AllScopeBranches(t *testing.T) {
	i := validInventory()
	base, activity := i.Flows[0], i.Activities[0]
	cases := []struct {
		name   string
		mutate func(ProcessingDataFlow) ProcessingDataFlow
	}{
		{"id", func(f ProcessingDataFlow) ProcessingDataFlow { f.ID = ""; return f }},
		{"activity id", func(f ProcessingDataFlow) ProcessingDataFlow { f.ActivityID = ""; return f }},
		{"version", func(f ProcessingDataFlow) ProcessingDataFlow { f.Version = ""; return f }},
		{"source", func(f ProcessingDataFlow) ProcessingDataFlow { f.SourceSystem = ""; return f }},
		{"destination", func(f ProcessingDataFlow) ProcessingDataFlow { f.DestinationSystem = ""; return f }},
		{"recipient", func(f ProcessingDataFlow) ProcessingDataFlow { f.Recipient = ""; return f }},
		{"controller", func(f ProcessingDataFlow) ProcessingDataFlow { f.Controller = ""; return f }},
		{"processor", func(f ProcessingDataFlow) ProcessingDataFlow { f.Processor = ""; return f }},
		{"purpose", func(f ProcessingDataFlow) ProcessingDataFlow { f.Purpose = ""; return f }},
		{"retention ref", func(f ProcessingDataFlow) ProcessingDataFlow { f.RetentionRef = ""; return f }},
		{"data categories", func(f ProcessingDataFlow) ProcessingDataFlow { f.DataCategories = nil; return f }},
		{"operations", func(f ProcessingDataFlow) ProcessingDataFlow { f.Operations = nil; return f }},
		{"transfer regions", func(f ProcessingDataFlow) ProcessingDataFlow { f.TransferRegions = nil; return f }},
		{"contract refs", func(f ProcessingDataFlow) ProcessingDataFlow { f.ContractRefs = nil; return f }},
		{"safeguards", func(f ProcessingDataFlow) ProcessingDataFlow { f.Safeguards = nil; return f }},
		{"security controls", func(f ProcessingDataFlow) ProcessingDataFlow { f.SecurityControls = nil; return f }},
		{"interval", func(f ProcessingDataFlow) ProcessingDataFlow { f.EffectiveTo = f.EffectiveFrom; return f }},
		{"source outside systems", func(f ProcessingDataFlow) ProcessingDataFlow { f.SourceSystem = "other"; return f }},
		{"recipient outside activity", func(f ProcessingDataFlow) ProcessingDataFlow { f.Recipient = "other"; return f }},
		{"region outside activity", func(f ProcessingDataFlow) ProcessingDataFlow { f.TransferRegions = []string{"APAC"}; return f }},
		{"category outside activity", func(f ProcessingDataFlow) ProcessingDataFlow { f.DataCategories = []string{"secret"}; return f }},
		{"purpose outside activity", func(f ProcessingDataFlow) ProcessingDataFlow { f.Purpose = "other"; return f }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.mutate(base).Validate(activity); !errors.Is(err, ErrInvalid) {
				t.Fatalf("Validate() = %v, want ErrInvalid", err)
			}
		})
	}
	if err := base.Validate(activity); err != nil {
		t.Fatalf("valid flow rejected: %v", err)
	}
}

func TestDataFlowOccurrence_Validate_AllReceiptBranches(t *testing.T) {
	i := validInventory()
	flow, base := i.Flows[0], i.Occurrences[0]
	cases := []struct {
		name   string
		mutate func(DataFlowOccurrence) DataFlowOccurrence
	}{
		{"id", func(o DataFlowOccurrence) DataFlowOccurrence { o.ID = ""; return o }},
		{"flow id", func(o DataFlowOccurrence) DataFlowOccurrence { o.FlowID = ""; return o }},
		{"recipient", func(o DataFlowOccurrence) DataFlowOccurrence { o.Recipient = ""; return o }},
		{"region", func(o DataFlowOccurrence) DataFlowOccurrence { o.Region = ""; return o }},
		{"received at", func(o DataFlowOccurrence) DataFlowOccurrence { o.ReceivedAt = time.Time{}; return o }},
		{"wrong flow", func(o DataFlowOccurrence) DataFlowOccurrence { o.FlowID = "other"; return o }},
		{"wrong recipient", func(o DataFlowOccurrence) DataFlowOccurrence { o.Recipient = "other"; return o }},
		{"wrong region", func(o DataFlowOccurrence) DataFlowOccurrence { o.Region = "APAC"; return o }},
		{"no categories", func(o DataFlowOccurrence) DataFlowOccurrence { o.DataCategories = nil; return o }},
		{"category outside flow", func(o DataFlowOccurrence) DataFlowOccurrence { o.DataCategories = []string{"secret"}; return o }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.mutate(base).Validate(flow); !errors.Is(err, ErrInvalid) {
				t.Fatalf("Validate() = %v, want ErrInvalid", err)
			}
		})
	}
	if err := base.Validate(flow); err != nil {
		t.Fatalf("valid occurrence rejected: %v", err)
	}
}

func TestInventory_ValidateAndDigest_AllRelationshipBranches(t *testing.T) {
	base := validInventory()
	if err := base.Validate(); err != nil {
		t.Fatalf("valid inventory rejected: %v", err)
	}
	if digest, err := base.Digest(); err != nil || len(digest) != 64 || digest != mustInventoryDigest(t, base) {
		t.Fatalf("Digest() = %q, %v", digest, err)
	}
	cases := []struct {
		name   string
		mutate func(*Inventory)
	}{
		{"duplicate activity", func(i *Inventory) { i.Activities = append(i.Activities, i.Activities[0]) }},
		{"flow without activity version", func(i *Inventory) { i.Flows[0].Version = "missing" }},
		{"duplicate flow", func(i *Inventory) { i.Flows = append(i.Flows, i.Flows[0]) }},
		{"occurrence without flow", func(i *Inventory) { i.Occurrences[0].FlowID = "missing" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := validInventory()
			tc.mutate(&candidate)
			if err := candidate.Validate(); !errors.Is(err, ErrInvalid) {
				t.Fatalf("Validate() = %v, want ErrInvalid", err)
			}
		})
	}
}

func mustInventoryDigest(t *testing.T, i Inventory) string {
	t.Helper()
	d, err := i.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestExecutable_ValidationCanonicalizationAndExplain(t *testing.T) {
	base := validInventory()
	release, err := ValidateExecutable(base)
	if err != nil || release.Digest == "" || !strings.Contains(release.Explain(), release.Digest) {
		t.Fatalf("release = %+v, err=%v", release, err)
	}
	if _, err := ValidateExecutable(Inventory{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty executable error = %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*Inventory)
	}{
		{"flow category", func(i *Inventory) { i.Flows[0].DataCategories = []string{"secret"} }},
		{"flow region", func(i *Inventory) { i.Flows[0].TransferRegions = []string{"APAC"} }},
		{"flow authority", func(i *Inventory) { i.Flows[0].Processor = "other" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := validInventory()
			tc.mutate(&candidate)
			if _, err := ValidateExecutable(candidate); !errors.Is(err, ErrInvalid) {
				t.Fatalf("ValidateExecutable() = %v, want ErrInvalid", err)
			}
		})
	}
	ordered := validInventory()
	ordered.Activities[0].DataCategories = []string{"salary"}
	ordered.Activities[0].Regions = []string{"US", "EU"}
	ordered.Flows[0].Operations = []string{"transmit"}
	if _, err := ValidateExecutable(ordered); err != nil {
		t.Fatalf("ordered valid release rejected: %v", err)
	}
	noFlow := validInventory()
	noFlow.Flows = nil
	noFlow.Occurrences = nil
	if _, err := ValidateExecutable(noFlow); !errors.Is(err, ErrInvalid) {
		t.Fatalf("approved no-flow error = %v", err)
	}
}

func TestValidateExecutable_DoesNotMutateInputOrdering(t *testing.T) {
	i := validInventory()
	i.Activities[0].Obligations[0], i.Activities[0].Obligations[1] = i.Activities[0].Obligations[1], i.Activities[0].Obligations[0]
	wantFirst, wantSecond := i.Activities[0].Obligations[0].Kind, i.Activities[0].Obligations[1].Kind
	if _, err := ValidateExecutable(i); err != nil {
		t.Fatalf("ValidateExecutable: %v", err)
	}
	if i.Activities[0].Obligations[0].Kind != wantFirst || i.Activities[0].Obligations[1].Kind != wantSecond {
		t.Fatalf("ValidateExecutable mutated input obligation order: got %v,%v want %v,%v", i.Activities[0].Obligations[0].Kind, i.Activities[0].Obligations[1].Kind, wantFirst, wantSecond)
	}
}
