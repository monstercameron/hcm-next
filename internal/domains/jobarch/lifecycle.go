package jobarch

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/governance/decision"
)

var (
	ErrPublicationNotApproved = errors.New("jobarch: publication is not approved")
	ErrCompatibilityRequired  = errors.New("jobarch: compatibility report is required")
	ErrConfigurationRequired  = errors.New("jobarch: configuration approval binding is required")
	ErrImpactIncomplete       = errors.New("jobarch: impact analysis is incomplete")
	ErrImpactRequired         = errors.New("jobarch: retirement requires a frozen impact set")
)

// CompatibilityReport is the immutable compatibility evidence bound to a
// publication. Diagnostics are identifiers and explanations, never worker or
// compensation values.
type CompatibilityReport struct {
	RevisionDigest string
	Compatible     bool
	Breaking       bool
	Diagnostics    []CompatibilityDiagnostic
	Digest         string
}

func (r CompatibilityReport) Validate(expectedDigest string) error {
	if strings.TrimSpace(r.RevisionDigest) == "" || r.RevisionDigest != expectedDigest {
		return fmt.Errorf("%w: revision digest does not match candidate", ErrCompatibilityRequired)
	}
	if !r.Compatible {
		return fmt.Errorf("%w: report is incompatible", ErrCompatibilityRequired)
	}
	return nil
}

func (r CompatibilityReport) Canonical() []byte {
	w := canonicalbytes.New("hcmnext.domains.jobarch.CompatibilityReport", 1).
		String("revision_digest", r.RevisionDigest).Bool("compatible", r.Compatible).Bool("breaking", r.Breaking)
	diagnostics := append([]CompatibilityDiagnostic(nil), r.Diagnostics...)
	sort.Slice(diagnostics, func(i, j int) bool {
		return diagnostics[i].Code+diagnostics[i].Detail < diagnostics[j].Code+diagnostics[j].Detail
	})
	for _, d := range diagnostics {
		w.String("diagnostic", d.Code+"\x00"+d.Detail)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (r CompatibilityReport) CanonicalDigest() string {
	b := r.Canonical()
	if b == nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

// PublicationRequest binds a candidate revision to a governance decision,
// configuration snapshot and reference-data snapshot.
type PublicationRequest struct {
	Candidate           ArchitectureRevision
	ProfileID           string
	Approval            decision.Decision
	Compatibility       CompatibilityReport
	ConfigurationDigest string
	References          ReferenceCatalog
	EffectiveAt         time.Time
}

// PublicationReceipt records the evidence used to publish an immutable
// successor. Existing assignments are not rewritten by this operation.
type PublicationReceipt struct {
	ArchitectureID       string
	ProfileID            string
	PreviousRevision     string
	PublishedRevision    string
	ApprovalDigest       string
	CompatibilityDigest  string
	ConfigurationDigest  string
	ReferenceCatalogHash string
}

// PublishProfileRevision publishes one draft profile as a new immutable
// architecture successor after approval and complete reference resolution.
func (a ArchitectureRevision) PublishProfileRevision(req PublicationRequest) (ArchitectureRevision, PublicationReceipt, error) {
	if err := a.Validate(); err != nil {
		return ArchitectureRevision{}, PublicationReceipt{}, err
	}
	candidate := req.Candidate
	if candidate.ID == "" {
		candidate = a
	}
	if err := candidate.Validate(); err != nil {
		return ArchitectureRevision{}, PublicationReceipt{}, err
	}
	if candidate.ID != a.ID || candidate.SupersedesRevision != "" && candidate.SupersedesRevision != a.Revision {
		return ArchitectureRevision{}, PublicationReceipt{}, fmt.Errorf("%w: candidate is not the successor of current architecture", ErrPublicationNotApproved)
	}
	profileID := req.ProfileID
	if profileID == "" && len(candidate.Profiles) == 1 {
		profileID = candidate.Profiles[0].ProfileIDOrID()
	}
	profileIndex := profileIndex(candidate.Profiles, profileID)
	if profileIndex < 0 {
		return ArchitectureRevision{}, PublicationReceipt{}, fmt.Errorf("%w: profile %q", ErrImpactIncomplete, profileID)
	}
	profile := candidate.Profiles[profileIndex]
	if profile.Lifecycle != LifecycleDraft {
		return ArchitectureRevision{}, PublicationReceipt{}, fmt.Errorf("%w: profile %s is not DRAFT", ErrPublicationNotApproved, profileID)
	}
	if strings.TrimSpace(req.ConfigurationDigest) == "" {
		return ArchitectureRevision{}, PublicationReceipt{}, ErrConfigurationRequired
	}
	if req.Approval.State != decision.Allow && req.Approval.State != decision.AllowWithObligations {
		return ArchitectureRevision{}, PublicationReceipt{}, ErrPublicationNotApproved
	}
	if req.Approval.ProposalRevisionDigest != candidate.CanonicalDigest {
		return ArchitectureRevision{}, PublicationReceipt{}, fmt.Errorf("%w: approval is bound to another revision", ErrPublicationNotApproved)
	}
	if err := req.Compatibility.Validate(candidate.CanonicalDigest); err != nil {
		return ArchitectureRevision{}, PublicationReceipt{}, err
	}
	if err := profile.ValidateGoverned(req.References, req.EffectiveAt); err != nil {
		return ArchitectureRevision{}, PublicationReceipt{}, err
	}

	next := cloneArchitecture(candidate)
	next.Revision = nextRevision(a.Revision)
	next.SupersedesRevision = a.Revision
	next.Profiles = cloneProfiles(candidate.Profiles)
	next.Profiles[profileIndex].Lifecycle = LifecyclePublished
	next.Profiles[profileIndex].Revision = profile.Revision + ".published"
	next.Profiles[profileIndex].Lineage = RevisionLineage{RootID: profile.ProfileIDOrID(), Supersedes: profile.Revision}
	result, err := a.Revise(next)
	if err != nil {
		return ArchitectureRevision{}, PublicationReceipt{}, err
	}
	receipt := PublicationReceipt{ArchitectureID: a.ID, ProfileID: profileID, PreviousRevision: profile.Revision, PublishedRevision: result.Profiles[profileIndex].Revision, ApprovalDigest: req.Approval.CanonicalDigest(), CompatibilityDigest: req.Compatibility.CanonicalDigest(), ConfigurationDigest: req.ConfigurationDigest, ReferenceCatalogHash: req.References.CanonicalDigest()}
	return result, receipt, nil
}

// Publish is the concise method spelling used by workflow adapters.
func (a ArchitectureRevision) Publish(req PublicationRequest) (ArchitectureRevision, PublicationReceipt, error) {
	return a.PublishProfileRevision(req)
}

// PublishRevision is the package-level spelling for callers that keep the
// current architecture and request separate.
func PublishRevision(current ArchitectureRevision, req PublicationRequest) (ArchitectureRevision, PublicationReceipt, error) {
	return current.PublishProfileRevision(req)
}

func profileIndex(profiles []JobProfileRevision, id string) int {
	for i, p := range profiles {
		if p.ProfileIDOrID() == id {
			return i
		}
	}
	return -1
}

// ImpactKind names every governed dependency class that a job revision can
// affect. Keeping these explicit prevents compensation, qualification,
// access, or talent effects from disappearing into an untyped list.
type ImpactKind string

const (
	ImpactPosition      ImpactKind = "POSITION"
	ImpactWorker        ImpactKind = "WORKER"
	ImpactRequisition   ImpactKind = "REQUISITION"
	ImpactCompensation  ImpactKind = "COMPENSATION"
	ImpactQualification ImpactKind = "QUALIFICATION"
	ImpactAccess        ImpactKind = "ACCESS"
	ImpactTalent        ImpactKind = "TALENT"
)

// ImpactSubject preserves the historical job revision held by an existing
// assignment or dependency. It is never rewritten by AnalyzeImpact.
type ImpactSubject struct {
	ID                 string
	ProfileID          string
	HistoricalRevision string
	WorkerID           string
	PositionID         string
	RequisitionID      string
}

// ImpactDependency is a scoped dependency reference, not the dependency's
// protected business data.
type ImpactDependency struct {
	Kind         ImpactKind
	ID, Revision string
}

// ImpactInput is supplied by an authority adapter. The domain freezes the
// detached values and creates review/migration intents from them.
type ImpactInput struct {
	Positions      []ImpactSubject
	Workers        []ImpactSubject
	Requisitions   []ImpactSubject
	Compensation   []ImpactDependency
	Qualifications []ImpactDependency
	Access         []ImpactDependency
	Talent         []ImpactDependency
}

type ImpactIntent struct {
	ID        string
	Kind      string
	Scope     ImpactKind
	SubjectID string
	ProfileID string
}

// ImpactAnalysis is frozen evidence for a publication or retirement review.
// The intent lists tell consumers what to review or migrate; they do not
// perform either mutation.
type ImpactAnalysis struct {
	ProfileID        string
	FromRevision     string
	ToRevision       string
	Positions        []ImpactSubject
	Workers          []ImpactSubject
	Requisitions     []ImpactSubject
	Dependencies     []ImpactDependency
	ReviewIntents    []ImpactIntent
	MigrationIntents []ImpactIntent
	Frozen           bool
	CanonicalDigest  string
}

func cloneSubjects(in []ImpactSubject) []ImpactSubject { return append([]ImpactSubject(nil), in...) }
func cloneDependencies(in []ImpactDependency) []ImpactDependency {
	return append([]ImpactDependency(nil), in...)
}

// AnalyzeImpact freezes all supplied dependency classes and emits a review and
// migration intent for each scoped subject. HistoricalRevision is retained.
func AnalyzeImpact(profileID, fromRevision, toRevision string, in ImpactInput) (ImpactAnalysis, error) {
	if strings.TrimSpace(profileID) == "" || strings.TrimSpace(fromRevision) == "" || strings.TrimSpace(toRevision) == "" || fromRevision == toRevision {
		return ImpactAnalysis{}, ErrImpactIncomplete
	}
	out := ImpactAnalysis{ProfileID: profileID, FromRevision: fromRevision, ToRevision: toRevision, Positions: cloneSubjects(in.Positions), Workers: cloneSubjects(in.Workers), Requisitions: cloneSubjects(in.Requisitions), Frozen: true}
	out.Dependencies = append(out.Dependencies, in.Compensation...)
	out.Dependencies = append(out.Dependencies, in.Qualifications...)
	out.Dependencies = append(out.Dependencies, in.Access...)
	out.Dependencies = append(out.Dependencies, in.Talent...)
	for i := range out.Positions {
		if err := validateSubject(out.Positions[i], ImpactPosition); err != nil {
			return ImpactAnalysis{}, err
		}
		out.addIntents(ImpactPosition, out.Positions[i])
	}
	for i := range out.Workers {
		if err := validateSubject(out.Workers[i], ImpactWorker); err != nil {
			return ImpactAnalysis{}, err
		}
		out.addIntents(ImpactWorker, out.Workers[i])
	}
	for i := range out.Requisitions {
		if err := validateSubject(out.Requisitions[i], ImpactRequisition); err != nil {
			return ImpactAnalysis{}, err
		}
		out.addIntents(ImpactRequisition, out.Requisitions[i])
	}
	for _, d := range out.Dependencies {
		if d.Kind != ImpactCompensation && d.Kind != ImpactQualification && d.Kind != ImpactAccess && d.Kind != ImpactTalent || strings.TrimSpace(d.ID) == "" || strings.TrimSpace(d.Revision) == "" {
			return ImpactAnalysis{}, ErrImpactIncomplete
		}
		out.addDependencyIntents(d)
	}
	sort.Slice(out.Dependencies, func(i, j int) bool {
		return string(out.Dependencies[i].Kind)+out.Dependencies[i].ID+out.Dependencies[i].Revision < string(out.Dependencies[j].Kind)+out.Dependencies[j].ID+out.Dependencies[j].Revision
	})
	sort.Slice(out.ReviewIntents, func(i, j int) bool { return out.ReviewIntents[i].ID < out.ReviewIntents[j].ID })
	sort.Slice(out.MigrationIntents, func(i, j int) bool { return out.MigrationIntents[i].ID < out.MigrationIntents[j].ID })
	out.CanonicalDigest = canonicalbytes.Digest(out.canonical())
	return out, nil
}

func validateSubject(s ImpactSubject, kind ImpactKind) error {
	if strings.TrimSpace(s.ID) == "" || strings.TrimSpace(s.ProfileID) == "" || strings.TrimSpace(s.HistoricalRevision) == "" {
		return fmt.Errorf("%w: %s subject requires id, profile and historical revision", ErrImpactIncomplete, kind)
	}
	return nil
}
func (a *ImpactAnalysis) addIntents(kind ImpactKind, s ImpactSubject) {
	a.ReviewIntents = append(a.ReviewIntents, ImpactIntent{ID: "review:" + strings.ToLower(string(kind)) + ":" + s.ID, Kind: "REVIEW", Scope: kind, SubjectID: s.ID, ProfileID: s.ProfileID})
	a.MigrationIntents = append(a.MigrationIntents, ImpactIntent{ID: "migrate:" + strings.ToLower(string(kind)) + ":" + s.ID, Kind: "MIGRATION", Scope: kind, SubjectID: s.ID, ProfileID: s.ProfileID})
}
func (a *ImpactAnalysis) addDependencyIntents(d ImpactDependency) {
	a.ReviewIntents = append(a.ReviewIntents, ImpactIntent{ID: "review:" + strings.ToLower(string(d.Kind)) + ":" + d.ID, Kind: "REVIEW", Scope: d.Kind, SubjectID: d.ID, ProfileID: a.ProfileID})
	a.MigrationIntents = append(a.MigrationIntents, ImpactIntent{ID: "migrate:" + strings.ToLower(string(d.Kind)) + ":" + d.ID, Kind: "MIGRATION", Scope: d.Kind, SubjectID: d.ID, ProfileID: a.ProfileID})
}

func (a ImpactAnalysis) canonical() []byte {
	w := canonicalbytes.New("hcmnext.domains.jobarch.ImpactAnalysis", 1).String("profile_id", a.ProfileID).String("from_revision", a.FromRevision).String("to_revision", a.ToRevision).Bool("frozen", a.Frozen)
	for _, s := range a.Positions {
		w.String("position", s.ID+"\x00"+s.ProfileID+"\x00"+s.HistoricalRevision)
	}
	for _, s := range a.Workers {
		w.String("worker", s.ID+"\x00"+s.ProfileID+"\x00"+s.HistoricalRevision)
	}
	for _, s := range a.Requisitions {
		w.String("requisition", s.ID+"\x00"+s.ProfileID+"\x00"+s.HistoricalRevision)
	}
	for _, d := range a.Dependencies {
		w.String("dependency", string(d.Kind)+"\x00"+d.ID+"\x00"+d.Revision)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

// RetireRevision binds retirement to approval and a previously frozen impact
// set. Existing references remain historical until a consumer completes a
// separate governed migration.
type RetirementRequest struct {
	ProfileID      string
	Approval       decision.Decision
	Impact         ImpactAnalysis
	PositionReader ActivePositionReader
}

func (a ArchitectureRevision) RetireRevision(req RetirementRequest) (ArchitectureRevision, error) {
	if req.Approval.State != decision.Allow && req.Approval.State != decision.AllowWithObligations {
		return ArchitectureRevision{}, ErrPublicationNotApproved
	}
	if !req.Impact.Frozen || req.Impact.ProfileID != req.ProfileID {
		return ArchitectureRevision{}, ErrImpactRequired
	}
	return a.RetireProfile(context.Background(), req.ProfileID, req.PositionReader)
}

func (a ArchitectureRevision) Retire(req RetirementRequest) (ArchitectureRevision, error) {
	return a.RetireRevision(req)
}

func AnalyzeRevisionImpact(profileID, fromRevision, toRevision string, in ImpactInput) (ImpactAnalysis, error) {
	return AnalyzeImpact(profileID, fromRevision, toRevision, in)
}

func (a ImpactAnalysis) Explain() string {
	return fmt.Sprintf("job architecture impact profile=%s from=%s to=%s positions=%d workers=%d requisitions=%d dependencies=%d review_intents=%d migration_intents=%d frozen=%t", a.ProfileID, a.FromRevision, a.ToRevision, len(a.Positions), len(a.Workers), len(a.Requisitions), len(a.Dependencies), len(a.ReviewIntents), len(a.MigrationIntents), a.Frozen)
}
