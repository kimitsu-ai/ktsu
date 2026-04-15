package builtins

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kimitsu-ai/ktsu/internal/config"
)

// ResolveWorkflowRef resolves a workflow step reference to a WorkflowConfig.
//
// Handles:
//   - "./path/to/file.yaml" → local path relative to projectDir
//   - "../path/..."         → local path relative to projectDir
//   - absolute paths        → used as-is
//
// Hub-installed references ("author/name") are not yet supported and return an error.
func ResolveWorkflowRef(ref, projectDir string) (*config.WorkflowConfig, error) {
	if strings.HasPrefix(ref, "./") || strings.HasPrefix(ref, "../") || filepath.IsAbs(ref) {
		path := ref
		if !filepath.IsAbs(ref) {
			path = filepath.Join(projectDir, ref)
		}
		return config.LoadWorkflow(path)
	}
	return nil, fmt.Errorf("hub-installed workflow references (%q) are not yet supported; use ./path/ for local paths", ref)
}
