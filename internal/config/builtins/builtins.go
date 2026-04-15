package builtins

import (
	"embed"
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/kimitsu-ai/ktsu/internal/config"
)

//go:embed *.workflow.yaml
var workflowFiles embed.FS

// Load returns the WorkflowConfig for a shipped workflow by its ktsu/ name.
// name must be in the form "ktsu/NAME" (e.g. "ktsu/slack-input").
func Load(name string) (*config.WorkflowConfig, error) {
	if !strings.HasPrefix(name, "ktsu/") {
		return nil, fmt.Errorf("shipped workflow name must start with ktsu/, got %q", name)
	}
	short := strings.TrimPrefix(name, "ktsu/")
	filename := "ktsu-" + short + ".workflow.yaml"
	data, err := workflowFiles.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("shipped workflow %q not found", name)
	}
	var cfg config.WorkflowConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse shipped workflow %q: %w", name, err)
	}
	return &cfg, nil
}

// List returns all shipped workflow names (e.g. "ktsu/slack-input").
func List() []string {
	entries, _ := workflowFiles.ReadDir(".")
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		n := e.Name()
		if strings.HasSuffix(n, ".workflow.yaml") {
			short := strings.TrimSuffix(strings.TrimPrefix(n, "ktsu-"), ".workflow.yaml")
			names = append(names, "ktsu/"+short)
		}
	}
	return names
}

// ResolveWorkflowRef resolves a workflow step reference to a WorkflowConfig.
//
// Handles:
//   - "ktsu/name"           → shipped workflow (always sub-workflow visibility)
//   - "./path/to/file.yaml" → local path relative to projectDir
//   - "../path/..."         → local path relative to projectDir
//   - absolute paths        → used as-is
//
// Hub-installed references ("author/name") are not yet supported and return an error.
func ResolveWorkflowRef(ref, projectDir string) (*config.WorkflowConfig, error) {
	if strings.HasPrefix(ref, "ktsu/") {
		return Load(ref)
	}
	if strings.HasPrefix(ref, "./") || strings.HasPrefix(ref, "../") || filepath.IsAbs(ref) {
		path := ref
		if !filepath.IsAbs(ref) {
			path = filepath.Join(projectDir, ref)
		}
		return config.LoadWorkflow(path)
	}
	return nil, fmt.Errorf("hub-installed workflow references (%q) are not yet supported; use ./path/ for local or ktsu/ for shipped", ref)
}
