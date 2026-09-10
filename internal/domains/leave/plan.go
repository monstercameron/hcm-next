package leave

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Plan segment kinds: the closed partition vocabulary.
const (
	SegmentProtected = "protected"
	SegmentPaid      = "paid"
	SegmentUnpaid    = "unpaid"
	SegmentUnknown   = "unknown"
)

// Segment is one non-overlapping interval slice with its kind, covering
// programs and exact hours.
type Segment struct {
	StartDay int
	EndDay   int
	Kind     string
	Programs []string
	Hours    int
}

// EffectPlan is one downstream effect descriptor. Plans describe;
// nothing posts balances or effects.
type EffectPlan struct {
	System string
	Action string
	Detail string
}

// PlanRequest partitions one requested interval. Timezone binds the plan:
// rezoning always produces a new plan, never an implicit change.
type PlanRequest struct {
	StartDay         int
	EndDay           int
	HoursPerDay      int
	BalanceAvailable int
	HolidayDays      []int
	Programs         []ProgramResult
	Timezone         string
}

// LeaveEntitlementPlan is the immutable partitioned plan with zero
// mutation: calculation lives here, never in the workflow.
type LeaveEntitlementPlan struct {
	Segments       []Segment
	ScheduledHours int
	PlannedDebits  int
	Obligations    []string
	Effects        []EffectPlan
	Trace          []string
	Timezone       string
	Digest         string
}

func planDigest(request PlanRequest, plan LeaveEntitlementPlan) string {
	parts := []string{"leave-entitlement-plan", request.Timezone, fmt.Sprint(request.StartDay, request.EndDay, request.HoursPerDay, request.BalanceAvailable)}
	holidays := append([]int(nil), request.HolidayDays...)
	sort.Ints(holidays)
	for _, day := range holidays {
		parts = append(parts, "holiday="+fmt.Sprint(day))
	}
	for _, segment := range plan.Segments {
		programs := append([]string(nil), segment.Programs...)
		sort.Strings(programs)
		parts = append(parts, strings.Join([]string{fmt.Sprint(segment.StartDay, segment.EndDay), segment.Kind, strings.Join(programs, ","), fmt.Sprint(segment.Hours)}, "\x01"))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ComposePlan partitions the requested interval into non-overlapping
// segments. Overlapping programs never double-count protection; paid
// hours never exceed schedule or balance; holidays are explicit unpaid
// segments; unknown program results stay unknown, never unpaid.
func ComposePlan(request PlanRequest) (LeaveEntitlementPlan, error) {
	if request.EndDay < request.StartDay {
		return LeaveEntitlementPlan{}, fmt.Errorf("leave: plan interval is inverted")
	}
	if request.HoursPerDay <= 0 || request.BalanceAvailable < 0 {
		return LeaveEntitlementPlan{}, fmt.Errorf("leave: plan needs positive daily hours and a non-negative balance")
	}
	if strings.TrimSpace(request.Timezone) == "" {
		return LeaveEntitlementPlan{}, fmt.Errorf("leave: plan binds an explicit timezone")
	}
	plan := LeaveEntitlementPlan{Timezone: request.Timezone}
	chronicle := func(format string, args ...any) {
		plan.Trace = append(plan.Trace, fmt.Sprintf(format, args...))
	}
	holidays := make(map[int]bool, len(request.HolidayDays))
	for _, day := range request.HolidayDays {
		if day < request.StartDay || day > request.EndDay {
			return LeaveEntitlementPlan{}, fmt.Errorf("leave: holiday %d falls outside the requested interval", day)
		}
		holidays[day] = true
	}
	unknownPrograms := false
	paidPrograms := make(map[string]bool)
	protectedPrograms := make(map[string]bool)
	for _, program := range request.Programs {
		switch program.Result {
		case ProgramEligible:
			protectedPrograms[program.ProgramID] = true
			paidPrograms[program.ProgramID] = true
		case ProgramConditional:
			protectedPrograms[program.ProgramID] = true
		case ProgramUnknown:
			unknownPrograms = true
		case ProgramIneligible:
		default:
			return LeaveEntitlementPlan{}, fmt.Errorf("leave: program %s result %q is not plannable", program.ProgramID, program.Result)
		}
	}
	paid := false
	for day := request.StartDay; day <= request.EndDay; day++ {
		segment := Segment{StartDay: day, EndDay: day, Hours: request.HoursPerDay}
		switch {
		case holidays[day]:
			segment.Kind = SegmentUnpaid
			chronicle("day %d is an explicit holiday segment", day)
		case unknownPrograms:
			segment.Kind = SegmentUnknown
			chronicle("day %d stays unknown while a program result is unknown", day)
		case len(paidPrograms) > 0:
			segment.Kind = SegmentPaid
			paid = true
			for id := range paidPrograms {
				segment.Programs = append(segment.Programs, id)
			}
		case len(protectedPrograms) > 0:
			segment.Kind = SegmentProtected
			for id := range protectedPrograms {
				segment.Programs = append(segment.Programs, id)
			}
		default:
			segment.Kind = SegmentUnpaid
		}
		sort.Strings(segment.Programs)
		plan.Segments = append(plan.Segments, segment)
		plan.ScheduledHours += request.HoursPerDay
	}
	if paid {
		plan.PlannedDebits = plan.ScheduledHours - len(request.HolidayDays)*request.HoursPerDay
	}
	if plan.PlannedDebits > plan.ScheduledHours || plan.PlannedDebits > request.BalanceAvailable {
		return LeaveEntitlementPlan{}, fmt.Errorf("leave: planned debits %d exceed schedule or balance", plan.PlannedDebits)
	}
	for _, program := range request.Programs {
		plan.Obligations = append(plan.Obligations, program.Obligations...)
	}
	sort.Strings(plan.Obligations)
	plan.Effects = []EffectPlan{
		{System: "payroll", Action: "plan-paid-time", Detail: fmt.Sprintf("debit %d hours", plan.PlannedDebits)},
		{System: "benefits", Action: "plan-continuation", Detail: "maintain coverage across protected segments"},
		{System: "wfm", Action: "plan-coverage", Detail: "backfill unpaid and unknown segments"},
	}
	plan.Digest = planDigest(request, plan)
	return plan, nil
}

// Verify checks the plan seal and its tiling invariants: segments tile
// exactly with no overlap, hours add up and debits stay within schedule.
func (plan LeaveEntitlementPlan) Verify() error {
	if plan.Digest == "" {
		return fmt.Errorf("leave: entitlement plan seal is missing")
	}
	if len(plan.Segments) == 0 {
		return fmt.Errorf("leave: plan covers no segments")
	}
	hours := 0
	for i, segment := range plan.Segments {
		if segment.EndDay < segment.StartDay {
			return fmt.Errorf("leave: segment %d is inverted", i)
		}
		if i > 0 && segment.StartDay != plan.Segments[i-1].EndDay+1 {
			return fmt.Errorf("leave: segments %d and %d overlap or gap", i-1, i)
		}
		switch segment.Kind {
		case SegmentProtected, SegmentPaid, SegmentUnpaid, SegmentUnknown:
		default:
			return fmt.Errorf("leave: segment %d kind %q is outside the vocabulary", i, segment.Kind)
		}
		hours += segment.Hours
	}
	if hours != plan.ScheduledHours {
		return fmt.Errorf("leave: segment hours %d do not total %d", hours, plan.ScheduledHours)
	}
	if plan.PlannedDebits > plan.ScheduledHours {
		return fmt.Errorf("leave: debits exceed the schedule")
	}
	return nil
}

// PlanRegistry guards plan inputs for concurrent composition.
type PlanRegistry struct {
	mu      sync.Mutex
	records map[string]LeaveEntitlementPlan
}

// NewPlanRegistry starts an empty registry.
func NewPlanRegistry() *PlanRegistry {
	return &PlanRegistry{records: make(map[string]LeaveEntitlementPlan)}
}

// Record stores one plan under its digest. Identical digests are
// idempotent; conflicting plans for one key refuse.
func (registry *PlanRegistry) Record(key string, plan LeaveEntitlementPlan) error {
	if registry == nil {
		return fmt.Errorf("leave: nil plan registry")
	}
	if plan.Digest == "" {
		return fmt.Errorf("leave: unsealed plan cannot be recorded")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if prior, ok := registry.records[key]; ok && prior.Digest != plan.Digest {
		return fmt.Errorf("leave: conflicting plan for %s", key)
	}
	registry.records[key] = plan
	return nil
}
