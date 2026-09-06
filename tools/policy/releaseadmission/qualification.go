package releaseadmission

// QualificationDecision is the disposition of a library or tool
// qualification record.
type QualificationDecision string

const (
	QualificationDefer QualificationDecision = "DEFER"
)

// QualificationRecord documents why Cosign/Sigstore is not a runtime or
// module dependency of this policy yet, and what evidence would trigger a
// future adoption review.
type QualificationRecord struct {
	Tool             string                `json:"tool"`
	Decision         QualificationDecision `json:"decision"`
	Reason           string                `json:"reason"`
	AdoptionTrigger  string                `json:"adoption_trigger"`
	DependencyPolicy string                `json:"dependency_policy"`
}

// CosignSigstoreQualification is the pinned TOOL-023 qualification record.
var CosignSigstoreQualification = QualificationRecord{
	Tool:             "Cosign/Sigstore",
	Decision:         QualificationDefer,
	Reason:           "No keyless flow is used; release admission uses the existing signed provenance statement and pinned Ed25519 public keys with no new dependency.",
	AdoptionTrigger:  "Adopt only when a production release requires keyless identity or transparency-log verification and a separately qualified, offline-retained verification bundle is available.",
	DependencyPolicy: "No sigstore or cosign Go module may be added before that qualification and a conformance test pins the retained verification mechanics.",
}

// Qualification returns a copy of the pinned record.
func Qualification() QualificationRecord {
	return CosignSigstoreQualification
}
