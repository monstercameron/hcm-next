package taxprofile

import (
	"encoding/hex"
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

type PresenceState string

const (
	PresencePresent  PresenceState = "PRESENT"
	PresenceUnknown  PresenceState = "UNKNOWN"
	PresenceRedacted PresenceState = "REDACTED"
)

func (p PresenceState) Valid() bool {
	return p == PresencePresent || p == PresenceUnknown || p == PresenceRedacted
}

var (
	ErrSnapshotInvalid  = errors.New("taxprofile: invalid pinned snapshot")
	ErrSnapshotUnsafe   = errors.New("taxprofile: snapshot contains unsafe unknown or redacted input")
	ErrSnapshotMismatch = errors.New("taxprofile: tax and payroll snapshot digests differ")
)

type TaxProfileSnapshotRequest struct {
	Profile                WorkerTaxProfileRevision
	EmploymentRef          string
	PayGroupRef            string
	EffectiveAsOf          values.Instant
	KnownAt                values.Instant
	FormReleaseDigest      string
	RuleReleaseDigest      string
	RegistrationPresence   PresenceState
	ElectionPresence       PresenceState
	ClassificationPresence PresenceState
}

type PinnedTaxInputSnapshot struct {
	WorkerRef           string
	EmploymentRef       string
	PayGroupRef         string
	ProfileRevision     uint64
	ProfileDigest       string
	RegistrationDigests []string
	ElectionDigests     []string
	FormReleaseDigest   string
	RuleReleaseDigest   string
	EffectiveAsOf       values.Instant
	KnownAt             values.Instant
	RegistrationState   PresenceState
	ElectionState       PresenceState
	ClassificationState PresenceState
	Digest              string
	seal                string
}

func validSnapshotDigest(value string) bool {
	const prefix = "sha256:"
	if len(value) != len(prefix)+64 || value[:len(prefix)] != prefix {
		return false
	}
	_, err := hex.DecodeString(value[len(prefix):])
	return err == nil
}

func BuildTaxProfileSnapshot(req TaxProfileSnapshotRequest) (PinnedTaxInputSnapshot, error) {
	if err := req.Profile.Validate(); err != nil {
		return PinnedTaxInputSnapshot{}, fmt.Errorf("%w: profile: %v", ErrSnapshotInvalid, err)
	}
	if !validSnapshotDigest(req.Profile.CanonicalDigest) || !validSnapshotDigest(req.FormReleaseDigest) || !validSnapshotDigest(req.RuleReleaseDigest) {
		return PinnedTaxInputSnapshot{}, fmt.Errorf("%w: profile, form and rule digests are required", ErrSnapshotInvalid)
	}
	if err := req.EffectiveAsOf.Validate(); err != nil {
		return PinnedTaxInputSnapshot{}, fmt.Errorf("%w: effective as-of: %v", ErrSnapshotInvalid, err)
	}
	if err := req.KnownAt.Validate(); err != nil {
		return PinnedTaxInputSnapshot{}, fmt.Errorf("%w: known-at: %v", ErrSnapshotInvalid, err)
	}
	effective, err := req.Profile.Effective.ContainsInstant(req.EffectiveAsOf)
	if err != nil || !effective {
		return PinnedTaxInputSnapshot{}, fmt.Errorf("%w: profile is not effective at snapshot instant", ErrSnapshotInvalid)
	}
	if req.KnownAt.Before(req.Profile.KnownAt) {
		return PinnedTaxInputSnapshot{}, fmt.Errorf("%w: profile was not known at snapshot cutoff", ErrSnapshotInvalid)
	}
	if req.RegistrationPresence == "" {
		req.RegistrationPresence = PresenceUnknown
	}
	if req.ElectionPresence == "" {
		req.ElectionPresence = PresenceUnknown
	}
	if req.ClassificationPresence == "" {
		req.ClassificationPresence = PresenceUnknown
	}
	if !req.RegistrationPresence.Valid() || !req.ElectionPresence.Valid() || !req.ClassificationPresence.Valid() {
		return PinnedTaxInputSnapshot{}, fmt.Errorf("%w: presence state is not declared", ErrSnapshotInvalid)
	}
	if req.EmploymentRef == "" || req.PayGroupRef == "" {
		return PinnedTaxInputSnapshot{}, fmt.Errorf("%w: employment and pay group are required", ErrSnapshotInvalid)
	}
	out := PinnedTaxInputSnapshot{WorkerRef: req.Profile.WorkerRef, EmploymentRef: req.EmploymentRef, PayGroupRef: req.PayGroupRef, ProfileRevision: req.Profile.Revision, ProfileDigest: req.Profile.CanonicalDigest, FormReleaseDigest: req.FormReleaseDigest, RuleReleaseDigest: req.RuleReleaseDigest, EffectiveAsOf: req.EffectiveAsOf, KnownAt: req.KnownAt, RegistrationState: req.RegistrationPresence, ElectionState: req.ElectionPresence, ClassificationState: req.ClassificationPresence}
	for _, r := range req.Profile.Registrations {
		applies, intervalErr := r.Effective.ContainsInstant(req.EffectiveAsOf)
		if intervalErr != nil {
			return PinnedTaxInputSnapshot{}, fmt.Errorf("%w: registration effective interval", ErrSnapshotInvalid)
		}
		if !applies || req.KnownAt.Before(r.KnownAt) {
			continue
		}
		if !validSnapshotDigest(r.CanonicalDigest) {
			return PinnedTaxInputSnapshot{}, fmt.Errorf("%w: registration digest is required", ErrSnapshotInvalid)
		}
		out.RegistrationDigests = append(out.RegistrationDigests, r.CanonicalDigest)
	}
	for _, e := range req.Profile.Elections {
		applies, intervalErr := e.Effective.ContainsInstant(req.EffectiveAsOf)
		if intervalErr != nil {
			return PinnedTaxInputSnapshot{}, fmt.Errorf("%w: election effective interval", ErrSnapshotInvalid)
		}
		if !applies || req.KnownAt.Before(e.KnownAt) {
			continue
		}
		if !validSnapshotDigest(e.CanonicalDigest) {
			return PinnedTaxInputSnapshot{}, fmt.Errorf("%w: election digest is required", ErrSnapshotInvalid)
		}
		out.ElectionDigests = append(out.ElectionDigests, e.CanonicalDigest)
	}
	sort.Strings(out.RegistrationDigests)
	sort.Strings(out.ElectionDigests)
	if out.RegistrationState == PresencePresent && len(out.RegistrationDigests) == 0 {
		return PinnedTaxInputSnapshot{}, fmt.Errorf("%w: present registration has no immutable pin", ErrSnapshotInvalid)
	}
	if out.ElectionState == PresencePresent && len(out.ElectionDigests) == 0 {
		return PinnedTaxInputSnapshot{}, fmt.Errorf("%w: present election has no immutable pin", ErrSnapshotInvalid)
	}
	out.Digest = canonicalbytes.Digest(out.canonical())
	out.seal = out.Digest
	return out, nil
}

func (s PinnedTaxInputSnapshot) canonical() []byte {
	w := canonicalbytes.New("hcmnext.domains.taxprofile.PinnedTaxInputSnapshot", schemaVersion).
		String("worker_ref", s.WorkerRef).String("employment_ref", s.EmploymentRef).String("pay_group_ref", s.PayGroupRef).
		Int("profile_revision", int64(s.ProfileRevision)).String("profile_digest", s.ProfileDigest).
		SortedStrings("registration_digest", s.RegistrationDigests).SortedStrings("election_digest", s.ElectionDigests).
		String("form_release_digest", s.FormReleaseDigest).String("rule_release_digest", s.RuleReleaseDigest).
		Value("effective_as_of", s.EffectiveAsOf).Value("known_at", s.KnownAt).
		String("registration_state", string(s.RegistrationState)).String("election_state", string(s.ElectionState))
	w.String("classification_state", string(s.ClassificationState))
	b, _ := w.Bytes()
	return b
}

func (s PinnedTaxInputSnapshot) Validate() error {
	if s.WorkerRef == "" || s.EmploymentRef == "" || s.PayGroupRef == "" || s.ProfileRevision == 0 || !validSnapshotDigest(s.ProfileDigest) || !validSnapshotDigest(s.FormReleaseDigest) || !validSnapshotDigest(s.RuleReleaseDigest) {
		return ErrSnapshotInvalid
	}
	if !s.RegistrationState.Valid() || !s.ElectionState.Valid() || !s.ClassificationState.Valid() || s.RegistrationState == "" || s.ElectionState == "" || s.ClassificationState == "" {
		return ErrSnapshotInvalid
	}
	if s.EffectiveAsOf.Validate() != nil || s.KnownAt.Validate() != nil || s.Digest == "" {
		return ErrSnapshotInvalid
	}
	for _, digest := range append(append([]string(nil), s.RegistrationDigests...), s.ElectionDigests...) {
		if !validSnapshotDigest(digest) {
			return ErrSnapshotInvalid
		}
	}
	if (s.RegistrationState == PresencePresent && len(s.RegistrationDigests) == 0) || (s.ElectionState == PresencePresent && len(s.ElectionDigests) == 0) {
		return ErrSnapshotInvalid
	}
	if s.seal == "" || s.seal != s.Digest || s.Digest != canonicalbytes.Digest(s.canonical()) {
		return ErrSnapshotInvalid
	}
	return nil
}

func (s PinnedTaxInputSnapshot) Unsafe() bool {
	return s.RegistrationState != PresencePresent || s.ElectionState != PresencePresent || s.ClassificationState != PresencePresent
}

// TaxCalculationConformance independently reconstructs the pinned digest.
func TaxCalculationConformance(s PinnedTaxInputSnapshot) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if s.Unsafe() {
		return ErrSnapshotUnsafe
	}
	w := canonicalbytes.New("hcmnext.domains.taxprofile.PinnedTaxInputSnapshot", schemaVersion).
		String("worker_ref", s.WorkerRef).String("employment_ref", s.EmploymentRef).String("pay_group_ref", s.PayGroupRef).
		Int("profile_revision", int64(s.ProfileRevision)).String("profile_digest", s.ProfileDigest).
		SortedStrings("registration_digest", append([]string(nil), s.RegistrationDigests...)).SortedStrings("election_digest", append([]string(nil), s.ElectionDigests...)).
		String("form_release_digest", s.FormReleaseDigest).String("rule_release_digest", s.RuleReleaseDigest).Value("effective_as_of", s.EffectiveAsOf).Value("known_at", s.KnownAt).
		String("registration_state", string(s.RegistrationState)).String("election_state", string(s.ElectionState))
	w.String("classification_state", string(s.ClassificationState))
	b, _ := w.Bytes()
	if canonicalbytes.Digest(b) != s.Digest {
		return ErrSnapshotMismatch
	}
	return nil
}

// PayrollTaxConformance is an independent consumer implementation, rather
// than an alias to the tax calculation check.
func PayrollTaxConformance(s PinnedTaxInputSnapshot) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if s.RegistrationState != PresencePresent || s.ElectionState != PresencePresent || s.ClassificationState != PresencePresent {
		return ErrSnapshotUnsafe
	}
	regs := append([]string(nil), s.RegistrationDigests...)
	elections := append([]string(nil), s.ElectionDigests...)
	sort.Strings(regs)
	sort.Strings(elections)
	w := canonicalbytes.New("hcmnext.domains.taxprofile.PinnedTaxInputSnapshot", schemaVersion).String("worker_ref", s.WorkerRef).String("employment_ref", s.EmploymentRef).String("pay_group_ref", s.PayGroupRef).
		Int("profile_revision", int64(s.ProfileRevision)).String("profile_digest", s.ProfileDigest).SortedStrings("registration_digest", regs).
		SortedStrings("election_digest", elections).String("form_release_digest", s.FormReleaseDigest).String("rule_release_digest", s.RuleReleaseDigest).
		Value("effective_as_of", s.EffectiveAsOf).Value("known_at", s.KnownAt).String("registration_state", string(s.RegistrationState)).String("election_state", string(s.ElectionState))
	w.String("classification_state", string(s.ClassificationState))
	b, _ := w.Bytes()
	if canonicalbytes.Digest(b) != s.Digest {
		return ErrSnapshotMismatch
	}
	return nil
}

func TaxAndPayrollConformance(s PinnedTaxInputSnapshot) error {
	if err := TaxCalculationConformance(s); err != nil {
		return err
	}
	return PayrollTaxConformance(s)
}
