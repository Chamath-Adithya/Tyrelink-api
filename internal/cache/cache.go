package cache

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type Cache interface {
	Get(ctx context.Context, key string, dest any) bool
	Set(ctx context.Context, key string, val any, ttl time.Duration) error
	Delete(ctx context.Context, keys ...string) error
	FlushPattern(ctx context.Context, pattern string) error
	IsRedis() bool
}

type memoryEntry struct {
	data      []byte
	expiresAt time.Time
}

type hybridCache struct {
	rdb        *redis.Client
	isRedis    bool
	mu         sync.RWMutex
	memoryData map[string]memoryEntry
}

func New(redisURL string) Cache {
	c := &hybridCache{
		memoryData: make(map[string]memoryEntry),
	}

	if redisURL != "" {
		opt, err := redis.ParseURL(redisURL)
		if err == nil {
			rdb := redis.NewClient(opt)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := rdb.Ping(ctx).Err(); err == nil {
				c.rdb = rdb
				c.isRedis = true
				log.Println("Connected to Redis cache at", opt.Addr)
				return c
			}
		}
	}

	log.Println("Running in-memory cache mode (Redis offline or not configured)")
	return c
}

func (c *hybridCache) IsRedis() bool {
	return c.isRedis
}

func (c *hybridCache) Get(ctx context.Context, key string, dest any) bool {
	if c.isRedis && c.rdb != nil {
		raw, err := c.rdb.Get(ctx, key).Bytes()
		if err == nil {
			if err := json.Unmarshal(raw, dest); err == nil {
				return true
			}
		}
	}

	c.mu.RLock()
	entry, ok := c.memoryData[key]
	c.mu.RUnlock()

	if !ok {
		return false
	}
	if time.Now().After(entry.expiresAt) {
		c.mu.Lock()
		delete(c.memoryData, key)
		c.mu.Unlock()
		return false
	}

	return json.Unmarshal(entry.data, dest) == nil
}

func (c *hybridCache) Set(ctx context.Context, key string, val any, ttl time.Duration) error {
	raw, err := json.Marshal(val)
	if err != nil {
		return err
	}

	if c.isRedis && c.rdb != nil {
		if err := c.rdb.Set(ctx, key, raw, ttl).Err(); err == nil {
			return nil
		}
	}

	c.mu.Lock()
	c.memoryData[key] = memoryEntry{
		data:      raw,
		expiresAt: time.Now().Add(ttl),
	}
	c.mu.Unlock()
	return nil
}

func (c *hybridCache) Delete(ctx context.Context, keys ...string) error {
	if c.isRedis && c.rdb != nil {
		_ = c.rdb.Del(ctx, keys...).Err()
	}

	c.mu.Lock()
	for _, k := range keys {
		delete(c.memoryData, k)
	}
	c.mu.Unlock()
	return nil
}

func (c *hybridCache) FlushPattern(ctx context.Context, pattern string) error {
	if c.isRedis && c.rdb != nil {
		iter := c.rdb.Scan(ctx, 0, pattern, 0).Iterator()
		var keys []string
		for iter.Next(ctx) {
			keys = append(keys, iter.Val())
		}
		if len(keys) > 0 {
			_ = c.rdb.Del(ctx, keys...).Err()
		}
	}

	c.mu.Lock()
	c.memoryData = make(map[string]memoryEntry)
	c.mu.Unlock()
	return nil
}

