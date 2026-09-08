package contact

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func TestContactConformancePreservesVerificationAndPurposeAcrossCorrectionAndSync(t *testing.T) {
	base := contactEndpoint(t)
	verified, err := base.MarkVerified()
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewContactEndpointRevision(contactSubject(), "phone-1", EndpointPhone, "+14155550123", "account-recovery", 2, "profile")
	if err != nil {
		t.Fatal(err)
	}
	other, err = other.MarkVerified()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := SelectPrimaryEndpointForPurpose([]ContactEndpointRevision{base, verified, other}, "account-recovery"); err != nil || got.EndpointID != base.EndpointID || got.Revision != verified.Revision {
		t.Fatalf("primary = %+v, err=%v", got, err)
	}
	if _, err := SelectPrimaryEndpointForPurpose([]ContactEndpointRevision{verified}, "work"); !errors.Is(err, ErrNoVerifiedEndpoint) {
		t.Fatalf("purpose mixing error = %v", err)
	}
	correction := verified
	correction.Source = "correction"
	correction.Revision = verified.Revision + 1
	correction.SupersedesRevision = verified.Revision
	corrected, err := CorrectEndpointRevision(verified, correction)
	if err != nil || corrected.Verification != Verified || corrected.SupersedesRevision != verified.Revision {
		t.Fatalf("correction = %+v, err=%v", corrected, err)
	}
	obs := ContactObservation{Subject: verified.Subject, EndpointID: verified.EndpointID, Purpose: verified.Purpose, NormalizedValueDigest: verified.NormalizedValueDigest, Source: "provider", ObservedAt: time.Unix(100, 0)}
	result, err := ReconcileExternalContact(corrected, obs)
	if err != nil || result.Status != ContactReconciled || result.Repair != nil {
		t.Fatalf("reconciliation = %+v, err=%v", result, err)
	}
	store := NewInMemoryChallengeStore()
	old := contactChallenge(t, 2)
	if err := store.Put(old); err != nil {
		t.Fatal(err)
	}
	replacement, _, err := IssueContactChallenge("challenge-2", old.Subject, base, old.Purpose, "654321", contactNow.Add(time.Minute), time.Hour, 2)
	if err != nil {
		t.Fatal(err)
	}
	revoked, issued, err := store.Reissue(old.ChallengeID, old.CanonicalDigest, replacement, contactNow.Add(2*time.Minute))
	if err != nil || revoked.Status != ContactChallengeRevoked || issued.Status != ContactChallengeIssued {
		t.Fatalf("atomic reissue old=%+v new=%+v err=%v", revoked, issued, err)
	}
	replayedOld, replayedNew, err := store.Reissue(old.ChallengeID, old.CanonicalDigest, replacement, contactNow.Add(2*time.Minute))
	if err != nil || replayedOld.CanonicalDigest != revoked.CanonicalDigest || replayedNew.CanonicalDigest != issued.CanonicalDigest {
		t.Fatalf("reissue replay old=%+v new=%+v err=%v", replayedOld, replayedNew, err)
	}
}

func TestTodo_CONTACT_002_ReissueRevokesOldChallenge(t *testing.T) {
	old := contactChallenge(t, 2)
	newChallenge, _, err := IssueContactChallenge("challenge-2", old.Subject, contactEndpoint(t), old.Purpose, "654321", contactNow.Add(time.Minute), time.Hour, 2)
	if err != nil {
		t.Fatal(err)
	}
	revoked, replacement, err := ReissueContactChallenge(old, newChallenge, contactNow.Add(2*time.Minute))
	if err != nil || revoked.Status != ContactChallengeRevoked || replacement.Status != ContactChallengeIssued {
		t.Fatalf("reissue = old=%+v new=%+v err=%v", revoked, replacement, err)
	}
	if _, status, err := revoked.Respond("123456", contactNow.Add(3*time.Minute)); err != nil || status != ContactChallengeRevoked {
		t.Fatalf("revoked response status=%s err=%v", status, err)
	}
}

func TestTodo_CONTACT_002_Property(t *testing.T) {
	for priority := 0; priority < 32; priority++ {
		base := contactEndpoint(t)
		base.Priority = priority
		base.CanonicalDigest = base.computedDigest()
		verified, err := base.MarkVerified()
		if err != nil {
			t.Fatal(err)
		}
		got, err := SelectPrimaryEndpointForPurpose([]ContactEndpointRevision{base, verified}, base.Purpose)
		if err != nil || got.CanonicalDigest != verified.CanonicalDigest {
			t.Fatalf("priority %d selected %+v, err=%v", priority, got, err)
		}
	}
	makeVerified := func(id string, priority int) ContactEndpointRevision {
		r, err := NewContactEndpointRevision(contactSubject(), id, EndpointEmail, id+"@example.com", "recovery", priority, "profile")
		if err != nil {
			t.Fatal(err)
		}
		r, err = r.MarkVerified()
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	a, b, winner := makeVerified("alpha", 5), makeVerified("bravo", 5), makeVerified("winner", 1)
	permutations := [][]ContactEndpointRevision{{a, b, winner}, {a, winner, b}, {winner, b, a}, {b, a, winner}, {b, winner, a}, {winner, a, b}}
	for i, input := range permutations {
		got, err := SelectPrimaryEndpointForPurpose(input, "recovery")
		if err != nil || got.EndpointID != winner.EndpointID {
			t.Fatalf("permutation %d selected %+v, err=%v", i, got, err)
		}
	}
	equivocation := winner
	equivocation.Source = "conflicting-source"
	equivocation.CanonicalDigest = equivocation.computedDigest()
	if _, err := SelectPrimaryEndpointForPurpose([]ContactEndpointRevision{winner, equivocation}, "recovery"); !errors.Is(err, ErrContactLineage) {
		t.Fatalf("equal-revision equivocation error=%v", err)
	}
}

func TestTodo_CONTACT_002_Golden(t *testing.T) {
	old := contactChallenge(t, 2)
	replacement, _, err := IssueContactChallenge("challenge-2", old.Subject, contactEndpoint(t), old.Purpose, "654321", contactNow.Add(time.Minute), time.Hour, 2)
	if err != nil {
		t.Fatal(err)
	}
	revoked, _, err := ReissueContactChallenge(old, replacement, contactNow.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	const want = "sha256:719b105c56a6b49d92c4c7f58cc904d4efccc2b57408d4c6b9ff86fa22e7bae1"
	if revoked.CanonicalDigest != want {
		t.Fatalf("revoked digest changed: got %q", revoked.CanonicalDigest)
	}
}

func TestTodo_CONTACT_002_Race(t *testing.T) {
	store := NewInMemoryChallengeStore()
	old := contactChallenge(t, 2)
	if err := store.Put(old); err != nil {
		t.Fatal(err)
	}
	replacement, _, err := IssueContactChallenge("challenge-2", old.Subject, contactEndpoint(t), old.Purpose, "654321", contactNow.Add(time.Minute), time.Hour, 2)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := store.Reissue(old.ChallengeID, old.CanonicalDigest, replacement, contactNow.Add(2*time.Minute))
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	gotOld, _ := store.Get(old.ChallengeID)
	gotNew, ok := store.Get(replacement.ChallengeID)
	if gotOld.Status != ContactChallengeRevoked || len(gotOld.Events) != len(old.Events)+1 || !ok || gotNew.CanonicalDigest != replacement.CanonicalDigest {
		t.Fatalf("non-atomic or duplicate reissue: old=%+v new=%+v", gotOld, gotNew)
	}
}

func TestTodo_CONTACT_002_Fault(t *testing.T) {
	store := NewInMemoryChallengeStore()
	old := contactChallenge(t, 2)
	replacement, _, err := IssueContactChallenge("challenge-2", old.Subject, contactEndpoint(t), old.Purpose, "654321", contactNow.Add(time.Minute), time.Hour, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(old); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(replacement); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Reissue(old.ChallengeID, old.CanonicalDigest, replacement, contactNow.Add(2*time.Minute)); !errors.Is(err, ErrInvalidReissue) {
		t.Fatalf("collision error=%v", err)
	}
	stored, _ := store.Get(old.ChallengeID)
	if stored.Status != ContactChallengeIssued || stored.CanonicalDigest != old.CanonicalDigest {
		t.Fatalf("failed replacement revoked old challenge: %+v", stored)
	}
	freshStore := NewInMemoryChallengeStore()
	if err := freshStore.Put(old); err != nil {
		t.Fatal(err)
	}
	if _, _, err := freshStore.Reissue(old.ChallengeID, old.CanonicalDigest, replacement, contactNow.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := freshStore.Reissue(old.ChallengeID, old.CanonicalDigest, replacement, contactNow.Add(3*time.Minute)); !errors.Is(err, ErrContactLineage) {
		t.Fatalf("forged replay time error=%v", err)
	}
	sameID, _, err := IssueContactChallenge(old.ChallengeID, old.Subject, contactEndpoint(t), old.Purpose, "different", contactNow.Add(time.Minute), time.Hour, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReissueContactChallenge(old, sameID, contactNow.Add(2*time.Minute)); !errors.Is(err, ErrInvalidReissue) {
		t.Fatalf("same-id replacement error=%v", err)
	}
	reusedToken, _, err := IssueContactChallenge("challenge-3", old.Subject, contactEndpoint(t), old.Purpose, "123456", contactNow.Add(time.Minute), time.Hour, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReissueContactChallenge(old, reusedToken, contactNow.Add(2*time.Minute)); !errors.Is(err, ErrInvalidReissue) {
		t.Fatalf("reused-token replacement error=%v", err)
	}
	future, _, err := IssueContactChallenge("challenge-4", old.Subject, contactEndpoint(t), old.Purpose, "different", contactNow.Add(10*time.Minute), time.Hour, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReissueContactChallenge(old, future, contactNow.Add(2*time.Minute)); !errors.Is(err, ErrInvalidReissue) {
		t.Fatalf("future replacement error=%v", err)
	}
}

func TestTodo_CONTACT_002_Security(t *testing.T) {
	current := contactEndpoint(t)
	foreign := current
	foreign.Subject = values.EntityRef{Tenant: values.TenantId("other-tenant"), Kind: current.Subject.Kind, Id: current.Subject.Id}
	foreign.CanonicalDigest = foreign.computedDigest()
	if _, err := SelectPrimaryEndpointForPurpose([]ContactEndpointRevision{current, foreign}, current.Purpose); !errors.Is(err, ErrContactLineage) {
		t.Fatalf("mixed-tenant primary error=%v", err)
	}
	obs := ContactObservation{Subject: foreign.Subject, EndpointID: current.EndpointID, Purpose: current.Purpose, NormalizedValueDigest: current.NormalizedValueDigest, Source: "provider", ObservedAt: contactNow}
	if result, err := ReconcileExternalContact(current, obs); !errors.Is(err, ErrExternalContactMismatch) || result.Repair != nil {
		t.Fatalf("cross-tenant observation result=%+v err=%v", result, err)
	}
	if _, _, err := IssueContactChallenge("foreign", foreign.Subject, current, current.Purpose, "123456", contactNow, time.Hour, 2); !errors.Is(err, ErrInvalidContactRevision) {
		t.Fatalf("cross-tenant challenge issue error=%v", err)
	}
}

func TestTodo_CONTACT_002_Conformance(t *testing.T) {
	current := contactEndpoint(t)
	obs := ContactObservation{Subject: current.Subject, EndpointID: current.EndpointID, Purpose: current.Purpose, NormalizedValueDigest: SHA256Digest([]byte("different")), Source: "provider", ObservedAt: contactNow}
	result, err := ReconcileExternalContact(current, obs)
	if err != nil || result.Repair == nil || result.Repair.ExpectedRevisionDigest != current.CanonicalDigest || result.Repair.ExpectedValueDigest != current.NormalizedValueDigest {
		t.Fatalf("repair is not revision-bounded: %+v err=%v", result, err)
	}
}

func TestTodo_CONTACT_002_Mutation(t *testing.T) {
	current, err := contactEndpoint(t).MarkVerified()
	if err != nil {
		t.Fatal(err)
	}
	replacement := current
	replacement.Revision++
	replacement.SupersedesRevision = current.Revision
	replacement.NormalizedValueDigest = SHA256Digest([]byte("changed"))
	replacement.CanonicalDigest = replacement.computedDigest()
	if _, err := CorrectEndpointRevision(current, replacement); !errors.Is(err, ErrContactLineage) {
		t.Fatalf("verified-value mutation error=%v", err)
	}
}

func TestTodo_CONTACT_002_ExternalMismatchCreatesBoundedRepair(t *testing.T) {
	endpoint := contactEndpoint(t)
	obs := ContactObservation{Subject: endpoint.Subject, EndpointID: endpoint.EndpointID, Purpose: endpoint.Purpose, NormalizedValueDigest: SHA256Digest([]byte("different")), Source: "provider", ObservedAt: contactNow}
	result, err := ReconcileExternalContact(endpoint, obs)
	if err != nil || result.Status != ContactRepairRequired || result.Repair == nil || result.Repair.ExpectedValueDigest != endpoint.NormalizedValueDigest {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
