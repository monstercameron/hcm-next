package delivery

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/documents/signing"
)

func noticeRequirement() NoticeRequirement {
	return NoticeRequirement{
		ID: "notice-1", Recipient: "worker:w1",
		RecipientVerified: true, RecipientProof: "verify:kyc-9",
		ContentDigest: "sha256:notice", ContentVersion: "notice/v3",
		Timestamp: 1700000000, JurisdictionRule: "ca-notice/v1",
		AckProof: "ack:worker-w1", SignatureDigest: "sha256:ceremony",
	}
}

func knownNoticeRules() map[string]bool {
	return map[string]bool{"ca-notice/v1": true, "ny-notice/v1": true}
}

func TestTodo_MSG_013(t *testing.T) {
	assessment, err := AssessRequirement(noticeRequirement(), knownNoticeRules())
	if err != nil {
		t.Fatalf("AssessRequirement: %v", err)
	}
	if assessment.Outcome != NoticeSatisfied {
		t.Fatalf("assessment=%+v", assessment)
	}
	if len(assessment.Evidence) == 0 || assessment.Digest == "" {
		t.Fatal("satisfied assessment carries no evidence package")
	}
	if err := assessment.Verify(noticeRequirement()); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// RED: provider delivery without verified recipient, content,
	// timestamp, ack, signature or jurisdiction rule never satisfies.
	base := noticeRequirement()
	cases := map[string]func(*NoticeRequirement){
		"unverified recipient": func(r *NoticeRequirement) { r.RecipientVerified = false },
		"missing proof":        func(r *NoticeRequirement) { r.RecipientProof = "" },
		"unbound content":      func(r *NoticeRequirement) { r.ContentDigest = "" },
		"missing version":      func(r *NoticeRequirement) { r.ContentVersion = "" },
		"missing timestamp":    func(r *NoticeRequirement) { r.Timestamp = 0 },
		"unknown rule":         func(r *NoticeRequirement) { r.JurisdictionRule = "xx/v9" },
		"missing signature":    func(r *NoticeRequirement) { r.SignatureDigest = "" },
	}
	for name, mutate := range cases {
		requirement := base
		mutate(&requirement)
		assessed, err := AssessRequirement(requirement, knownNoticeRules())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if assessed.Outcome == NoticeSatisfied {
			t.Fatalf("%s satisfied", name)
		}
	}
	// A lone missing ack is repairable; a proven dispute disputes.
	ackless := base
	ackless.AckProof = ""
	repair, err := AssessRequirement(ackless, knownNoticeRules())
	if err != nil || repair.Outcome != NoticeRepairRequired {
		t.Fatalf("repair=%+v err=%v", repair, err)
	}
	disputed := base
	disputed.DisputeProof = "dispute:never-received"
	contest, err := AssessRequirement(disputed, knownNoticeRules())
	if err != nil || contest.Outcome != NoticeDisputed {
		t.Fatalf("contest=%+v err=%v", contest, err)
	}
	if _, err := AssessRequirement(NoticeRequirement{}, knownNoticeRules()); err == nil {
		t.Fatal("hollow requirement assessed")
	}
}

func TestTodo_MSG_013_Golden(t *testing.T) {
	assessment, err := AssessRequirement(noticeRequirement(), knownNoticeRules())
	if err != nil {
		t.Fatal(err)
	}
	lines := []string{
		"requirement=" + assessment.RequirementID,
		"outcome=" + assessment.Outcome,
		"evidence=" + strings.Join(assessment.Evidence, ","),
		"digest=" + assessment.Digest,
	}
	got := strings.Join(lines, "\n") + "\n"
	path := filepath.Join("testdata", "msg013_notice.golden")
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (set HCMNEXT_UPDATE_GOLDEN=1)", err)
	}
	if string(want) != got {
		t.Fatalf("golden mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestTodo_MSG_013_Race(t *testing.T) {
	registry := NewRequirementRegistry([]string{"ca-notice/v1"})
	if err := registry.Register(noticeRequirement()); err != nil {
		t.Fatal(err)
	}
	const workers = 16
	var wg sync.WaitGroup
	outcomes := make([]string, workers)
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			assessment, err := registry.Assess("notice-1")
			if err != nil {
				errs[i] = err
				return
			}
			outcomes[i] = assessment.Outcome
		}(i)
	}
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("worker %d: %v", i, errs[i])
		}
		if outcomes[i] != NoticeSatisfied {
			t.Fatalf("worker %d outcome = %q", i, outcomes[i])
		}
	}
	if err := registry.Register(noticeRequirement()); err == nil {
		t.Fatal("duplicate requirement registered")
	}
	if _, err := registry.Assess("ghost"); err == nil {
		t.Fatal("unknown requirement assessed")
	}
}

func TestTodo_MSG_013_Integration(t *testing.T) {
	// The signature binds a real proof-bound ceremony: notification,
	// acknowledgement and legal signature stay distinct artifacts.
	ceremony, err := signing.Request("cer-notice-1", "sha256:notice", "worker:w1", 100, 200)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	ceremony, err = ceremony.Deliver("provider:ok", "cb-deliver", 110)
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	ceremony, err = ceremony.Acknowledge("sha256:notice", 120)
	if err != nil {
		t.Fatalf("Acknowledge: %v", err)
	}
	ceremony, err = ceremony.Sign(signing.SignerProofFor("worker:w1", "sha256:notice"), "sha256:notice", "cb-sign", 130)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := ceremony.Verify(); err != nil {
		t.Fatalf("ceremony Verify: %v", err)
	}
	requirement := noticeRequirement()
	requirement.ContentDigest = "sha256:notice"
	requirement.SignatureDigest = ceremony.Digest
	assessment, err := AssessRequirement(requirement, knownNoticeRules())
	if err != nil {
		t.Fatalf("AssessRequirement: %v", err)
	}
	if assessment.Outcome != NoticeSatisfied {
		t.Fatalf("assessment=%+v", assessment)
	}
	found := false
	for _, element := range assessment.Evidence {
		if element == "signature:"+ceremony.Digest {
			found = true
		}
	}
	if !found {
		t.Fatalf("evidence=%v", assessment.Evidence)
	}
}

func TestTodo_MSG_013_Fault(t *testing.T) {
	// Stale content versions never satisfy against the bound digest.
	stale := noticeRequirement()
	stale.ContentVersion = "notice/v2"
	assessed, err := AssessContentRequirement(stale, knownNoticeRules(), map[string]string{"sha256:notice": "notice/v3"})
	if err != nil {
		t.Fatal(err)
	}
	if assessed.Outcome == NoticeSatisfied {
		t.Fatalf("stale version satisfied: %+v", assessed)
	}
	// The registered version satisfies.
	current, err := AssessContentRequirement(noticeRequirement(), knownNoticeRules(), map[string]string{"sha256:notice": "notice/v3"})
	if err != nil || current.Outcome != NoticeSatisfied {
		t.Fatalf("current=%+v err=%v", current, err)
	}
	// Empty rule tables leave every requirement unsatisfied.
	ruled, err := AssessRequirement(noticeRequirement(), map[string]bool{})
	if err != nil || ruled.Outcome == NoticeSatisfied {
		t.Fatalf("ruled=%+v err=%v", ruled, err)
	}
}

func TestTodo_MSG_013_Security(t *testing.T) {
	// Recipient substitution refuses: proofs must bind the recipient, and
	// the assessment names the bound one.
	substituted := noticeRequirement()
	substituted.Recipient = "worker:mallory"
	assessed, err := AssessRequirement(substituted, knownNoticeRules())
	if err != nil {
		t.Fatal(err)
	}
	if assessed.RequirementID != "notice-1" {
		t.Fatalf("assessment=%+v", assessed)
	}
	for _, element := range assessed.Evidence {
		if element == "recipient:worker:w1" {
			t.Fatal("assessment leaked the original recipient binding")
		}
	}
	// Forged assessments never verify.
	assessment, err := AssessRequirement(noticeRequirement(), knownNoticeRules())
	if err != nil {
		t.Fatal(err)
	}
	assessment.Outcome = NoticeSatisfied
	tampered := noticeRequirement()
	tampered.AckProof = ""
	if err := assessment.Verify(tampered); err == nil {
		t.Fatal("forged assessment verified")
	}
}

func TestTodo_MSG_013_Mutation(t *testing.T) {
	base, err := AssessRequirement(noticeRequirement(), knownNoticeRules())
	if err != nil {
		t.Fatal(err)
	}
	// Ack loss flips the outcome to repair with a new seal.
	ackless := noticeRequirement()
	ackless.AckProof = ""
	repair, err := AssessRequirement(ackless, knownNoticeRules())
	if err != nil {
		t.Fatal(err)
	}
	if repair.Outcome != NoticeRepairRequired || repair.Digest == base.Digest {
		t.Fatalf("repair=%+v", repair)
	}
	// Jurisdiction change re-identifies the assessment.
	moved := noticeRequirement()
	moved.JurisdictionRule = "ny-notice/v1"
	relocated, err := AssessRequirement(moved, knownNoticeRules())
	if err != nil {
		t.Fatal(err)
	}
	if relocated.Digest == base.Digest {
		t.Fatal("jurisdiction mutation kept the assessment digest")
	}
}
