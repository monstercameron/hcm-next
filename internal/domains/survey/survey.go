package survey

import (
	"errors"
	"fmt"
	"slices"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrInvalidSurveyRevision          = errors.New("survey: invalid survey revision")
	ErrInvalidQuestionBankRevision    = errors.New("survey: invalid question bank revision")
	ErrInvalidCampaignRevision        = errors.New("survey: invalid campaign revision")
	ErrInvalidQuestion                = errors.New("survey: invalid question")
	ErrInvalidQuestionKind            = errors.New("survey: invalid question kind")
	ErrQuestionBankEmpty              = errors.New("survey: question bank has no questions")
	ErrAnonymityThresholdTooLow       = errors.New("survey: anonymity threshold below minimum of 5")
	ErrPopulationSnapshotRequired     = errors.New("survey: campaign requires population snapshot reference")
	ErrCampaignWindowRequired         = errors.New("survey: campaign requires active window")
	ErrFreeTextRequiresClassification = errors.New("survey: free text question requires classification")
	ErrEmptyChannelRefs               = errors.New("survey: campaign requires at least one channel reference")
)

// QuestionKind identifies the type of question in a question bank.
type QuestionKind string

const (
	QuestionKindScale       QuestionKind = "SCALE"
	QuestionKindChoice      QuestionKind = "CHOICE"
	QuestionKindMultiChoice QuestionKind = "MULTI_CHOICE"
	QuestionKindFreeText    QuestionKind = "FREE_TEXT"
)

func (k QuestionKind) Valid() bool {
	return k == QuestionKindScale || k == QuestionKindChoice ||
		k == QuestionKindMultiChoice || k == QuestionKindFreeText
}

// Question is an immutable question within a question bank.
type Question struct {
	QuestionID     string
	Kind           QuestionKind
	Text           string
	Required       bool
	Classification string   // only required for FREE_TEXT kind
	ScaleMin       int      // for SCALE kind only
	ScaleMax       int      // for SCALE kind only
	Choices        []string // for CHOICE/MULTI_CHOICE kinds only
}

// Validate reports whether the question is well-formed.
func (q Question) Validate() error {
	if q.QuestionID == "" {
		return fmt.Errorf("%w: question id is empty", ErrInvalidQuestion)
	}
	if !q.Kind.Valid() {
		return fmt.Errorf("%w: unknown kind %q", ErrInvalidQuestionKind, q.Kind)
	}
	if q.Text == "" {
		return fmt.Errorf("%w: question text is empty", ErrInvalidQuestion)
	}

	switch q.Kind {
	case QuestionKindScale:
		if q.ScaleMin < 0 || q.ScaleMax < 0 || q.ScaleMin >= q.ScaleMax {
			return fmt.Errorf("%w: scale range invalid (min=%d, max=%d)", ErrInvalidQuestion, q.ScaleMin, q.ScaleMax)
		}
	case QuestionKindChoice, QuestionKindMultiChoice:
		if len(q.Choices) == 0 {
			return fmt.Errorf("%w: %s requires at least one choice", ErrInvalidQuestion, q.Kind)
		}
		for i, choice := range q.Choices {
			if choice == "" {
				return fmt.Errorf("%w: choice %d is empty", ErrInvalidQuestion, i)
			}
		}
	case QuestionKindFreeText:
		if q.Classification == "" {
			return ErrFreeTextRequiresClassification
		}
	}

	return nil
}

// Canonical returns the deterministic byte encoding of the question, or nil when invalid.
func (q Question) Canonical() []byte {
	if q.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.survey.Question", 1).
		String("question_id", q.QuestionID).
		String("kind", string(q.Kind)).
		String("text", q.Text).
		Bool("required", q.Required).
		String("classification", q.Classification)

	switch q.Kind {
	case QuestionKindScale:
		w.Int("scale_min", int64(q.ScaleMin)).Int("scale_max", int64(q.ScaleMax))
	case QuestionKindChoice, QuestionKindMultiChoice:
		w.Count("choices", len(q.Choices))
		for _, choice := range q.Choices {
			w.String("choice", choice)
		}
	}

	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// QuestionBankRevision is an immutable definition of a set of survey questions.
type QuestionBankRevision struct {
	QuestionBankID string
	Revision       uint64
	CreatedAt      values.Instant
	Questions      []Question
}

// Validate reports whether the revision is well-formed.
func (qb QuestionBankRevision) Validate() error {
	if qb.QuestionBankID == "" {
		return fmt.Errorf("%w: question bank id is empty", ErrInvalidQuestionBankRevision)
	}
	if qb.Revision == 0 {
		return fmt.Errorf("%w: revision must be greater than zero", ErrInvalidQuestionBankRevision)
	}
	if err := qb.CreatedAt.Validate(); err != nil {
		return fmt.Errorf("%w: created_at: %w", ErrInvalidQuestionBankRevision, err)
	}
	if len(qb.Questions) == 0 {
		return ErrQuestionBankEmpty
	}

	seenIDs := make(map[string]bool)
	for i, q := range qb.Questions {
		if err := q.Validate(); err != nil {
			return fmt.Errorf("%w: question %d: %w", ErrInvalidQuestionBankRevision, i, err)
		}
		if seenIDs[q.QuestionID] {
			return fmt.Errorf("%w: duplicate question id %q", ErrInvalidQuestionBankRevision, q.QuestionID)
		}
		seenIDs[q.QuestionID] = true
	}

	return nil
}

// Canonical returns the deterministic byte encoding, or nil when invalid.
func (qb QuestionBankRevision) Canonical() []byte {
	if qb.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.survey.QuestionBankRevision", 1).
		String("question_bank_id", qb.QuestionBankID).
		Int("revision", int64(qb.Revision)).
		Value("created_at", qb.CreatedAt).
		Count("questions", len(qb.Questions))

	for i, q := range qb.Questions {
		if err := w.Err(); err != nil {
			return nil
		}
		qcanon := q.Canonical()
		if qcanon == nil {
			return nil
		}
		w.Field(fmt.Sprintf("question_%d", i), qcanon)
	}

	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Digest returns the hex digest of the canonical encoding.
func (qb QuestionBankRevision) Digest() string {
	raw := qb.Canonical()
	if raw == nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// SurveyRevision is an immutable definition of a survey campaign.
type SurveyRevision struct {
	SurveyID        string
	Revision        uint64
	Title           string
	Description     string
	QuestionBankRef values.EntityRef
	CreatedAt       values.Instant
}

// Validate reports whether the revision is well-formed.
func (s SurveyRevision) Validate() error {
	if s.SurveyID == "" {
		return fmt.Errorf("%w: survey id is empty", ErrInvalidSurveyRevision)
	}
	if s.Revision == 0 {
		return fmt.Errorf("%w: revision must be greater than zero", ErrInvalidSurveyRevision)
	}
	if s.Title == "" {
		return fmt.Errorf("%w: title is empty", ErrInvalidSurveyRevision)
	}
	if err := s.QuestionBankRef.Validate(); err != nil {
		return fmt.Errorf("%w: question_bank_ref: %w", ErrInvalidSurveyRevision, err)
	}
	if err := s.CreatedAt.Validate(); err != nil {
		return fmt.Errorf("%w: created_at: %w", ErrInvalidSurveyRevision, err)
	}
	return nil
}

// Canonical returns the deterministic byte encoding, or nil when invalid.
func (s SurveyRevision) Canonical() []byte {
	if s.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.survey.SurveyRevision", 1).
		String("survey_id", s.SurveyID).
		Int("revision", int64(s.Revision)).
		String("title", s.Title).
		String("description", s.Description).
		String("question_bank_ref", s.QuestionBankRef.String()).
		Value("created_at", s.CreatedAt).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Digest returns the hex digest of the canonical encoding.
func (s SurveyRevision) Digest() string {
	raw := s.Canonical()
	if raw == nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// CampaignRevision is an immutable definition of a survey campaign.
type CampaignRevision struct {
	CampaignID            string
	Revision              uint64
	SurveyRef             values.EntityRef
	PopulationSnapshotRef values.EntityRef   // required; names a population_snapshot entity
	WindowStart           values.Instant     // campaign response window start
	WindowEnd             values.Instant     // campaign response window end
	AnonymityThreshold    int                // minimum 5; cells below this are suppressed
	ChannelRefs           []values.EntityRef // at least one channel reference required
	CreatedAt             values.Instant
}

// Validate reports whether the revision is well-formed.
func (c CampaignRevision) Validate() error {
	if c.CampaignID == "" {
		return fmt.Errorf("%w: campaign id is empty", ErrInvalidCampaignRevision)
	}
	if c.Revision == 0 {
		return fmt.Errorf("%w: revision must be greater than zero", ErrInvalidCampaignRevision)
	}
	if err := c.SurveyRef.Validate(); err != nil {
		return fmt.Errorf("%w: survey_ref: %w", ErrInvalidCampaignRevision, err)
	}
	if err := c.PopulationSnapshotRef.Validate(); err != nil {
		return fmt.Errorf("%w: population_snapshot_ref: %w; %w", ErrInvalidCampaignRevision, err, ErrPopulationSnapshotRequired)
	}

	if err := c.WindowStart.Validate(); err != nil {
		return fmt.Errorf("%w: window_start: %w", ErrInvalidCampaignRevision, err)
	}
	if err := c.WindowEnd.Validate(); err != nil {
		return fmt.Errorf("%w: window_end: %w", ErrInvalidCampaignRevision, err)
	}
	if !c.WindowStart.Before(c.WindowEnd) {
		return fmt.Errorf("%w: window start must be before window end", ErrInvalidCampaignRevision)
	}

	if c.AnonymityThreshold < 5 {
		return ErrAnonymityThresholdTooLow
	}

	if len(c.ChannelRefs) == 0 {
		return ErrEmptyChannelRefs
	}

	seenChannels := make(map[string]bool)
	for i, ref := range c.ChannelRefs {
		if err := ref.Validate(); err != nil {
			return fmt.Errorf("%w: channel_ref[%d]: %w", ErrInvalidCampaignRevision, i, err)
		}
		key := ref.String()
		if seenChannels[key] {
			return fmt.Errorf("%w: duplicate channel ref %q", ErrInvalidCampaignRevision, key)
		}
		seenChannels[key] = true
	}

	if err := c.CreatedAt.Validate(); err != nil {
		return fmt.Errorf("%w: created_at: %w", ErrInvalidCampaignRevision, err)
	}

	return nil
}

// Canonical returns the deterministic byte encoding, or nil when invalid.
func (c CampaignRevision) Canonical() []byte {
	if c.Validate() != nil {
		return nil
	}

	w := canonicalbytes.New("hcmnext.domains.survey.CampaignRevision", 1).
		String("campaign_id", c.CampaignID).
		Int("revision", int64(c.Revision)).
		String("survey_ref", c.SurveyRef.String()).
		String("population_snapshot_ref", c.PopulationSnapshotRef.String()).
		Value("window_start", c.WindowStart).
		Value("window_end", c.WindowEnd).
		Int("anonymity_threshold", int64(c.AnonymityThreshold)).
		Count("channel_refs", len(c.ChannelRefs))

	// Sort channel refs for deterministic encoding
	sorted := make([]values.EntityRef, len(c.ChannelRefs))
	copy(sorted, c.ChannelRefs)
	slices.SortFunc(sorted, func(a, b values.EntityRef) int {
		as := a.String()
		bs := b.String()
		if as < bs {
			return -1
		}
		if as > bs {
			return 1
		}
		return 0
	})

	for _, ref := range sorted {
		w.String("channel_ref", ref.String())
	}

	w.Value("created_at", c.CreatedAt)

	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Digest returns the hex digest of the canonical encoding.
func (c CampaignRevision) Digest() string {
	raw := c.Canonical()
	if raw == nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}
