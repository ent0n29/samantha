package reliability

import "time"

// IsRetryableHTTPStatus classifies retryable HTTP status codes.
func IsRetryableHTTPStatus(code int) bool {
	switch code {
	case 429, 500, 502, 503, 504:
		return true
	default:
		return false
	}
}

// IsRetryableRealtimeMessageType classifies retryable upstream realtime errors.
func IsRetryableRealtimeMessageType(messageType string) bool {
	switch messageType {
	case "rate_limited", "resource_exhausted", "queue_overflow", "error":
		return true
	default:
		return false
	}
}

// ExponentialBackoff computes a deterministic capped backoff duration.
func ExponentialBackoff(attempt int, base, cap time.Duration) time.Duration {
	if attempt <= 0 {
		return base
	}
	d := base
	for i := 0; i < attempt; i++ {
		d *= 2
		if d >= cap {
			return cap
		}
	}
	return d
}
