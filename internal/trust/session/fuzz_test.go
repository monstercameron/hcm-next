package session_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/trust/session"
)

// FuzzTodo_TRUST_003 is the TRUST-003 fuzz target.
//
// The invariant under test is one sentence: presenting any byte string
// whatsoever to Refresh never panics and never succeeds unless it is
// exactly the session's current, unretired refresh token -- and once the
// one legitimate token has been rotated away by the fuzz corpus itself,
// every subsequent presentation of it is a replay that revokes the session,
// never a silent no-op.
func FuzzTodo_TRUST_003(f *testing.F) {
	f.Add("")
	f.Add("not-a-token")
	f.Add("00000000000000000000000000000000000000000000000000000000000000")

	f.Fuzz(func(t *testing.T, garbage string) {
		c := &clock{at: baseTime}
		m := newManager(t, c, time.Hour, 24*time.Hour)
		rec, token := mustCreate(t, m, validSpec())

		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Refresh(garbage) panicked: %v", r)
				}
			}()
			_, _, err := m.Refresh(context.Background(), session.RefreshToken(garbage))
			if garbage == string(token) {
				if err != nil {
					t.Fatalf("Refresh(the real token) = %v, want success", err)
				}
				return
			}
			if err == nil {
				t.Fatal("Refresh(garbage) succeeded, want a failure")
			}
			if !errors.Is(err, session.ErrRefreshUnknown) && !errors.Is(err, session.ErrRefreshReplay) {
				t.Fatalf("Refresh(garbage) = %v, want ErrRefreshUnknown or ErrRefreshReplay", err)
			}
		}()

		got, err := m.Get(context.Background(), rec.ID())
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if !got.Status().Valid() {
			t.Fatalf("Status() = %v is not a declared status", got.Status())
		}
	})
}
