package formcontinuity

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Channel identifies the human route used to complete a form.
type Channel string

const (
	ChannelWeb        Channel = "WEB"
	ChannelRTL        Channel = "RTL"
	ChannelAccessible Channel = "ACCESSIBLE"
	ChannelPhone      Channel = "PHONE"
	ChannelInPerson   Channel = "IN_PERSON"
	ChannelPostal     Channel = "POSTAL"
)

// Outcome is the result of attempting to establish continuity.
type Outcome string

const (
	OutcomeSafe       Outcome = "SAFE_TO_CONTINUE"
	OutcomeBlocked    Outcome = "BLOCKED"
	OutcomeExpired    Outcome = "EXPIRED"
	OutcomeRevalidate Outcome = "REVALIDATE_REQUIRED"
)

// ErrInvalid identifies malformed continuity input. ErrUnsafe identifies a
// route that would weaken the governing form contract.
var (
	ErrInvalid = errors.New("invalid form continuity")
	ErrUnsafe  = errors.New("unsafe form continuity")
)

// Identity binds the respondent to the authoritative principal. An assistant
// can be recorded but cannot become the respondent or decision maker.
type Identity struct {
	PrincipalID string
	Assurance   string
	AssistantID string
}

// Authority binds the route to the authority held by the respondent.
type Authority struct {
	DecisionRight string
	Scope         string
	AuthorityRef  string
}

// Attribution records who supplied and who transcribed a response.
type Attribution struct {
	RespondentID  string
	AssistantID   string
	TranscriberID string
	ReadBackBy    string
}

// Privacy keeps the route within the same data compartment and purpose.
type Privacy struct {
	Purpose       string
	Compartment   string
	RedactionRule string
}

// Validation identifies the server-side form revision and validation proof.
type Validation struct {
	FormRevision string
	SchemaDigest string
	ProofDigest  string
}

// ContinuityRequest is an immutable snapshot of the canonical route and its
// proposed alternate. Deadline is intentionally supplied by the server-held
// canonical snapshot, not recalculated by an alternate channel.
type ContinuityRequest struct {
	FormID            string
	TaskVersion       string
	CanonicalChannel  Channel
	AlternateChannel  Channel
	OriginalDeadline  time.Time
	Now               time.Time
	Identity          Identity
	Authority         Authority
	Attribution       Attribution
	Privacy           Privacy
	Validation        Validation
	AccommodationRef  string
	TranscriptionRef  string
	ReadBackConfirmed bool
	EvidenceDigest    string
	ResponseDigest    string
}

// Request and Record are concise aliases for callers that already operate on
// a form request/record pair.
type Request = ContinuityRequest

// ContinuityRecord is the auditable decision to use an alternate route.
type ContinuityRecord struct {
	Outcome           Outcome
	FormID            string
	TaskVersion       string
	Channel           Channel
	OriginalDeadline  time.Time
	RespondentID      string
	AssistantID       string
	AuthorityRef      string
	Purpose           string
	Compartment       string
	FormRevision      string
	SchemaDigest      string
	AccommodationRef  string
	TranscriptionRef  string
	ReadBackConfirmed bool
	EvidenceDigest    string
	ResponseDigest    string
}

type Record = ContinuityRecord

// Deadline returns the canonical deadline. Alternate routes cannot extend it.
func (r ContinuityRecord) Deadline() time.Time { return r.OriginalDeadline }

func required(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%w: %s is required", ErrInvalid, field)
	}
	return nil
}

func validChannel(c Channel) bool {
	switch c {
	case ChannelWeb, ChannelRTL, ChannelAccessible, ChannelPhone, ChannelInPerson, ChannelPostal:
		return true
	default:
		return false
	}
}

// Validate checks all invariant-bearing fields. It does not authorize a
// decision; it only proves that continuity can safely preserve the canonical
// contract.
func (r ContinuityRequest) Validate() error {
	for _, v := range []struct{ n, v string }{{"form_id", r.FormID}, {"task_version", r.TaskVersion}, {"identity.principal_id", r.Identity.PrincipalID}, {"identity.assurance", r.Identity.Assurance}, {"authority.decision_right", r.Authority.DecisionRight}, {"authority.scope", r.Authority.Scope}, {"authority.authority_ref", r.Authority.AuthorityRef}, {"attribution.respondent_id", r.Attribution.RespondentID}, {"privacy.purpose", r.Privacy.Purpose}, {"privacy.compartment", r.Privacy.Compartment}, {"validation.form_revision", r.Validation.FormRevision}, {"validation.schema_digest", r.Validation.SchemaDigest}, {"validation.proof_digest", r.Validation.ProofDigest}, {"evidence_digest", r.EvidenceDigest}, {"response_digest", r.ResponseDigest}} {
		if err := required(v.n, v.v); err != nil {
			return err
		}
	}
	if !validChannel(r.CanonicalChannel) || !validChannel(r.AlternateChannel) || r.CanonicalChannel == r.AlternateChannel {
		return fmt.Errorf("%w: channels must be known and alternate", ErrInvalid)
	}
	if r.OriginalDeadline.IsZero() || r.Now.IsZero() {
		return fmt.Errorf("%w: original_deadline and now are required", ErrInvalid)
	}
	if r.Identity.AssistantID != "" && r.Attribution.AssistantID != r.Identity.AssistantID {
		return fmt.Errorf("%w: assistant attribution mismatch", ErrUnsafe)
	}
	if r.Attribution.RespondentID != r.Identity.PrincipalID {
		return fmt.Errorf("%w: respondent attribution does not match identity", ErrUnsafe)
	}
	if r.AlternateChannel == ChannelPhone || r.AlternateChannel == ChannelInPerson || r.AlternateChannel == ChannelPostal {
		if r.TranscriptionRef == "" || !r.ReadBackConfirmed {
			return fmt.Errorf("%w: assisted route requires transcription and confirmed read-back", ErrInvalid)
		}
	}
	if r.AlternateChannel == ChannelAccessible && r.AccommodationRef == "" {
		return fmt.Errorf("%w: accessible route requires accommodation", ErrInvalid)
	}
	return nil
}

// Establish validates and records an alternate route. It retains the exact
// original deadline and respondent authority; no assistant authority is ever
// copied into the record.
func Establish(r ContinuityRequest) (ContinuityRecord, error) {
	if err := r.Validate(); err != nil {
		return ContinuityRecord{}, err
	}
	if !r.Now.Before(r.OriginalDeadline) {
		return ContinuityRecord{}, fmt.Errorf("%w: original deadline has passed", ErrUnsafe)
	}
	return ContinuityRecord{Outcome: OutcomeSafe, FormID: r.FormID, TaskVersion: r.TaskVersion, Channel: r.AlternateChannel, OriginalDeadline: r.OriginalDeadline, RespondentID: r.Identity.PrincipalID, AssistantID: r.Attribution.AssistantID, AuthorityRef: r.Authority.AuthorityRef, Purpose: r.Privacy.Purpose, Compartment: r.Privacy.Compartment, FormRevision: r.Validation.FormRevision, SchemaDigest: r.Validation.SchemaDigest, AccommodationRef: r.AccommodationRef, TranscriptionRef: r.TranscriptionRef, ReadBackConfirmed: r.ReadBackConfirmed, EvidenceDigest: r.EvidenceDigest, ResponseDigest: r.ResponseDigest}, nil
}

// Route is the compatibility spelling for Establish used by channel adapters.
func Route(r Request) (Record, error) { return Establish(r) }
