// Package leave owns the governed LeaveProgram vocabulary.  A program is
// policy input: it explains eligibility and composition, but never grants
// leave or mutates workforce state.
package leave

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const schemaVersion = 1

var (
	ErrInvalidProgram    = errors.New("leave: invalid program revision")
	ErrDuplicateRevision = errors.New("leave: duplicate program revision")
	ErrRevisionOrder     = errors.New("leave: program revisions must be appended in order")
)

// ProgramKind identifies the policy sponsor without assuming a jurisdiction.
type ProgramKind string

const (
	StatutoryProgram  ProgramKind = "STATUTORY"
	CollectiveProgram ProgramKind = "COLLECTIVE"
	CompanyProgram    ProgramKind = "COMPANY"
)

// UnknownPolicy states how unresolved facts are treated by a consumer.
type UnknownPolicy string

const (
	UnknownBlocks        UnknownPolicy = "BLOCK"
	UnknownReview        UnknownPolicy = "REVIEW"
	UnknownNotApplicable UnknownPolicy = "NOT_APPLICABLE"
)

// InteractionMetadata declares composition and conflict behavior. It is
// metadata, not an algorithm: jurisdiction rule packs supply the content.
type InteractionMetadata struct {
	ConcurrencyGroup string
	Priority         int
	Stacking         bool
	OffsetAllowed    bool
	MostProtective   bool
	ConflictPolicy   string
}

func (m InteractionMetadata) validate() error {
	if strings.TrimSpace(m.ConcurrencyGroup) == "" {
		return fmt.Errorf("%w: interaction concurrency group is required", ErrInvalidProgram)
	}
	if m.Priority < 0 {
		return fmt.Errorf("%w: interaction priority cannot be negative", ErrInvalidProgram)
	}
	if strings.TrimSpace(m.ConflictPolicy) == "" {
		return fmt.Errorf("%w: interaction conflict policy is required", ErrInvalidProgram)
	}
	return nil
}

// LeaveProgramRevision is a complete, source-attributed immutable policy
// revision. Slices are copied when constructed and when read from a catalog.
type LeaveProgramRevision struct {
	ProgramID            string
	Revision             uint64
	Name                 string
	Kind                 ProgramKind
	Sponsor              string
	Authority            evidence.SourceAuthority
	Jurisdiction         legal.Jurisdiction
	Scope                []string
	EligibilityRuleRefs  []string
	Effective            values.EffectiveInterval
	Protected            bool
	Paid                 bool
	PayTreatment         string
	EvidenceRequirements []string
	NoticeRequirements   []string
	Interaction          InteractionMetadata
	BalanceSource        string
	ReturnObligations    []string
	UnknownPolicy        UnknownPolicy
	Digest               string
}

func (p LeaveProgramRevision) Validate() error {
	if strings.TrimSpace(p.ProgramID) == "" || p.Revision == 0 || strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Sponsor) == "" {
		return fmt.Errorf("%w: program id, revision, name and sponsor are required", ErrInvalidProgram)
	}
	if p.Kind != StatutoryProgram && p.Kind != CollectiveProgram && p.Kind != CompanyProgram {
		return fmt.Errorf("%w: unknown program kind %q", ErrInvalidProgram, p.Kind)
	}
	if err := p.Authority.Validate(); err != nil {
		return fmt.Errorf("%w: authority: %v", ErrInvalidProgram, err)
	}
	if err := p.Jurisdiction.Validate(); err != nil {
		return fmt.Errorf("%w: jurisdiction: %v", ErrInvalidProgram, err)
	}
	if len(p.Scope) == 0 || len(p.EligibilityRuleRefs) == 0 {
		return fmt.Errorf("%w: scope and eligibility rules are required", ErrInvalidProgram)
	}
	if err := validateRefs("scope", p.Scope); err != nil {
		return err
	}
	if err := validateRefs("eligibility rules", p.EligibilityRuleRefs); err != nil {
		return err
	}
	if err := p.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective interval: %v", ErrInvalidProgram, err)
	}
	if p.Paid && strings.TrimSpace(p.PayTreatment) == "" {
		return fmt.Errorf("%w: paid program requires pay treatment", ErrInvalidProgram)
	}
	if len(p.EvidenceRequirements) == 0 || len(p.NoticeRequirements) == 0 {
		return fmt.Errorf("%w: evidence and notice requirements are required", ErrInvalidProgram)
	}
	if err := validateRefs("evidence requirements", p.EvidenceRequirements); err != nil {
		return err
	}
	if err := validateRefs("notice requirements", p.NoticeRequirements); err != nil {
		return err
	}
	if err := p.Interaction.validate(); err != nil {
		return err
	}
	if strings.TrimSpace(p.BalanceSource) == "" || len(p.ReturnObligations) == 0 {
		return fmt.Errorf("%w: balance source and return obligations are required", ErrInvalidProgram)
	}
	if err := validateRefs("return obligations", p.ReturnObligations); err != nil {
		return err
	}
	if p.UnknownPolicy != UnknownBlocks && p.UnknownPolicy != UnknownReview && p.UnknownPolicy != UnknownNotApplicable {
		return fmt.Errorf("%w: unknown policy %q", ErrInvalidProgram, p.UnknownPolicy)
	}
	return nil
}

// validateRefs rejects blank and duplicate identifiers. These fields are
// semantic sets: accepting duplicate/blank members would make two apparently
// equivalent rule-pack revisions canonicalize differently or hide a missing
// policy input.
func validateRefs(name string, refs []string) error {
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		if strings.TrimSpace(ref) == "" {
			return fmt.Errorf("%w: %s contains a blank reference", ErrInvalidProgram, name)
		}
		if _, ok := seen[ref]; ok {
			return fmt.Errorf("%w: %s contains duplicate reference %q", ErrInvalidProgram, name, ref)
		}
		seen[ref] = struct{}{}
	}
	return nil
}

func copyStrings(in []string) []string { return append([]string(nil), in...) }
func (p LeaveProgramRevision) copy() LeaveProgramRevision {
	p.Scope = copyStrings(p.Scope)
	p.EligibilityRuleRefs = copyStrings(p.EligibilityRuleRefs)
	p.EvidenceRequirements = copyStrings(p.EvidenceRequirements)
	p.NoticeRequirements = copyStrings(p.NoticeRequirements)
	p.ReturnObligations = copyStrings(p.ReturnObligations)
	return p
}

// NewProgramRevision validates and mints the digest of a revision.
func NewProgramRevision(p LeaveProgramRevision) (LeaveProgramRevision, error) {
	p = p.copy()
	p.Digest = ""
	if err := p.Validate(); err != nil {
		return LeaveProgramRevision{}, err
	}
	raw, err := p.canonical(false)
	if err != nil {
		return LeaveProgramRevision{}, err
	}
	p.Digest = canonicalbytes.Digest(raw)
	return p, nil
}
func (p LeaveProgramRevision) canonical(includeDigest bool) ([]byte, error) {
	w := canonicalbytes.New("hcmnext.domains.leave.LeaveProgramRevision", schemaVersion).
		String("program_id", p.ProgramID).Int("revision", int64(p.Revision)).String("name", p.Name).String("kind", string(p.Kind)).String("sponsor", p.Sponsor).
		Value("authority", p.Authority).String("jurisdiction", p.Jurisdiction.String()).SortedStrings("scope", p.Scope).SortedStrings("eligibility_rules", p.EligibilityRuleRefs).Value("effective", p.Effective).
		Bool("protected", p.Protected).Bool("paid", p.Paid).String("pay_treatment", p.PayTreatment).SortedStrings("evidence", p.EvidenceRequirements).SortedStrings("notices", p.NoticeRequirements).
		String("concurrency_group", p.Interaction.ConcurrencyGroup).Int("priority", int64(p.Interaction.Priority)).Bool("stacking", p.Interaction.Stacking).Bool("offset_allowed", p.Interaction.OffsetAllowed).Bool("most_protective", p.Interaction.MostProtective).String("conflict_policy", p.Interaction.ConflictPolicy).
		String("balance_source", p.BalanceSource).SortedStrings("return_obligations", p.ReturnObligations).String("unknown_policy", string(p.UnknownPolicy))
	if includeDigest {
		w = w.String("digest", p.Digest)
	}
	return w.Bytes()
}
func (p LeaveProgramRevision) Canonical() []byte {
	if p.Validate() != nil || p.Digest == "" {
		return nil
	}
	raw, err := p.canonical(true)
	if err != nil {
		return nil
	}
	return raw
}
func (p LeaveProgramRevision) Verify() bool {
	if p.Digest == "" {
		return false
	}
	raw, err := p.canonical(false)
	return err == nil && canonicalbytes.Digest(raw) == p.Digest
}

// ProgramCatalog is an append-only, concurrency-safe set of program streams.
type ProgramCatalog struct {
	mu      sync.RWMutex
	streams map[string][]LeaveProgramRevision
}

func NewProgramCatalog() *ProgramCatalog {
	return &ProgramCatalog{streams: make(map[string][]LeaveProgramRevision)}
}
func (c *ProgramCatalog) Append(p LeaveProgramRevision) error {
	if c == nil {
		return ErrInvalidProgram
	}
	minted, err := NewProgramRevision(p)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	prior := c.streams[minted.ProgramID]
	if len(prior) > 0 {
		if minted.Revision == prior[len(prior)-1].Revision {
			return fmt.Errorf("%w: %w", ErrDuplicateRevision, ErrRevisionOrder)
		}
		if minted.Revision != prior[len(prior)-1].Revision+1 {
			return ErrRevisionOrder
		}
	}
	c.streams[minted.ProgramID] = append(prior, minted)
	return nil
}
func (c *ProgramCatalog) Revisions(id string) []LeaveProgramRevision {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := append([]LeaveProgramRevision(nil), c.streams[id]...)
	for i := range out {
		out[i] = out[i].copy()
	}
	return out
}
func (c *ProgramCatalog) Current(id string) (LeaveProgramRevision, bool) {
	rs := c.Revisions(id)
	if len(rs) == 0 {
		return LeaveProgramRevision{}, false
	}
	return rs[len(rs)-1], true
}
func (p LeaveProgramRevision) SortedRefs() LeaveProgramRevision {
	p = p.copy()
	sort.Strings(p.Scope)
	sort.Strings(p.EligibilityRuleRefs)
	sort.Strings(p.EvidenceRequirements)
	sort.Strings(p.NoticeRequirements)
	sort.Strings(p.ReturnObligations)
	return p
}
