package privacy

import "testing"

func disclosurePolicy() DisclosurePolicy {
	return DisclosurePolicy{ID: "analytics", Version: "1", MinCell: 5, Complementary: true, Budget: 3, RepeatedQueryBudget: 2}
}

func disclosureQuery() AnalyticsQuery {
	return AnalyticsQuery{TenantID: "t1", Principal: "analyst", Purpose: "analytics", Dimensions: []string{"department"}}
}

func TestTodo_DISCLOSURE_001_Golden(t *testing.T) {
	p := disclosurePolicy()
	q := disclosureQuery()
	b := NewDisclosureBudget()
	r, err := Apply(p, q, []AnalyticsCell{{Key: "a", Dimension: "department", Value: 2}, {Key: "b", Dimension: "department", Value: 8}}, b)
	if err != nil {
		t.Fatal(err)
	}
	if r.Evidence.Status != DisclosureSuppressed || len(r.Evidence.Suppressed) != 2 {
		t.Fatalf("suppression = %#v", r.Evidence)
	}
	if r.Evidence.QueryDigest != QueryDigest(q) || r.Evidence.PolicyDigest != p.Digest() {
		t.Fatal("evidence is not digest-bound")
	}
	round := p
	round.Complementary = false
	round.Round = 5
	r, err = Apply(round, q, []AnalyticsCell{{Key: "x", Value: 18}}, NewDisclosureBudget())
	if err != nil || r.Cells[0].Value != 15 {
		t.Fatalf("rounding = %#v, %v", r, err)
	}
}

func TestTodo_DISCLOSURE_001(t *testing.T) {
	p := disclosurePolicy()
	q := disclosureQuery()
	b := NewDisclosureBudget()
	for i := 0; i < p.RepeatedQueryBudget; i++ {
		if _, err := Apply(p, q, []AnalyticsCell{{Key: "x", Value: 9}}, b); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Apply(p, q, []AnalyticsCell{{Key: "x", Value: 9}}, b); err != ErrBudgetExceeded {
		t.Fatalf("repeat error = %v", err)
	}
}

func TestTodo_DISCLOSURE_001_Property(t *testing.T) {
	p := disclosurePolicy()
	p.Noise = 2
	p.Complementary = false
	q := disclosureQuery()
	a, err := Apply(p, q, []AnalyticsCell{{Key: "x", Value: 20}}, NewDisclosureBudget())
	if err != nil {
		t.Fatal(err)
	}
	b, err := Apply(p, q, []AnalyticsCell{{Key: "x", Value: 20}}, NewDisclosureBudget())
	if err != nil {
		t.Fatal(err)
	}
	if a.Cells[0].Value != b.Cells[0].Value || a.Evidence.QueryDigest != b.Evidence.QueryDigest {
		t.Fatal("noise is not deterministic")
	}
}

func TestTodo_DISCLOSURE_001_Security(t *testing.T) {
	p := disclosurePolicy()
	q := disclosureQuery()
	q.TenantID = ""
	if _, err := Apply(p, q, nil, NewDisclosureBudget()); err != ErrDisclosureDenied {
		t.Fatalf("missing tenant error = %v", err)
	}
	p.MinCell = 0
	if _, err := Apply(p, disclosureQuery(), nil, NewDisclosureBudget()); err != ErrInvalidDisclosurePolicy {
		t.Fatalf("invalid policy error = %v", err)
	}
	p = disclosurePolicy()
	p.Round = 10
	p.Noise = 1
	if _, err := Apply(p, disclosureQuery(), nil, NewDisclosureBudget()); err != ErrInvalidDisclosurePolicy {
		t.Fatalf("mixed transform error = %v", err)
	}
}
