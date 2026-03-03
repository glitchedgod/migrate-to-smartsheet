package ratelimit_test

import (
	"testing"
	"time"

	"github.com/glitchedgod/migrate-to-smartsheet/internal/ratelimit"
	"github.com/stretchr/testify/assert"
)

func TestRateLimiterForPlatform(t *testing.T) {
	rl := ratelimit.ForPlatform("notion")
	assert.NotNil(t, rl)
	start := time.Now()
	rl.Wait()
	elapsed := time.Since(start)
	assert.Less(t, elapsed, 500*time.Millisecond, "first token should be immediate")
}

func TestRateLimiterUnknownPlatform(t *testing.T) {
	rl := ratelimit.ForPlatform("unknown")
	assert.NotNil(t, rl)
}

func TestRateLimiterAllPlatforms(t *testing.T) {
	platforms := []string{"asana", "airtable", "wrike", "trello", "jira", "monday", "smartsheet", "unknown"}
	for _, p := range platforms {
		rl := ratelimit.ForPlatform(p)
		assert.NotNil(t, rl, "platform %s should return non-nil limiter", p)
		// First Wait() should be immediate for all (burst >= 1)
		done := make(chan struct{})
		go func() {
			rl.Wait()
			close(done)
		}()
		select {
		case <-done:
			// ok
		case <-time.After(500 * time.Millisecond):
			t.Errorf("platform %s: first Wait() took too long", p)
		}
	}
}
