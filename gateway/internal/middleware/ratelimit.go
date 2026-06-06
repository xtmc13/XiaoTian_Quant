package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// bucketEntry wraps a TokenBucket with last-access tracking for TTL eviction.
type bucketEntry struct {
	bucket     *TokenBucket
	lastAccess time.Time
}

// TokenBucket implements a simple token bucket rate limiter.
type TokenBucket struct {
	capacity   int
	tokens     float64
	refillRate float64 // tokens per second
	lastRefill time.Time
	mu         sync.Mutex
}

func NewTokenBucket(capacity int, refillRate float64) *TokenBucket {
	return &TokenBucket{
		capacity:   capacity,
		tokens:     float64(capacity),
		refillRate: refillRate,
		lastRefill: time.Now(),
	}
}

func (tb *TokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()
	tb.tokens = min(float64(tb.capacity), tb.tokens+elapsed*tb.refillRate)
	tb.lastRefill = now

	if tb.tokens >= 1 {
		tb.tokens--
		return true
	}
	return false
}

// RateLimiter middleware limits requests per client IP.
// Inactive buckets are evicted after bucketTTL to prevent unbounded memory growth.
func RateLimiter(requestsPerSecond float64, burst int) gin.HandlerFunc {
	const bucketTTL = 10 * time.Minute
	const cleanupInterval = 5 * time.Minute

	buckets := sync.Map{} // map[string]*bucketEntry

	// Background goroutine: evict stale buckets periodically.
	go func() {
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			cutoff := time.Now().Add(-bucketTTL)
			buckets.Range(func(key, value any) bool {
				if be, ok := value.(*bucketEntry); ok && be.lastAccess.Before(cutoff) {
					buckets.Delete(key)
				}
				return true
			})
		}
	}()

	return func(c *gin.Context) {
		clientIP := c.ClientIP()
		if clientIP == "" {
			clientIP = "unknown"
		}

		now := time.Now()
		actual, _ := buckets.LoadOrStore(clientIP, &bucketEntry{
			bucket:     NewTokenBucket(burst, requestsPerSecond),
			lastAccess: now,
		})
		be := actual.(*bucketEntry)
		be.lastAccess = now

		if !be.bucket.Allow() {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":       "rate limit exceeded",
				"retry_after": "1s",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// StrictRateLimiter is a stricter variant for auth endpoints.
func StrictRateLimiter() gin.HandlerFunc {
	return RateLimiter(0.5, 3) // 3 burst, 0.5/sec = 1 request per 2 seconds
}
