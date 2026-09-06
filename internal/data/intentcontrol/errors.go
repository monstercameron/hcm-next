package intentcontrol

import (
	"errors"
	"fmt"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
)

// The sentinels this package classifies failures with. A caller distinguishes
// "you raced someone" from "you asked for something contradictory" from "that
// row is not there", and never has to read a driver error string to do it.
var (
	// ErrInvalidRow reports a row that could not be stored because it is not
	// internally consistent: a missing digest, an empty key, a zero timestamp.
	// It is returned before any statement runs.
	ErrInvalidRow = errors.New("intentcontrol: invalid row")

	// ErrDuplicate reports a row whose identity the tenant already carries. It
	// is the answer to every "record this twice" attempt, and it is deliberately
	// not an error the caller can suppress by retrying.
	ErrDuplicate = errors.New("intentcontrol: duplicate row")

	// ErrNotFound reports a row that does not exist.
	ErrNotFound = errors.New("intentcontrol: not found")

	// ErrVersionConflict reports a compare-and-swap whose expected version is
	// not the stored one. Nothing was written.
	ErrVersionConflict = errors.New("intentcontrol: version conflict")

	// ErrRelationshipCycle reports an intent relationship that would close a
	// cycle in the parentage graph.
	ErrRelationshipCycle = errors.New("intentcontrol: relationship cycle")

	// ErrReceiptConflict reports an attempt to record a commit receipt for a
	// plan that already aborted, or an abort receipt for one that already
	// committed.
	ErrReceiptConflict = errors.New("intentcontrol: receipt conflict")

	// ErrSnapshotMismatch reports a simulation result whose declared input
	// snapshot belongs to a different intent.
	ErrSnapshotMismatch = errors.New("intentcontrol: snapshot mismatch")

	// ErrIllegalTransition reports a state transition the lifecycle does not
	// allow, refused before the statement runs.
	ErrIllegalTransition = errors.New("intentcontrol: illegal transition")
)

// invalid builds an [ErrInvalidRow] naming the field that is wrong. The field
// name is part of the message because a validation failure a caller cannot
// locate is a failure it will guess at.
func invalid(field, detail string) error {
	return fmt.Errorf("%w: %s: %s", ErrInvalidRow, field, detail)
}

// isNoRows reports whether err is the port's "selected nothing" answer. Callers
// use it instead of importing the driver.
func isNoRows(err error) bool { return errors.Is(err, dbport.ErrNoRows) }
