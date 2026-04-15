package builtins

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed *.workflow.yaml
var workflowFiles embed.FS

// ReadFile returns the raw YAML data for a shipped workflow by its ktsu/ name.
// name must be in the form "ktsu/NAME" (e.g. "ktsu/slack-input").
func ReadFile(name string) ([]byte, error) {
	if !strings.HasPrefix(name, "ktsu/") {
		return nil, fmt.Errorf("shipped workflow name must start with ktsu/, got %q", name)
	}
	short := strings.TrimPrefix(name, "ktsu/")
	filename := "ktsu-" + short + ".workflow.yaml"
	data, err := workflowFiles.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("shipped workflow %q not found", name)
	}
	return data, nil
}

// List returns all shipped workflow names (e.g. "ktsu/slack-input").
func List() []string {
	entries, _ := workflowFiles.ReadDir(".")
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		n := e.Name()
		if strings.HasSuffix(n, ".workflow.yaml") {
			// ktsu-slack-input.workflow.yaml → ktsu/slack-input
			short := strings.TrimSuffix(strings.TrimPrefix(n, "ktsu-"), ".workflow.yaml")
			names = append(names, "ktsu/"+short)
		}
	}
	return names
}
