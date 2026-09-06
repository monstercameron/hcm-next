package auditpack

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
)

// idempotencyKeyProfile is folded into every key this package derives, so a
// sha256 hex string produced here can never be mistaken for one produced by
// an unrelated profile even if the inputs happened to collide.
const idempotencyKeyProfile = "hcmnext.canonical.PAYROLL_AUDITPACK_IDEMPOTENCY_KEY.v1"

// IdempotencyKey is the immutable idempotency key one payroll run's
// reconciliation binds under: a pure function of (tenant, run id), with no
// clock, random source or resolved amount folded in.
//
// "Immutable" means exactly this: the key is never stored as its own
// decision and never chosen by a caller, it is recomputed identically every
// time from the two facts that already identify the run. [Bind] appends
// under this key, so a second [Bind] call for a run already bound is an
// exact replay under internal/data/ledger's own idempotency rule - the same
// bytes come back, not a second, competing reconciliation record - and a
// caller that computed different totals for the same run and tried to bind
// them is refused as an idempotency conflict rather than silently
// overwriting the first attestation.
func IdempotencyKey(tenant TenantID, runID string) string {
	h := sha256.New()
	writeField := func(b []byte) {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(b)))
		_, _ = h.Write(length[:])
		_, _ = h.Write(b)
	}
	writeField([]byte(idempotencyKeyProfile))
	writeField(tenant[:])
	writeField([]byte(runID))
	return hex.EncodeToString(h.Sum(nil))
}
