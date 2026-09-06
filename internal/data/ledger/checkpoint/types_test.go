package checkpoint_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/ledger/checkpoint"
)

func TestKeyStatusUsableAtIsHalfOpenAndClosedByRevocation(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name   string
		status checkpoint.KeyStatus
		at     time.Time
		want   bool
	}{
		{"before the window", checkpoint.KeyStatus{NotBefore: from, NotAfter: to}, from.Add(-time.Nanosecond), false},
		{"exactly at the start is inside", checkpoint.KeyStatus{NotBefore: from, NotAfter: to}, from, true},
		{"inside", checkpoint.KeyStatus{NotBefore: from, NotAfter: to}, from.AddDate(0, 1, 0), true},
		{"exactly at the end is outside", checkpoint.KeyStatus{NotBefore: from, NotAfter: to}, to, false},
		{"no expiry", checkpoint.KeyStatus{NotBefore: from}, to.AddDate(10, 0, 0), true},
		{"exactly at revocation is outside", checkpoint.KeyStatus{NotBefore: from, RevokedAt: to}, to, false},
		{"before revocation is inside", checkpoint.KeyStatus{NotBefore: from, RevokedAt: to}, to.Add(-time.Nanosecond), true},
		{"no window at all", checkpoint.KeyStatus{}, from, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.status.UsableAt(tc.at); got != tc.want {
				t.Fatalf("UsableAt(%s) = %t, want %t", tc.at, got, tc.want)
			}
		})
	}
}

func TestErrorCodesAreStableAndSelfDescribing(t *testing.T) {
	tenant := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	cases := []struct {
		err       error
		code      string
		mustNames []string
	}{
		{
			checkpoint.ErrManifestInvalid{EpochNumber: 2, Missing: []string{"root digest is required", "tenant is required"}},
			"LEDGER_CHECKPOINT_MANIFEST_INVALID",
			[]string{"root digest is required", "tenant is required"},
		},
		{
			checkpoint.ErrIncompleteCoverage{Tenant: tenant, Missing: []string{"worker:2@3"}, Reason: "no chain link"},
			"LEDGER_CHECKPOINT_INCOMPLETE_COVERAGE",
			[]string{tenant.String(), "worker:2@3", "no chain link"},
		},
		{
			checkpoint.ErrKeyNotUsable{KeyID: "k1", At: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), Reason: "was revoked"},
			"LEDGER_CHECKPOINT_KEY_NOT_USABLE",
			[]string{"k1", "was revoked", "2026-03-01"},
		},
		{
			checkpoint.ErrSignatureInvalid{EpochNumber: 4, KeyID: "k1", Reason: "does not verify"},
			"LEDGER_CHECKPOINT_SIGNATURE_INVALID",
			[]string{"4", "k1", "does not verify"},
		},
		{
			checkpoint.ErrRootDigestMismatch{EpochNumber: 1, Expected: "aa", Actual: "bb"},
			"LEDGER_CHECKPOINT_ROOT_DIGEST_MISMATCH",
			[]string{"aa", "bb"},
		},
		{
			checkpoint.ErrEpochChainBroken{Tenant: tenant, EpochNumber: 3, Reason: "gap", Expected: "2", Actual: "5"},
			"LEDGER_CHECKPOINT_EPOCH_CHAIN_BROKEN",
			[]string{"3", "gap", "2", "5"},
		},
		{
			checkpoint.ErrEpochAlreadyRecorded{Tenant: tenant, EpochNumber: 7},
			"LEDGER_CHECKPOINT_EPOCH_ALREADY_RECORDED",
			[]string{"7"},
		},
		{
			checkpoint.ErrEpochNotFound{Tenant: tenant, EpochNumber: 9},
			"LEDGER_CHECKPOINT_EPOCH_NOT_FOUND",
			[]string{"9"},
		},
	}
	for _, tc := range cases {
		msg := tc.err.Error()
		if !strings.HasPrefix(msg, tc.code+":") {
			t.Errorf("%T reports %q, want it to start with %q", tc.err, msg, tc.code+":")
		}
		for _, name := range tc.mustNames {
			if !strings.Contains(msg, name) {
				t.Errorf("%T reports %q, want it to name %q", tc.err, msg, name)
			}
		}
	}
}

func TestEpochChainBrokenOmitsAnEmptyComparison(t *testing.T) {
	err := checkpoint.ErrEpochChainBroken{EpochNumber: 2, Reason: "only a reason"}
	if strings.Contains(err.Error(), "expected") {
		t.Fatalf("error %q reports an empty expected/actual pair", err)
	}
}
