package cache

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestTodo_CACHE_001(t *testing.T) {
	tenantA := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	tenantB := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	t.Run("key omits tenant fails", func(t *testing.T) {
		if _, err := BuildKey(uuid.Nil, "v1", "ns", "id"); err == nil {
			t.Fatal("expected error for nil tenant")
		}
	})
	t.Run("key omits version fails", func(t *testing.T) {
		if _, err := BuildKey(tenantA, "", "ns", "id"); err == nil {
			t.Fatal("expected error for empty version")
		}
	})

	t.Run("versioned keys isolate", func(t *testing.T) {
		c := New[string](Config{MaxEntries: 10, TTL: time.Minute})
		k1, _ := BuildKey(tenantA, "v1", "worker", "w1")
		k2, _ := BuildKey(tenantA, "v2", "worker", "w1")
		_ = c.Set(k1, "val1")
		if _, ok := c.Get(k2, nil); ok {
			t.Fatal("v2 key must not hit v1 entry")
		}
		if v, ok := c.Get(k1, nil); !ok || v != "val1" {
			t.Fatalf("v1 miss: %v %v", v, ok)
		}
	})

	t.Run("bounded TTL and size", func(t *testing.T) {
		c := New[string](Config{MaxEntries: 2, TTL: 10 * time.Millisecond})
		fixed := time.Now()
		c.now = func() time.Time { return fixed }
		k1, _ := BuildKey(tenantA, "v1", "ns", "1")
		k2, _ := BuildKey(tenantA, "v1", "ns", "2")
		k3, _ := BuildKey(tenantA, "v1", "ns", "3")
		_ = c.Set(k1, "a")
		_ = c.Set(k2, "b")
		if c.Len() != 2 {
			t.Fatalf("len 2 got %d", c.Len())
		}
		_ = c.Set(k3, "c")
		if c.Len() != 2 {
			t.Fatalf("bounded size violated %d", c.Len())
		}
		if _, ok := c.Get(k1, nil); ok {
			t.Fatal("LRU eviction failed")
		}
		c.now = func() time.Time { return fixed.Add(20 * time.Millisecond) }
		if _, ok := c.Get(k2, nil); ok {
			t.Fatal("TTL expiry failed")
		}
	})

	t.Run("post cache authz", func(t *testing.T) {
		c := New[string](Config{MaxEntries: 10, TTL: time.Minute})
		k, _ := BuildKey(tenantA, "v1", "salary", "emp1")
		_ = c.Set(k, "120000")
		if _, ok := c.Get(k, func(v string) bool { return false }); ok {
			t.Fatal("post-cache deny must be miss")
		}
		if v, ok := c.Get(k, func(v string) bool { return true }); !ok || v != "120000" {
			t.Fatal("allow must hit")
		}
	})

	t.Run("safe miss reloads", func(t *testing.T) {
		c := New[string](Config{MaxEntries: 10, TTL: time.Minute})
		k, _ := BuildKey(tenantA, "v1", "ns", "miss")
		calls := 0
		v, err := c.GetOrLoad(k, func() (string, error) {
			calls++
			return "rebuilt", nil
		}, nil)
		if err != nil || v != "rebuilt" || calls != 1 {
			t.Fatalf("rebuild failed %v %v %d", v, err, calls)
		}
		v2, err := c.GetOrLoad(k, func() (string, error) {
			t.Fatal("should not reload on hit")
			return "", nil
		}, nil)
		if err != nil || v2 != "rebuilt" {
			t.Fatalf("cache hit failed %v %v", v2, err)
		}
		c.Clear()
		_ = tenantB
		v3, err := c.GetOrLoad(k, func() (string, error) { return "rebuilt2", nil }, nil)
		if err != nil || v3 != "rebuilt2" {
			t.Fatalf("after clear rebuild failed %v %v", v3, err)
		}
	})
}

func TestTodo_CACHE_001_Race(t *testing.T) {
	c := New[int](Config{MaxEntries: 100, TTL: time.Minute})
	tenant := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			k, _ := BuildKey(tenant, "v1", "race", uuid.NewString())
			_ = c.Set(k, n)
			_, _ = c.Get(k, nil)
			_, _ = c.GetOrLoad(k, func() (int, error) { return n, nil }, nil)
		}(i)
	}
	wg.Wait()
}

func TestTodo_CACHE_001_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenant := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	_, err := db.Conn.Exec(t.Context(), `create table if not exists cache_src (tenant_id uuid primary key, val text not null)`)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = db.Conn.Exec(t.Context(), `insert into cache_src (tenant_id, val) values ($1,$2) on conflict (tenant_id) do update set val=$2`, tenant.String(), "from-db")
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	c := New[string](Config{MaxEntries: 10, TTL: time.Minute})
	k, _ := BuildKey(tenant, "v1", "src", "row1")
	load := func() (string, error) {
		var v string
		err := db.Conn.QueryRow(t.Context(), `select val from cache_src where tenant_id=$1`, tenant.String()).Scan(&v)
		return v, err
	}
	v, err := c.GetOrLoad(k, load, nil)
	if err != nil || v != "from-db" {
		t.Fatalf("load %v %v", v, err)
	}
	v2, err := c.GetOrLoad(k, func() (string, error) { t.Fatal("should be cached"); return "", nil }, nil)
	if err != nil || v2 != "from-db" {
		t.Fatalf("cached %v %v", v2, err)
	}
}

func TestTodo_CACHE_001_Security(t *testing.T) {
	tenantA := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	tenantB := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	c := New[string](Config{MaxEntries: 10, TTL: time.Minute})
	ka, _ := BuildKey(tenantA, "v1", "secret", "emp1")
	_ = c.Set(ka, "sensitive")
	if _, ok := c.Get(ka, nil); !ok {
		t.Fatal("owner must hit")
	}
	kb, _ := BuildKey(tenantB, "v1", "secret", "emp1")
	if _, ok := c.Get(kb, nil); ok {
		t.Fatal("cross-tenant must miss")
	}
	forged := tenantA.String() + ":v1:secret:emp1"
	if _, ok := c.Get(forged, func(v string) bool { return false }); ok {
		t.Fatal("forged with failing authz must miss")
	}
	if err := c.Set("bad-key", "x"); err == nil {
		t.Fatal("bad key must be rejected")
	}
	if err := c.Set(tenantA.String()+":v1:ns:id:with:colon", "x"); err == nil {
		t.Fatal("key with colon in id must be rejected")
	}
	_ = tenantB
}

func TestTodo_CACHE_001_Recovery(t *testing.T) {
	tenant := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	c := New[string](Config{MaxEntries: 10, TTL: time.Minute})
	k, _ := BuildKey(tenant, "v1", "ns", "recover")
	_ = c.Set(k, "old")
	if c.Len() != 1 {
		t.Fatalf("len 1 got %d", c.Len())
	}
	c.Clear()
	if c.Len() != 0 {
		t.Fatalf("clear failed %d", c.Len())
	}
	if _, ok := c.Get(k, nil); ok {
		t.Fatal("after clear must miss")
	}
	v, err := c.GetOrLoad(k, func() (string, error) { return "new", nil }, nil)
	if err != nil || v != "new" {
		t.Fatalf("rebuild after loss %v %v", v, err)
	}
	if _, ok := c.Get(k, nil); !ok {
		t.Fatal("rebuilt must hit")
	}
	_ = c.Set(k, "new2")
	c2 := New[string](Config{MaxEntries: 1, TTL: 10 * time.Millisecond})
	fixed := time.Now()
	c2.now = func() time.Time { return fixed }
	k2, _ := BuildKey(tenant, "v1", "ns", "exp")
	_ = c2.Set(k2, "x")
	c2.now = func() time.Time { return fixed.Add(20 * time.Millisecond) }
	if _, ok := c2.Get(k2, nil); ok {
		t.Fatal("expired must miss")
	}
	v3, err := c2.GetOrLoad(k2, func() (string, error) { return "rebuilt-exp", nil }, nil)
	if err != nil || v3 != "rebuilt-exp" {
		t.Fatalf("expired rebuild %v %v", v3, err)
	}
}
