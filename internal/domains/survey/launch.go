package survey

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/audience"
	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/engines/messagetemplate"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

var (
	ErrInvalidLaunch             = errors.New("survey: invalid campaign launch")
	ErrSampleUnfrozen            = errors.New("survey: campaign sample is not frozen")
	ErrSampleSuperseded          = errors.New("survey: campaign sample is superseded")
	ErrSampleCampaignMismatch    = errors.New("survey: sample does not belong to campaign")
	ErrTemplatePurposeMismatch   = errors.New("survey: message template purpose does not match campaign")
	ErrTemplateDigestUnavailable = errors.New("survey: message template digest is unavailable")
	ErrAudienceMismatch          = errors.New("survey: audience resolution does not match delivery plan")
	ErrDeliveryPlanOutsideSample = errors.New("survey: delivery plan names a member outside the frozen sample")
	ErrDistinctApproverRequired  = errors.New("survey: launch requires a distinct approver")
	ErrInvalidReminderPolicy     = errors.New("survey: invalid reminder policy")
	ErrReminderLimitExceeded     = errors.New("survey: reminder count exceeds declared maximum")
)

// SampleState is the launcher's view of the lifecycle of a CampaignSample.
// CampaignSample predates launch and carries its frozen proof in Digest; the
// explicit state lets a caller identify a superseded revision without
// mutating that immutable value.
type SampleState string

const (
	SampleStateFrozen     SampleState = "FROZEN"
	SampleStateUnfrozen   SampleState = "UNFROZEN"
	SampleStateSuperseded SampleState = "SUPERSEDED"
)

// ReminderPolicy bounds governed follow-up communications. MaxCount is the
// maximum number of reminders for one launch, and Interval is the fixed
// spacing from the launch and between subsequent reminders.
type ReminderPolicy struct {
	MaxCount int
	Interval time.Duration
}

func (p ReminderPolicy) Validate() error {
	if p.MaxCount < 1 || p.Interval <= 0 {
		return fmt.Errorf("%w: maximum count and positive interval are required", ErrInvalidReminderPolicy)
	}
	return nil
}

// LaunchRequest contains all already-resolved, read-only inputs needed to
// launch a campaign. No provider, database or clock is consulted.
type LaunchRequest struct {
	Campaign       CampaignRevision
	Purpose        messagetemplate.Purpose
	Sample         CampaignSample
	SampleState    SampleState
	Template       messagetemplate.Template
	Audience       audience.Resolution
	DeliveryPlan   audience.DeliveryPlan
	LaunchedBy     string
	Approver       string
	LaunchedAt     values.Instant
	ReminderPolicy ReminderPolicy
}

// LaunchRecord is the append-only evidence of a governed campaign launch.
// It intentionally contains digests and counts, never the audience's member
// references. That makes the record safe for protected-membership samples.
type LaunchRecord struct {
	CampaignID          string
	CampaignRevision    uint64
	CampaignDigest      string
	Purpose             messagetemplate.Purpose
	SampleDigest        string
	MembershipProtected bool
	TemplateKey         string
	TemplateVersion     int
	TemplateDigest      string
	AudienceDigest      string
	DeliveryPlanDigest  string
	LaunchedBy          string
	Approver            string
	LaunchedAt          values.Instant
	ReminderPolicy      ReminderPolicy
	Digest              string
}

// LaunchExplanation is the bounded, disclosure-safe explanation of a launch.
// In particular, it has no member IDs or EntityRefs.
type LaunchExplanation struct {
	CampaignID          string
	CampaignRevision    uint64
	Purpose             messagetemplate.Purpose
	SampleDigest        string
	MembershipProtected bool
	TemplateKey         string
	TemplateVersion     int
	TemplateDigest      string
	AudienceDigest      string
	DeliveryPlanDigest  string
	LaunchedBy          string
	Approver            string
	LaunchedAt          values.Instant
	ReminderMaxCount    int
	ReminderInterval    time.Duration
	Digest              string
}

// Explain returns a digest-oriented launch explanation with no member refs.
func (r LaunchRecord) Explain() LaunchExplanation {
	return LaunchExplanation{
		CampaignID:          r.CampaignID,
		CampaignRevision:    r.CampaignRevision,
		Purpose:             r.Purpose,
		SampleDigest:        r.SampleDigest,
		MembershipProtected: r.MembershipProtected,
		TemplateKey:         r.TemplateKey,
		TemplateVersion:     r.TemplateVersion,
		TemplateDigest:      r.TemplateDigest,
		AudienceDigest:      r.AudienceDigest,
		DeliveryPlanDigest:  r.DeliveryPlanDigest,
		LaunchedBy:          r.LaunchedBy,
		Approver:            r.Approver,
		LaunchedAt:          r.LaunchedAt,
		ReminderMaxCount:    r.ReminderPolicy.MaxCount,
		ReminderInterval:    r.ReminderPolicy.Interval,
		Digest:              r.Digest,
	}
}

// Explain is the package-level launch explanation symbol.
func Explain(r LaunchRecord) LaunchExplanation { return r.Explain() }

// TemplateDigest returns the deterministic digest of a validated message
// template definition, including its governed key and business version.
func TemplateDigest(t messagetemplate.Template) string {
	if err := t.Validate(); err != nil {
		return ""
	}
	key := t.Key
	if key == "" {
		key = t.TemplateKey
	}
	version := t.Version
	if version == 0 {
		version = t.TemplateVersion
	}
	placeholders := append([]string(nil), t.Placeholders...)
	if len(placeholders) == 0 {
		placeholders = append(placeholders, t.DeclaredPlaceholders...)
	}
	sort.Strings(placeholders)
	w := canonicalbytes.New("hcmnext.domains.survey.MessageTemplateBinding", 1).
		String("key", key).
		Int("version", int64(version)).
		String("purpose", string(t.Purpose)).
		String("channel", string(t.Channel)).
		String("locale", t.Locale).
		String("classification", t.Classification).
		String("legal_basis", t.LegalBasis).
		String("subject", t.Subject).
		String("body", t.Body).
		SortedStrings("placeholder", placeholders)
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

func normalizedTemplateIdentity(t messagetemplate.Template) (string, int) {
	key := t.Key
	if key == "" {
		key = t.TemplateKey
	}
	version := t.Version
	if version == 0 {
		version = t.TemplateVersion
	}
	return key, version
}

func validateSampleForLaunch(request LaunchRequest) error {
	state := request.SampleState
	if state == "" {
		if request.Sample.Digest == "" {
			state = SampleStateUnfrozen
		} else {
			state = SampleStateFrozen
		}
	}
	switch state {
	case SampleStateUnfrozen:
		return ErrSampleUnfrozen
	case SampleStateSuperseded:
		return ErrSampleSuperseded
	case SampleStateFrozen:
	default:
		return fmt.Errorf("%w: unknown sample state %q", ErrInvalidLaunch, state)
	}
	if request.Sample.CampaignID != request.Campaign.CampaignID {
		return ErrSampleCampaignMismatch
	}
	if request.Sample.Digest == "" || request.Sample.FrozenAt.Validate() != nil {
		return ErrSampleUnfrozen
	}
	if request.Sample.MembershipProtected && len(request.Sample.MemberIDs) != 0 {
		return fmt.Errorf("%w: protected sample contains member refs", ErrInvalidLaunch)
	}
	if request.Sample.Count < 0 {
		return fmt.Errorf("%w: sample count is negative", ErrInvalidLaunch)
	}
	expected, err := FreezeSample(request.Sample.CampaignID, request.Sample.Binding, request.Sample.Rule, request.Sample.MemberIDs, request.Sample.MembershipProtected, request.Sample.Count, request.Sample.FrozenAt)
	if err != nil || expected.Digest != request.Sample.Digest {
		return ErrSampleUnfrozen
	}
	return nil
}

func refMatchesSampleID(id string, ref values.EntityRef) bool {
	return id == ref.String() || id == ref.Id
}

func validateAudienceAndPlan(request LaunchRequest) error {
	if request.Audience.ResultDigest == "" || request.DeliveryPlan.ResultDigest == "" {
		return fmt.Errorf("%w: audience and delivery plan digests are required", ErrAudienceMismatch)
	}
	if request.DeliveryPlan.AudienceDigest != request.Audience.ResultDigest {
		return ErrAudienceMismatch
	}
	if request.DeliveryPlan.Purpose != string(request.Purpose) {
		return fmt.Errorf("%w: delivery purpose %q", ErrAudienceMismatch, request.DeliveryPlan.Purpose)
	}
	audienceMembers := make(map[string]struct{}, len(request.Audience.Principals))
	for _, principal := range request.Audience.Principals {
		if err := principal.Validate(); err != nil {
			return fmt.Errorf("%w: invalid audience principal: %v", ErrAudienceMismatch, err)
		}
		key := principal.String()
		if _, exists := audienceMembers[key]; exists {
			return fmt.Errorf("%w: duplicate audience principal", ErrAudienceMismatch)
		}
		audienceMembers[key] = struct{}{}
	}
	seenPlanMembers := make(map[string]struct{}, len(request.DeliveryPlan.Plans))
	for _, plan := range request.DeliveryPlan.Plans {
		if err := plan.Principal.Validate(); err != nil {
			return fmt.Errorf("%w: invalid delivery principal: %v", ErrAudienceMismatch, err)
		}
		key := plan.Principal.String()
		if _, exists := seenPlanMembers[key]; exists {
			return fmt.Errorf("%w: duplicate delivery principal", ErrAudienceMismatch)
		}
		seenPlanMembers[key] = struct{}{}
		if _, exists := audienceMembers[key]; !exists {
			return ErrAudienceMismatch
		}
	}
	if len(seenPlanMembers) != len(audienceMembers) {
		return ErrAudienceMismatch
	}
	if request.Sample.MembershipProtected {
		if request.Sample.Count != len(request.DeliveryPlan.Plans) {
			return fmt.Errorf("%w: protected sample count does not match delivery plan", ErrDeliveryPlanOutsideSample)
		}
		return nil
	}
	for _, plan := range request.DeliveryPlan.Plans {
		inside := false
		for _, id := range request.Sample.MemberIDs {
			if refMatchesSampleID(id, plan.Principal) {
				inside = true
				break
			}
		}
		if !inside {
			return ErrDeliveryPlanOutsideSample
		}
	}
	return nil
}

func launchRecordDigest(r LaunchRecord) string {
	w := canonicalbytes.New("hcmnext.domains.survey.LaunchRecord", 1).
		String("campaign_id", r.CampaignID).
		Int("campaign_revision", int64(r.CampaignRevision)).
		String("campaign_digest", r.CampaignDigest).
		String("purpose", string(r.Purpose)).
		String("sample_digest", r.SampleDigest).
		Bool("membership_protected", r.MembershipProtected).
		String("template_key", r.TemplateKey).
		Int("template_version", int64(r.TemplateVersion)).
		String("template_digest", r.TemplateDigest).
		String("audience_digest", r.AudienceDigest).
		String("delivery_plan_digest", r.DeliveryPlanDigest).
		String("launched_by", r.LaunchedBy).
		String("approver", r.Approver).
		Value("launched_at", r.LaunchedAt).
		Int("reminder_max_count", int64(r.ReminderPolicy.MaxCount)).
		Int("reminder_interval_ns", int64(r.ReminderPolicy.Interval))
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

func buildLaunch(request LaunchRequest) (LaunchRecord, error) {
	if err := request.Campaign.Validate(); err != nil {
		return LaunchRecord{}, fmt.Errorf("%w: campaign: %v", ErrInvalidLaunch, err)
	}
	if request.Purpose == "" {
		return LaunchRecord{}, fmt.Errorf("%w: campaign purpose is required", ErrInvalidLaunch)
	}
	if err := validateSampleForLaunch(request); err != nil {
		return LaunchRecord{}, err
	}
	if err := request.Template.Validate(); err != nil {
		return LaunchRecord{}, fmt.Errorf("%w: template: %v", ErrInvalidLaunch, err)
	}
	if request.Template.Purpose != request.Purpose {
		return LaunchRecord{}, ErrTemplatePurposeMismatch
	}
	if err := request.LaunchedAt.Validate(); err != nil {
		return LaunchRecord{}, fmt.Errorf("%w: launched-at: %v", ErrInvalidLaunch, err)
	}
	if strings.TrimSpace(request.LaunchedBy) == "" || strings.TrimSpace(request.Approver) == "" || request.LaunchedBy == request.Approver {
		return LaunchRecord{}, ErrDistinctApproverRequired
	}
	if err := validateAudienceAndPlan(request); err != nil {
		return LaunchRecord{}, err
	}
	templateKey, templateVersion := normalizedTemplateIdentity(request.Template)
	templateDigest := TemplateDigest(request.Template)
	if templateKey == "" || templateVersion < 1 || templateDigest == "" {
		return LaunchRecord{}, ErrTemplateDigestUnavailable
	}
	policy := request.ReminderPolicy
	if policy != (ReminderPolicy{}) {
		if err := policy.Validate(); err != nil {
			return LaunchRecord{}, err
		}
	}
	record := LaunchRecord{
		CampaignID:          request.Campaign.CampaignID,
		CampaignRevision:    request.Campaign.Revision,
		CampaignDigest:      request.Campaign.Digest(),
		Purpose:             request.Purpose,
		SampleDigest:        request.Sample.Digest,
		MembershipProtected: request.Sample.MembershipProtected,
		TemplateKey:         templateKey,
		TemplateVersion:     templateVersion,
		TemplateDigest:      templateDigest,
		AudienceDigest:      request.Audience.ResultDigest,
		DeliveryPlanDigest:  request.DeliveryPlan.ResultDigest,
		LaunchedBy:          request.LaunchedBy,
		Approver:            request.Approver,
		LaunchedAt:          request.LaunchedAt,
		ReminderPolicy:      policy,
	}
	record.Digest = launchRecordDigest(record)
	if record.Digest == "" {
		return LaunchRecord{}, ErrInvalidLaunch
	}
	return record, nil
}

// LaunchCampaign validates and digests one governed campaign launch. It does
// not persist or send anything.
func LaunchCampaign(request LaunchRequest) (LaunchRecord, error) {
	return buildLaunch(request)
}

// Launch is the concise alias for LaunchCampaign.
func Launch(request LaunchRequest) (LaunchRecord, error) {
	return LaunchCampaign(request)
}

// LaunchRegistry is a concurrency-safe in-memory append-only launch record
// registry. It is a pure domain test seam, not a replacement for durable
// messaging storage.
type LaunchRegistry struct {
	mu      sync.Mutex
	records map[string]LaunchRecord
	order   []string
}

func NewLaunchRegistry() *LaunchRegistry {
	return &LaunchRegistry{records: make(map[string]LaunchRecord)}
}

// Launch validates the request and returns the existing record for a repeated
// input digest. A repeated launch therefore creates no second record.
func (r *LaunchRegistry) Launch(request LaunchRequest) (LaunchRecord, error) {
	if r == nil {
		return LaunchRecord{}, ErrInvalidLaunch
	}
	record, err := LaunchCampaign(request)
	if err != nil {
		return LaunchRecord{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.records[record.Digest]; ok {
		return existing, nil
	}
	r.records[record.Digest] = record
	r.order = append(r.order, record.Digest)
	return record, nil
}

// Records returns a defensive, insertion-ordered copy of the launch records.
func (r *LaunchRegistry) Records() []LaunchRecord {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]LaunchRecord, 0, len(r.order))
	for _, digest := range r.order {
		result = append(result, r.records[digest])
	}
	return result
}

// ReminderRecord is one scheduled governed follow-up communication. It is
// still only a domain record; dispatch is outside this package.
type ReminderRecord struct {
	LaunchDigest string
	Sequence     int
	ScheduledAt  values.Instant
	Digest       string
}

// ScheduleReminders creates at most policy.MaxCount reminders at the declared
// fixed interval. The first reminder is one interval after launch.
func ScheduleReminders(launch LaunchRecord, policy ReminderPolicy, count int) ([]ReminderRecord, error) {
	if launch.Digest == "" {
		return nil, fmt.Errorf("%w: launch digest is required", ErrInvalidReminderPolicy)
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if count < 0 || count > policy.MaxCount {
		return nil, ErrReminderLimitExceeded
	}
	reminders := make([]ReminderRecord, 0, count)
	for sequence := 1; sequence <= count; sequence++ {
		at := values.NewInstant(launch.LaunchedAt.Time().Add(time.Duration(sequence) * policy.Interval))
		reminder := ReminderRecord{LaunchDigest: launch.Digest, Sequence: sequence, ScheduledAt: at}
		w := canonicalbytes.New("hcmnext.domains.survey.ReminderRecord", 1).
			String("launch_digest", reminder.LaunchDigest).
			Int("sequence", int64(reminder.Sequence)).
			Value("scheduled_at", reminder.ScheduledAt)
		reminder.Digest, _ = w.Digest()
		reminders = append(reminders, reminder)
	}
	return reminders, nil
}
