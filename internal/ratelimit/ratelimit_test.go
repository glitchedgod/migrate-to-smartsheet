package ratelimit_test

import (
	"testing"
	"time"

	"github.com/bchauhan/migrate-to-smartsheet/internal/ratelimit"
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
