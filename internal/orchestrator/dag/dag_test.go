package dag

import (
	"sort"
	"testing"
)

// sortLayers sorts each layer alphabetically so tests are order-independent.
func sortLayers(layers [][]string) [][]string {
	for i := range layers {
		sort.Strings(layers[i])
	}
	return layers
}

// --- Resolve: layer assignment ---

func assertLayers(t *testing.T, got, want [][]string) {
	t.Helper()
	got = sortLayers(got)
	want = sortLayers(want)
	if len(got) != len(want) {
		t.Fatalf("got %d layers %v, want %d layers %v", len(got), got, len(want), want)
	}
	for i := range want {
		sort.Strings(got[i])
		sort.Strings(want[i])
		if !equalStringSlice(got[i], want[i]) {
			t.Errorf("layer %d: got %v, want %v", i, got[i], want[i])
		}
	}
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestResolve_emptyNodes(t *testing.T) {
	layers, err := Resolve(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(layers) != 0 {
		t.Errorf("expected no layers, got %v", layers)
	}
}

func TestResolve_singleNode(t *testing.T) {
	nodes := []Node{{ID: "A"}}
	layers, err := Resolve(nodes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertLayers(t, layers, [][]string{{"A"}})
}

func TestResolve_twoIndependentNodes_sameLayer(t *testing.T) {
	nodes := []Node{{ID: "A"}, {ID: "B"}}
	layers, err := Resolve(nodes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertLayers(t, layers, [][]string{{"A", "B"}})
}

func TestResolve_linearChain(t *testing.T) {
	// C depends on B, B depends on A → [[A], [B], [C]]
	nodes := []Node{
		{ID: "A"},
		{ID: "B", Depends: []string{"A"}},
		{ID: "C", Depends: []string{"B"}},
	}
	layers, err := Resolve(nodes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertLayers(t, layers, [][]string{{"A"}, {"B"}, {"C"}})
}

func TestResolve_diamond(t *testing.T) {
	// B and C both depend on A; D depends on B and C → [[A], [B,C], [D]]
	nodes := []Node{
		{ID: "A"},
		{ID: "B", Depends: []string{"A"}},
		{ID: "C", Depends: []string{"A"}},
		{ID: "D", Depends: []string{"B", "C"}},
	}
	layers, err := Resolve(nodes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertLayers(t, layers, [][]string{{"A"}, {"B", "C"}, {"D"}})
}

func TestResolve_multipleRoots(t *testing.T) {
	// A and B are independent roots; C depends on both → [[A,B], [C]]
	nodes := []Node{
		{ID: "A"},
		{ID: "B"},
		{ID: "C", Depends: []string{"A", "B"}},
	}
	layers, err := Resolve(nodes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertLayers(t, layers, [][]string{{"A", "B"}, {"C"}})
}

// --- Cycle detection ---

func TestResolve_selfCycle(t *testing.T) {
	nodes := []Node{{ID: "A", Depends: []string{"A"}}}
	_, err := Resolve(nodes)
	if err == nil {
		t.Error("expected error for self-cycle, got nil")
	}
}

func TestResolve_twoNodeCycle(t *testing.T) {
	nodes := []Node{
		{ID: "A", Depends: []string{"B"}},
		{ID: "B", Depends: []string{"A"}},
	}
	_, err := Resolve(nodes)
	if err == nil {
		t.Error("expected error for two-node cycle, got nil")
	}
}

func TestResolve_threeNodeCycle(t *testing.T) {
	nodes := []Node{
		{ID: "A", Depends: []string{"C"}},
		{ID: "B", Depends: []string{"A"}},
		{ID: "C", Depends: []string{"B"}},
	}
	_, err := Resolve(nodes)
	if err == nil {
		t.Error("expected error for three-node cycle, got nil")
	}
}
