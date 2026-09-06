package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// Canonicalization profiles, following the same profile-prefixed sha256 style
// internal/workflow/digest.go, internal/workflow/runtime/receipts.go and
// internal/workflow/quarantine's own digest helper all use: a digest is
// always a function of the profile plus the canonical JSON of the value, and
// every slice a caller of this package builds is sorted before it reaches
// here.
const (
	previewRecordDigestProfile = "hcmnext.workflow.migrate.PreviewRecord/v1"
	receiptDigestProfile       = "hcmnext.workflow.migrate.Receipt/v1"
	// frontierDigestProfile is this package's own definition of "the digest
	// of a frontier", used to prove a durable workflow_checkpoint row
	// describes the exact frontier [Migrate] is about to act on. No other
	// package writes workflow_checkpoint rows yet (WF-RUN-008's pause path
	// does not), so this is the one and only producer of that digest for now;
	// [FrontierDigest] is exported so any caller taking a checkpoint ahead of
	// a migration computes the identical value.
	frontierDigestProfile = "hcmnext.workflow.migrate.Frontier/v1"
)

// canonicalDigest hashes a value under a profile.
func canonicalDigest(profile string, v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		// Every value this package digests is plain data it assembled
		// itself, so this is unreachable. If it ever happens, produce bytes
		// that cannot collide with a real digest rather than silently
		// returning an empty one.
		b = []byte("unencodable:" + err.Error())
	}
	h := sha256.New()
	h.Write([]byte(profile))
	h.Write([]byte{0})
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

// FrontierDigest is the content identity of a set of frontier node ids, order
// independent. It is what this package requires a workflow_checkpoint row's
// frontier_digest column to equal before trusting that checkpoint as proof an
// instance is paused at the exact frontier it now carries.
func FrontierDigest(frontier []string) string {
	sorted := append([]string(nil), frontier...)
	sort.Strings(sorted)
	return canonicalDigest(frontierDigestProfile, sorted)
}
