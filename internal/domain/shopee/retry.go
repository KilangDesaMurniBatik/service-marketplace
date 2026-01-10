package shopee

import (
	"context"
	"math"
	"math/rand"
	"time"
)

// RetryPolicy defines the retry behavior for API calls.
type RetryPolicy struct {
	maxAttempts     int
	initialDelay    time.Duration
	maxDelay        time.Duration
	multiplier      float64
	jitterFactor    float64
	retryableErrors []error
}

// DefaultRetryPolicy returns a production-ready retry policy.
func DefaultRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		maxAttempts:  3,
		initialDelay: 1 * time.Second,
		maxDelay:     30 * time.Second,
		multiplier:   2.0,
		jitterFactor: 0.1,
		retryableErrors: []error{
			ErrRateLimited,
			ErrServiceUnavailable,
		},
	}
}

// NoRetryPolicy returns a policy that never retries.
func NoRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		maxAttempts: 1,
	}
}

// WithMaxAttempts sets the maximum number of retry attempts.
func (p *RetryPolicy) WithMaxAttempts(n int) *RetryPolicy {
	p.maxAttempts = n
	return p
}

// WithInitialDelay sets the initial delay between retries.
func (p *RetryPolicy) WithInitialDelay(d time.Duration) *RetryPolicy {
	p.initialDelay = d
	return p
}

// WithMaxDelay sets the maximum delay between retries.
func (p *RetryPolicy) WithMaxDelay(d time.Duration) *RetryPolicy {
	p.maxDelay = d
	return p
}

// WithMultiplier sets the delay multiplier for exponential backoff.
func (p *RetryPolicy) WithMultiplier(m float64) *RetryPolicy {
	p.multiplier = m
	return p
}

// WithJitter sets the jitter factor (0.0 to 1.0).
func (p *RetryPolicy) WithJitter(j float64) *RetryPolicy {
	p.jitterFactor = j
	return p
}

// MaxAttempts returns the maximum number of attempts.
func (p *RetryPolicy) MaxAttempts() int {
	return p.maxAttempts
}

// ShouldRetry determines if an error should be retried.
func (p *RetryPolicy) ShouldRetry(err error, attempt int) bool {
	if attempt >= p.maxAttempts {
		return false
	}

	if err == nil {
		return false
	}

	// Check if it's an APIError with retryable flag
	if apiErr, ok := err.(*APIError); ok {
		return apiErr.IsRetryable()
	}

	return false
}

// DelayForAttempt calculates the delay before the next retry attempt.
func (p *RetryPolicy) DelayForAttempt(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}

	// Calculate exponential delay
	delay := float64(p.initialDelay) * math.Pow(p.multiplier, float64(attempt-1))

	// Apply jitter
	if p.jitterFactor > 0 {
		jitter := delay * p.jitterFactor * (rand.Float64()*2 - 1) // Random between -jitter and +jitter
		delay += jitter
	}

	// Cap at max delay
	if delay > float64(p.maxDelay) {
		delay = float64(p.maxDelay)
	}

	return time.Duration(delay)
}

// WaitForRetry waits for the calculated delay before retry.
// Returns false if the context is cancelled during wait.
func (p *RetryPolicy) WaitForRetry(ctx context.Context, attempt int) bool {
	delay := p.DelayForAttempt(attempt)
	if delay <= 0 {
		return true
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// RetryResult holds the result of a retry operation.
type RetryResult struct {
	Attempts int
	LastError error
	Duration time.Duration
}

// Executor executes an operation with the retry policy.
type Executor struct {
	policy *RetryPolicy
}

// NewExecutor creates a new retry executor with the given policy.
func NewExecutor(policy *RetryPolicy) *Executor {
	return &Executor{policy: policy}
}

// Execute runs the operation with retries according to the policy.
func (e *Executor) Execute(ctx context.Context, operation func() error) *RetryResult {
	start := time.Now()
	result := &RetryResult{}

	for attempt := 1; attempt <= e.policy.maxAttempts; attempt++ {
		result.Attempts = attempt

		err := operation()
		if err == nil {
			result.Duration = time.Since(start)
			return result
		}

		result.LastError = err

		if !e.policy.ShouldRetry(err, attempt) {
			break
		}

		if attempt < e.policy.maxAttempts {
			if !e.policy.WaitForRetry(ctx, attempt) {
				result.LastError = ctx.Err()
				break
			}
		}
	}

	result.Duration = time.Since(start)
	return result
}
