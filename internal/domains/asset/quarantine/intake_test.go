package quarantine_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/asset/quarantine"
)

func baseRequest(content []byte, declared quarantine.ContentType) quarantine.UploadRequest {
	return quarantine.UploadRequest{
		Tenant:              "tenant-a",
		Content:             content,
		DeclaredContentType: declared,
		CreatorPrincipalRef: "user:uploader",
		EvidenceID:          "ev:intake:1",
	}
}

// TestTodo_DOC_MAL_001 proves DOC-MAL-001's whole state machine: an upload
// is always recorded QUARANTINED first; a scanner failure, an unsafe
// verdict, an oversized upload, a disallowed content type and a polyglot
// magic-byte mismatch each end in REJECTED with a non-empty reason and never
// reach the scanner unnecessarily; only an upload that clears every check
// and a safe scanner verdict reaches ADMITTED; and the Use gate refuses
// every state but ADMITTED, including a content id nothing was ever
// uploaded for.
func TestTodo_DOC_MAL_001(t *testing.T) {
	ctx := context.Background()

	t.Run("golden path: clean content is quarantined then admitted", func(t *testing.T) {
		store := newFakeStore()
		scanner := &fakeScanner{verdict: quarantine.Verdict{Safe: true}}
		policy := testPolicy(quarantine.ContentTXT)
		req := baseRequest(txtFixture, quarantine.ContentTXT)

		result, err := quarantine.Upload(ctx, store, scanner, policy, req, testNow)
		if err != nil {
			t.Fatalf("upload: %v", err)
		}
		if result.State != quarantine.Admitted {
			t.Fatalf("state = %s, want %s (reason: %s)", result.State, quarantine.Admitted, result.Reason)
		}
		wantID := quarantine.ComputeDigest(txtFixture)
		if result.ContentID != wantID {
			t.Fatalf("content id = %s, want %s", result.ContentID, wantID)
		}
		if len(scanner.calls) != 1 || scanner.calls[0] != wantID {
			t.Fatalf("scanner calls = %v, want exactly one call for %s", scanner.calls, wantID)
		}
		// Two facts were recorded: QUARANTINED, then ADMITTED.
		if got := store.verdictCount(req.Tenant, wantID); got != 2 {
			t.Fatalf("verdict log has %d rows, want 2 (QUARANTINED then ADMITTED)", got)
		}

		state, err := quarantine.Use(ctx, store, req.Tenant, wantID)
		if err != nil {
			t.Fatalf("use gate refused an admitted artifact: %v", err)
		}
		if state.State != quarantine.Admitted {
			t.Fatalf("use gate returned state %s, want %s", state.State, quarantine.Admitted)
		}
	})

	t.Run("a scanner failure is REJECTED-with-reason, never an admit", func(t *testing.T) {
		store := newFakeStore()
		scanner := &fakeScanner{err: errScannerUnavailable}
		policy := testPolicy(quarantine.ContentTXT)
		req := baseRequest(txtFixture, quarantine.ContentTXT)

		result, err := quarantine.Upload(ctx, store, scanner, policy, req, testNow)
		if err != nil {
			t.Fatalf("upload: %v", err)
		}
		if result.State != quarantine.Rejected {
			t.Fatalf("state = %s, want %s", result.State, quarantine.Rejected)
		}
		if result.Reason == "" {
			t.Fatal("a scanner failure must record a non-empty reason")
		}
		assertUseRefused(t, ctx, store, req.Tenant, result.ContentID)
	})

	t.Run("an explicit unsafe verdict is REJECTED with the scanner's own reason", func(t *testing.T) {
		store := newFakeStore()
		scanner := &fakeScanner{verdict: quarantine.Verdict{Safe: false, Reason: "signature match: EICAR-Test-File"}}
		policy := testPolicy(quarantine.ContentTXT)
		req := baseRequest(txtFixture, quarantine.ContentTXT)

		result, err := quarantine.Upload(ctx, store, scanner, policy, req, testNow)
		if err != nil {
			t.Fatalf("upload: %v", err)
		}
		if result.State != quarantine.Rejected {
			t.Fatalf("state = %s, want %s", result.State, quarantine.Rejected)
		}
		if result.Reason != "signature match: EICAR-Test-File" {
			t.Fatalf("reason = %q, want the scanner's own reason", result.Reason)
		}
		assertUseRefused(t, ctx, store, req.Tenant, result.ContentID)
	})

	t.Run("oversized content is REJECTED before the scanner ever runs", func(t *testing.T) {
		store := newFakeStore()
		scanner := &fakeScanner{verdict: quarantine.Verdict{Safe: true}}
		policy := testPolicy(quarantine.ContentTXT)
		policy.MaxContentBytes = 4
		req := baseRequest(txtFixture, quarantine.ContentTXT)

		result, err := quarantine.Upload(ctx, store, scanner, policy, req, testNow)
		if err != nil {
			t.Fatalf("upload: %v", err)
		}
		if result.State != quarantine.Rejected {
			t.Fatalf("state = %s, want %s", result.State, quarantine.Rejected)
		}
		if len(scanner.calls) != 0 {
			t.Fatalf("scanner was called %d times for oversized content, want 0", len(scanner.calls))
		}
	})

	t.Run("a declared type outside the allowlist is REJECTED before the scanner ever runs", func(t *testing.T) {
		store := newFakeStore()
		scanner := &fakeScanner{verdict: quarantine.Verdict{Safe: true}}
		policy := testPolicy(quarantine.ContentPDF) // txt is not allowed
		req := baseRequest(txtFixture, quarantine.ContentTXT)

		result, err := quarantine.Upload(ctx, store, scanner, policy, req, testNow)
		if err != nil {
			t.Fatalf("upload: %v", err)
		}
		if result.State != quarantine.Rejected {
			t.Fatalf("state = %s, want %s", result.State, quarantine.Rejected)
		}
		if len(scanner.calls) != 0 {
			t.Fatalf("scanner was called %d times for a disallowed content type, want 0", len(scanner.calls))
		}
	})

	t.Run("a PDF declared as txt (polyglot/mismatched magic bytes) is REJECTED before the scanner ever runs", func(t *testing.T) {
		store := newFakeStore()
		scanner := &fakeScanner{verdict: quarantine.Verdict{Safe: true}}
		policy := testPolicy(quarantine.ContentTXT, quarantine.ContentPDF)
		req := baseRequest(pdfFixture, quarantine.ContentTXT)

		result, err := quarantine.Upload(ctx, store, scanner, policy, req, testNow)
		if err != nil {
			t.Fatalf("upload: %v", err)
		}
		if result.State != quarantine.Rejected {
			t.Fatalf("state = %s, want %s", result.State, quarantine.Rejected)
		}
		if !strings.Contains(result.Reason, "polyglot") && !strings.Contains(result.Reason, "mismatched") {
			t.Fatalf("reason %q does not explain the magic-byte mismatch", result.Reason)
		}
		if len(scanner.calls) != 0 {
			t.Fatalf("scanner was called %d times for a polyglot mismatch, want 0", len(scanner.calls))
		}
	})

	t.Run("a docx (zip container) declared as docx is not mistaken for a mismatch", func(t *testing.T) {
		store := newFakeStore()
		scanner := &fakeScanner{verdict: quarantine.Verdict{Safe: true}}
		policy := testPolicy(quarantine.ContentDOCX)
		req := baseRequest(zipFixture, quarantine.ContentDOCX)

		result, err := quarantine.Upload(ctx, store, scanner, policy, req, testNow)
		if err != nil {
			t.Fatalf("upload: %v", err)
		}
		if result.State != quarantine.Admitted {
			t.Fatalf("state = %s, want %s (reason: %s)", result.State, quarantine.Admitted, result.Reason)
		}
	})

	t.Run("the use gate refuses a content id nothing was ever uploaded for", func(t *testing.T) {
		store := newFakeStore()
		_, err := quarantine.Use(ctx, store, "tenant-a", "0000000000000000000000000000000000000000000000000000000000000000")
		var notFound quarantine.ErrNotFound
		if !errors.As(err, &notFound) {
			t.Fatalf("error = %v, want ErrNotFound", err)
		}
	})

	t.Run("the use gate refuses a still-quarantined artifact awaiting a verdict", func(t *testing.T) {
		store := newFakeStore()
		contentID := quarantine.ComputeDigest(txtFixture)
		if err := store.RecordQuarantined(ctx, "tenant-a", quarantine.QuarantinedRecord{
			ContentID:           contentID,
			DigestAlgorithm:     quarantine.Algorithm,
			ByteSize:            int64(len(txtFixture)),
			DeclaredContentType: quarantine.ContentTXT,
			SniffedCategory:     quarantine.CategoryText,
			CreatorPrincipalRef: "user:uploader",
			EvidenceID:          "ev:pending",
			Content:             txtFixture,
		}); err != nil {
			t.Fatalf("record quarantined: %v", err)
		}
		assertUseRefused(t, ctx, store, "tenant-a", contentID)
	})
}

// assertUseRefused fails the test unless Use refuses contentID with a typed
// ErrUseRefused naming a non-ADMITTED state.
func assertUseRefused(t *testing.T, ctx context.Context, store quarantine.Store, tenant, contentID string) {
	t.Helper()
	_, err := quarantine.Use(ctx, store, tenant, contentID)
	var refused quarantine.ErrUseRefused
	if !errors.As(err, &refused) {
		t.Fatalf("error = %v, want ErrUseRefused", err)
	}
	if refused.State == quarantine.Admitted {
		t.Fatal("use gate reported ErrUseRefused while naming state ADMITTED")
	}
}

// FuzzTodo_DOC_MAL_001 fuzzes Upload's own invariant, independent of any
// particular fixture: it must never reach ADMITTED when the content's
// sniffed category disagrees with the declared type's expected category,
// and it must never panic on arbitrary bytes.
func FuzzTodo_DOC_MAL_001(f *testing.F) {
	seeds := [][]byte{pdfFixture, pngFixture, jpegFixture, zipFixture, csvFixture, txtFixture, {}, {0x00, 0xFF}, []byte("PK\x03\x04not really a docx")}
	for _, s := range seeds {
		f.Add(s)
	}
	declaredTypes := []quarantine.ContentType{
		quarantine.ContentPDF, quarantine.ContentPNG, quarantine.ContentJPEG,
		quarantine.ContentDOCX, quarantine.ContentCSV, quarantine.ContentTXT,
	}

	f.Fuzz(func(t *testing.T, content []byte) {
		if len(content) == 0 || len(content) > 4096 {
			t.Skip("Upload requires non-empty content and this fuzz target only cares about the sniff/declare boundary")
		}
		for _, declared := range declaredTypes {
			store := newFakeStore()
			scanner := &fakeScanner{verdict: quarantine.Verdict{Safe: true}}
			policy := testPolicy(declaredTypes...)
			policy.MaxContentBytes = 8192
			req := baseRequest(content, declared)
			// A digest collision across the six declared-type sub-runs
			// under the same fake store would make the second call see
			// an "already quarantined under a different declared type"
			// identity conflict that has nothing to do with this
			// property; give each declared type its own tenant so the
			// six checks stay independent.
			req.Tenant = string(declared)

			result, err := quarantine.Upload(context.Background(), store, scanner, policy, req, testNow)
			if err != nil {
				// A malformed request (e.g. an unrecognized declared
				// type) is out of scope for this property; declaredTypes
				// only contains valid ones, so any error here is
				// unexpected.
				t.Fatalf("declared=%s content=%q: unexpected error: %v", declared, content, err)
			}

			expected, _ := declared.ExpectedCategory()
			sniffed := quarantine.Sniff(content)
			if sniffed != expected && result.State == quarantine.Admitted {
				t.Fatalf("declared=%s content=%q: sniffed as %s but reached ADMITTED", declared, content, sniffed)
			}
		}
	})
}
