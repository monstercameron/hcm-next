package subworkflow

import (
	"strings"
	"testing"
)

func validExpansion() Expansion {
	return Expansion{
		ParentRevision:      "parent/v3#rev42",
		Child:               ChildRef{Workflow: "leave.approval", Version: "v1.4.0"},
		ParentApprovedScope: []string{"leave:read", "leave:approve", "notify:send"},
		CallerScope:         []string{"leave:read", "leave:approve"},
		ChildPolicyScope:    []string{"leave:read"},
		Depth:               1,
		MaxDepth:            3,
		SiblingOrdinal:      0,
		MaxFanout:           4,
		Ancestors:           []ChildRef{{Workflow: "hr.root", Version: "v2.0.0"}},
		Certificate:         "cert/expansion-77",
		WaitMode:            WaitModeWait,
	}
}

func TestTodo_WF_STEP_009(t *testing.T) {
	expanded, err := Expand(validExpansion())
	if err != nil {
		t.Fatalf("valid expansion rejected: %v", err)
	}
	if expanded.PinnedVersion != "v1.4.0" {
		t.Fatalf("version not pinned: %+v", expanded)
	}
	if len(expanded.EffectiveScope) != 1 || expanded.EffectiveScope[0] != "leave:read" {
		t.Fatalf("scope not attenuated to intersection: %+v", expanded)
	}
	if !strings.HasPrefix(expanded.IdempotencyKey, "subwf:") {
		t.Fatalf("idempotency key not bound: %q", expanded.IdempotencyKey)
	}
	again, err := Expand(validExpansion())
	if err != nil {
		t.Fatal(err)
	}
	if again.IdempotencyKey != expanded.IdempotencyKey {
		t.Fatal("identical expansion produced a different idempotency key")
	}

	adversaries := []struct {
		name   string
		mutate func(*Expansion)
		code   error
	}{
		{"unpinned version", func(e *Expansion) { e.Child.Version = "" }, ErrVersionUnpinned},
		{"changed version", func(e *Expansion) { e.PinnedVersion = "v1.3.0" }, ErrVersionChanged},
		{"authority expansion", func(e *Expansion) { e.ChildPolicyScope = []string{"payroll:write"} }, ErrAuthorityExpansion},
		{"depth exceeded", func(e *Expansion) { e.Depth = 4 }, ErrDepthExceeded},
		{"fanout exceeded", func(e *Expansion) { e.SiblingOrdinal = 4 }, ErrFanoutExceeded},
		{"recursive cycle", func(e *Expansion) { e.Ancestors = []ChildRef{{Workflow: "leave.approval", Version: "v9.9.9"}} }, ErrRecursiveCycle},
		{"missing certificate", func(e *Expansion) { e.Certificate = "" }, ErrMissingCertificate},
		{"invalid wait mode", func(e *Expansion) { e.WaitMode = "FIRE_AND_FORGET" }, ErrInvalidWaitMode},
	}
	for _, tc := range adversaries {
		t.Run(tc.name, func(t *testing.T) {
			e := validExpansion()
			tc.mutate(&e)
			if _, err := Expand(e); !IsCode(err, tc.code) {
				t.Fatalf("want %s, got %v", tc.code, err)
			}
		})
	}

	t.Run("depth and fanout bounds are inclusive", func(t *testing.T) {
		e := validExpansion()
		e.Depth = e.MaxDepth
		e.SiblingOrdinal = e.MaxFanout - 1
		if _, err := Expand(e); err != nil {
			t.Fatalf("boundary expansion rejected: %v", err)
		}
	})

	t.Run("detach leaves explicit obligation", func(t *testing.T) {
		e := validExpansion()
		e.WaitMode = WaitModeDetachWithObligation
		expanded, err := Expand(e)
		if err != nil {
			t.Fatal(err)
		}
		obligation, err := Detach(expanded, "corr/parent-42/child-0")
		if err != nil {
			t.Fatal(err)
		}
		if obligation.Child != e.Child || obligation.Correlation == "" || obligation.IdempotencyKey == "" {
			t.Fatalf("obligation lost the child link: %+v", obligation)
		}
		if _, err := Detach(expanded, ""); err == nil {
			t.Fatal("detachment without correlation was accepted")
		}
	})

	t.Run("cancellation reports child truth", func(t *testing.T) {
		cases := []struct {
			name  string
			child CancellableChild
			want  ChildReport
		}{
			{"running cancellable", CancellableChild{Ref: ChildRef{Workflow: "a", Version: "v1.0.0"}, State: StateRunning, Cancellable: true}, ReportCancelled},
			{"running uncancellable", CancellableChild{Ref: ChildRef{Workflow: "a", Version: "v1.0.0"}, State: StateRunning}, ReportCannotCancel},
			{"already complete", CancellableChild{Ref: ChildRef{Workflow: "a", Version: "v1.0.0"}, State: StateSucceeded}, ReportAlreadyCompleted},
			{"compensated", CancellableChild{Ref: ChildRef{Workflow: "a", Version: "v1.0.0"}, State: StateCompensated}, ReportCompensated},
		}
		for _, tc := range cases {
			if got, err := PropagateCancellation(tc.child); err != nil || got != tc.want {
				t.Errorf("%s: got %q, %v; want %q", tc.name, got, err, tc.want)
			}
		}
		if _, err := PropagateCancellation(CancellableChild{State: "VANISHED"}); err == nil {
			t.Error("unknown child state propagated without error")
		}
	})

	t.Run("parent completion preserves child truth", func(t *testing.T) {
		truth := []ChildTruth{
			{Child: ChildRef{Workflow: "leave.approval", Version: "v1.4.0"}, Outcome: ReportCancelled, Mandatory: true},
			{Child: ChildRef{Workflow: "notify.send", Version: "v2.0.0"}, Outcome: ReportAlreadyCompleted},
		}
		completion, err := CompleteParent(truth, []ChildRef{{Workflow: "leave.approval", Version: "v1.4.0"}})
		if err != nil {
			t.Fatalf("completion rejected: %v", err)
		}
		if len(completion.Children) != len(truth) {
			t.Fatalf("completion dropped child truth: %+v", completion)
		}
		for i := range truth {
			if completion.Children[i] != truth[i] {
				t.Fatalf("completion overwrote child truth: %+v", completion.Children[i])
			}
		}
		if _, err := CompleteParent(truth[:1], []ChildRef{{Workflow: "leave.approval", Version: "v1.4.0"}, {Workflow: "missing.child", Version: "v1.0.0"}}); !IsCode(err, ErrMissingChildResult) {
			t.Fatalf("ignored mandatory child result accepted: %v", err)
		}
	})
}
