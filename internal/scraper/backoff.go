package scraper

import (
	"context"
	"time"
)

// retryableFunc performs one attempt. retryable reports whether a failure
// is worth retrying (e.g. HTTP 429/5xx); err is nil on success.
type retryableFunc func() (retryable bool, err error)

// withBackoff calls fn up to maxAttempts times, sleeping base, 2*base,
// 4*base, ... between retryable failures. It returns early on a
// non-retryable failure, once ctx is done, or after the final attempt.
func withBackoff(ctx context.Context, maxAttempts int, base time.Duration, fn retryableFunc) error {
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		retryable, err := fn()
		if err == nil {
			return nil
		}

		lastErr = err
		if !retryable || attempt == maxAttempts-1 {
			return lastErr
		}

		delay := base * time.Duration(uint(1)<<uint(attempt))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}

	return lastErr
}
