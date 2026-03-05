package ratelimit

import (
	"context"

	"golang.org/x/time/rate"
)

type Limiter struct {
	l *rate.Limiter
}

// Wait blocks until a token is available or ctx is cancelled.
// Callers should pass the request context so cancellation propagates.
func (l *Limiter) Wait(ctx ...context.Context) {
	c := context.Background()
	if len(ctx) > 0 && ctx[0] != nil {
		c = ctx[0]
	}
	l.l.Wait(c) //nolint:errcheck
}

func ForPlatform(platform string) *Limiter {
	var r rate.Limit
	var burst int
	switch platform {
	case "notion":
		r, burst = 3, 3
	case "airtable":
		r, burst = 5, 5
	case "wrike":
		r, burst = 2, 5
	case "trello":
		r, burst = 30, 30
	case "asana":
		r, burst = 25, 50
	case "jira":
		r, burst = 50, 50
	case "monday":
		r, burst = 10, 10
	case "smartsheet":
		r, burst = 5, 10
	default:
		r, burst = 5, 5
	}
	return &Limiter{l: rate.NewLimiter(r, burst)}
}
