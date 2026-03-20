package dag

import "fmt"

// Node represents a step in the DAG
type Node struct {
	ID      string
	Depends []string
}

// Resolve performs topological sort and returns execution layers (steps that can run in parallel).
// Returns an error if a cycle is detected.
func Resolve(nodes []Node) ([][]string, error) {
	if len(nodes) == 0 {
		return nil, nil
	}

	if err := detectCycle(nodes); err != nil {
		return nil, err
	}

	// Build in-degree map and adjacency list
	inDegree := make(map[string]int, len(nodes))
	for _, n := range nodes {
		if _, ok := inDegree[n.ID]; !ok {
			inDegree[n.ID] = 0
		}
		for _, dep := range n.Depends {
			inDegree[dep] = inDegree[dep] // ensure dep is tracked
			inDegree[n.ID]++
		}
	}

	var layers [][]string
	for {
		// Collect all nodes with in-degree 0
		var layer []string
		for _, n := range nodes {
			if inDegree[n.ID] == 0 {
				layer = append(layer, n.ID)
			}
		}
		if len(layer) == 0 {
			break
		}
		layers = append(layers, layer)

		// Remove these nodes: mark as done and decrement dependents
		done := make(map[string]bool, len(layer))
		for _, id := range layer {
			done[id] = true
			inDegree[id] = -1 // sentinel: already placed
		}
		for _, n := range nodes {
			if done[n.ID] {
				continue
			}
			for _, dep := range n.Depends {
				if done[dep] {
					inDegree[n.ID]--
				}
			}
		}
	}

	return layers, nil
}

func detectCycle(nodes []Node) error {
	// DFS-based cycle detection using node coloring
	// white=0 (unvisited), gray=1 (in stack), black=2 (done)
	adj := make(map[string][]string, len(nodes))
	for _, n := range nodes {
		adj[n.ID] = n.Depends
	}

	color := make(map[string]int, len(nodes))
	var visit func(id string) error
	visit = func(id string) error {
		if color[id] == 2 {
			return nil
		}
		if color[id] == 1 {
			return fmt.Errorf("cycle detected at node %q", id)
		}
		color[id] = 1
		for _, dep := range adj[id] {
			if err := visit(dep); err != nil {
				return err
			}
		}
		color[id] = 2
		return nil
	}

	for _, n := range nodes {
		if err := visit(n.ID); err != nil {
			return err
		}
	}
	return nil
}
