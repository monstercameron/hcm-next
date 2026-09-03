package governance

import (
	"testing"
	"time"
)

var fixedNow = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

func baseReq() ComposeRequest {
	return ComposeRequest{
		AuthZ:   AuthZInput{Effect: "ALLOW", Reason: "granted", Restrictions: []string{"read"}, Digest: "a1", ValidUntil: fixedNow.Add(time.Hour), EvaluatedAt: fixedNow},
		Legal:   LegalInput{Effect: "COMPLIANT", Reason: "ok", Obligations: []string{"log"}, Digest: "l1", ValidUntil: fixedNow.Add(time.Hour), EvaluatedAt: fixedNow},
		Purpose: PurposeInput{Allowed: true, Reason: "purpose ok"},
		Risk:    RiskInput{Level: "LOW", Obligations: nil},
		Now:     fixedNow,
	}
}

func TestTodo_GOVERN_002(t *testing.T) {
	t.Run("deny dominance over allow", func(t *testing.T) {
		req := baseReq()
		req.AuthZ.Effect = "DENY"
		req.AuthZ.Reason = "no_grant"
		res := Compose(req)
		if res.Decision != "DENY" {
			t.Fatalf("want DENY got %s", res.Decision)
		}
		req2 := baseReq()
		req2.Legal.Effect = "NON_COMPLIANT"
		res2 := Compose(req2)
		if res2.Decision != "DENY" {
			t.Fatalf("legal deny should dominate, got %s", res2.Decision)
		}
	})
	t.Run("restriction intersection", func(t *testing.T) {
		req := baseReq()
		req.AuthZ.Restrictions = []string{"read", "write"}
		req.Legal.Obligations = []string{"write", "execute"}
		res := Compose(req)
		if len(res.Restrictions) != 1 || res.Restrictions[0] != "write" {
			t.Fatalf("intersection want [write] got %v", res.Restrictions)
		}
	})
	t.Run("restriction broadens blocked", func(t *testing.T) {
		req := baseReq()
		req.AuthZ.Restrictions = []string{"read"}
		req.Legal.Obligations = []string{"read", "write"}
		res := Compose(req)
		for _, r := range res.Restrictions {
			if r == "write" {
				t.Fatal("restriction broadening should not happen")
			}
		}
	})
	t.Run("obligation union dedup", func(t *testing.T) {
		req := baseReq()
		req.Legal.Obligations = []string{"log", "notify"}
		req.Risk.Obligations = []string{"notify", "review"}
		res := Compose(req)
		if res.Decision != "OBLIGATIONS" {
			t.Fatalf("want OBLIGATIONS got %s", res.Decision)
		}
		if len(res.Obligations) != 3 {
			t.Fatalf("want 3 obligations got %v", res.Obligations)
		}
	})
	t.Run("unknown preserved", func(t *testing.T) {
		req := baseReq()
		req.Risk.Level = "UNKNOWN"
		res := Compose(req)
		if res.Decision != "UNKNOWN" {
			t.Fatalf("want UNKNOWN got %s", res.Decision)
		}
	})
	t.Run("stale decision permits nothing", func(t *testing.T) {
		req := baseReq()
		req.AuthZ.ValidUntil = fixedNow.Add(-time.Hour)
		res := Compose(req)
		if res.Decision == "ALLOW" {
			t.Fatal("stale should not allow")
		}
		if res.Decision != "UNKNOWN" {
			t.Fatalf("stale want UNKNOWN got %s", res.Decision)
		}
	})
	t.Run("deterministic digest", func(t *testing.T) {
		a := Compose(baseReq())
		b := Compose(baseReq())
		if a.Digest != b.Digest {
			t.Fatalf("digest not deterministic %s vs %s", a.Digest, b.Digest)
		}
		if len(a.Digest) != 64 {
			t.Fatalf("digest len %d", len(a.Digest))
		}
		req1 := baseReq()
		req1.AuthZ.Restrictions = []string{"write", "read"}
		req2 := baseReq()
		req2.AuthZ.Restrictions = []string{"read", "write"}
		if Compose(req1).Digest != Compose(req2).Digest {
			t.Fatal("digest should be order independent")
		}
	})
	t.Run("allow produces allow", func(t *testing.T) {
		res := Compose(baseReq())
		if res.Decision != "OBLIGATIONS" {
			t.Fatalf("base with obligations should be OBLIGATIONS got %s", res.Decision)
		}
		req := baseReq()
		req.Legal.Obligations = nil
		req.Risk.Obligations = nil
		res2 := Compose(req)
		if res2.Decision != "ALLOW" {
			t.Fatalf("want ALLOW got %s", res2.Decision)
		}
	})
}

func TestTodo_GOVERN_002_Race(t *testing.T) {
	req := baseReq()
	done := make(chan string, 8)
	for i := 0; i < 8; i++ {
		go func() { done <- Compose(req).Digest }()
	}
	first := <-done
	for i := 1; i < 8; i++ {
		if v := <-done; v != first {
			t.Fatalf("race digest mismatch %s vs %s", first, v)
		}
	}
}

func TestTodo_GOVERN_002_Mutation(t *testing.T) {
	req := baseReq()
	req.AuthZ.Effect = "ALLOW"
	req.Legal.Effect = "COMPLIANT"
	orig := Compose(req)
	mut := req
	mut.AuthZ.Effect = "DENY"
	if Compose(mut).Decision == orig.Decision {
		t.Fatal("mutant deny should change decision")
	}
	mut2 := req
	mut2.Legal.Obligations = []string{"log", "extra"}
	if len(Compose(mut2).Obligations) == len(orig.Obligations) {
		t.Fatal("obligation addition should change")
	}
}
