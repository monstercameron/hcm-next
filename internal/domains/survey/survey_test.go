package survey

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// validQuestion creates a valid question for testing.
func validQuestion(t *testing.T, kind QuestionKind, qid string) Question {
	t.Helper()
	q := Question{
		QuestionID: qid,
		Kind:       kind,
		Text:       "Sample question for " + qid,
		Required:   true,
	}
	switch kind {
	case QuestionKindScale:
		q.ScaleMin, q.ScaleMax = 1, 5
	case QuestionKindChoice:
		q.Choices = []string{"Yes", "No"}
	case QuestionKindMultiChoice:
		q.Choices = []string{"Option A", "Option B", "Option C"}
	case QuestionKindFreeText:
		q.Classification = "feedback"
	}
	return q
}

// validQuestionBank creates a valid question bank for testing.
func validQuestionBank(t *testing.T) QuestionBankRevision {
	t.Helper()
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	return QuestionBankRevision{
		QuestionBankID: "550e8400-e29b-41d4-a716-446655440002",
		Revision:       1,
		CreatedAt:      values.NewInstant(now),
		Questions: []Question{
			validQuestion(t, QuestionKindScale, "550e8400-e29b-41d4-a716-446655440006"),
			validQuestion(t, QuestionKindChoice, "550e8400-e29b-41d4-a716-446655440007"),
			validQuestion(t, QuestionKindMultiChoice, "550e8400-e29b-41d4-a716-446655440008"),
			validQuestion(t, QuestionKindFreeText, "550e8400-e29b-41d4-a716-446655440009"),
		},
	}
}

// validSurvey creates a valid survey for testing.
func validSurvey(t *testing.T) SurveyRevision {
	t.Helper()
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	return SurveyRevision{
		SurveyID:    "550e8400-e29b-41d4-a716-446655440001",
		Revision:    1,
		Title:       "Employee Experience Survey",
		Description: "Annual employee experience feedback collection",
		QuestionBankRef: values.EntityRef{
			Tenant: "test-tenant",
			Kind:   "question_bank",
			Id:     "550e8400-e29b-41d4-a716-446655440002",
		},
		CreatedAt: values.NewInstant(now),
	}
}

// validCampaign creates a valid campaign for testing.
func validCampaign(t *testing.T) CampaignRevision {
	t.Helper()
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 9, 30, 23, 59, 59, 0, time.UTC)
	return CampaignRevision{
		CampaignID: "550e8400-e29b-41d4-a716-446655440003",
		Revision:   1,
		SurveyRef: values.EntityRef{
			Tenant: "test-tenant",
			Kind:   "survey",
			Id:     "550e8400-e29b-41d4-a716-446655440001",
		},
		PopulationSnapshotRef: values.EntityRef{
			Tenant: "test-tenant",
			Kind:   "population_snapshot",
			Id:     "550e8400-e29b-41d4-a716-446655440004",
		},
		WindowStart:        values.NewInstant(start),
		WindowEnd:          values.NewInstant(end),
		AnonymityThreshold: 5,
		ChannelRefs: []values.EntityRef{
			{
				Tenant: "test-tenant",
				Kind:   "channel",
				Id:     "550e8400-e29b-41d4-a716-446655440005",
			},
		},
		CreatedAt: values.NewInstant(start),
	}
}

// TestTodo_SURVEY_001 is the PRIMARY test for survey revision definition.
func TestTodo_SURVEY_001(t *testing.T) {
	qb := validQuestionBank(t)
	if err := qb.Validate(); err != nil {
		t.Fatalf("valid question bank failed validation: %v", err)
	}
	if len(qb.Canonical()) == 0 {
		t.Fatal("valid question bank has no canonical encoding")
	}
	if qb.Digest() == "" {
		t.Fatal("valid question bank has no digest")
	}

	s := validSurvey(t)
	if err := s.Validate(); err != nil {
		t.Fatalf("valid survey failed validation: %v", err)
	}
	if len(s.Canonical()) == 0 {
		t.Fatal("valid survey has no canonical encoding")
	}
	if s.Digest() == "" {
		t.Fatal("valid survey has no digest")
	}

	c := validCampaign(t)
	if err := c.Validate(); err != nil {
		t.Fatalf("valid campaign failed validation: %v", err)
	}
	if len(c.Canonical()) == 0 {
		t.Fatal("valid campaign has no canonical encoding")
	}
	if c.Digest() == "" {
		t.Fatal("valid campaign has no digest")
	}
}

// TestTodo_SURVEY_001_Golden tests that canonical digests are stable.
func TestTodo_SURVEY_001_Golden(t *testing.T) {
	qb := validQuestionBank(t)
	firstDigest := qb.Digest()

	// Call multiple times; digest must be identical.
	for i := 0; i < 3; i++ {
		if got := qb.Digest(); got != firstDigest {
			t.Fatalf("digest inconsistent: iteration %d got %q, want %q", i, got, firstDigest)
		}
	}

	// Same for survey.
	s := validSurvey(t)
	sFirstDigest := s.Digest()
	for i := 0; i < 3; i++ {
		if got := s.Digest(); got != sFirstDigest {
			t.Fatalf("survey digest inconsistent: iteration %d got %q, want %q", i, got, sFirstDigest)
		}
	}

	// Same for campaign.
	c := validCampaign(t)
	cFirstDigest := c.Digest()
	for i := 0; i < 3; i++ {
		if got := c.Digest(); got != cFirstDigest {
			t.Fatalf("campaign digest inconsistent: iteration %d got %q, want %q", i, got, cFirstDigest)
		}
	}
}

// TestTodo_SURVEY_001_Security tests that free-text questions require classification
// and that campaigns without a population snapshot are rejected.
func TestTodo_SURVEY_001_Security(t *testing.T) {
	tests := []struct {
		name    string
		setupFn func(*testing.T) interface{}
		want    error
	}{
		{"free text without classification", func(t *testing.T) interface{} {
			q := validQuestion(t, QuestionKindFreeText, "q-bad")
			q.Classification = "" // This makes it invalid
			return q
		}, ErrFreeTextRequiresClassification},
		{"campaign without population snapshot", func(t *testing.T) interface{} {
			c := validCampaign(t)
			c.PopulationSnapshotRef = values.EntityRef{} // This makes it invalid
			return c
		}, ErrInvalidCampaignRevision},
		{"campaign with zero anonymity threshold", func(t *testing.T) interface{} {
			c := validCampaign(t)
			c.AnonymityThreshold = 0
			return c
		}, ErrAnonymityThresholdTooLow},
		{"campaign with threshold below 5", func(t *testing.T) interface{} {
			c := validCampaign(t)
			c.AnonymityThreshold = 3
			return c
		}, ErrAnonymityThresholdTooLow},
		{"campaign with no channel refs", func(t *testing.T) interface{} {
			c := validCampaign(t)
			c.ChannelRefs = []values.EntityRef{}
			return c
		}, ErrEmptyChannelRefs},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			obj := tc.setupFn(t)
			var err error
			switch v := obj.(type) {
			case Question:
				err = v.Validate()
			case CampaignRevision:
				err = v.Validate()
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("got error %v, want %v", err, tc.want)
			}
		})
	}
}

// TestTodo_SURVEY_001_Mutation tests that changes to any field alter the digest.
func TestTodo_SURVEY_001_Mutation(t *testing.T) {
	t.Run("question bank mutations", func(t *testing.T) {
		baseQB := validQuestionBank(t)
		baseDigest := baseQB.Digest()

		cases := []struct {
			name   string
			mutate func(*QuestionBankRevision)
		}{
			{"question_bank_id", func(qb *QuestionBankRevision) { qb.QuestionBankID = "qbank-002" }},
			{"revision", func(qb *QuestionBankRevision) { qb.Revision = 2 }},
			{"created_at", func(qb *QuestionBankRevision) {
				qb.CreatedAt = values.NewInstant(time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC))
			}},
			{"question text", func(qb *QuestionBankRevision) {
				qb.Questions[0].Text = "Modified question"
			}},
			{"add question", func(qb *QuestionBankRevision) {
				qb.Questions = append(qb.Questions, validQuestion(t, QuestionKindScale, "q5"))
			}},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				qb := baseQB
				tc.mutate(&qb)
				if got := qb.Digest(); got == baseDigest {
					t.Fatal("mutation did not change digest")
				}
			})
		}
	})

	t.Run("survey mutations", func(t *testing.T) {
		baseSurvey := validSurvey(t)
		baseDigest := baseSurvey.Digest()

		cases := []struct {
			name   string
			mutate func(*SurveyRevision)
		}{
			{"survey_id", func(s *SurveyRevision) { s.SurveyID = "550e8400-e29b-41d4-a716-446655440010" }},
			{"revision", func(s *SurveyRevision) { s.Revision = 2 }},
			{"title", func(s *SurveyRevision) { s.Title = "Different Title" }},
			{"description", func(s *SurveyRevision) { s.Description = "New description" }},
			{"question_bank_ref", func(s *SurveyRevision) {
				s.QuestionBankRef.Id = "550e8400-e29b-41d4-a716-446655440011"
			}},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				s := baseSurvey // Create a new instance for each test
				tc.mutate(&s)
				if got := s.Digest(); got == baseDigest {
					t.Fatalf("mutation %q did not change digest: base=%q, got=%q", tc.name, baseDigest, got)
				}
			})
		}
	})

	t.Run("campaign mutations", func(t *testing.T) {
		baseCampaign := validCampaign(t)
		baseDigest := baseCampaign.Digest()

		cases := []struct {
			name   string
			mutate func(*CampaignRevision)
		}{
			{"campaign_id", func(c *CampaignRevision) { c.CampaignID = "550e8400-e29b-41d4-a716-446655440012" }},
			{"revision", func(c *CampaignRevision) { c.Revision = 2 }},
			{"survey_ref", func(c *CampaignRevision) { c.SurveyRef.Id = "550e8400-e29b-41d4-a716-446655440013" }},
			{"population_snapshot_ref", func(c *CampaignRevision) {
				c.PopulationSnapshotRef.Id = "550e8400-e29b-41d4-a716-446655440014"
			}},
			{"window_start", func(c *CampaignRevision) {
				c.WindowStart = values.NewInstant(time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC))
			}},
			{"anonymity_threshold", func(c *CampaignRevision) { c.AnonymityThreshold = 10 }},
			{"add channel", func(c *CampaignRevision) {
				c.ChannelRefs = append(c.ChannelRefs, values.EntityRef{
					Tenant: "test-tenant",
					Kind:   "channel",
					Id:     "550e8400-e29b-41d4-a716-446655440015",
				})
			}},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				c := baseCampaign // Create a new instance for each test
				tc.mutate(&c)
				if got := c.Digest(); got == baseDigest {
					t.Fatalf("mutation %q did not change digest: base=%q, got=%q", tc.name, baseDigest, got)
				}
			})
		}
	})
}

// TestTodo_SURVEY_001_Conformance tests that invalid data is rejected and canonical encoding always returns nil.
func TestTodo_SURVEY_001_Conformance(t *testing.T) {
	tests := []struct {
		name    string
		setupFn func(*testing.T) interface{}
		want    error
	}{
		{"question bank empty", func(t *testing.T) interface{} {
			qb := validQuestionBank(t)
			qb.Questions = []Question{}
			return qb
		}, ErrQuestionBankEmpty},
		{"question bank with zero revision", func(t *testing.T) interface{} {
			qb := validQuestionBank(t)
			qb.Revision = 0
			return qb
		}, ErrInvalidQuestionBankRevision},
		{"survey with empty title", func(t *testing.T) interface{} {
			s := validSurvey(t)
			s.Title = ""
			return s
		}, ErrInvalidSurveyRevision},
		{"survey with zero revision", func(t *testing.T) interface{} {
			s := validSurvey(t)
			s.Revision = 0
			return s
		}, ErrInvalidSurveyRevision},
		{"campaign with inverted window", func(t *testing.T) interface{} {
			c := validCampaign(t)
			end := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
			start := time.Date(2026, 9, 30, 23, 59, 59, 0, time.UTC)
			c.WindowStart = values.NewInstant(start)
			c.WindowEnd = values.NewInstant(end)
			return c
		}, ErrInvalidCampaignRevision},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			obj := tc.setupFn(t)
			var err error
			var canonical []byte
			switch v := obj.(type) {
			case QuestionBankRevision:
				err = v.Validate()
				canonical = v.Canonical()
			case SurveyRevision:
				err = v.Validate()
				canonical = v.Canonical()
			case CampaignRevision:
				err = v.Validate()
				canonical = v.Canonical()
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("got error %v, want %v", err, tc.want)
			}
			if canonical != nil {
				t.Fatal("invalid object returned non-nil canonical bytes")
			}
		})
	}
}

// TestQuestionValidation tests question kind-specific validation.
func TestQuestionValidation(t *testing.T) {
	tests := []struct {
		name    string
		setupFn func(*testing.T) Question
		want    error
	}{
		{"empty question id", func(t *testing.T) Question {
			q := validQuestion(t, QuestionKindScale, "")
			q.QuestionID = ""
			return q
		}, ErrInvalidQuestion},
		{"empty question text", func(t *testing.T) Question {
			q := validQuestion(t, QuestionKindScale, "q")
			q.Text = ""
			return q
		}, ErrInvalidQuestion},
		{"scale with invalid range", func(t *testing.T) Question {
			q := validQuestion(t, QuestionKindScale, "q")
			q.ScaleMin = 5
			q.ScaleMax = 5
			return q
		}, ErrInvalidQuestion},
		{"choice with no options", func(t *testing.T) Question {
			q := validQuestion(t, QuestionKindChoice, "q")
			q.Choices = []string{}
			return q
		}, ErrInvalidQuestion},
		{"choice with empty option", func(t *testing.T) Question {
			q := validQuestion(t, QuestionKindChoice, "q")
			q.Choices = []string{"", "Option"}
			return q
		}, ErrInvalidQuestion},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q := tc.setupFn(t)
			if err := q.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("got error %v, want %v", err, tc.want)
			}
		})
	}
}

// TestDuplicateQuestionIDs tests that duplicate question IDs are rejected.
func TestDuplicateQuestionIDs(t *testing.T) {
	qb := validQuestionBank(t)
	qb.Questions[1].QuestionID = qb.Questions[0].QuestionID // Duplicate
	if err := qb.Validate(); !errors.Is(err, ErrInvalidQuestionBankRevision) {
		t.Fatalf("duplicate question IDs should be rejected, got %v", err)
	}
}

// TestDuplicateChannelRefs tests that duplicate channel refs are rejected.
func TestDuplicateChannelRefs(t *testing.T) {
	c := validCampaign(t)
	// Add a duplicate channel ref
	c.ChannelRefs = append(c.ChannelRefs, c.ChannelRefs[0])
	if err := c.Validate(); !errors.Is(err, ErrInvalidCampaignRevision) {
		t.Fatalf("duplicate channel refs should be rejected, got %v", err)
	}
}

// TestChannelRefDeterminism tests that channel refs are canonicalized in sorted order.
func TestChannelRefDeterminism(t *testing.T) {
	c1 := validCampaign(t)
	c1.ChannelRefs = []values.EntityRef{
		{Tenant: "test-tenant", Kind: "channel", Id: "550e8400-e29b-41d4-a716-446655440020"},
		{Tenant: "test-tenant", Kind: "channel", Id: "550e8400-e29b-41d4-a716-446655440010"},
		{Tenant: "test-tenant", Kind: "channel", Id: "550e8400-e29b-41d4-a716-446655440015"},
	}

	c2 := validCampaign(t)
	c2.ChannelRefs = []values.EntityRef{
		{Tenant: "test-tenant", Kind: "channel", Id: "550e8400-e29b-41d4-a716-446655440010"},
		{Tenant: "test-tenant", Kind: "channel", Id: "550e8400-e29b-41d4-a716-446655440020"},
		{Tenant: "test-tenant", Kind: "channel", Id: "550e8400-e29b-41d4-a716-446655440015"},
	}

	if c1.Digest() != c2.Digest() {
		t.Fatal("channel refs in different order should produce same digest")
	}
}
