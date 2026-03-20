package builtins

import (
	"context"
	"encoding/json"
	"errors"

	mcp "github.com/kimitsu-ai/ktsu/pkg/mcp"
)

// ErrNotImplemented is returned by builtin stubs
var ErrNotImplemented = errors.New("not implemented")

// BuiltinServer is the interface all built-in tool servers must implement
type BuiltinServer interface {
	Name() string
	Tools() []mcp.Tool
	Call(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error)
}
