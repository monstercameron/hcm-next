package model

import "sort"

// ClassificationLabel is a data-classification label (MODEL-023). Labels are
// ranked: [Strongest] and [Propagate] always keep the highest-ranked label a
// value's sources carry, so a derived value can never be classified more
// weakly than the data it was built from.
type ClassificationLabel string

// Classification labels, in ascending rank order.
const (
	ClassPublic          ClassificationLabel = "PUBLIC"
	ClassInternal        ClassificationLabel = "INTERNAL"
	ClassPII             ClassificationLabel = "PII"
	ClassCompensation    ClassificationLabel = "COMPENSATION"
	ClassBank            ClassificationLabel = "BANK"
	ClassMedical         ClassificationLabel = "MEDICAL"
	ClassImmigration     ClassificationLabel = "IMMIGRATION"
	ClassCase            ClassificationLabel = "CASE"
	ClassSpecialCategory ClassificationLabel = "SPECIAL_CATEGORY"
)

var classificationRank = map[ClassificationLabel]int{
	ClassPublic:          0,
	ClassInternal:        1,
	ClassPII:             2,
	ClassCompensation:    3,
	ClassBank:            3,
	ClassMedical:         3,
	ClassImmigration:     3,
	ClassCase:            3,
	ClassSpecialCategory: 4,
}

// Valid reports whether c is one of the declared classification labels.
func (c ClassificationLabel) Valid() bool { _, ok := classificationRank[c]; return ok }

// Rank returns c's propagation rank. A higher rank never yields to a lower
// one when two labels are combined.
func (c ClassificationLabel) Rank() int { return classificationRank[c] }

// Strongest returns the highest-ranked label among labels. It panics on an
// empty slice: a classified artifact always carries at least one label.
func Strongest(labels []ClassificationLabel) ClassificationLabel {
	if len(labels) == 0 {
		panic("model: Strongest called with no labels")
	}
	best := labels[0]
	for _, l := range labels[1:] {
		if l.Rank() > best.Rank() {
			best = l
		}
	}
	return best
}

// ClassifiedArtifact is one derived value, artifact, log record or outbound
// payload carrying propagated classification labels.
type ClassifiedArtifact struct {
	ArtifactRef   string
	Labels        []ClassificationLabel
	PolicyVersion string
}

// Validate rejects a material artifact with no classification label or no
// policy version.
func (a ClassifiedArtifact) Validate() error {
	if a.ArtifactRef == "" {
		return newError("ClassifiedArtifact.Validate", "artifact_ref", ErrInvalidClassification,
			"artifact carries no reference")
	}
	if len(a.Labels) == 0 {
		return newError("ClassifiedArtifact.Validate", "labels", ErrMissingClassification,
			"%s carries no classification label", a.ArtifactRef)
	}
	for _, l := range a.Labels {
		if !l.Valid() {
			return newError("ClassifiedArtifact.Validate", "labels", ErrInvalidClassification,
				"%s carries unknown label %q", a.ArtifactRef, l)
		}
	}
	if a.PolicyVersion == "" {
		return newError("ClassifiedArtifact.Validate", "policy_version", ErrInvalidClassification,
			"%s carries no classification policy version", a.ArtifactRef)
	}
	return nil
}

// LabelSet returns a's labels deduplicated and sorted by descending rank then
// name, so two equivalent label sets always compare and serialize identically.
func (a ClassifiedArtifact) LabelSet() []ClassificationLabel {
	seen := map[ClassificationLabel]bool{}
	var out []ClassificationLabel
	for _, l := range a.Labels {
		if !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Rank() != out[j].Rank() {
			return out[i].Rank() > out[j].Rank()
		}
		return out[i] < out[j]
	})
	return out
}

// Propagate computes the label set a derived artifact must carry given its
// source artifacts: the union of every source label, deduplicated and ranked.
// It rejects a source with no classification of its own (a source can never
// silently contribute an unclassified derivation) and requires a policy
// version to stamp the result with.
func Propagate(sources []ClassifiedArtifact, derivedRef, policyVersion string) (ClassifiedArtifact, error) {
	if derivedRef == "" {
		return ClassifiedArtifact{}, newError("Propagate", "derived_ref", ErrInvalidClassification,
			"derived artifact carries no reference")
	}
	if policyVersion == "" {
		return ClassifiedArtifact{}, newError("Propagate", "policy_version", ErrInvalidClassification,
			"no classification policy version supplied")
	}
	var labels []ClassificationLabel
	for _, s := range sources {
		if err := s.Validate(); err != nil {
			return ClassifiedArtifact{}, err
		}
		labels = append(labels, s.Labels...)
	}
	if len(labels) == 0 {
		labels = []ClassificationLabel{ClassPublic}
	}
	out := ClassifiedArtifact{ArtifactRef: derivedRef, Labels: labels, PolicyVersion: policyVersion}
	out.Labels = out.LabelSet()
	return out, nil
}

// CheckDerived rejects a derived artifact whose declared label set is weaker
// than the union its sources require: the strongest source label must appear
// in the derived artifact's own label set (MODEL-023 RED — "a derived value,
// artifact, log or outbound payload missing the strongest applicable label
// fails creation/delivery").
func CheckDerived(derived ClassifiedArtifact, sources []ClassifiedArtifact) error {
	if err := derived.Validate(); err != nil {
		return err
	}
	want, err := Propagate(sources, derived.ArtifactRef, derived.PolicyVersion)
	if err != nil {
		return err
	}
	have := map[ClassificationLabel]bool{}
	for _, l := range derived.LabelSet() {
		have[l] = true
	}
	strongest := Strongest(want.Labels)
	if !have[strongest] {
		return newError("CheckDerived", "labels", ErrMissingClassification,
			"%s omits the strongest applicable label %s carried by its sources",
			derived.ArtifactRef, strongest)
	}
	return nil
}
