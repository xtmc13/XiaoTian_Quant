package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisCache implements the Cache interface using Redis.
type RedisCache struct {
	prefix string
	client *redis.Client
	ctx    context.Context
}

// NewRedisCache creates a new Redis-backed cache.
// It connects to the Redis URL from the REDIS_URL environment variable.
func NewRedisCache(prefix string) (*RedisCache, error) {
	if prefix == "" {
		prefix = "xt:"
	}

	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		return nil, fmt.Errorf("REDIS_URL environment variable is not set")
	}

	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("invalid REDIS_URL: %w", err)
	}

	client := redis.NewClient(opt)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}

	return &RedisCache{
		prefix: prefix,
		client: client,
		ctx:    context.Background(),
	}, nil
}

func (c *RedisCache) key(k string) string {
	if len(k) >= len(c.prefix) && k[:len(c.prefix)] == c.prefix {
		return k
	}
	return c.prefix + k
}

// Get retrieves a value from Redis.
func (c *RedisCache) Get(key string) (string, error) {
	val, err := c.client.Get(c.ctx, c.key(key)).Result()
	if err == redis.Nil {
		return "", fmt.Errorf("key not found")
	}
	if err != nil {
		return "", fmt.Errorf("redis get error: %w", err)
	}
	return val, nil
}

// Set stores a value in Redis with an optional TTL.
func (c *RedisCache) Set(key string, value string, ttl time.Duration) error {
	err := c.client.Set(c.ctx, c.key(key), value, ttl).Err()
	if err != nil {
		return fmt.Errorf("redis set error: %w", err)
	}
	return nil
}

// Delete removes a key from Redis.
func (c *RedisCache) Delete(key string) error {
	err := c.client.Del(c.ctx, c.key(key)).Err()
	if err != nil {
		return fmt.Errorf("redis delete error: %w", err)
	}
	return nil
}

// Exists checks if a key exists in Redis.
func (c *RedisCache) Exists(key string) bool {
	n, err := c.client.Exists(c.ctx, c.key(key)).Result()
	return err == nil && n > 0
}

// GetJSON retrieves and unmarshals a JSON value.
func (c *RedisCache) GetJSON(key string, dest any) error {
	val, err := c.Get(key)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(val), dest)
}

// SetJSON marshals and stores a value as JSON.
func (c *RedisCache) SetJSON(key string, value any, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.Set(key, string(data), ttl)
}

// Close closes the Redis connection.
func (c *RedisCache) Close() error {
	return c.client.Close()
}

// Health returns the Redis connection health status.
func (c *RedisCache) Health() map[string]any {
	info := map[string]any{"connected": false}
	if c.client == nil {
		return info
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.client.Ping(ctx).Err(); err != nil {
		info["error"] = err.Error()
		return info
	}
	info["connected"] = true
	poolStats := c.client.PoolStats()
	info["hits"] = poolStats.Hits
	info["misses"] = poolStats.Misses
	info["total_conns"] = poolStats.TotalConns
	info["idle_conns"] = poolStats.IdleConns
	return info
}

// InvalidatePattern removes keys matching a pattern using Redis SCAN + DEL.
// Note: This is a best-effort operation and may not catch all keys in very large datasets.
func (c *RedisCache) InvalidatePattern(pattern string) error {
	fullPattern := c.key(pattern) + "*"
	var cursor uint64
	for {
		keys, nextCursor, err := c.client.Scan(c.ctx, cursor, fullPattern, 100).Result()
		if err != nil {
			return fmt.Errorf("redis scan error: %w", err)
		}
		if len(keys) > 0 {
			if err := c.client.Del(c.ctx, keys...).Err(); err != nil {
				return fmt.Errorf("redis del error: %w", err)
			}
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return nil
}
