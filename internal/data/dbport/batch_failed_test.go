package dbport

import "testing"

func TestFailedStatementNamesTheStatementThatDidNotComplete(t *testing.T) {
	cases := []struct {
		name   string
		counts []int64
		total  int
		want   int
	}{
		{name: "first statement failed", counts: nil, total: 3, want: 0},
		{name: "second statement failed", counts: []int64{1}, total: 3, want: 1},
		{name: "last statement failed", counts: []int64{1, 1}, total: 3, want: 2},
		{name: "close failed after every statement", counts: []int64{1, 1, 1}, total: 3, want: 2},
		{name: "nothing was queued", counts: nil, total: 0, want: -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FailedStatement(tc.counts, tc.total); got != tc.want {
				t.Fatalf("FailedStatement(%v, %d) = %d, want %d", tc.counts, tc.total, got, tc.want)
			}
		})
	}
}
