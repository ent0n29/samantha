package tasks

import (
	"regexp"
	"strings"
)

var (
	planGraphSplitRe = regexp.MustCompile(`(?i)\b(?:and then|then|after that|next|finally)\b|[.;\n]+`)
	planGraphSpaceRe = regexp.MustCompile(`\s+`)
)

func BuildPlanGraph(summary, intent string, risk RiskLevel, requiresApproval bool) TaskPlanGraph {
	chunks := splitIntentPlanChunks(intent)
	if len(chunks) == 0 {
		base := strings.TrimSpace(summary)
		if base == "" {
			base = strings.TrimSpace(intent)
		}
		if base == "" {
			base = "Execute task"
		}
		chunks = []string{base}
	}

	nodes := make([]TaskPlanNode, 0, len(chunks))
	edges := make([]TaskPlanEdge, 0, maxInt(0, len(chunks)-1))
	for i, chunk := range chunks {
		nodeID := "n" + itoa(i+1)
		node := TaskPlanNode{
			ID:               nodeID,
			Seq:              i + 1,
			Title:            chunk,
			Kind:             "action",
			Status:           StepStatusPlanned,
			RiskLevel:        risk,
			RequiresApproval: requiresApproval && i == 0,
		}
		nodes = append(nodes, node)
		if i > 0 {
			edges = append(edges, TaskPlanEdge{
				From: "n" + itoa(i),
				To:   nodeID,
				Kind: "next",
			})
		}
	}

	return TaskPlanGraph{
		Version: 1,
		Nodes:   nodes,
		Edges:   edges,
	}
}

func splitIntentPlanChunks(intent string) []string {
	intent = strings.TrimSpace(intent)
	if intent == "" {
		return nil
	}
	parts := planGraphSplitRe.Split(intent, -1)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		p = planGraphSpaceRe.ReplaceAllString(p, " ")
		p = strings.Trim(p, " ,:-")
		if p == "" {
			continue
		}
		out = append(out, capitalizeFirst(p))
		if len(out) >= 6 {
			break
		}
	}
	return out
}

func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	if r[0] >= 'a' && r[0] <= 'z' {
		r[0] = r[0] - ('a' - 'A')
	}
	return string(r)
}

func itoa(n int) string {
	if n <= 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + (n % 10))
		n /= 10
	}
	return string(buf[i:])
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
