package snapshot

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

var (
	// ErrCompletenessSpecificationInvalid reports a malformed completeness
	// specification or presence-bearing resolved input.
	ErrCompletenessSpecificationInvalid = errors.New("snapshot: completeness specification is invalid")
	// ErrCompletenessRefused reports that a required input could not be
	// established because its presence is UNKNOWN.
	ErrCompletenessRefused = errors.New("snapshot: completeness evaluation refused")
)

// InputPresence is the source-neutral presence lattice used by completeness.
// It deliberately uses the same PRESENT/ABSENT/UNKNOWN vocabulary as the
// transformation taint envelope without importing that sibling engine
// subpackage.
type InputPresence string

const (
	PresencePresent InputPresence = "PRESENT"
	PresenceAbsent  InputPresence = "ABSENT"
	PresenceUnknown InputPresence = "UNKNOWN"

	// Short aliases keep the three-state wire vocabulary visible at call sites.
	Present InputPresence = PresencePresent
	Absent  InputPresence = PresenceAbsent
	Unknown InputPresence = PresenceUnknown
)

// Valid reports whether p is one of the three declared presence states.
func (p InputPresence) Valid() bool {
	return p == PresencePresent || p == PresenceAbsent || p == PresenceUnknown
}

// String returns the wire token for p.
func (p InputPresence) String() string { return string(p) }

// SnapshotInput is the presence-bearing descriptor for one resolved input.
// An absent descriptor is allowed: omission from CompletenessEvaluation.Inputs
// is interpreted as an explicit ABSENT state, never as a default value.
// Value is intentionally a string because conditions compare an exact,
// canonical caller-supplied value while Explain never reveals it.
type SnapshotInput struct {
	Name     string
	Presence InputPresence
	Value    string
}

// ResolvedInput is a descriptive alias for SnapshotInput.
type ResolvedInput = SnapshotInput

func (i SnapshotInput) validate() error {
	if strings.TrimSpace(i.Name) == "" {
		return fmt.Errorf("%w: resolved input name is required", ErrCompletenessSpecificationInvalid)
	}
	if !i.Presence.Valid() {
		return fmt.Errorf("%w: input %q has invalid presence %q", ErrCompletenessSpecificationInvalid, i.Name, i.Presence)
	}
	if i.Presence != PresencePresent && i.Value != "" {
		return fmt.Errorf("%w: non-PRESENT input %q carries a value", ErrCompletenessSpecificationInvalid, i.Name)
	}
	return nil
}

// CompletenessCondition activates a conditional requirement when InputName is
// PRESENT and its exact value equals ExpectedValue. An UNKNOWN condition is
// not treated as false.
type CompletenessCondition struct {
	InputName     string
	ExpectedValue string
}

func (c CompletenessCondition) validate(owner string) error {
	if strings.TrimSpace(c.InputName) == "" {
		return fmt.Errorf("%w: conditional input %q has no condition input", ErrCompletenessSpecificationInvalid, owner)
	}
	if c.InputName == owner {
		return fmt.Errorf("%w: input %q cannot condition itself", ErrCompletenessSpecificationInvalid, owner)
	}
	return nil
}

// CompletenessRequirement declares the policy for one input. REQUIRED and
// OPTIONAL inspect the input directly. CONDITIONAL requires Condition and
// inspects the input only when that condition is satisfied.
type CompletenessRequirement struct {
	Name      string
	Policy    InputPolicy
	Condition *CompletenessCondition
}

// InputSpec is a concise alias for a completeness requirement.
type InputSpec = CompletenessRequirement

func (r CompletenessRequirement) validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return fmt.Errorf("%w: requirement name is required", ErrCompletenessSpecificationInvalid)
	}
	if !r.Policy.valid() {
		return fmt.Errorf("%w: input %q has policy %q", ErrCompletenessSpecificationInvalid, r.Name, r.Policy)
	}
	if r.Policy == InputConditional {
		if r.Condition == nil {
			return fmt.Errorf("%w: conditional input %q has no condition", ErrCompletenessSpecificationInvalid, r.Name)
		}
		return r.Condition.validate(r.Name)
	}
	if r.Condition != nil {
		return fmt.Errorf("%w: non-conditional input %q has a condition", ErrCompletenessSpecificationInvalid, r.Name)
	}
	return nil
}

// InputSpecification is the complete declared input contract for one
// evaluation. Declaration order is retained in Dispositions and Explain;
// digest material is sorted canonically by input name.
type InputSpecification struct {
	Inputs []CompletenessRequirement
}

// CompletenessSpec is a descriptive alias for InputSpecification.
type CompletenessSpec = InputSpecification

func (s InputSpecification) validate() error {
	if len(s.Inputs) == 0 {
		return fmt.Errorf("%w: at least one input requirement is required", ErrCompletenessSpecificationInvalid)
	}
	seen := make(map[string]struct{}, len(s.Inputs))
	for _, requirement := range s.Inputs {
		if err := requirement.validate(); err != nil {
			return err
		}
		if _, ok := seen[requirement.Name]; ok {
			return fmt.Errorf("%w: input %q is declared twice", ErrCompletenessSpecificationInvalid, requirement.Name)
		}
		seen[requirement.Name] = struct{}{}
	}
	for _, requirement := range s.Inputs {
		if requirement.Policy == InputConditional {
			if _, ok := seen[requirement.Condition.InputName]; !ok {
				return fmt.Errorf("%w: condition input %q for %q is not declared", ErrCompletenessSpecificationInvalid, requirement.Condition.InputName, requirement.Name)
			}
		}
	}
	return nil
}

// CompletenessVerdict is the three-valued result for an input and for the
// aggregate evaluation. MISSING is a known absence; UNKNOWN means the
// presence or the condition could not be established.
type CompletenessVerdict string

const (
	VerdictSatisfied CompletenessVerdict = "SATISFIED"
	VerdictMissing   CompletenessVerdict = "MISSING"
	VerdictUnknown   CompletenessVerdict = "UNKNOWN"

	// Short aliases mirror the wire verdict vocabulary.
	Satisfied      CompletenessVerdict = VerdictSatisfied
	Missing        CompletenessVerdict = VerdictMissing
	UnknownVerdict CompletenessVerdict = VerdictUnknown
)

// CompletenessDisposition records one requirement's safe result. Value is
// never copied here, so Explain can be used in audit and refusal paths.
type CompletenessDisposition struct {
	Name            string
	Policy          InputPolicy
	Presence        InputPresence
	Verdict         CompletenessVerdict
	ConditionInput  string
	ConditionActive bool
	Detail          string
}

// CompletenessError identifies the input whose UNKNOWN presence caused a
// required evaluation to refuse. It intentionally contains no input value.
type CompletenessError struct {
	InputName string
	Detail    string
}

func (e *CompletenessError) Error() string {
	return fmt.Sprintf("snapshot: completeness refused [input=%s]: %s", e.InputName, e.Detail)
}

func (e *CompletenessError) Unwrap() error { return ErrCompletenessRefused }

// CompletenessInputOf returns the exact input named by a completeness
// refusal, or an empty string for another error.
func CompletenessInputOf(err error) string {
	var e *CompletenessError
	if errors.As(err, &e) {
		return e.InputName
	}
	return ""
}

// CompletenessResult is the immutable-by-convention result of evaluating one
// ReadSnapshot against one declared input specification.
type CompletenessResult struct {
	SnapshotDigest string
	Dispositions   []CompletenessDisposition
	Overall        CompletenessVerdict
	Digest         string
}

// CanonicalDigest returns the digest over the snapshot, exact presence/value
// observations, declared requirements, and resulting dispositions.
func (r CompletenessResult) CanonicalDigest() string { return r.Digest }

// Explain reports names, policies, presence states and verdicts, but never
// includes an input value or condition value.
func (r CompletenessResult) Explain() string {
	var b strings.Builder
	fmt.Fprintf(&b, "snapshot completeness %s: overall=%s, %d input(s)", r.Digest, r.Overall, len(r.Dispositions))
	for _, d := range r.Dispositions {
		fmt.Fprintf(&b, "\n- %s=%s (policy %s, presence %s)", d.Name, d.Verdict, d.Policy, d.Presence)
		if d.Detail != "" {
			fmt.Fprintf(&b, ": %s", d.Detail)
		}
	}
	return b.String()
}

// EvaluateCompleteness evaluates presence-bearing resolved inputs against a
// declaration. Inputs omitted from resolved are explicitly ABSENT. A required
// UNKNOWN input returns a result plus a typed refusal naming that input; the
// result remains available for evidence and safe explanation.
func EvaluateCompleteness(resolved ReadSnapshot, specification InputSpecification, inputs []SnapshotInput) (CompletenessResult, error) {
	if err := specification.validate(); err != nil {
		return CompletenessResult{}, err
	}
	observations := make(map[string]SnapshotInput, len(inputs))
	for _, input := range inputs {
		if err := input.validate(); err != nil {
			return CompletenessResult{}, err
		}
		if _, ok := observations[input.Name]; ok {
			return CompletenessResult{}, fmt.Errorf("%w: resolved input %q is duplicated", ErrCompletenessSpecificationInvalid, input.Name)
		}
		observations[input.Name] = input
	}

	dispositions := make([]CompletenessDisposition, 0, len(specification.Inputs))
	for _, requirement := range specification.Inputs {
		input := observations[requirement.Name]
		if input.Name == "" {
			input = SnapshotInput{Name: requirement.Name, Presence: PresenceAbsent}
		}
		disposition := CompletenessDisposition{
			Name:     requirement.Name,
			Policy:   requirement.Policy,
			Presence: input.Presence,
			Verdict:  VerdictMissing,
			Detail:   "input is ABSENT",
		}

		if requirement.Policy == InputConditional {
			condition := observations[requirement.Condition.InputName]
			if condition.Name == "" {
				condition = SnapshotInput{Name: requirement.Condition.InputName, Presence: PresenceAbsent}
			}
			disposition.ConditionInput = requirement.Condition.InputName
			switch condition.Presence {
			case PresenceUnknown:
				disposition.Presence = input.Presence
				disposition.Verdict = VerdictUnknown
				disposition.Detail = "condition input is UNKNOWN"
				dispositions = append(dispositions, disposition)
				continue
			case PresencePresent:
				disposition.ConditionActive = condition.Value == requirement.Condition.ExpectedValue
			case PresenceAbsent:
				disposition.ConditionActive = false
			}
			if !disposition.ConditionActive {
				disposition.Presence = input.Presence
				disposition.Verdict = VerdictSatisfied
				disposition.Detail = "condition is not satisfied"
				dispositions = append(dispositions, disposition)
				continue
			}
		}

		switch input.Presence {
		case PresencePresent:
			disposition.Verdict = VerdictSatisfied
			disposition.Detail = "input is PRESENT"
		case PresenceUnknown:
			disposition.Verdict = VerdictUnknown
			disposition.Detail = "input presence is UNKNOWN"
		case PresenceAbsent:
			// Keep MISSING explicit for OPTIONAL: no default value is selected.
			disposition.Verdict = VerdictMissing
			disposition.Detail = "input is ABSENT"
		}
		dispositions = append(dispositions, disposition)
	}

	overall := VerdictSatisfied
	var refusal *CompletenessError
	for i, requirement := range specification.Inputs {
		disposition := dispositions[i]
		blocking := requirement.Policy == InputRequired || requirement.Policy == InputConditional
		if !blocking {
			continue
		}
		switch disposition.Verdict {
		case VerdictUnknown:
			overall = VerdictUnknown
			if refusal == nil {
				name := requirement.Name
				detail := "required input presence is UNKNOWN"
				if requirement.Policy == InputConditional && disposition.Detail == "condition input is UNKNOWN" {
					name = disposition.ConditionInput
					detail = "condition input presence is UNKNOWN"
				}
				refusal = &CompletenessError{InputName: name, Detail: detail}
			}
		case VerdictMissing:
			if overall != VerdictUnknown {
				overall = VerdictMissing
			}
		}
	}

	result := CompletenessResult{
		SnapshotDigest: resolved.Digest,
		Dispositions:   append([]CompletenessDisposition(nil), dispositions...),
		Overall:        overall,
	}
	var err error
	result.Digest, err = completenessDigest(resolved, specification, inputs, result)
	if err != nil {
		return CompletenessResult{}, err
	}
	if refusal != nil {
		return result, refusal
	}
	return result, nil
}

func completenessDigest(resolved ReadSnapshot, specification InputSpecification, inputs []SnapshotInput, result CompletenessResult) (string, error) {
	observations := append([]SnapshotInput(nil), inputs...)
	sort.Slice(observations, func(i, j int) bool { return observations[i].Name < observations[j].Name })
	requirements := append([]CompletenessRequirement(nil), specification.Inputs...)
	sort.Slice(requirements, func(i, j int) bool { return requirements[i].Name < requirements[j].Name })
	dispositions := append([]CompletenessDisposition(nil), result.Dispositions...)
	sort.Slice(dispositions, func(i, j int) bool { return dispositions[i].Name < dispositions[j].Name })

	w := canonicalbytes.New("hcmnext.engines.snapshot.CompletenessResult", 1).
		String("snapshot_digest", resolved.Digest)
	w.Count("observations", len(observations))
	for _, input := range observations {
		w.String("observation_name", input.Name).
			String("observation_presence", input.Presence.String()).
			String("observation_value", input.Value)
	}
	w.Count("requirements", len(requirements))
	for _, requirement := range requirements {
		w.String("requirement_name", requirement.Name).
			String("requirement_policy", string(requirement.Policy))
		if requirement.Condition == nil {
			w.Bool("has_condition", false)
			continue
		}
		w.Bool("has_condition", true).
			String("condition_input", requirement.Condition.InputName).
			String("condition_value", requirement.Condition.ExpectedValue)
	}
	w.Count("dispositions", len(dispositions))
	for _, disposition := range dispositions {
		w.String("disposition_name", disposition.Name).
			String("disposition_policy", string(disposition.Policy)).
			String("disposition_presence", disposition.Presence.String()).
			String("disposition_verdict", string(disposition.Verdict)).
			String("disposition_condition_input", disposition.ConditionInput).
			Bool("disposition_condition_active", disposition.ConditionActive).
			String("disposition_detail", disposition.Detail)
	}
	w.String("overall", string(result.Overall))
	return w.Digest()
}
