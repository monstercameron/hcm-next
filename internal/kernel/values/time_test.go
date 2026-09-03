package values

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
)

var (
	testZone     = ZoneRef{ID: "America/New_York", TzdbVersion: "2026a"}
	testCalendar = CalendarRef{Ref: "us-federal", Version: "2026.1"}
)

func mustLocalDate(t *testing.T, s string) LocalDate {
	t.Helper()
	d, err := ParseLocalDate(s)
	if err != nil {
		t.Fatalf("ParseLocalDate(%q) error = %v", s, err)
	}
	return d
}

func mustLocalTime(t *testing.T, s string) LocalTime {
	t.Helper()
	v, err := ParseLocalTime(s)
	if err != nil {
		t.Fatalf("ParseLocalTime(%q) error = %v", s, err)
	}
	return v
}

func mustDateInterval(t *testing.T, start, end string) EffectiveInterval {
	t.Helper()
	iv, err := NewLocalDateInterval(mustLocalDate(t, start), mustLocalDate(t, end), testCalendar)
	if err != nil {
		t.Fatalf("NewLocalDateInterval(%q,%q) error = %v", start, end, err)
	}
	return iv
}

// TestTodo_MODEL_004 is the primary test for MODEL-004: business-time
// primitives with half-open intervals and explicit DST resolution.
func TestTodo_MODEL_004(t *testing.T) {
	t.Parallel()

	t.Run("LocalDateRejectsImpossibleDates", func(t *testing.T) {
		for _, s := range []string{
			"2026-02-30", "2026-13-01", "2026-00-10", "2026-01-00", "2025-02-29",
			"2026-1-1", "20260101", "2026-01-32", "", "2026-01-01T00:00:00Z",
		} {
			if _, err := ParseLocalDate(s); err == nil {
				t.Errorf("ParseLocalDate(%q) = nil error, want failure", s)
			}
		}
		// A leap day in a leap year is legal.
		leap := mustLocalDate(t, "2024-02-29")
		if got := leap.String(); got != "2024-02-29" {
			t.Fatalf("String() = %q", got)
		}
		if _, err := NewLocalDate(2026, time.February, 30); !errors.Is(err, ErrInvalidLocalDate) {
			t.Fatalf("NewLocalDate error = %v, want ErrInvalidLocalDate", err)
		}
	})

	t.Run("LocalTimeRejectsOutOfRange", func(t *testing.T) {
		for _, s := range []string{"24:00:00", "23:60:00", "23:59:60", "9:30:00", "", "23:59"} {
			if _, err := ParseLocalTime(s); err == nil {
				t.Errorf("ParseLocalTime(%q) = nil error, want failure", s)
			}
		}
		v := mustLocalTime(t, "09:30:00")
		if got := v.String(); got != "09:30:00" {
			t.Fatalf("String() = %q", got)
		}
		if _, err := NewLocalTime(0, 0, 0, 1_000_000_000); !errors.Is(err, ErrInvalidLocalTime) {
			t.Fatalf("NewLocalTime error = %v, want ErrInvalidLocalTime", err)
		}
	})

	t.Run("ADateIsNotAnInstant", func(t *testing.T) {
		// A LocalDate never silently becomes midnight UTC; resolving it needs a
		// zone, a tzdb version and a disambiguation policy.
		date := mustLocalDate(t, "2026-03-08")
		if _, err := date.AtStartOfDay(ZoneRef{}, DisambiguationRejectGap); !errors.Is(err, ErrZoneRequired) {
			t.Fatalf("AtStartOfDay without a zone = %v, want ErrZoneRequired", err)
		}
		if _, err := date.AtStartOfDay(ZoneRef{ID: "America/New_York"}, DisambiguationRejectGap); !errors.Is(err, ErrTzdbVersionRequired) {
			t.Fatalf("AtStartOfDay without a tzdb version = %v, want ErrTzdbVersionRequired", err)
		}
		z, err := date.AtStartOfDay(testZone, DisambiguationRejectGap)
		if err != nil {
			t.Fatalf("AtStartOfDay error = %v", err)
		}
		if got, want := z.Instant().String(), "2026-03-08T05:00:00Z"; got != want {
			t.Fatalf("start of day = %q, want %q", got, want)
		}

		// Interval kinds do not mix.
		dateInterval := mustDateInterval(t, "2026-01-01", "2026-02-01")
		if _, err := dateInterval.ContainsInstant(z.Instant()); !errors.Is(err, ErrIntervalKindMismatch) {
			t.Fatalf("ContainsInstant on a LOCAL_DATE interval = %v, want ErrIntervalKindMismatch", err)
		}
		instantInterval, err := NewInstantInterval(z.Instant(), MustInstant(t, "2026-04-01T00:00:00Z"))
		if err != nil {
			t.Fatalf("NewInstantInterval error = %v", err)
		}
		if _, err := instantInterval.ContainsDate(date); !errors.Is(err, ErrIntervalKindMismatch) {
			t.Fatalf("ContainsDate on an INSTANT interval = %v, want ErrIntervalKindMismatch", err)
		}
		if _, err := dateInterval.Overlaps(instantInterval); !errors.Is(err, ErrIntervalKindMismatch) {
			t.Fatalf("Overlaps across kinds = %v, want ErrIntervalKindMismatch", err)
		}
		if bytes.Equal(dateInterval.Canonical(), instantInterval.Canonical()) {
			t.Fatal("a date interval and an instant interval share canonical bytes")
		}
	})

	t.Run("DSTGapNeedsAResolution", func(t *testing.T) {
		// 2026-03-08 02:30 does not exist in America/New_York.
		gapDate := mustLocalDate(t, "2026-03-08")
		gapTime := mustLocalTime(t, "02:30:00")
		if _, err := NewZonedDateTime(gapDate, gapTime, testZone, DisambiguationRejectGap); !errors.Is(err, ErrDSTGap) {
			t.Fatalf("REJECT_GAP on a gap = %v, want ErrDSTGap", err)
		}
		if _, err := NewZonedDateTime(gapDate, gapTime, testZone, DisambiguationUnspecified); !errors.Is(err, ErrDisambiguationRequired) {
			t.Fatalf("unspecified disambiguation = %v, want ErrDisambiguationRequired", err)
		}
		earlier, err := NewZonedDateTime(gapDate, gapTime, testZone, DisambiguationEarlier)
		if err != nil {
			t.Fatalf("EARLIER on a gap error = %v", err)
		}
		later, err := NewZonedDateTime(gapDate, gapTime, testZone, DisambiguationLater)
		if err != nil {
			t.Fatalf("LATER on a gap error = %v", err)
		}
		if got, want := earlier.Instant().String(), "2026-03-08T07:00:00Z"; got != want {
			t.Fatalf("gap EARLIER = %q, want %q", got, want)
		}
		if got, want := later.Instant().String(), "2026-03-08T07:00:00Z"; got != want {
			t.Fatalf("gap LATER = %q, want %q", got, want)
		}
		// A time on the same day that does exist resolves without a policy fight.
		ok, err := NewZonedDateTime(gapDate, mustLocalTime(t, "04:30:00"), testZone, DisambiguationRejectGap)
		if err != nil {
			t.Fatalf("unambiguous time error = %v", err)
		}
		if got, want := ok.Instant().String(), "2026-03-08T08:30:00Z"; got != want {
			t.Fatalf("unambiguous resolve = %q, want %q", got, want)
		}
	})

	t.Run("DSTFoldNeedsAResolution", func(t *testing.T) {
		// 2026-11-01 01:30 happens twice in America/New_York.
		foldDate := mustLocalDate(t, "2026-11-01")
		foldTime := mustLocalTime(t, "01:30:00")
		if _, err := NewZonedDateTime(foldDate, foldTime, testZone, DisambiguationRejectGap); !errors.Is(err, ErrDSTAmbiguous) {
			t.Fatalf("REJECT_GAP on a fold = %v, want ErrDSTAmbiguous", err)
		}
		earlier, err := NewZonedDateTime(foldDate, foldTime, testZone, DisambiguationEarlier)
		if err != nil {
			t.Fatalf("EARLIER on a fold error = %v", err)
		}
		later, err := NewZonedDateTime(foldDate, foldTime, testZone, DisambiguationLater)
		if err != nil {
			t.Fatalf("LATER on a fold error = %v", err)
		}
		if got, want := earlier.Instant().String(), "2026-11-01T05:30:00Z"; got != want {
			t.Fatalf("fold EARLIER = %q, want %q", got, want)
		}
		if got, want := later.Instant().String(), "2026-11-01T06:30:00Z"; got != want {
			t.Fatalf("fold LATER = %q, want %q", got, want)
		}
		if earlier.OffsetSeconds() != -4*3600 || later.OffsetSeconds() != -5*3600 {
			t.Fatalf("offsets = %d, %d", earlier.OffsetSeconds(), later.OffsetSeconds())
		}
		if bytes.Equal(earlier.Canonical(), later.Canonical()) {
			t.Fatal("the two sides of a fold share canonical bytes")
		}
	})

	t.Run("ZoneAndTzdbVersionAreMandatory", func(t *testing.T) {
		date := mustLocalDate(t, "2026-06-01")
		tod := mustLocalTime(t, "12:00:00")
		if _, err := NewZonedDateTime(date, tod, ZoneRef{TzdbVersion: "2026a"}, DisambiguationRejectGap); !errors.Is(err, ErrZoneRequired) {
			t.Fatalf("missing zone = %v, want ErrZoneRequired", err)
		}
		if _, err := NewZonedDateTime(date, tod, ZoneRef{ID: "America/New_York"}, DisambiguationRejectGap); !errors.Is(err, ErrTzdbVersionRequired) {
			t.Fatalf("missing tzdb version = %v, want ErrTzdbVersionRequired", err)
		}
		if _, err := NewZonedDateTime(date, tod, ZoneRef{ID: "Mars/Olympus", TzdbVersion: "2026a"}, DisambiguationRejectGap); !errors.Is(err, ErrUnknownZone) {
			t.Fatalf("unknown zone = %v, want ErrUnknownZone", err)
		}
	})

	t.Run("IntervalsAreHalfOpen", func(t *testing.T) {
		iv := mustDateInterval(t, "2026-01-01", "2026-02-01")
		for _, tc := range []struct {
			date string
			want bool
		}{
			{"2025-12-31", false},
			{"2026-01-01", true},
			{"2026-01-31", true},
			{"2026-02-01", false},
		} {
			got, err := iv.ContainsDate(mustLocalDate(t, tc.date))
			if err != nil {
				t.Fatalf("ContainsDate(%q) error = %v", tc.date, err)
			}
			if got != tc.want {
				t.Errorf("ContainsDate(%q) = %v, want %v", tc.date, got, tc.want)
			}
		}
	})

	t.Run("AdjacentIntervalsDoNotOverlap", func(t *testing.T) {
		a := mustDateInterval(t, "2026-01-01", "2026-02-01")
		b := mustDateInterval(t, "2026-02-01", "2026-03-01")
		overlap, err := a.Overlaps(b)
		if err != nil {
			t.Fatalf("Overlaps error = %v", err)
		}
		if overlap {
			t.Fatal("adjacent half-open intervals reported an overlap")
		}
		reverse, err := b.Overlaps(a)
		if err != nil {
			t.Fatalf("Overlaps error = %v", err)
		}
		if reverse {
			t.Fatal("overlap is not symmetric for adjacent intervals")
		}
		c := mustDateInterval(t, "2026-01-15", "2026-02-15")
		if got, err := a.Overlaps(c); err != nil || !got {
			t.Fatalf("straddling intervals Overlaps = %v, %v", got, err)
		}
	})

	t.Run("RejectsInvertedAndEmptyIntervals", func(t *testing.T) {
		if _, err := NewLocalDateInterval(mustLocalDate(t, "2026-02-01"), mustLocalDate(t, "2026-01-01"), testCalendar); !errors.Is(err, ErrIntervalInverted) {
			t.Fatalf("inverted interval = %v, want ErrIntervalInverted", err)
		}
		same := mustLocalDate(t, "2026-01-01")
		if _, err := NewLocalDateInterval(same, same, testCalendar); !errors.Is(err, ErrIntervalEmpty) {
			t.Fatalf("empty interval = %v, want ErrIntervalEmpty", err)
		}
		start := MustInstant(t, "2026-01-02T00:00:00Z")
		end := MustInstant(t, "2026-01-01T00:00:00Z")
		if _, err := NewInstantInterval(start, end); !errors.Is(err, ErrIntervalInverted) {
			t.Fatalf("inverted instant interval = %v, want ErrIntervalInverted", err)
		}
		if _, err := NewInstantInterval(start, start); !errors.Is(err, ErrIntervalEmpty) {
			t.Fatalf("empty instant interval = %v, want ErrIntervalEmpty", err)
		}
	})

	t.Run("LocalDateIntervalNeedsAGoverningCalendar", func(t *testing.T) {
		start := mustLocalDate(t, "2026-01-01")
		end := mustLocalDate(t, "2026-02-01")
		if _, err := NewLocalDateInterval(start, end, CalendarRef{}); !errors.Is(err, ErrCalendarRequired) {
			t.Fatalf("calendarless date interval = %v, want ErrCalendarRequired", err)
		}
		if _, err := NewLocalDateInterval(start, end, CalendarRef{Ref: "us-federal"}); !errors.Is(err, ErrCalendarVersionRequired) {
			t.Fatalf("unversioned calendar = %v, want ErrCalendarVersionRequired", err)
		}
	})

	t.Run("OpenEndedIntervals", func(t *testing.T) {
		open, err := NewOpenLocalDateInterval(mustLocalDate(t, "2026-01-01"), testCalendar)
		if err != nil {
			t.Fatalf("NewOpenLocalDateInterval error = %v", err)
		}
		if !open.IsOpenEnded() {
			t.Fatal("open interval reported a closed end")
		}
		if _, ok := open.EndDate(); ok {
			t.Fatal("open interval exposed an end date")
		}
		far, err := open.ContainsDate(mustLocalDate(t, "2199-12-31"))
		if err != nil {
			t.Fatalf("ContainsDate error = %v", err)
		}
		if !far {
			t.Fatal("open interval excluded a far future date")
		}
		closed := mustDateInterval(t, "2026-01-01", "2026-02-01")
		if bytes.Equal(open.Canonical(), closed.Canonical()) {
			t.Fatal("open and closed intervals share canonical bytes")
		}
	})

	t.Run("RecordedAtAndKnownAtAreDistinctTypes", func(t *testing.T) {
		recorded, err := NewRecordedAt(MustInstant(t, "2026-06-01T12:00:00Z"))
		if err != nil {
			t.Fatalf("NewRecordedAt error = %v", err)
		}
		known, err := NewKnownAt(MustInstant(t, "2026-05-01T12:00:00Z"))
		if err != nil {
			t.Fatalf("NewKnownAt error = %v", err)
		}
		if err := ValidateKnowledgeOrder(known, recorded, false); err != nil {
			t.Fatalf("known before recorded = %v, want nil", err)
		}
		late, err := NewKnownAt(MustInstant(t, "2026-07-01T12:00:00Z"))
		if err != nil {
			t.Fatalf("NewKnownAt error = %v", err)
		}
		if err := ValidateKnowledgeOrder(late, recorded, false); !errors.Is(err, ErrFutureKnowledge) {
			t.Fatalf("known after recorded = %v, want ErrFutureKnowledge", err)
		}
		if err := ValidateKnowledgeOrder(late, recorded, true); err != nil {
			t.Fatalf("declared future-knowledge claim = %v, want nil", err)
		}
		if bytes.Equal(recorded.Canonical(), known.Canonical()) {
			t.Fatal("RecordedAt and KnownAt share canonical bytes")
		}
	})

	t.Run("PayPeriodAndBusinessDay", func(t *testing.T) {
		period, err := NewPayPeriod("2026-P01", mustDateInterval(t, "2026-01-01", "2026-01-16"), 1)
		if err != nil {
			t.Fatalf("NewPayPeriod error = %v", err)
		}
		if got, want := period.String(), "2026-P01[2026-01-01,2026-01-16)"; got != want {
			t.Fatalf("PayPeriod.String() = %q, want %q", got, want)
		}
		next, err := NewPayPeriod("2026-P02", mustDateInterval(t, "2026-01-16", "2026-02-01"), 2)
		if err != nil {
			t.Fatalf("NewPayPeriod error = %v", err)
		}
		overlap, err := period.Interval().Overlaps(next.Interval())
		if err != nil {
			t.Fatalf("Overlaps error = %v", err)
		}
		if overlap {
			t.Fatal("adjacent pay periods overlapped")
		}
		open, err := NewOpenLocalDateInterval(mustLocalDate(t, "2026-01-01"), testCalendar)
		if err != nil {
			t.Fatalf("NewOpenLocalDateInterval error = %v", err)
		}
		if _, err := NewPayPeriod("2026-P03", open, 3); !errors.Is(err, ErrPayPeriodOpenEnded) {
			t.Fatalf("open-ended pay period = %v, want ErrPayPeriodOpenEnded", err)
		}
		instantInterval, err := NewInstantInterval(MustInstant(t, "2026-01-01T00:00:00Z"), MustInstant(t, "2026-02-01T00:00:00Z"))
		if err != nil {
			t.Fatalf("NewInstantInterval error = %v", err)
		}
		if _, err := NewPayPeriod("2026-P04", instantInterval, 4); !errors.Is(err, ErrIntervalKindMismatch) {
			t.Fatalf("instant-kind pay period = %v, want ErrIntervalKindMismatch", err)
		}

		day, err := NewBusinessDay(mustLocalDate(t, "2026-01-01"), testCalendar, false)
		if err != nil {
			t.Fatalf("NewBusinessDay error = %v", err)
		}
		if day.IsWorkingDay() {
			t.Fatal("New Year's Day reported as a working day")
		}
		if _, err := NewBusinessDay(mustLocalDate(t, "2026-01-02"), CalendarRef{Ref: "us-federal"}, true); !errors.Is(err, ErrCalendarVersionRequired) {
			t.Fatalf("unversioned business day = %v, want ErrCalendarVersionRequired", err)
		}
	})
}

// TestTodo_MODEL_004_Property asserts temporal invariants over a table of
// dates, instants and intervals.
func TestTodo_MODEL_004_Property(t *testing.T) {
	t.Parallel()

	dates := []string{"2024-02-29", "2026-01-01", "2026-01-31", "2026-02-01", "2026-12-31"}
	for _, s := range dates {
		d := mustLocalDate(t, s)
		// Text round trip is exact.
		var back LocalDate
		if err := back.UnmarshalText([]byte(s)); err != nil || back != d {
			t.Fatalf("LocalDate round trip %q -> %+v err=%v", s, back, err)
		}
		// Canonical bytes are stable and injective across the table.
		if !bytes.Equal(d.Canonical(), d.Canonical()) {
			t.Fatalf("LocalDate canonical bytes are not stable for %q", s)
		}
		// Day arithmetic and comparison agree.
		if next := d.AddDays(1); next.Compare(d) <= 0 {
			t.Fatalf("%q + 1 day did not sort after it", s)
		}
		if same := d.AddDays(0); same != d {
			t.Fatalf("%q + 0 days changed the date", s)
		}
	}

	seen := map[string]string{}
	for _, s := range dates {
		key := string(mustLocalDate(t, s).Canonical())
		if prev, dup := seen[key]; dup {
			t.Fatalf("%q and %q share canonical bytes", prev, s)
		}
		seen[key] = s
	}

	// Property: for any two half-open intervals, overlap is symmetric, and an
	// interval that ends exactly where another starts never overlaps it.
	bounds := []string{"2026-01-01", "2026-01-15", "2026-02-01", "2026-03-01"}
	var intervals []EffectiveInterval
	for i := range bounds {
		for j := i + 1; j < len(bounds); j++ {
			intervals = append(intervals, mustDateInterval(t, bounds[i], bounds[j]))
		}
	}
	for i, a := range intervals {
		for j, b := range intervals {
			ab, err1 := a.Overlaps(b)
			ba, err2 := b.Overlaps(a)
			if err1 != nil || err2 != nil {
				t.Fatalf("Overlaps errors = %v, %v", err1, err2)
			}
			if ab != ba {
				t.Fatalf("interval %d and %d disagree on overlap", i, j)
			}
			aEnd, ok := a.EndDate()
			if !ok {
				continue
			}
			bStart, _ := a.StartDate()
			_ = bStart
			if bs, ok := b.StartDate(); ok && aEnd == bs && ab {
				t.Fatalf("interval %d ends where %d starts but reported an overlap", i, j)
			}
		}
		// An interval always contains its start and never its end.
		start, _ := a.StartDate()
		contains, err := a.ContainsDate(start)
		if err != nil || !contains {
			t.Fatalf("interval %d does not contain its start", i)
		}
		if end, ok := a.EndDate(); ok {
			contains, err := a.ContainsDate(end)
			if err != nil || contains {
				t.Fatalf("interval %d contains its exclusive end", i)
			}
		}
	}
}

// TestTodo_MODEL_004_Golden pins business-time vectors from testdata.
func TestTodo_MODEL_004_Golden(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("testdata/model_004_time.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var golden struct {
		Instants []struct {
			Unix      int64  `json:"unix_seconds"`
			Nanos     int32  `json:"nanos"`
			RFC3339   string `json:"rfc3339"`
			Canonical string `json:"canonical_hex"`
		} `json:"instants"`
		LocalDates []struct {
			Text      string `json:"text"`
			Canonical string `json:"canonical_hex"`
		} `json:"local_dates"`
		Zoned []struct {
			Date           string `json:"date"`
			Time           string `json:"time"`
			Zone           string `json:"zone"`
			Tzdb           string `json:"tzdb_version"`
			Disambiguation string `json:"disambiguation"`
			WantInstant    string `json:"want_instant"`
			WantOffset     int    `json:"want_offset_seconds"`
			WantError      string `json:"want_error"`
		} `json:"zoned"`
		Overlaps []struct {
			AStart string `json:"a_start"`
			AEnd   string `json:"a_end"`
			BStart string `json:"b_start"`
			BEnd   string `json:"b_end"`
			Want   bool   `json:"want_overlap"`
		} `json:"interval_overlaps"`
		InvalidDates []string `json:"invalid_local_dates"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatalf("decode golden: %v", err)
	}

	for _, tc := range golden.Instants {
		in, err := NewInstantFromUnix(tc.Unix, tc.Nanos)
		if err != nil {
			t.Errorf("NewInstantFromUnix(%d,%d) error = %v", tc.Unix, tc.Nanos, err)
			continue
		}
		if got := in.String(); got != tc.RFC3339 {
			t.Errorf("instant %d string = %q, want %q", tc.Unix, got, tc.RFC3339)
		}
		if got := hex.EncodeToString(in.Canonical()); got != tc.Canonical {
			t.Errorf("instant %d canonical = %s, want %s", tc.Unix, got, tc.Canonical)
		}
	}
	for _, tc := range golden.LocalDates {
		d, err := ParseLocalDate(tc.Text)
		if err != nil {
			t.Errorf("ParseLocalDate(%q) error = %v", tc.Text, err)
			continue
		}
		if got := hex.EncodeToString(d.Canonical()); got != tc.Canonical {
			t.Errorf("date %q canonical = %s, want %s", tc.Text, got, tc.Canonical)
		}
	}
	for _, tc := range golden.Zoned {
		disambiguation, err := ParseDisambiguation(tc.Disambiguation)
		if err != nil {
			t.Errorf("ParseDisambiguation(%q) error = %v", tc.Disambiguation, err)
			continue
		}
		date, err := ParseLocalDate(tc.Date)
		if err != nil {
			t.Errorf("ParseLocalDate(%q) error = %v", tc.Date, err)
			continue
		}
		tod, err := ParseLocalTime(tc.Time)
		if err != nil {
			t.Errorf("ParseLocalTime(%q) error = %v", tc.Time, err)
			continue
		}
		z, err := NewZonedDateTime(date, tod, ZoneRef{ID: tc.Zone, TzdbVersion: tc.Tzdb}, disambiguation)
		if tc.WantError != "" {
			if err == nil {
				t.Errorf("%s %s %s: want error %s, got %s", tc.Date, tc.Time, tc.Disambiguation, tc.WantError, z.Instant().String())
			}
			continue
		}
		if err != nil {
			t.Errorf("%s %s %s: error = %v", tc.Date, tc.Time, tc.Disambiguation, err)
			continue
		}
		if got := z.Instant().String(); got != tc.WantInstant {
			t.Errorf("%s %s %s = %q, want %q", tc.Date, tc.Time, tc.Disambiguation, got, tc.WantInstant)
		}
		if z.OffsetSeconds() != tc.WantOffset {
			t.Errorf("%s %s %s offset = %d, want %d", tc.Date, tc.Time, tc.Disambiguation, z.OffsetSeconds(), tc.WantOffset)
		}
	}
	for _, tc := range golden.Overlaps {
		a := mustDateInterval(t, tc.AStart, tc.AEnd)
		b := mustDateInterval(t, tc.BStart, tc.BEnd)
		got, err := a.Overlaps(b)
		if err != nil {
			t.Errorf("Overlaps error = %v", err)
			continue
		}
		if got != tc.Want {
			t.Errorf("[%s,%s) vs [%s,%s) overlap = %v, want %v", tc.AStart, tc.AEnd, tc.BStart, tc.BEnd, got, tc.Want)
		}
	}
	for _, s := range golden.InvalidDates {
		if _, err := ParseLocalDate(s); err == nil {
			t.Errorf("invalid golden date %q was accepted", s)
		}
	}
}

// FuzzTodo_MODEL_004 checks that temporal parsing never panics and that any
// value that parses re-encodes to the exact input bytes.
func FuzzTodo_MODEL_004(f *testing.F) {
	for _, s := range []string{
		"2026-01-01", "2024-02-29", "2026-02-30", "0000-01-01", "9999-12-31",
		"2026-1-1", "", "2026-01-01T00:00:00Z", "-0001-01-01",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if d, err := ParseLocalDate(s); err == nil {
			if got := d.String(); got != s {
				t.Fatalf("LocalDate re-encode = %q, want %q", got, s)
			}
			if len(d.Canonical()) == 0 {
				t.Fatalf("parsed date %q produced no canonical bytes", s)
			}
			// A parsed date is always a real calendar date.
			round := time.Date(int(d.Year()), d.Month(), int(d.Day()), 0, 0, 0, 0, time.UTC)
			if round.Year() != int(d.Year()) || round.Month() != d.Month() || round.Day() != int(d.Day()) {
				t.Fatalf("parsed %q is not a real calendar date", s)
			}
		}
		if v, err := ParseLocalTime(s); err == nil {
			if got := v.String(); got != s {
				t.Fatalf("LocalTime re-encode = %q, want %q", got, s)
			}
		}
		var in Instant
		if err := in.UnmarshalText([]byte(s)); err == nil {
			if got := in.String(); got != s {
				t.Fatalf("Instant re-encode = %q, want %q", got, s)
			}
		}
	})
}

// MustInstant parses an RFC 3339 UTC instant for tests.
func MustInstant(t *testing.T, s string) Instant {
	t.Helper()
	var in Instant
	if err := in.UnmarshalText([]byte(s)); err != nil {
		t.Fatalf("Instant.UnmarshalText(%q) error = %v", s, err)
	}
	return in
}
