package fixture

import "testing"

func TestExecutionOnly(t *testing.T) {
	defer func() {
		if recover() != nil {
			t.Fatal("unexpected panic")
		}
	}()
	if Decide(Input{Allowed: true, CurrentVersion: 1, ExpectedVersion: 1}) == nil {
		t.Fatal("non-nil result expected")
	}
}

func TestTodo_GOV_021_Mutation(t *testing.T) {
	cases := []struct {
		name  string
		input Input
		want  error
	}{
		{name: "denies unauthorized input", input: Input{CurrentVersion: 1, ExpectedVersion: 1}, want: ErrDenied},
		{name: "denies stale version", input: Input{Allowed: true, CurrentVersion: 1, ExpectedVersion: 2}, want: ErrStale},
		{name: "allows current authorized input", input: Input{Allowed: true, CurrentVersion: 2, ExpectedVersion: 2}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Decide(tc.input); got != tc.want {
				t.Fatalf("Decide(%+v) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}
