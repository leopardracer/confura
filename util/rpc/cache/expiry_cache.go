package cache

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/Conflux-Chain/confura/util"
)

// for atomic load/store in cache.
type cacheValue struct {
	value    interface{}
	expireAt time.Time
}

// expiryCache is used to cache value with specified expiration time.
type expiryCache struct {
	value   atomic.Value
	timeout time.Duration
	mu      sync.Mutex
}

func newExpiryCache(timeout time.Duration) *expiryCache {
	return &expiryCache{
		timeout: timeout,
	}
}

func (cache *expiryCache) get() (interface{}, bool) {
	return cache.getAt(time.Now())
}

func (cache *expiryCache) getAt(time time.Time) (interface{}, bool) {
	value := cache.value.Load()
	if value == nil {
		return nil, false
	}

	val := value.(cacheValue)
	if val.expireAt.Before(time) {
		return nil, false
	}

	return val.value, true
}

func (cache *expiryCache) getOrUpdate(updateFunc func() (interface{}, error)) (interface{}, bool, error) {
	return cache.getOrUpdateAt(time.Now(), updateFunc)
}

func (cache *expiryCache) getOrUpdateAt(time time.Time, updateFunc func() (interface{}, error)) (interface{}, bool, error) {
	// cache value not expired
	if val, ok := cache.getAt(time); ok {
		return val, true, nil
	}

	// otherwise, query from fullnode and cache
	cache.mu.Lock()
	defer cache.mu.Unlock()

	// double check for concurrency
	if val, ok := cache.getAt(time); ok {
		return val, true, nil
	}

	val, err := updateFunc()
	if err != nil {
		return nil, false, err
	}

	// update cache
	cache.value.Store(cacheValue{
		value:    val,
		expireAt: time.Add(cache.timeout),
	})

	return val, false, nil
}

// nodeExpiryCaches is used for multiple nodes to cache data respectively.
type nodeExpiryCaches struct {
	node2Caches util.ConcurrentMap // node name => expiryCache
	timeout     time.Duration
}

func newNodeExpiryCaches(timeout time.Duration) *nodeExpiryCaches {
	return &nodeExpiryCaches{
		timeout: timeout,
	}
}

func (caches *nodeExpiryCaches) getOrUpdate(node string, updateFunc func() (interface{}, error)) (interface{}, bool, error) {
	val, _ := caches.node2Caches.LoadOrStoreFn(node, func(interface{}) interface{} {
		return newExpiryCache(caches.timeout)
	})

	return val.(*expiryCache).getOrUpdate(updateFunc)
}

// keyExpiryLruCaches caches value with specified expiration time and size using LRU eviction policy.
type keyExpiryLruCaches struct {
	key2Caches *util.ExpirableLruCache // cache key => expiryCache
	ttl        time.Duration
}

func newKeyExpiryLruCaches(ttl time.Duration, size int) *keyExpiryLruCaches {
	return &keyExpiryLruCaches{
		ttl:        ttl,
		key2Caches: util.NewExpirableLruCache(size, ttl),
	}
}

func (caches *keyExpiryLruCaches) getOrUpdate(cacheKey string, updateFunc func() (interface{}, error)) (interface{}, bool, error) {
	val, _ := caches.key2Caches.GetOrUpdate(cacheKey, func() (interface{}, error) {
		return newExpiryCache(caches.ttl), nil
	})

	return val.(*expiryCache).getOrUpdate(updateFunc)
}

func (caches *keyExpiryLruCaches) get(cacheKey string) (interface{}, bool) {
	if val, ok := caches.key2Caches.Get(cacheKey); ok {
		return val.(*expiryCache).get()
	}
	return nil, false
}

func (caches *keyExpiryLruCaches) del(cacheKey string) bool {
	return caches.key2Caches.Del(cacheKey)
}
