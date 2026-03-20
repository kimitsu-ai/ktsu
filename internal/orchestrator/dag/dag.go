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
	if err := detectCycle(nodes); err != nil {
		return nil, err
	}
	// stub — returns empty layers
	return nil, nil
}

func detectCycle(nodes []Node) error {
	// stub
	_ = nodes
	_ = fmt.Sprintf
	return nil
}
