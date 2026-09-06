package cache

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func testKey(t *testing.T, tenant, policy, content, namespace, id string) Key {
	t.Helper()
	key, err := NewKey(tenant, policy, content, namespace, id)
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	return key
}

func allowString(string) bool { return true }

func TestTodo_CACHE_001(t *testing.T) {
	t.Run("construction refuses incomplete identity", func(t *testing.T) {
		fields := []struct {
			name string
			make func() (Key, error)
		}{
			{"tenant", func() (Key, error) { return NewKey("", "policy-1", "content-1", "worker", "1") }},
			{"policy version", func() (Key, error) { return NewKey("tenant-a", "", "content-1", "worker", "1") }},
			{"content version", func() (Key, error) { return NewKey("tenant-a", "policy-1", "", "worker", "1") }},
			{"namespace", func() (Key, error) { return NewKey("tenant-a", "policy-1", "content-1", "", "1") }},
			{"id", func() (Key, error) { return NewKey("tenant-a", "policy-1", "content-1", "worker", "") }},
		}
		for _, field := range fields {
			t.Run(field.name, func(t *testing.T) {
				if _, err := field.make(); err == nil {
					t.Fatalf("incomplete %s key was accepted", field.name)
				}
			})
		}
		if _, err := NewKey("tenant:a", "policy-1", "content-1", "worker", "1"); !errors.Is(err, ErrKeyInvalid) {
			t.Fatalf("separator-bearing tenant error = %v, want ErrKeyInvalid", err)
		}
		if (Key{}).String() != "" {
			t.Fatal("zero key must not serialize")
		}
	})

	t.Run("versioned tenant keys isolate", func(t *testing.T) {
		c := New[string](Config{MaxEntries: 10, TTL: time.Minute})
		v1 := testKey(t, "tenant-a", "policy-1", "content-1", "worker", "w1")
		v2 := testKey(t, "tenant-a", "policy-2", "content-1", "worker", "w1")
		otherTenant := testKey(t, "tenant-b", "policy-1", "content-1", "worker", "w1")
		if err := c.Set(v1, "value-1"); err != nil {
			t.Fatal(err)
		}
		if _, ok := c.Get(v2, allowString); ok {
			t.Fatal("policy version changed but cache hit")
		}
		if _, ok := c.Get(otherTenant, allowString); ok {
			t.Fatal("tenant changed but cache hit")
		}
		if got, ok := c.Get(v1, allowString); !ok || got != "value-1" {
			t.Fatalf("same complete key missed: %q, %v", got, ok)
		}
	})

	t.Run("bounded TTL and deterministic LRU size", func(t *testing.T) {
		c := New[string](Config{MaxEntries: 2, TTL: time.Second})
		fixed := time.Unix(100, 0)
		c.now = func() time.Time { return fixed }
		k1 := testKey(t, "tenant-a", "policy-1", "content-1", "item", "1")
		k2 := testKey(t, "tenant-a", "policy-1", "content-1", "item", "2")
		k3 := testKey(t, "tenant-a", "policy-1", "content-1", "item", "3")
		_ = c.Set(k1, "one")
		_ = c.Set(k2, "two")
		if _, ok := c.Get(k1, allowString); !ok {
			t.Fatal("first entry should be a hit")
		}
		_ = c.Set(k3, "three")
		if _, ok := c.Get(k2, allowString); ok {
			t.Fatal("least-recently-used entry was not evicted")
		}
		if got := c.Len(); got != 2 {
			t.Fatalf("size = %d, want 2", got)
		}
		c.now = func() time.Time { return fixed.Add(2 * time.Second) }
		if _, ok := c.Get(k1, allowString); ok {
			t.Fatal("expired entry was returned")
		}
		if c.Len() != 0 {
			t.Fatal("expired entries were not removed")
		}
	})

	t.Run("rebuild scopes tenant and versions", func(t *testing.T) {
		c := New[string](Config{MaxEntries: 10, TTL: time.Minute})
		a := testKey(t, "tenant-a", "policy-1", "content-1", "item", "a")
		b := testKey(t, "tenant-b", "policy-1", "content-2", "item", "b")
		cKey := testKey(t, "tenant-c", "policy-3", "content-3", "item", "c")
		_ = c.Set(a, "a")
		_ = c.Set(b, "b")
		_ = c.Set(cKey, "c")
		c.Rebuild(RebuildScope{Tenant: "tenant-a"})
		if _, ok := c.Get(a, allowString); ok {
			t.Fatal("tenant rebuild did not evict tenant-a")
		}
		c.RebuildVersion("policy-1", "")
		if _, ok := c.Get(b, allowString); ok {
			t.Fatal("policy rebuild did not evict policy-1")
		}
		c.RebuildVersion("", "content-3")
		if c.Len() != 0 {
			t.Fatal("content rebuild did not evict content-3")
		}
	})
}

func TestTodo_CACHE_001_Race(t *testing.T) {
	c := New[int](Config{MaxEntries: 64, TTL: time.Minute})
	key := testKey(t, "tenant-race", "policy-1", "content-1", "race", "shared")
	_ = c.Set(key, 1)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if n%2 == 0 {
					_, _ = c.Get(key, func(v int) bool { return v >= 0 })
				} else {
					c.Rebuild(RebuildScope{Tenant: "tenant-race"})
					_ = c.Set(key, n)
				}
			}
		}(i)
	}
	wg.Wait()
}

type memoryBackend[V any] struct {
	mu      sync.Mutex
	values  map[Key]V
	failGet bool
	cleared bool
}

func newMemoryBackend[V any]() *memoryBackend[V] {
	return &memoryBackend[V]{values: make(map[Key]V)}
}

func (b *memoryBackend[V]) Set(key Key, value V, _ time.Duration) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.values[key] = value
	return nil
}

func (b *memoryBackend[V]) Get(key Key) (V, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var zero V
	if b.failGet {
		return zero, false, errors.New("backend unavailable")
	}
	v, ok := b.values[key]
	return v, ok, nil
}

func (b *memoryBackend[V]) Delete(key Key) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.values, key)
	return nil
}

func (b *memoryBackend[V]) Rebuild(scope RebuildScope) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for key := range b.values {
		if scope.matches(key) {
			delete(b.values, key)
		}
	}
	return nil
}

func (b *memoryBackend[V]) Clear() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.values = make(map[Key]V)
	b.cleared = true
	return nil
}

func TestTodo_CACHE_001_Integration(t *testing.T) {
	backend := newMemoryBackend[string]()
	c := NewWithBackend[string](Config{TTL: time.Minute}, backend)
	key := testKey(t, "tenant-integration", "policy-2", "content-7", "projection", "row")
	if err := c.Set(key, "authoritative-result"); err != nil {
		t.Fatalf("provider Set surfaced an error: %v", err)
	}
	got, ok := c.Get(key, allowString)
	if !ok || got != "authoritative-result" {
		t.Fatalf("provider adapter hit = %q, %v", got, ok)
	}
	c.Rebuild(RebuildScope{Tenant: key.Tenant})
	if _, ok := c.Get(key, allowString); ok {
		t.Fatal("provider rebuild did not remove tenant entries")
	}
	c.Clear()
	if !backend.cleared {
		t.Fatal("Clear did not reach provider port")
	}
}

func TestTodo_CACHE_001_Security(t *testing.T) {
	c := New[string](Config{MaxEntries: 10, TTL: time.Minute})
	key := testKey(t, "tenant-secret", "policy-1", "content-1", "salary", "employee-1")
	otherTenant := testKey(t, "tenant-other", "policy-1", "content-1", "salary", "employee-1")
	_ = c.Set(key, "sensitive-value")
	if _, ok := c.Get(key, nil); ok {
		t.Fatal("missing authorization hook must not expose a cached value")
	}
	if c.Len() != 0 {
		t.Fatal("missing authorization hook did not evict the value")
	}
	_ = c.Set(key, "sensitive-value")
	checks := 0
	if _, ok := c.Get(key, func(value string) bool {
		checks++
		return value != "sensitive-value"
	}); ok {
		t.Fatal("authorization denial returned sensitive value")
	}
	if checks != 1 {
		t.Fatalf("authorization checks = %d, want one per hit", checks)
	}
	if _, ok := c.Get(key, allowString); ok {
		t.Fatal("denied entry remained available")
	}
	if _, ok := c.Get(otherTenant, allowString); ok {
		t.Fatal("cross-tenant lookup hit")
	}
	if got := Explain(key); got == "" || containsValue(got, "sensitive-value") {
		t.Fatalf("Explain leaked cached value: %q", got)
	}
}

func containsValue(explanation, value string) bool {
	return len(value) > 0 && len(explanation) >= len(value) && stringContains(explanation, value)
}

func stringContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestTodo_CACHE_001_Recovery(t *testing.T) {
	key := testKey(t, "tenant-recovery", "policy-1", "content-1", "projection", "row")
	c := New[string](Config{MaxEntries: 10, TTL: time.Minute})
	computed := 0
	load := func() (string, error) {
		computed++
		return "source-of-truth", nil
	}
	first, err := c.GetOrLoad(key, load, allowString)
	if err != nil || first != "source-of-truth" {
		t.Fatalf("initial computation = %q, %v", first, err)
	}
	c.Clear()
	second, err := c.GetOrLoad(key, load, allowString)
	if err != nil || second != first {
		t.Fatalf("recovery computation = %q, %v; want %q", second, err, first)
	}
	if computed != 2 {
		t.Fatalf("authoritative computations = %d, want 2 after cache loss", computed)
	}
	failing := newMemoryBackend[string]()
	failing.failGet = true
	remote := NewWithBackend[string](Config{TTL: time.Minute}, failing)
	result, err := remote.GetOrLoad(key, func() (string, error) { return "source-of-truth", nil }, allowString)
	if err != nil || result != "source-of-truth" {
		t.Fatalf("backend loss changed computed result: %q, %v", result, err)
	}
	c.Clear()
	if _, err := c.GetOrLoad(key, func() (string, error) { return "", errors.New("authoritative failure") }, allowString); err == nil {
		t.Fatal("authoritative loader failure was silently treated as a successful computation")
	}
}
