package effectivedate

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// testCalendar is the versioned business calendar the fixtures are dated under.
func testCalendar() values.CalendarRef {
	return values.CalendarRef{Ref: "test.business", Version: "2026.1"}
}

// date parses an ISO date.
func date(t *testing.T, text string) values.LocalDate {
	t.Helper()
	d, err := values.ParseLocalDate(text)
	if err != nil {
		t.Fatalf("parse date %q: %v", text, err)
	}
	return d
}

// instant parses an RFC3339 timestamp.
func instant(t *testing.T, text string) values.Instant {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatalf("parse instant %q: %v", text, err)
	}
	return values.NewInstant(parsed)
}

// known wraps a timestamp as a knowledge time.
func known(t *testing.T, text string) values.KnownAt {
	t.Helper()
	k, err := values.NewKnownAt(instant(t, text))
	if err != nil {
		t.Fatalf("known at %q: %v", text, err)
	}
	return k
}

// recorded wraps a timestamp as a recorded time.
func recorded(t *testing.T, text string) values.RecordedAt {
	t.Helper()
	r, err := values.NewRecordedAt(instant(t, text))
	if err != nil {
		t.Fatalf("recorded at %q: %v", text, err)
	}
	return r
}

// coord builds one coordinate. An empty end means an open-ended interval.
func coord(t *testing.T, id, start, end, knownText string, supersedes string) Coordinate {
	t.Helper()
	var iv values.EffectiveInterval
	var err error
	if end == "" {
		iv, err = values.NewOpenLocalDateInterval(date(t, start), testCalendar())
	} else {
		iv, err = values.NewLocalDateInterval(date(t, start), date(t, end), testCalendar())
	}
	if err != nil {
		t.Fatalf("interval for %s: %v", id, err)
	}
	return Coordinate{
		ID:         id,
		Effective:  iv,
		KnownAt:    known(t, knownText),
		RecordedAt: recorded(t, knownText),
		Supersedes: supersedes,
	}
}

// fixture is the specification's compensation timeline: a January value, a
// June supersession, an August correction to the June value, and a future
// September value.
func fixture(t *testing.T) []Coordinate {
	t.Helper()
	return []Coordinate{
		coord(t, "a4", "2026-09-01", "", "2026-08-20T00:00:00Z", ""),
		coord(t, "a2", "2026-06-01", "2026-09-01", "2026-05-20T00:00:00Z", ""),
		coord(t, "a1", "2026-01-01", "2026-06-01", "2025-12-15T00:00:00Z", ""),
		coord(t, "a3", "2026-06-01", "2026-09-01", "2026-08-14T00:00:00Z", "a2"),
	}
}

// TestTimelineSeparatesEffectiveFromKnownTime proves the two questions stay
// separate and that the correction wins only once it is known.
func TestTimelineSeparatesEffectiveFromKnownTime(t *testing.T) {
	line, err := NewTimeline(fixture(t))
	if err != nil {
		t.Fatalf("NewTimeline: %v", err)
	}
	if line.Len() != 4 {
		t.Fatalf("timeline holds %d coordinate(s)", line.Len())
	}

	// The order is a property of the coordinates, not of the input slice.
	wantOrder := []string{"a1", "a2", "a3", "a4"}
	for i, c := range line.Ordered() {
		if c.ID != wantOrder[i] {
			t.Fatalf("order = %v, want %v", ids(line.Ordered()), wantOrder)
		}
	}

	for _, tc := range []struct {
		on, cut string
		want    string
		found   bool
	}{
		{"2026-03-01", "2026-09-30T00:00:00Z", "a1", true},
		{"2026-06-15", "2026-06-15T00:00:00Z", "a2", true},
		{"2026-06-15", "2026-08-31T00:00:00Z", "a3", true},
		{"2026-09-15", "2026-08-31T00:00:00Z", "a4", true},
		{"2026-09-15", "2026-06-15T00:00:00Z", "", false},
		{"2025-01-01", "2026-09-30T00:00:00Z", "", false},
	} {
		got, found, err := line.InForce(date(t, tc.on), known(t, tc.cut))
		if err != nil {
			t.Fatalf("as of %s known at %s: %v", tc.on, tc.cut, err)
		}
		if found != tc.found || (found && got.ID != tc.want) {
			t.Fatalf("as of %s known at %s: got %q found=%t, want %q/%t",
				tc.on, tc.cut, got.ID, found, tc.want, tc.found)
		}
	}
}

// TestTimelineRetainsWhatACorrectionSupersedes proves the engine never
// deletes.
func TestTimelineRetainsWhatACorrectionSupersedes(t *testing.T) {
	line, err := NewTimeline(fixture(t))
	if err != nil {
		t.Fatalf("NewTimeline: %v", err)
	}
	if !line.IsSuperseded("a2") {
		t.Fatal("the corrected coordinate is not marked superseded")
	}
	by, ok := line.SupersededBy("a2")
	if !ok || by != "a3" {
		t.Fatalf("a2 superseded by %q (found %t)", by, ok)
	}
	if _, ok := line.Lookup("a2"); !ok {
		t.Fatal("the corrected coordinate was removed from the timeline")
	}
	corrections := line.Corrections()
	if len(corrections) != 1 || corrections[0] != [2]string{"a3", "a2"} {
		t.Fatalf("corrections = %v", corrections)
	}
	if line.Canonical() == nil {
		t.Fatal("the timeline has no canonical encoding")
	}
}

// TestTimelineRefusesAnIncoherentSet proves that an ambiguous or dangling set
// is refused rather than silently repaired.
func TestTimelineRefusesAnIncoherentSet(t *testing.T) {
	base := fixture(t)

	t.Run("duplicate ids", func(t *testing.T) {
		dup := append(append([]Coordinate(nil), base...), base[0])
		if _, err := NewTimeline(dup); !errors.Is(err, ErrDuplicateID) {
			t.Fatalf("err = %v, want ErrDuplicateID", err)
		}
	})

	t.Run("a correction of something not in the set", func(t *testing.T) {
		dangling := append([]Coordinate(nil), base...)
		dangling[3].Supersedes = "nowhere"
		if _, err := NewTimeline(dangling); !errors.Is(err, ErrUnknownSupersedes) {
			t.Fatalf("err = %v, want ErrUnknownSupersedes", err)
		}
	})

	t.Run("two corrections of one assertion", func(t *testing.T) {
		double := append([]Coordinate(nil), base...)
		extra := coord(t, "a5", "2026-06-01", "2026-09-01", "2026-08-15T00:00:00Z", "a2")
		double = append(double, extra)
		if _, err := NewTimeline(double); !errors.Is(err, ErrDuplicateID) {
			t.Fatalf("err = %v, want ErrDuplicateID", err)
		}
	})

	t.Run("a coordinate that corrects itself", func(t *testing.T) {
		self := append([]Coordinate(nil), base...)
		self[0].Supersedes = self[0].ID
		if _, err := NewTimeline(self); !errors.Is(err, ErrSelfSupersedes) {
			t.Fatalf("err = %v, want ErrSelfSupersedes", err)
		}
	})

	t.Run("an instant interval", func(t *testing.T) {
		start := instant(t, "2026-01-01T00:00:00Z")
		iv, err := values.NewOpenInstantInterval(start)
		if err != nil {
			t.Fatalf("interval: %v", err)
		}
		wrong := append([]Coordinate(nil), base...)
		wrong[0].Effective = iv
		if _, err := NewTimeline(wrong); !errors.Is(err, ErrIntervalKind) {
			t.Fatalf("err = %v, want ErrIntervalKind", err)
		}
	})

	t.Run("a coordinate with no identity", func(t *testing.T) {
		anonymous := append([]Coordinate(nil), base...)
		anonymous[0].ID = ""
		if _, err := NewTimeline(anonymous); !errors.Is(err, ErrCoordinateInvalid) {
			t.Fatalf("err = %v, want ErrCoordinateInvalid", err)
		}
	})
}

// TestTimelineOrderIsIndependentOfInputOrder proves the total order is a
// property of the coordinates.
func TestTimelineOrderIsIndependentOfInputOrder(t *testing.T) {
	forward, err := NewTimeline(fixture(t))
	if err != nil {
		t.Fatalf("NewTimeline: %v", err)
	}
	reversed := fixture(t)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	backward, err := NewTimeline(reversed)
	if err != nil {
		t.Fatalf("NewTimeline reversed: %v", err)
	}
	if string(forward.Canonical()) != string(backward.Canonical()) {
		t.Fatalf("order depends on input: %v vs %v",
			ids(forward.Ordered()), ids(backward.Ordered()))
	}
}

// TestTimelineQueriesAreSeparable proves that "what was effective" and "what
// did we know" can each be asked on their own.
func TestTimelineQueriesAreSeparable(t *testing.T) {
	line, err := NewTimeline(fixture(t))
	if err != nil {
		t.Fatalf("NewTimeline: %v", err)
	}
	covering, err := line.CoveringDate(date(t, "2026-06-15"))
	if err != nil {
		t.Fatalf("CoveringDate: %v", err)
	}
	if got := ids(covering); len(got) != 2 {
		t.Fatalf("covering 2026-06-15 = %v, want the June value and its correction", got)
	}
	visible, err := line.KnownAtOrBefore(known(t, "2026-06-01T00:00:00Z"))
	if err != nil {
		t.Fatalf("KnownAtOrBefore: %v", err)
	}
	if got := ids(visible); len(got) != 2 {
		t.Fatalf("known at 2026-06-01 = %v, want the two already-recorded values", got)
	}
	if _, err := line.CoveringDate(values.LocalDate{}); !errors.Is(err, ErrCutoffInvalid) {
		t.Fatalf("unset date: err = %v, want ErrCutoffInvalid", err)
	}
	if _, err := line.KnownAtOrBefore(values.KnownAt{}); !errors.Is(err, ErrCutoffInvalid) {
		t.Fatalf("unset cut-off: err = %v, want ErrCutoffInvalid", err)
	}
}

// TestTimelineExplainsItsOwnSelection proves the engine contract: a version,
// and a bounded explanation that names identities and counts only.
func TestTimelineExplainsItsOwnSelection(t *testing.T) {
	if Version() < 1 {
		t.Fatalf("engine version = %d", Version())
	}
	line, err := NewTimeline(fixture(t))
	if err != nil {
		t.Fatalf("NewTimeline: %v", err)
	}
	lines, err := line.Explain(date(t, "2026-06-15"), known(t, "2026-08-31T00:00:00Z"))
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if len(lines) != 5 {
		t.Fatalf("explanation = %v", lines)
	}
	if lines[len(lines)-1] != "in force: a3" {
		t.Fatalf("explanation does not name the winner: %v", lines)
	}
	if _, err := line.Explain(values.LocalDate{}, known(t, "2026-08-31T00:00:00Z")); !errors.Is(err, ErrCutoffInvalid) {
		t.Fatalf("err = %v, want ErrCutoffInvalid", err)
	}
}

// ids returns the identifiers of a coordinate slice.
func ids(cs []Coordinate) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}
