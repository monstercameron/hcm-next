package outbox

// Fuzz break attempts against the outbox trace-identity validators. These
// guard every trace link the pipeline persists; a panic on adversarial
// input would turn hostile telemetry metadata into a crash. Oracle:
// verdicts may vary, panics must not.

import (
	"strings"
	"testing"
)

func FuzzBreak_TraceStateValidation(f *testing.F) {
	seeds := []string{
		"",
		"vendor=value",
		"a=1,b=2,c=3",
		"=",
		"novalue",
		",",
		"a=",
		"=b",
		"a==b",
		"UPPER=1",
		"a=1,",
		",a=1",
		"a=1,,b=2",
		" tenant=1",
		"a=1 ",
		"a=b=c",
		"rojo@tenant=1",
		"@=1",
		"a@=1",
		strings.Repeat("a=1,", 40) + "a=1", // over the 32-entry cap
		strings.Repeat("k", 300) + "=v",    // overlong key
		"k=" + strings.Repeat("v", 300),    // long value (legal size, legal charset)
		"a=\xff\xfe",                       // non-UTF8 bytes
		"a=üñí",                            // non-ASCII runes
		"a=\x19",                           // below printable range
		"a=\x7f",                           // DEL
		"a=b,c",                            // comma inside value
		"a b=c",                            // space inside key
		"9tenant=1",                        // leading digit without vendor
		"1@vendor=1",                       // tenant-first multitenant key
		"a*/_-$=ok",                        // every allowed key symbol
		"a=tab\there",                      // interior tab
		"a=new\nline",                      // interior newline
		"a=trailing ",                      // trailing space
		" leading=a",                       // leading space
		"dup=1,dup=2",                      // duplicate key
		"Dup=1,dup=2",                      // case-distinct keys
		"a=" + strings.Repeat("=", 10),     // extra equals
		strings.Repeat("x", 256),           // exactly at the length cap, no equals
		strings.Repeat("y", 257),           // one past the length cap
		"*=1",                              // wildcard key
		"a=1;b=2",                          // wrong separator
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		_ = validTraceState(s)
		_ = validTraceStateKey(s)
		_ = validTraceStateValue(s)
		_ = validHexID(s, 8)
		_ = validHexID(s, 16)
		_ = validHexID(s, 32)
	})
}

func FuzzBreak_CausalBoundaries(f *testing.F) {
	seeds := []string{"", " ", "x", strings.Repeat("x", 128), strings.Repeat("x", 129), "corr-1", "a:b", "\x00", "ü"}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		_, _ = normalizeCausal(&CausalMetadata{
			CorrelationID: s, CausationID: s, LogicalOperationID: s, AttemptID: s,
		})
		_ = sameCausal(
			&CausalMetadata{CorrelationID: s, CausationID: s, LogicalOperationID: s, AttemptID: s},
			&CausalMetadata{CorrelationID: s, CausationID: s, LogicalOperationID: s, AttemptID: s},
		)
	})
}
