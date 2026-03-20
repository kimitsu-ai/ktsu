package mcp

import "encoding/json"

// Tool represents an MCP tool definition
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema ToolInputSchema `json:"inputSchema"`
}

// ToolInputSchema is the JSON Schema for tool inputs
type ToolInputSchema struct {
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties,omitempty"`
	Required   []string               `json:"required,omitempty"`
}

// ListToolsRequest is the request to list available tools
type ListToolsRequest struct {
	Cursor string `json:"cursor,omitempty"`
}

// ListToolsResult is the response containing available tools
type ListToolsResult struct {
	Tools      []Tool `json:"tools"`
	NextCursor string `json:"nextCursor,omitempty"`
}

// CallToolRequest is the request to invoke a specific tool
type CallToolRequest struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// CallToolResult is the response from a tool invocation
type CallToolResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ToolContent is a content item in a tool result
type ToolContent struct {
	Type string `json:"type"` // text, image, etc.
	Text string `json:"text,omitempty"`
	Data string `json:"data,omitempty"`
}

// Message represents a message in a conversation
type Message struct {
	Role    string        `json:"role"`
	Content []ToolContent `json:"content"`
}
