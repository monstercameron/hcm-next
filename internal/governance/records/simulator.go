// Package records contains the Gate A, non-destructive retention simulator.
// It evaluates record metadata and produces evidence without opening a
// database, reading payload bytes, or deleting anything.
package records

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Status string

const (
	Eligible           Status = "ELIGIBLE"
	BlockedWithReasons Status = "BLOCKED_WITH_REASONS"
	RepairRequired     Status = "REPAIR_REQUIRED"
)

var ErrInvalid = errors.New("records simulator: invalid")

// Copy is metadata for one physical or logical copy. Payload bytes are
// intentionally absent from this type.
type Copy struct {
	ID                  string
	RecordSeries        string
	Custodian           string
	Jurisdiction        string
	CreatedAt           time.Time
	CutoffAt            *time.Time
	ArchiveAcknowledged bool
	ActiveHoldRefs      []string
}

// RetentionRule is a jurisdiction-scoped minimum and optional mandatory
// destruction maximum for one record series.
type RetentionRule struct {
	RecordSeries string
	Jurisdiction string
	MinimumDays  int
	MaximumDays  int
	AuthorityRef string
}

// CutoffCorrection is an append-only correction to a copy's cutoff history.
type CutoffCorrection struct {
	CopyID          string
	At              time.Time
	PreviousCutoff  *time.Time
	CorrectedCutoff *time.Time
	Reason          string
}

type SimulationRequest struct {
	AsOf        time.Time
	Copies      []Copy
	Rules       []RetentionRule
	Corrections []CutoffCorrection
}

type Blocker struct {
	CopyID string
	Code   string
	Detail string
}

type ComposedSchedule struct {
	RecordSeries  string
	Jurisdiction  string
	MinimumDays   int
	MaximumDays   int
	AuthorityRefs []string
}

type CutoffEvent struct {
	CopyID string
	At     time.Time
	From   *time.Time
	To     *time.Time
	Reason string
}

type CopyDisposition struct {
	CopyID     string
	Status     Status
	CutoffAt   *time.Time
	EligibleAt *time.Time
	Schedule   ComposedSchedule
	Blockers   []Blocker
}

type Report struct {
	Status        Status
	Copies        []CopyDisposition
	Schedules     []ComposedSchedule
	CutoffHistory []CutoffEvent
	Blockers      []Blocker
	DeletionCount int
	DeletedBytes  int64
	Digest        string
}

func (c Copy) validate() error {
	if strings.TrimSpace(c.ID) == "" {
		return fmt.Errorf("%w: copy requires an id", ErrInvalid)
	}
	if c.CreatedAt.IsZero() {
		return fmt.Errorf("%w: copy %q has no creation time", ErrInvalid, c.ID)
	}
	if c.CutoffAt != nil && c.CutoffAt.Before(c.CreatedAt) {
		return fmt.Errorf("%w: copy %q cutoff precedes creation", ErrInvalid, c.ID)
	}
	return nil
}

func (r RetentionRule) validate() error {
	if strings.TrimSpace(r.RecordSeries) == "" || strings.TrimSpace(r.Jurisdiction) == "" || strings.TrimSpace(r.AuthorityRef) == "" {
		return fmt.Errorf("%w: retention rule requires series, jurisdiction and authority", ErrInvalid)
	}
	if r.MinimumDays < 0 || (r.MaximumDays > 0 && r.MaximumDays < r.MinimumDays) {
		return fmt.Errorf("%w: retention rule %q has an invalid interval", ErrInvalid, r.RecordSeries)
	}
	return nil
}

func (c CutoffCorrection) validate() error {
	if strings.TrimSpace(c.CopyID) == "" || c.At.IsZero() || c.CorrectedCutoff == nil || strings.TrimSpace(c.Reason) == "" {
		return fmt.Errorf("%w: cutoff correction is incomplete", ErrInvalid)
	}
	if c.PreviousCutoff != nil && c.CorrectedCutoff.Before(*c.PreviousCutoff) {
		return fmt.Errorf("%w: cutoff correction moves backwards", ErrInvalid)
	}
	return nil
}

// Simulate evaluates every copy and returns an evidence-bearing report. The
// result never authorizes deletion: DeletionCount and DeletedBytes are always
// zero by construction.
func Simulate(in SimulationRequest) (Report, error) {
	if in.AsOf.IsZero() {
		return Report{}, fmt.Errorf("%w: as-of time is required", ErrInvalid)
	}
	if len(in.Copies) == 0 {
		return Report{}, fmt.Errorf("%w: at least one copy is required", ErrInvalid)
	}
	for _, r := range in.Rules {
		if err := r.validate(); err != nil {
			return Report{}, err
		}
	}
	for _, c := range in.Copies {
		if err := c.validate(); err != nil {
			return Report{}, err
		}
	}
	for _, c := range in.Corrections {
		if err := c.validate(); err != nil {
			return Report{}, err
		}
	}

	corrections := map[string][]CutoffCorrection{}
	for _, correction := range in.Corrections {
		corrections[correction.CopyID] = append(corrections[correction.CopyID], correction)
	}
	for copyID := range corrections {
		sort.Slice(corrections[copyID], func(i, j int) bool { return corrections[copyID][i].At.Before(corrections[copyID][j].At) })
	}

	schedules, conflicts := composeSchedules(in.Rules)
	bySeriesJurisdiction := make(map[string]ComposedSchedule, len(schedules))
	for _, schedule := range schedules {
		bySeriesJurisdiction[schedule.RecordSeries+"\x00"+schedule.Jurisdiction] = schedule
	}

	report := Report{Status: Eligible, Schedules: schedules}
	for _, copy := range in.Copies {
		item := CopyDisposition{CopyID: copy.ID, Status: Eligible}
		key := copy.RecordSeries + "\x00" + copy.Jurisdiction
		item.Schedule = bySeriesJurisdiction[key]
		var blockers []Blocker
		if strings.TrimSpace(copy.RecordSeries) == "" {
			blockers = append(blockers, Blocker{CopyID: copy.ID, Code: "RECORD_SERIES_UNKNOWN", Detail: "the copy is not bound to a known record series"})
		}
		if strings.TrimSpace(copy.Custodian) == "" {
			blockers = append(blockers, Blocker{CopyID: copy.ID, Code: "CUSTODIAN_UNKNOWN", Detail: "the copy has no accountable custodian"})
		}
		if strings.TrimSpace(copy.Jurisdiction) == "" {
			blockers = append(blockers, Blocker{CopyID: copy.ID, Code: "JURISDICTION_UNKNOWN", Detail: "the copy has no governing jurisdiction"})
		}
		if len(conflicts[copy.RecordSeries]) > 0 {
			blockers = append(blockers, conflicts[copy.RecordSeries]...)
		}
		if item.Schedule.RecordSeries == "" {
			blockers = append(blockers, Blocker{CopyID: copy.ID, Code: "RETENTION_SCHEDULE_UNKNOWN", Detail: "no jurisdiction-scoped rule resolves this record series"})
		}

		cutoff := copy.CutoffAt
		for _, correction := range corrections[copy.ID] {
			from := cutoff
			cutoff = correction.CorrectedCutoff
			report.CutoffHistory = append(report.CutoffHistory, CutoffEvent{CopyID: copy.ID, At: correction.At, From: cloneTime(from), To: cloneTime(cutoff), Reason: correction.Reason})
		}
		item.CutoffAt = cloneTime(cutoff)
		if cutoff == nil {
			blockers = append(blockers, Blocker{CopyID: copy.ID, Code: "CUTOFF_UNCERTAIN", Detail: "no effective cutoff or correction is recorded"})
		}
		if len(copy.ActiveHoldRefs) > 0 {
			blockers = append(blockers, Blocker{CopyID: copy.ID, Code: "ACTIVE_LEGAL_HOLD", Detail: "an active hold blocks disposition"})
		}
		if !copy.ArchiveAcknowledged {
			blockers = append(blockers, Blocker{CopyID: copy.ID, Code: "ARCHIVE_ACKNOWLEDGEMENT_MISSING", Detail: "archive custody has not acknowledged the copy"})
		}

		if len(blockers) == 0 {
			item.EligibleAt = cloneTime(cutoff)
			item.EligibleAt = addDays(item.EligibleAt, item.Schedule.MinimumDays)
			if in.AsOf.Before(*item.EligibleAt) {
				blockers = append(blockers, Blocker{CopyID: copy.ID, Code: "RETENTION_NOT_MET", Detail: "the minimum retention period has not elapsed"})
			}
		}
		item.Blockers = append([]Blocker(nil), blockers...)
		if len(blockers) > 0 {
			item.Status = BlockedWithReasons
			for _, blocker := range blockers {
				if blocker.Code == "RETENTION_SCHEDULE_UNKNOWN" || blocker.Code == "CUTOFF_UNCERTAIN" || blocker.Code == "RECORD_SERIES_UNKNOWN" || blocker.Code == "CUSTODIAN_UNKNOWN" || blocker.Code == "JURISDICTION_UNKNOWN" {
					item.Status = RepairRequired
				}
			}
			report.Blockers = append(report.Blockers, blockers...)
		}
		report.Copies = append(report.Copies, item)
	}
	for _, item := range report.Copies {
		if item.Status == RepairRequired {
			report.Status = RepairRequired
		} else if item.Status == BlockedWithReasons && report.Status == Eligible {
			report.Status = BlockedWithReasons
		}
	}
	sort.Slice(report.CutoffHistory, func(i, j int) bool { return report.CutoffHistory[i].At.Before(report.CutoffHistory[j].At) })
	sort.Slice(report.Blockers, func(i, j int) bool {
		if report.Blockers[i].CopyID != report.Blockers[j].CopyID {
			return report.Blockers[i].CopyID < report.Blockers[j].CopyID
		}
		return report.Blockers[i].Code < report.Blockers[j].Code
	})
	report.Digest = digest(report)
	return report, nil
}

func composeSchedules(rules []RetentionRule) ([]ComposedSchedule, map[string][]Blocker) {
	type aggregate struct {
		schedule      ComposedSchedule
		jurisdictions []RetentionRule
	}
	aggregates := map[string]*aggregate{}
	for _, rule := range rules {
		key := rule.RecordSeries + "\x00" + rule.Jurisdiction
		ag := aggregates[key]
		if ag == nil {
			ag = &aggregate{schedule: ComposedSchedule{RecordSeries: rule.RecordSeries, Jurisdiction: rule.Jurisdiction}}
			aggregates[key] = ag
		}
		if rule.MinimumDays > ag.schedule.MinimumDays {
			ag.schedule.MinimumDays = rule.MinimumDays
		}
		if ag.schedule.MaximumDays == 0 || (rule.MaximumDays > 0 && rule.MaximumDays < ag.schedule.MaximumDays) {
			ag.schedule.MaximumDays = rule.MaximumDays
		}
		ag.schedule.AuthorityRefs = append(ag.schedule.AuthorityRefs, rule.AuthorityRef)
		ag.jurisdictions = append(ag.jurisdictions, rule)
	}
	keys := make([]string, 0, len(aggregates))
	for key := range aggregates {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]ComposedSchedule, 0, len(keys))
	for _, key := range keys {
		ag := aggregates[key]
		sort.Strings(ag.schedule.AuthorityRefs)
		out = append(out, ag.schedule)
	}
	conflicts := map[string][]Blocker{}
	bySeries := map[string][]ComposedSchedule{}
	for _, schedule := range out {
		bySeries[schedule.RecordSeries] = append(bySeries[schedule.RecordSeries], schedule)
	}
	for series, schedules := range bySeries {
		for _, left := range schedules {
			for _, right := range schedules {
				if left.Jurisdiction >= right.Jurisdiction || left.MaximumDays == 0 {
					continue
				}
				if right.MinimumDays > left.MaximumDays {
					conflicts[series] = append(conflicts[series], Blocker{Code: "RETENTION_SCHEDULE_CONFLICT", Detail: fmt.Sprintf("%s maximum %d days is shorter than %s minimum %d days", left.Jurisdiction, left.MaximumDays, right.Jurisdiction, right.MinimumDays)})
				}
			}
		}
	}
	for series := range conflicts {
		sort.Slice(conflicts[series], func(i, j int) bool { return conflicts[series][i].Detail < conflicts[series][j].Detail })
	}
	return out, conflicts
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}
func addDays(value *time.Time, days int) *time.Time {
	if value == nil {
		return nil
	}
	out := value.AddDate(0, 0, days)
	return &out
}

func digest(report Report) string {
	copyReport := report
	copyReport.Digest = ""
	b, _ := json.Marshal(copyReport)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Explain gives operators a concise, payload-free summary.
func (r Report) Explain() string {
	return fmt.Sprintf("retention simulation status=%s copies=%d blockers=%d deletions=%d digest=%s", r.Status, len(r.Copies), len(r.Blockers), r.DeletionCount, r.Digest)
}
