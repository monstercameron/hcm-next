// Package accessreview implements SECARCH-003: periodic review of effective
// authorization grants and time-bounded JIT roles.
//
// The package is deliberately a kernel-pure coordinator. It reads grants
// through narrow ports, derives review deadlines from a versioned policy, and
// records only immutable, digest-linked evidence. It never mutates an authz
// or JIT grant: CONTINUE, REVOKE, and NARROW are attestations that downstream
// lifecycle owners can consume.
package accessreview

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/jit"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/sod"
)

var (
	ErrInvalidRequest        = errors.New("accessreview: invalid request")
	ErrReviewAlreadyRecorded = errors.New("accessreview: review already recorded")
	ErrGrantNotFound         = errors.New("accessreview: grant not found")
)

// Refusal is a typed fail-closed error. Field names identify the rejected
// contract coordinate without echoing the supplied identifier or secret.
type Refusal struct {
	Field  string
	Reason string
}

func (r Refusal) Error() string { return "accessreview: " + r.Field + ": " + r.Reason }

func refuse(field, reason string) error {
	return fmt.Errorf("%w: %w", ErrInvalidRequest, Refusal{Field: field, Reason: reason})
}

// Clock is the time source used for all schedule and review decisions.
type Clock interface{ Now() time.Time }

// ClockFunc adapts a function to Clock.
type ClockFunc func() time.Time

func (f ClockFunc) Now() time.Time { return f() }

// AuthzPort reads the effective standing grants resolved by authz at at.
// Implementations must return snapshots: the manager never retains a caller
// owned mutable slice.
type AuthzPort interface {
	EffectiveGrants(at time.Time) ([]Grant, error)
}

// JITPort reads JIT role assignments. Expired assignments are intentionally
// still returned so a missed review remains visible in the overdue report.
type JITPort interface {
	JITRoles(at time.Time) ([]Grant, error)
}

// EvidencePort is the durable append boundary. Append must return the
// immutable revision that was accepted, including its assigned revision and
// digest.
type EvidencePort interface {
	Append(Evidence) (Evidence, error)
}

// GrantClass identifies the policy interval applied to a grant.
type GrantClass string

const (
	GrantClassAuthz GrantClass = "AUTHZ_EFFECTIVE"
	GrantClassJIT   GrantClass = "JIT_ROLE"
)

// Grant is a normalized immutable snapshot of an effective entitlement.
// Identifiers are useful to the coordinator, but evidence and Explain never
// render them; they are represented there by digests.
type Grant struct {
	ID          string
	Holder      string
	Class       GrantClass
	Source      string
	GrantedAt   time.Time
	ExpiresAt   time.Time
	ScopeDigest string
}

func (g Grant) validate() error {
	for _, field := range []struct {
		name  string
		value string
	}{
		{"grant_id", g.ID}, {"grant_holder", g.Holder}, {"grant_class", string(g.Class)},
	} {
		if strings.TrimSpace(field.value) == "" || strings.TrimSpace(field.value) != field.value {
			return refuse(field.name, "required and may not be padded")
		}
	}
	if g.GrantedAt.IsZero() {
		return refuse("granted_at", "required")
	}
	if !g.ExpiresAt.IsZero() && !g.GrantedAt.Before(g.ExpiresAt) {
		return refuse("expires_at", "must be after granted_at")
	}
	if g.Class == GrantClassJIT && g.ExpiresAt.IsZero() {
		return refuse("expires_at", "required for JIT roles")
	}
	return nil
}

// Digest is a stable SHA-256 identity for the exact grant snapshot.
func (g Grant) Digest() string {
	return digest("accessreview:grant:v1", g.ID, g.Holder, string(g.Class), g.Source,
		canonicalTime(g.GrantedAt), canonicalTime(g.ExpiresAt), g.ScopeDigest)
}

// Policy is the versioned review interval table. Every grant class returned
// by either source must have a positive interval; an absent class is a
// configuration refusal, never an implicit default.
type Policy struct {
	Version   string
	Intervals map[GrantClass]time.Duration
}

func (p Policy) validate() error {
	if strings.TrimSpace(p.Version) == "" || strings.TrimSpace(p.Version) != p.Version {
		return refuse("policy_version", "required and may not be padded")
	}
	if len(p.Intervals) == 0 {
		return refuse("policy_intervals", "at least one grant class is required")
	}
	for class, interval := range p.Intervals {
		if strings.TrimSpace(string(class)) == "" {
			return refuse("policy_grant_class", "required")
		}
		if interval <= 0 {
			return refuse("policy_interval", "must be positive")
		}
	}
	return nil
}

// DefaultPolicy is the review baseline for the two shipped grant classes.
func DefaultPolicy() Policy {
	return Policy{
		Version: "accessreview.secarch003.v1",
		Intervals: map[GrantClass]time.Duration{
			GrantClassAuthz: 30 * 24 * time.Hour,
			GrantClassJIT:   24 * time.Hour,
		},
	}
}

func (p Policy) due(g Grant) (time.Time, error) {
	interval, ok := p.Intervals[g.Class]
	if !ok {
		return time.Time{}, refuse("grant_class", "no review interval in policy")
	}
	return g.GrantedAt.UTC().Add(interval), nil
}

// ScheduleEntry is one grant and its policy-derived review deadline.
type ScheduleEntry struct {
	Grant     Grant
	ReviewDue time.Time
}

func (e ScheduleEntry) validate() error {
	if err := e.Grant.validate(); err != nil {
		return err
	}
	if e.ReviewDue.IsZero() {
		return refuse("review_due", "required")
	}
	return nil
}

// Schedule is an immutable point-in-time review schedule.
type Schedule struct {
	AsOf    time.Time
	Policy  Policy
	Entries []ScheduleEntry
	Digest  string
}

func (s Schedule) copy() Schedule {
	s.Policy.Intervals = cloneIntervals(s.Policy.Intervals)
	s.Entries = slices.Clone(s.Entries)
	return s
}

// Explain returns counts and policy tokens only. It never renders grant,
// holder, tenant, account, or other caller-provided identifiers.
func (s Schedule) Explain() string {
	overdue := 0
	for _, entry := range s.Entries {
		if !s.AsOf.Before(entry.ReviewDue) {
			overdue++
		}
	}
	return fmt.Sprintf("accessreview schedule policy=%s entries=%d overdue_at_schedule=%d digest=%s",
		s.Policy.Version, len(s.Entries), overdue, shortDigest(s.Digest))
}

// OverdueReport contains only entries that were due at AsOf and had not yet
// received a decision in the manager's immutable review ledger.
type OverdueReport struct {
	AsOf    time.Time
	Policy  string
	Entries []ScheduleEntry
	Digest  string
}

func (r OverdueReport) Explain() string {
	return fmt.Sprintf("accessreview overdue policy=%s entries=%d digest=%s",
		r.Policy, len(r.Entries), shortDigest(r.Digest))
}

// ReviewAction is the closed review decision vocabulary.
type ReviewAction string

const (
	Continue ReviewAction = "CONTINUE"
	Revoke   ReviewAction = "REVOKE"
	Narrow   ReviewAction = "NARROW"
)

// ReviewRequest is the reviewer-supplied attestation. NARROW requires a
// non-empty, sorted set of replacement scope tokens; the tokens themselves
// are digested in evidence and never included in Explain.
type ReviewRequest struct {
	GrantID       string
	Reviewer      string
	Action        ReviewAction
	Justification string
	NarrowTo      []string
}

// ReviewRecord is an immutable completed review revision.
type ReviewRecord struct {
	GrantID       string
	GrantDigest   string
	GrantClass    GrantClass
	Reviewer      string
	Action        ReviewAction
	Justification string
	NarrowTo      []string
	ReviewedAt    time.Time
	ReviewDue     time.Time
	PolicyVersion string
	Digest        string
}

func (r ReviewRecord) DigestValue() string { return r.Digest }

// Explain is audit-safe: it contains no raw grant or reviewer identifiers and
// no narrow-scope values.
func (r ReviewRecord) Explain() string {
	return fmt.Sprintf("accessreview review class=%s action=%s policy=%s overdue=%t digest=%s",
		r.GrantClass, r.Action, r.PolicyVersion, !r.ReviewedAt.Before(r.ReviewDue), shortDigest(r.Digest))
}

// EvidenceKind is the immutable revision vocabulary.
type EvidenceKind string

const (
	EvidenceSchedule EvidenceKind = "SCHEDULE"
	EvidenceReview   EvidenceKind = "REVIEW"
)

// Evidence is digest-only durable evidence. It does not carry raw
// identifiers, secrets, account numbers, or review text.
type Evidence struct {
	Revision       uint64
	Kind           EvidenceKind
	At             time.Time
	PolicyVersion  string
	GrantDigest    string
	ReviewerDigest string
	GrantClass     GrantClass
	ReviewDue      time.Time
	Action         ReviewAction
	ScopeDigest    string
	PreviousDigest string
	Digest         string
}

func (e Evidence) Explain() string {
	return fmt.Sprintf("accessreview evidence revision=%d kind=%s class=%s action=%s policy=%s digest=%s",
		e.Revision, e.Kind, e.GrantClass, e.Action, e.PolicyVersion, shortDigest(e.Digest))
}

// MemoryEvidenceStore is a deterministic append-only evidence port suitable
// for tests and in-process composition. Its returned records are copies.
type MemoryEvidenceStore struct {
	mu      sync.Mutex
	records []Evidence
}

func NewMemoryEvidenceStore() *MemoryEvidenceStore { return &MemoryEvidenceStore{} }

func (s *MemoryEvidenceStore) Append(in Evidence) (Evidence, error) {
	if s == nil {
		return Evidence{}, refuse("evidence_store", "must not be nil")
	}
	if in.Kind != EvidenceSchedule && in.Kind != EvidenceReview {
		return Evidence{}, refuse("evidence_kind", "unrecognized")
	}
	if in.At.IsZero() {
		return Evidence{}, refuse("evidence_at", "required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	in = normalizeEvidence(in)
	in.Revision = uint64(len(s.records) + 1)
	if len(s.records) > 0 {
		in.PreviousDigest = s.records[len(s.records)-1].Digest
	}
	in.Digest = evidenceDigest(in)
	s.records = append(s.records, in)
	return in, nil
}

// Evidence returns a copy of the immutable evidence stream.
func (s *MemoryEvidenceStore) Evidence() []Evidence {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.records)
}

// Config wires the pure coordinator.
type Config struct {
	Authz    AuthzPort
	JIT      JITPort
	Evidence EvidencePort
	Clock    Clock
	Policy   Policy
}

// Manager coordinates schedules and review attestations.
type Manager struct {
	authz    AuthzPort
	jit      JITPort
	evidence EvidencePort
	clock    Clock
	policy   Policy

	mu      sync.Mutex
	reviews map[string]ReviewRecord
}

// New constructs a manager. All ports are required so a missing source or
// clock cannot silently produce an incomplete review schedule.
func New(cfg Config) (*Manager, error) {
	if cfg.Authz == nil {
		return nil, refuse("authz_port", "required")
	}
	if cfg.JIT == nil {
		return nil, refuse("jit_port", "required")
	}
	if cfg.Evidence == nil {
		return nil, refuse("evidence_port", "required")
	}
	if cfg.Clock == nil {
		return nil, refuse("clock", "required")
	}
	if err := cfg.Policy.validate(); err != nil {
		return nil, err
	}
	return &Manager{authz: cfg.Authz, jit: cfg.JIT, evidence: cfg.Evidence,
		clock: cfg.Clock, policy: Policy{Version: cfg.Policy.Version, Intervals: cloneIntervals(cfg.Policy.Intervals)},
		reviews: make(map[string]ReviewRecord)}, nil
}

// Schedule reads both sources at the clock instant and creates one immutable
// due entry for every returned effective grant or JIT role.
func (m *Manager) Schedule() (Schedule, error) {
	if m == nil {
		return Schedule{}, refuse("manager", "must not be nil")
	}
	asOf := m.clock.Now().UTC()
	if asOf.IsZero() {
		return Schedule{}, refuse("clock_now", "must be non-zero")
	}
	authzGrants, err := m.authz.EffectiveGrants(asOf)
	if err != nil {
		return Schedule{}, fmt.Errorf("accessreview: authz port: %w", err)
	}
	jitGrants, err := m.jit.JITRoles(asOf)
	if err != nil {
		return Schedule{}, fmt.Errorf("accessreview: jit port: %w", err)
	}
	all := append(slices.Clone(authzGrants), jitGrants...)
	entries := make([]ScheduleEntry, 0, len(all))
	seen := make(map[string]struct{}, len(all))
	for _, grant := range all {
		if err := grant.validate(); err != nil {
			return Schedule{}, err
		}
		if _, ok := seen[grant.ID]; ok {
			return Schedule{}, refuse("grant_id", "duplicate grant revision")
		}
		seen[grant.ID] = struct{}{}
		due, err := m.policy.due(grant)
		if err != nil {
			return Schedule{}, err
		}
		entries = append(entries, ScheduleEntry{Grant: grant, ReviewDue: due.UTC()})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Grant.ID < entries[j].Grant.ID })
	schedule := Schedule{AsOf: asOf, Policy: Policy{Version: m.policy.Version, Intervals: cloneIntervals(m.policy.Intervals)}, Entries: entries}
	schedule.Digest = scheduleDigest(schedule)
	if _, err := m.evidence.Append(Evidence{Kind: EvidenceSchedule, At: asOf,
		PolicyVersion: m.policy.Version, GrantDigest: schedule.Digest}); err != nil {
		return Schedule{}, fmt.Errorf("accessreview: append schedule evidence: %w", err)
	}
	return schedule.copy(), nil
}

// Overdue returns grants due at the clock instant with no completed review.
func (m *Manager) Overdue() (OverdueReport, error) {
	schedule, err := m.Schedule()
	if err != nil {
		return OverdueReport{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entries := make([]ScheduleEntry, 0)
	for _, entry := range schedule.Entries {
		if schedule.AsOf.Before(entry.ReviewDue) {
			continue
		}
		if _, reviewed := m.reviews[entry.Grant.ID]; !reviewed {
			entries = append(entries, entry)
		}
	}
	report := OverdueReport{AsOf: schedule.AsOf, Policy: schedule.Policy.Version, Entries: entries}
	report.Digest = overdueDigest(report)
	return report, nil
}

// CompleteReview records one immutable review after locating the current
// grant in a fresh schedule and applying the SoD reviewer-distinct rule.
func (m *Manager) CompleteReview(req ReviewRequest) (ReviewRecord, error) {
	if err := validateReviewRequest(req); err != nil {
		return ReviewRecord{}, err
	}
	schedule, err := m.Schedule()
	if err != nil {
		return ReviewRecord{}, err
	}
	var entry ScheduleEntry
	found := false
	for _, candidate := range schedule.Entries {
		if candidate.Grant.ID == req.GrantID {
			entry, found = candidate, true
			break
		}
	}
	if !found {
		return ReviewRecord{}, fmt.Errorf("%w: grant_id", ErrGrantNotFound)
	}
	m.mu.Lock()
	if _, exists := m.reviews[req.GrantID]; exists {
		m.mu.Unlock()
		return ReviewRecord{}, ErrReviewAlreadyRecorded
	}
	m.mu.Unlock()

	sodResult, err := sod.Evaluate(sod.DecisionContext{
		Requester: sod.Actor{Subject: entry.Grant.Holder},
		Approvers: []sod.Actor{{Subject: req.Reviewer}},
	}, sod.Constraints{RequesterMayNotApprove: true, RuleID: "accessreview.reviewer_distinct"}, 1)
	if err != nil || len(sodResult.Eligible) != 1 {
		return ReviewRecord{}, refuse("reviewer", "must be distinct from grant holder under separation of duties")
	}
	narrow := slices.Clone(req.NarrowTo)
	slices.Sort(narrow)
	reviewedAt := m.clock.Now().UTC()
	record := ReviewRecord{
		GrantID: req.GrantID, GrantDigest: entry.Grant.Digest(), GrantClass: entry.Grant.Class,
		Reviewer: req.Reviewer, Action: req.Action, Justification: req.Justification,
		NarrowTo: narrow, ReviewedAt: reviewedAt, ReviewDue: entry.ReviewDue,
		PolicyVersion: schedule.Policy.Version,
	}
	record.Digest = reviewDigest(record)
	if _, err := m.evidence.Append(Evidence{Kind: EvidenceReview, At: reviewedAt,
		PolicyVersion: record.PolicyVersion, GrantDigest: record.GrantDigest,
		ReviewerDigest: digest("accessreview:reviewer:v1", req.Reviewer), GrantClass: record.GrantClass,
		ReviewDue: record.ReviewDue, Action: record.Action, ScopeDigest: digestStrings(narrow)}); err != nil {
		return ReviewRecord{}, fmt.Errorf("accessreview: append review evidence: %w", err)
	}
	m.mu.Lock()
	m.reviews[req.GrantID] = record
	m.mu.Unlock()
	return record.copy(), nil
}

func validateReviewRequest(req ReviewRequest) error {
	if strings.TrimSpace(req.GrantID) == "" || strings.TrimSpace(req.GrantID) != req.GrantID {
		return refuse("grant_id", "required and may not be padded")
	}
	if strings.TrimSpace(req.Reviewer) == "" || strings.TrimSpace(req.Reviewer) != req.Reviewer {
		return refuse("reviewer", "required and may not be padded")
	}
	if strings.TrimSpace(req.Justification) == "" || strings.TrimSpace(req.Justification) != req.Justification {
		return refuse("justification", "required and may not be padded")
	}
	switch req.Action {
	case Continue, Revoke:
		if len(req.NarrowTo) != 0 {
			return refuse("narrow_to", "must be empty unless action is NARROW")
		}
	case Narrow:
		if len(req.NarrowTo) == 0 {
			return refuse("narrow_to", "required for NARROW")
		}
		seen := make(map[string]struct{}, len(req.NarrowTo))
		for _, item := range req.NarrowTo {
			if strings.TrimSpace(item) == "" || strings.TrimSpace(item) != item || item == "*" {
				return refuse("narrow_to", "must contain named non-wildcard scope tokens")
			}
			if _, ok := seen[item]; ok {
				return refuse("narrow_to", "must not contain duplicates")
			}
			seen[item] = struct{}{}
		}
	default:
		return refuse("action", "must be CONTINUE, REVOKE, or NARROW")
	}
	return nil
}

func (r ReviewRecord) copy() ReviewRecord {
	r.NarrowTo = slices.Clone(r.NarrowTo)
	return r
}

func cloneIntervals(in map[GrantClass]time.Duration) map[GrantClass]time.Duration {
	out := make(map[GrantClass]time.Duration, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func canonicalTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func digest(profile string, values ...string) string {
	h := sha256.New()
	frame := func(s string) {
		fmt.Fprintf(h, "%d:", len(s))
		h.Write([]byte(s))
	}
	frame(profile)
	for _, value := range values {
		frame(value)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func digestStrings(values []string) string { return digest("accessreview:strings:v1", values...) }

func scheduleDigest(s Schedule) string {
	parts := []string{s.Policy.Version, canonicalTime(s.AsOf)}
	classes := make([]string, 0, len(s.Policy.Intervals))
	for class := range s.Policy.Intervals {
		classes = append(classes, string(class))
	}
	slices.Sort(classes)
	for _, class := range classes {
		parts = append(parts, class, s.Policy.Intervals[GrantClass(class)].String())
	}
	for _, entry := range s.Entries {
		parts = append(parts, entry.Grant.Digest(), canonicalTime(entry.ReviewDue))
	}
	return digest("accessreview:schedule:v1", parts...)
}

func overdueDigest(r OverdueReport) string {
	parts := []string{r.Policy, canonicalTime(r.AsOf)}
	for _, entry := range r.Entries {
		parts = append(parts, entry.Grant.Digest(), canonicalTime(entry.ReviewDue))
	}
	return digest("accessreview:overdue:v1", parts...)
}

func reviewDigest(r ReviewRecord) string {
	return digest("accessreview:review:v1", r.GrantDigest, string(r.GrantClass),
		digest("accessreview:reviewer:v1", r.Reviewer), string(r.Action), r.Justification,
		digestStrings(r.NarrowTo), canonicalTime(r.ReviewedAt), canonicalTime(r.ReviewDue), r.PolicyVersion)
}

func normalizeEvidence(e Evidence) Evidence {
	e.At = e.At.UTC()
	e.ReviewDue = e.ReviewDue.UTC()
	return e
}

func evidenceDigest(e Evidence) string {
	return digest("accessreview:evidence:v1", fmt.Sprintf("%d", e.Revision), string(e.Kind),
		canonicalTime(e.At), e.PolicyVersion, e.GrantDigest, e.ReviewerDigest, string(e.GrantClass),
		canonicalTime(e.ReviewDue), string(e.Action), e.ScopeDigest, e.PreviousDigest)
}

func shortDigest(value string) string {
	if len(value) > 16 {
		return value[:16]
	}
	return value
}

// AuthzReader adapts the real authz field resolver into the access-review
// port. Each allowed/redacted field-purpose ruling is an effective grant
// snapshot, so no policy grant is silently omitted.
type AuthzReader struct {
	principal *trust.Principal
	fields    []authz.FieldID
	purposes  []string
}

// NewAuthzReader builds an authz reader over a verified principal. Empty
// fields use the complete exported field registry; empty purposes use the
// principal's declared purposes.
func NewAuthzReader(principal *trust.Principal, fields []authz.FieldID, purposes []string) (*AuthzReader, error) {
	if principal == nil {
		return nil, refuse("principal", "required")
	}
	if len(fields) == 0 {
		for field := range authz.FieldRegistry {
			fields = append(fields, field)
		}
		slices.Sort(fields)
	}
	if len(purposes) == 0 {
		purposes = principal.Purposes()
	}
	if len(purposes) == 0 {
		return nil, refuse("purposes", "at least one purpose is required")
	}
	return &AuthzReader{principal: principal, fields: slices.Clone(fields), purposes: slices.Clone(purposes)}, nil
}

func (r *AuthzReader) EffectiveGrants(at time.Time) ([]Grant, error) {
	if r == nil || r.principal == nil {
		return nil, refuse("authz_reader", "must not be nil")
	}
	issued := r.principal.IssuedAt().UTC()
	if issued.IsZero() {
		issued = at.UTC()
	}
	var out []Grant
	for _, purpose := range r.purposes {
		decision, err := authz.ResolveFields(r.principal, purpose, r.fields, nil)
		if err != nil {
			return nil, fmt.Errorf("accessreview: resolve authz fields: %w", err)
		}
		for _, field := range sortedFields(decision.Rulings) {
			ruling := decision.Rulings[field]
			if ruling.Effect != authz.EffectAllow && ruling.Effect != authz.EffectRedacted {
				continue
			}
			id := digest("accessreview:authz-grant:v1", r.principal.Fingerprint(), purpose, string(field), ruling.RuleID)
			out = append(out, Grant{ID: id, Holder: r.principal.Subject(), Class: GrantClassAuthz,
				Source: ruling.RuleID, GrantedAt: issued, ScopeDigest: digest("accessreview:authz-scope:v1", purpose, string(field), ruling.RuleID)})
		}
	}
	return out, nil
}

func sortedFields(in map[authz.FieldID]authz.FieldRuling) []authz.FieldID {
	out := make([]authz.FieldID, 0, len(in))
	for field := range in {
		out = append(out, field)
	}
	slices.Sort(out)
	return out
}

// JITReader adapts already-constructed jit grants. The jit.New constructor is
// intentionally used by callers before handing grants to this read port.
type JITReader struct{ grants []*jit.Grant }

func NewJITReader(grants ...*jit.Grant) (*JITReader, error) {
	for i, grant := range grants {
		if grant == nil {
			return nil, refuse(fmt.Sprintf("grant[%d]", i), "must not be nil")
		}
	}
	return &JITReader{grants: slices.Clone(grants)}, nil
}

func (r *JITReader) JITRoles(_ time.Time) ([]Grant, error) {
	if r == nil {
		return nil, refuse("jit_reader", "must not be nil")
	}
	out := make([]Grant, 0, len(r.grants))
	for _, grant := range r.grants {
		out = append(out, Grant{ID: grant.ID, Holder: grant.Principal, Class: GrantClassJIT,
			Source: string(grant.Role), GrantedAt: grant.IssuedAt.UTC(), ExpiresAt: grant.ExpiresAt.UTC(),
			ScopeDigest: digest("accessreview:jit-scope:v1", string(grant.Role), grant.TicketRef, grant.Purpose)})
	}
	return out, nil
}
