package privacy

import (
	"testing"
)

// TestTodo_PRIV_002_Mutation is the MUTATION matrix test for PRIV-002. It
// proves the digest/decision machinery actually depends on every field it
// claims to, one at a time, and that consent status resolution is exact at
// its boundary instants -- the class of bug a mutant that drops a field from
// a canonical encoding, or gets a `<` vs `<=` boundary backwards, would
// introduce without any of the other matrix tests necessarily catching it.
func TestTodo_PRIV_002_Mutation(t *testing.T) {
	t.Run("Notice.Digest changes when any single field changes", func(t *testing.T) {
		base := fixtureNotice(t)
		baseDigest := base.Digest()

		mutants := map[string]Notice{
			"id":               withNoticeID(base, base.ID+"-x"),
			"version":          withNoticeVersion(base, base.Version+"-x"),
			"purpose":          withNoticePurpose(base, base.Purpose+"-x"),
			"jurisdiction":     withNoticeJurisdiction(base, base.Jurisdiction+"-x"),
			"locale":           withNoticeLocale(base, base.Locale+"-x"),
			"mandatory":        withNoticeMandatory(base, true),
			"data_class_add":   withNoticeDataClasses(base, append(append([]string{}, base.DataClasses...), "extra")),
			"data_class_order": withNoticeDataClasses(base, []string{base.DataClasses[len(base.DataClasses)-1], base.DataClasses[0]}),
		}
		for name, mutant := range mutants {
			if mutant.Digest() == baseDigest {
				t.Errorf("mutating %s did not change Notice.Digest (mutant indistinguishable from base)", name)
			}
		}

		// Identical inputs, independently constructed, must still match.
		again := fixtureNotice(t)
		if again.Digest() != baseDigest {
			t.Errorf("two independently-built identical notices produced different digests: %s vs %s", again.Digest(), baseDigest)
		}
	})

	t.Run("OptionalProcessing.StatusAt is exact at its withdrawal and expiry boundaries", func(t *testing.T) {
		notice := fixtureNotice(t)
		presentation := fixturePresentation(t, notice)
		consent, err := NewOptionalProcessing("consent-boundary", fixturePrincipal, []string{"ai_assist"},
			presentation.ID, mustInstant(t, fxGrantedAt), mustInstant(t, fxGrantedAt+1000))
		if err != nil {
			t.Fatalf("NewOptionalProcessing: %v", err)
		}

		if status := consent.StatusAt(mustInstant(t, fxGrantedAt)); status != ConsentStatusGranted {
			t.Errorf("StatusAt(granted_at) = %s, want %s (granted_at is inclusive)", status, ConsentStatusGranted)
		}
		if status := consent.StatusAt(mustInstant(t, fxGrantedAt-1)); status != ConsentStatusUnspecified {
			t.Errorf("StatusAt(before granted_at) = %s, want %s", status, ConsentStatusUnspecified)
		}
		if status := consent.StatusAt(mustInstant(t, fxGrantedAt+999)); status != ConsentStatusGranted {
			t.Errorf("StatusAt(one instant before expiry) = %s, want %s", status, ConsentStatusGranted)
		}
		if status := consent.StatusAt(mustInstant(t, fxGrantedAt+1000)); status != ConsentStatusExpired {
			t.Errorf("StatusAt(exactly expires_at) = %s, want %s (expiry is inclusive of the boundary instant, per this codebase's half-open interval convention)", status, ConsentStatusExpired)
		}

		withdrawnAt := mustInstant(t, fxGrantedAt+500)
		withdrawn, err := consent.Withdraw(withdrawnAt)
		if err != nil {
			t.Fatalf("Withdraw: %v", err)
		}
		if status := withdrawn.StatusAt(mustInstant(t, fxGrantedAt+499)); status != ConsentStatusGranted {
			t.Errorf("StatusAt(one instant before withdrawal) = %s, want %s", status, ConsentStatusGranted)
		}
		if status := withdrawn.StatusAt(withdrawnAt); status != ConsentStatusWithdrawn {
			t.Errorf("StatusAt(exactly withdrawn_at) = %s, want %s (withdrawal is inclusive of the boundary instant)", status, ConsentStatusWithdrawn)
		}
		// Withdrawal must not mutate the original record (Withdraw returns a
		// copy): the pre-withdrawal consent must still resolve GRANTED at the
		// same instant that now resolves WITHDRAWN on the returned copy.
		if status := consent.StatusAt(withdrawnAt); status != ConsentStatusGranted {
			t.Errorf("Withdraw mutated the receiver in place: original consent.StatusAt(withdrawn_at) = %s, want %s", status, ConsentStatusGranted)
		}
		// A withdrawal past its own expiry is still WITHDRAWN, not EXPIRED:
		// withdrawal is checked first.
		lateWithdrawn, err := consent.Withdraw(mustInstant(t, fxGrantedAt+5000))
		if err != nil {
			t.Fatalf("Withdraw (late): %v", err)
		}
		if status := lateWithdrawn.StatusAt(mustInstant(t, fxGrantedAt+5000)); status != ConsentStatusWithdrawn {
			t.Errorf("a consent withdrawn after its own expiry resolved %s, want %s (withdrawal takes precedence)", status, ConsentStatusWithdrawn)
		}
	})

	t.Run("EvaluateAuthority's InputsDigest changes when any single input changes", func(t *testing.T) {
		base := fixtureAuthorityInput(t)
		baseDigest := EvaluateAuthority(base).InputsDigest

		otherPurpose := base
		otherPurpose.Purpose = "analytics" // still in scope, but a different purpose
		if d := EvaluateAuthority(otherPurpose).InputsDigest; d == baseDigest {
			t.Errorf("changing Purpose did not change InputsDigest")
		}

		otherTime := base
		otherTime.EffectiveAt = mustInstant(t, fxEvaluateAt+1)
		if d := EvaluateAuthority(otherTime).InputsDigest; d == baseDigest {
			t.Errorf("changing EffectiveAt did not change InputsDigest")
		}

		otherLocales := base
		otherLocales.SupportedLocales = []string{"en-US"} // dropped "es-MX"
		if d := EvaluateAuthority(otherLocales).InputsDigest; d == baseDigest {
			t.Errorf("changing SupportedLocales did not change InputsDigest")
		}

		// Two independently-built, logically identical requests must match.
		again := fixtureAuthorityInput(t)
		if d := EvaluateAuthority(again).InputsDigest; d != baseDigest {
			t.Errorf("two independently-built identical authority requests produced different digests: %s vs %s", d, baseDigest)
		}
	})
}

func withNoticeID(n Notice, v string) Notice            { n.ID = v; return n }
func withNoticeVersion(n Notice, v string) Notice       { n.Version = v; return n }
func withNoticePurpose(n Notice, v string) Notice       { n.Purpose = v; return n }
func withNoticeJurisdiction(n Notice, v string) Notice  { n.Jurisdiction = v; return n }
func withNoticeLocale(n Notice, v string) Notice        { n.Locale = v; return n }
func withNoticeMandatory(n Notice, v bool) Notice       { n.Mandatory = v; return n }
func withNoticeDataClasses(n Notice, v []string) Notice { n.DataClasses = v; return n }
