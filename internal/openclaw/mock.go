package openclaw

import (
	"context"
	"fmt"
	"strings"
)

// MockAdapter provides deterministic local replies when OpenClaw is unavailable.
type MockAdapter struct{}

func NewMockAdapter() *MockAdapter { return &MockAdapter{} }

func (a *MockAdapter) StreamResponse(
	ctx context.Context,
	req MessageRequest,
	onDelta DeltaHandler,
) (MessageResponse, error) {
	select {
	case <-ctx.Done():
		return MessageResponse{}, ctx.Err()
	default:
	}

	text := buildMockReply(req)
	if onDelta != nil && text != "" {
		if err := onDelta(text); err != nil {
			return MessageResponse{}, err
		}
	}
	return MessageResponse{Text: text}, nil
}

func buildMockReply(req MessageRequest) string {
	base := strings.TrimSpace(req.InputText)
	if base == "" {
		base = "I am listening."
	}

	if len(req.MemoryContext) == 0 {
		return fmt.Sprintf("I heard you: %s", base)
	}

	last := strings.TrimSpace(req.MemoryContext[len(req.MemoryContext)-1])
	if last == "" {
		return fmt.Sprintf("I heard you: %s", base)
	}

	return fmt.Sprintf("I heard you: %s\nI also remember: %s", base, last)
}
