package shopee

import (
	"context"
	"sync"
	"time"
)

// RateLimiter implements a token bucket rate limiter for Shopee API calls.
// Shopee has different rate limits for different API categories.
type RateLimiter struct {
	buckets map[string]*tokenBucket
	mu      sync.RWMutex
	config  RateLimitConfig
}

// RateLimitConfig holds rate limit configuration.
type RateLimitConfig struct {
	// Default rate limit (requests per second)
	DefaultRPS int
	// Burst size (maximum requests that can be made at once)
	DefaultBurst int
	// Custom limits per API path prefix
	PathLimits map[string]PathLimit
}

// PathLimit defines rate limit for a specific API path.
type PathLimit struct {
	RPS   int
	Burst int
}

// DefaultRateLimitConfig returns production-safe rate limit configuration.
// Based on Shopee Open Platform documentation.
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		DefaultRPS:   10, // Conservative default
		DefaultBurst: 20,
		PathLimits: map[string]PathLimit{
			// Product APIs - typically 10 requests per second
			"/api/v2/product/": {RPS: 10, Burst: 20},
			// Order APIs - typically 10 requests per second
			"/api/v2/order/": {RPS: 10, Burst: 20},
			// Shop APIs - typically 10 requests per second
			"/api/v2/shop/": {RPS: 10, Burst: 15},
			// Auth APIs - lower rate to prevent abuse
			"/api/v2/auth/": {RPS: 5, Burst: 10},
			// Media APIs - higher limit for image uploads
			"/api/v2/media_space/": {RPS: 5, Burst: 10},
			// Logistics APIs
			"/api/v2/logistics/": {RPS: 10, Burst: 15},
		},
	}
}

// tokenBucket implements the token bucket algorithm.
type tokenBucket struct {
	tokens     float64
	maxTokens  float64
	refillRate float64 // tokens per second
	lastRefill time.Time
	mu         sync.Mutex
}

// newTokenBucket creates a new token bucket.
func newTokenBucket(rps, burst int) *tokenBucket {
	return &tokenBucket{
		tokens:     float64(burst),
		maxTokens:  float64(burst),
		refillRate: float64(rps),
		lastRefill: time.Now(),
	}
}

// take attempts to take a token from the bucket.
// Returns the time to wait if no tokens are available.
func (tb *tokenBucket) take() time.Duration {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	// Refill tokens based on time elapsed
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()
	tb.tokens += elapsed * tb.refillRate
	if tb.tokens > tb.maxTokens {
		tb.tokens = tb.maxTokens
	}
	tb.lastRefill = now

	// Check if we have a token
	if tb.tokens >= 1 {
		tb.tokens--
		return 0
	}

	// Calculate wait time for next token
	deficit := 1 - tb.tokens
	waitSeconds := deficit / tb.refillRate
	return time.Duration(waitSeconds * float64(time.Second))
}

// NewRateLimiter creates a new rate limiter with the given configuration.
func NewRateLimiter(config RateLimitConfig) *RateLimiter {
	return &RateLimiter{
		buckets: make(map[string]*tokenBucket),
		config:  config,
	}
}

// Wait blocks until a request can be made for the given path.
// Returns an error if the context is cancelled while waiting.
func (rl *RateLimiter) Wait(ctx context.Context, path string) error {
	bucket := rl.getBucket(path)
	waitTime := bucket.take()

	if waitTime == 0 {
		return nil
	}

	timer := time.NewTimer(waitTime)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// TryAcquire attempts to acquire a rate limit token without waiting.
// Returns true if successful, false if rate limited.
func (rl *RateLimiter) TryAcquire(path string) bool {
	bucket := rl.getBucket(path)
	return bucket.take() == 0
}

// getBucket returns the token bucket for a given path.
func (rl *RateLimiter) getBucket(path string) *tokenBucket {
	// Find matching path prefix
	bucketKey := rl.findBucketKey(path)

	rl.mu.RLock()
	bucket, exists := rl.buckets[bucketKey]
	rl.mu.RUnlock()

	if exists {
		return bucket
	}

	// Create new bucket
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Double-check after acquiring write lock
	if bucket, exists = rl.buckets[bucketKey]; exists {
		return bucket
	}

	// Get rate limit for this path
	rps, burst := rl.getLimitForPath(path)
	bucket = newTokenBucket(rps, burst)
	rl.buckets[bucketKey] = bucket

	return bucket
}

// findBucketKey finds the bucket key (path prefix) for a given path.
func (rl *RateLimiter) findBucketKey(path string) string {
	for prefix := range rl.config.PathLimits {
		if len(path) >= len(prefix) && path[:len(prefix)] == prefix {
			return prefix
		}
	}
	return "default"
}

// getLimitForPath returns the rate limit configuration for a path.
func (rl *RateLimiter) getLimitForPath(path string) (rps, burst int) {
	for prefix, limit := range rl.config.PathLimits {
		if len(path) >= len(prefix) && path[:len(prefix)] == prefix {
			return limit.RPS, limit.Burst
		}
	}
	return rl.config.DefaultRPS, rl.config.DefaultBurst
}

// GetStatus returns the current status of all rate limit buckets.
func (rl *RateLimiter) GetStatus() map[string]BucketStatus {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	status := make(map[string]BucketStatus)
	for key, bucket := range rl.buckets {
		bucket.mu.Lock()
		status[key] = BucketStatus{
			AvailableTokens: bucket.tokens,
			MaxTokens:       bucket.maxTokens,
			RefillRate:      bucket.refillRate,
		}
		bucket.mu.Unlock()
	}
	return status
}

// BucketStatus represents the current state of a rate limit bucket.
type BucketStatus struct {
	AvailableTokens float64
	MaxTokens       float64
	RefillRate      float64
}
