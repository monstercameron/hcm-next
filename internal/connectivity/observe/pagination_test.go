package observe_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/fakeincumbent"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/observe"
)

// TestTodo_INTG_008 is the INTG-008 primary test.
//
// RED: a duplicate or missing page, unstable ordering, token expiry, throttle,
// partial response or restart loses or duplicates an observation.
// GREEN: stable tie-break pagination and fenced checkpoint resume produce one
// immutable observation per semantic source version.
func TestTodo_INTG_008(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("a bounded traversal sees every record exactly once", func(t *testing.T) {
		t.Parallel()
		for _, object := range connectivity.ObjectKinds() {
			h := newHarness(t)
			total := h.Incumbent.RecordCount(object)

			result, err := observe.RunToCompletion(ctx, h.Runner, request(object), 32)
			if err != nil {
				t.Fatalf("%s: run: %v", object, err)
			}
			if result.Status != observe.RunCompleted {
				t.Fatalf("%s: status %s, want COMPLETED", object, result.Status)
			}
			if result.Records != total {
				t.Fatalf("%s: observed %d records, source holds %d", object, result.Records, total)
			}
			if result.Duplicate != 0 {
				t.Fatalf("%s: a clean run reported %d duplicate pages", object, result.Duplicate)
			}

			pages := observedPages(t, h.Store, object)
			if len(pages) != result.Pages {
				t.Fatalf("%s: stored %d pages, ran %d", object, len(pages), result.Pages)
			}
			seen := map[string]int{}
			counted := 0
			for i, page := range pages {
				if page.PageSequence != uint64(i+1) {
					t.Fatalf("%s: page %d has sequence %d", object, i, page.PageSequence)
				}
				counted += page.RecordCount
				if _, dup := seen[page.ContentDigest]; dup {
					t.Fatalf("%s: two pages share digest %s", object, page.ContentDigest)
				}
				seen[page.ContentDigest] = i
			}
			if counted != total {
				t.Fatalf("%s: stored evidence covers %d records, source holds %d", object, counted, total)
			}
			if !pages[len(pages)-1].Complete {
				t.Fatalf("%s: the last stored page is not marked complete", object)
			}
		}
	})

	t.Run("a re-run after completion observes nothing new", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		first, err := observe.RunToCompletion(ctx, h.Runner, request(connectivity.ObjectWorker), 32)
		if err != nil {
			t.Fatalf("first run: %v", err)
		}
		before := len(observedPages(t, h.Store, connectivity.ObjectWorker))

		second, err := h.Runner.Run(ctx, request(connectivity.ObjectWorker))
		if err != nil {
			t.Fatalf("second run: %v", err)
		}
		if second.Status != observe.RunAlreadyComplete {
			t.Fatalf("re-run status is %s, want ALREADY_COMPLETE", second.Status)
		}
		if second.Pages != 0 {
			t.Fatalf("re-run read %d pages", second.Pages)
		}
		if after := len(observedPages(t, h.Store, connectivity.ObjectWorker)); after != before {
			t.Fatalf("re-run added %d observations", after-before)
		}
		if second.Checkpoint.Fence != first.Checkpoint.Fence {
			t.Fatalf("re-run moved the fence from %d to %d",
				first.Checkpoint.Fence, second.Checkpoint.Fence)
		}
	})

	t.Run("a one-page-at-a-time run resumes from its checkpoint", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		object := connectivity.ObjectPosition
		total := h.Incumbent.RecordCount(object)

		req := request(object)
		req.MaxPages = 1
		var records, pages int
		var lastFence uint64
		for range 16 {
			result, err := h.Runner.Run(ctx, req)
			if err != nil {
				t.Fatalf("bounded run: %v", err)
			}
			if result.Status == observe.RunAlreadyComplete {
				break
			}
			if result.Pages != 1 {
				t.Fatalf("a MaxPages=1 run read %d pages", result.Pages)
			}
			if result.Checkpoint.Fence <= lastFence {
				t.Fatalf("fence did not advance: %d then %d", lastFence, result.Checkpoint.Fence)
			}
			lastFence = result.Checkpoint.Fence
			records += result.Records
			pages++
			if result.Duplicate != 0 {
				t.Fatalf("a resumed run re-observed %d pages", result.Duplicate)
			}
			if result.Status == observe.RunCompleted {
				break
			}
		}
		if records != total {
			t.Fatalf("resumed traversal observed %d records, source holds %d", records, total)
		}
		if stored := len(observedPages(t, h.Store, object)); stored != pages {
			t.Fatalf("stored %d observations across %d resumed pages", stored, pages)
		}
	})

	t.Run("a crash between append and checkpoint re-observes idempotently", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		object := connectivity.ObjectPosition
		total := h.Incumbent.RecordCount(object)

		// The checkpoint store loses its second commit, exactly as a process
		// dying between the durable append and the checkpoint would.
		flaky := &losingCheckpoints{CheckpointStore: h.Store, loseCommit: 2}
		h.Runner.Checkpoints = flaky

		if _, err := h.Runner.Run(ctx, request(object)); err == nil {
			t.Fatal("the lost commit did not surface as an error")
		}
		afterCrash := len(observedPages(t, h.Store, object))
		if afterCrash < 2 {
			t.Fatalf("only %d observations survived the crash", afterCrash)
		}

		// A new worker resumes from the last durable checkpoint.
		h.Runner.Checkpoints = h.Store
		result, err := observe.RunToCompletion(ctx, h.Runner, request(object), 32)
		if err != nil {
			t.Fatalf("resume after crash: %v", err)
		}
		if result.Duplicate == 0 {
			t.Fatal("the resumed run re-read no page; the crash window was not exercised")
		}

		pages := observedPages(t, h.Store, object)
		counted := 0
		for i, page := range pages {
			if page.PageSequence != uint64(i+1) {
				t.Fatalf("page %d has sequence %d after resume", i, page.PageSequence)
			}
			counted += page.RecordCount
		}
		if counted != total {
			t.Fatalf("after crash and resume the evidence covers %d records, source holds %d",
				counted, total)
		}
	})

	t.Run("unstable ordering is rejected rather than observed", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.Runner.Connector = &unstableConnector{inner: h.Incumbent}
		_, err := h.Runner.Run(ctx, request(connectivity.ObjectWorker))
		if !errors.Is(err, connectivity.ErrInvalid) {
			t.Fatalf("an unordered page was accepted: %v", err)
		}
		if pages := observedPages(t, h.Store, connectivity.ObjectWorker); len(pages) != 0 {
			t.Fatalf("an unordered page produced %d observations", len(pages))
		}
	})

	t.Run("bounds are enforced, not clamped", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		req := request(connectivity.ObjectWorker)
		req.PageSize = h.Runner.Connection.Bounds().MaxPageSize + 1
		if _, err := h.Runner.Run(ctx, req); !errors.Is(err, connectivity.ErrBounds) {
			t.Fatalf("an over-large page size returned %v, want ErrBounds", err)
		}
		if calls := len(h.Incumbent.Calls()); calls != 0 {
			t.Fatalf("a bounds violation still made %d external calls", calls)
		}
	})

	t.Run("a stale worker cannot rewind the traversal", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		result, err := observe.RunToCompletion(ctx, h.Runner, request(connectivity.ObjectWorker), 32)
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		stale := result.Checkpoint
		stale.Fence--
		stale.Complete = false
		if err := h.Store.Commit(ctx, stale); !errors.Is(err, observe.ErrFenced) {
			t.Fatalf("a stale commit returned %v, want ErrFenced", err)
		}
		current, ok, err := h.Store.Load(ctx, stale.Key)
		if err != nil || !ok {
			t.Fatalf("load checkpoint: %v (found=%t)", err, ok)
		}
		if !current.Complete || current.Fence != result.Checkpoint.Fence {
			t.Fatalf("the stale commit changed the checkpoint: %+v", current)
		}
	})
}

// TestTodo_INTG_008_Fault drives the provider failures that a paginated read
// has to survive: throttling, token expiry, partial responses and schema drift.
func TestTodo_INTG_008_Fault(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("a throttled read interrupts the run and leaves a usable checkpoint", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		object := connectivity.ObjectPosition
		total := h.Incumbent.RecordCount(object)
		h.Incumbent.InjectFaults(fakeincumbent.Faults{TransientOnReads: []int{2}})

		first, err := h.Runner.Run(ctx, request(object))
		if err != nil {
			t.Fatalf("interrupted run returned an error rather than a result: %v", err)
		}
		if first.Status != observe.RunInterrupted {
			t.Fatalf("status is %s, want INTERRUPTED", first.Status)
		}
		if !errors.Is(first.Cause, connectivity.ErrTransient) {
			t.Fatalf("cause is %v, want ErrTransient", first.Cause)
		}
		if class, ok := connectivity.ClassOf(first.Cause); !ok || !class.Retryable() {
			t.Fatalf("a throttle was not classified retryable: %v", first.Cause)
		}
		if first.Pages != 1 || first.Checkpoint.Fence == 0 {
			t.Fatalf("the interrupted run left no usable checkpoint: %+v", first.Checkpoint)
		}

		h.Incumbent.ClearFaults()
		resumed, err := observe.RunToCompletion(ctx, h.Runner, request(object), 32)
		if err != nil {
			t.Fatalf("resume: %v", err)
		}
		if resumed.Status != observe.RunCompleted {
			t.Fatalf("resumed status is %s", resumed.Status)
		}
		counted := 0
		for _, page := range observedPages(t, h.Store, object) {
			counted += page.RecordCount
		}
		if counted != total {
			t.Fatalf("after throttling and resume the evidence covers %d of %d records", counted, total)
		}
	})

	t.Run("an expired token stops the run instead of restarting it silently", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.Incumbent.InjectFaults(fakeincumbent.Faults{ExpireCursorsFromPage: 1})

		result, err := h.Runner.Run(ctx, request(connectivity.ObjectPosition))
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if result.Status != observe.RunInterrupted {
			t.Fatalf("status is %s, want INTERRUPTED", result.Status)
		}
		if !errors.Is(result.Cause, connectivity.ErrCursor) {
			t.Fatalf("cause is %v, want ErrCursor", result.Cause)
		}
		if result.Pages != 1 {
			t.Fatalf("the run observed %d pages before the token expired", result.Pages)
		}
	})

	t.Run("a partial response is recorded as partial, never as complete", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.Incumbent.InjectFaults(fakeincumbent.Faults{PartialOnReads: []int{1}})

		result, err := observe.RunToCompletion(ctx, h.Runner, request(connectivity.ObjectPosition), 32)
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if result.Freshness != observe.FreshnessPartial {
			t.Fatalf("run freshness is %s, want PARTIAL", result.Freshness)
		}
		pages := observedPages(t, h.Store, connectivity.ObjectPosition)
		if pages[0].Freshness != observe.FreshnessPartial {
			t.Fatalf("the partial page reports freshness %s", pages[0].Freshness)
		}
		if pages[0].Complete {
			t.Fatal("a partial page was recorded as completing the traversal")
		}
		counted := 0
		for _, page := range pages {
			counted += page.RecordCount
		}
		if want := h.Incumbent.RecordCount(connectivity.ObjectPosition); counted != want {
			t.Fatalf("a partial page lost records: observed %d of %d", counted, want)
		}
	})

	t.Run("schema drift mid-traversal aborts rather than mixing shapes", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.Incumbent.InjectFaults(fakeincumbent.Faults{DriftAfterReads: 1})

		result, err := h.Runner.Run(ctx, request(connectivity.ObjectPosition))
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if result.Status != observe.RunInterrupted {
			t.Fatalf("status is %s, want INTERRUPTED", result.Status)
		}
		if !errors.Is(result.Cause, connectivity.ErrSchema) {
			t.Fatalf("cause is %v, want ErrSchema", result.Cause)
		}
		if result.Freshness != observe.FreshnessUnavailable {
			t.Fatalf("freshness after drift is %s, want UNAVAILABLE", result.Freshness)
		}
		pages := observedPages(t, h.Store, connectivity.ObjectPosition)
		shapes := map[string]bool{}
		for _, page := range pages {
			shapes[page.SchemaVersion] = true
		}
		if len(shapes) != 1 {
			t.Fatalf("stored evidence mixes %d schema versions: %v", len(shapes), shapes)
		}
	})

	t.Run("a credential or permission failure observes nothing", func(t *testing.T) {
		t.Parallel()
		for name, faults := range map[string]fakeincumbent.Faults{
			"credential": {CredentialInvalid: true},
			"permission": {PermissionDenied: []connectivity.ObjectKind{connectivity.ObjectWorker}},
		} {
			h := newHarness(t)
			h.Incumbent.InjectFaults(faults)
			_, err := h.Runner.Run(ctx, request(connectivity.ObjectWorker))
			if err == nil {
				t.Fatalf("%s: the run succeeded", name)
			}
			if pages := observedPages(t, h.Store, connectivity.ObjectWorker); len(pages) != 0 {
				t.Fatalf("%s: %d observations were stored", name, len(pages))
			}
		}
	})
}

// TestTodo_INTG_008_Security proves the read path cannot write.
//
// The first half is structural: the Connector interface's method set is
// reflected over and every method name checked against a closed read-only
// vocabulary, so adding a Write method to the port breaks this test rather
// than shipping. The second half is behavioural: a full traversal of every
// object leaves the incumbent's content fingerprint unchanged and its call log
// free of any non-read operation.
func TestTodo_INTG_008_Security(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("the connector port exposes no mutating method", func(t *testing.T) {
		t.Parallel()
		allowed := map[string]bool{
			"Descriptor": true, "Bounds": true, "Capabilities": true,
			"SchemaVersion": true, "Snapshot": true, "Read": true,
		}
		mutatingVerbs := []string{
			"write", "create", "update", "delete", "put", "post", "patch",
			"upsert", "insert", "remove", "send", "apply", "commit", "mutate",
			"set", "exec", "do", "call", "dispatch", "publish", "sync",
		}
		connector := reflect.TypeFor[connectivity.Connector]()
		if connector.NumMethod() != len(allowed) {
			t.Fatalf("Connector has %d methods, the allowlist names %d",
				connector.NumMethod(), len(allowed))
		}
		for i := range connector.NumMethod() {
			method := connector.Method(i)
			if !allowed[method.Name] {
				t.Fatalf("Connector exposes %q, which is not in the read-only allowlist", method.Name)
			}
			lowered := strings.ToLower(method.Name)
			for _, verb := range mutatingVerbs {
				if strings.HasPrefix(lowered, verb) {
					t.Fatalf("Connector method %q begins with the mutating verb %q", method.Name, verb)
				}
			}
			assertInertResults(t, method.Name, method.Type, mutatingVerbs)
		}
	})

	t.Run("a full traversal changes nothing in the external system", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		before, err := h.Incumbent.Fingerprint()
		if err != nil {
			t.Fatalf("fingerprint: %v", err)
		}
		for _, object := range connectivity.ObjectKinds() {
			if _, err := observe.RunToCompletion(ctx, h.Runner, request(object), 32); err != nil {
				t.Fatalf("%s: run: %v", object, err)
			}
		}
		after, err := h.Incumbent.Fingerprint()
		if err != nil {
			t.Fatalf("fingerprint: %v", err)
		}
		if before != after {
			t.Fatalf("the external system changed during observation:\nbefore %s\nafter  %s", before, after)
		}
		if n := h.Incumbent.MutatingCalls(); n != 0 {
			t.Fatalf("observation performed %d mutating calls", n)
		}
		if calls := h.Incumbent.Calls(); len(calls) == 0 {
			t.Fatal("observation made no external call at all; the proof is vacuous")
		} else {
			for _, call := range calls {
				switch call.Op {
				case fakeincumbent.OpRead, fakeincumbent.OpSnapshot, fakeincumbent.OpSchemaVersion:
				default:
					t.Fatalf("call log records operation %q", call.Op)
				}
			}
		}
	})
}

// assertInertResults checks that nothing a port method hands back is itself a
// door to the external system: every result is a plain value, not an interface
// or a type with methods of its own.
func assertInertResults(t *testing.T, method string, fn reflect.Type, mutatingVerbs []string) {
	t.Helper()
	for i := range fn.NumOut() {
		out := fn.Out(i)
		if out == reflect.TypeFor[error]() {
			continue
		}
		if out.Kind() == reflect.Interface {
			t.Fatalf("%s returns the interface %s, which could carry a mutating method", method, out)
		}
		for m := range out.NumMethod() {
			lowered := strings.ToLower(out.Method(m).Name)
			for _, verb := range mutatingVerbs {
				if strings.HasPrefix(lowered, verb) {
					t.Fatalf("%s returns %s, whose method %q begins with the mutating verb %q",
						method, out, out.Method(m).Name, verb)
				}
			}
		}
	}
}

// FuzzTodo_INTG_008 fuzzes cursor encoding. A token must round-trip exactly, a
// tampered token must be rejected rather than silently addressing a different
// position, and nothing may panic.
func FuzzTodo_INTG_008(f *testing.F) {
	f.Add("snap-1", "W-1001", "id-1", uint64(3), int64(0))
	f.Add("", "", "", uint64(0), int64(0))
	f.Add("snap#abc", "\x00\xff", "\U0001F600", uint64(1<<40), int64(1700000000))
	f.Add(strings.Repeat("s", 300), "z", "z", uint64(1), int64(-1))

	f.Fuzz(func(t *testing.T, snapshot, sortKey, externalID string, page uint64, expiryUnix int64) {
		cursor := connectivity.Cursor{
			SnapshotID:     snapshot,
			LastSortKey:    sortKey,
			LastExternalID: externalID,
			Page:           page,
		}
		if expiryUnix != 0 {
			cursor.ExpiresAt = time.Unix(expiryUnix, 0).UTC()
		}

		token, err := cursor.Token()
		if err != nil {
			t.Fatalf("encode cursor: %v", err)
		}
		back, err := connectivity.ParseCursor(token)
		if err != nil {
			t.Fatalf("parse own token: %v", err)
		}
		if back != cursor {
			t.Fatalf("cursor did not round-trip:\n got %+v\nwant %+v", back, cursor)
		}
		again, err := cursor.Token()
		if err != nil || again != token {
			t.Fatalf("token is not deterministic: %q then %q (%v)", token, again, err)
		}

		// Tampering is detected, never silently accepted.
		for _, tampered := range tamperings(token) {
			parsed, err := connectivity.ParseCursor(tampered)
			if err == nil && parsed != cursor {
				t.Fatalf("tampered token %q parsed to a different position %+v", tampered, parsed)
			}
			if err != nil && !errors.Is(err, connectivity.ErrCursor) {
				t.Fatalf("tampered token %q failed with %v, want ErrCursor", tampered, err)
			}
		}
	})
}

func tamperings(token string) []string {
	out := []string{token + "x", "x" + token}
	if len(token) > 2 {
		out = append(out, token[:len(token)-1], token[1:])
	}
	parts := strings.Split(token, ".")
	if len(parts) == 3 {
		out = append(out, parts[0]+"."+parts[1]+".0000000000000000")
		out = append(out, fmt.Sprintf("%s.%s.%s", parts[0], parts[1]+"A", parts[2]))
	}
	return out
}

// losingCheckpoints drops one commit, simulating a process that died between a
// durable append and the checkpoint that would have skipped past it.
type losingCheckpoints struct {
	observe.CheckpointStore
	loseCommit int
	commits    int
}

func (l *losingCheckpoints) Commit(ctx context.Context, cp observe.Checkpoint) error {
	l.commits++
	if l.commits == l.loseCommit {
		return errors.New("simulated crash before the checkpoint was committed")
	}
	return l.CheckpointStore.Commit(ctx, cp)
}

// unstableConnector returns records in an order the pagination contract
// forbids, which a caller must reject rather than record as evidence.
type unstableConnector struct {
	inner connectivity.Connector
}

func (u *unstableConnector) Descriptor() connectivity.Descriptor { return u.inner.Descriptor() }
func (u *unstableConnector) Bounds() connectivity.Bounds         { return u.inner.Bounds() }
func (u *unstableConnector) Capabilities() []connectivity.Capability {
	return u.inner.Capabilities()
}

func (u *unstableConnector) SchemaVersion(ctx context.Context, object connectivity.ObjectKind) (string, error) {
	return u.inner.SchemaVersion(ctx, object)
}

func (u *unstableConnector) Snapshot(ctx context.Context, object connectivity.ObjectKind) (string, error) {
	return u.inner.Snapshot(ctx, object)
}

func (u *unstableConnector) Read(ctx context.Context, req connectivity.ReadRequest) (connectivity.Page, error) {
	page, err := u.inner.Read(ctx, req)
	if err != nil || len(page.Records) < 2 {
		return page, err
	}
	page.Records[0], page.Records[1] = page.Records[1], page.Records[0]
	return page, nil
}
