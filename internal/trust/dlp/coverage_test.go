package dlp

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/trust/outbound"
)

func coverageOutbound(t *testing.T) *outbound.Policy {
	t.Helper()
	p, err := outbound.NewPolicy(outbound.Destination{Name: "dest", TrustBundleRef: "bundle", Purposes: []string{"purpose"}, DataClasses: []string{string(ClassPublic), string(ClassPII), string(ClassInternal)}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestVocabulariesAndDetectorValidation(t *testing.T) {
	for _, class := range []DataClass{ClassPublic, ClassInternal, ClassPII, ClassCompensation, ClassBank, ClassMedical, ClassImmigration, ClassCase, ClassSpecialCategory} {
		if !class.Valid() {
			t.Errorf("class %q reported invalid", class)
		}
	}
	if DataClass("UNKNOWN").Valid() {
		t.Fatal("unknown class reported valid")
	}
	for _, severity := range []Severity{SeverityInfo, SeverityLow, SeverityMedium, SeverityHigh, SeverityCritical} {
		if !severity.Valid() {
			t.Errorf("severity %q reported invalid", severity)
		}
	}
	if Severity("UNKNOWN").Valid() {
		t.Fatal("unknown severity reported valid")
	}
	for _, tc := range []struct {
		name string
		id   string
		c    DataClass
		s    Severity
		p    string
	}{{"missing id", "", ClassPII, SeverityHigh, "x"}, {"padded id", " id", ClassPII, SeverityHigh, "x"}, {"invalid class", "id", DataClass("x"), SeverityHigh, "x"}, {"invalid severity", "id", ClassPII, Severity("x"), "x"}, {"no matcher", "id", ClassPII, SeverityHigh, ""}, {"bad regexp", "id", ClassPII, SeverityHigh, "["}} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewDetector(tc.id, tc.c, tc.s, tc.p); !errors.Is(err, ErrInvalidDetector) {
				t.Fatalf("NewDetector error = %v, want ErrInvalidDetector", err)
			}
		})
	}
	d := Detector{ID: "matcher", Class: ClassInternal, Severity: SeverityLow, Matcher: func([]byte) []Location { return []Location{{Path: "body", Start: 1, End: 3}} }}
	inspector, err := NewInspector(d)
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := inspector.Inspect([]byte("abcd"))
	if err != nil || len(inspection.Findings) != 1 || inspection.Findings[0].Location.Path != "body" {
		t.Fatalf("matcher inspection = %+v, %v", inspection, err)
	}
	if _, err := NewInspector(d, d); !errors.Is(err, ErrInvalidDetector) {
		t.Fatalf("duplicate detector error = %v", err)
	}
	var nilInspector *Inspector
	if _, err := nilInspector.Inspect(nil); !errors.Is(err, ErrInvalidInspector) {
		t.Fatalf("nil inspector error = %v", err)
	}
}

func TestInspection_FindingsCopiesDigestAndInvalidLocations(t *testing.T) {
	inspector, err := NewInspector(Detector{ID: "bad-path", Class: ClassPII, Severity: SeverityHigh, Matcher: func([]byte) []Location { return []Location{{Path: " bad", Start: 0, End: 1}} }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inspector.Inspect([]byte("x")); !errors.Is(err, ErrInvalidFinding) {
		t.Fatalf("padded path error = %v", err)
	}
	inspection := Inspection{Findings: []Finding{{DetectorID: "d", Class: ClassPII, Severity: SeverityHigh, Location: Location{Start: 0, End: 1}}}}
	copyFindings := inspection.FindingsCopy()
	copyFindings[0].DetectorID = "changed"
	if inspection.Findings[0].DetectorID == "changed" || inspection.Digest() == "" || DigestPayload([]byte("x")) == DigestPayload([]byte("y")) || DigestFindings(inspection.Findings) == DigestFindings(copyFindings) {
		t.Fatal("finding copy/digest helpers failed")
	}
}

func TestPolicy_ConstructionEvaluationAndDecideBranches(t *testing.T) {
	if _, err := NewPolicy(nil); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("nil egress error = %v", err)
	}
	base := func(c Clearance) (*Policy, error) { return NewPolicy(coverageOutbound(t), c) }
	for _, tc := range []Clearance{{Destination: " dest", DataClass: ClassPII, Decision: Allow}, {Destination: "dest", DataClass: ClassPII, Decision: Decision("bad")}, {Destination: "dest", Decision: Allow}, {Destination: "dest", Classes: []DataClass{DataClass("bad")}, Decision: Allow}} {
		if _, err := base(tc); !errors.Is(err, ErrInvalidPolicy) {
			t.Fatalf("NewPolicy(%+v) error = %v", tc, err)
		}
	}
	if _, err := NewPolicy(coverageOutbound(t), Clearance{Destination: "dest", Classes: []DataClass{ClassPII}, DataClass: ClassPII, Decision: Allow}); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("duplicate class error = %v", err)
	}
	policies := map[string]*Policy{}
	for name, clearance := range map[string]Clearance{"allow": {Destination: "dest", DataClass: ClassPII, Decision: Allow}, "redact": {Destination: "dest", DataClass: ClassPII, Decision: Redact}, "refuse": {Destination: "dest", DataClass: ClassPII, Decision: Refuse}, "approval": {Destination: "dest", DataClass: ClassPII, Decision: ApprovalRequired}} {
		var err error
		policies[name], err = base(clearance)
		if err != nil {
			t.Fatal(err)
		}
	}
	request := func(class DataClass, finding bool) DecisionRequest {
		r := DecisionRequest{Destination: "dest", Purpose: "purpose", Principal: "principal", DeclaredClasses: []DataClass{class}}
		if finding {
			r.Inspection.Findings = []Finding{{DetectorID: "d", Class: class, Severity: SeverityHigh, Location: Location{Start: 0, End: 1}}}
		}
		return r
	}
	for name, want := range map[string]Decision{"allow": Allow, "redact": Redact, "refuse": Refuse, "approval": ApprovalRequired} {
		got, err := policies[name].Decide(request(ClassPII, true))
		if err != nil || got != want {
			t.Fatalf("Decide(%s) = %s, %v, want %s", name, got, err, want)
		}
	}
	if eval, err := policies["allow"].Evaluate(request(ClassPII, true)); err != nil || !strings.Contains(eval.Reason, "cleared") || eval.FindingsDigest == "" {
		t.Fatalf("allowed evaluation = %+v, %v", eval, err)
	}
	public, err := policies["allow"].Evaluate(DecisionRequest{Destination: "dest", Purpose: "purpose", Principal: "principal"})
	if err != nil || public.Decision != Allow || len(public.Classes) != 1 || public.Classes[0] != ClassPublic {
		t.Fatalf("empty-class evaluation = %+v, %v", public, err)
	}
	if got, err := policies["allow"].Evaluate(request(ClassSpecialCategory, false)); err != nil || got.Decision != Refuse {
		t.Fatalf("uncleared outbound class = %+v, %v", got, err)
	}
	var nilPolicy *Policy
	if _, err := nilPolicy.Evaluate(request(ClassPII, false)); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("nil Evaluate error = %v", err)
	}
	for _, req := range []DecisionRequest{{Destination: " dest", Purpose: "purpose", Principal: "principal"}, {Destination: "dest", Purpose: " purpose", Principal: "principal"}, {Destination: "dest", Purpose: "purpose", Principal: " principal"}, {Destination: "dest", Purpose: "purpose", Principal: "principal", DeclaredClasses: []DataClass{DataClass("bad")}}, {Destination: "dest", Purpose: "purpose", Principal: "principal", Inspection: Inspection{Findings: []Finding{{DetectorID: "d", Class: ClassPII, Severity: SeverityHigh, Location: Location{Start: 0, End: 0}}}}}} {
		if _, err := policies["allow"].Evaluate(req); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("invalid request %+v error = %v", req, err)
		}
	}
}

func TestReceiptLog_AppendCopiesAndVerifiesChain(t *testing.T) {
	input := ReceiptInput{Destination: "dest", Purpose: "purpose", Principal: "principal", Payload: []byte("payload"), Decision: Allow}
	var nilLog *ReceiptLog
	if _, err := nilLog.Append(input); !errors.Is(err, ErrInvalidReceipt) {
		t.Fatalf("nil Append error = %v", err)
	}
	log := NewLedger()
	if log == nil {
		t.Fatal("NewLedger returned nil")
	}
	for _, tc := range []ReceiptInput{{Purpose: "purpose", Principal: "principal", Decision: Allow}, {Destination: " dest", Purpose: "purpose", Principal: "principal", Decision: Allow}, {Destination: "dest", Purpose: "purpose", Principal: "principal", Decision: Decision("bad")}, {Destination: "dest", Purpose: "purpose", Principal: "principal", PayloadDigest: "bad", Decision: Allow}, {Destination: "dest", Purpose: "purpose", Principal: "principal", Payload: []byte("x"), PayloadDigest: DigestPayload([]byte("y")), Decision: Allow}} {
		if _, err := log.Append(tc); !errors.Is(err, ErrInvalidReceipt) {
			t.Fatalf("invalid input %+v error = %v", tc, err)
		}
	}
	first, err := log.Append(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := log.Append(ReceiptInput{Destination: "dest", Purpose: "purpose", Principal: "principal", PayloadDigest: DigestPayload([]byte("x")), FindingsDigest: DigestFindings(nil), Decision: Redact})
	if err != nil || second.Sequence != 2 || second.PreviousDigest != first.Digest {
		t.Fatalf("second receipt = %+v, %v", second, err)
	}
	if len(log.Receipts()) != 2 || log.Verify() != nil || DigestOfReceipt(first) != first.Digest {
		t.Fatalf("receipt chain invalid: %+v err=%v", log.Receipts(), log.Verify())
	}
	receipts := log.Receipts()
	receipts[0].Decision = Refuse
	if log.Receipts()[0].Decision == Refuse {
		t.Fatal("Receipts exposed backing storage")
	}
	log.receipts[0].Decision = Refuse
	if !errors.Is(log.Verify(), ErrReceiptTampered) {
		t.Fatal("tampered receipt chain verified")
	}
	var nilVerify *ReceiptLog
	if !errors.Is(nilVerify.Verify(), ErrReceiptTampered) {
		t.Fatal("nil Verify did not report tamper")
	}
}
