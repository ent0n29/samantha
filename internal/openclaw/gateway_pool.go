package openclaw

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// pooledGatewayConn holds a connected gateway websocket for reuse across turns.
//
// We keep this intentionally simple: one request at a time per sessionKey. The
// orchestrator already serializes "real" turns, and speculative prefetch uses
// ephemeral connections (TurnID prefixed with "spec-").
type pooledGatewayConn struct {
	mu       sync.Mutex
	conn     *websocket.Conn
	ws       *gatewayWS
	lastUsed time.Time
}

func (p *pooledGatewayConn) stream(
	ctx context.Context,
	a *GatewayAdapter,
	prompt, agentID, sessionKey, runID string,
	onDelta DeltaHandler,
) (MessageResponse, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.conn == nil || p.ws == nil {
		conn, ws, err := a.dialAndConnect(ctx)
		if err != nil {
			return MessageResponse{}, false, err
		}
		p.conn = conn
		p.ws = ws
	}

	resp, keep, err := a.streamAgent(ctx, p.conn, p.ws, prompt, agentID, sessionKey, runID, onDelta)
	p.lastUsed = time.Now()

	// If the context was canceled, close eagerly so any in-flight run stops
	// and we don't buffer late deltas into the next turn.
	if err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		keep = false
	}

	if !keep || err != nil {
		if p.conn != nil {
			_ = p.conn.Close()
		}
		p.conn = nil
		p.ws = nil
	}

	return resp, keep, err
}

func (p *pooledGatewayConn) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn != nil {
		_ = p.conn.Close()
	}
	p.conn = nil
	p.ws = nil
}
