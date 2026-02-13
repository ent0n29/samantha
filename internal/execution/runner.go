package execution

import (
	"context"
	"strings"

	"github.com/ent0n29/samantha/internal/openclaw"
	"github.com/ent0n29/samantha/internal/tasks"
)

type Runner struct {
	adapter openclaw.Adapter
}

func NewRunner(adapter openclaw.Adapter) *Runner {
	return &Runner{adapter: adapter}
}

func (r *Runner) RunTask(ctx context.Context, task tasks.Task, onDelta func(string) error) (string, error) {
	var out strings.Builder
	res, err := r.adapter.StreamResponse(ctx, openclaw.MessageRequest{
		UserID:    task.UserID,
		SessionID: task.SessionID,
		TurnID:    task.ID,
		InputText: task.IntentText,
	}, func(delta string) error {
		d := strings.TrimSpace(delta)
		if d == "" {
			return nil
		}
		if out.Len() > 0 {
			out.WriteString("\n")
		}
		out.WriteString(d)
		if onDelta != nil {
			return onDelta(d)
		}
		return nil
	})
	if err != nil {
		return "", err
	}

	final := strings.TrimSpace(out.String())
	if final == "" {
		final = strings.TrimSpace(res.Text)
	}
	return final, nil
}
