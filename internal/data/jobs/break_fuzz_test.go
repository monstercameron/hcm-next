package jobs

// Fuzz break attempts against the jobs validators. Oracle: verdicts may
// vary, panics must not.

import (
	"strings"
	"testing"
)

func FuzzBreak_CausalValidation(f *testing.F) {
	seeds := []string{
		"",
		" ",
		"x",
		"4bf92f3577b34da6a3ce929d0e0e4736",
		"4BF92F3577B34DA6A3CE929D0E0E4736", // uppercase hex
		"00f067aa0ba902b7",
		strings.Repeat("0", 32), // all-zero trace
		strings.Repeat("0", 16), // all-zero span
		"4bf92f35",              // short
		strings.Repeat("4", 64), // long
		"zz" + strings.Repeat("4", 30),
		"0x4bf92f3577b34da6a3ce929d0e0e4736",   // 0x prefix
		"4bf92f35-77b3-4da6-a3ce-929d0e0e4736", // UUID dashes
		"vendor=value",
		"a,b=c",
		" padded ",
		"\x00",
		strings.Repeat("x", 128),
		strings.Repeat("x", 129),
		strings.Repeat("x", 10000),
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		_ = validTraceID(s, 16)
		_ = validTraceID(s, 8)
		_ = validTraceID(s, 32)
		_ = validTraceID(s, 0)
		_ = validTraceState(s)
		_ = boundedIdentifier(s)
		_ = isHex64(s)
	})
}
