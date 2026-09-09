package schemasnapshot_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/schemasnapshot"
)

func TestWellFormednessValidator(t *testing.T) {
	v := schemasnapshot.WellFormednessValidator()
	cases := []struct {
		name   string
		format schemasnapshot.DeclaredFormat
		raw    []byte
		want   bool
	}{
		{"unrecognized format", schemasnapshot.DeclaredFormat("COBOL_COPYBOOK"), []byte("x"), false},
		{"xml with no tokens", schemasnapshot.FormatXSD, []byte(""), false},
		{"csv header with blank column", schemasnapshot.FormatCSVHeader, []byte("id,,email\n"), false},
		{"csv header with invalid utf8", schemasnapshot.FormatCSVHeader, []byte{0xff, 0xfe}, false},
		{"plain text with NUL byte", schemasnapshot.FormatGraphQLSDL, []byte("type A {\x00}"), false},
		{"plain text empty", schemasnapshot.FormatProtobuf, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			passed, detail := v.Check(tc.format, tc.raw)
			if passed != tc.want {
				t.Fatalf("Check(%s, %q) passed=%v detail=%q, want passed=%v", tc.format, tc.raw, passed, detail, tc.want)
			}
			if detail == "" {
				t.Fatal("Check returned no detail")
			}
		})
	}
}

func TestSizeBoundValidator(t *testing.T) {
	cases := []struct {
		name     string
		maxBytes int64
		raw      []byte
		want     bool
	}{
		{"misconfigured bound", 0, []byte("x"), false},
		{"empty content", 100, nil, false},
		{"within bound", 100, []byte("hello"), true},
		{"exceeds bound", 4, []byte("hello"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := schemasnapshot.SizeBoundValidator(tc.maxBytes)
			passed, _ := v.Check(schemasnapshot.FormatJSONSchema, tc.raw)
			if passed != tc.want {
				t.Fatalf("Check() passed=%v, want %v", passed, tc.want)
			}
		})
	}
}

func TestDefaultForbiddenContentScan(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		blocked bool
	}{
		{"clean content", `{"type":"object"}`, false},
		{"pem marker", "-----BEGIN PRIVATE KEY-----\nMIIB...", true},
		{"aws key prefix", "key=AKIAABCDEFGHIJKLMNOP", true},
		{"github token prefix", "token=ghp_1234567890abcdef1234567890abcdef1234", true},
		{"jwt shaped token", "auth: eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dQw4w9WgXcQ_m3rHKR3Y", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			blocked, reason := schemasnapshot.DefaultForbiddenContentScan([]byte(tc.raw))
			if blocked != tc.blocked {
				t.Fatalf("DefaultForbiddenContentScan(%q) blocked=%v reason=%q, want %v", tc.raw, blocked, reason, tc.blocked)
			}
			if blocked && reason == "" {
				t.Fatal("blocked content produced no reason")
			}
		})
	}
}

func TestForbiddenContentValidatorCustomScanner(t *testing.T) {
	custom := func(raw []byte) (bool, string) {
		if strings.Contains(string(raw), "denyme") {
			return true, "custom policy hit"
		}
		return false, ""
	}
	v := schemasnapshot.ForbiddenContentValidator(custom)
	if passed, _ := v.Check(schemasnapshot.FormatJSONSchema, []byte("denyme")); passed {
		t.Fatal("custom scanner should have blocked this content")
	}
	if passed, _ := v.Check(schemasnapshot.FormatJSONSchema, []byte("fine")); !passed {
		t.Fatal("custom scanner should have allowed this content")
	}
}

func TestDecideNoShortCircuit(t *testing.T) {
	// Every validator runs even after one has already failed: Decide must
	// not short-circuit, so Evidence always shows the complete picture.
	calls := 0
	always := schemasnapshot.Validator{Name: "ALWAYS_RUNS", Check: func(schemasnapshot.DeclaredFormat, []byte) (bool, string) {
		calls++
		return true, "ran"
	}}
	fails := schemasnapshot.Validator{Name: "ALWAYS_FAILS", Check: func(schemasnapshot.DeclaredFormat, []byte) (bool, string) {
		return false, "nope"
	}}
	verdict, results := schemasnapshot.Decide(schemasnapshot.FormatJSONSchema, []byte("x"), []schemasnapshot.Validator{fails, always})
	if verdict != schemasnapshot.StateRejected {
		t.Fatalf("verdict = %s, want REJECTED", verdict)
	}
	if calls != 1 {
		t.Fatalf("validator after a failure ran %d times, want 1 (no short-circuit)", calls)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
}
