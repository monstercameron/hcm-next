package temporal

import (
	"slices"
	"testing"
	"time"
)

func TestAuthorityRowCoversIsHalfOpen(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	bounded := authorityRow{EffectiveFrom: from, EffectiveTo: &to}
	open := authorityRow{EffectiveFrom: from}

	cases := []struct {
		name string
		row  authorityRow
		at   time.Time
		want bool
	}{
		{"before the interval", bounded, from.Add(-time.Nanosecond), false},
		{"exactly at the start is inside", bounded, from, true},
		{"inside", bounded, from.AddDate(0, 1, 0), true},
		{"exactly at the end is outside", bounded, to, false},
		{"after the end", bounded, to.Add(time.Nanosecond), false},
		{"open ended, long after", open, to.AddDate(10, 0, 0), true},
		{"open ended, before the start", open, from.Add(-time.Nanosecond), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.row.covers(tc.at); got != tc.want {
				t.Fatalf("covers(%s) = %t, want %t", tc.at, got, tc.want)
			}
		})
	}
}

func TestDistinctAuthorityRefsIsSortedAndDeduplicated(t *testing.T) {
	assertions := []Assertion{
		{Authority: AuthorityLabel{Present: true, Ref: "b"}},
		{Authority: AuthorityLabel{Present: true, Ref: "a"}},
		{Authority: AuthorityLabel{Present: true, Ref: "b"}},
		{Authority: AuthorityLabel{Present: false, Ref: "ignored"}},
		{Authority: AuthorityLabel{Present: true, Ref: ""}},
	}
	got := distinctAuthorityRefs(assertions)
	if !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("distinctAuthorityRefs = %v, want [a b]", got)
	}
	if distinctAuthorityRefs(nil) != nil {
		t.Fatal("distinctAuthorityRefs(nil) is non-nil; there is nothing to query for")
	}
}
