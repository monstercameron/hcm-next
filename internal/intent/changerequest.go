package intent

import (
	"context"
	"slices"
	"sort"

	"github.com/monstercameron/hcm-next/internal/intent/lifecycle"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// PreflightStatus is the typed outcome of preflight. There are exactly four,
// and they are distinct on purpose: NEEDS_DATA is the caller's problem to fix,
// BLOCKED is a state-of-the-world problem, and DENIED is an authority answer
// that no amount of extra data changes.
type PreflightStatus uint8

// PreflightStatus values.
const (
	PreflightUnspecified PreflightStatus = iota
	PreflightReady
	PreflightNeedsData
	PreflightBlocked
	PreflightDenied
)

var preflightStatusNames = map[PreflightStatus]string{
	PreflightUnspecified: "UNSPECIFIED",
	PreflightReady:       "READY",
	PreflightNeedsData:   "NEEDS_DATA",
	PreflightBlocked:     "BLOCKED",
	PreflightDenied:      "DENIED",
}

func (s PreflightStatus) String() string {
	return enumName(preflightStatusNames, s, "PreflightStatus")
}

// severity ranks statuses so that combining kernel and domain results always
// keeps the worst answer. DENIED outranks BLOCKED outranks NEEDS_DATA.
func (s PreflightStatus) severity() int {
	switch s {
	case PreflightReady:
		return 1
	case PreflightNeedsData:
		return 2
	case PreflightBlocked:
		return 3
	case PreflightDenied:
		return 4
	default:
		return 0
	}
}

// FindingCode classifies a preflight finding. Codes are stable identifiers; the
// human-readable detail beside them is never the thing a caller branches on.
type FindingCode string

// Kernel finding codes.
const (
	FindingUnknownSubject     FindingCode = "UNKNOWN_SUBJECT"
	FindingUnknownReference   FindingCode = "UNKNOWN_REFERENCE"
	FindingForbiddenField     FindingCode = "FORBIDDEN_FIELD"
	FindingInvalidEffective   FindingCode = "INVALID_EFFECTIVE_DATE"
	FindingStaleBaseline      FindingCode = "STALE_BASELINE"
	FindingMissingRequired    FindingCode = "MISSING_REQUIRED_DATA"
	FindingAuthorityDenied    FindingCode = "AUTHORITY_DENIED"
	FindingNegativeStateBlock FindingCode = "NEGATIVE_STATE_BLOCK"
)

// Finding is one typed preflight observation.
type Finding struct {
	Code      FindingCode
	FieldPath string
	Detail    string
	Status    PreflightStatus
}

// BaselineSnapshot is the exact input state preflight evaluated. It is supplied
// by a domain projection; the kernel reads it and never queries a store.
type BaselineSnapshot struct {
	// SnapshotID identifies the snapshot for evidence.
	SnapshotID string

	// ObservedAt is when the snapshot was taken.
	ObservedAt values.Instant

	// Revisions pins the revision every read resource was at.
	Revisions map[string]values.RevisionToken

	// KnownSubjects are the subjects the snapshot resolved.
	KnownSubjects []SubjectReference

	// ForbiddenFields are field paths the caller may not propose for, whether
	// because of authority, classification or an externally mastered field.
	ForbiddenFields []string

	// PresentInputs are the required-input schema paths the request actually
	// populated.
	PresentInputs []string

	// NegativeStates are the negative states observed on the read facts.
	NegativeStates []NegativeState
}

// PreflightRequest is what the kernel hands a domain preflighter.
type PreflightRequest struct {
	Instance   Instance
	Definition Definition
	Baseline   BaselineSnapshot
}

// DomainPreflightResult is what a domain preflighter returns. It has no channel
// through which a write or an external effect could be expressed, and the one
// field that could name an effect exists solely so that a domain that
// mistakenly declares one is rejected rather than obeyed.
type DomainPreflightResult struct {
	Status   PreflightStatus
	Findings []Finding

	// DeclaredEffects must be empty. Preflight reads; it never mutates and
	// never causes an external effect. A non-empty value fails the preflight
	// with ErrEffectInPreflight.
	DeclaredEffects []PlannedEffect
}

// Preflighter is the domain preflight port. A domain package implements it; the
// kernel only knows the request and result shapes.
type Preflighter interface {
	Preflight(ctx context.Context, req PreflightRequest) (DomainPreflightResult, error)
}

// PreflightResult is the combined kernel and domain answer.
type PreflightResult struct {
	Status   PreflightStatus
	Findings []Finding

	// InputSnapshot is the exact snapshot the answer was computed against.
	InputSnapshot BaselineSnapshot

	// Lifecycle is the tuple the instance should move to. On a READY result the
	// request advances to PREFLIGHTED; otherwise it stays where it was.
	Lifecycle lifecycle.Dimensions
}

// Ready reports whether the request may enter simulation.
func (r PreflightResult) Ready() bool { return r.Status == PreflightReady }

// Draft records a new change request in DRAFT. It is a thin, deliberate
// wrapper over [NewInstance]: an HCMChangeRequest is a CHANGE_REQUEST instance,
// not a second envelope type, and the only thing Draft adds is the family
// check that keeps analytical and calculation intents out of the change path.
func Draft(spec InstanceSpec, def Definition, d Digester, ids IDSource, clock Clock) (Instance, CreationEvidence, error) {
	if def.Family != FamilyChangeRequest {
		return Instance{}, CreationEvidence{}, newError("Draft", "kernel_family", ErrInvalidDefinition,
			"%s is a %s; only a CHANGE_REQUEST is drafted as a change request",
			def.Ref, def.Family)
	}
	return NewInstance(spec, def, d, ids, clock)
}

// Preflight evaluates a drafted request against a baseline snapshot and an
// optional domain preflighter.
//
// The kernel checks what it owns: subjects resolve, no forbidden field is
// proposed for, the effective date is representable and within the snapshot's
// horizon, no read baseline is stale, and every required input is present. The
// domain port answers what it owns. The combined status is the worst of the
// two, and every finding from both is returned.
//
// Nothing here mutates anything. A domain preflighter that declares an effect
// fails the whole call.
func Preflight(ctx context.Context, req PreflightRequest, reg *Registry, p Preflighter) (PreflightResult, error) {
	def := req.Definition
	if err := req.Instance.Validate(def); err != nil {
		return PreflightResult{}, err
	}
	if !def.ZeroEffect() {
		// Preflight is defined only where the release ceiling is zero effect.
		// A definition scheduled to mutate must go through the P1B path, which
		// does not exist yet.
		return PreflightResult{}, newError("Preflight", "effect_class", ErrEffectInPreflight,
			"%s has effect class %s; P1A preflight is zero-effect only", def.Ref, def.EffectClass)
	}

	findings := kernelFindings(req, reg)
	status := worstStatus(PreflightReady, findings)

	if p != nil {
		domain, err := p.Preflight(ctx, req)
		if err != nil {
			return PreflightResult{}, newError("Preflight", "", ErrPreflight, "%v", err)
		}
		if len(domain.DeclaredEffects) > 0 {
			return PreflightResult{}, newError("Preflight", "declared_effects", ErrEffectInPreflight,
				"domain preflight for %s declared %d effect(s)", def.Ref, len(domain.DeclaredEffects))
		}
		findings = append(findings, domain.Findings...)
		if domain.Status.severity() > status.severity() {
			status = domain.Status
		}
		status = worstStatus(status, domain.Findings)
	}

	sortFindings(findings)
	next := req.Instance.Lifecycle
	if status == PreflightReady {
		next.Request = lifecycle.RequestPreflighted
	}
	if err := lifecycle.Check(next, req.Instance.LifecycleContext(def)); err != nil {
		return PreflightResult{}, err
	}
	return PreflightResult{
		Status:        status,
		Findings:      findings,
		InputSnapshot: req.Baseline,
		Lifecycle:     next,
	}, nil
}

func kernelFindings(req PreflightRequest, reg *Registry) []Finding {
	var findings []Finding
	def := req.Definition
	base := req.Baseline

	known := make(map[SubjectReference]bool, len(base.KnownSubjects))
	for _, s := range base.KnownSubjects {
		known[s] = true
	}
	for _, s := range req.Instance.Subjects {
		if !known[s] {
			findings = append(findings, Finding{
				Code:      FindingUnknownSubject,
				FieldPath: "subjects",
				Detail:    "subject " + s.Kind + "/" + s.SubjectID + " does not resolve in the baseline snapshot",
				Status:    PreflightBlocked,
			})
		}
	}

	forbidden := make(map[string]bool, len(base.ForbiddenFields))
	for _, f := range base.ForbiddenFields {
		forbidden[f] = true
	}
	present := make(map[string]bool, len(base.PresentInputs))
	for _, f := range base.PresentInputs {
		present[f] = true
	}
	for _, in := range def.RequiredInputs {
		if forbidden[in.Path] {
			findings = append(findings, Finding{
				Code:      FindingForbiddenField,
				FieldPath: in.Path,
				Detail:    "the caller may not supply this field",
				Status:    PreflightDenied,
			})
			continue
		}
		if in.Required && !present[in.Path] {
			findings = append(findings, Finding{
				Code:      FindingMissingRequired,
				FieldPath: in.Path,
				Detail:    "required input is absent",
				Status:    PreflightNeedsData,
			})
		}
	}
	for _, f := range base.PresentInputs {
		if forbidden[f] {
			findings = append(findings, Finding{
				Code:      FindingForbiddenField,
				FieldPath: f,
				Detail:    "the request supplies a field the caller may not write",
				Status:    PreflightDenied,
			})
		}
	}

	// A definition that declares an effective-time input cannot be simulated
	// without one: "when does this take effect" is not a value the kernel is
	// allowed to guess, and a domain that guesses it produces a proposal nobody
	// asked for.
	if req.Instance.RequestedEffectiveAt == nil {
		for _, in := range def.RequiredInputs {
			if in.Kind == InputKindEffectiveTime && in.Required {
				findings = append(findings, Finding{
					Code:      FindingInvalidEffective,
					FieldPath: "requested_effective_at",
					Detail: "the definition declares required effective-time input " +
						in.Path + " but the request carries no effective time",
					Status: PreflightNeedsData,
				})
				break
			}
		}
	}

	if !base.ObservedAt.IsSet() {
		findings = append(findings, Finding{
			Code:      FindingStaleBaseline,
			FieldPath: "baseline.observed_at",
			Detail:    "baseline snapshot records no observation time",
			Status:    PreflightBlocked,
		})
	}
	for _, stream := range sortedKeys(base.Revisions) {
		if rev := base.Revisions[stream]; !rev.IsSpecified() {
			findings = append(findings, Finding{
				Code:      FindingStaleBaseline,
				FieldPath: "baseline.revisions." + stream,
				Detail:    "baseline read is not pinned to a revision",
				Status:    PreflightBlocked,
			})
		}
	}

	if reg != nil && len(base.NegativeStates) > 0 {
		policy, err := reg.PolicyFor(def.Ref)
		if err != nil {
			findings = append(findings, Finding{
				Code:      FindingUnknownReference,
				FieldPath: "negative_state_policy_ref",
				Detail:    err.Error(),
				Status:    PreflightBlocked,
			})
		} else {
			for _, state := range base.NegativeStates {
				rule, err := policy.Decide(state)
				if err != nil {
					findings = append(findings, Finding{
						Code:      FindingNegativeStateBlock,
						FieldPath: "baseline.negative_states",
						Detail:    err.Error(),
						Status:    PreflightBlocked,
					})
					continue
				}
				switch rule.Action {
				case ActionBlock:
					findings = append(findings, Finding{
						Code:      FindingNegativeStateBlock,
						FieldPath: "baseline.negative_states",
						Detail:    state.String() + " is decided BLOCK by " + policy.Ref(),
						Status:    PreflightBlocked,
					})
				case ActionRouteHuman, ActionCreateObligation, ActionCreateRepair:
					findings = append(findings, Finding{
						Code:      FindingNegativeStateBlock,
						FieldPath: "baseline.negative_states",
						Detail:    state.String() + " is decided " + rule.Action.String() + " by " + policy.Ref(),
						Status:    PreflightNeedsData,
					})
				}
			}
		}
	}
	return findings
}

// sortedKeys returns a map's keys in sorted order, so that findings derived
// from a map are deterministic whatever Go's iteration order does.
func sortedKeys(m map[string]values.RevisionToken) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func worstStatus(start PreflightStatus, findings []Finding) PreflightStatus {
	out := start
	for _, f := range findings {
		if f.Status.severity() > out.severity() {
			out = f.Status
		}
	}
	return out
}

func sortFindings(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Code != findings[j].Code {
			return findings[i].Code < findings[j].Code
		}
		return findings[i].FieldPath < findings[j].FieldPath
	})
}

// FindingCodes returns the distinct codes present in a result, sorted. It is
// what a transport layer renders; the detail strings are never parsed.
func (r PreflightResult) FindingCodes() []FindingCode {
	seen := map[FindingCode]bool{}
	var out []FindingCode
	for _, f := range r.Findings {
		if !seen[f.Code] {
			seen[f.Code] = true
			out = append(out, f.Code)
		}
	}
	slices.Sort(out)
	return out
}
