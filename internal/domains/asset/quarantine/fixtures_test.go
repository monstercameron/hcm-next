package quarantine_test

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/asset/quarantine"
)

// testNow is the fixed instant tests hand [quarantine.Upload] wherever the
// exact time is not itself under test.
var testNow = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

func testPolicy(allowed ...quarantine.ContentType) quarantine.Policy {
	return quarantine.Policy{
		MaxContentBytes:     1024,
		AllowedContentTypes: allowed,
		ScannerID:           "fake-scanner",
		ScannerVersion:      "1.0.0",
	}
}

// fakeStore is an in-memory [quarantine.Store] double. It reproduces the two
// invariants the PostgreSQL-backed implementation
// (internal/data/artifacts, migrations/00030_artifact_quarantine.sql) is
// responsible for: RecordQuarantined is idempotent by (tenant, content id),
// and CurrentState always reports the most recently appended verdict, never
// an aggregate.
type fakeStore struct {
	mu          sync.Mutex
	quarantined map[string]quarantine.QuarantinedRecord
	verdicts    map[string][]quarantine.VerdictRecord
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		quarantined: map[string]quarantine.QuarantinedRecord{},
		verdicts:    map[string][]quarantine.VerdictRecord{},
	}
}

func fakeKey(tenant, contentID string) string { return tenant + "|" + contentID }

func (s *fakeStore) RecordQuarantined(ctx context.Context, tenant string, rec quarantine.QuarantinedRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := fakeKey(tenant, rec.ContentID)
	if existing, ok := s.quarantined[key]; ok {
		if existing.ByteSize != rec.ByteSize || existing.DeclaredContentType != rec.DeclaredContentType {
			return errors.New("fakeStore: quarantine identity conflict")
		}
		return nil
	}
	s.quarantined[key] = rec
	s.verdicts[key] = append(s.verdicts[key], quarantine.VerdictRecord{
		ContentID:  rec.ContentID,
		State:      quarantine.Quarantined,
		EvidenceID: rec.EvidenceID,
	})
	return nil
}

func (s *fakeStore) RecordVerdict(ctx context.Context, tenant string, rec quarantine.VerdictRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := fakeKey(tenant, rec.ContentID)
	if _, ok := s.quarantined[key]; !ok {
		return quarantine.ErrNotFound{ContentID: rec.ContentID}
	}
	s.verdicts[key] = append(s.verdicts[key], rec)
	return nil
}

func (s *fakeStore) CurrentState(ctx context.Context, tenant, contentID string) (quarantine.StateRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := fakeKey(tenant, contentID)
	history := s.verdicts[key]
	if len(history) == 0 {
		return quarantine.StateRecord{}, quarantine.ErrNotFound{ContentID: contentID}
	}
	latest := history[len(history)-1]
	return quarantine.StateRecord{
		ContentID:      latest.ContentID,
		State:          latest.State,
		ScannerID:      latest.ScannerID,
		ScannerVersion: latest.ScannerVersion,
		Reason:         latest.Reason,
		EvidenceID:     latest.EvidenceID,
		RecordedAt:     testNow,
	}, nil
}

// verdictCount reports how many verdict-log rows (including the implicit
// QUARANTINED one) have been recorded for a content id, so a test can assert
// exactly how many facts an upload produced.
func (s *fakeStore) verdictCount(tenant, contentID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.verdicts[fakeKey(tenant, contentID)])
}

// fakeScanner is a configurable [quarantine.Scanner] double: it returns
// verdict for every call unless err is set, in which case it returns err and
// a zero Verdict, exactly like a scanner that failed to run.
type fakeScanner struct {
	verdict quarantine.Verdict
	err     error
	// calls records the digest argument of every Scan call, so a test can
	// assert the scanner was (or was not) invoked at all, and with which
	// content id.
	calls []string
}

func (s *fakeScanner) Scan(ctx context.Context, digest string, r io.Reader) (quarantine.Verdict, error) {
	s.calls = append(s.calls, digest)
	if s.err != nil {
		return quarantine.Verdict{}, s.err
	}
	return s.verdict, nil
}

var errScannerUnavailable = errors.New("scanner offline")
