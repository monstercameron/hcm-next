package artifacts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"

	"github.com/google/uuid"
)

// receiptDigestProfile follows the profile-prefixed sha256 style
// internal/workflow/digest.go, internal/workflow/runtime/receipts.go and
// internal/workflow/migrate/digest.go all use: a digest is a function of the
// profile plus the canonical JSON of the value, and every slice is sorted
// before it is hashed.
const receiptDigestProfile = "hcmnext.workflow.migrate.artifacts.Receipt/v1"

// Entry is what one handler did with one artifact: the canonical, digestible
// statement of preserved identity, preserved owner, preserved deadline and
// the disposition reached.
type Entry struct {
	Kind Kind
	// Identity is what survives the migration unchanged. It never contains
	// an epoch, a node id that moved or a durable row id that was re-keyed:
	// two entries with the same identity on either side of a migration are
	// the same artifact.
	Identity string
	// Disposition is the verdict. A refusal is never an entry.
	Disposition Disposition

	// FromRef and ToRef are the durable row identities under the source and
	// target epochs. They are equal for a carried artifact.
	FromRef string
	ToRef   string

	// Owner is who the artifact belongs to, empty for a kind that has none.
	Owner string
	// Deadline is the instant the artifact is promised for, zero for a kind
	// that has none. A migration never moves it.
	Deadline time.Time
}

// Receipt is the digested evidence one [Migrate] call produced: every pending
// artifact of the instance, what happened to it, and under which contract.
type Receipt struct {
	ContractVersion string

	TenantID   uuid.UUID
	InstanceID uuid.UUID

	From Epoch
	To   Epoch

	// Entries are sorted canonically: handler order first, then identity, so
	// two runs that migrated the same artifacts digest identically however
	// the store happened to return the rows.
	Entries []Entry

	MigratedBy string
	MigratedAt time.Time

	digest string
}

// Digest is the receipt's content identity.
func (r Receipt) Digest() string { return r.digest }

// Count returns how many entries reached a disposition.
func (r Receipt) Count(d Disposition) int {
	n := 0
	for _, e := range r.Entries {
		if e.Disposition == d {
			n++
		}
	}
	return n
}

// OfKind returns the entries of one artifact kind, in canonical order.
func (r Receipt) OfKind(k Kind) []Entry {
	out := make([]Entry, 0, len(r.Entries))
	for _, e := range r.Entries {
		if e.Kind == k {
			out = append(out, e)
		}
	}
	return out
}

// sortEntries puts entries in canonical order: handler order, then identity,
// then the durable reference, so the ordering is total even if one instance
// somehow carried two artifacts of one kind under one identity.
func sortEntries(entries []Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Kind != b.Kind {
			return a.Kind.order() < b.Kind.order()
		}
		if a.Identity != b.Identity {
			return a.Identity < b.Identity
		}
		return a.FromRef < b.FromRef
	})
}

type entryIdentity struct {
	Kind        Kind
	Identity    string
	Disposition Disposition
	FromRef     string
	ToRef       string
	Owner       string
	Deadline    string
}

type receiptIdentity struct {
	ContractVersion string
	TenantID        string
	InstanceID      string

	FromWorkflowVersion uint32
	FromPlanDigest      string
	FromNodeID          string
	FromAttempt         int

	ToWorkflowVersion uint32
	ToPlanDigest      string
	ToNodeID          string
	ToAttempt         int

	Entries    []entryIdentity
	MigratedBy string
}

// instantText renders an instant canonically, or "" when it is unset. It is
// used only for digesting, so that a zero deadline and an epoch-zero deadline
// cannot digest the same way as each other by accident.
func instantText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func computeReceiptDigest(r Receipt) string {
	id := receiptIdentity{
		ContractVersion: r.ContractVersion,
		TenantID:        r.TenantID.String(), InstanceID: r.InstanceID.String(),
		FromWorkflowVersion: r.From.WorkflowVersion, FromPlanDigest: r.From.CompiledPlanDigest,
		FromNodeID: r.From.NodeID, FromAttempt: r.From.Attempt,
		ToWorkflowVersion: r.To.WorkflowVersion, ToPlanDigest: r.To.CompiledPlanDigest,
		ToNodeID: r.To.NodeID, ToAttempt: r.To.Attempt,
		MigratedBy: r.MigratedBy,
	}
	id.Entries = make([]entryIdentity, 0, len(r.Entries))
	for _, e := range r.Entries {
		id.Entries = append(id.Entries, entryIdentity{
			Kind: e.Kind, Identity: e.Identity, Disposition: e.Disposition,
			FromRef: e.FromRef, ToRef: e.ToRef, Owner: e.Owner, Deadline: instantText(e.Deadline),
		})
	}
	return canonicalDigest(receiptDigestProfile, id)
}

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
