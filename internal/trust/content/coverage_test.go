package content

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func coverageArtifact(t *testing.T) *Artifact {
	t.Helper()
	a, err := New(Ingress{ID: "coverage", Tenant: "tenant", Source: "upload", Filename: "x.pdf", DeclaredMIME: "application/pdf"}, []byte("%PDF-data"), time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestNew_AccessorsAndIngressValidation(t *testing.T) {
	for _, in := range []Ingress{{Tenant: "t", Source: "s"}, {ID: "i", Source: "s"}, {ID: "i", Tenant: "t"}} {
		if _, err := New(in, []byte("x"), time.Unix(1, 0)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("New(%+v) error = %v, want ErrInvalid", in, err)
		}
	}
	if _, err := New(Ingress{ID: "i", Tenant: "t", Source: "s"}, nil, time.Unix(1, 0)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty payload error = %v, want ErrInvalid", err)
	}
	payload := []byte("%PDF-data")
	a, err := New(Ingress{ID: "i", Tenant: "t", Source: "s", Filename: "x.pdf", DeclaredMIME: "application/pdf"}, payload, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	payload[0] = 'X'
	if got := string(a.Payload()); got != "%PDF-data" {
		t.Fatalf("New retained caller payload mutation: %q", got)
	}
	if snapshot := a.Snapshot(); snapshot.State != Quarantined || snapshot.Digest == "" || snapshot.ReceivedAt.IsZero() {
		t.Fatalf("snapshot = %+v", a.Snapshot())
	}
	transitions := a.Transitions()
	if len(transitions) != 1 || transitions[0].To != Quarantined {
		t.Fatalf("transitions = %+v", transitions)
	}
	transitions[0].Reason = "mutated"
	if a.Transitions()[0].Reason == "mutated" {
		t.Fatal("Transitions exposed backing storage")
	}
}

func TestArtifact_ValidationLimitsSignaturesAndBeginValidation(t *testing.T) {
	limits := []struct {
		name string
		set  func(*Policy, *Artifact)
	}{
		{"max bytes", func(p *Policy, _ *Artifact) { p.Limits.MaxBytes = 1 }},
		{"expanded bytes", func(p *Policy, a *Artifact) { p.Limits.MaxExpandedBytes = 1; a.ExpandedBytes = 2 }},
		{"file count", func(p *Policy, a *Artifact) { p.Limits.MaxFiles = 1; a.FileCount = 2 }},
		{"archive depth", func(p *Policy, a *Artifact) { p.Limits.MaxArchiveDepth = 1; a.ArchiveDepth = 2 }},
		{"compression ratio", func(p *Policy, a *Artifact) { p.Limits.MaxCompressionRatio = 1; a.CompressionRatio = 2 }},
		{"extension", func(p *Policy, _ *Artifact) { p.Extensions = map[string]struct{}{".txt": {}} }},
		{"mime", func(p *Policy, _ *Artifact) { p.MIME = map[string]struct{}{"text/plain": {}} }},
		{"signature", func(_ *Policy, a *Artifact) { a.DeclaredMIME = "image/png" }},
	}
	for _, tc := range limits {
		t.Run(tc.name, func(t *testing.T) {
			a := coverageArtifact(t)
			p := policy()
			tc.set(&p, a)
			if err := a.Validate(p, time.Unix(11, 0)); !errors.Is(err, ErrInvalid) || a.Snapshot().State != Rejected {
				t.Fatalf("Validate() = %v, state=%s", err, a.Snapshot().State)
			}
		})
	}
	for _, tc := range []struct {
		mime string
		body []byte
	}{
		{"image/png", []byte{137, 80, 78, 71, 13, 10, 26, 10}}, {"image/jpeg", []byte{255, 216, 255}}, {"application/zip", []byte("PK\x03\x04")}, {"text/plain", []byte("plain")}, {"text/csv", []byte("a,b")}, {"application/json", []byte(`{"ok":true}`)},
	} {
		t.Run(tc.mime, func(t *testing.T) {
			a, err := New(Ingress{ID: tc.mime, Tenant: "t", Source: "s", Filename: "x.bin", DeclaredMIME: tc.mime}, tc.body, time.Unix(1, 0))
			if err != nil {
				t.Fatal(err)
			}
			if err := a.Validate(Policy{Limits: Limits{MaxBytes: 1024}}, time.Unix(2, 0)); err != nil {
				t.Fatalf("signature %s rejected: %v", tc.mime, err)
			}
		})
	}
	a := coverageArtifact(t)
	if err := a.BeginValidation(time.Unix(11, 0)); err != nil || a.Snapshot().State != Validating {
		t.Fatalf("BeginValidation() = %v, state=%s", err, a.Snapshot().State)
	}
	if err := a.BeginValidation(time.Unix(12, 0)); !errors.Is(err, ErrWrongState) {
		t.Fatalf("second BeginValidation() = %v, want ErrWrongState", err)
	}
}

func TestArtifact_ScanTransformRescanAndPromotionBranches(t *testing.T) {
	if err := coverageArtifact(t).Scan(context.Background(), scanner(VerdictSafe), time.Unix(1, 0)); !errors.Is(err, ErrWrongState) {
		t.Fatalf("Scan from quarantine = %v, want ErrWrongState", err)
	}
	a := coverageArtifact(t)
	if err := a.Validate(policy(), time.Unix(11, 0)); err != nil {
		t.Fatal(err)
	}
	if err := a.Scan(context.Background(), func(_ context.Context, got []byte) (Inspection, error) {
		got[0] = 'X'
		return Inspection{Verdict: VerdictSafe, ScannerVersion: "v1"}, errors.New("scanner failed")
	}, time.Unix(12, 0)); err == nil || a.Snapshot().State != Rejected || len(a.Inspections()) != 1 || string(a.Payload()) != "%PDF-data" {
		t.Fatalf("scanner error state=%s inspections=%+v payload=%q err=%v", a.Snapshot().State, a.Inspections(), a.Payload(), err)
	}
	if _, err := a.Promoted(); !errors.Is(err, ErrNotPromotable) {
		t.Fatalf("rejected artifact promoted: %v", err)
	}
	a = coverageArtifact(t)
	if err := a.Rescan(context.Background(), policy(), scanner(VerdictSafe), transformer, time.Unix(20, 0)); err != nil {
		t.Fatal(err)
	}
	derivatives := a.Derivatives()
	if len(derivatives) != 1 || derivatives[0].Revoked || derivatives[0].Digest == "" || derivatives[0].SourceDigest == "" || derivatives[0].Classification != "SAFE" {
		t.Fatalf("derivatives = %+v", derivatives)
	}
	if got := a.Inspections(); len(got) != 1 || got[0].Digest == "" {
		t.Fatalf("inspections = %+v", got)
	}
	if promoted, err := a.Promoted(); err != nil || promoted.Digest != derivatives[0].Digest || promoted.payload != nil {
		t.Fatalf("Promoted() = %+v, %v", promoted, err)
	}
	if err := a.Transform(context.Background(), transformer, time.Unix(21, 0)); !errors.Is(err, ErrWrongState) {
		t.Fatalf("second Transform() = %v, want ErrWrongState", err)
	}
	if err := a.Rescan(context.Background(), policy(), scanner(VerdictUnsafe), transformer, time.Unix(22, 0)); err == nil || a.Snapshot().State != Rejected {
		t.Fatalf("unsafe rescan state=%s err=%v", a.Snapshot().State, err)
	}
	if got := a.Derivatives(); len(got) != 1 || !got[0].Revoked {
		t.Fatalf("revoked history = %+v", got)
	}
	for _, tc := range []struct {
		name      string
		scanner   Scanner
		transform Transformer
		want      State
	}{
		{"scanner unavailable", nil, transformer, Quarantined}, {"scanner unsafe with empty reason", scanner(VerdictUnsafe), transformer, Rejected}, {"transformer unavailable", scanner(VerdictSafe), nil, Quarantined}, {"transformer empty", scanner(VerdictSafe), func(context.Context, []byte) ([]byte, error) { return nil, nil }, Rejected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := coverageArtifact(t)
			err := x.Rescan(context.Background(), policy(), tc.scanner, tc.transform, time.Unix(30, 0))
			if err == nil || x.Snapshot().State != tc.want {
				t.Fatalf("Rescan() state=%s err=%v, want %s and error", x.Snapshot().State, err, tc.want)
			}
		})
	}
}

func TestArtifact_TransformFailureAndPromotionWrongStates(t *testing.T) {
	a := coverageArtifact(t)
	if err := a.Validate(policy(), time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := a.Scan(context.Background(), scanner(VerdictSafe), time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	if err := a.Transform(context.Background(), func(context.Context, []byte) ([]byte, error) { return nil, errors.New("transform failed") }, time.Unix(3, 0)); !strings.Contains(err.Error(), "transform failed") || a.Snapshot().State != Rejected {
		t.Fatalf("failed transform state=%s err=%v", a.Snapshot().State, err)
	}
	for _, state := range []State{Received, Validating, Scanning, Transforming} {
		x := &Artifact{Ingress: Ingress{State: state}}
		if _, err := x.Promoted(); !errors.Is(err, ErrNotPromotable) {
			t.Fatalf("Promoted in state %s = %v", state, err)
		}
	}
}
