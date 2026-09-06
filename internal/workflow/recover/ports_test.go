package recover

import (
	"testing"
	"time"
)

func TestPorts_ClockFuncReportsTheInstantItWasGiven(t *testing.T) {
	want := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	var clock Clock = ClockFunc(func() time.Time { return want })
	if got := clock.Now(); !got.Equal(want) {
		t.Fatalf("ClockFunc.Now() = %s, want %s", got, want)
	}
}

func TestPorts_FixedClockNeverMoves(t *testing.T) {
	want := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	var clock Clock = FixedClock(want)
	first, second := clock.Now(), clock.Now()
	if !first.Equal(want) || !second.Equal(want) {
		t.Fatalf("FixedClock reported %s then %s, want %s twice", first, second, want)
	}
}

func TestPorts_EffectOutcomeDistinguishesReplayFromPerform(t *testing.T) {
	performed := EffectOutcome{}
	if performed.Replayed {
		t.Fatal("the zero EffectOutcome claims it was replayed")
	}
	replayed := EffectOutcome{Replayed: true}
	if !replayed.Replayed {
		t.Fatal("Replayed did not survive being set")
	}
}
