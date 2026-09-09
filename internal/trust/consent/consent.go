// Package consent implements the pure TRUST-024 processing-authority
// lifecycle. A versioned notice and its presentation are distinct from an
// affirmative consent decision, and neither is a substitute for another
// explicitly declared lawful basis. Objections and restrictions are
// fail-closed overlays whose downstream propagation can be reconciled by
// consumer acknowledgement receipts.
package consent

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Version is the semantic contract version for this package.
func Version() int { return 1 }

// Errors returned by constructors and lifecycle transitions.
var (
	ErrInvalidRequest      = errors.New("consent: invalid request")
	ErrInvalidNotice       = errors.New("consent: invalid notice")
	ErrInvalidPresentation = errors.New("consent: invalid notice presentation")
	ErrInvalidConsent      = errors.New("consent: invalid consent decision")
	ErrInvalidPreference   = errors.New("consent: invalid preference")
	ErrInvalidObjection    = errors.New("consent: invalid objection")
	ErrInvalidRestriction  = errors.New("consent: invalid restriction")
	ErrConsentBasis        = errors.New("consent: consent requires the consent lawful basis")
	ErrAlreadyWithdrawn    = errors.New("consent: consent is already withdrawn")
	ErrAlreadyResolved     = errors.New("consent: objection is already resolved")
	ErrAlreadyReleased     = errors.New("consent: restriction is already released")
)

// LawfulBasis identifies the legal or contractual authority for processing.
// Consent is one basis, not a generic permission bit.
type LawfulBasis string

const (
	BasisUnspecified     LawfulBasis = ""
	BasisConsent         LawfulBasis = "CONSENT"
	BasisEmployment      LawfulBasis = "EMPLOYMENT"
	BasisContract        LawfulBasis = "CONTRACT"
	BasisLegalObligation LawfulBasis = "LEGAL_OBLIGATION"
)

func (b LawfulBasis) valid() bool {
	return b == BasisConsent || b == BasisEmployment || b == BasisContract || b == BasisLegalObligation
}

// ConsentState is the closed state vocabulary for consent decisions.
type ConsentState string

const (
	ConsentGranted   ConsentState = "GRANTED"
	ConsentRefused   ConsentState = "REFUSED"
	ConsentWithdrawn ConsentState = "WITHDRAWN"
	ConsentExpired   ConsentState = "EXPIRED"
	ConsentInvalid   ConsentState = "INVALID"
)

// Notice is one immutable, versioned processing notice. LegalContext is
// required even for a consent notice so an authority decision cannot be
// detached from the purpose and jurisdiction that make the processing
// intelligible.
type Notice struct {
	ID               string
	Version          string
	Controller       string
	Purpose          string
	DataCategories   []string
	Operations       []string
	RecipientClasses []string
	Jurisdiction     string
	Locale           string
	LegalContext     string
	Basis            LawfulBasis
	EffectiveFrom    values.Instant
	EffectiveTo      values.Instant
}

// Validate rejects a notice that cannot support an authority decision.
func (n Notice) Validate() error {
	fields := []struct {
		name  string
		value string
	}{
		{"id", n.ID}, {"version", n.Version}, {"controller", n.Controller},
		{"purpose", n.Purpose}, {"jurisdiction", n.Jurisdiction},
		{"locale", n.Locale}, {"legal_context", n.LegalContext},
	}
	for _, field := range fields {
		if !nonBlank(field.value) {
			return fmt.Errorf("%w: %s is required", ErrInvalidNotice, field.name)
		}
	}
	if !n.Basis.valid() {
		return fmt.Errorf("%w: lawful basis %q is not supported", ErrInvalidNotice, n.Basis)
	}
	if err := validateSet("data category", n.DataCategories); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidNotice, err)
	}
	if err := validateSet("operation", n.Operations); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidNotice, err)
	}
	if err := validateSet("recipient class", n.RecipientClasses); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidNotice, err)
	}
	if err := n.EffectiveFrom.Validate(); err != nil {
		return fmt.Errorf("%w: effective_from: %v", ErrInvalidNotice, err)
	}
	if n.EffectiveTo.IsSet() && !n.EffectiveFrom.Before(n.EffectiveTo) {
		return fmt.Errorf("%w: effective_to must follow effective_from", ErrInvalidNotice)
	}
	return nil
}

// ActiveAt reports whether this valid notice is effective at asOf.
func (n Notice) ActiveAt(asOf values.Instant) bool {
	if n.Validate() != nil || !asOf.IsSet() || asOf.Before(n.EffectiveFrom) {
		return false
	}
	return !n.EffectiveTo.IsSet() || asOf.Before(n.EffectiveTo)
}

// Digest is the content identity of the exact published notice version.
func (n Notice) Digest() string {
	b := newCanonical()
	b.field("id", n.ID)
	b.field("version", n.Version)
	b.field("controller", n.Controller)
	b.field("purpose", n.Purpose)
	b.list("data_categories", n.DataCategories)
	b.list("operations", n.Operations)
	b.list("recipient_classes", n.RecipientClasses)
	b.field("jurisdiction", n.Jurisdiction)
	b.field("locale", n.Locale)
	b.field("legal_context", n.LegalContext)
	b.field("basis", string(n.Basis))
	b.field("effective_from", n.EffectiveFrom.String())
	b.field("effective_to", n.EffectiveTo.String())
	return b.digest()
}

// Presentation proves that a principal was shown one exact notice digest.
type Presentation struct {
	ID             string
	Subject        string
	NoticeID       string
	NoticeVersion  string
	NoticeDigest   string
	Locale         string
	Channel        string
	Accessible     bool
	PresentedAt    values.Instant
	AcknowledgedAt values.Instant
}

// NewPresentation records the notice version shown to a subject. It does not
// grant authority; an affirmative consent decision is a separate transition.
func NewPresentation(id, subject string, n Notice, locale, channel string, accessible bool, presentedAt, acknowledgedAt values.Instant) (Presentation, error) {
	if err := n.Validate(); err != nil {
		return Presentation{}, err
	}
	p := Presentation{
		ID: id, Subject: subject, NoticeID: n.ID, NoticeVersion: n.Version,
		NoticeDigest: n.Digest(), Locale: locale, Channel: channel,
		Accessible: accessible, PresentedAt: presentedAt, AcknowledgedAt: acknowledgedAt,
	}
	if err := p.ValidateFor(n, subject); err != nil {
		return Presentation{}, err
	}
	return p, nil
}

// ValidateFor checks exact notice binding and presentation evidence.
func (p Presentation) ValidateFor(n Notice, subject string) error {
	if err := n.Validate(); err != nil {
		return fmt.Errorf("%w: notice: %v", ErrInvalidPresentation, err)
	}
	for name, value := range map[string]string{"id": p.ID, "subject": p.Subject, "locale": p.Locale, "channel": p.Channel} {
		if !nonBlank(value) {
			return fmt.Errorf("%w: %s is required", ErrInvalidPresentation, name)
		}
	}
	if p.Subject != subject {
		return fmt.Errorf("%w: subject mismatch", ErrInvalidPresentation)
	}
	if p.NoticeID != n.ID || p.NoticeVersion != n.Version || p.NoticeDigest != n.Digest() {
		return fmt.Errorf("%w: presentation is not bound to the exact notice version", ErrInvalidPresentation)
	}
	if !p.Accessible {
		return fmt.Errorf("%w: inaccessible presentation", ErrInvalidPresentation)
	}
	if err := p.PresentedAt.Validate(); err != nil {
		return fmt.Errorf("%w: presented_at: %v", ErrInvalidPresentation, err)
	}
	if !n.ActiveAt(p.PresentedAt) {
		return fmt.Errorf("%w: notice was not active when presented", ErrInvalidPresentation)
	}
	if err := p.AcknowledgedAt.Validate(); err != nil {
		return fmt.Errorf("%w: acknowledged_at: %v", ErrInvalidPresentation, err)
	}
	if p.AcknowledgedAt.Before(p.PresentedAt) {
		return fmt.Errorf("%w: acknowledgement precedes presentation", ErrInvalidPresentation)
	}
	return nil
}

// GrantRequest is the affirmative action needed to create consent.
type GrantRequest struct {
	ID           string
	Subject      string
	Controller   string
	Purpose      string
	Scope        []string
	Notice       Notice
	Presentation Presentation
	Affirmative  bool
	ExpiresAt    values.Instant
	GrantedAt    values.Instant
}

// Grant creates a version-bound, affirmative consent decision. It refuses
// notices governed by employment, contract, or legal obligation: those bases
// must never be mislabeled as consent.
func Grant(req GrantRequest) (ConsentDecision, error) {
	if req.Notice.Basis != BasisConsent {
		return ConsentDecision{}, fmt.Errorf("%w: notice basis is %s", ErrConsentBasis, req.Notice.Basis)
	}
	if err := validateGrantRequest(req); err != nil {
		return ConsentDecision{}, err
	}
	d := ConsentDecision{
		ID: req.ID, Subject: req.Subject, Controller: req.Controller, Purpose: req.Purpose,
		Scope: slices.Clone(req.Scope), NoticeID: req.Notice.ID, NoticeVersion: req.Notice.Version,
		NoticeDigest: req.Notice.Digest(), PresentationID: req.Presentation.ID,
		State: ConsentGranted, Affirmative: true, GrantedAt: req.GrantedAt, ExpiresAt: req.ExpiresAt,
	}
	d.EvidenceID = d.evidenceID()
	return d, nil
}

// Refuse records an explicit refusal after the same notice and presentation
// checks as a grant. A refusal is evidence of a decision, but it is never an
// affirmative authority to process.
func Refuse(req GrantRequest, reason string) (ConsentDecision, error) {
	if req.Notice.Basis != BasisConsent {
		return ConsentDecision{}, fmt.Errorf("%w: notice basis is %s", ErrConsentBasis, req.Notice.Basis)
	}
	if !nonBlank(reason) {
		return ConsentDecision{}, fmt.Errorf("%w: refusal reason is required", ErrInvalidConsent)
	}
	validationReq := req
	validationReq.Affirmative = true
	if err := validateGrantRequest(validationReq); err != nil {
		return ConsentDecision{}, err
	}
	d := ConsentDecision{
		ID: req.ID, Subject: req.Subject, Controller: req.Controller, Purpose: req.Purpose,
		Scope: slices.Clone(req.Scope), NoticeID: req.Notice.ID, NoticeVersion: req.Notice.Version,
		NoticeDigest: req.Notice.Digest(), PresentationID: req.Presentation.ID,
		State: ConsentRefused, GrantedAt: req.GrantedAt, ExpiresAt: req.ExpiresAt, Reason: reason,
	}
	d.EvidenceID = d.evidenceID()
	return d, nil
}

func validateGrantRequest(req GrantRequest) error {
	if err := req.Notice.Validate(); err != nil {
		return err
	}
	for name, value := range map[string]string{"id": req.ID, "subject": req.Subject, "controller": req.Controller, "purpose": req.Purpose} {
		if !nonBlank(value) {
			return fmt.Errorf("%w: %s is required", ErrInvalidConsent, name)
		}
	}
	if req.Controller != req.Notice.Controller || req.Purpose != req.Notice.Purpose {
		return fmt.Errorf("%w: controller or purpose does not match notice", ErrInvalidConsent)
	}
	if !req.Affirmative {
		return fmt.Errorf("%w: affirmative action is required", ErrInvalidConsent)
	}
	if err := validateSet("scope", req.Scope); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidConsent, err)
	}
	if err := req.GrantedAt.Validate(); err != nil {
		return fmt.Errorf("%w: granted_at: %v", ErrInvalidConsent, err)
	}
	if !req.Notice.ActiveAt(req.GrantedAt) {
		return fmt.Errorf("%w: notice is not active at grant", ErrInvalidConsent)
	}
	if err := req.Presentation.ValidateFor(req.Notice, req.Subject); err != nil {
		return fmt.Errorf("%w: presentation: %v", ErrInvalidConsent, err)
	}
	if req.Presentation.AcknowledgedAt.After(req.GrantedAt) {
		return fmt.Errorf("%w: grant precedes notice acknowledgement", ErrInvalidConsent)
	}
	if req.ExpiresAt.IsSet() && !req.GrantedAt.Before(req.ExpiresAt) {
		return fmt.Errorf("%w: expires_at must follow granted_at", ErrInvalidConsent)
	}
	return nil
}

// ConsentDecision is the append-only decision record. Withdraw returns a new
// record so the grant and the withdrawal remain independently auditable.
type ConsentDecision struct {
	ID             string
	Subject        string
	Controller     string
	Purpose        string
	Scope          []string
	NoticeID       string
	NoticeVersion  string
	NoticeDigest   string
	PresentationID string
	State          ConsentState
	Affirmative    bool
	GrantedAt      values.Instant
	ExpiresAt      values.Instant
	WithdrawnAt    values.Instant
	Reason         string
	EvidenceID     string
}

// Validate checks the decision's required fields and evidence identity.
func (d ConsentDecision) Validate() error {
	for name, value := range map[string]string{
		"id": d.ID, "subject": d.Subject, "controller": d.Controller, "purpose": d.Purpose,
		"notice_id": d.NoticeID, "notice_version": d.NoticeVersion, "notice_digest": d.NoticeDigest,
		"presentation_id": d.PresentationID, "evidence_id": d.EvidenceID,
	} {
		if !nonBlank(value) {
			return fmt.Errorf("%w: %s is required", ErrInvalidConsent, name)
		}
	}
	if d.State != ConsentGranted && d.State != ConsentRefused && d.State != ConsentWithdrawn && d.State != ConsentExpired && d.State != ConsentInvalid {
		return fmt.Errorf("%w: unknown state %q", ErrInvalidConsent, d.State)
	}
	if err := validateSet("scope", d.Scope); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidConsent, err)
	}
	if err := d.GrantedAt.Validate(); err != nil {
		return fmt.Errorf("%w: granted_at: %v", ErrInvalidConsent, err)
	}
	if d.ExpiresAt.IsSet() && !d.GrantedAt.Before(d.ExpiresAt) {
		return fmt.Errorf("%w: expires_at must follow granted_at", ErrInvalidConsent)
	}
	if d.WithdrawnAt.IsSet() && d.WithdrawnAt.Before(d.GrantedAt) {
		return fmt.Errorf("%w: withdrawn_at precedes granted_at", ErrInvalidConsent)
	}
	if d.State == ConsentGranted && !d.Affirmative {
		return fmt.Errorf("%w: granted decision lacks affirmative action", ErrInvalidConsent)
	}
	if d.EvidenceID != d.evidenceID() {
		return fmt.Errorf("%w: evidence does not match canonical decision", ErrInvalidConsent)
	}
	return nil
}

// Withdraw records a permanent withdrawal without mutating the grant.
func (d ConsentDecision) Withdraw(at values.Instant, reason string) (ConsentDecision, error) {
	if err := d.Validate(); err != nil {
		return ConsentDecision{}, err
	}
	if d.State == ConsentWithdrawn {
		return ConsentDecision{}, ErrAlreadyWithdrawn
	}
	if d.State != ConsentGranted {
		return ConsentDecision{}, fmt.Errorf("%w: only a granted decision can be withdrawn", ErrInvalidConsent)
	}
	if err := at.Validate(); err != nil || !nonBlank(reason) || at.Before(d.GrantedAt) {
		return ConsentDecision{}, fmt.Errorf("%w: withdrawal requires a reason and a valid time after grant", ErrInvalidConsent)
	}
	result := d
	result.Scope = slices.Clone(d.Scope)
	result.State, result.WithdrawnAt, result.Reason = ConsentWithdrawn, at, reason
	result.EvidenceID = result.evidenceID()
	return result, nil
}

// StatusAt resolves the state at an instant without storing a second mutable
// status field. Withdrawal takes precedence over expiry at the boundary.
func (d ConsentDecision) StatusAt(at values.Instant) ConsentState {
	if d.Validate() != nil || !at.IsSet() || at.Before(d.GrantedAt) {
		return ConsentInvalid
	}
	if d.State == ConsentWithdrawn && d.WithdrawnAt.IsSet() && !at.Before(d.WithdrawnAt) {
		return ConsentWithdrawn
	}
	if d.ExpiresAt.IsSet() && !at.Before(d.ExpiresAt) {
		return ConsentExpired
	}
	return d.State
}

func (d ConsentDecision) evidenceID() string {
	b := newCanonical()
	b.field("id", d.ID)
	b.field("subject", d.Subject)
	b.field("controller", d.Controller)
	b.field("purpose", d.Purpose)
	b.list("scope", d.Scope)
	b.field("notice_id", d.NoticeID)
	b.field("notice_version", d.NoticeVersion)
	b.field("notice_digest", d.NoticeDigest)
	b.field("presentation_id", d.PresentationID)
	b.field("state", string(d.State))
	b.field("affirmative", fmt.Sprintf("%t", d.Affirmative))
	b.field("granted_at", d.GrantedAt.String())
	b.field("expires_at", d.ExpiresAt.String())
	b.field("withdrawn_at", d.WithdrawnAt.String())
	b.field("reason", d.Reason)
	return "ev:consent:" + b.digest()
}

// PreferenceValue is a versioned subject preference. Preferences cannot
// create consent by themselves, but an active opt-out blocks optional use.
type PreferenceValue string

const (
	PreferenceOptIn  PreferenceValue = "OPT_IN"
	PreferenceOptOut PreferenceValue = "OPT_OUT"
)

// Preference is an immutable preference revision.
type Preference struct {
	ID, Subject, Purpose, Version, Source string
	Value                                 PreferenceValue
	EffectiveFrom, EffectiveTo            values.Instant
	EvidenceID                            string
}

// NewPreference creates a versioned opt-in or opt-out preference.
func NewPreference(id, subject, purpose, version, source string, value PreferenceValue, from, to values.Instant) (Preference, error) {
	p := Preference{ID: id, Subject: subject, Purpose: purpose, Version: version, Source: source, Value: value, EffectiveFrom: from, EffectiveTo: to}
	if err := p.validateWithoutEvidence(); err != nil {
		return Preference{}, err
	}
	p.EvidenceID = "ev:preference:" + p.digest()
	return p, nil
}

func (p Preference) validateWithoutEvidence() error {
	for name, value := range map[string]string{"id": p.ID, "subject": p.Subject, "purpose": p.Purpose, "version": p.Version, "source": p.Source} {
		if !nonBlank(value) {
			return fmt.Errorf("%w: %s is required", ErrInvalidPreference, name)
		}
	}
	if p.Value != PreferenceOptIn && p.Value != PreferenceOptOut {
		return fmt.Errorf("%w: unsupported value %q", ErrInvalidPreference, p.Value)
	}
	if err := p.EffectiveFrom.Validate(); err != nil {
		return fmt.Errorf("%w: effective_from: %v", ErrInvalidPreference, err)
	}
	if p.EffectiveTo.IsSet() && !p.EffectiveFrom.Before(p.EffectiveTo) {
		return fmt.Errorf("%w: effective_to must follow effective_from", ErrInvalidPreference)
	}
	return nil
}

// Validate checks preference evidence.
func (p Preference) Validate() error {
	if err := p.validateWithoutEvidence(); err != nil {
		return err
	}
	if p.EvidenceID != "ev:preference:"+p.digest() {
		return fmt.Errorf("%w: evidence mismatch", ErrInvalidPreference)
	}
	return nil
}

// ActiveAt reports whether a preference revision applies at asOf.
func (p Preference) ActiveAt(asOf values.Instant) bool {
	if p.Validate() != nil || !asOf.IsSet() || asOf.Before(p.EffectiveFrom) {
		return false
	}
	return !p.EffectiveTo.IsSet() || asOf.Before(p.EffectiveTo)
}

func (p Preference) digest() string {
	b := newCanonical()
	b.field("id", p.ID)
	b.field("subject", p.Subject)
	b.field("purpose", p.Purpose)
	b.field("version", p.Version)
	b.field("source", p.Source)
	b.field("value", string(p.Value))
	b.field("effective_from", p.EffectiveFrom.String())
	b.field("effective_to", p.EffectiveTo.String())
	return b.digest()
}

// ObjectionState is the objection lifecycle.
type ObjectionState string

const (
	ObjectionOpen      ObjectionState = "OPEN"
	ObjectionResolved  ObjectionState = "RESOLVED"
	ObjectionWithdrawn ObjectionState = "WITHDRAWN"
)

// Objection is a subject's versioned challenge to processing. An open
// objection is restrictive until a distinct, explicit resolution is recorded.
type Objection struct {
	ID, Subject, Controller, Purpose, Version, Reason string
	Scope                                             []string
	State                                             ObjectionState
	SubmittedAt, ResolvedAt                           values.Instant
	ResolvedBy, Resolution                            string
	EvidenceID                                        string
}

// SubmitObjection opens an objection; it is not an implicit deletion and it
// never silently grants permission to continue processing.
func SubmitObjection(id, subject, controller, purpose, version, reason string, scope []string, at values.Instant) (Objection, error) {
	o := Objection{ID: id, Subject: subject, Controller: controller, Purpose: purpose, Version: version, Reason: reason, Scope: slices.Clone(scope), State: ObjectionOpen, SubmittedAt: at}
	if err := o.validateWithoutEvidence(); err != nil {
		return Objection{}, err
	}
	o.EvidenceID = "ev:objection:" + o.digest()
	return o, nil
}

func (o Objection) validateWithoutEvidence() error {
	for name, value := range map[string]string{"id": o.ID, "subject": o.Subject, "controller": o.Controller, "purpose": o.Purpose, "version": o.Version, "reason": o.Reason} {
		if !nonBlank(value) {
			return fmt.Errorf("%w: %s is required", ErrInvalidObjection, name)
		}
	}
	if err := validateSet("scope", o.Scope); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidObjection, err)
	}
	if o.State != ObjectionOpen && o.State != ObjectionResolved && o.State != ObjectionWithdrawn {
		return fmt.Errorf("%w: unknown state %q", ErrInvalidObjection, o.State)
	}
	if err := o.SubmittedAt.Validate(); err != nil {
		return fmt.Errorf("%w: submitted_at: %v", ErrInvalidObjection, err)
	}
	if o.State == ObjectionResolved {
		if !nonBlank(o.ResolvedBy) || !nonBlank(o.Resolution) {
			return fmt.Errorf("%w: resolved objection needs actor and resolution", ErrInvalidObjection)
		}
		if err := o.ResolvedAt.Validate(); err != nil || o.ResolvedAt.Before(o.SubmittedAt) {
			return fmt.Errorf("%w: invalid resolved_at", ErrInvalidObjection)
		}
	}
	return nil
}

// Validate checks objection evidence.
func (o Objection) Validate() error {
	if err := o.validateWithoutEvidence(); err != nil {
		return err
	}
	if o.EvidenceID != "ev:objection:"+o.digest() {
		return fmt.Errorf("%w: evidence mismatch", ErrInvalidObjection)
	}
	return nil
}

// Resolve closes an open objection with an explicit actor and disposition.
func (o Objection) Resolve(by, resolution string, at values.Instant) (Objection, error) {
	if err := o.Validate(); err != nil {
		return Objection{}, err
	}
	if o.State != ObjectionOpen {
		return Objection{}, ErrAlreadyResolved
	}
	if !nonBlank(by) || !nonBlank(resolution) || !at.IsSet() || at.Before(o.SubmittedAt) {
		return Objection{}, fmt.Errorf("%w: resolution requires actor, disposition and valid time", ErrInvalidObjection)
	}
	r := o
	r.Scope = slices.Clone(o.Scope)
	r.State, r.ResolvedBy, r.Resolution, r.ResolvedAt = ObjectionResolved, by, resolution, at
	r.EvidenceID = "ev:objection:" + r.digest()
	return r, nil
}

// ActiveAt reports whether an objection is restrictive at asOf.
func (o Objection) ActiveAt(asOf values.Instant) bool {
	if o.Validate() != nil || !asOf.IsSet() || asOf.Before(o.SubmittedAt) {
		return false
	}
	return o.State == ObjectionOpen || (o.State == ObjectionResolved && asOf.Before(o.ResolvedAt))
}

func (o Objection) digest() string {
	b := newCanonical()
	b.field("id", o.ID)
	b.field("subject", o.Subject)
	b.field("controller", o.Controller)
	b.field("purpose", o.Purpose)
	b.field("version", o.Version)
	b.field("reason", o.Reason)
	b.list("scope", o.Scope)
	b.field("state", string(o.State))
	b.field("submitted_at", o.SubmittedAt.String())
	b.field("resolved_at", o.ResolvedAt.String())
	b.field("resolved_by", o.ResolvedBy)
	b.field("resolution", o.Resolution)
	return b.digest()
}

// Restriction is an explicit stop-processing overlay. It remains active until
// a separate release transition; downstream use is never allowed while it is
// active.
type Restriction struct {
	ID, Subject, Controller, Purpose, Version, Reason string
	Scope                                             []string
	AppliedAt, ReleasedAt                             values.Instant
	ReleasedBy                                        string
	EvidenceID                                        string
}

// ApplyRestriction records an active restriction.
func ApplyRestriction(id, subject, controller, purpose, version, reason string, scope []string, at values.Instant) (Restriction, error) {
	r := Restriction{ID: id, Subject: subject, Controller: controller, Purpose: purpose, Version: version, Reason: reason, Scope: slices.Clone(scope), AppliedAt: at}
	if err := r.validateWithoutEvidence(); err != nil {
		return Restriction{}, err
	}
	r.EvidenceID = "ev:restriction:" + r.digest()
	return r, nil
}

func (r Restriction) validateWithoutEvidence() error {
	for name, value := range map[string]string{"id": r.ID, "subject": r.Subject, "controller": r.Controller, "purpose": r.Purpose, "version": r.Version, "reason": r.Reason} {
		if !nonBlank(value) {
			return fmt.Errorf("%w: %s is required", ErrInvalidRestriction, name)
		}
	}
	if err := validateSet("scope", r.Scope); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidRestriction, err)
	}
	if err := r.AppliedAt.Validate(); err != nil {
		return fmt.Errorf("%w: applied_at: %v", ErrInvalidRestriction, err)
	}
	if r.ReleasedAt.IsSet() {
		if !nonBlank(r.ReleasedBy) || !r.AppliedAt.Before(r.ReleasedAt) {
			return fmt.Errorf("%w: invalid release transition", ErrInvalidRestriction)
		}
	}
	return nil
}

// Validate checks restriction evidence.
func (r Restriction) Validate() error {
	if err := r.validateWithoutEvidence(); err != nil {
		return err
	}
	if r.EvidenceID != "ev:restriction:"+r.digest() {
		return fmt.Errorf("%w: evidence mismatch", ErrInvalidRestriction)
	}
	return nil
}

// Release returns a new restriction record with a recorded release actor.
func (r Restriction) Release(by string, at values.Instant) (Restriction, error) {
	if err := r.Validate(); err != nil {
		return Restriction{}, err
	}
	if r.ReleasedAt.IsSet() {
		return Restriction{}, ErrAlreadyReleased
	}
	if !nonBlank(by) || !at.IsSet() || at.Before(r.AppliedAt) {
		return Restriction{}, fmt.Errorf("%w: release requires actor and valid time", ErrInvalidRestriction)
	}
	result := r
	result.Scope = slices.Clone(r.Scope)
	result.ReleasedBy, result.ReleasedAt = by, at
	result.EvidenceID = "ev:restriction:" + result.digest()
	return result, nil
}

// ActiveAt reports whether the restriction applies at asOf.
func (r Restriction) ActiveAt(asOf values.Instant) bool {
	if r.Validate() != nil || !asOf.IsSet() || asOf.Before(r.AppliedAt) {
		return false
	}
	return !r.ReleasedAt.IsSet() || asOf.Before(r.ReleasedAt)
}

func (r Restriction) digest() string {
	b := newCanonical()
	b.field("id", r.ID)
	b.field("subject", r.Subject)
	b.field("controller", r.Controller)
	b.field("purpose", r.Purpose)
	b.field("version", r.Version)
	b.field("reason", r.Reason)
	b.list("scope", r.Scope)
	b.field("applied_at", r.AppliedAt.String())
	b.field("released_at", r.ReleasedAt.String())
	b.field("released_by", r.ReleasedBy)
	return b.digest()
}

// AuthorityStatus is the fail-closed result of resolving one request.
type AuthorityStatus string

const (
	AuthorityAllowed    AuthorityStatus = "ALLOW"
	AuthorityDenied     AuthorityStatus = "DENY"
	AuthorityRestricted AuthorityStatus = "RESTRICT"
	AuthorityUnknown    AuthorityStatus = "UNKNOWN"
)

// AuthorityCode is a stable, redaction-safe explanation token.
type AuthorityCode string

const (
	CodeAllowed                 AuthorityCode = "ALLOWED"
	CodeInvalidRequest          AuthorityCode = "INVALID_REQUEST"
	CodeInvalidNotice           AuthorityCode = "NOTICE_INVALID"
	CodeNoticeInactive          AuthorityCode = "NOTICE_INACTIVE"
	CodeNoticeNotPresented      AuthorityCode = "NOTICE_NOT_PRESENTED"
	CodePresentationInvalid     AuthorityCode = "PRESENTATION_INVALID"
	CodeConsentMissing          AuthorityCode = "CONSENT_MISSING"
	CodeConsentInvalid          AuthorityCode = "CONSENT_INVALID"
	CodeConsentNotAffirmed      AuthorityCode = "CONSENT_NOT_AFFIRMED"
	CodeConsentOutOfScope       AuthorityCode = "CONSENT_OUT_OF_SCOPE"
	CodeConsentExpired          AuthorityCode = "CONSENT_EXPIRED"
	CodeConsentWithdrawn        AuthorityCode = "CONSENT_WITHDRAWN"
	CodeOperationNotDeclared    AuthorityCode = "OPERATION_NOT_DECLARED"
	CodeDataCategoryNotDeclared AuthorityCode = "DATA_CATEGORY_NOT_DECLARED"
	CodePreferenceOptedOut      AuthorityCode = "PREFERENCE_OPTED_OUT"
	CodeObjectionActive         AuthorityCode = "OBJECTION_ACTIVE"
	CodeRestrictionActive       AuthorityCode = "RESTRICTION_ACTIVE"
)

// Obligation is a typed downstream obligation created by withdrawal,
// objection, restriction, or an opt-out.
type Obligation string

const (
	ObligationStopProcessing  Obligation = "STOP_PROCESSING"
	ObligationDeleteDerived   Obligation = "DELETE_DERIVED_COPIES"
	ObligationNotifyConsumers Obligation = "NOTIFY_CONSUMERS"
	ObligationReevaluate      Obligation = "REEVALUATE_AUTHORITY"
)

// AuthorityRequest is a fully resolved, read-only policy input.
type AuthorityRequest struct {
	Subject, Controller, Purpose, Operation string
	DataCategories                          []string
	Notice                                  Notice
	Presentation                            *Presentation
	Consent                                 *ConsentDecision
	Preference                              *Preference
	Objection                               *Objection
	Restriction                             *Restriction
	At                                      values.Instant
}

// AuthorityDecision contains the status and evidence identity for one
// processing attempt. AllowsUse is the only safe gate for downstream use.
type AuthorityDecision struct {
	Status        AuthorityStatus
	Code          AuthorityCode
	Subject       string
	Controller    string
	Purpose       string
	Basis         LawfulBasis
	NoticeVersion string
	NoticeDigest  string
	EvaluatedAt   values.Instant
	Obligations   []Obligation
	EvidenceID    string
}

// Resolve evaluates one authority request. It never returns allow for an
// invalid notice, missing consent evidence, active objection, or active
// restriction. Non-consent lawful bases can authorize without consent, but
// only when their explicit legal context is present on the notice.
func Resolve(req AuthorityRequest) AuthorityDecision {
	d := AuthorityDecision{Subject: req.Subject, Controller: req.Controller, Purpose: req.Purpose, Basis: req.Notice.Basis, NoticeVersion: req.Notice.Version, NoticeDigest: req.Notice.Digest(), EvaluatedAt: req.At}
	finish := func(status AuthorityStatus, code AuthorityCode, obligations ...Obligation) AuthorityDecision {
		d.Status, d.Code = status, code
		d.Obligations = uniqueSorted(obligations)
		d.EvidenceID = authorityEvidence(d, req)
		return d
	}
	if !nonBlank(req.Subject) || !nonBlank(req.Controller) || !nonBlank(req.Purpose) || !nonBlank(req.Operation) || !req.At.IsSet() {
		return finish(AuthorityUnknown, CodeInvalidRequest)
	}
	if err := req.Notice.Validate(); err != nil {
		return finish(AuthorityUnknown, CodeInvalidNotice)
	}
	if req.Controller != req.Notice.Controller || req.Purpose != req.Notice.Purpose {
		return finish(AuthorityDenied, CodeInvalidNotice)
	}
	if !req.Notice.ActiveAt(req.At) {
		return finish(AuthorityDenied, CodeNoticeInactive)
	}
	if !slices.Contains(req.Notice.Operations, req.Operation) {
		return finish(AuthorityDenied, CodeOperationNotDeclared)
	}
	for _, category := range req.DataCategories {
		if !slices.Contains(req.Notice.DataCategories, category) {
			return finish(AuthorityDenied, CodeDataCategoryNotDeclared)
		}
	}
	if req.Preference != nil {
		if err := req.Preference.Validate(); err != nil || req.Preference.Subject != req.Subject || req.Preference.Purpose != req.Purpose {
			return finish(AuthorityUnknown, CodeInvalidRequest)
		}
	}
	if req.Objection != nil {
		if err := req.Objection.Validate(); err != nil || req.Objection.Subject != req.Subject || req.Objection.Controller != req.Controller || req.Objection.Purpose != req.Purpose {
			return finish(AuthorityUnknown, CodeInvalidRequest)
		}
	}
	if req.Restriction != nil {
		if err := req.Restriction.Validate(); err != nil || req.Restriction.Subject != req.Subject || req.Restriction.Controller != req.Controller || req.Restriction.Purpose != req.Purpose {
			return finish(AuthorityUnknown, CodeInvalidRequest)
		}
	}
	if req.Objection != nil && req.Objection.ActiveAt(req.At) {
		return finish(AuthorityRestricted, CodeObjectionActive, ObligationStopProcessing, ObligationNotifyConsumers, ObligationReevaluate)
	}
	if req.Restriction != nil && req.Restriction.ActiveAt(req.At) {
		return finish(AuthorityRestricted, CodeRestrictionActive, ObligationStopProcessing, ObligationDeleteDerived, ObligationNotifyConsumers, ObligationReevaluate)
	}
	if req.Preference != nil && req.Preference.ActiveAt(req.At) && req.Preference.Value == PreferenceOptOut {
		return finish(AuthorityRestricted, CodePreferenceOptedOut, ObligationStopProcessing, ObligationNotifyConsumers, ObligationReevaluate)
	}
	if req.Notice.Basis != BasisConsent {
		return finish(AuthorityAllowed, CodeAllowed)
	}
	if req.Presentation == nil {
		return finish(AuthorityDenied, CodeNoticeNotPresented)
	}
	if err := req.Presentation.ValidateFor(req.Notice, req.Subject); err != nil {
		return finish(AuthorityDenied, CodePresentationInvalid)
	}
	if req.Presentation.AcknowledgedAt.After(req.At) {
		return finish(AuthorityDenied, CodeNoticeNotPresented)
	}
	if req.Consent == nil {
		return finish(AuthorityDenied, CodeConsentMissing)
	}
	if err := req.Consent.Validate(); err != nil || req.Consent.Subject != req.Subject || req.Consent.Controller != req.Controller || req.Consent.Purpose != req.Purpose || req.Consent.NoticeDigest != req.Notice.Digest() || req.Consent.PresentationID != req.Presentation.ID {
		return finish(AuthorityDenied, CodeConsentInvalid)
	}
	if !slices.Contains(req.Consent.Scope, req.Purpose) {
		return finish(AuthorityDenied, CodeConsentOutOfScope)
	}
	if req.Consent.StatusAt(req.At) == ConsentWithdrawn {
		return finish(AuthorityRestricted, CodeConsentWithdrawn, ObligationStopProcessing, ObligationDeleteDerived, ObligationNotifyConsumers, ObligationReevaluate)
	}
	switch req.Consent.StatusAt(req.At) {
	case ConsentGranted:
		return finish(AuthorityAllowed, CodeAllowed)
	case ConsentExpired:
		return finish(AuthorityDenied, CodeConsentExpired)
	default:
		return finish(AuthorityDenied, CodeConsentNotAffirmed)
	}
}

// AllowsUse is the downstream processing gate.
func (d AuthorityDecision) AllowsUse() bool {
	return d.Status == AuthorityAllowed && d.Code == CodeAllowed
}

// Explain renders only stable status, code, basis and obligation tokens.
func (d AuthorityDecision) Explain() string {
	return fmt.Sprintf("processing authority status=%s code=%s basis=%s obligations=%v evidence=%s", d.Status, d.Code, d.Basis, d.Obligations, d.EvidenceID)
}

// ConsumerBinding identifies a downstream consumer that must receive a
// restrictive transition before it can be considered reconciled.
type ConsumerBinding struct {
	ID, Consumer, Subject, Purpose, Version string
	DataCategories                          []string
	Active                                  bool
}

// NewConsumerBinding creates a versioned downstream binding.
func NewConsumerBinding(id, consumer, subject, purpose, version string, categories []string) (ConsumerBinding, error) {
	b := ConsumerBinding{ID: id, Consumer: consumer, Subject: subject, Purpose: purpose, Version: version, DataCategories: slices.Clone(categories), Active: true}
	if !nonBlank(id) || !nonBlank(consumer) || !nonBlank(subject) || !nonBlank(purpose) || !nonBlank(version) {
		return ConsumerBinding{}, fmt.Errorf("%w: consumer binding fields are required", ErrInvalidRequest)
	}
	if err := validateSet("data category", categories); err != nil {
		return ConsumerBinding{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	return b, nil
}

// ReceiptStatus is the closed acknowledgement vocabulary.
type ReceiptStatus string

const (
	ReceiptAcknowledged ReceiptStatus = "ACKNOWLEDGED"
	ReceiptFailed       ReceiptStatus = "FAILED"
)

// PropagationReceipt proves one consumer observed a restrictive authority
// transition. A receipt is bound to both the consumer binding and decision.
type PropagationReceipt struct {
	BindingID, Consumer, AuthorityEvidenceID string
	Status                                   ReceiptStatus
	ObservedAt                               values.Instant
	EvidenceID                               string
}

// Acknowledge creates a positive propagation receipt.
func Acknowledge(binding ConsumerBinding, authority AuthorityDecision, at values.Instant) (PropagationReceipt, error) {
	if !binding.Active || !nonBlank(binding.ID) || !nonBlank(binding.Consumer) || !authority.AllowsUse() && authority.EvidenceID == "" {
		return PropagationReceipt{}, fmt.Errorf("%w: active binding and decision evidence are required", ErrInvalidRequest)
	}
	if err := at.Validate(); err != nil {
		return PropagationReceipt{}, fmt.Errorf("%w: observed_at: %v", ErrInvalidRequest, err)
	}
	r := PropagationReceipt{BindingID: binding.ID, Consumer: binding.Consumer, AuthorityEvidenceID: authority.EvidenceID, Status: ReceiptAcknowledged, ObservedAt: at}
	r.EvidenceID = "ev:propagation:" + receiptDigest(r)
	return r, nil
}

// Reconciliation reports whether all active consumers have acknowledged the
// authority transition. It never treats a missing or mismatched receipt as
// success.
type Reconciliation struct {
	AuthorityEvidenceID string
	Pending             []string
	Acknowledged        []string
	Complete            bool
	EvidenceID          string
}

// Reconcile computes the bounded propagation state for an authority decision.
func Reconcile(authority AuthorityDecision, bindings []ConsumerBinding, receipts []PropagationReceipt) Reconciliation {
	r := Reconciliation{AuthorityEvidenceID: authority.EvidenceID}
	ack := make(map[string]bool, len(receipts))
	for _, binding := range bindings {
		if !binding.Active {
			continue
		}
		for _, receipt := range receipts {
			if receipt.Status == ReceiptAcknowledged && receipt.BindingID == binding.ID && receipt.Consumer == binding.Consumer && receipt.AuthorityEvidenceID == authority.EvidenceID && receipt.EvidenceID == "ev:propagation:"+receiptDigest(receipt) {
				ack[binding.ID] = true
				break
			}
		}
		if ack[binding.ID] {
			r.Acknowledged = append(r.Acknowledged, binding.ID)
		} else {
			r.Pending = append(r.Pending, binding.ID)
		}
	}
	slices.Sort(r.Acknowledged)
	slices.Sort(r.Pending)
	r.Complete = len(r.Pending) == 0
	r.EvidenceID = "ev:reconciliation:" + reconciliationDigest(r)
	return r
}

func authorityEvidence(d AuthorityDecision, req AuthorityRequest) string {
	b := newCanonical()
	b.field("status", string(d.Status))
	b.field("code", string(d.Code))
	b.field("subject", d.Subject)
	b.field("controller", d.Controller)
	b.field("purpose", d.Purpose)
	b.field("basis", string(d.Basis))
	b.field("notice_version", d.NoticeVersion)
	b.field("notice_digest", d.NoticeDigest)
	b.field("at", d.EvaluatedAt.String())
	b.field("operation", req.Operation)
	b.list("data_categories", req.DataCategories)
	b.list("obligations", stringSlice(d.Obligations))
	return "ev:authority:" + b.digest()
}

func receiptDigest(r PropagationReceipt) string {
	b := newCanonical()
	b.field("binding_id", r.BindingID)
	b.field("consumer", r.Consumer)
	b.field("authority", r.AuthorityEvidenceID)
	b.field("status", string(r.Status))
	b.field("observed_at", r.ObservedAt.String())
	return b.digest()
}

func reconciliationDigest(r Reconciliation) string {
	b := newCanonical()
	b.field("authority", r.AuthorityEvidenceID)
	b.list("pending", r.Pending)
	b.list("acknowledged", r.Acknowledged)
	b.field("complete", fmt.Sprintf("%t", r.Complete))
	return b.digest()
}

func uniqueSorted(items []Obligation) []Obligation {
	seen := make(map[Obligation]struct{}, len(items))
	result := make([]Obligation, 0, len(items))
	for _, item := range items {
		if _, ok := seen[item]; !ok {
			seen[item] = struct{}{}
			result = append(result, item)
		}
	}
	slices.Sort(result)
	return result
}

func stringSlice[T ~string](items []T) []string {
	result := make([]string, len(items))
	for i, item := range items {
		result[i] = string(item)
	}
	return result
}

func validateSet(name string, items []string) error {
	if len(items) == 0 {
		return fmt.Errorf("%s is required", name)
	}
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if !nonBlank(item) {
			return fmt.Errorf("%s contains a blank value", name)
		}
		if _, exists := seen[item]; exists {
			return fmt.Errorf("%s contains duplicate %q", name, item)
		}
		seen[item] = struct{}{}
	}
	return nil
}

func nonBlank(value string) bool { return strings.TrimSpace(value) == value && value != "" }

type canonical struct{ data []byte }

func newCanonical() *canonical { return &canonical{data: []byte("trust-consent-v1\x00")} }

func (c *canonical) field(label, value string) {
	c.data = append(c.data, []byte(fmt.Sprintf("%d:%s=%d:%s;", len(label), label, len(value), value))...)
}

func (c *canonical) list(label string, values []string) {
	c.field(label+".count", fmt.Sprintf("%d", len(values)))
	for _, value := range values {
		c.field(label+".item", value)
	}
}

func (c *canonical) digest() string {
	sum := sha256.Sum256(c.data)
	return hex.EncodeToString(sum[:])
}
