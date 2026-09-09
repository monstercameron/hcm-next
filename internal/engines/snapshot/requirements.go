package snapshot

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Requirement-policy errors are deliberately distinct from the SNAPSHOT-001
// all-inputs-required resolver. They let a caller distinguish an optional
// degradation from a required-input refusal while keeping the input name in
// every refusal.
var (
	ErrInputRequirementIncomplete = errors.New("snapshot: input requirement is malformed")
	ErrInputPolicyInvalid         = errors.New("snapshot: input requirement policy is invalid")
	ErrInputUnavailable           = errors.New("snapshot: required input is unavailable")
	ErrMinimumHeadUnsatisfied     = errors.New("snapshot: input head is below the required head")
	ErrFreshnessTooOld            = errors.New("snapshot: input freshness is older than the allowed horizon")
	ErrFreshnessAfterKnownAt      = errors.New("snapshot: input freshness is after the known-at horizon")
	ErrRequirementUnsatisfied     = errors.New("snapshot: required input requirement is unsatisfied")
)

// RequirementError is a typed refusal with the exact input that caused it.
// It unwraps to both the broad requirement error and its specific cause.
type RequirementError struct {
	Code      string
	InputName string
	Detail    string
	Cause     error
}

func (e *RequirementError) Error() string {
	return fmt.Sprintf("snapshot: %s [input=%s]: %s", e.Code, e.InputName, e.Detail)
}

func (e *RequirementError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrRequirementUnsatisfied}
	}
	return []error{ErrRequirementUnsatisfied, e.Cause}
}

// RequirementCodeOf returns the stable code of a typed requirement refusal.
func RequirementCodeOf(err error) string {
	var e *RequirementError
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// RequirementInputOf returns the exact input named by a typed refusal.
func RequirementInputOf(err error) string {
	var e *RequirementError
	if errors.As(err, &e) {
		return e.InputName
	}
	return ""
}

func requirementRefusal(code, input string, cause error, format string, args ...any) error {
	return &RequirementError{Code: code, InputName: input, Cause: cause, Detail: fmt.Sprintf(format, args...)}
}

// InputPolicy declares whether an input is required for the result, may be
// degraded independently, or is required only when ConditionActive is true.
type InputPolicy string

const (
	InputPolicyUnspecified InputPolicy = ""
	InputRequired          InputPolicy = "REQUIRED"
	InputOptional          InputPolicy = "OPTIONAL"
	InputConditional       InputPolicy = "CONDITIONAL"
)

// Wire-friendly aliases.
const (
	Required    = InputRequired
	Optional    = InputOptional
	Conditional = InputConditional
)

func (p InputPolicy) valid() bool {
	return p == InputRequired || p == InputOptional || p == InputConditional
}

// InputRequirement is the request-time contract for one snapshot input.
// MinimumWatermark and MinimumHead are independent: a source must satisfy
// each one that is declared. MaximumStaleness is in relation to KnownAt; a
// zero value means that no freshness floor is declared.
type InputRequirement struct {
	Name string

	MinimumWatermark  values.RevisionToken
	MinWatermark      values.RevisionToken
	MinimumRevision   values.RevisionToken
	MinimumHead       string
	MinHead           string
	RequiredAuthority AuthorityClass
	Authority         AuthorityClass
	MaximumStaleness  time.Duration
	MaxAge            time.Duration
	Policy            InputPolicy
	ConditionActive   bool
}

// FreshnessRequirement is the descriptive alias used by freshness-focused
// callers.
type FreshnessRequirement = InputRequirement

func (r InputRequirement) normalized() (InputRequirement, error) {
	out := r
	if out.MinimumWatermark == (values.RevisionToken{}) {
		out.MinimumWatermark = out.MinWatermark
	}
	if out.MinimumWatermark == (values.RevisionToken{}) {
		out.MinimumWatermark = out.MinimumRevision
	}
	if r.MinimumWatermark != (values.RevisionToken{}) && r.MinWatermark != (values.RevisionToken{}) && r.MinimumWatermark != r.MinWatermark {
		return InputRequirement{}, fmt.Errorf("%w: input %q has conflicting minimum watermarks", ErrInputRequirementIncomplete, r.Name)
	}
	if r.MinimumWatermark != (values.RevisionToken{}) && r.MinimumRevision != (values.RevisionToken{}) && r.MinimumWatermark != r.MinimumRevision {
		return InputRequirement{}, fmt.Errorf("%w: input %q has conflicting minimum revisions", ErrInputRequirementIncomplete, r.Name)
	}
	if out.MinimumHead == "" {
		out.MinimumHead = out.MinHead
	}
	if r.MinimumHead != "" && r.MinHead != "" && r.MinimumHead != r.MinHead {
		return InputRequirement{}, fmt.Errorf("%w: input %q has conflicting minimum heads", ErrInputRequirementIncomplete, r.Name)
	}
	if out.RequiredAuthority == AuthorityUnspecified {
		out.RequiredAuthority = out.Authority
	}
	if r.RequiredAuthority != AuthorityUnspecified && r.Authority != AuthorityUnspecified && r.RequiredAuthority != r.Authority {
		return InputRequirement{}, fmt.Errorf("%w: input %q has conflicting authorities", ErrInputRequirementIncomplete, r.Name)
	}
	if out.MaximumStaleness == 0 {
		out.MaximumStaleness = out.MaxAge
	}
	if r.MaximumStaleness != 0 && r.MaxAge != 0 && r.MaximumStaleness != r.MaxAge {
		return InputRequirement{}, fmt.Errorf("%w: input %q has conflicting freshness ages", ErrInputRequirementIncomplete, r.Name)
	}
	return out, nil
}

func (r InputRequirement) Validate() error {
	n, err := r.normalized()
	if err != nil {
		return err
	}
	if strings.TrimSpace(n.Name) == "" {
		return fmt.Errorf("%w: input name is required", ErrInputRequirementIncomplete)
	}
	if !n.Policy.valid() {
		return fmt.Errorf("%w: input %q must be REQUIRED, OPTIONAL, or CONDITIONAL", ErrInputPolicyInvalid, n.Name)
	}
	if n.RequiredAuthority != AuthorityUnspecified && !n.RequiredAuthority.Valid() {
		return fmt.Errorf("%w: input %q has authority %q", ErrInputRequirementIncomplete, n.Name, n.RequiredAuthority)
	}
	if n.MinimumWatermark != (values.RevisionToken{}) {
		if err := n.MinimumWatermark.Validate(); err != nil || !n.MinimumWatermark.IsSpecified() {
			return fmt.Errorf("%w: input %q minimum watermark is not specified", ErrInputRequirementIncomplete, n.Name)
		}
	}
	if n.MaximumStaleness < 0 {
		return fmt.Errorf("%w: input %q maximum staleness is negative", ErrInputRequirementIncomplete, n.Name)
	}
	return nil
}

// SnapshotRequest carries the per-input requirements and the one shared
// tenant/known-at boundary. KnownAt is a compatibility spelling for
// KnownAtHorizon; if both are present they must agree.
type SnapshotRequest struct {
	Tenant         values.TenantId
	KnownAtHorizon values.KnownAt
	KnownAt        values.KnownAt
	Inputs         []InputRequirement
}

// FreshnessRequest is an alias for SnapshotRequest.
type FreshnessRequest = SnapshotRequest

func (r SnapshotRequest) normalized() (SnapshotRequest, error) {
	out := r
	if out.KnownAtHorizon.Canonical() == nil {
		out.KnownAtHorizon = out.KnownAt
	}
	if r.KnownAtHorizon.Canonical() != nil && r.KnownAt.Canonical() != nil && r.KnownAtHorizon.Instant().Compare(r.KnownAt.Instant()) != 0 {
		return SnapshotRequest{}, fmt.Errorf("%w: known-at spellings disagree", ErrInputRequirementIncomplete)
	}
	if err := out.Tenant.Validate(); err != nil {
		return SnapshotRequest{}, fmt.Errorf("%w: tenant: %v", ErrInputRequirementIncomplete, err)
	}
	if out.KnownAtHorizon.Canonical() == nil {
		return SnapshotRequest{}, fmt.Errorf("%w: known-at horizon is required", ErrInputRequirementIncomplete)
	}
	if len(out.Inputs) == 0 {
		return SnapshotRequest{}, fmt.Errorf("%w: at least one input is required", ErrInputRequirementIncomplete)
	}
	seen := make(map[string]struct{}, len(out.Inputs))
	for i := range out.Inputs {
		n, err := out.Inputs[i].normalized()
		if err != nil {
			return SnapshotRequest{}, err
		}
		out.Inputs[i] = n
		if err := n.Validate(); err != nil {
			return SnapshotRequest{}, err
		}
		if _, ok := seen[n.Name]; ok {
			return SnapshotRequest{}, fmt.Errorf("%w: input %q is declared twice", ErrInputRequirementIncomplete, n.Name)
		}
		seen[n.Name] = struct{}{}
	}
	return out, nil
}

// RequirementStatus is the recorded disposition of a request-time input.
type RequirementStatus string

const (
	RequirementSatisfied      RequirementStatus = "SATISFIED"
	RequirementMissing        RequirementStatus = "MISSING"
	RequirementStale          RequirementStatus = "STALE"
	RequirementBelowWatermark RequirementStatus = "BELOW_WATERMARK"
	RequirementHeadMismatch   RequirementStatus = "HEAD_MISMATCH"
	RequirementNotApplicable  RequirementStatus = "NOT_APPLICABLE"
)

// RequirementDisposition records both successful and degraded checks. The
// horizon is included per input because each input can declare a different
// maximum staleness.
type RequirementDisposition struct {
	InputName        string
	Policy           InputPolicy
	Status           RequirementStatus
	Satisfied        bool
	FreshnessHorizon values.Instant
	Detail           string
}

// RequirementResolution is the resolved snapshot plus every input's exact
// requirement disposition. Optional and inactive conditional inputs remain
// visible as unsatisfied/not-applicable rather than being silently dropped.
type RequirementResolution struct {
	Snapshot     ReadSnapshot
	Dispositions []RequirementDisposition
	Digest       string
}

// CanonicalDigest returns the digest over the request and requirement
// dispositions, in addition to the snapshot's exact input digest.
func (r RequirementResolution) CanonicalDigest() string { return r.Digest }

// Explain returns an audit-safe summary naming every requested input and its
// disposition, without including input payloads.
func (r RequirementResolution) Explain() string {
	var b strings.Builder
	fmt.Fprintf(&b, "snapshot requirement resolution %s: %d input(s)", r.Digest, len(r.Dispositions))
	for _, d := range r.Dispositions {
		fmt.Fprintf(&b, "\n- %s=%s (%s)", d.InputName, d.Status, d.Policy)
	}
	return b.String()
}

// FreshnessHorizon derives the oldest acceptable freshness instant from a
// known-at boundary and a maximum age. It is pure and uses the kernel Instant
// rather than a process-local wall clock.
func FreshnessHorizon(knownAt values.KnownAt, maximumStaleness time.Duration) (values.Instant, error) {
	if knownAt.Canonical() == nil {
		return values.Instant{}, fmt.Errorf("%w: known-at is unset", ErrInputRequirementIncomplete)
	}
	if maximumStaleness < 0 {
		return values.Instant{}, fmt.Errorf("%w: maximum staleness is negative", ErrInputRequirementIncomplete)
	}
	return values.NewInstant(knownAt.Instant().Time().Add(-maximumStaleness)), nil
}

// ResolveWithRequirements resolves one explicit SnapshotRequest. Required
// failures refuse with a RequirementError naming the input; optional failures
// are returned as dispositions and do not block unrelated inputs.
func ResolveWithRequirements(ctx context.Context, source Source, request SnapshotRequest) (RequirementResolution, error) {
	normalized, err := request.normalized()
	if err != nil {
		return RequirementResolution{}, err
	}
	if source == nil {
		return RequirementResolution{}, fmt.Errorf("%w: no source configured", ErrSourceFailed)
	}
	requests := make([]InputRequest, len(normalized.Inputs))
	byName := make(map[string]InputRequirement, len(normalized.Inputs))
	for i, req := range normalized.Inputs {
		requests[i] = InputRequest{Name: req.Name, RequiredAuthority: req.RequiredAuthority}
		byName[req.Name] = req
	}
	entries, err := source.Resolve(ctx, normalized.Tenant, requests)
	if err != nil {
		return RequirementResolution{}, fmt.Errorf("%w: %w", ErrSourceFailed, err)
	}
	answers := make(map[string]InputEntry, len(entries))
	for _, entry := range entries {
		if _, ok := byName[entry.Name]; !ok {
			return RequirementResolution{}, fmt.Errorf("%w: %q", ErrUnrequestedEntry, entry.Name)
		}
		if _, dup := answers[entry.Name]; dup {
			return RequirementResolution{}, fmt.Errorf("%w: %q", ErrDuplicateEntry, entry.Name)
		}
		if err := entry.Validate(); err != nil {
			return RequirementResolution{}, err
		}
		if entry.Tenant != normalized.Tenant {
			return RequirementResolution{}, requirementRefusal("TENANT_MISMATCH", entry.Name, ErrTenantMismatch, "entry tenant %q does not match requirement tenant %q", entry.Tenant, normalized.Tenant)
		}
		if entry.KnownAt.Instant().Compare(normalized.KnownAtHorizon.Instant()) != 0 {
			return RequirementResolution{}, requirementRefusal("KNOWN_AT_MISMATCH", entry.Name, ErrKnownAtHorizonMismatch, "entry known-at %s does not match horizon %s", entry.KnownAt, normalized.KnownAtHorizon)
		}
		answers[entry.Name] = entry
	}

	dispositions := make([]RequirementDisposition, 0, len(normalized.Inputs))
	resolved := make([]InputEntry, 0, len(answers))
	for _, req := range normalized.Inputs {
		disposition := RequirementDisposition{InputName: req.Name, Policy: req.Policy, Status: RequirementMissing}
		entry, ok := answers[req.Name]
		if !ok {
			if req.Policy == InputConditional && !req.ConditionActive {
				disposition.Status = RequirementNotApplicable
				disposition.Satisfied = true
				dispositions = append(dispositions, disposition)
				continue
			}
			dispositions = append(dispositions, disposition)
			if req.Policy == InputRequired || (req.Policy == InputConditional && req.ConditionActive) {
				return RequirementResolution{}, requirementRefusal("INPUT_UNAVAILABLE", req.Name, ErrInputUnavailable, "required input was not returned by the source")
			}
			continue
		}
		resolved = append(resolved, entry)
		if req.RequiredAuthority != AuthorityUnspecified && entry.Authority != req.RequiredAuthority {
			return RequirementResolution{}, requirementRefusal("AUTHORITY_MISMATCH", req.Name, ErrAuthorityMismatch, "entry authority %s does not match required %s", entry.Authority, req.RequiredAuthority)
		}
		if entry.Freshness.Instant().Compare(normalized.KnownAtHorizon.Instant()) > 0 {
			return RequirementResolution{}, requirementRefusal("FRESHNESS_AFTER_KNOWN_AT", req.Name, ErrFreshnessAfterKnownAt, "freshness %s is after known-at %s", entry.Freshness, normalized.KnownAtHorizon)
		}
		if req.MinimumWatermark != (values.RevisionToken{}) {
			cmp, compareErr := entry.Watermark.CompareInStream(req.MinimumWatermark)
			if compareErr != nil {
				return RequirementResolution{}, requirementRefusal("WATERMARK_INCOMPARABLE", req.Name, ErrWatermarkIncomparable, "%v", compareErr)
			}
			if cmp < 0 {
				disposition.Status = RequirementBelowWatermark
				disposition.Detail = "resolved watermark is below the requested minimum"
				if refusal := enforceDisposition(req, disposition, ErrWatermarkBelowMinimum); refusal != nil {
					return RequirementResolution{}, refusal
				}
			}
		}
		if req.MinimumHead != "" && entry.Head != req.MinimumHead {
			disposition.Status = RequirementHeadMismatch
			disposition.Detail = fmt.Sprintf("resolved head %q does not equal required head %q", entry.Head, req.MinimumHead)
			if refusal := enforceDisposition(req, disposition, ErrMinimumHeadUnsatisfied); refusal != nil {
				return RequirementResolution{}, refusal
			}
		}
		if req.MaximumStaleness > 0 {
			horizon, horizonErr := FreshnessHorizon(normalized.KnownAtHorizon, req.MaximumStaleness)
			if horizonErr != nil {
				return RequirementResolution{}, horizonErr
			}
			disposition.FreshnessHorizon = horizon
			if entry.Freshness.Instant().Compare(horizon) < 0 {
				disposition.Status = RequirementStale
				disposition.Detail = fmt.Sprintf("freshness %s is older than horizon %s", entry.Freshness, horizon)
				if refusal := enforceDisposition(req, disposition, ErrFreshnessTooOld); refusal != nil {
					return RequirementResolution{}, refusal
				}
			}
		}
		if disposition.Status == RequirementMissing {
			disposition.Status = RequirementSatisfied
			disposition.Satisfied = true
		}
		dispositions = append(dispositions, disposition)
	}

	floors := make([]InputWatermarkFloor, 0, len(normalized.Inputs))
	for _, req := range normalized.Inputs {
		if req.MinimumWatermark != (values.RevisionToken{}) {
			floors = append(floors, InputWatermarkFloor{InputName: req.Name, Minimum: req.MinimumWatermark})
		}
	}
	baseRequirement := ConsistencyRequirement{Tenant: normalized.Tenant, KnownAtHorizon: normalized.KnownAtHorizon, MinWatermarks: floors}
	sort.Slice(resolved, func(i, j int) bool { return resolved[i].Name < resolved[j].Name })
	snapshotDigest, err := computeDigest(baseRequirement, resolved)
	if err != nil {
		return RequirementResolution{}, err
	}
	result := RequirementResolution{Snapshot: ReadSnapshot{Tenant: normalized.Tenant, KnownAtHorizon: normalized.KnownAtHorizon, Requirement: baseRequirement, Entries: resolved, Digest: snapshotDigest}, Dispositions: dispositions}
	result.Digest, err = requirementDigest(normalized, result)
	if err != nil {
		return RequirementResolution{}, err
	}
	return result, nil
}

// ResolveFreshness and ResolveRequirements are descriptive aliases for the
// same request-time resolver.
func ResolveFreshness(ctx context.Context, source Source, request FreshnessRequest) (RequirementResolution, error) {
	return ResolveWithRequirements(ctx, source, request)
}

func ResolveRequirements(ctx context.Context, source Source, request SnapshotRequest) (RequirementResolution, error) {
	return ResolveWithRequirements(ctx, source, request)
}

func enforceDisposition(req InputRequirement, disposition RequirementDisposition, cause error) error {
	if req.Policy == InputOptional || (req.Policy == InputConditional && !req.ConditionActive) {
		return nil
	}
	return requirementRefusal("REQUIREMENT_UNSATISFIED", req.Name, cause, "%s", disposition.Detail)
}

func requirementDigest(request SnapshotRequest, result RequirementResolution) (string, error) {
	w := canonicalbytes.New("hcmnext.engines.snapshot.RequirementResolution", 1).
		String("tenant", string(request.Tenant)).Value("known_at", request.KnownAtHorizon).String("snapshot_digest", result.Snapshot.Digest)
	w.Count("requirements", len(request.Inputs))
	for _, req := range request.Inputs {
		w.String("input", req.Name).String("policy", string(req.Policy)).String("authority", req.RequiredAuthority.String()).
			String("minimum_watermark", req.MinimumWatermark.String()).String("minimum_head", req.MinimumHead).
			Int("maximum_staleness_nanos", int64(req.MaximumStaleness)).Bool("condition_active", req.ConditionActive)
	}
	w.Count("dispositions", len(result.Dispositions))
	for _, d := range result.Dispositions {
		w.String("disposition_input", d.InputName).String("status", string(d.Status)).Bool("satisfied", d.Satisfied).
			String("freshness_horizon", d.FreshnessHorizon.String()).String("detail", d.Detail)
	}
	return w.Digest()
}
