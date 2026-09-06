package abuse

import "sort"

// SignalKind is the closed vocabulary of governed activity signal kinds an
// ActivitySignal may carry. It is not free text: a value outside this
// vocabulary is not a valid signal kind, and every value inside it has
// exactly one classification (see signalKindClassification and the
// ABUSE-001 conformance test).
type SignalKind string

const (
	// SignalKindSensitiveRead is an observation of a read against
	// sensitive or protected data.
	SignalKindSensitiveRead SignalKind = "SENSITIVE_READ"
	// SignalKindBulkExport is an observation of a bulk export or download
	// of records.
	SignalKindBulkExport SignalKind = "BULK_EXPORT"
	// SignalKindPrivilegedChange is an observation of a change made under
	// elevated or administrative privilege (e.g. payroll/bank changes).
	SignalKindPrivilegedChange SignalKind = "PRIVILEGED_CHANGE"
	// SignalKindAccessGrant is an observation of an access or entitlement
	// grant (e.g. a new privileged role assignment).
	SignalKindAccessGrant SignalKind = "ACCESS_GRANT"
	// SignalKindAuthAnomaly is an observation of an authentication event
	// with anomalous time, location, or velocity.
	SignalKindAuthAnomaly SignalKind = "AUTH_ANOMALY"
)

// signalKindClassification is the closed mapping from every governed
// SignalKind to its classification. Every entry in the vocabulary below
// (signalKindOrder) MUST have exactly one entry here; the conformance test
// pins this invariant so the vocabulary can never grow a kind without a
// classification.
var signalKindClassification = map[SignalKind]string{
	SignalKindSensitiveRead:    "SENSITIVE_DATA_ACCESS",
	SignalKindBulkExport:       "SENSITIVE_DATA_ACCESS",
	SignalKindPrivilegedChange: "PRIVILEGED_ACTION",
	SignalKindAccessGrant:      "PRIVILEGED_ACTION",
	SignalKindAuthAnomaly:      "AUTHENTICATION_ANOMALY",
}

// signalKindOrder is the authoritative, ordered vocabulary. It exists
// separately from the classification map so SignalKinds() has a stable,
// deterministic iteration order (Go map iteration is not ordered) without
// resorting to a runtime sort of an unordered source of truth.
var signalKindOrder = []SignalKind{
	SignalKindSensitiveRead,
	SignalKindBulkExport,
	SignalKindPrivilegedChange,
	SignalKindAccessGrant,
	SignalKindAuthAnomaly,
}

// SignalKinds returns the full closed vocabulary of governed signal kinds,
// in a stable order.
func SignalKinds() []SignalKind {
	out := append([]SignalKind(nil), signalKindOrder...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Valid reports whether k is a member of the closed signal-kind vocabulary.
func (k SignalKind) Valid() bool {
	_, ok := signalKindClassification[k]
	return ok
}

// Classification returns the closed-vocabulary classification for k, and
// whether k is governed at all. A signal kind outside the vocabulary has
// no classification.
func (k SignalKind) Classification() (string, bool) {
	c, ok := signalKindClassification[k]
	return c, ok
}
