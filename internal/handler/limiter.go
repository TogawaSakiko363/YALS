package handler

import (
	"sync"
	"time"

	"YALS/internal/config"
)

// RateLimiter manages rate limiting for command execution
type RateLimiter struct {
	enabled     bool
	maxCommands int
	timeWindow  time.Duration
	sessions    map[string]*SessionRateLimit
	mu          sync.RWMutex
}

// SessionRateLimit tracks command execution for a session
type SessionRateLimit struct {
	timestamps []time.Time
}

// NewRateLimiter creates a limiter from runtime settings.
func NewRateLimiter(settings config.RuntimeSettings) *RateLimiter {
	config.NormalizeRuntimeSettings(&settings)
	return &RateLimiter{
		enabled:     settings.RateLimit.Enabled,
		maxCommands: settings.RateLimit.MaxCommands,
		timeWindow:  time.Duration(settings.RateLimit.TimeWindow) * time.Second,
		sessions:    make(map[string]*SessionRateLimit),
	}
}

// Update applies hot runtime settings to the limiter.
func (rl *RateLimiter) Update(settings config.RuntimeSettings) {
	config.NormalizeRuntimeSettings(&settings)
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.enabled = settings.RateLimit.Enabled
	rl.maxCommands = settings.RateLimit.MaxCommands
	rl.timeWindow = time.Duration(settings.RateLimit.TimeWindow) * time.Second
	if rl.sessions == nil {
		rl.sessions = make(map[string]*SessionRateLimit)
	}
}

// checkRateLimit checks if a session has exceeded the rate limit
func (rl *RateLimiter) checkRateLimit(sessionID string) bool {
	if !rl.enabled {
		return true
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	if _, exists := rl.sessions[sessionID]; !exists {
		rl.sessions[sessionID] = &SessionRateLimit{
			timestamps: []time.Time{},
		}
	}

	session := rl.sessions[sessionID]

	validTimestamps := []time.Time{}
	for _, ts := range session.timestamps {
		if now.Sub(ts) < rl.timeWindow {
			validTimestamps = append(validTimestamps, ts)
		}
	}
	session.timestamps = validTimestamps

	if len(session.timestamps) >= rl.maxCommands {
		return false
	}

	session.timestamps = append(session.timestamps, now)
	return true
}

// getRemainingTime returns the time until the rate limit resets
func (rl *RateLimiter) getRemainingTime(sessionID string) time.Duration {
	if !rl.enabled {
		return 0
	}

	rl.mu.RLock()
	defer rl.mu.RUnlock()

	session, exists := rl.sessions[sessionID]
	if !exists || len(session.timestamps) == 0 {
		return 0
	}

	oldestTimestamp := session.timestamps[0]
	elapsed := time.Since(oldestTimestamp)
	remaining := rl.timeWindow - elapsed

	if remaining < 0 {
		return 0
	}
	return remaining
}
