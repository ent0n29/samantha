package voice

import (
	"fmt"
	"testing"
	"time"

	"github.com/ent0n29/samantha/internal/observability"
	"github.com/ent0n29/samantha/internal/protocol"
)

func TestSendDeliversCriticalWhenOutboundQueueTemporarilyFull(t *testing.T) {
	o := &Orchestrator{
		metrics: observability.NewMetrics(fmt.Sprintf("samantha_test_send_%d", time.Now().UnixNano())),
	}

	outbound := make(chan any, 1)
	outbound <- protocol.AssistantTextDelta{Type: protocol.TypeAssistantTextDelta, TextDelta: "filler"}

	go func() {
		time.Sleep(40 * time.Millisecond)
		<-outbound
	}()

	o.send(outbound, protocol.AssistantTurnEnd{
		Type:      protocol.TypeAssistantTurnEnd,
		SessionID: "s1",
		TurnID:    "t1",
		Reason:    "completed",
	})

	select {
	case msg := <-outbound:
		end, ok := msg.(protocol.AssistantTurnEnd)
		if !ok {
			t.Fatalf("outbound msg type = %T, want protocol.AssistantTurnEnd", msg)
		}
		if end.Type != protocol.TypeAssistantTurnEnd {
			t.Fatalf("AssistantTurnEnd.Type = %q, want %q", end.Type, protocol.TypeAssistantTurnEnd)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatalf("timed out waiting for critical outbound message")
	}
}
