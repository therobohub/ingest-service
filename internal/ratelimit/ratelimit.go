package ratelimit

import (
	"sync"
	"time"
)

// Limiter implements a token bucket rate limiter per repo
type Limiter struct {
	mu              sync.RWMutex
	buckets         map[string]*bucket
	rate            int           // tokens per second
	burst           int           // max bucket capacity
	cleanupInterval time.Duration // cleanup interval for old buckets
}

type bucket struct {
	tokens    float64
	lastCheck time.Time
}

// NewLimiter creates a new rate limiter
func NewLimiter(rps, burst int) *Limiter {
	l := &Limiter{
		buckets:         make(map[string]*bucket),
		rate:            rps,
		burst:           burst,
		cleanupInterval: 5 * time.Minute,
	}

	// Start cleanup goroutine
	go l.cleanupLoop()

	return l
}

// Allow checks if a request for the given key (repo) should be allowed
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()

	b, exists := l.buckets[key]
	if !exists {
		// Create new bucket with full capacity
		b = &bucket{
			tokens:    float64(l.burst) - 1, // Subtract 1 for current request
			lastCheck: now,
		}
		l.buckets[key] = b
		return true
	}

	// Calculate tokens to add based on elapsed time
	elapsed := now.Sub(b.lastCheck).Seconds()
	b.tokens += elapsed * float64(l.rate)

	// Cap at burst limit
	if b.tokens > float64(l.burst) {
		b.tokens = float64(l.burst)
	}

	b.lastCheck = now

	// Check if we can allow this request
	if b.tokens >= 1 {
		b.tokens--
		return true
	}

	return false
}

// cleanupLoop periodically removes old buckets
func (l *Limiter) cleanupLoop() {
	ticker := time.NewTicker(l.cleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		l.cleanupOldBuckets()
	}
}

func (l *Limiter) cleanupOldBuckets() {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	threshold := 10 * time.Minute

	for key, b := range l.buckets {
		if now.Sub(b.lastCheck) > threshold {
			delete(l.buckets, key)
		}
	}
}
