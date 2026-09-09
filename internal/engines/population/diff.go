// POP-006: diff two population snapshots deterministically. Every subject
// that appears in either snapshot lands in exactly one partition; when either
// snapshot protects raw membership, the diff never reconstructs it through
// added/removed lists or count arithmetic.
package population

import (
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrDiffDefinitionMismatch is returned when the two snapshots do not cite the
// same population definition: a diff across unrelated populations is a
// contract error, not a membership comparison.
var ErrDiffDefinitionMismatch = errors.New("population: cannot diff snapshots of different definitions")

// DiffPartition names the one bucket a subject lands in.
type DiffPartition uint8

// Diff partitions.
const (
	DiffUnspecified DiffPartition = iota
	// DiffAdded means the subject is in To but not From.
	DiffAdded
	// DiffRemoved means the subject is in From but not To.
	DiffRemoved
	// DiffUnchanged means the subject is in both.
	DiffUnchanged
)

// String returns the wire token.
func (p DiffPartition) String() string {
	switch p {
	case DiffAdded:
		return "ADDED"
	case DiffRemoved:
		return "REMOVED"
	case DiffUnchanged:
		return "UNCHANGED"
	default:
		return "DIFF_UNSPECIFIED"
	}
}

// SnapshotDiff is the deterministic partition of two snapshots' disclosed
// membership. When either snapshot protects raw membership, Added, Removed
// and Unchanged are nil and CountDelta carries no value: comparing protected
// snapshots must not leak more than either snapshot alone discloses.
type SnapshotDiff struct {
	FromDigest string
	ToDigest   string
	Added      []string
	Removed    []string
	Unchanged  []string
	Protected  bool
	CountDelta values.Presence[int]
	Digest     string
}

// Diff partitions from and to. Both must cite the same population definition.
func Diff(from, to Snapshot) (SnapshotDiff, error) {
	if from.DefinitionID == "" || to.DefinitionID == "" {
		return SnapshotDiff{}, ErrSnapshotIncomplete
	}
	if from.DefinitionID != to.DefinitionID {
		return SnapshotDiff{}, fmt.Errorf("%w: %q vs %q", ErrDiffDefinitionMismatch, from.DefinitionID, to.DefinitionID)
	}

	protected := from.MembershipProtected || to.MembershipProtected

	result := SnapshotDiff{FromDigest: from.Digest, ToDigest: to.Digest, Protected: protected}

	if !protected {
		fromSet := make(map[string]bool, len(from.SubjectIDs))
		for _, id := range from.SubjectIDs {
			fromSet[id] = true
		}
		toSet := make(map[string]bool, len(to.SubjectIDs))
		for _, id := range to.SubjectIDs {
			toSet[id] = true
		}
		for id := range fromSet {
			if toSet[id] {
				result.Unchanged = append(result.Unchanged, id)
			} else {
				result.Removed = append(result.Removed, id)
			}
		}
		for id := range toSet {
			if !fromSet[id] {
				result.Added = append(result.Added, id)
			}
		}
		sort.Strings(result.Added)
		sort.Strings(result.Removed)
		sort.Strings(result.Unchanged)
	}

	fromCount, fromOK := from.Count.Get()
	toCount, toOK := to.Count.Get()
	if !protected && fromOK && toOK {
		result.CountDelta = values.Value(toCount - fromCount)
	} else {
		result.CountDelta = values.NotApplicable[int]("population: count delta requires both snapshots to disclose a count")
	}

	w := writerFor(diffSchema).
		String("from_digest", result.FromDigest).
		String("to_digest", result.ToDigest).
		Bool("protected", result.Protected).
		SortedStrings("added", result.Added).
		SortedStrings("removed", result.Removed).
		SortedStrings("unchanged", result.Unchanged)
	digest, err := w.Digest()
	if err != nil {
		return SnapshotDiff{}, fmt.Errorf("population: diff digest: %w", err)
	}
	result.Digest = digest
	return result, nil
}
