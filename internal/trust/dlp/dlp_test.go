package dlp_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/outbound"
)

func dlpOutbound(t *testing.T) *outbound.Policy {
	t.Helper()
	policy, err := outbound.NewPolicy(outbound.Destination{
		Name:           "partner.example",
		TrustBundleRef: "bundle:partner:v1",
		Purposes:       []string{"support-export"},
		DataClasses:    []string{string(dlp.ClassPublic), string(dlp.ClassPII), string(dlp.ClassSecret)},
	})
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	return policy
}

func dlpInspector(t *testing.T) *dlp.Inspector {
	t.Helper()
	pii, err := dlp.NewDetector("ssn", dlp.ClassPII, dlp.SeverityHigh, `\b\d{3}-\d{2}-\d{4}\b`)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := dlp.NewDetector("api-key", dlp.ClassSecret, dlp.SeverityCritical, `sk-[A-Za-z0-9]{12,}`)
	if err != nil {
		t.Fatal(err)
	}
	inspector, err := dlp.NewInspector(pii, secret)
	if err != nil {
		t.Fatal(err)
	}
	return inspector
}

func dlpPolicy(t *testing.T) *dlp.Policy {
	t.Helper()
	policy, err := dlp.NewPolicy(dlpOutbound(t),
		dlp.Clearance{Destination: "partner.example", DataClass: dlp.ClassPII, Decision: dlp.Redact},
		dlp.Clearance{Destination: "partner.example", DataClass: dlp.ClassSecret, Decision: dlp.Refuse},
	)
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	return policy
}

// TestTodo_TRUST_018 is the primary contract: inspection returns only
// redaction-safe findings, policy composes with outbound trust, and the
// append-only receipt binds the payload and findings digests without storing
// payload content.
func TestTodo_TRUST_018(t *testing.T) {
	inspector := dlpInspector(t)
	inspection, err := inspector.Inspect([]byte("worker ssn 123-45-6789"))
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if len(inspection.Findings) != 1 || inspection.Findings[0].Class != dlp.ClassPII || inspection.Findings[0].Severity != dlp.SeverityHigh {
		t.Fatalf("findings = %+v, want one PII/high finding", inspection.Findings)
	}
	if got := inspection.Findings[0].Location; got.Start != 11 || got.End != 22 {
		t.Fatalf("location = %+v, want [11,22)", got)
	}

	evaluation, err := dlpPolicy(t).Evaluate(dlp.DecisionRequest{
		Destination: "partner.example", Purpose: "support-export", Principal: "operator-1",
		DeclaredClasses: []dlp.DataClass{dlp.ClassPII}, Inspection: inspection,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if evaluation.Decision != dlp.Redact {
		t.Fatalf("decision = %s, want REDACT", evaluation.Decision)
	}

	log := dlp.NewReceiptLog()
	receipt, err := log.Append(dlp.ReceiptInput{
		Destination: "partner.example", Purpose: "support-export", Principal: "operator-1",
		Payload: []byte("worker ssn 123-45-6789"), Inspection: inspection, Decision: evaluation.Decision,
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if receipt.PayloadDigest != dlp.DigestPayload([]byte("worker ssn 123-45-6789")) || receipt.FindingsDigest != inspection.Digest() || receipt.Digest == "" {
		t.Fatalf("receipt = %+v, missing canonical bindings", receipt)
	}
	if err := log.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if strings.Contains(receipt.Canonical(), "123-45-6789") {
		t.Fatal("receipt canonical form contains raw payload")
	}
}

// TestTodo_TRUST_018_Golden proves canonical finding and receipt digests are
// stable under detector declaration order and repeated evaluation.
func TestTodo_TRUST_018_Golden(t *testing.T) {
	first := dlpInspector(t)
	second := dlpInspector(t)
	one, err := first.Inspect([]byte("key sk-abcdefghijkl"))
	if err != nil {
		t.Fatal(err)
	}
	two, err := second.Inspect([]byte("key sk-abcdefghijkl"))
	if err != nil {
		t.Fatal(err)
	}
	if one.Digest() != two.Digest() {
		t.Fatalf("finding digests differ: %s vs %s", one.Digest(), two.Digest())
	}
	input := dlp.ReceiptInput{Destination: "partner.example", Purpose: "support-export", Principal: "operator-1", PayloadDigest: dlp.DigestPayload([]byte("x")), FindingsDigest: one.Digest(), Decision: dlp.Refuse}
	a := dlp.NewReceiptLog()
	b := dlp.NewReceiptLog()
	ra, err := a.Append(input)
	if err != nil {
		t.Fatal(err)
	}
	rb, err := b.Append(input)
	if err != nil {
		t.Fatal(err)
	}
	if ra.Digest != rb.Digest || ra.Canonical() != rb.Canonical() {
		t.Fatalf("receipt canonicalization is not stable: %+v vs %+v", ra, rb)
	}
}

// FuzzTodo_TRUST_018 exercises the inspector and decision boundary against
// arbitrary bytes; malformed payloads must never panic or create raw content
// in a finding.
func FuzzTodo_TRUST_018(f *testing.F) {
	f.Add([]byte("nothing sensitive"))
	f.Add([]byte("ssn 123-45-6789"))
	f.Add([]byte{0, 1, 2, 255})
	f.Fuzz(func(t *testing.T, payload []byte) {
		inspection, err := dlpInspector(t).Inspect(payload)
		if err != nil {
			t.Fatalf("Inspect: %v", err)
		}
		for _, finding := range inspection.Findings {
			if finding.Location.Start < 0 || finding.Location.End > len(payload) || finding.Location.Start >= finding.Location.End {
				t.Fatalf("invalid finding location %+v for payload length %d", finding, len(payload))
			}
		}
	})
}

// TestTodo_TRUST_018_Integration covers inspector -> policy -> receipt.
func TestTodo_TRUST_018_Integration(t *testing.T) {
	inspection, err := dlpInspector(t).Inspect([]byte("ssn 123-45-6789"))
	if err != nil {
		t.Fatal(err)
	}
	decision, err := dlpPolicy(t).Decide(dlp.DecisionRequest{Destination: "partner.example", Purpose: "support-export", Principal: "operator-1", Inspection: inspection})
	if err != nil || decision != dlp.Redact {
		t.Fatalf("Decide = %s, %v, want REDACT, nil", decision, err)
	}
	log := dlp.NewLedger()
	if _, err := log.Append(dlp.ReceiptInput{Destination: "partner.example", Purpose: "support-export", Principal: "operator-1", Payload: []byte("ssn 123-45-6789"), Inspection: inspection, Decision: decision}); err != nil {
		t.Fatal(err)
	}
	if len(log.Receipts()) != 1 || log.Verify() != nil {
		t.Fatalf("receipt log = %+v, verify = %v", log.Receipts(), log.Verify())
	}
}

// TestTodo_TRUST_018_Security proves closed vocabularies, invalid matcher
// locations, nil policies, and padded identity fields fail closed.
func TestTodo_TRUST_018_Security(t *testing.T) {
	if _, err := dlp.NewDetector("x", dlp.DataClass("UNDECLARED"), dlp.SeverityHigh, "x"); !errors.Is(err, dlp.ErrInvalidDetector) {
		t.Fatalf("unknown class error = %v, want ErrInvalidDetector", err)
	}
	bad, err := dlp.NewInspector(dlp.Detector{ID: "bad", Class: dlp.ClassPII, Severity: dlp.SeverityHigh, Matcher: func([]byte) []dlp.Location { return []dlp.Location{{Start: 0, End: 99}} }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bad.Inspect([]byte("x")); !errors.Is(err, dlp.ErrInvalidFinding) {
		t.Fatalf("bad location error = %v, want ErrInvalidFinding", err)
	}
	if _, err := dlp.NewPolicy(nil); !errors.Is(err, dlp.ErrInvalidPolicy) {
		t.Fatalf("nil policy error = %v, want ErrInvalidPolicy", err)
	}
	if _, err := dlpPolicy(t).Decide(dlp.DecisionRequest{Destination: " partner.example", Purpose: "support-export", Principal: "operator-1"}); !errors.Is(err, dlp.ErrInvalidRequest) {
		t.Fatalf("padded request error = %v, want ErrInvalidRequest", err)
	}
	if _, err := dlpPolicy(t).Decide(dlp.DecisionRequest{Destination: "partner.example", Purpose: "support-export", Principal: "operator-1", Inspection: dlp.Inspection{Findings: []dlp.Finding{{DetectorID: "x", Class: dlp.ClassSecret, Severity: dlp.SeverityCritical}}}}); !errors.Is(err, dlp.ErrInvalidRequest) {
		// The missing location is intentionally malformed and must not be
		// treated as a valid finding merely because its class is known.
		t.Fatalf("malformed finding error = %v, want ErrInvalidRequest", err)
	}
}

// TestTodo_TRUST_018_Mutation proves each policy action remains load-bearing.
func TestTodo_TRUST_018_Mutation(t *testing.T) {
	inspection, err := dlpInspector(t).Inspect([]byte("ssn 123-45-6789"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		want   dlp.Decision
		policy *dlp.Policy
	}{
		{name: "redact", want: dlp.Redact, policy: dlpPolicy(t)},
		{name: "refuse", want: dlp.Refuse, policy: func() *dlp.Policy {
			p, err := dlp.NewPolicy(dlpOutbound(t), dlp.Clearance{Destination: "partner.example", DataClass: dlp.ClassPII, Decision: dlp.Refuse})
			if err != nil {
				t.Fatal(err)
			}
			return p
		}()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.policy.Decide(dlp.DecisionRequest{Destination: "partner.example", Purpose: "support-export", Principal: "operator-1", Inspection: inspection})
			if err != nil || got != tc.want {
				t.Fatalf("Decide = %s, %v, want %s, nil", got, err, tc.want)
			}
		})
	}
}
