package abuse_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/engines/abuse"
)

// TestABUSE001ClosureTypedRejectionAndZeroEffect closes the registry red
// clause at the publication boundary: every malformed governed definition is
// refused with machine-readable evidence and no effect counters.
func TestABUSE001ClosureTypedRejectionAndZeroEffect(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*abuse.SignalDefinition, *abuse.DetectorDefinition)
		field  string
		state  string
	}{
		{"signal purpose", func(s *abuse.SignalDefinition, _ *abuse.DetectorDefinition) { s.Purpose = "" }, "purpose", "MISSING"},
		{"detector source", func(_ *abuse.SignalDefinition, d *abuse.DetectorDefinition) { d.Sources[0].Quality = "" }, "source", "MISSING_OR_INVALID"},
		{"protected policy", func(_ *abuse.SignalDefinition, d *abuse.DetectorDefinition) { d.ProtectedAttributePolicy = "" }, "protected_attribute_policy", "MISSING"},
		{"unbound signal", func(_ *abuse.SignalDefinition, d *abuse.DetectorDefinition) { d.SignalIDs = []string{"other-signal"} }, "signal_ids", "UNBOUND"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, d := signal(), detector()
			tc.mutate(&s, &d)
			_, err := abuse.Publish(s, d)
			if err == nil || !abuse.IsRejected(err) || !errors.Is(err, abuse.ErrRejected) {
				t.Fatalf("error=%v, want typed %s", err, abuse.RejectionCode)
			}
			var rej *abuse.Rejection
			if !errors.As(err, &rej) {
				t.Fatalf("error=%v does not carry rejection evidence", err)
			}
			if rej.Code != abuse.RejectionCode || rej.Field != tc.field || rej.State != tc.state || rej.Version != d.Version {
				t.Fatalf("rejection=%+v, want code=%s field=%s state=%s version=%s", rej, abuse.RejectionCode, tc.field, tc.state, d.Version)
			}
			if !rej.Effects.IsZero() {
				t.Fatalf("rejection counted effects: %+v", rej.Effects)
			}
		})
	}

	rev, err := abuse.Publish(signal(), detector())
	if err != nil {
		t.Fatalf("valid publication rejected: %v", err)
	}
	if rev.Digest == "" || !rev.Effects.IsZero() {
		t.Fatalf("publication receipt=%+v, want digest and zero effects", rev)
	}
}
