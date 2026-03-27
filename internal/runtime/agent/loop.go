package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/kimitsu-ai/ktsu/internal/runtime/agent/mcp"
)

const forcedConclusionMessage = "You have reached the maximum number of tool calls. Provide your final answer now without requesting any additional tools."
const jsonCorrectionMessage = "Your response was not valid JSON. Please respond with only a valid JSON object and nothing else."

// gatewayRequest is the JSON body sent to POST {gateway}/invoke.
type gatewayRequest struct {
	RunID     string    `json:"run_id"`
	StepID    string    `json:"step_id"`
	Group     string    `json:"group"`
	Messages  []Message `json:"messages"`
	MaxTokens int       `json:"max_tokens"`
	Tools     []toolDef `json:"tools,omitempty"`
}

// Message is a single conversation turn sent to the gateway.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// toolDef mirrors providers.ToolDefinition for the gateway wire format.
type toolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// toolCall mirrors providers.ToolCall returned by the gateway.
type toolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// gatewayResponse is the JSON body returned by the gateway on success.
type gatewayResponse struct {
	Content       string     `json:"content"`
	ModelResolved string     `json:"model_resolved"`
	TokensIn      int        `json:"tokens_in"`
	TokensOut     int        `json:"tokens_out"`
	CostUSD       float64    `json:"cost_usd"`
	ToolCalls     []toolCall `json:"tool_calls,omitempty"`
}

// gatewayErrorResponse is the JSON body returned by the gateway on error.
type gatewayErrorResponse struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

// Loop executes the agent reasoning loop against a gateway and MCP tool servers.
type Loop struct {
	gatewayURL string
	mcpClient  *mcp.Client
	httpClient *http.Client
}

// NewLoop creates a Loop that calls gatewayURL for LLM invocations.
func NewLoop(gatewayURL string, mcpClient *mcp.Client) *Loop {
	return &Loop{
		gatewayURL: gatewayURL,
		mcpClient:  mcpClient,
		httpClient: http.DefaultClient,
	}
}

// Run executes the reasoning loop and returns a CallbackPayload with the result.
// It never returns a non-nil error; failures are encoded in the payload status.
func (l *Loop) Run(ctx context.Context, req InvokeRequest) CallbackPayload {
	start := time.Now()
	payload := CallbackPayload{
		RunID:  req.RunID,
		StepID: req.StepID,
	}

	output, rawOutput, metrics, err := l.run(ctx, req)
	metrics.DurationMS = time.Since(start).Milliseconds()
	payload.Metrics = metrics

	if err != nil {
		payload.Status = "failed"
		payload.Error = err.Error()
		payload.RawOutput = rawOutput
	} else {
		payload.Status = "ok"
		payload.Output = output
	}
	return payload
}

func (l *Loop) run(ctx context.Context, req InvokeRequest) (map[string]any, string, Metrics, error) {
	var metrics Metrics

	// --- Tool discovery ---
	// toolByName maps tool name → ToolServerSpec (for routing tool calls with auth).
	// tools is the flat list sent to the gateway.
	type serverRef struct {
		url       string
		authToken string
	}
	toolByName := make(map[string]serverRef)
	var tools []toolDef
	for _, srv := range req.ToolServers {
		discovered, err := l.mcpClient.DiscoverTools(ctx, srv.URL, srv.AuthToken, srv.Allowlist)
		if err != nil {
			return nil, "", metrics, fmt.Errorf("discover tools from %s: %w", srv.Name, err)
		}
		for _, t := range discovered {
			toolByName[t.Name] = serverRef{url: srv.URL, authToken: srv.AuthToken}
			tools = append(tools, toolDef{
				Name:        t.Name,
				Description: t.Description,
				InputSchema: t.InputSchema,
			})
		}
	}

	// --- Initial messages ---
	inputJSON, err := json.Marshal(req.Input)
	if err != nil {
		return nil, "", metrics, fmt.Errorf("marshal input: %w", err)
	}
	systemPrompt := req.System
	if len(req.OutputSchema) > 0 {
		schemaJSON, err := json.MarshalIndent(req.OutputSchema, "", "  ")
		if err == nil {
			systemPrompt += "\n\nYour output MUST conform to this JSON schema:\n" + string(schemaJSON)
		}
	}
	messages := []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: string(inputJSON)},
	}

	maxTurns := req.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 10 // sensible default
	}

	var lastInvalidContent string

	// --- Turn loop ---
	for turn := 1; turn <= maxTurns; turn++ {
		// On the last turn, drop tools so the LLM must produce a text response.
		turnTools := tools
		if turn == maxTurns {
			messages = append(messages, Message{Role: "user", Content: forcedConclusionMessage})
			turnTools = nil
		}

		gwResp, err := l.callGateway(ctx, req, messages, turnTools)
		if err != nil {
			return nil, "", metrics, err
		}

		metrics.LLMCalls++
		metrics.TokensIn += gwResp.TokensIn
		metrics.TokensOut += gwResp.TokensOut
		metrics.CostUSD += gwResp.CostUSD
		if metrics.ModelResolved == "" {
			metrics.ModelResolved = gwResp.ModelResolved
		}

		if len(gwResp.ToolCalls) > 0 {
			for _, tc := range gwResp.ToolCalls {
				sref, ok := toolByName[tc.Name]
				if !ok {
					return nil, "", metrics, fmt.Errorf("tool_not_permitted: %s", tc.Name)
				}
				argsJSON, _ := json.Marshal(tc.Arguments)
				log.Printf("[agent] run=%s step=%s tool=%s args=%s", req.RunID, req.StepID, tc.Name, argsJSON)
				result, err := l.mcpClient.CallTool(ctx, sref.url, sref.authToken, tc.Name, tc.Arguments)
				if err != nil {
					return nil, "", metrics, fmt.Errorf("tool call %s: %w", tc.Name, err)
				}
				metrics.ToolCalls++

				// Append assistant tool_use block.
				toolUseContent, _ := json.Marshal([]map[string]any{{
					"type":  "tool_use",
					"id":    tc.ID,
					"name":  tc.Name,
					"input": tc.Arguments,
				}})
				messages = append(messages, Message{Role: "assistant", Content: string(toolUseContent)})

				// Append user tool_result block.
				resultText := ""
				if len(result.Content) > 0 {
					resultText = result.Content[0].Text
				}
				toolResultContent, _ := json.Marshal([]map[string]any{{
					"type":        "tool_result",
					"tool_use_id": tc.ID,
					"content":     resultText,
				}})
				messages = append(messages, Message{Role: "user", Content: string(toolResultContent)})
			}
			continue
		}

		// No tool calls — parse content as final JSON output.
		output, err := parseOutput(gwResp.Content)
		if err != nil {
			// LLM returned non-JSON; ask it to correct its output and retry.
			lastInvalidContent = gwResp.Content
			messages = append(messages, Message{Role: "assistant", Content: gwResp.Content})
			messages = append(messages, Message{Role: "user", Content: jsonCorrectionMessage})
			continue
		}
		return output, "", metrics, nil
	}

	// Free JSON correction: if the loop exhausted all turns but the last
	// response was invalid JSON, give the LLM one more toolless chance to
	// fix its formatting. This avoids a common footgun where tool-using
	// agents spend all turns on tools and then fail on a trivial parse error.
	if lastInvalidContent != "" {
		gwResp, err := l.callGateway(ctx, req, messages, nil)
		if err != nil {
			return nil, lastInvalidContent, metrics, fmt.Errorf("max_turns_exceeded")
		}
		metrics.LLMCalls++
		metrics.TokensIn += gwResp.TokensIn
		metrics.TokensOut += gwResp.TokensOut
		metrics.CostUSD += gwResp.CostUSD

		if output, err := parseOutput(gwResp.Content); err == nil {
			return output, "", metrics, nil
		}
	}

	return nil, lastInvalidContent, metrics, fmt.Errorf("max_turns_exceeded")
}

// callGateway POSTs to the gateway /invoke and returns the parsed response.
func (l *Loop) callGateway(ctx context.Context, req InvokeRequest, messages []Message, tools []toolDef) (gatewayResponse, error) {
	body, err := json.Marshal(gatewayRequest{
		RunID:     req.RunID,
		StepID:    req.StepID,
		Group:     req.Model.Group,
		Messages:  messages,
		MaxTokens: req.Model.MaxTokens,
		Tools:     tools,
	})
	if err != nil {
		return gatewayResponse{}, fmt.Errorf("marshal gateway request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, l.gatewayURL+"/invoke", bytes.NewReader(body))
	if err != nil {
		return gatewayResponse{}, fmt.Errorf("create gateway request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := l.httpClient.Do(httpReq)
	if err != nil {
		return gatewayResponse{}, fmt.Errorf("gateway call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp gatewayErrorResponse
		if jsonErr := json.NewDecoder(resp.Body).Decode(&errResp); jsonErr == nil && errResp.Error != "" {
			return gatewayResponse{}, fmt.Errorf("gateway error %s: %s", errResp.Error, errResp.Message)
		}
		return gatewayResponse{}, fmt.Errorf("gateway returned %d", resp.StatusCode)
	}

	var gwResp gatewayResponse
	if err := json.NewDecoder(resp.Body).Decode(&gwResp); err != nil {
		return gatewayResponse{}, fmt.Errorf("decode gateway response: %w", err)
	}
	return gwResp, nil
}

// parseOutput attempts to parse content as a JSON object.
// Strips markdown code fences if present before parsing.
// Returns an error if content is not valid JSON.
func parseOutput(content string) (map[string]any, error) {
	content = stripCodeFence(content)
	var out map[string]any
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		return nil, fmt.Errorf("LLM output is not valid JSON: %w", err)
	}
	return out, nil
}

// stripCodeFence removes markdown code fences (```json ... ``` or ``` ... ```) from s.
func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// Remove opening fence line
	end := strings.Index(s, "\n")
	if end == -1 {
		return s
	}
	s = s[end+1:]
	// Remove closing fence
	if idx := strings.LastIndex(s, "```"); idx != -1 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}
