package openclaw

import (
	"context"
	"errors"
	"fmt"
)

// FallbackAdapter attempts a primary adapter first and falls back on error.
type FallbackAdapter struct {
	primary  Adapter
	fallback Adapter
}

func NewFallbackAdapter(primary Adapter, fallback Adapter) *FallbackAdapter {
	return &FallbackAdapter{
		primary:  primary,
		fallback: fallback,
	}
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
