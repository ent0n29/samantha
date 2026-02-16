package tasks

import "testing"

func TestBuildPlanGraphSplitsIntentIntoOrderedNodes(t *testing.T) {
	graph := BuildPlanGraph(
		"build release",
		"draft migration plan then implement endpoint; finally add tests",
		RiskLevelMedium,
		true,
	)
	if graph.Version != 1 {
		t.Fatalf("Version = %d, want 1", graph.Version)
	}
	if len(graph.Nodes) < 3 {
		t.Fatalf("Nodes len = %d, want >=3", len(graph.Nodes))
	}
	if len(graph.Edges) != len(graph.Nodes)-1 {
		t.Fatalf("Edges len = %d, want %d", len(graph.Edges), len(graph.Nodes)-1)
	}
	if !graph.Nodes[0].RequiresApproval {
		t.Fatalf("first node RequiresApproval = false, want true")
	}
	for i, node := range graph.Nodes {
		wantID := "n" + itoa(i+1)
		if node.ID != wantID {
			t.Fatalf("node[%d].ID = %q, want %q", i, node.ID, wantID)
		}
	}
}

func TestBuildPlanGraphFallsBackToSummary(t *testing.T) {
	graph := BuildPlanGraph("ship patch", "", RiskLevelLow, false)
	if len(graph.Nodes) != 1 {
		t.Fatalf("Nodes len = %d, want 1", len(graph.Nodes))
	}
	if graph.Nodes[0].Title != "ship patch" {
		t.Fatalf("node title = %q, want %q", graph.Nodes[0].Title, "ship patch")
	}
}
