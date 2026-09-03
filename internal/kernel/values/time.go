package values

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	// The IANA database is embedded so that zone resolution does not depend on
	// the host operating system. Which release is embedded is a build fact; the
	// business-meaningful tzdb version is always declared explicitly on a
	// ZoneRef and governed by MODEL-005.
	_ "time/tzdata"
)

// Canonical type tags. Every temporal value carries one so that two different
// kinds can never produce the same canonical bytes.
const (
	tagInstant     byte = 0x01
	tagLocalDate   byte = 0x02
	tagLocalTime   byte = 0x03
	tagZoned       byte = 0x04
	tagInterval    byte = 0x05
	tagRecordedAt  byte = 0x06
	tagKnownAt     byte = 0x07
	tagPayPeriod   byte = 0x08
	tagBusinessDay byte = 0x09
)

// Business-time errors. All are matchable with errors.Is.
var (
	ErrInstantUnset            = errors.New("values: instant is unset")
	ErrInstantFormat           = errors.New("values: instant text is not a canonical RFC 3339 UTC timestamp")
	ErrInstantRange            = errors.New("values: instant is out of range")
	ErrInvalidLocalDate        = errors.New("values: local date is not a real calendar date")
	ErrInvalidLocalTime        = errors.New("values: local time is out of range")
	ErrLocalDateUnset          = errors.New("values: local date is unset")
	ErrLocalTimeUnset          = errors.New("values: local time is unset")
	ErrZoneRequired            = errors.New("values: IANA timezone id is required")
	ErrTzdbVersionRequired     = errors.New("values: tzdb version is required")
	ErrUnknownZone             = errors.New("values: unknown IANA timezone id")
	ErrDisambiguationRequired  = errors.New("values: local time needs a disambiguation policy")
	ErrDisambiguation          = errors.New("values: unknown disambiguation policy")
	ErrDSTGap                  = errors.New("values: local time does not exist in this zone")
	ErrDSTAmbiguous            = errors.New("values: local time occurs twice in this zone")
	ErrOffsetNotValid          = errors.New("values: explicit offset does not apply to this local time")
	ErrCalendarRequired        = errors.New("values: business calendar reference is required")
	ErrCalendarVersionRequired = errors.New("values: business calendar version is required")
	ErrIntervalUnset           = errors.New("values: effective interval is unset")
	ErrIntervalInverted        = errors.New("values: effective interval ends before it starts")
	ErrIntervalEmpty           = errors.New("values: half-open effective interval is empty")
	ErrIntervalKindMismatch    = errors.New("values: effective interval kinds do not match")
	ErrFutureKnowledge         = errors.New("values: known_at is after recorded_at without a future-knowledge claim")
	ErrPayPeriodUnset          = errors.New("values: pay period is unset")
	ErrPayPeriodOpenEnded      = errors.New("values: pay period must be closed")
	ErrBusinessDayUnset        = errors.New("values: business day is unset")
)

// Instant is a point on the UTC timeline with nanosecond resolution. It is not
// a local time and never carries a display timezone.
//
// The zero Instant is unset and fails Validate.
type Instant struct {
	set  bool
	sec  int64
	nsec int32
}

// NewInstant converts a time.Time to an instant, discarding the location and
// any monotonic reading.
func NewInstant(t time.Time) Instant {
	u := t.UTC()
	return Instant{set: true, sec: u.Unix(), nsec: int32(u.Nanosecond())}
}

// NewInstantFromUnix builds an instant from UTC seconds and nanoseconds.
func NewInstantFromUnix(sec int64, nsec int32) (Instant, error) {
	if nsec < 0 || nsec > 999_999_999 {
		return Instant{}, fmt.Errorf("%w: nanoseconds %d", ErrInstantRange, nsec)
	}
	return Instant{set: true, sec: sec, nsec: nsec}, nil
}

// Validate reports whether the instant is usable.
func (i Instant) Validate() error {
	if !i.set {
		return ErrInstantUnset
	}
	if i.nsec < 0 || i.nsec > 999_999_999 {
		return fmt.Errorf("%w: nanoseconds %d", ErrInstantRange, i.nsec)
	}
	return nil
}

// IsSet reports whether the instant carries a value.
func (i Instant) IsSet() bool { return i.set }

// Time returns the instant as a UTC time.Time.
func (i Instant) Time() time.Time { return time.Unix(i.sec, int64(i.nsec)).UTC() }

// Unix returns the UTC seconds and nanoseconds.
func (i Instant) Unix() (int64, int32) { return i.sec, i.nsec }

// Compare returns -1, 0 or +1.
func (i Instant) Compare(o Instant) int {
	switch {
	case i.sec != o.sec:
		if i.sec < o.sec {
			return -1
		}
		return 1
	case i.nsec != o.nsec:
		if i.nsec < o.nsec {
			return -1
		}
		return 1
	default:
		return 0
	}
}

// Before reports whether i is strictly before o.
func (i Instant) Before(o Instant) bool { return i.Compare(o) < 0 }

// After reports whether i is strictly after o.
func (i Instant) After(o Instant) bool { return i.Compare(o) > 0 }

// String returns the canonical RFC 3339 UTC text.
func (i Instant) String() string {
	if i.Validate() != nil {
		return ""
	}
	return i.Time().Format(time.RFC3339Nano)
}

// Canonical returns the canonical byte encoding, or nil when unset.
func (i Instant) Canonical() []byte {
	if i.Validate() != nil {
		return nil
	}
	out := make([]byte, 0, 13)
	out = append(out, tagInstant)
	out = binary.BigEndian.AppendUint64(out, uint64(i.sec))
	return binary.BigEndian.AppendUint32(out, uint32(i.nsec))
}

// MarshalText implements encoding.TextMarshaler.
func (i Instant) MarshalText() ([]byte, error) {
	if err := i.Validate(); err != nil {
		return nil, err
	}
	return []byte(i.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler. Only the canonical UTC
// spelling is accepted: a numeric offset, a lowercase "z" and a padded
// fractional part all fail, so one instant has exactly one text form.
func (i *Instant) UnmarshalText(text []byte) error {
	*i = Instant{}
	s := string(text)
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return fmt.Errorf("%w: %q: %v", ErrInstantFormat, s, err)
	}
	candidate := NewInstant(t)
	if candidate.String() != s {
		return fmt.Errorf("%w: %q is not the canonical UTC spelling", ErrInstantFormat, s)
	}
	*i = candidate
	return nil
}

// LocalDate is a calendar date with no time and no timezone. It never converts
// itself to an instant; see AtStartOfDay.
//
// The zero LocalDate is unset and fails Validate.
type LocalDate struct {
	set   bool
	year  int32
	month uint8
	day   uint8
}

// NewLocalDate builds a calendar date, rejecting anything that is not a real
// date in the proleptic Gregorian calendar.
func NewLocalDate(year int, month time.Month, day int) (LocalDate, error) {
	if year < 1 || year > 9999 {
		return LocalDate{}, fmt.Errorf("%w: year %d is outside [1,9999]", ErrInvalidLocalDate, year)
	}
	if month < time.January || month > time.December {
		return LocalDate{}, fmt.Errorf("%w: month %d", ErrInvalidLocalDate, int(month))
	}
	if day < 1 || day > 31 {
		return LocalDate{}, fmt.Errorf("%w: day %d", ErrInvalidLocalDate, day)
	}
	probe := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	if probe.Year() != year || probe.Month() != month || probe.Day() != day {
		return LocalDate{}, fmt.Errorf("%w: %04d-%02d-%02d", ErrInvalidLocalDate, year, int(month), day)
	}
	return LocalDate{set: true, year: int32(year), month: uint8(month), day: uint8(day)}, nil
}

// ParseLocalDate decodes canonical YYYY-MM-DD text.
func ParseLocalDate(s string) (LocalDate, error) {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return LocalDate{}, fmt.Errorf("%w: %q is not YYYY-MM-DD", ErrInvalidLocalDate, s)
	}
	for _, idx := range []int{0, 1, 2, 3, 5, 6, 8, 9} {
		if s[idx] < '0' || s[idx] > '9' {
			return LocalDate{}, fmt.Errorf("%w: %q is not YYYY-MM-DD", ErrInvalidLocalDate, s)
		}
	}
	year, _ := strconv.Atoi(s[0:4])
	month, _ := strconv.Atoi(s[5:7])
	day, _ := strconv.Atoi(s[8:10])
	return NewLocalDate(year, time.Month(month), day)
}

// Validate reports whether the date is usable.
func (d LocalDate) Validate() error {
	if !d.set {
		return ErrLocalDateUnset
	}
	_, err := NewLocalDate(int(d.year), time.Month(d.month), int(d.day))
	return err
}

// IsSet reports whether the date carries a value.
func (d LocalDate) IsSet() bool { return d.set }

// Year returns the calendar year.
func (d LocalDate) Year() int32 { return d.year }

// Month returns the calendar month.
func (d LocalDate) Month() time.Month { return time.Month(d.month) }

// Day returns the day of the month.
func (d LocalDate) Day() uint8 { return d.day }

// AddDays returns the date n days later, or the unset date when d is unset or
// the result leaves the supported year range.
func (d LocalDate) AddDays(n int) LocalDate {
	if d.Validate() != nil {
		return LocalDate{}
	}
	shifted := time.Date(int(d.year), time.Month(d.month), int(d.day), 0, 0, 0, 0, time.UTC).AddDate(0, 0, n)
	out, err := NewLocalDate(shifted.Year(), shifted.Month(), shifted.Day())
	if err != nil {
		return LocalDate{}
	}
	return out
}

// Compare returns -1, 0 or +1 in calendar order.
func (d LocalDate) Compare(o LocalDate) int {
	switch {
	case d.year != o.year:
		if d.year < o.year {
			return -1
		}
		return 1
	case d.month != o.month:
		if d.month < o.month {
			return -1
		}
		return 1
	case d.day != o.day:
		if d.day < o.day {
			return -1
		}
		return 1
	default:
		return 0
	}
}

// String returns canonical YYYY-MM-DD text.
func (d LocalDate) String() string {
	if d.Validate() != nil {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.year, d.month, d.day)
}

// Canonical returns the canonical byte encoding, or nil when unset.
func (d LocalDate) Canonical() []byte {
	if d.Validate() != nil {
		return nil
	}
	out := make([]byte, 0, 7)
	out = append(out, tagLocalDate)
	out = binary.BigEndian.AppendUint32(out, uint32(d.year))
	return append(out, d.month, d.day)
}

// MarshalText implements encoding.TextMarshaler.
func (d LocalDate) MarshalText() ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return []byte(d.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (d *LocalDate) UnmarshalText(text []byte) error {
	*d = LocalDate{}
	parsed, err := ParseLocalDate(string(text))
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

// AtStartOfDay resolves the date's first instant in a zone. It requires an
// explicit zone, tzdb version and disambiguation policy; a date never becomes
// midnight UTC on its own.
func (d LocalDate) AtStartOfDay(zone ZoneRef, disambiguation Disambiguation) (ZonedDateTime, error) {
	midnight, err := NewLocalTime(0, 0, 0, 0)
	if err != nil {
		return ZonedDateTime{}, err
	}
	return NewZonedDateTime(d, midnight, zone, disambiguation)
}

// LocalTime is a wall-clock time of day with no date and no timezone.
//
// The zero LocalTime is unset and fails Validate.
type LocalTime struct {
	set    bool
	hour   uint8
	minute uint8
	second uint8
	nanos  uint32
}

// NewLocalTime builds a time of day. Leap seconds are not representable.
func NewLocalTime(hour, minute, second, nanos int) (LocalTime, error) {
	switch {
	case hour < 0 || hour > 23:
		return LocalTime{}, fmt.Errorf("%w: hour %d", ErrInvalidLocalTime, hour)
	case minute < 0 || minute > 59:
		return LocalTime{}, fmt.Errorf("%w: minute %d", ErrInvalidLocalTime, minute)
	case second < 0 || second > 59:
		return LocalTime{}, fmt.Errorf("%w: second %d", ErrInvalidLocalTime, second)
	case nanos < 0 || nanos > 999_999_999:
		return LocalTime{}, fmt.Errorf("%w: nanoseconds %d", ErrInvalidLocalTime, nanos)
	}
	return LocalTime{set: true, hour: uint8(hour), minute: uint8(minute), second: uint8(second), nanos: uint32(nanos)}, nil
}

// ParseLocalTime decodes canonical HH:MM:SS or HH:MM:SS.fffffffff text.
func ParseLocalTime(s string) (LocalTime, error) {
	head, frac, hasFrac := strings.Cut(s, ".")
	if len(head) != 8 || head[2] != ':' || head[5] != ':' {
		return LocalTime{}, fmt.Errorf("%w: %q is not HH:MM:SS", ErrInvalidLocalTime, s)
	}
	for _, idx := range []int{0, 1, 3, 4, 6, 7} {
		if head[idx] < '0' || head[idx] > '9' {
			return LocalTime{}, fmt.Errorf("%w: %q is not HH:MM:SS", ErrInvalidLocalTime, s)
		}
	}
	nanos := 0
	if hasFrac {
		if len(frac) != 9 {
			return LocalTime{}, fmt.Errorf("%w: fractional seconds must be exactly 9 digits", ErrInvalidLocalTime)
		}
		for i := 0; i < 9; i++ {
			if frac[i] < '0' || frac[i] > '9' {
				return LocalTime{}, fmt.Errorf("%w: %q", ErrInvalidLocalTime, s)
			}
		}
		nanos, _ = strconv.Atoi(frac)
		if nanos == 0 {
			return LocalTime{}, fmt.Errorf("%w: an all-zero fraction must be omitted", ErrInvalidLocalTime)
		}
	}
	hour, _ := strconv.Atoi(head[0:2])
	minute, _ := strconv.Atoi(head[3:5])
	second, _ := strconv.Atoi(head[6:8])
	return NewLocalTime(hour, minute, second, nanos)
}

// Validate reports whether the time of day is usable.
func (v LocalTime) Validate() error {
	if !v.set {
		return ErrLocalTimeUnset
	}
	_, err := NewLocalTime(int(v.hour), int(v.minute), int(v.second), int(v.nanos))
	return err
}

// IsSet reports whether the time of day carries a value.
func (v LocalTime) IsSet() bool { return v.set }

// Clock returns the hour, minute, second and nanoseconds.
func (v LocalTime) Clock() (hour, minute, second int, nanos int) {
	return int(v.hour), int(v.minute), int(v.second), int(v.nanos)
}

// String returns canonical HH:MM:SS text, with nine fractional digits only when
// the value has a nonzero fraction.
func (v LocalTime) String() string {
	if v.Validate() != nil {
		return ""
	}
	base := fmt.Sprintf("%02d:%02d:%02d", v.hour, v.minute, v.second)
	if v.nanos == 0 {
		return base
	}
	return fmt.Sprintf("%s.%09d", base, v.nanos)
}

// Canonical returns the canonical byte encoding, or nil when unset.
func (v LocalTime) Canonical() []byte {
	if v.Validate() != nil {
		return nil
	}
	out := make([]byte, 0, 8)
	out = append(out, tagLocalTime, v.hour, v.minute, v.second)
	return binary.BigEndian.AppendUint32(out, v.nanos)
}

// MarshalText implements encoding.TextMarshaler.
func (v LocalTime) MarshalText() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	return []byte(v.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (v *LocalTime) UnmarshalText(text []byte) error {
	*v = LocalTime{}
	parsed, err := ParseLocalTime(string(text))
	if err != nil {
		return err
	}
	*v = parsed
	return nil
}

// ZoneRef names an IANA timezone together with the tzdb release that governs
// it. Both halves are mandatory: the same zone id resolves differently under
// different tzdb releases, so a zone without a version is not a decision.
type ZoneRef struct {
	ID          string
	TzdbVersion string
}

// Validate reports whether the zone reference is complete and loadable.
func (z ZoneRef) Validate() error {
	if z.ID == "" {
		return ErrZoneRequired
	}
	if z.TzdbVersion == "" {
		return fmt.Errorf("%w: zone %q", ErrTzdbVersionRequired, z.ID)
	}
	if _, err := time.LoadLocation(z.ID); err != nil {
		return fmt.Errorf("%w: %q", ErrUnknownZone, z.ID)
	}
	return nil
}

// Location loads the zone.
func (z ZoneRef) Location() (*time.Location, error) {
	if err := z.Validate(); err != nil {
		return nil, err
	}
	return time.LoadLocation(z.ID)
}

// String returns "<zone id>@<tzdb version>".
func (z ZoneRef) String() string {
	if z.ID == "" && z.TzdbVersion == "" {
		return ""
	}
	return z.ID + "@" + z.TzdbVersion
}

// CalendarRef names a business calendar together with its dataset version.
// Both halves are mandatory for the same reason a zone needs a tzdb version.
type CalendarRef struct {
	Ref     string
	Version string
}

// Validate reports whether the calendar reference is complete.
func (c CalendarRef) Validate() error {
	if c.Ref == "" {
		return ErrCalendarRequired
	}
	if c.Version == "" {
		return fmt.Errorf("%w: calendar %q", ErrCalendarVersionRequired, c.Ref)
	}
	return nil
}

// String returns "<calendar ref>@<version>".
func (c CalendarRef) String() string {
	if c.Ref == "" && c.Version == "" {
		return ""
	}
	return c.Ref + "@" + c.Version
}

// Disambiguation is the declared policy for a local time that a zone skips or
// repeats. There is no default; an unspecified policy is an error.
type Disambiguation uint8

// Disambiguation policies.
const (
	// DisambiguationUnspecified is the zero value and is never legal.
	DisambiguationUnspecified Disambiguation = iota
	// DisambiguationRejectGap refuses to resolve a skipped or repeated local
	// time and reports which one it was.
	DisambiguationRejectGap
	// DisambiguationEarlier takes the earlier of two repeated instants, and the
	// transition instant with the pre-transition offset for a skipped time.
	DisambiguationEarlier
	// DisambiguationLater takes the later of two repeated instants, and the
	// transition instant with the post-transition offset for a skipped time.
	DisambiguationLater
	// DisambiguationExplicitOffset resolves by a caller-supplied UTC offset; see
	// NewZonedDateTimeAtOffset.
	DisambiguationExplicitOffset
)

var disambiguationWire = map[Disambiguation]string{
	DisambiguationRejectGap:      "REJECT_GAP",
	DisambiguationEarlier:        "EARLIER",
	DisambiguationLater:          "LATER",
	DisambiguationExplicitOffset: "EXPLICIT_OFFSET",
}

// String returns the stable wire token.
func (d Disambiguation) String() string {
	if w, ok := disambiguationWire[d]; ok {
		return w
	}
	return "DISAMBIGUATION_UNSPECIFIED"
}

// Valid reports whether d is a declared policy.
func (d Disambiguation) Valid() bool {
	_, ok := disambiguationWire[d]
	return ok
}

// ParseDisambiguation decodes a wire token. The unspecified token is rejected.
func ParseDisambiguation(token string) (Disambiguation, error) {
	for policy, wire := range disambiguationWire {
		if wire == token {
			return policy, nil
		}
	}
	return DisambiguationUnspecified, fmt.Errorf("%w: %q", ErrDisambiguation, token)
}

// ZonedDateTime is a local civil date and time bound to a zone, a tzdb version,
// a resolved UTC offset and the disambiguation policy that produced it. The
// policy is retained because it is the evidence for how an ambiguous local time
// was resolved.
//
// The zero ZonedDateTime is unset and fails Validate.
type ZonedDateTime struct {
	set            bool
	date           LocalDate
	tod            LocalTime
	zone           ZoneRef
	disambiguation Disambiguation
	offsetSeconds  int32
	instant        Instant
}

// NewZonedDateTime resolves a local civil time in a zone under an explicit
// disambiguation policy.
func NewZonedDateTime(date LocalDate, tod LocalTime, zone ZoneRef, disambiguation Disambiguation) (ZonedDateTime, error) {
	if err := date.Validate(); err != nil {
		return ZonedDateTime{}, err
	}
	if err := tod.Validate(); err != nil {
		return ZonedDateTime{}, err
	}
	if err := zone.Validate(); err != nil {
		return ZonedDateTime{}, err
	}
	if disambiguation == DisambiguationUnspecified {
		return ZonedDateTime{}, ErrDisambiguationRequired
	}
	if !disambiguation.Valid() {
		return ZonedDateTime{}, fmt.Errorf("%w: %d", ErrDisambiguation, uint8(disambiguation))
	}
	if disambiguation == DisambiguationExplicitOffset {
		return ZonedDateTime{}, fmt.Errorf("%w: EXPLICIT_OFFSET needs NewZonedDateTimeAtOffset", ErrDisambiguationRequired)
	}
	loc, err := zone.Location()
	if err != nil {
		return ZonedDateTime{}, err
	}
	candidates := resolveLocal(date, tod, loc)
	switch len(candidates) {
	case 1:
		return buildZoned(date, tod, zone, disambiguation, candidates[0], loc), nil
	case 2:
		if disambiguation == DisambiguationRejectGap {
			return ZonedDateTime{}, fmt.Errorf("%w: %s %s in %s", ErrDSTAmbiguous, date, tod, zone.ID)
		}
		pick := candidates[0]
		if disambiguation == DisambiguationLater {
			pick = candidates[1]
		}
		return buildZoned(date, tod, zone, disambiguation, pick, loc), nil
	default:
		if disambiguation == DisambiguationRejectGap {
			return ZonedDateTime{}, fmt.Errorf("%w: %s %s in %s", ErrDSTGap, date, tod, zone.ID)
		}
		transition, err := gapTransition(date, tod, loc)
		if err != nil {
			return ZonedDateTime{}, err
		}
		return buildZoned(date, tod, zone, disambiguation, transition, loc), nil
	}
}

// NewZonedDateTimeAtOffset resolves a local civil time using an explicitly
// supplied UTC offset. The offset must be one the zone actually uses for that
// local time, so a stale or invented offset cannot slip through.
func NewZonedDateTimeAtOffset(date LocalDate, tod LocalTime, zone ZoneRef, offsetSeconds int) (ZonedDateTime, error) {
	if err := date.Validate(); err != nil {
		return ZonedDateTime{}, err
	}
	if err := tod.Validate(); err != nil {
		return ZonedDateTime{}, err
	}
	if err := zone.Validate(); err != nil {
		return ZonedDateTime{}, err
	}
	loc, err := zone.Location()
	if err != nil {
		return ZonedDateTime{}, err
	}
	for _, candidate := range resolveLocal(date, tod, loc) {
		if _, off := candidate.In(loc).Zone(); off == offsetSeconds {
			return buildZoned(date, tod, zone, DisambiguationExplicitOffset, candidate, loc), nil
		}
	}
	return ZonedDateTime{}, fmt.Errorf("%w: %s %s in %s at %+d seconds",
		ErrOffsetNotValid, date, tod, zone.ID, offsetSeconds)
}

func buildZoned(date LocalDate, tod LocalTime, zone ZoneRef, disambiguation Disambiguation, at time.Time, loc *time.Location) ZonedDateTime {
	_, off := at.In(loc).Zone()
	return ZonedDateTime{
		set:            true,
		date:           date,
		tod:            tod,
		zone:           zone,
		disambiguation: disambiguation,
		offsetSeconds:  int32(off),
		instant:        NewInstant(at),
	}
}

// resolveLocal returns every UTC instant whose wall clock in loc equals the
// requested local date and time, in ascending order. An empty result is a DST
// gap; two results are a DST fold.
func resolveLocal(date LocalDate, tod LocalTime, loc *time.Location) []time.Time {
	hour, minute, second, nanos := tod.Clock()
	naive := time.Date(int(date.Year()), date.Month(), int(date.Day()), hour, minute, second, nanos, time.UTC)
	probe := time.Date(int(date.Year()), date.Month(), int(date.Day()), hour, minute, second, nanos, loc)
	_, offBefore := probe.Add(-24 * time.Hour).Zone()
	_, offAfter := probe.Add(24 * time.Hour).Zone()

	var out []time.Time
	for _, off := range dedupeOffsets(offBefore, offAfter) {
		candidate := naive.Add(-time.Duration(off) * time.Second)
		if sameWallClock(candidate.In(loc), date, tod) {
			out = append(out, candidate)
		}
	}
	if len(out) == 2 && out[0].After(out[1]) {
		out[0], out[1] = out[1], out[0]
	}
	return out
}

func dedupeOffsets(a, b int) []int {
	if a == b {
		return []int{a}
	}
	return []int{a, b}
}

func sameWallClock(t time.Time, date LocalDate, tod LocalTime) bool {
	hour, minute, second, nanos := tod.Clock()
	return t.Year() == int(date.Year()) &&
		t.Month() == date.Month() &&
		t.Day() == int(date.Day()) &&
		t.Hour() == hour &&
		t.Minute() == minute &&
		t.Second() == second &&
		t.Nanosecond() == nanos
}

// gapTransition finds the instant at which the zone offset changes across a
// skipped local time. A skipped local time has no instant of its own, so the
// transition instant is the first instant that exists, which is what both the
// EARLIER and LATER policies resolve to; they differ only in the offset the
// resolution records.
func gapTransition(date LocalDate, tod LocalTime, loc *time.Location) (time.Time, error) {
	hour, minute, second, nanos := tod.Clock()
	naive := time.Date(int(date.Year()), date.Month(), int(date.Day()), hour, minute, second, nanos, time.UTC)
	probe := time.Date(int(date.Year()), date.Month(), int(date.Day()), hour, minute, second, nanos, loc)
	_, offBefore := probe.Add(-24 * time.Hour).Zone()
	_, offAfter := probe.Add(24 * time.Hour).Zone()
	if offBefore == offAfter {
		return time.Time{}, fmt.Errorf("%w: %s %s has no surrounding transition", ErrDSTGap, date, tod)
	}
	lo := naive.Add(-time.Duration(offBefore) * time.Second)
	hi := naive.Add(-time.Duration(offAfter) * time.Second)
	if lo.After(hi) {
		lo, hi = hi, lo
	}
	_, target := hi.In(loc).Zone()
	// Binary search for the first second at which the target offset applies.
	for hi.Sub(lo) > time.Second {
		mid := lo.Add(hi.Sub(lo) / 2).Truncate(time.Second)
		if !mid.After(lo) {
			break
		}
		if _, off := mid.In(loc).Zone(); off == target {
			hi = mid
		} else {
			lo = mid
		}
	}
	return hi.Truncate(time.Second), nil
}

// Validate reports whether the zoned date-time is usable.
func (z ZonedDateTime) Validate() error {
	if !z.set {
		return ErrIntervalUnset
	}
	if err := z.date.Validate(); err != nil {
		return err
	}
	if err := z.tod.Validate(); err != nil {
		return err
	}
	if err := z.zone.Validate(); err != nil {
		return err
	}
	if !z.disambiguation.Valid() {
		return ErrDisambiguationRequired
	}
	return z.instant.Validate()
}

// Date returns the local calendar date.
func (z ZonedDateTime) Date() LocalDate { return z.date }

// TimeOfDay returns the local wall-clock time.
func (z ZonedDateTime) TimeOfDay() LocalTime { return z.tod }

// Zone returns the zone and tzdb version that resolved the value.
func (z ZonedDateTime) Zone() ZoneRef { return z.zone }

// Disambiguation returns the policy that resolved the value.
func (z ZonedDateTime) Disambiguation() Disambiguation { return z.disambiguation }

// OffsetSeconds returns the resolved UTC offset in seconds.
func (z ZonedDateTime) OffsetSeconds() int { return int(z.offsetSeconds) }

// Instant returns the resolved UTC instant.
func (z ZonedDateTime) Instant() Instant { return z.instant }

// String returns "<date>T<time><zone>[<offset>/<policy>]".
func (z ZonedDateTime) String() string {
	if z.Validate() != nil {
		return ""
	}
	return fmt.Sprintf("%sT%s[%s%+d/%s]", z.date, z.tod, z.zone, z.offsetSeconds, z.disambiguation)
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (z ZonedDateTime) Canonical() []byte {
	if z.Validate() != nil {
		return nil
	}
	out := []byte{tagZoned}
	out = append(out, z.date.Canonical()...)
	out = append(out, z.tod.Canonical()...)
	out = binary.BigEndian.AppendUint32(out, uint32(z.offsetSeconds))
	out = append(out, byte(z.disambiguation))
	out = appendLengthPrefixed(out, z.zone.ID)
	out = appendLengthPrefixed(out, z.zone.TzdbVersion)
	return append(out, z.instant.Canonical()...)
}

// appendLengthPrefixed writes a uint32 big-endian length followed by the bytes,
// so that two adjacent strings can never be reparsed as a different split.
func appendLengthPrefixed(dst []byte, s string) []byte {
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(s)))
	return append(dst, s...)
}

// IntervalKind names the boundary semantics of an effective interval. A
// LOCAL_DATE interval and an INSTANT interval are never interchangeable.
type IntervalKind uint8

// Interval kinds.
const (
	// IntervalKindUnspecified is the zero value and is never legal.
	IntervalKindUnspecified IntervalKind = iota
	// IntervalKindLocalDate bounds the interval by calendar dates under a
	// governing business calendar.
	IntervalKindLocalDate
	// IntervalKindInstant bounds the interval by UTC instants.
	IntervalKindInstant
)

var intervalKindWire = map[IntervalKind]string{
	IntervalKindLocalDate: "LOCAL_DATE",
	IntervalKindInstant:   "INSTANT",
}

// String returns the stable wire token.
func (k IntervalKind) String() string {
	if w, ok := intervalKindWire[k]; ok {
		return w
	}
	return "INTERVAL_KIND_UNSPECIFIED"
}

// EffectiveInterval is the one temporal boundary contract every domain uses.
// It is half-open: the start is inclusive, the end is exclusive, and an absent
// end means open-ended. Domains may add constraints; they may not redefine
// these boundaries.
//
// The zero EffectiveInterval is unset and fails Validate.
type EffectiveInterval struct {
	kind           IntervalKind
	startDate      LocalDate
	endDate        LocalDate
	startInstant   Instant
	endInstant     Instant
	hasEnd         bool
	calendar       CalendarRef
	zone           ZoneRef
	disambiguation Disambiguation
}

// NewLocalDateInterval builds a closed half-open date interval under a
// versioned business calendar.
func NewLocalDateInterval(start, end LocalDate, calendar CalendarRef) (EffectiveInterval, error) {
	iv, err := newOpenLocalDateInterval(start, calendar)
	if err != nil {
		return EffectiveInterval{}, err
	}
	if err := end.Validate(); err != nil {
		return EffectiveInterval{}, err
	}
	switch start.Compare(end) {
	case 1:
		return EffectiveInterval{}, fmt.Errorf("%w: [%s,%s)", ErrIntervalInverted, start, end)
	case 0:
		return EffectiveInterval{}, fmt.Errorf("%w: [%s,%s)", ErrIntervalEmpty, start, end)
	}
	iv.endDate = end
	iv.hasEnd = true
	return iv, nil
}

// NewOpenLocalDateInterval builds an open-ended half-open date interval.
func NewOpenLocalDateInterval(start LocalDate, calendar CalendarRef) (EffectiveInterval, error) {
	return newOpenLocalDateInterval(start, calendar)
}

func newOpenLocalDateInterval(start LocalDate, calendar CalendarRef) (EffectiveInterval, error) {
	if err := start.Validate(); err != nil {
		return EffectiveInterval{}, err
	}
	if err := calendar.Validate(); err != nil {
		return EffectiveInterval{}, err
	}
	return EffectiveInterval{kind: IntervalKindLocalDate, startDate: start, calendar: calendar}, nil
}

// NewInstantInterval builds a closed half-open instant interval.
func NewInstantInterval(start, end Instant) (EffectiveInterval, error) {
	iv, err := NewOpenInstantInterval(start)
	if err != nil {
		return EffectiveInterval{}, err
	}
	if err := end.Validate(); err != nil {
		return EffectiveInterval{}, err
	}
	switch start.Compare(end) {
	case 1:
		return EffectiveInterval{}, fmt.Errorf("%w: [%s,%s)", ErrIntervalInverted, start, end)
	case 0:
		return EffectiveInterval{}, fmt.Errorf("%w: [%s,%s)", ErrIntervalEmpty, start, end)
	}
	iv.endInstant = end
	iv.hasEnd = true
	return iv, nil
}

// NewOpenInstantInterval builds an open-ended half-open instant interval.
func NewOpenInstantInterval(start Instant) (EffectiveInterval, error) {
	if err := start.Validate(); err != nil {
		return EffectiveInterval{}, err
	}
	return EffectiveInterval{kind: IntervalKindInstant, startInstant: start}, nil
}

// WithZone records the zone and disambiguation policy that govern a LOCAL_DATE
// interval when it is projected onto the UTC timeline. It never converts the
// boundaries itself.
func (iv EffectiveInterval) WithZone(zone ZoneRef, disambiguation Disambiguation) (EffectiveInterval, error) {
	if err := iv.Validate(); err != nil {
		return EffectiveInterval{}, err
	}
	if err := zone.Validate(); err != nil {
		return EffectiveInterval{}, err
	}
	if !disambiguation.Valid() {
		return EffectiveInterval{}, ErrDisambiguationRequired
	}
	iv.zone = zone
	iv.disambiguation = disambiguation
	return iv, nil
}

// Validate reports whether the interval is usable.
func (iv EffectiveInterval) Validate() error {
	switch iv.kind {
	case IntervalKindLocalDate:
		if err := iv.startDate.Validate(); err != nil {
			return err
		}
		if err := iv.calendar.Validate(); err != nil {
			return err
		}
		if iv.hasEnd {
			if err := iv.endDate.Validate(); err != nil {
				return err
			}
			if iv.startDate.Compare(iv.endDate) >= 0 {
				return ErrIntervalInverted
			}
		}
		return nil
	case IntervalKindInstant:
		if err := iv.startInstant.Validate(); err != nil {
			return err
		}
		if iv.hasEnd {
			if err := iv.endInstant.Validate(); err != nil {
				return err
			}
			if iv.startInstant.Compare(iv.endInstant) >= 0 {
				return ErrIntervalInverted
			}
		}
		return nil
	default:
		return ErrIntervalUnset
	}
}

// Kind returns the boundary semantics.
func (iv EffectiveInterval) Kind() IntervalKind { return iv.kind }

// IsOpenEnded reports whether the interval has no exclusive end.
func (iv EffectiveInterval) IsOpenEnded() bool { return !iv.hasEnd }

// Calendar returns the governing business calendar for a LOCAL_DATE interval.
func (iv EffectiveInterval) Calendar() CalendarRef { return iv.calendar }

// StartDate returns the inclusive start date and whether the interval is a
// LOCAL_DATE interval.
func (iv EffectiveInterval) StartDate() (LocalDate, bool) {
	return iv.startDate, iv.kind == IntervalKindLocalDate
}

// EndDate returns the exclusive end date and whether one exists.
func (iv EffectiveInterval) EndDate() (LocalDate, bool) {
	return iv.endDate, iv.kind == IntervalKindLocalDate && iv.hasEnd
}

// StartInstant returns the inclusive start instant and whether the interval is
// an INSTANT interval.
func (iv EffectiveInterval) StartInstant() (Instant, bool) {
	return iv.startInstant, iv.kind == IntervalKindInstant
}

// EndInstant returns the exclusive end instant and whether one exists.
func (iv EffectiveInterval) EndInstant() (Instant, bool) {
	return iv.endInstant, iv.kind == IntervalKindInstant && iv.hasEnd
}

// ContainsDate reports whether a date falls in a LOCAL_DATE interval. Asking an
// INSTANT interval is an error, not a silent conversion.
func (iv EffectiveInterval) ContainsDate(d LocalDate) (bool, error) {
	if err := iv.Validate(); err != nil {
		return false, err
	}
	if iv.kind != IntervalKindLocalDate {
		return false, fmt.Errorf("%w: interval is %s, date given", ErrIntervalKindMismatch, iv.kind)
	}
	if err := d.Validate(); err != nil {
		return false, err
	}
	if d.Compare(iv.startDate) < 0 {
		return false, nil
	}
	if iv.hasEnd && d.Compare(iv.endDate) >= 0 {
		return false, nil
	}
	return true, nil
}

// ContainsInstant reports whether an instant falls in an INSTANT interval.
func (iv EffectiveInterval) ContainsInstant(i Instant) (bool, error) {
	if err := iv.Validate(); err != nil {
		return false, err
	}
	if iv.kind != IntervalKindInstant {
		return false, fmt.Errorf("%w: interval is %s, instant given", ErrIntervalKindMismatch, iv.kind)
	}
	if err := i.Validate(); err != nil {
		return false, err
	}
	if i.Compare(iv.startInstant) < 0 {
		return false, nil
	}
	if iv.hasEnd && i.Compare(iv.endInstant) >= 0 {
		return false, nil
	}
	return true, nil
}

// Overlaps reports whether two intervals of the same kind share any point.
// Because both are half-open, an interval that ends exactly where another
// starts does not overlap it.
func (iv EffectiveInterval) Overlaps(other EffectiveInterval) (bool, error) {
	if err := iv.Validate(); err != nil {
		return false, err
	}
	if err := other.Validate(); err != nil {
		return false, err
	}
	if iv.kind != other.kind {
		return false, fmt.Errorf("%w: %s vs %s", ErrIntervalKindMismatch, iv.kind, other.kind)
	}
	if iv.kind == IntervalKindLocalDate {
		if iv.hasEnd && iv.endDate.Compare(other.startDate) <= 0 {
			return false, nil
		}
		if other.hasEnd && other.endDate.Compare(iv.startDate) <= 0 {
			return false, nil
		}
		return true, nil
	}
	if iv.hasEnd && iv.endInstant.Compare(other.startInstant) <= 0 {
		return false, nil
	}
	if other.hasEnd && other.endInstant.Compare(iv.startInstant) <= 0 {
		return false, nil
	}
	return true, nil
}

// String returns "[start,end)" or "[start,)" for an open end.
func (iv EffectiveInterval) String() string {
	if iv.Validate() != nil {
		return ""
	}
	start, end := "", ""
	if iv.kind == IntervalKindLocalDate {
		start = iv.startDate.String()
		if iv.hasEnd {
			end = iv.endDate.String()
		}
	} else {
		start = iv.startInstant.String()
		if iv.hasEnd {
			end = iv.endInstant.String()
		}
	}
	return "[" + start + "," + end + ")"
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (iv EffectiveInterval) Canonical() []byte {
	if iv.Validate() != nil {
		return nil
	}
	out := []byte{tagInterval, byte(iv.kind)}
	if iv.hasEnd {
		out = append(out, 0x01)
	} else {
		out = append(out, 0x00)
	}
	if iv.kind == IntervalKindLocalDate {
		out = append(out, iv.startDate.Canonical()...)
		if iv.hasEnd {
			out = append(out, iv.endDate.Canonical()...)
		}
	} else {
		out = append(out, iv.startInstant.Canonical()...)
		if iv.hasEnd {
			out = append(out, iv.endInstant.Canonical()...)
		}
	}
	out = appendLengthPrefixed(out, iv.calendar.Ref)
	out = appendLengthPrefixed(out, iv.calendar.Version)
	out = appendLengthPrefixed(out, iv.zone.ID)
	out = appendLengthPrefixed(out, iv.zone.TzdbVersion)
	return append(out, byte(iv.disambiguation))
}

// RecordedAt is the trusted platform receipt or commit time of an assertion. It
// is a distinct type from KnownAt so that the two can never be swapped.
type RecordedAt struct{ at Instant }

// NewRecordedAt wraps an instant as a recorded time.
func NewRecordedAt(at Instant) (RecordedAt, error) {
	if err := at.Validate(); err != nil {
		return RecordedAt{}, err
	}
	return RecordedAt{at: at}, nil
}

// Instant returns the underlying instant.
func (r RecordedAt) Instant() Instant { return r.at }

// String returns the canonical instant text.
func (r RecordedAt) String() string { return r.at.String() }

// Canonical returns the canonical byte encoding, or nil when unset.
func (r RecordedAt) Canonical() []byte {
	if r.at.Validate() != nil {
		return nil
	}
	return append([]byte{tagRecordedAt}, r.at.Canonical()...)
}

// KnownAt is the earliest time an asserted fact was available to the stated
// authority. It is a distinct type from RecordedAt.
type KnownAt struct{ at Instant }

// NewKnownAt wraps an instant as a knowledge time.
func NewKnownAt(at Instant) (KnownAt, error) {
	if err := at.Validate(); err != nil {
		return KnownAt{}, err
	}
	return KnownAt{at: at}, nil
}

// Instant returns the underlying instant.
func (k KnownAt) Instant() Instant { return k.at }

// String returns the canonical instant text.
func (k KnownAt) String() string { return k.at.String() }

// Canonical returns the canonical byte encoding, or nil when unset.
func (k KnownAt) Canonical() []byte {
	if k.at.Validate() != nil {
		return nil
	}
	return append([]byte{tagKnownAt}, k.at.Canonical()...)
}

// ValidateKnowledgeOrder enforces the bitemporal invariant that knowledge does
// not precede its own recording: known_at may not be after recorded_at unless
// the caller declares an explicit future-knowledge claim.
func ValidateKnowledgeOrder(known KnownAt, recorded RecordedAt, futureKnowledgeClaim bool) error {
	if err := known.at.Validate(); err != nil {
		return err
	}
	if err := recorded.at.Validate(); err != nil {
		return err
	}
	if known.at.After(recorded.at) && !futureKnowledgeClaim {
		return fmt.Errorf("%w: known_at %s > recorded_at %s", ErrFutureKnowledge, known.at, recorded.at)
	}
	return nil
}

// PayPeriod is a named, closed, half-open date range in a pay calendar.
//
// The zero PayPeriod is unset and fails Validate.
type PayPeriod struct {
	set      bool
	id       string
	interval EffectiveInterval
	ordinal  int32
}

// NewPayPeriod builds a pay period. The interval must be a closed LOCAL_DATE
// interval: an open-ended pay period cannot be paid.
func NewPayPeriod(id string, interval EffectiveInterval, ordinal int32) (PayPeriod, error) {
	if id == "" {
		return PayPeriod{}, fmt.Errorf("%w: id is empty", ErrPayPeriodUnset)
	}
	if err := interval.Validate(); err != nil {
		return PayPeriod{}, err
	}
	if interval.Kind() != IntervalKindLocalDate {
		return PayPeriod{}, fmt.Errorf("%w: pay period needs a LOCAL_DATE interval, got %s",
			ErrIntervalKindMismatch, interval.Kind())
	}
	if interval.IsOpenEnded() {
		return PayPeriod{}, fmt.Errorf("%w: %s", ErrPayPeriodOpenEnded, id)
	}
	if ordinal < 1 {
		return PayPeriod{}, fmt.Errorf("%w: ordinal %d must be positive", ErrPayPeriodUnset, ordinal)
	}
	return PayPeriod{set: true, id: id, interval: interval, ordinal: ordinal}, nil
}

// Validate reports whether the pay period is usable.
func (p PayPeriod) Validate() error {
	if !p.set {
		return ErrPayPeriodUnset
	}
	return p.interval.Validate()
}

// ID returns the pay period identifier.
func (p PayPeriod) ID() string { return p.id }

// Interval returns the half-open date range.
func (p PayPeriod) Interval() EffectiveInterval { return p.interval }

// Ordinal returns the period's one-based position in its cycle.
func (p PayPeriod) Ordinal() int32 { return p.ordinal }

// String returns "<id>[start,end)".
func (p PayPeriod) String() string {
	if p.Validate() != nil {
		return ""
	}
	return p.id + p.interval.String()
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (p PayPeriod) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	out := []byte{tagPayPeriod}
	out = appendLengthPrefixed(out, p.id)
	out = binary.BigEndian.AppendUint32(out, uint32(p.ordinal))
	return append(out, p.interval.Canonical()...)
}

// BusinessDay is one calendar date's working status under a versioned business
// calendar. The calendar version is part of the value because a calendar
// republish can change whether a date is a working day.
//
// The zero BusinessDay is unset and fails Validate.
type BusinessDay struct {
	set      bool
	date     LocalDate
	calendar CalendarRef
	working  bool
}

// NewBusinessDay builds a business day.
func NewBusinessDay(date LocalDate, calendar CalendarRef, working bool) (BusinessDay, error) {
	if err := date.Validate(); err != nil {
		return BusinessDay{}, err
	}
	if err := calendar.Validate(); err != nil {
		return BusinessDay{}, err
	}
	return BusinessDay{set: true, date: date, calendar: calendar, working: working}, nil
}

// Validate reports whether the business day is usable.
func (b BusinessDay) Validate() error {
	if !b.set {
		return ErrBusinessDayUnset
	}
	if err := b.date.Validate(); err != nil {
		return err
	}
	return b.calendar.Validate()
}

// Date returns the calendar date.
func (b BusinessDay) Date() LocalDate { return b.date }

// Calendar returns the governing calendar and version.
func (b BusinessDay) Calendar() CalendarRef { return b.calendar }

// IsWorkingDay reports whether the calendar treats the date as a working day.
func (b BusinessDay) IsWorkingDay() bool { return b.working }

// String returns "<date>@<calendar> WORKING|NON_WORKING".
func (b BusinessDay) String() string {
	if b.Validate() != nil {
		return ""
	}
	status := "NON_WORKING"
	if b.working {
		status = "WORKING"
	}
	return b.date.String() + "@" + b.calendar.String() + " " + status
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (b BusinessDay) Canonical() []byte {
	if b.Validate() != nil {
		return nil
	}
	out := []byte{tagBusinessDay}
	out = append(out, b.date.Canonical()...)
	out = appendLengthPrefixed(out, b.calendar.Ref)
	out = appendLengthPrefixed(out, b.calendar.Version)
	if b.working {
		return append(out, 0x01)
	}
	return append(out, 0x00)
}
