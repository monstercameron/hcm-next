package cache

import (
	"container/list"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrKeyInvalid      = errors.New("cache: key invalid")
	ErrTenantRequired  = errors.New("cache: tenant required")
	ErrVersionRequired = errors.New("cache: version required")
	ErrTTLTooLarge     = errors.New("cache: ttl too large")
	ErrSizeTooLarge    = errors.New("cache: size too large")
)

const (
	DefaultMaxEntries = 1024
	DefaultTTL        = 5 * time.Minute
	MaxTTL            = 24 * time.Hour
	MaxEntriesHard    = 100000
)

type Config struct {
	MaxEntries int
	TTL        time.Duration
}

func (c Config) normalized() Config {
	if c.MaxEntries <= 0 {
		c.MaxEntries = DefaultMaxEntries
	}
	if c.MaxEntries > MaxEntriesHard {
		c.MaxEntries = MaxEntriesHard
	}
	if c.TTL <= 0 {
		c.TTL = DefaultTTL
	}
	if c.TTL > MaxTTL {
		c.TTL = MaxTTL
	}
	return c
}

type entry[V any] struct {
	key       string
	value     V
	expiresAt time.Time
	tenant    uuid.UUID
	version   string
}

type Cache[V any] struct {
	mu    sync.Mutex
	cfg   Config
	items map[string]*list.Element
	order *list.List
	now   func() time.Time
}

func New[V any](cfg Config) *Cache[V] {
	cfg = cfg.normalized()
	return &Cache[V]{
		cfg:   cfg,
		items: make(map[string]*list.Element),
		order: list.New(),
		now:   time.Now,
	}
}

func BuildKey(tenant uuid.UUID, version, namespace, id string) (string, error) {
	if tenant == uuid.Nil {
		return "", ErrTenantRequired
	}
	if version == "" {
		return "", ErrVersionRequired
	}
	if namespace == "" {
		return "", fmt.Errorf("%w: namespace required", ErrKeyInvalid)
	}
	if id == "" {
		return "", fmt.Errorf("%w: id required", ErrKeyInvalid)
	}
	if strings.Contains(version, ":") || strings.Contains(namespace, ":") || strings.Contains(id, ":") {
		return "", fmt.Errorf("%w: field contains separator", ErrKeyInvalid)
	}
	return fmt.Sprintf("%s:%s:%s:%s", tenant.String(), version, namespace, id), nil
}

func ParseKey(key string) (uuid.UUID, string, string, string, error) {
	if strings.Count(key, ":") != 3 {
		return uuid.Nil, "", "", "", ErrKeyInvalid
	}
	parts := strings.SplitN(key, ":", 4)
	if len(parts) != 4 {
		return uuid.Nil, "", "", "", ErrKeyInvalid
	}
	tenant, err := uuid.Parse(parts[0])
	if err != nil {
		return uuid.Nil, "", "", "", ErrKeyInvalid
	}
	if tenant == uuid.Nil {
		return uuid.Nil, "", "", "", ErrTenantRequired
	}
	if parts[1] == "" {
		return uuid.Nil, "", "", "", ErrVersionRequired
	}
	return tenant, parts[1], parts[2], parts[3], nil
}

func (c *Cache[V]) Set(key string, value V) error {
	if _, _, _, _, err := ParseKey(key); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictExpiredLocked()
	if el, ok := c.items[key]; ok {
		c.order.MoveToFront(el)
		e := el.Value.(*entry[V])
		e.value = value
		e.expiresAt = c.now().Add(c.cfg.TTL)
		return nil
	}
	if c.order.Len() >= c.cfg.MaxEntries {
		back := c.order.Back()
		if back != nil {
			ev := back.Value.(*entry[V])
			delete(c.items, ev.key)
			c.order.Remove(back)
		}
	}
	t, v, _, _, _ := ParseKey(key)
	e := &entry[V]{key: key, value: value, expiresAt: c.now().Add(c.cfg.TTL), tenant: t, version: v}
	el := c.order.PushFront(e)
	c.items[key] = el
	return nil
}

func (c *Cache[V]) Get(key string, authorize func(V) bool) (V, bool) {
	var zero V
	if _, _, _, _, err := ParseKey(key); err != nil {
		return zero, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return zero, false
	}
	e := el.Value.(*entry[V])
	if !c.now().Before(e.expiresAt) {
		delete(c.items, key)
		c.order.Remove(el)
		return zero, false
	}
	if authorize != nil && !authorize(e.value) {
		return zero, false
	}
	c.order.MoveToFront(el)
	return e.value, true
}

func (c *Cache[V]) GetOrLoad(key string, load func() (V, error), authorize func(V) bool) (V, error) {
	var zero V
	if v, ok := c.Get(key, authorize); ok {
		return v, nil
	}
	if load == nil {
		return zero, nil
	}
	v, err := load()
	if err != nil {
		return zero, err
	}
	if authorize != nil && !authorize(v) {
		return zero, nil
	}
	_ = c.Set(key, v)
	return v, nil
}

func (c *Cache[V]) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		delete(c.items, key)
		c.order.Remove(el)
	}
}

func (c *Cache[V]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*list.Element)
	c.order.Init()
}

func (c *Cache[V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictExpiredLocked()
	return c.order.Len()
}

func (c *Cache[V]) evictExpiredLocked() {
	now := c.now()
	for k, el := range c.items {
		e := el.Value.(*entry[V])
		if !now.Before(e.expiresAt) {
			delete(c.items, k)
			c.order.Remove(el)
		}
	}
}
