package timeauth

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrInvalidQuorumProfile = errors.New("timeauth: invalid quorum profile")
	ErrQuorumUntrusted      = ErrTimeUntrusted
)

// QuorumProfile is the decision record for an authenticated trusted-time
// deployment. Source names and numeric limits are intentionally supplied by a
// human/operator; this package never selects a vendor or region.
type QuorumProfile struct {
	ID                    string
	RequiredSources       int
	MaxOffset             time.Duration
	MaxUncertainty        time.Duration
	MaxHoldover           time.Duration
	LeapSmear             string
	RequireAuthentication bool
	Digest                string
}

type QuorumSource struct {
	ID            string
	Epoch         values.Instant
	Offset        time.Duration
	Uncertainty   time.Duration
	Authenticated bool
	Health        Health
	LeapSmear     string
	Holdover      bool
	HoldoverAge   time.Duration
}

type QuorumReceipt struct {
	ProfileID     string
	ProfileDigest string
	SourceIDs     []string
	Epoch         values.Instant
	Offset        time.Duration
	Skew          time.Duration
	Uncertainty   time.Duration
	Holdover      bool
	Health        Health
	Reason        string
	Digest        string
}

func NewQuorumProfile(profile QuorumProfile) (QuorumProfile, error) {
	if strings.TrimSpace(profile.ID) == "" || profile.RequiredSources < 2 || profile.MaxOffset <= 0 || profile.MaxUncertainty <= 0 || profile.MaxHoldover < 0 || strings.TrimSpace(profile.LeapSmear) == "" || !profile.RequireAuthentication {
		return QuorumProfile{}, ErrInvalidQuorumProfile
	}
	profile.Digest = digestQuorum(profile)
	return profile, nil
}

func (p QuorumProfile) Validate() error {
	if strings.TrimSpace(p.ID) == "" || p.RequiredSources < 2 || p.MaxOffset <= 0 || p.MaxUncertainty <= 0 || p.MaxHoldover < 0 || strings.TrimSpace(p.LeapSmear) == "" || !p.RequireAuthentication || p.Digest == "" {
		return ErrInvalidQuorumProfile
	}
	if digestQuorum(p) != p.Digest {
		return ErrInvalidQuorumProfile
	}
	return nil
}

// Evaluate accepts observations only; it never contacts a time provider.
// A receipt is trusted only when the authenticated quorum agrees within the
// configured offset, uncertainty, leap-smear and holdover bounds.
func (p QuorumProfile) Evaluate(sources []QuorumSource) (QuorumReceipt, error) {
	if err := p.Validate(); err != nil {
		return QuorumReceipt{}, err
	}
	if len(sources) < p.RequiredSources {
		return QuorumReceipt{}, fmt.Errorf("%w: quorum has %d sources, requires %d", ErrQuorumUntrusted, len(sources), p.RequiredSources)
	}
	byID := make(map[string]QuorumSource, len(sources))
	for _, source := range sources {
		if strings.TrimSpace(source.ID) == "" || !source.Epoch.IsSet() || !source.Health.Valid() || source.Uncertainty < 0 || source.HoldoverAge < 0 || source.LeapSmear != p.LeapSmear || (p.RequireAuthentication && !source.Authenticated) || source.Health != HealthTrusted || source.Uncertainty > p.MaxUncertainty || (source.Holdover && source.HoldoverAge > p.MaxHoldover) {
			return QuorumReceipt{}, fmt.Errorf("%w: source %q failed authentication, health, uncertainty or holdover policy", ErrQuorumUntrusted, source.ID)
		}
		if _, exists := byID[source.ID]; exists {
			return QuorumReceipt{}, fmt.Errorf("%w: duplicate source %q", ErrQuorumUntrusted, source.ID)
		}
		byID[source.ID] = source
	}
	ordered := append([]QuorumSource(nil), sources...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	minOffset, maxOffset := ordered[0].Offset, ordered[0].Offset
	minEpoch, maxEpoch := ordered[0].Epoch.Time(), ordered[0].Epoch.Time()
	uncertainty := ordered[0].Uncertainty
	holdover := ordered[0].Holdover
	for _, source := range ordered[1:] {
		if source.Offset < minOffset {
			minOffset = source.Offset
		}
		if source.Offset > maxOffset {
			maxOffset = source.Offset
		}
		if source.Epoch.Time().Before(minEpoch) {
			minEpoch = source.Epoch.Time()
		}
		if source.Epoch.Time().After(maxEpoch) {
			maxEpoch = source.Epoch.Time()
		}
		if source.Uncertainty > uncertainty {
			uncertainty = source.Uncertainty
		}
		holdover = holdover || source.Holdover
	}
	if maxOffset-minOffset > p.MaxOffset || maxEpoch.Sub(minEpoch) > p.MaxOffset {
		return QuorumReceipt{}, fmt.Errorf("%w: quorum disagreement exceeds profile", ErrQuorumUntrusted)
	}
	receipt := QuorumReceipt{ProfileID: p.ID, ProfileDigest: p.Digest, Uncertainty: uncertainty, Holdover: holdover, Health: HealthTrusted, Skew: maxOffset - minOffset}
	for _, source := range ordered {
		receipt.SourceIDs = append(receipt.SourceIDs, source.ID)
	}
	receipt.Epoch = values.NewInstant(maxEpoch.Add(minEpoch.Sub(maxEpoch) / 2))
	receipt.Offset = (minOffset + maxOffset) / 2
	receipt.Reason = "authenticated quorum agrees within configured bounds"
	receipt.Digest = digestReceipt(receipt)
	return receipt, nil
}

func (r QuorumReceipt) RequireSensitive() error {
	if r.Health != HealthTrusted || r.Digest == "" || digestReceipt(r) != r.Digest {
		return ErrQuorumUntrusted
	}
	return nil
}

func (r QuorumReceipt) Explain() string {
	return fmt.Sprintf("trusted-time profile=%s sources=%d health=%s skew=%s uncertainty=%s holdover=%t digest=%s", r.ProfileID, len(r.SourceIDs), r.Health, r.Skew, r.Uncertainty, r.Holdover, r.Digest)
}
func Explain(r QuorumReceipt) string { return r.Explain() }

func digestQuorum(p QuorumProfile) string {
	return hashString(fmt.Sprintf("v1|%s|%d|%s|%s|%s|%s|%t", p.ID, p.RequiredSources, p.MaxOffset, p.MaxUncertainty, p.MaxHoldover, p.LeapSmear, p.RequireAuthentication))
}
func digestReceipt(r QuorumReceipt) string {
	return hashString(fmt.Sprintf("v1|%s|%s|%s|%s|%s|%t|%s", r.ProfileID, r.ProfileDigest, r.Epoch, r.Offset, r.Skew, r.Holdover, strings.Join(r.SourceIDs, ",")))
}
func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(sum[:])
}
