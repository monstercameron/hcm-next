package quarantine

import (
	"context"
	"io"
)

// Verdict is what a [Scanner] reports about one quarantined content id.
// Safe is the only field [Upload] inspects to decide between [Admitted] and
// [Rejected]; Reason is recorded as evidence either way, but is required
// only when Safe is false ([Upload] refuses to admit content whose scanner
// left no reason recorded, since an admit is exactly the one outcome that
// must never be ambiguous about why it was reached).
type Verdict struct {
	// Safe is the scanner's own determination that the content carries no
	// detected threat. It says nothing about the declared size or
	// content-type allowlists, or the magic-byte sniff -- [Upload]
	// enforces those independently, before a [Scanner] is ever called.
	Safe bool
	// Reason is a human-readable explanation of the verdict: which
	// signature matched, which heuristic fired, or -- when the scanner
	// itself could not reach a verdict at all -- why. [Scan] returning a
	// non-nil error is a separate, always-unsafe outcome from a Verdict
	// with Safe set to false; both end in [Rejected].
	Reason string
}

// Scanner is the declared malware-inspection port [Upload] calls after its
// own syntactic checks (size, content-type allowlist, magic-byte sniff) have
// already passed. It is intentionally the smallest possible surface: one
// method, given the content id it is scanning and a reader over the exact
// bytes already recorded under that id, so a real implementation (an
// external ClamAV-style engine, a sandboxed analyzer, or -- for a test or an
// environment with no scanner configured at all -- a fake that always
// refuses) can be swapped in without this package's own logic changing.
//
// A Scanner that returns a non-nil error has failed to produce a verdict at
// all. [Upload] treats that exactly like an explicit unsafe [Verdict]: a
// scanner failure is a rejection with a reason, never treated as "no
// finding" and never treated as an admit.
type Scanner interface {
	Scan(ctx context.Context, digest string, r io.Reader) (Verdict, error)
}
