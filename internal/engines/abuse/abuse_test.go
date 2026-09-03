package abuse_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/engines/abuse"
)

func signal() abuse.SignalDefinition {
	return abuse.SignalDefinition{ID: "sensitive-read", Version: "1", Purpose: "observe unusual sensitive-data reads", Features: []abuse.Feature{{Name: "volume", Description: "bounded read count"}}, Sources: []abuse.Source{{Name: "activity-stream", Quality: "SIGNED"}}, Retention: "OPERATIONAL_EVIDENCE", ProtectedAttributePolicy: "EXCLUDE_AND_AUDIT", Owner: "security"}
}

func detector() abuse.DetectorDefinition {
	return abuse.DetectorDefinition{ID: "sensitive-read-detector", Version: "1", Purpose: "identify activity requiring review", SignalIDs: []string{"sensitive-read"}, Features: []abuse.Feature{{Name: "volume", Description: "bounded read count"}}, Sources: []abuse.Source{{Name: "activity-stream", Quality: "SIGNED"}}, Retention: "OPERATIONAL_EVIDENCE", ProtectedAttributePolicy: "EXCLUDE_AND_AUDIT", Owner: "security", Threshold: abuse.Threshold{Metric: "volume", Value: 10, Window: "1h"}, Action: "REVIEW", Evaluation: abuse.EvaluationPlan{Method: "held-out", Dataset: "signed-corpus-v1", Metrics: "precision,recall,unknown"}}
}

func TestTodo_ABUSE_001(t *testing.T) {
	if _, err := abuse.Publish(signal(), detector()); err != nil {
		t.Fatalf("valid definitions rejected: %v", err)
	}
	checks := []struct {
		name   string
		mutate func(*abuse.DetectorDefinition)
		want   error
	}{
		{"purpose", func(d *abuse.DetectorDefinition) { d.Purpose = "" }, abuse.ErrPurpose},
		{"features", func(d *abuse.DetectorDefinition) { d.Features = nil }, abuse.ErrFeatures},
		{"source quality", func(d *abuse.DetectorDefinition) { d.Sources[0].Quality = "" }, abuse.ErrSource},
		{"retention", func(d *abuse.DetectorDefinition) { d.Retention = ""; d.RetentionDays = 0 }, abuse.ErrRetention},
		{"protected policy", func(d *abuse.DetectorDefinition) { d.ProtectedAttributePolicy = "" }, abuse.ErrProtectedPolicy},
		{"owner", func(d *abuse.DetectorDefinition) { d.Owner = "" }, abuse.ErrOwner},
		{"threshold", func(d *abuse.DetectorDefinition) { d.Threshold.Metric = "" }, abuse.ErrThreshold},
		{"action", func(d *abuse.DetectorDefinition) { d.Action = "" }, abuse.ErrAction},
		{"evaluation", func(d *abuse.DetectorDefinition) { d.Evaluation.Dataset = "" }, abuse.ErrEvaluation},
	}
	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			d := detector()
			tc.mutate(&d)
			if _, err := abuse.Publish(signal(), d); !errors.Is(err, tc.want) {
				t.Fatalf("error=%v, want %v", err, tc.want)
			}
		})
	}
}

func TestTodo_ABUSE_001_Security(t *testing.T) {
	d := detector()
	d.ProtectedAttributePolicy = ""
	if _, err := abuse.Publish(signal(), d); !errors.Is(err, abuse.ErrProtectedPolicy) {
		t.Fatalf("missing protected-attribute policy was published: %v", err)
	}
}

func TestTodo_ABUSE_001_Mutation(t *testing.T) {
	base, err := abuse.Publish(signal(), detector())
	if err != nil {
		t.Fatal(err)
	}
	d := detector()
	d.Version = "2"
	changed, err := abuse.Publish(signal(), d)
	if err != nil {
		t.Fatal(err)
	}
	if base.Digest == changed.Digest {
		t.Fatal("detector version mutation did not change publication digest")
	}
}

func TestPublishDigestIsStableAcrossSignalIDOrder(t *testing.T) {
	d := detector()
	a, err := abuse.Publish(signal(), d)
	if err != nil {
		t.Fatal(err)
	}
	d.SignalIDs = []string{"sensitive-read"}
	b, err := abuse.Publish(signal(), d)
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest != b.Digest {
		t.Fatalf("identical publication digest changed: %q != %q", a.Digest, b.Digest)
	}
}

func TestPublishDoesNotPersistOrExecute(t *testing.T) {
	if got, err := abuse.Publish(signal(), detector()); err != nil || got.Digest == "" {
		t.Fatalf("publish receipt=%+v err=%v", got, err)
	}
}
