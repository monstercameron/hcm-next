package leave

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	delivery "github.com/monstercameron/human-capital-management-suite/internal/operations/messagingdelivery"
)

// Determination delivery states: provider acceptance is never delivery.
const (
	DeterminationPending      = "PENDING"
	DeterminationDelivered    = "DELIVERED"
	DeterminationAcknowledged = "ACKNOWLEDGED"
	DeterminationFailed       = "FAILED"
)

// DeterminationInput assembles the notice from governed components. The
// template must be approved and version-pinned: mutable or unapproved
// templates never render.
type DeterminationInput struct {
	ProposalDigest   string
	LegalRelease     string
	Programs         []ProgramResult
	Interval         string
	PaidHours        int
	UnpaidHours      int
	Rights           []string
	Obligations      []string
	Explanation      string
	TemplateID       string
	TemplateVersion  string
	TemplateApproved bool
	Locale           string
	Recipient        string
	BlockingNoticeID string
}

// Determination is the deterministic localized artifact plus its
// delivery obligation signal.
type Determination struct {
	Artifact          string
	ArtifactDigest    string
	DeliveryState     string
	LeaveStartAllowed bool
	ObligationSignal  string
	ManagerCopy       string
	Digest            string
}

func determinationDigest(input DeterminationInput, artifact, state string, allowed bool) string {
	programs := make([]string, 0, len(input.Programs))
	for _, program := range input.Programs {
		programs = append(programs, program.ProgramID+"="+program.Result)
	}
	sort.Strings(programs)
	rights := append([]string(nil), input.Rights...)
	sort.Strings(rights)
	obligations := append([]string(nil), input.Obligations...)
	sort.Strings(obligations)
	parts := append([]string{"leave-determination", input.ProposalDigest, input.LegalRelease, input.Interval, fmt.Sprint(input.PaidHours, input.UnpaidHours), strings.Join(rights, ","), strings.Join(obligations, ","), input.Explanation, input.TemplateID, input.TemplateVersion, input.Locale, input.Recipient, input.BlockingNoticeID, artifact, state, fmt.Sprint(allowed)}, programs...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// RenderDetermination renders the worker-facing artifact. Medical
// evidence never enters: the explanation carries program, interval,
// paid/unpaid treatment, rights and obligations only.
func RenderDetermination(input DeterminationInput) (Determination, error) {
	if !input.TemplateApproved || strings.TrimSpace(input.TemplateVersion) == "" || strings.TrimSpace(input.TemplateID) == "" {
		return Determination{}, fmt.Errorf("leave: determination renders only from an approved version-pinned template")
	}
	if strings.TrimSpace(input.ProposalDigest) == "" || strings.TrimSpace(input.LegalRelease) == "" {
		return Determination{}, fmt.Errorf("leave: determination binds proposal and legal versions")
	}
	if len(input.Programs) == 0 || strings.TrimSpace(input.Interval) == "" {
		return Determination{}, fmt.Errorf("leave: determination names its programs and interval")
	}
	if len(input.Rights) == 0 || len(input.Obligations) == 0 {
		return Determination{}, fmt.Errorf("leave: determination states rights and obligations")
	}
	if strings.TrimSpace(input.Locale) == "" || strings.TrimSpace(input.Recipient) == "" {
		return Determination{}, fmt.Errorf("leave: determination binds a locale and a recipient")
	}
	var programs []string
	for _, program := range input.Programs {
		programs = append(programs, program.ProgramID+":"+program.Result)
	}
	sort.Strings(programs)
	artifact := strings.Join([]string{
		"template:" + input.TemplateID + "@" + input.TemplateVersion,
		"locale:" + input.Locale,
		"proposal:" + input.ProposalDigest,
		"legal:" + input.LegalRelease,
		"programs:" + strings.Join(programs, ","),
		"interval:" + input.Interval,
		fmt.Sprintf("paid:%d unpaid:%d", input.PaidHours, input.UnpaidHours),
		"rights:" + strings.Join(input.Rights, ","),
		"obligations:" + strings.Join(input.Obligations, ","),
		"explanation:" + input.Explanation,
	}, "\n")
	sum := sha256.Sum256([]byte("leave-determination-artifact\x00" + artifact))
	determination := Determination{
		Artifact: artifact, ArtifactDigest: "sha256:" + hex.EncodeToString(sum[:]),
		DeliveryState: DeterminationPending,
		ManagerCopy:   "absence:" + input.Interval + " paid:" + fmt.Sprint(input.PaidHours),
	}
	determination.Digest = determinationDigest(input, artifact, determination.DeliveryState, false)
	return determination, nil
}

// DeliverDetermination advances delivery and computes the obligation
// signal from the blocking notice assessment. Provider acceptance alone
// never delivers; leave starts only behind a satisfied blocking notice.
func DeliverDetermination(determination Determination, input DeterminationInput, assessment delivery.NoticeAssessment, providerAccepted bool, acknowledged bool) (Determination, error) {
	_ = providerAccepted
	switch {
	case assessment.Outcome != delivery.NoticeSatisfied:
		determination.DeliveryState = DeterminationFailed
		determination.LeaveStartAllowed = false
		determination.ObligationSignal = "obligation:unsatisfied:" + input.BlockingNoticeID
	case acknowledged:
		determination.DeliveryState = DeterminationAcknowledged
		determination.LeaveStartAllowed = true
		determination.ObligationSignal = "obligation:satisfied:" + input.BlockingNoticeID
	default:
		determination.DeliveryState = DeterminationDelivered
		determination.LeaveStartAllowed = false
		determination.ObligationSignal = "obligation:delivered-pending-ack:" + input.BlockingNoticeID
	}
	determination.Digest = determinationDigest(input, determination.Artifact, determination.DeliveryState, determination.LeaveStartAllowed)
	return determination, nil
}

// Verify recomputes the determination seal.
func (determination Determination) Verify(input DeterminationInput) error {
	if determination.Digest == "" || determinationDigest(input, determination.Artifact, determination.DeliveryState, determination.LeaveStartAllowed) != determination.Digest {
		return fmt.Errorf("leave: determination seal is broken")
	}
	return nil
}
