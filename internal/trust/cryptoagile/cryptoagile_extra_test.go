package cryptoagile

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var errExtraKeySource = errors.New("extra key source failure")

type extraSigner struct{}

func (extraSigner) Sign(string, []byte) ([]byte, error) { return nil, errExtraKeySource }

type extraVerifier struct{ result bool }

func (v extraVerifier) Verify(string, []byte, []byte) (bool, error) { return v.result, nil }

func TestEnvelopeSignerVerifier_PropagateProviderAndEnvelopeFailures(t *testing.T) {
	if _, err := NewEnvelopeSigner(extraSigner{}).Sign("suite", []byte("m")); !errors.Is(err, errExtraKeySource) {
		t.Fatalf("provider signing error = %v, want wrapped provider error", err)
	}
	registry := NewRegistry()
	if err := registry.Register(AlgorithmSuite{ID: "suite", Kind: KindSignature, Status: StatusActive, ActivatedAt: day(0)}); err != nil {
		t.Fatal(err)
	}
	verifier := NewEnvelopeVerifier(registry, extraVerifier{result: false})
	if err := verifier.Verify([]byte("m"), Envelope{SuiteID: "suite", Signature: []byte("bad")}); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("false provider verification = %v, want ErrSignatureInvalid", err)
	}
	providerErrorVerifier := NewEnvelopeVerifier(registry, extraVerifierWithError{})
	if err := providerErrorVerifier.Verify([]byte("m"), Envelope{SuiteID: "suite", Signature: []byte("bad")}); !errors.Is(err, errExtraKeySource) {
		t.Fatalf("provider verification error = %v, want wrapped provider error", err)
	}
	if _, err := verifier.VerifyAny([]byte("m"), nil); err == nil {
		t.Fatal("VerifyAny accepted an empty envelope list")
	}
	allFailed, err := verifier.VerifyAny([]byte("m"), []Envelope{{SuiteID: "", Signature: []byte{1}}, {SuiteID: "missing", Signature: []byte{1}}})
	if err == nil || allFailed.SuiteID != "" || len(allFailed.Signature) != 0 || !errors.Is(err, ErrStrippedSuiteID) || !errors.Is(err, ErrSuiteNotFound) {
		t.Fatalf("VerifyAny all failures = %+v, %v, want joined refusal reasons", allFailed, err)
	}
	var retired *RetiredSuiteError
	retiredErr := (&RetiredSuiteError{SuiteID: "old", RetiredAt: day(3)}).Error()
	if !strings.Contains(retiredErr, "old") || !strings.Contains(retiredErr, day(3).Format(time.RFC3339)) || errors.As(errors.New(retiredErr), &retired) {
		t.Fatalf("RetiredSuiteError formatting = %q", retiredErr)
	}
}

type extraVerifierWithError struct{}

func (extraVerifierWithError) Verify(string, []byte, []byte) (bool, error) {
	return false, errExtraKeySource
}

func TestEnvelope_BindingAndDecodeBoundaries(t *testing.T) {
	if string(bindSuite("ab", []byte("c"))) == string(bindSuite("a", []byte("bc"))) {
		t.Fatal("suite binding allowed a length-prefix collision")
	}
	for _, encoded := range []string{"cryptoagile.v0:suite:00", "cryptoagile.v1:suite", "cryptoagile.v1:suite:0g", "cryptoagile.v1::"} {
		if _, err := DecodeEnvelope(encoded); !errors.Is(err, ErrEnvelopeFormat) {
			t.Fatalf("DecodeEnvelope(%q) = %v, want ErrEnvelopeFormat", encoded, err)
		}
	}
}

func TestRegistry_TransitionCoversStatusBranchesAndDoesNotMutateOnFailure(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(AlgorithmSuite{ID: "dual", Kind: KindMAC, Status: StatusRetired, ActivatedAt: day(0), RetiredAt: day(1)}); err != nil {
		t.Fatal(err)
	}
	previous, err := r.Transition("dual", StatusActive, day(2))
	if err != nil || previous.Status != StatusRetired {
		t.Fatalf("retired to active = %+v, %v", previous, err)
	}
	got, _ := r.Get("dual")
	if got.Status != StatusActive || !got.ActivatedAt.Equal(day(0)) || !got.RetiredAt.Equal(day(1)) {
		t.Fatalf("transition retained incorrect lifecycle timestamps: %+v", got)
	}
	if _, err := r.Transition("dual", Status("BROKEN"), day(3)); !errors.Is(err, ErrSuiteStatus) {
		t.Fatalf("invalid transition status = %v, want ErrSuiteStatus", err)
	}
	unchanged, _ := r.Get("dual")
	if unchanged.Status != StatusActive {
		t.Fatalf("invalid transition mutated registry: %+v", unchanged)
	}
}

func TestResume_UsesHighestRecordedIndexWithoutMutatingEvidence(t *testing.T) {
	plan := threeWindowPlan()
	recorded := []Evidence{{Index: 0, ActiveSuiteID: "a", At: day(0)}, {Index: 0, ActiveSuiteID: "a", At: day(0)}}
	copyOfRecorded := append([]Evidence(nil), recorded...)
	got, err := Resume(plan, recorded, day(15))
	if err != nil || len(got) != 1 || got[0].Index != 1 || got[0].DualSuiteID != "a" {
		t.Fatalf("Resume duplicate prefix = %+v, %v", got, err)
	}
	if len(recorded) != len(copyOfRecorded) || recorded[0] != copyOfRecorded[0] || recorded[1] != copyOfRecorded[1] {
		t.Fatal("Resume mutated recorded evidence")
	}
	if _, err := Resume(MigrationPlan{Windows: []Window{{Start: day(0), End: day(0), ActiveSuiteID: "a"}}}, nil, day(0)); !errors.Is(err, ErrPlanBadWindow) {
		t.Fatalf("Resume bad window = %v, want ErrPlanBadWindow", err)
	}
}
