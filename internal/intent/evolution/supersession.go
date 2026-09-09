package evolution

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// supersessionDigestMagic namespaces the supersession digest so it is never
// mistaken for a digest of the same bytes produced by a different encoding
// generation.
const supersessionDigestMagic = "hcmnext.intent.evolution.supersession_digest.v1"

// LiveInstancePolicy is what a [SupersessionRecord] declares should happen
// to instances already live under the superseded version.
type LiveInstancePolicy string

// LiveInstancePolicy values.
const (
	// PolicyContinueOnOld lets a live instance run to completion under the
	// definition version it started under. It is a legal choice only when
	// the successor's change does not alter what the live instance already
	// committed to; that judgment belongs to whoever records the
	// supersession, not to this package.
	PolicyContinueOnOld LiveInstancePolicy = "CONTINUE_ON_OLD"

	// PolicyMigrateWithPreview requires a live instance to be re-bound to
	// the successor only after a reviewed migration preview — the same
	// live-instance compatibility shape internal/workflow/migrationpreview
	// computes for compiled workflow versions.
	PolicyMigrateWithPreview LiveInstancePolicy = "MIGRATE_WITH_PREVIEW"

	// PolicyDrain refuses new work under the superseded version and lets
	// existing live instances finish or be explicitly cancelled; none may
	// be silently rebound to the successor.
	PolicyDrain LiveInstancePolicy = "DRAIN"
)

// Valid reports whether p is one of the three declared live-instance
// policies.
func (p LiveInstancePolicy) Valid() bool {
	switch p {
	case PolicyContinueOnOld, PolicyMigrateWithPreview, PolicyDrain:
		return true
	default:
		return false
	}
}

// SupersessionRecord is the immutable governance artifact that lets a new
// definition version replace an old one when [CompatibilityCheck] found the
// change INCOMPATIBLE.
//
// It is never edited: a mistake in a recorded reason, approver or policy is
// corrected by recording a further, later supersession — exactly as an
// intent.RelationshipRevision is corrected by a new revision rather than a
// rewrite. [SupersessionRecord.Verify] proves, by digest, that a value in
// hand is exactly what [NewSupersessionRecord] produced, so a caller never
// has to take a copy's good faith on trust.
type SupersessionRecord struct {
	Previous intent.Ref `json:"previous"`
	Current  intent.Ref `json:"current"`

	// Reason is the recorded business or governance reason the successor
	// replaces the previous version. It is never inferred from the
	// compatibility diff.
	Reason string `json:"reason"`

	// AuthorPrincipalID is who authored the successor definition.
	AuthorPrincipalID string `json:"author_principal_id"`

	// ApproverPrincipalID is who approved the supersession. It must differ
	// from AuthorPrincipalID: the person who wrote the change is never the
	// only signature on retiring what it replaces.
	ApproverPrincipalID string `json:"approver_principal_id"`

	// EffectiveAt is the instant the supersession takes effect.
	EffectiveAt values.Instant `json:"effective_at"`

	// LiveInstancePolicy governs what happens to instances already live
	// under Previous.
	LiveInstancePolicy LiveInstancePolicy `json:"live_instance_policy"`

	// CompatibilityVerdict is the verdict of the [Report] this supersession
	// was recorded over. [NewSupersessionRecord] only ever records
	// VerdictIncompatible here; the field is carried on the record itself so
	// a reader never has to fetch the original report to know why a
	// supersession — rather than a direct binding — was necessary.
	CompatibilityVerdict Verdict `json:"compatibility_verdict"`

	// Digest is the content digest of every field above, computed once at
	// construction. [Verify] recomputes it from the fields in hand and
	// compares.
	Digest string `json:"digest"`
}

// supersessionProjection is every field of SupersessionRecord except Digest
// itself, in a fixed declaration order, so the digest is computed over
// exactly the content it protects and never over itself.
type supersessionProjection struct {
	Previous             intent.Ref         `json:"previous"`
	Current              intent.Ref         `json:"current"`
	Reason               string             `json:"reason"`
	AuthorPrincipalID    string             `json:"author_principal_id"`
	ApproverPrincipalID  string             `json:"approver_principal_id"`
	EffectiveAt          values.Instant     `json:"effective_at"`
	LiveInstancePolicy   LiveInstancePolicy `json:"live_instance_policy"`
	CompatibilityVerdict Verdict            `json:"compatibility_verdict"`
}

func (s SupersessionRecord) contentDigest() (string, error) {
	encoded, err := json.Marshal(supersessionProjection{
		Previous:             s.Previous,
		Current:              s.Current,
		Reason:               s.Reason,
		AuthorPrincipalID:    s.AuthorPrincipalID,
		ApproverPrincipalID:  s.ApproverPrincipalID,
		EffectiveAt:          s.EffectiveAt,
		LiveInstancePolicy:   s.LiveInstancePolicy,
		CompatibilityVerdict: s.CompatibilityVerdict,
	})
	if err != nil {
		return "", fmt.Errorf("evolution: encode supersession %s -> %s for digest: %w",
			s.Previous, s.Current, err)
	}
	sum := sha256.Sum256(append([]byte(supersessionDigestMagic), encoded...))
	return hex.EncodeToString(sum[:]), nil
}

// NewSupersessionRecord validates and mints one immutable supersession
// record over a compatibility report.
//
// It refuses: a report whose verdict is COMPATIBLE (a compatible successor
// never needs a supersession — it binds live instances directly), an empty
// reason, author or approver, an unset effective instant, an approver equal
// to the author, and an unrecognized live-instance policy.
func NewSupersessionRecord(
	report Report,
	reason string,
	authorPrincipalID string,
	approverPrincipalID string,
	effectiveAt values.Instant,
	policy LiveInstancePolicy,
) (SupersessionRecord, error) {
	if report.Verdict != VerdictIncompatible {
		return SupersessionRecord{}, fmt.Errorf("%w: %s -> %s is %s",
			ErrSupersessionRequiresIncompatibility, report.Previous, report.Current, report.Verdict)
	}
	if reason == "" {
		return SupersessionRecord{}, fmt.Errorf("%w: reason is empty", ErrInvalidSupersession)
	}
	if authorPrincipalID == "" {
		return SupersessionRecord{}, fmt.Errorf("%w: author_principal_id is empty", ErrInvalidSupersession)
	}
	if approverPrincipalID == "" {
		return SupersessionRecord{}, fmt.Errorf("%w: approver_principal_id is empty", ErrInvalidSupersession)
	}
	if approverPrincipalID == authorPrincipalID {
		return SupersessionRecord{}, fmt.Errorf("%w: %q authored and approved %s -> %s",
			ErrApproverIsAuthor, authorPrincipalID, report.Previous, report.Current)
	}
	if !effectiveAt.IsSet() {
		return SupersessionRecord{}, fmt.Errorf("%w: effective_at is unset", ErrInvalidSupersession)
	}
	if !policy.Valid() {
		return SupersessionRecord{}, fmt.Errorf("%w: %q is not a live-instance policy", ErrInvalidSupersession, policy)
	}

	rec := SupersessionRecord{
		Previous:             report.Previous,
		Current:              report.Current,
		Reason:               reason,
		AuthorPrincipalID:    authorPrincipalID,
		ApproverPrincipalID:  approverPrincipalID,
		EffectiveAt:          effectiveAt,
		LiveInstancePolicy:   policy,
		CompatibilityVerdict: report.Verdict,
	}
	digest, err := rec.contentDigest()
	if err != nil {
		return SupersessionRecord{}, err
	}
	rec.Digest = digest
	return rec, nil
}

// Verify recomputes the record's content digest and compares it against the
// recorded Digest. A mismatch means the record was mutated after
// construction — proof, not assumption, that a value in hand stayed
// immutable.
func (s SupersessionRecord) Verify() error {
	got, err := s.contentDigest()
	if err != nil {
		return err
	}
	if got != s.Digest {
		return fmt.Errorf("%w: %s -> %s recorded %s, recomputed %s",
			ErrSupersessionTampered, s.Previous, s.Current, s.Digest, got)
	}
	return nil
}
