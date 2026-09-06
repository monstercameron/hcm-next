// Package sla owns the deterministic, kernel-pure clock behind human work.
//
// It computes civil-calendar deadlines against an explicitly versioned zone
// and calendar, then hands those deadlines to workflow/steps/wait as durable
// requirements. It does not sleep, persist, deliver notices, or read a wall
// clock. Workflow owns what an escalation does; messaging only delivers the
// resulting signal.
package sla

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/workflow/steps/wait"
)

const contractVersion = 1

// Version reports this package's stable contract version.
func Version() int { return contractVersion }

// Errors are stable refusal identities for callers and tests.
var (
	ErrInvalidClock          = errors.New("sla: clock definition is invalid")
	ErrClockIdentity         = errors.New("sla: clock identity is required")
	ErrClockVersion          = errors.New("sla: clock version is required")
	ErrClockZoneRequired     = values.ErrZoneRequired
	ErrClockCalendar         = values.ErrCalendarRequired
	ErrClockCalendarRequired = values.ErrCalendarRequired
	ErrZoneRequired          = values.ErrZoneRequired
	ErrCalendarRequired      = values.ErrCalendarRequired
	ErrTzdbVersionRequired   = values.ErrTzdbVersionRequired
	ErrClockPolicy           = values.ErrReferenceUpdatePolicyRequired
	ErrOffset                = errors.New("sla: offset is invalid")
	ErrWorkItemIdentity      = errors.New("sla: work item identity is required")
	ErrStartRequired         = errors.New("sla: start instant is required")
	ErrSignalKind            = errors.New("sla: signal kind is invalid")
	ErrAlreadyPaused         = errors.New("sla: clock instance is already paused")
	ErrNotPaused             = errors.New("sla: clock instance is not paused")
	ErrPauseReason           = errors.New("sla: pause or resume reason is required")
	ErrPauseOrder            = errors.New("sla: pause and resume instants are invalid")
	ErrTooManyDatasets       = errors.New("sla: at most one dataset override is allowed")
	ErrDatasetUnavailable    = errors.New("sla: clock dataset is unavailable")
)

// SignalKind is the business meaning of a clock wake. These are intentionally
// closed: an unknown kind cannot accidentally become an escalation effect.
type SignalKind string

const (
	SignalReminder   SignalKind = "REMINDER"
	SignalEscalation SignalKind = "ESCALATION"
	SignalExpiry     SignalKind = "EXPIRY"
)

func (k SignalKind) Valid() bool {
	switch k {
	case SignalReminder, SignalEscalation, SignalExpiry:
		return true
	default:
		return false
	}
}

// CalendarDuration is a civil-calendar offset. Days and CalendarDays are
// compatibility spellings for the same calendar-day component; callers
// should normally use CalendarDays. The remaining components are applied in
// local civil time, so a DST transition is not silently treated as a fixed
// 24-hour day. The clock's CalendarRef is carried in every wait requirement;
// this type does not invent holiday data that was not supplied by a caller.
type CalendarDuration struct {
	CalendarDays int `json:"calendar_days,omitempty"`
	Days         int `json:"days,omitempty"`
	Hours        int `json:"hours,omitempty"`
	Minutes      int `json:"minutes,omitempty"`
	Seconds      int `json:"seconds,omitempty"`
	Nanoseconds  int `json:"nanoseconds,omitempty"`
}

// Duration and Offset are readable aliases for callers describing SLA
// offsets. CalendarDuration remains the canonical contract name.
type Duration = CalendarDuration
type Offset = CalendarDuration
type CalendarAwareDuration = CalendarDuration

func (d CalendarDuration) calendarDays() int {
	return d.CalendarDays + d.Days
}

func (d CalendarDuration) validate() error {
	if d.CalendarDays < 0 || d.Days < 0 || d.Hours < 0 || d.Minutes < 0 || d.Seconds < 0 || d.Nanoseconds < 0 {
		return fmt.Errorf("%w: components must not be negative", ErrOffset)
	}
	maxInt := int(^uint(0) >> 1)
	if d.Days > maxInt-d.CalendarDays {
		return fmt.Errorf("%w: calendar-day component overflows", ErrOffset)
	}
	if d.Nanoseconds > 999_999_999 {
		return fmt.Errorf("%w: nanoseconds must be less than one second", ErrOffset)
	}
	// time.Time.Add and time.Duration are intentionally bounded here. A
	// duration too large to represent cannot become a deterministic wake.
	if durationUnits(d) >= maxDurationUnits {
		return fmt.Errorf("%w: total offset exceeds time.Duration range", ErrOffset)
	}
	return nil
}

// Validate reports whether all components fit the pure calendar arithmetic
// contract.
func (d CalendarDuration) Validate() error { return d.validate() }

func (d CalendarDuration) isZero() bool {
	return d.CalendarDays == 0 && d.Days == 0 && d.Hours == 0 && d.Minutes == 0 && d.Seconds == 0 && d.Nanoseconds == 0
}

func (d CalendarDuration) String() string {
	if d.isZero() {
		return "0d"
	}
	return fmt.Sprintf("%dd %dh %dm %ds %dns", d.calendarDays(), d.Hours, d.Minutes, d.Seconds, d.Nanoseconds)
}

// NewCalendarDuration creates an offset using civil calendar days followed by
// local hours, minutes and seconds. All inputs are explicit and validated.
func NewCalendarDuration(days, hours, minutes, seconds int) (CalendarDuration, error) {
	d := CalendarDuration{CalendarDays: days, Hours: hours, Minutes: minutes, Seconds: seconds}
	if err := d.validate(); err != nil {
		return CalendarDuration{}, err
	}
	return d, nil
}

// SLAClock is an immutable, versioned SLA definition. Zone and Calendar are
// mandatory references, including their release versions. ReferenceUpdatePolicy
// declares what a timer replay does if either dataset is republished.
type SLAClock struct {
	ClockID string `json:"clock_id"`
	ID      string `json:"id,omitempty"`

	Version           uint32 `json:"version"`
	DefinitionVersion uint32 `json:"definition_version,omitempty"`

	Zone     values.ZoneRef     `json:"zone"`
	Calendar values.CalendarRef `json:"calendar"`

	ReferenceUpdatePolicy values.ReferenceUpdatePolicy `json:"reference_update_policy"`
	Policy                values.ReferenceUpdatePolicy `json:"policy,omitempty"`
	Disambiguation        values.Disambiguation        `json:"disambiguation,omitempty"`

	ReminderOffset   CalendarDuration `json:"reminder_offset"`
	EscalationOffset CalendarDuration `json:"escalation_offset"`
	ExpiryOffset     CalendarDuration `json:"expiry_offset"`

	// The short names are accepted for ergonomic construction and normalize to
	// the Offset fields above. They are never both allowed to disagree.
	Reminder   CalendarDuration `json:"reminder,omitempty"`
	Escalation CalendarDuration `json:"escalation,omitempty"`
	Expiry     CalendarDuration `json:"expiry,omitempty"`
}

// Definition is an alias that makes the definition/instance relationship
// explicit to callers without creating a second contract.
type Definition = SLAClock
type ClockDefinition = SLAClock
type SLAClockDefinition = SLAClock

func (c SLAClock) clockID() string {
	if c.ClockID != "" {
		return c.ClockID
	}
	return c.ID
}

func (c SLAClock) clockVersion() uint32 {
	if c.Version != 0 {
		return c.Version
	}
	return c.DefinitionVersion
}

func (c SLAClock) policy() values.ReferenceUpdatePolicy {
	if c.ReferenceUpdatePolicy != values.ReferenceUpdateUnspecified {
		return c.ReferenceUpdatePolicy
	}
	return c.Policy
}

func chooseOffset(primary, alias CalendarDuration, name string) (CalendarDuration, error) {
	if !primary.isZero() && !alias.isZero() && primary != alias {
		return CalendarDuration{}, fmt.Errorf("%w: %s fields disagree", ErrInvalidClock, name)
	}
	if !primary.isZero() {
		return primary, nil
	}
	return alias, nil
}

func (c SLAClock) offsets() (CalendarDuration, CalendarDuration, CalendarDuration, error) {
	reminder, err := chooseOffset(c.ReminderOffset, c.Reminder, "reminder")
	if err != nil {
		return CalendarDuration{}, CalendarDuration{}, CalendarDuration{}, err
	}
	escalation, err := chooseOffset(c.EscalationOffset, c.Escalation, "escalation")
	if err != nil {
		return CalendarDuration{}, CalendarDuration{}, CalendarDuration{}, err
	}
	expiry, err := chooseOffset(c.ExpiryOffset, c.Expiry, "expiry")
	if err != nil {
		return CalendarDuration{}, CalendarDuration{}, CalendarDuration{}, err
	}
	return reminder, escalation, expiry, nil
}

// Validate proves that this clock can be replayed without ambient timezone,
// calendar, policy, or wall-clock state.
func (c SLAClock) Validate() error {
	switch {
	case strings.TrimSpace(c.clockID()) == "":
		return fmt.Errorf("%w: %v", ErrClockIdentity, ErrInvalidClock)
	case c.clockVersion() == 0:
		return fmt.Errorf("%w: %v", ErrClockVersion, ErrInvalidClock)
	case c.clockVersion() != c.Version && c.Version != 0 && c.DefinitionVersion != 0:
		return fmt.Errorf("%w: version fields disagree", ErrInvalidClock)
	case c.ClockID != "" && c.ID != "" && c.ClockID != c.ID:
		return fmt.Errorf("%w: clock identity fields disagree", ErrInvalidClock)
	case c.ReferenceUpdatePolicy != values.ReferenceUpdateUnspecified && c.Policy != values.ReferenceUpdateUnspecified && c.ReferenceUpdatePolicy != c.Policy:
		return fmt.Errorf("%w: policy fields disagree", ErrInvalidClock)
	}
	if err := c.Zone.Validate(); err != nil {
		return err
	}
	if err := c.Calendar.Validate(); err != nil {
		return err
	}
	policy := c.policy()
	if policy == values.ReferenceUpdateUnspecified {
		return ErrClockPolicy
	}
	if !policy.Valid() {
		return fmt.Errorf("%w: %d", values.ErrReferenceUpdatePolicy, uint8(policy))
	}
	if c.Disambiguation != values.DisambiguationUnspecified && !c.Disambiguation.Valid() {
		return fmt.Errorf("%w: %d", values.ErrDisambiguation, uint8(c.Disambiguation))
	}
	reminder, escalation, expiry, err := c.offsets()
	if err != nil {
		return err
	}
	for name, offset := range map[string]CalendarDuration{
		"reminder": reminder, "escalation": escalation, "expiry": expiry,
	} {
		if err := offset.validate(); err != nil {
			return fmt.Errorf("%w: %s: %v", ErrInvalidClock, name, err)
		}
	}
	if reminderGreater(reminder, escalation) || reminderGreater(escalation, expiry) {
		return fmt.Errorf("%w: offsets must be reminder <= escalation <= expiry", ErrInvalidClock)
	}
	if expiry.isZero() {
		return fmt.Errorf("%w: expiry offset is required", ErrInvalidClock)
	}
	return nil
}

func reminderGreater(a, b CalendarDuration) bool {
	return durationUnits(a) > durationUnits(b)
}

func durationUnits(d CalendarDuration) int64 {
	units := int64(0)
	add := func(value, multiplier int64) {
		if value <= 0 || units >= maxDurationUnits || value > (maxDurationUnits-units)/multiplier {
			if value > 0 {
				units = maxDurationUnits
			}
			return
		}
		units += value * multiplier
	}
	add(int64(d.calendarDays()), 24*60*60*1_000_000_000)
	add(int64(d.Hours), 60*60*1_000_000_000)
	add(int64(d.Minutes), 60*1_000_000_000)
	add(int64(d.Seconds), 1_000_000_000)
	add(int64(d.Nanoseconds), 1)
	return units
}

const maxDurationUnits = int64(1<<63 - 1)

// Explain returns a deterministic, value-free description of the clock
// contract. It names the pinned references and offsets but no work-item data.
func (c SLAClock) Explain() string {
	reminder, escalation, expiry, _ := c.offsets()
	return fmt.Sprintf("sla clock %s v%d; zone=%s; calendar=%s; policy=%s; reminder=%s; escalation=%s; expiry=%s",
		c.clockID(), c.clockVersion(), c.Zone.String(), c.Calendar.String(), c.policy().String(),
		reminder.String(), escalation.String(), expiry.String())
}

// Explain is the package-level explanation required of semantic packages.
func Explain() string {
	return "sla clocks use explicit versioned zones/calendars, civil offsets, durable wait requirements, and idempotent reminder/escalation/expiry signals"
}

// PauseRecord is immutable evidence of one pause interval. A pause remains in
// the instance even if it was quiet; its elapsed time is added to every later
// deadline only by Resume.
type PauseRecord struct {
	PausedAt     values.Instant `json:"paused_at"`
	PauseReason  string         `json:"pause_reason"`
	ResumedAt    values.Instant `json:"resumed_at,omitempty"`
	ResumeReason string         `json:"resume_reason,omitempty"`
}

func (p PauseRecord) complete() bool { return p.ResumedAt.IsSet() }

// Instance is a clock bound to one work item and one caller-supplied start
// instant. It contains no goroutine or mutable process-global state.
type Instance struct {
	WorkItemID string         `json:"work_item_id"`
	Clock      SLAClock       `json:"clock"`
	StartedAt  values.Instant `json:"started_at"`
	Pauses     []PauseRecord  `json:"pauses,omitempty"`
}

// ClockInstance is an alias for callers that prefer the longer name.
type ClockInstance = Instance

// NewInstance binds a clock to a work item. The identity and start are
// accepted as stringer/time values so both workitem UUIDs and kernel Instants
// can be passed without importing a storage or domain adapter into this pure
// package.
func NewInstance(clock SLAClock, workItemID any, startedAt any) (Instance, error) {
	if err := clock.Validate(); err != nil {
		return Instance{}, err
	}
	id, err := identityText(workItemID)
	if err != nil {
		return Instance{}, err
	}
	start, err := instantValue(startedAt)
	if err != nil {
		return Instance{}, err
	}
	return Instance{WorkItemID: id, Clock: clock, StartedAt: start}, nil
}

// NewClockInstance is the explicit constructor spelling for an SLA clock
// instance.
func NewClockInstance(clock SLAClock, workItemID any, startedAt any) (Instance, error) {
	return NewInstance(clock, workItemID, startedAt)
}

func identityText(v any) (string, error) {
	if v == nil {
		return "", ErrWorkItemIdentity
	}
	var text string
	switch value := v.(type) {
	case string:
		text = value
	case fmt.Stringer:
		text = value.String()
	default:
		return "", fmt.Errorf("%w: unsupported identity type %T", ErrWorkItemIdentity, v)
	}
	if strings.TrimSpace(text) == "" {
		return "", ErrWorkItemIdentity
	}
	return text, nil
}

func instantValue(v any) (values.Instant, error) {
	switch value := v.(type) {
	case values.Instant:
		if err := value.Validate(); err != nil {
			return values.Instant{}, fmt.Errorf("%w: %v", ErrStartRequired, err)
		}
		return value, nil
	case time.Time:
		if value.IsZero() {
			return values.Instant{}, ErrStartRequired
		}
		return values.NewInstant(value), nil
	default:
		return values.Instant{}, fmt.Errorf("%w: unsupported start type %T", ErrStartRequired, v)
	}
}

// Validate checks instance identity and pause history.
func (i Instance) Validate() error {
	if strings.TrimSpace(i.WorkItemID) == "" {
		return ErrWorkItemIdentity
	}
	if err := i.Clock.Validate(); err != nil {
		return err
	}
	if err := i.StartedAt.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrStartRequired, err)
	}
	open := false
	for n, pause := range i.Pauses {
		if err := pause.PausedAt.Validate(); err != nil {
			return fmt.Errorf("%w: pause %d: %v", ErrPauseOrder, n, err)
		}
		if strings.TrimSpace(pause.PauseReason) == "" {
			return fmt.Errorf("%w: pause %d", ErrPauseReason, n)
		}
		if open {
			return fmt.Errorf("%w: pause %d follows an open pause", ErrPauseOrder, n)
		}
		if pause.complete() {
			if err := pause.ResumedAt.Validate(); err != nil || !pause.ResumedAt.After(pause.PausedAt) {
				return fmt.Errorf("%w: pause %d resume must be after pause", ErrPauseOrder, n)
			}
			if strings.TrimSpace(pause.ResumeReason) == "" {
				return fmt.Errorf("%w: resume reason for pause %d", ErrPauseReason, n)
			}
		} else {
			open = true
		}
	}
	return nil
}

// IsPaused reports whether the instance currently has an open pause.
func (i Instance) IsPaused() bool {
	return len(i.Pauses) > 0 && !i.Pauses[len(i.Pauses)-1].complete()
}

// Pause records a pause and returns a new instance. A reason and explicit
// instant are required; the pause is never represented by deleting waits.
func (i Instance) Pause(at any, reason string) (Instance, error) {
	if err := i.Validate(); err != nil {
		return Instance{}, err
	}
	if i.IsPaused() {
		return Instance{}, ErrAlreadyPaused
	}
	if strings.TrimSpace(reason) == "" {
		return Instance{}, ErrPauseReason
	}
	when, err := instantValue(at)
	if err != nil {
		return Instance{}, err
	}
	if !when.After(i.StartedAt) {
		return Instance{}, ErrPauseOrder
	}
	if len(i.Pauses) > 0 && !when.After(i.Pauses[len(i.Pauses)-1].ResumedAt) {
		return Instance{}, ErrPauseOrder
	}
	out := i.clone()
	out.Pauses = append(out.Pauses, PauseRecord{PausedAt: when, PauseReason: reason})
	return out, nil
}

// Resume closes the current pause, recording why it resumed. The elapsed
// interval becomes an explicit deadline shift in every subsequent requirement.
func (i Instance) Resume(at any, reason string) (Instance, error) {
	if err := i.Validate(); err != nil {
		return Instance{}, err
	}
	if !i.IsPaused() {
		return Instance{}, ErrNotPaused
	}
	if strings.TrimSpace(reason) == "" {
		return Instance{}, ErrPauseReason
	}
	when, err := instantValue(at)
	if err != nil {
		return Instance{}, err
	}
	last := i.Pauses[len(i.Pauses)-1]
	if !when.After(last.PausedAt) {
		return Instance{}, ErrPauseOrder
	}
	out := i.clone()
	out.Pauses[len(out.Pauses)-1].ResumedAt = when
	out.Pauses[len(out.Pauses)-1].ResumeReason = reason
	return out, nil
}

// PauseAt and ResumeAt are typed convenience spellings for callers already
// holding kernel instants.
func (i Instance) PauseAt(at values.Instant, reason string) (Instance, error) {
	return i.Pause(at, reason)
}

func (i Instance) ResumeAt(at values.Instant, reason string) (Instance, error) {
	return i.Resume(at, reason)
}

func (i Instance) clone() Instance {
	out := i
	out.Pauses = append([]PauseRecord(nil), i.Pauses...)
	return out
}

func (i Instance) pausedDuration() time.Duration {
	var total time.Duration
	for _, pause := range i.Pauses {
		if !pause.complete() {
			continue
		}
		total += pause.ResumedAt.Time().Sub(pause.PausedAt.Time())
	}
	return total
}

func (i Instance) offset(kind SignalKind) (CalendarDuration, error) {
	reminder, escalation, expiry, err := i.Clock.offsets()
	if err != nil {
		return CalendarDuration{}, err
	}
	switch kind {
	case SignalReminder:
		return reminder, nil
	case SignalEscalation:
		return escalation, nil
	case SignalExpiry:
		return expiry, nil
	default:
		return CalendarDuration{}, fmt.Errorf("%w: %q", ErrSignalKind, kind)
	}
}

func (i Instance) deadline(kind SignalKind) (values.Instant, error) {
	offset, err := i.offset(kind)
	if err != nil {
		return values.Instant{}, err
	}
	base, err := addCalendarDuration(i.StartedAt, offset, i.Clock.Zone, i.Clock.Disambiguation)
	if err != nil {
		return values.Instant{}, err
	}
	shifted := base.Time().Add(i.pausedDuration())
	return values.NewInstant(shifted), nil
}

// Deadline returns the current deadline for one signal. Completed pause
// intervals shift it by their measured elapsed time; an open pause remains in
// the history and does not disappear from the returned requirement set.
func (i Instance) Deadline(kind SignalKind) (values.Instant, error) {
	if err := i.Validate(); err != nil {
		return values.Instant{}, err
	}
	return i.deadline(kind)
}

func addCalendarDuration(start values.Instant, offset CalendarDuration, zone values.ZoneRef, disambiguation values.Disambiguation) (values.Instant, error) {
	if err := start.Validate(); err != nil {
		return values.Instant{}, err
	}
	if err := offset.validate(); err != nil {
		return values.Instant{}, err
	}
	loc, err := zone.Location()
	if err != nil {
		return values.Instant{}, err
	}
	local := start.Time().In(loc)
	// Do arithmetic on a civil representation in UTC, then resolve that wall
	// clock against the explicit zone. This makes calendar days survive DST.
	civil := time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), local.Minute(), local.Second(), local.Nanosecond(), time.UTC)
	civil = civil.AddDate(0, 0, offset.calendarDays())
	nanos := durationUnits(offset) - int64(offset.calendarDays())*24*60*60*1_000_000_000
	if nanos < 0 {
		return values.Instant{}, ErrOffset
	}
	civil = civil.Add(time.Duration(nanos))
	date, err := values.ParseLocalDate(civil.Format("2006-01-02"))
	if err != nil {
		return values.Instant{}, err
	}
	tod, err := values.ParseLocalTime(civil.Format("15:04:05.999999999"))
	if err != nil {
		return values.Instant{}, err
	}
	if disambiguation == values.DisambiguationUnspecified {
		disambiguation = values.DisambiguationEarlier
	}
	zdt, err := values.NewZonedDateTime(date, tod, zone, disambiguation)
	if err != nil {
		return values.Instant{}, err
	}
	return zdt.Instant(), nil
}

func datasetFor(clock SLAClock, override []values.DatasetVersions) (values.DatasetVersions, error) {
	if len(override) > 1 {
		return values.DatasetVersions{}, ErrTooManyDatasets
	}
	if len(override) == 1 {
		return override[0], nil
	}
	dataset := values.DatasetVersions{TzdbVersion: clock.Zone.TzdbVersion, CalendarVersion: clock.Calendar.Version}
	if err := dataset.Validate(); err != nil {
		return values.DatasetVersions{}, fmt.Errorf("%w: %w", ErrDatasetUnavailable, err)
	}
	return dataset, nil
}

// Requirement derives one WF-RUN-004-compatible durable wake requirement.
// The optional dataset is explicit; when omitted the clock's historical
// dataset is used. No ambient tzdb or calendar version is consulted.
func (i Instance) Requirement(kind SignalKind, dataset ...values.DatasetVersions) (wait.TimerRequirement, error) {
	if err := i.Validate(); err != nil {
		return wait.TimerRequirement{}, err
	}
	due, err := i.deadline(kind)
	if err != nil {
		return wait.TimerRequirement{}, err
	}
	ds, err := datasetFor(i.Clock, dataset)
	if err != nil {
		return wait.TimerRequirement{}, err
	}
	node := wait.CompiledWaitNode{
		WorkflowID:      i.Clock.clockID(),
		WorkflowVersion: i.Clock.clockVersion(),
		NodeID:          "sla." + strings.ToLower(string(kind)),
		WakeInstant:     due,
		Zone:            i.Clock.Zone,
		Calendar:        i.Clock.Calendar,
		Policy:          i.Clock.policy(),
	}
	requirement, err := wait.ComputeTimerRequirement(node, ds)
	if err != nil {
		return wait.TimerRequirement{}, err
	}
	return requirement, nil
}

// Requirements returns reminder, escalation, and expiry requirements in that
// stable order. A pause therefore leaves three visible requirements in place,
// rather than turning a quiet wait into missing work.
func (i Instance) Requirements(dataset ...values.DatasetVersions) ([]wait.TimerRequirement, error) {
	kinds := []SignalKind{SignalReminder, SignalEscalation, SignalExpiry}
	out := make([]wait.TimerRequirement, 0, len(kinds))
	for _, kind := range kinds {
		requirement, err := i.Requirement(kind, dataset...)
		if err != nil {
			return nil, err
		}
		out = append(out, requirement)
	}
	return out, nil
}

// Signal is one semantic SLA signal. Its ID is derived from the work item,
// clock revision, and signal kind, so retries and deadline rescheduling address
// the same business signal rather than emitting a duplicate.
type Signal struct {
	SignalID          string                `json:"signal_id"`
	ID                string                `json:"id,omitempty"`
	WorkItemID        string                `json:"work_item_id"`
	ClockID           string                `json:"clock_id"`
	ClockVersion      uint32                `json:"clock_version"`
	Kind              SignalKind            `json:"kind"`
	DueAt             values.Instant        `json:"-"`
	At                values.Instant        `json:"-"`
	RequirementDigest string                `json:"requirement_digest"`
	Requirement       wait.TimerRequirement `json:"-"`
}

// DerivedSignalID returns the stable idempotency key for one semantic signal.
func DerivedSignalID(workItemID, clockID string, clockVersion uint32, kind SignalKind) string {
	h := sha256.New()
	h.Write([]byte("hcmnext.humanwork.sla.Signal/v1"))
	h.Write([]byte{0})
	fmt.Fprintf(h, "%s\x00%s\x00%d\x00%s", workItemID, clockID, clockVersion, kind)
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// SignalID is a concise alias for DerivedSignalID.
func SignalID(workItemID, clockID string, clockVersion uint32, kind SignalKind) string {
	return DerivedSignalID(workItemID, clockID, clockVersion, kind)
}

// Emit returns one signal identity and its current durable requirement. The
// same call is a semantic replay: SignalID is identical and no in-memory
// emitted-set is needed for correctness.
func (i Instance) Emit(kind SignalKind, dataset ...values.DatasetVersions) (Signal, error) {
	requirement, err := i.Requirement(kind, dataset...)
	if err != nil {
		return Signal{}, err
	}
	signal := Signal{
		SignalID:          DerivedSignalID(i.WorkItemID, i.Clock.clockID(), i.Clock.clockVersion(), kind),
		WorkItemID:        i.WorkItemID,
		ClockID:           i.Clock.clockID(),
		ClockVersion:      i.Clock.clockVersion(),
		Kind:              kind,
		DueAt:             requirement.FireAt,
		At:                requirement.FireAt,
		RequirementDigest: requirement.Digest,
		Requirement:       requirement,
	}
	signal.ID = signal.SignalID
	return signal, nil
}

// Signal is a named alias for Emit for callers expressing the read side of a
// signal projection.
func (i Instance) Signal(kind SignalKind, dataset ...values.DatasetVersions) (Signal, error) {
	return i.Emit(kind, dataset...)
}

// Signals returns one deterministic signal for each SLA milestone. A caller
// can safely retry this projection and deduplicate by SignalID.
func (i Instance) Signals(dataset ...values.DatasetVersions) ([]Signal, error) {
	kinds := []SignalKind{SignalReminder, SignalEscalation, SignalExpiry}
	out := make([]Signal, 0, len(kinds))
	for _, kind := range kinds {
		signal, err := i.Emit(kind, dataset...)
		if err != nil {
			return nil, err
		}
		out = append(out, signal)
	}
	return out, nil
}
