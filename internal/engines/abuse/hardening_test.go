package abuse

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func hardeningVersion(kind SignalKind) DetectorVersion {
	return DetectorVersion{
		DetectorID: "hardening-detector", Semver: "1.2.3",
		DeclaredInputs: []SignalKind{kind}, DeclaredOutputs: []string{"REVIEW"},
		Thresholds:  []ThresholdRef{{ID: "window-1"}},
		ActivatedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	}
}

func hardeningRiskEvent(id, principal, tenant string, kind SignalKind, at time.Time) RiskEvent {
	return RiskEvent{
		ID: id, Kind: kind, Principal: principal, Tenant: tenant,
		Subject: Ref{Type: "worker", ID: "worker-" + id}, ObservedAt: at,
	}
}

func TestAbuseHelpers_ErrorChainsAndValidationBranches(t *testing.T) {
	if !(Effects{}).IsZero() || (Effects{HumanWork: 1}).IsZero() {
		t.Fatal("Effects.IsZero did not distinguish zero from non-zero effects")
	}

	plain := &Rejection{Code: RejectionCode, Field: "field", State: "STATE", Version: "v1"}
	if !strings.Contains(plain.Error(), "field=field") || !errors.Is(plain, ErrRejected) {
		t.Fatalf("plain rejection=%q, errors.Is=%v", plain.Error(), errors.Is(plain, ErrRejected))
	}
	cause := &Rejection{Code: RejectionCode, Field: "purpose", State: "MISSING", Version: "v1", Cause: ErrPurpose}
	if !errors.Is(cause, ErrRejected) || !errors.Is(cause, ErrPurpose) || len(cause.Unwrap()) != 2 {
		t.Fatalf("cause rejection did not preserve both sentinels: unwrap=%v", cause.Unwrap())
	}
	if IsRejected(nil) || IsRejected(errors.New("other")) {
		t.Fatal("IsRejected accepted an unrelated error")
	}
	if got := rejection(ErrOwner, "owner", "MISSING", "v2"); !errors.Is(got, ErrOwner) || !IsRejected(got) {
		t.Fatalf("rejection did not wrap cause: %v", got)
	}

	classifications := []struct {
		err          error
		field, state string
	}{
		{ErrIdentity, "identity", "MISSING_OR_INVALID"}, {ErrPurpose, "purpose", "MISSING"},
		{ErrFeatures, "features", "MISSING_OR_INVALID"}, {ErrSource, "source", "MISSING_OR_INVALID"},
		{ErrRetention, "retention", "MISSING_OR_INVALID"}, {ErrProtectedPolicy, "protected_attribute_policy", "MISSING"},
		{ErrOwner, "owner", "MISSING"}, {ErrThreshold, "threshold", "MISSING_OR_INVALID"},
		{ErrAction, "action", "MISSING"}, {ErrEvaluation, "evaluation", "MISSING_OR_INVALID"},
		{ErrSignalBinding, "signal_ids", "UNBOUND"}, {errors.New("other"), "publication", "INVALID"},
	}
	for _, tc := range classifications {
		field, state := classify(tc.err)
		if field != tc.field || state != tc.state {
			t.Errorf("classify(%v)=(%s,%s), want (%s,%s)", tc.err, field, state, tc.field, tc.state)
		}
	}

	common := func() error {
		return validateCommon("id", "1", "purpose", []Feature{{Name: "f", Description: "d"}}, []Source{{Name: "s", Quality: "q"}}, "", 1, "policy", "owner")
	}
	if err := common(); err != nil {
		t.Fatalf("retention-days form rejected: %v", err)
	}
	commonCases := []struct {
		name string
		call func() error
		want error
	}{
		{"feature field", func() error {
			return validateCommon("id", "1", "p", []Feature{{Name: "", Description: "d"}}, []Source{{Name: "s", Quality: "q"}}, "r", 0, "x", "o")
		}, ErrFeatures},
		{"no sources", func() error {
			return validateCommon("id", "1", "p", []Feature{{Name: "f", Description: "d"}}, nil, "r", 0, "x", "o")
		}, ErrSource},
		{"source field", func() error {
			return validateCommon("id", "1", "p", []Feature{{Name: "f", Description: "d"}}, []Source{{Name: "", Quality: "q"}}, "r", 0, "x", "o")
		}, ErrSource},
		{"negative retention", func() error {
			return validateCommon("id", "1", "p", []Feature{{Name: "f", Description: "d"}}, []Source{{Name: "s", Quality: "q"}}, "r", -1, "x", "o")
		}, ErrRetention},
	}
	for _, tc := range commonCases {
		t.Run(tc.name, func(t *testing.T) {
			if !errors.Is(tc.call(), tc.want) {
				t.Fatalf("error=%v, want %v", tc.call(), tc.want)
			}
		})
	}

	d := DetectorDefinition{
		ID: "d", Version: "1", Purpose: "p", SignalIDs: []string{"s"},
		Features: []Feature{{Name: "f", Description: "d"}}, Sources: []Source{{Name: "s", Quality: "q"}},
		Retention: "r", ProtectedAttributePolicy: "p", Owner: "o",
		Threshold: Threshold{Metric: "m", Value: 1, Window: "w"}, Action: "a",
		Evaluation: EvaluationPlan{Method: "m", Dataset: "d", Metrics: "x"},
	}
	for _, tc := range []struct {
		name   string
		mutate func(*DetectorDefinition)
		want   error
	}{
		{"no signal ids", func(d *DetectorDefinition) { d.SignalIDs = nil }, ErrSource},
		{"blank signal id", func(d *DetectorDefinition) { d.SignalIDs = []string{" "} }, ErrSource},
		{"blank metric", func(d *DetectorDefinition) { d.Threshold.Metric = "" }, ErrThreshold},
		{"blank window", func(d *DetectorDefinition) { d.Threshold.Window = "" }, ErrThreshold},
		{"nan threshold", func(d *DetectorDefinition) { d.Threshold.Value = math.NaN() }, ErrThreshold},
		{"infinite threshold", func(d *DetectorDefinition) { d.Threshold.Value = math.Inf(1) }, ErrThreshold},
		{"blank action", func(d *DetectorDefinition) { d.Action = " " }, ErrAction},
		{"blank method", func(d *DetectorDefinition) { d.Evaluation.Method = "" }, ErrEvaluation},
		{"blank metrics", func(d *DetectorDefinition) { d.Evaluation.Metrics = "" }, ErrEvaluation},
	} {
		t.Run("detector "+tc.name, func(t *testing.T) {
			copy := d
			tc.mutate(&copy)
			if !errors.Is(copy.Validate(), tc.want) {
				t.Fatalf("error=%v, want %v", copy.Validate(), tc.want)
			}
		})
	}
	if _, err := digest(func() {}); err == nil {
		t.Fatal("digest accepted an unsupported JSON value")
	}
}

func TestActivitySignalAndDetectorDigestBoundaries(t *testing.T) {
	ref := Ref{Type: " worker ", ID: " entity "}
	if !ref.Valid() || (Ref{Type: " ", ID: "id"}).Valid() || (Ref{Type: "type", ID: " "}).Valid() {
		t.Fatal("Ref.Valid did not enforce both opaque reference fields")
	}
	s := ActivitySignal{ID: "s", Kind: SignalKindSensitiveRead, Subject: ref, Actor: Ref{Type: "user", ID: "u"}, Tenant: "t", ObservedAt: time.Date(2026, 9, 1, 12, 0, 0, 0, time.FixedZone("offset", 3600)), SourceSystem: "src"}
	if err := s.Validate(); err != nil {
		t.Fatalf("valid signal rejected: %v", err)
	}
	if class, ok := s.Classification(); !ok || class != "SENSITIVE_DATA_ACCESS" {
		t.Fatalf("signal classification=(%q,%v)", class, ok)
	}
	if class, ok := (ActivitySignal{Kind: SignalKind("unknown")}).Classification(); ok || class != "" {
		t.Fatalf("invalid signal classification=(%q,%v)", class, ok)
	}
	sameInstant := s
	sameInstant.ObservedAt = s.ObservedAt.UTC()
	first, err := s.Digest()
	if err != nil {
		t.Fatal(err)
	}
	second, err := sameInstant.Digest()
	if err != nil || first != second || !strings.HasPrefix(first, "sha256:") {
		t.Fatalf("UTC-normalized digests differ: %q %q err=%v", first, second, err)
	}
	s.RawContent = "  "
	if _, err := s.Digest(); err != nil {
		t.Fatalf("whitespace-only boundary content changed digest behavior: %v", err)
	}

	v := hardeningVersion(SignalKindSensitiveRead)
	if v.ConsumesUndeclaredKind(SignalKindSensitiveRead) || !v.ConsumesUndeclaredKind(SignalKindBulkExport) {
		t.Fatal("ConsumesUndeclaredKind returned the wrong result")
	}
	mutatedInputs := append([]SignalKind(nil), v.DeclaredInputs...)
	mutatedOutputs := append([]string(nil), v.DeclaredOutputs...)
	mutatedThresholds := append([]ThresholdRef(nil), v.Thresholds...)
	v.DeclaredInputs = append(v.DeclaredInputs, SignalKindBulkExport)
	v.DeclaredOutputs[0] = "MUTATED"
	v.Thresholds[0].ID = "MUTATED"
	v.DeclaredInputs = mutatedInputs
	v.DeclaredOutputs = mutatedOutputs
	v.Thresholds = mutatedThresholds
	other := v
	other.DeclaredInputs = []SignalKind{SignalKindBulkExport, SignalKindSensitiveRead}
	other.DeclaredOutputs = []string{"OTHER", "REVIEW"}
	other.Thresholds = []ThresholdRef{{ID: "other"}, {ID: "window-1"}}
	d1, err := v.Digest()
	if err != nil {
		t.Fatal(err)
	}
	d2, err := other.Digest()
	if err == nil && d1 == d2 {
		t.Fatal("changing detector contract did not change digest")
	}
}

func TestSignalKindsReturnsIndependentSortedCopy(t *testing.T) {
	a := SignalKinds()
	if len(a) != 5 || a[0] != SignalKindAccessGrant {
		t.Fatalf("SignalKinds order=%v, want sorted governed vocabulary", a)
	}
	a[0] = SignalKind("mutated")
	b := SignalKinds()
	if b[0] != SignalKindAccessGrant || b[0].Valid() == false {
		t.Fatalf("SignalKinds returned aliased or invalid state: %v", b)
	}
}

func TestExplainActivitySignalAndDetectorInvalidBranches(t *testing.T) {
	s := ActivitySignal{ID: "s", Kind: SignalKindSensitiveRead, Subject: Ref{Type: "worker", ID: "w"}, Actor: Ref{Type: "user", ID: "u"}, Tenant: "t", ObservedAt: time.Now().UTC(), SourceSystem: "src"}
	v := hardeningVersion(SignalKindSensitiveRead)
	for _, tc := range []struct {
		name, want string
		mutateS    func(*ActivitySignal)
		mutateV    func(*DetectorVersion)
	}{
		{"invalid signal", ExplainReasonSignalInvalid, func(s *ActivitySignal) { s.Tenant = "" }, nil},
		{"invalid detector", ExplainReasonDetectorVersionInvalid, nil, func(v *DetectorVersion) { v.ActivatedAt = time.Time{} }},
		{"undeclared kind", ExplainReasonNotDeclaredInput, func(s *ActivitySignal) { s.Kind = SignalKindBulkExport }, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ss, vv := s, v
			if tc.mutateS != nil {
				tc.mutateS(&ss)
			}
			if tc.mutateV != nil {
				tc.mutateV(&vv)
			}
			exp := Explain(ss, vv)
			if exp.Applicable || exp.Reason != tc.want {
				t.Fatalf("explanation=%+v, want reason=%s and denial", exp, tc.want)
			}
		})
	}
	matched := Explain(s, v)
	if !matched.Applicable || matched.MatchedInput != s.Kind || matched.Reason != ExplainReasonDeclaredInput {
		t.Fatalf("declared explanation=%+v", matched)
	}
}

func TestRegistryRejectionClassificationAndMissingLookups(t *testing.T) {
	reg := NewRegistry()
	if _, ok := reg.Active("missing"); ok {
		t.Fatal("empty registry reported an active detector")
	}
	if _, ok := reg.Get("missing", "1.0.0"); ok {
		t.Fatal("empty registry returned a missing version")
	}
	for _, tc := range []struct {
		err          error
		field, state string
	}{
		{ErrDetectorVersionIdentity, "identity", "MISSING_OR_INVALID"},
		{ErrDetectorVersionSemver, "semver", "MALFORMED"},
		{ErrDetectorVersionInputs, "declared_inputs", "MISSING"},
		{ErrDetectorVersionInputKind, "declared_inputs", "UNGOVERNED_KIND"},
		{ErrDetectorVersionOutputs, "declared_outputs", "MISSING_OR_INVALID"},
		{ErrDetectorVersionThreshold, "thresholds", "UNREFERENCED"},
		{ErrDetectorVersionActivation, "activated_at", "MISSING"},
		{errors.New("other"), "detector_version", "INVALID"},
	} {
		field, state := classifyDetectorVersion(tc.err)
		if field != tc.field || state != tc.state {
			t.Errorf("classifyDetectorVersion=%s,%s", field, state)
		}
	}
	nilCause := &RegistryRejection{Code: RegistryRejectionCode, DetectorID: "d", Semver: "1"}
	if !errors.Is(nilCause, ErrRegistryRejected) || len(nilCause.Unwrap()) != 1 || !strings.Contains(nilCause.Error(), "detector=d") {
		t.Fatalf("registry rejection=%+v unwrap=%v", nilCause, nilCause.Unwrap())
	}
	if IsRegistryRejected(nil) || IsRegistryRejected(errors.New("other")) {
		t.Fatal("IsRegistryRejected accepted unrelated error")
	}
}
