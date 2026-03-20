package blob

import (
	"context"
	"encoding/json"

	builtins "github.com/your-org/sdd-services/internal/builtins"
	mcp "github.com/your-org/sdd-services/pkg/mcp"
)

type BlobServer struct{}

func New() *BlobServer { return &BlobServer{} }

func (s *BlobServer) Name() string { return "rss/blob" }

func (s *BlobServer) Tools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:        "blob_get",
			Description: "Get a blob by key",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"key": map[string]interface{}{"type": "string"},
				},
				Required: []string{"key"},
			},
		},
		{
			Name:        "blob_put",
			Description: "Store a blob by key",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"key":  map[string]interface{}{"type": "string"},
					"data": map[string]interface{}{"type": "string"},
				},
				Required: []string{"key", "data"},
			},
		},
		{
			Name:        "blob_delete",
			Description: "Delete a blob by key",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"key": map[string]interface{}{"type": "string"},
				},
				Required: []string{"key"},
			},
		},
		{
			Name:        "blob_list",
			Description: "List blobs by prefix",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"prefix": map[string]interface{}{"type": "string"},
				},
			},
		},
	}
}

func (s *BlobServer) Call(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	return nil, builtins.ErrNotImplemented
}
