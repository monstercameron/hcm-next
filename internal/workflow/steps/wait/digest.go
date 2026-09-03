package wait

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Canonicalization profile identities. A digest is always prefixed with the
// profile that produced it, mirroring internal/workflow/frontier/digest.go's
// convention: a profile-prefixed sha256 hash over the encoding/json rendering
// of a canonical view struct (fields in declaration order, maps sorted by key
// by encoding/json itself, no floating point anywhere).
const (
	requirementDigestProfile = "hcmnext.workflow.steps.wait.TimerRequirement/v1"
	resolutionDigestProfile  = "hcmnext.workflow.steps.wait.Resolution/v1"
)

// canonicalDigest hashes v (rendered as indented-free JSON) under a profile.
func canonicalDigest(profile string, v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		// Every canonical view in this package is plain data built from
		// strings and value types with safe String() forms (never a raw
		// values.Instant/LocalDate that could fail MarshalText on an unset
		// zero value), so this is unreachable in practice. Produce bytes that
		// cannot collide with a real digest rather than panicking.
		b = []byte("unencodable:" + err.Error())
	}
	h := sha256.New()
	h.Write([]byte(profile))
	h.Write([]byte{0})
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

// requirementDigestView is the canonical digest input for a TimerRequirement.
// Every temporal value is rendered as its canonical text form explicitly
// (rather than embedding values.Instant/LocalDate directly) so an unset
// instant — the legitimate ReviewRequired case — never trips
// encoding.TextMarshaler's "unset" error path.
type requirementDigestView struct {
	WorkflowID      string `json:"workflow_id"`
	WorkflowVersion uint32 `json:"workflow_version"`
	NodeID          string `json:"node_id"`

	FireAt string `json:"fire_at"`

	Zone           string `json:"zone"`
	Calendar       string `json:"calendar"`
	Policy         string `json:"policy"`
	DatasetTzdb    string `json:"dataset_tzdb"`
	DatasetCal     string `json:"dataset_calendar"`
	ReviewRequired bool   `json:"review_required"`
	ReviewReason   string `json:"review_reason"`

	Evidence CalculationEvidence `json:"evidence"`
}

func computeRequirementDigest(r TimerRequirement) string {
	view := requirementDigestView{
		WorkflowID:      r.WorkflowID,
		WorkflowVersion: r.WorkflowVersion,
		NodeID:          r.NodeID,
		FireAt:          r.FireAtText(),
		Zone:            r.Reference.Zone.String(),
		Calendar:        r.Reference.Calendar.String(),
		Policy:          r.Reference.Policy.String(),
		DatasetTzdb:     r.Dataset.TzdbVersion,
		DatasetCal:      r.Dataset.CalendarVersion,
		ReviewRequired:  r.ReviewRequired,
		ReviewReason:    r.ReviewReason,
		Evidence:        r.Evidence,
	}
	return canonicalDigest(requirementDigestProfile, view)
}

// resolutionDigestView is the canonical digest input for a Resolution.
type resolutionDigestView struct {
	RequirementDigest string `json:"requirement_digest"`
	Outcome           string `json:"outcome"`
	ResolvedAt        string `json:"resolved_at"`
	Reason            string `json:"reason"`
}

func computeResolutionDigest(r Resolution) string {
	view := resolutionDigestView{
		RequirementDigest: r.RequirementDigest,
		Outcome:           string(r.Outcome),
		ResolvedAt:        resolvedAtText(r.ResolvedAt),
		Reason:            r.Reason,
	}
	return canonicalDigest(resolutionDigestProfile, view)
}
