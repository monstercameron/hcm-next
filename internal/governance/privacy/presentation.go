package privacy

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// ErrPresentationInvalid is returned by [Presentation.Validate] when a
// presentation record is missing evidence, or when its recorded EvidenceID
// no longer matches its own canonical digest (tamper evidence).
var ErrPresentationInvalid = errors.New("privacy: presentation fails validation")

// evidencePrefix names the durable evidence identifier's namespace, mirroring
// internal/trust/authz.Decision.EvidenceID's "ev:authz:" convention.
const presentationEvidencePrefix = "ev:privacy:presentation:"

// Presentation is the PRIV-002 evidence that one exact [Notice] version was
// shown to one principal, in one locale, through one channel, at one
// recorded time. [EvaluateAuthority] never accepts a Notice's mere existence
// as proof it was seen: it requires a Presentation whose
// NoticeID/NoticeVersion/NoticeDigest match the Notice currently in force,
// so a notice that was republished without a fresh presentation cannot
// authorize anything under the new version.
//
// Accessible records whether the presentation channel met this package's
// accessibility bar (see [RenderNoticeDocument] and
// TestTodo_PRIV_002_Browser) for the recorded Locale. A presentation that
// was shown through an inaccessible channel, or in a locale the notice does
// not support, is not valid authorization evidence even if the principal
// went on to grant consent.
type Presentation struct {
	ID             string
	NoticeID       string
	NoticeVersion  string
	NoticeDigest   string
	Principal      string
	Locale         string
	Accessible     bool
	PresentedAt    values.Instant
	AcknowledgedAt values.Instant // zero (unset) means shown but not acknowledged
	EvidenceID     string
}

// NewPresentation builds a Presentation bound to the exact notice version
// supplied, validates it, and computes its EvidenceID from the resulting
// record's own canonical digest. It is the only supported constructor:
// EvidenceID is always derived, never caller-assigned, so evidence identity
// cannot be forged independently of what was actually recorded.
func NewPresentation(id string, notice Notice, principal, locale string, accessible bool, presentedAt, acknowledgedAt values.Instant) (Presentation, error) {
	if err := notice.Validate(); err != nil {
		return Presentation{}, fmt.Errorf("%w: presentation %q bound to invalid notice: %v", ErrPresentationInvalid, id, err)
	}
	p := Presentation{
		ID:             id,
		NoticeID:       notice.ID,
		NoticeVersion:  notice.Version,
		NoticeDigest:   notice.Digest(),
		Principal:      principal,
		Locale:         locale,
		Accessible:     accessible,
		PresentedAt:    presentedAt,
		AcknowledgedAt: acknowledgedAt,
	}
	if err := p.validateWithoutEvidence(); err != nil {
		return Presentation{}, err
	}
	p.EvidenceID = presentationEvidencePrefix + p.canonicalDigest()
	return p, nil
}

func (p Presentation) validateWithoutEvidence() error {
	if p.ID == "" {
		return fmt.Errorf("%w: no id", ErrPresentationInvalid)
	}
	if p.NoticeID == "" || p.NoticeVersion == "" || p.NoticeDigest == "" {
		return fmt.Errorf("%w: presentation %q is not bound to a complete notice reference", ErrPresentationInvalid, p.ID)
	}
	if p.Principal == "" {
		return fmt.Errorf("%w: presentation %q has no principal", ErrPresentationInvalid, p.ID)
	}
	if p.Locale == "" {
		return fmt.Errorf("%w: presentation %q has no locale", ErrPresentationInvalid, p.ID)
	}
	if !p.PresentedAt.IsSet() {
		return fmt.Errorf("%w: presentation %q has no presented_at", ErrPresentationInvalid, p.ID)
	}
	if p.AcknowledgedAt.IsSet() && p.AcknowledgedAt.Before(p.PresentedAt) {
		return fmt.Errorf("%w: presentation %q acknowledged before it was presented", ErrPresentationInvalid, p.ID)
	}
	return nil
}

// Validate reports whether the presentation carries every required field
// and whether its EvidenceID still matches its own canonical digest. A
// record whose fields were mutated after construction without recomputing
// EvidenceID fails here rather than silently passing as authorization
// evidence.
func (p Presentation) Validate() error {
	if err := p.validateWithoutEvidence(); err != nil {
		return err
	}
	if p.EvidenceID == "" {
		return fmt.Errorf("%w: presentation %q has no evidence id", ErrPresentationInvalid, p.ID)
	}
	if p.EvidenceID != presentationEvidencePrefix+p.canonicalDigest() {
		return fmt.Errorf("%w: presentation %q evidence id does not match its own canonical digest", ErrPresentationInvalid, p.ID)
	}
	return nil
}

// canonicalDigest hashes every field the presentation's own evidence id is
// derived from.
func (p Presentation) canonicalDigest() string {
	dst := appendFields(nil,
		"id", p.ID,
		"notice_id", p.NoticeID,
		"notice_version", p.NoticeVersion,
		"notice_digest", p.NoticeDigest,
		"principal", p.Principal,
		"locale", p.Locale,
		"accessible", strconv.FormatBool(p.Accessible),
		"presented_at", p.PresentedAt.String(),
		"acknowledged_at", p.AcknowledgedAt.String(),
	)
	return digestHex(dst)
}

// MatchesNotice reports whether the presentation is bound to exactly this
// notice's current identity, version and content digest.
func (p Presentation) MatchesNotice(n Notice) bool {
	return p.NoticeID == n.ID && p.NoticeVersion == n.Version && p.NoticeDigest == n.Digest()
}
