package openclaw

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// FallbackAdapter attempts a primary adapter first and falls back on error.
type FallbackAdapter struct {
	primary  Adapter
	fallback Adapter
}

var fallbackFirstDeltaTimeout = 900 * time.Millisecond
var fallbackFirstDeltaRetries = 0

func NewFallbackAdapter(primary Adapter, fallback Adapter) *FallbackAdapter {
	return &FallbackAdapter{
		primary:  primary,
		fallback: fallback,
	}
}

// Primary returns the preferred adapter used before fallback.
func (a *FallbackAdapter) Primary() Adapter {
	if a == nil {
		return nil
	}
	return a.primary
}

// Secondary returns the fallback adapter.
func (a *FallbackAdapter) Secondary() Adapter {
	if a == nil {
		return nil
	}
	return a.fallback
}

func (a *FallbackAdapter) StreamResponse(
	ctx context.Context,
	req MessageRequest,
	onDelta DeltaHandler,
) (MessageResponse, error) {
	if a == nil || a.primary == nil {
		if a != nil && a.fallback != nil {
			return a.fallback.StreamResponse(ctx, req, onDelta)
		}
		return MessageResponse{}, fmt.Errorf("fallback adapter misconfigured")
	}
	// Fast path: keep the original behavior unless a first-delta timeout is explicitly enabled.
	if fallbackFirstDeltaTimeout <= 0 {
		resp, err := a.primary.StreamResponse(ctx, req, onDelta)
		if err == nil {
			return resp, nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return MessageResponse{}, err
		}
		if a.fallback == nil {
			return MessageResponse{}, err
		}
		fallbackResp, fallbackErr := a.fallback.StreamResponse(ctx, req, onDelta)
		if fallbackErr != nil {
			return MessageResponse{}, fmt.Errorf("primary adapter error: %w; fallback adapter error: %v", err, fallbackErr)
		}
		return fallbackResp, nil
	}

	type result struct {
		resp MessageResponse
		err  error
	}

	runPrimaryAttempt := func() (result, bool) {
		primaryCtx, cancelPrimary := context.WithCancel(ctx)
		defer cancelPrimary()

		firstDeltaCh := make(chan struct{})
		var firstDeltaOnce sync.Once
		var acceptPrimaryDeltas atomic.Bool
		acceptPrimaryDeltas.Store(true)
		primaryResultCh := make(chan result, 1)

		go func() {
			resp, err := a.primary.StreamResponse(primaryCtx, req, func(delta string) error {
				if strings.TrimSpace(delta) != "" {
					firstDeltaOnce.Do(func() {
						close(firstDeltaCh)
					})
				}
				if !acceptPrimaryDeltas.Load() {
					return context.Canceled
				}
				if onDelta == nil {
					return nil
				}
				return onDelta(delta)
			})
			primaryResultCh <- result{resp: resp, err: err}
		}()

		if a.fallback == nil || fallbackFirstDeltaTimeout <= 0 {
			return <-primaryResultCh, false
		}

		timer := time.NewTimer(fallbackFirstDeltaTimeout)
		defer timer.Stop()
		select {
		case primary := <-primaryResultCh:
			return primary, false
		case <-firstDeltaCh:
			return <-primaryResultCh, false
		case <-timer.C:
			acceptPrimaryDeltas.Store(false)
			cancelPrimary()
			select {
			case primary := <-primaryResultCh:
				return primary, true
			case <-time.After(200 * time.Millisecond):
				return result{}, true
			}
		}
	}

	var (
		primary             result
		timedOutBeforeDelta bool
	)
	for attempt := 0; attempt <= fallbackFirstDeltaRetries; attempt++ {
		primary, timedOutBeforeDelta = runPrimaryAttempt()
		if !timedOutBeforeDelta {
			break
		}
	}

	if primary.err == nil && !timedOutBeforeDelta {
		return primary.resp, nil
	}
	if !timedOutBeforeDelta && (errors.Is(primary.err, context.Canceled) || errors.Is(primary.err, context.DeadlineExceeded)) {
		return MessageResponse{}, primary.err
	}

	if a.fallback == nil {
		if timedOutBeforeDelta {
			return MessageResponse{}, context.DeadlineExceeded
		}
		return MessageResponse{}, primary.err
	}

	fallbackResp, fallbackErr := a.fallback.StreamResponse(ctx, req, onDelta)
	if fallbackErr != nil {
		if timedOutBeforeDelta {
			if primary.err != nil {
				return MessageResponse{}, fmt.Errorf("primary adapter timeout before first delta (%s): %w; fallback adapter error: %v", fallbackFirstDeltaTimeout, primary.err, fallbackErr)
			}
			return MessageResponse{}, fmt.Errorf("primary adapter timeout before first delta (%s); fallback adapter error: %v", fallbackFirstDeltaTimeout, fallbackErr)
		}
		return MessageResponse{}, fmt.Errorf("primary adapter error: %w; fallback adapter error: %v", primary.err, fallbackErr)
	}
	return fallbackResp, nil
}
