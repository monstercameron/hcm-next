// Package partition contains the logical-identity checks for a physically
// partitioned ledger.  A partition is storage placement only: it cannot alter
// a tenant, stream key, sequence, event id, or digest chain.
package partition

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/fnv"
	"sort"

	"github.com/google/uuid"

	datalogger "github.com/monstercameron/hcm-next/internal/data/ledger"
)

// Identity is the logical portion of a ledger row that maintenance is not
// allowed to change while moving or pruning a physical partition.
type Identity struct {
	Tenant          uuid.UUID
	StreamKey       string
	Sequence        int64
	EventID         uuid.UUID
	Digest          string
	DigestAlgorithm string
}

// IdentityOf extracts the immutable logical identity from an event envelope.
func IdentityOf(event datalogger.EventRecord) Identity {
	return Identity{
		Tenant: event.Tenant, StreamKey: event.StreamKey, Sequence: event.Sequence,
		EventID: event.EventID, Digest: event.Digest, DigestAlgorithm: event.DigestAlgorithm,
	}
}

// Difference identifies the first logical identity field that changed.
type Difference struct {
	Index  int
	Field  string
	Before string
	After  string
}

// ErrIdentityMismatch refuses a maintenance result that changed logical
// identity or ordering.  The first difference is intentional: operators need
// a precise repair target, not an aggregate count.
type ErrIdentityMismatch struct{ Difference Difference }

func (e ErrIdentityMismatch) Error() string {
	return fmt.Sprintf("ledger partition: logical identity differs at index %d field %s: before %q after %q", e.Difference.Index, e.Difference.Field, e.Difference.Before, e.Difference.After)
}

// Compare verifies that two physical views contain exactly the same ordered
// logical rows. It compares tenant, stream, sequence, event id and the stored
// digest algorithm/value, so a moved row cannot silently fork the chain.
func Compare(before, after []Identity) error {
	if len(before) != len(after) {
		return ErrIdentityMismatch{Difference: Difference{Index: min(len(before), len(after)), Field: "row_count", Before: fmt.Sprint(len(before)), After: fmt.Sprint(len(after))}}
	}
	for i := range before {
		if diff, ok := firstDifference(i, before[i], after[i]); ok {
			return ErrIdentityMismatch{Difference: diff}
		}
	}
	return nil
}

// CompareEvents is Compare for the ledger's public event envelope.
func CompareEvents(before, after []datalogger.EventRecord) error {
	left := make([]Identity, len(before))
	right := make([]Identity, len(after))
	for i := range before {
		left[i] = IdentityOf(before[i])
	}
	for i := range after {
		right[i] = IdentityOf(after[i])
	}
	return Compare(left, right)
}

// ChainDigest produces the stable digest used to compare a stream before and
// after partition maintenance. It is order-sensitive and length-framed; the
// previous row's digest is therefore part of the next row's preimage.
func ChainDigest(events []Identity) string {
	ordered := append([]Identity(nil), events...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].StreamKey != ordered[j].StreamKey {
			return ordered[i].StreamKey < ordered[j].StreamKey
		}
		return ordered[i].Sequence < ordered[j].Sequence
	})
	var previous []byte
	h := sha256.New()
	for _, event := range ordered {
		frame := sha256.New()
		writeBytes(frame, previous)
		writeBytes(frame, event.Tenant[:])
		writeBytes(frame, []byte(event.StreamKey))
		writeInt(frame, event.Sequence)
		writeBytes(frame, event.EventID[:])
		writeBytes(frame, []byte(event.DigestAlgorithm))
		writeBytes(frame, []byte(event.Digest))
		previous = frame.Sum(nil)
	}
	writeBytes(h, previous)
	writeInt(h, int64(len(ordered)))
	return hex.EncodeToString(h.Sum(nil))
}

// PartitionFor returns a stable routing bucket for the migration's
// PARTITION BY HASH (tenant_id) shape. It is routing metadata only and is not
// included in any ledger identity or digest.
func PartitionFor(tenant uuid.UUID, partitionCount int) (int, error) {
	if partitionCount < 1 {
		return 0, errors.New("ledger partition: partition count must be positive")
	}
	h := fnv.New32a()
	_, _ = h.Write(tenant[:])
	return int(h.Sum32() % uint32(partitionCount)), nil
}

// AtomicScope describes the tenant boundary of a planned append. The ledger
// may atomically append several streams, but it never claims ACID across
// tenants or an unspecified cross-cell boundary.
type AtomicScope struct {
	Tenant        uuid.UUID
	Streams       []string
	StreamTenants map[string]uuid.UUID
}

// Validate rejects an append plan that could accidentally span tenants or
// contain duplicate stream declarations. Physical partitions are not exposed
// as logical stream identity and therefore do not change a valid same-tenant
// plan.
func (s AtomicScope) Validate() error {
	if s.Tenant == uuid.Nil {
		return errors.New("ledger partition: atomic scope tenant is required")
	}
	seen := make(map[string]struct{}, len(s.Streams))
	for _, stream := range s.Streams {
		if stream == "" {
			return errors.New("ledger partition: atomic scope stream is required")
		}
		if _, exists := seen[stream]; exists {
			return fmt.Errorf("ledger partition: stream %q is declared more than once", stream)
		}
		seen[stream] = struct{}{}
		if streamTenant, ok := s.StreamTenants[stream]; ok && streamTenant != s.Tenant {
			return fmt.Errorf("ledger partition: stream %q belongs to tenant %s; cross-tenant ACID is unsupported", stream, streamTenant)
		}
	}
	for stream, streamTenant := range s.StreamTenants {
		if streamTenant != s.Tenant {
			return fmt.Errorf("ledger partition: stream %q belongs to tenant %s; cross-tenant ACID is unsupported", stream, streamTenant)
		}
	}
	return nil
}

func firstDifference(index int, before, after Identity) (Difference, bool) {
	fields := []struct {
		name  string
		left  string
		right string
	}{
		{"tenant", before.Tenant.String(), after.Tenant.String()},
		{"stream_key", before.StreamKey, after.StreamKey},
		{"sequence", fmt.Sprint(before.Sequence), fmt.Sprint(after.Sequence)},
		{"event_id", before.EventID.String(), after.EventID.String()},
		{"digest_algorithm", before.DigestAlgorithm, after.DigestAlgorithm},
		{"digest", before.Digest, after.Digest},
	}
	for _, field := range fields {
		if field.left != field.right {
			return Difference{Index: index, Field: field.name, Before: field.left, After: field.right}, true
		}
	}
	return Difference{}, false
}

func writeBytes(h interface{ Write([]byte) (int, error) }, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = h.Write(length[:])
	_, _ = h.Write(value)
}

func writeInt(h interface{ Write([]byte) (int, error) }, value int64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(value))
	_, _ = h.Write(encoded[:])
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}

// Version is the package contract version used by architecture tooling.
func Version() int { return 1 }

// Explain returns bounded partition semantics for operator telemetry.
func Explain() string {
	return fmt.Sprintf("ledger.partition v%d: physical placement preserves tenant, stream, sequence and digest-chain identity", Version())
}
