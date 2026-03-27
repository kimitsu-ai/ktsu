package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	jmespath "github.com/jmespath/go-jmespath"
	"github.com/kimitsu-ai/ktsu/internal/config"
	"github.com/kimitsu-ai/ktsu/internal/orchestrator/airlock"
	"github.com/kimitsu-ai/ktsu/internal/orchestrator/dag"
	"github.com/kimitsu-ai/ktsu/internal/orchestrator/state"
	"github.com/kimitsu-ai/ktsu/pkg/types"
)

// AgentDispatcher dispatches an agent invocation to the runtime and waits for the result.
type AgentDispatcher interface {
	Dispatch(ctx context.Context, runID, stepID string, step *config.PipelineStep, input map[string]interface{}) (map[string]interface{}, types.StepMetrics, error)
}

// Runner executes workflow pipelines.
type Runner struct {
	store      state.Store
	dispatcher AgentDispatcher // nil = agent steps are stubbed
}

// New creates a new Runner.
func New(store state.Store) *Runner {
	return &Runner{store: store}
}

// NewWithDispatcher creates a Runner that dispatches agent steps to the given AgentDispatcher.
func NewWithDispatcher(store state.Store, dispatcher AgentDispatcher) *Runner {
	return &Runner{
		store:      store,
		dispatcher: dispatcher,
	}
}

// Execute runs a workflow pipeline with the provided input.
// The input is pre-populated as stepOutputs["input"] and available to all steps.
func (r *Runner) Execute(ctx context.Context, workflowName string, runID string, wf *config.WorkflowConfig, input map[string]interface{}) error {
	if wf.ModelPolicy != nil && wf.ModelPolicy.TimeoutS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(wf.ModelPolicy.TimeoutS)*time.Second)
		defer cancel()
	}

	now := time.Now()
	run := &types.Run{
		ID:           runID,
		WorkflowName: workflowName,
		Status:       types.RunStatusRunning,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := r.store.CreateRun(ctx, run); err != nil {
		return fmt.Errorf("create run: %w", err)
	}

	// Build DAG nodes
	nodes := make([]dag.Node, len(wf.Pipeline))
	for i, step := range wf.Pipeline {
		nodes[i] = dag.Node{ID: step.ID, Depends: step.DependsOn}
	}

	layers, err := dag.Resolve(nodes)
	if err != nil {
		return fmt.Errorf("dag resolve: %w", err)
	}

	// Build step lookup map
	stepByID := make(map[string]*config.PipelineStep, len(wf.Pipeline))
	for i := range wf.Pipeline {
		stepByID[wf.Pipeline[i].ID] = &wf.Pipeline[i]
	}

	// Pre-populate workflow input as a synthetic step output.
	// All pipeline steps can reference upstream outputs via stepOutputs["input"].
	stepOutputs := make(map[string]map[string]interface{})
	if input != nil {
		stepOutputs["input"] = input
	}

	failRun := func(stepRec *types.Step, errMsg string) error {
		now := time.Now()
		stepRec.Status = types.StepStatusFailed
		stepRec.Error = errMsg
		stepRec.EndedAt = &now
		_ = r.store.UpdateStep(ctx, stepRec)

		run.Status = types.RunStatusFailed
		run.Error = errMsg
		run.UpdatedAt = now
		_ = r.store.UpdateRun(ctx, run)
		return errors.New(errMsg)
	}

	for _, layer := range layers {
		for _, stepID := range layer {
			step := stepByID[stepID]

			// Determine step type
			var stepType types.StepType
			switch {
			case step.Agent != "":
				stepType = types.StepTypeAgent
			case step.Transform != nil:
				stepType = types.StepTypeTransform
			case step.Webhook != nil:
				stepType = types.StepTypeWebhook
			default:
				stepType = types.StepTypeAgent
			}

			startedAt := time.Now()
			stepRec := &types.Step{
				ID:        step.ID,
				RunID:     runID,
				Name:      step.ID,
				Type:      stepType,
				Status:    types.StepStatusRunning,
				StartedAt: &startedAt,
			}
			if err := r.store.CreateStep(ctx, stepRec); err != nil {
				return fmt.Errorf("create step %s: %w", step.ID, err)
			}

			// Execute step
			var rawOutput map[string]interface{}
			var stepMetrics types.StepMetrics
			var execErr error

			switch stepType {
			case types.StepTypeTransform:
				rawOutput, execErr = r.executeTransform(ctx, step, stepOutputs)
			case types.StepTypeWebhook:
				rawOutput, execErr = r.executeWebhook(ctx, step, stepOutputs)
			case types.StepTypeAgent:
				if step.ForEach != nil {
					rawOutput, stepMetrics, execErr = r.executeFanout(ctx, step, stepOutputs, runID)
				} else {
					rawOutput, stepMetrics, execErr = r.executeAgent(ctx, step, stepOutputs, runID)
				}
			}

			if execErr != nil {
				stepRec.Metrics = stepMetrics
				return failRun(stepRec, execErr.Error())
			}

			// Check for skipped from webhook condition
			if skipped, _ := rawOutput["skipped"].(bool); skipped {
				now := time.Now()
				stepRec.Status = types.StepStatusSkipped
				stepRec.EndedAt = &now
				if err := r.store.UpdateStep(ctx, stepRec); err != nil {
					return fmt.Errorf("update step %s: %w", step.ID, err)
				}
				stepOutputs[step.ID] = rawOutput
				continue
			}

			// Process reserved fields
			var schema map[string]interface{}
			if step.Output != nil {
				schema = step.Output.Schema
			}

			cleanOutput, reservedFields, skipReason, reservedErr := ProcessReservedFields(rawOutput, step.ConfidenceThreshold)

			if reservedErr != nil {
				return failRun(stepRec, reservedErr.Error())
			}

			if skipReason != "" {
				now := time.Now()
				stepRec.Status = types.StepStatusSkipped
				stepRec.EndedAt = &now
				if err := r.store.UpdateStep(ctx, stepRec); err != nil {
					return fmt.Errorf("update step %s: %w", step.ID, err)
				}
				stepOutputs[step.ID] = cleanOutput
				continue
			}

			// Airlock validate
			if err := airlock.Validate(cleanOutput, schema, reservedFields); err != nil {
				return failRun(stepRec, err.Error())
			}

			now := time.Now()
			stepRec.Status = types.StepStatusComplete
			stepRec.Output = cleanOutput
			stepRec.Metrics = stepMetrics
			stepRec.EndedAt = &now
			if err := r.store.UpdateStep(ctx, stepRec); err != nil {
				return fmt.Errorf("update step %s: %w", step.ID, err)
			}
			stepOutputs[step.ID] = cleanOutput
		}
	}

	now = time.Now()
	run.Status = types.RunStatusComplete
	run.UpdatedAt = now
	run.CompletedAt = &now
	return r.store.UpdateRun(ctx, run)
}

// executeAgent dispatches an agent step to the runtime and waits for the result.
// If no dispatcher is configured, the step is stubbed with {"stubbed": true}.
func (r *Runner) executeAgent(ctx context.Context, step *config.PipelineStep, stepOutputs map[string]map[string]interface{}, runID string) (map[string]interface{}, types.StepMetrics, error) {
	if r.dispatcher == nil {
		return map[string]interface{}{"stubbed": true}, types.StepMetrics{}, nil
	}

	// Assemble inputs from upstream step outputs.
	input := make(map[string]interface{})
	for id, out := range stepOutputs {
		input[id] = out
	}

	return r.dispatcher.Dispatch(ctx, runID, step.ID, step, input)
}

// executeFanout iterates an agent step over an array resolved from ForEach.From,
// dispatching each item concurrently up to ForEach.Concurrency goroutines.
// Returns {"results": [...]} with each item's output in order.
func (r *Runner) executeFanout(ctx context.Context, step *config.PipelineStep, stepOutputs map[string]map[string]interface{}, runID string) (map[string]interface{}, types.StepMetrics, error) {
	if r.dispatcher == nil {
		return map[string]interface{}{"results": []interface{}{}}, types.StepMetrics{}, nil
	}

	spec := step.ForEach

	// Build JMESPath search context from accumulated step outputs.
	searchCtx := make(map[string]interface{})
	for k, v := range stepOutputs {
		searchCtx[k] = v
	}

	raw, err := jmespath.Search(sanitizeExpr(spec.From), searchCtx)
	if err != nil {
		return nil, types.StepMetrics{}, fmt.Errorf("for_each from %q: %w", spec.From, err)
	}
	items, ok := raw.([]interface{})
	if !ok {
		return nil, types.StepMetrics{}, fmt.Errorf("for_each from %q: expected array, got %T", spec.From, raw)
	}

	if spec.MaxItems > 0 && len(items) > spec.MaxItems {
		items = items[:spec.MaxItems]
	}

	if len(items) == 0 {
		return map[string]interface{}{"results": []interface{}{}}, types.StepMetrics{}, nil
	}

	concurrency := spec.Concurrency
	if concurrency <= 0 || concurrency > len(items) {
		concurrency = len(items)
	}

	type fanoutResult struct {
		output  map[string]interface{}
		metrics types.StepMetrics
		err     error
	}

	results := make([]fanoutResult, len(items))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, item := range items {
		wg.Add(1)
		go func(idx int, itm interface{}) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			itemInput := make(map[string]interface{})
			for k, v := range stepOutputs {
				itemInput[k] = v
			}
			itemInput["item"] = itm
			itemInput["item_index"] = idx

			subStepID := fmt.Sprintf("%s.%d", step.ID, idx)
			out, m, dispErr := r.dispatcher.Dispatch(ctx, runID, subStepID, step, itemInput)
			results[idx] = fanoutResult{output: out, metrics: m, err: dispErr}
		}(i, item)
	}

	wg.Wait()

	outputs := make([]interface{}, len(items))
	var totals types.StepMetrics
	var failCount int
	var firstErr error
	for i, res := range results {
		totals.TokensIn += res.metrics.TokensIn
		totals.TokensOut += res.metrics.TokensOut
		totals.CostUSD += res.metrics.CostUSD
		totals.LLMCalls += res.metrics.LLMCalls
		totals.ToolCalls += res.metrics.ToolCalls
		if res.err != nil {
			failCount++
			if firstErr == nil {
				firstErr = fmt.Errorf("fanout item %d: %w", i, res.err)
			}
			outputs[i] = map[string]interface{}{
				"ktsu_error": res.err.Error(),
				"item_index": i,
			}
		} else {
			outputs[i] = res.output
		}
	}

	maxFailures := spec.MaxFailures
	if maxFailures == 0 && firstErr != nil {
		// Default: fail on first error (preserves existing behavior).
		return nil, totals, firstErr
	}
	if maxFailures > 0 && failCount > maxFailures {
		return nil, totals, fmt.Errorf("fanout: %d items failed (max_failures: %d)", failCount, maxFailures)
	}
	// maxFailures == -1 (unlimited) or failCount within threshold: succeed.

	return map[string]interface{}{"results": outputs}, totals, nil
}

// executeWebhook executes a webhook step: evaluates the condition, builds the body
// via JMESPath, and POSTs to the configured URL. Expects a 2xx response for success.
func (r *Runner) executeWebhook(ctx context.Context, step *config.PipelineStep, stepOutputs map[string]map[string]interface{}) (map[string]interface{}, error) {
	spec := step.Webhook

	// Check condition
	if step.Condition != "" {
		envelopeCtx := make(map[string]interface{})
		for k, v := range stepOutputs {
			envelopeCtx[k] = v
		}
		result, err := jmespath.Search(sanitizeExpr(step.Condition), envelopeCtx)
		if err != nil || isFalsy(result) {
			return map[string]interface{}{"skipped": true, "reason": "condition_false"}, nil
		}
	}

	// Resolve URL — supports env:VAR_NAME
	url := spec.URL
	if strings.HasPrefix(url, "env:") {
		envVar := strings.TrimPrefix(url, "env:")
		url = os.Getenv(envVar)
		if url == "" {
			return nil, fmt.Errorf("env var %s not set for webhook URL", envVar)
		}
	}

	// Build body context from all step outputs
	outCtx := make(map[string]interface{})
	for k, v := range stepOutputs {
		outCtx[k] = v
	}

	// Apply body mapping via JMESPath or env:VAR_NAME
	body := make(map[string]interface{})
	for key, jmesExprRaw := range spec.Body {
		jmesExpr, ok := jmesExprRaw.(string)
		if !ok {
			continue
		}
		if strings.HasPrefix(jmesExpr, "env:") {
			envVar := strings.TrimPrefix(jmesExpr, "env:")
			if val := os.Getenv(envVar); val != "" {
				body[key] = val
			}
			continue
		}
		val, err := jmespath.Search(sanitizeExpr(jmesExpr), outCtx)
		if err != nil {
			return nil, fmt.Errorf("jmespath webhook body %q: %w", jmesExpr, err)
		}
		if val != nil {
			body[key] = val
		}
	}

	method := spec.Method
	if method == "" {
		method = http.MethodPost
	}

	timeout := time.Duration(spec.TimeoutS) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal webhook body: %w", err)
	}

	httpCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(httpCtx, method, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("webhook %s %s: %w", method, url, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("webhook %s %s returned status %d", method, url, resp.StatusCode)
	}

	return map[string]interface{}{"sent": true, "status_code": resp.StatusCode}, nil
}

// executeTransform processes a transform step.
func (r *Runner) executeTransform(_ context.Context, step *config.PipelineStep, stepOutputs map[string]map[string]interface{}) (map[string]interface{}, error) {
	if step.Transform == nil {
		return map[string]interface{}{"result": nil}, nil
	}

	// Collect inputs
	var currentData interface{}
	if len(step.Transform.Inputs) == 1 {
		from := step.Transform.Inputs[0].From
		currentData = stepOutputs[from]
	} else if len(step.Transform.Inputs) > 1 {
		// Merge all inputs into a flat map (left to right, right wins)
		merged := map[string]interface{}{}
		for _, inp := range step.Transform.Inputs {
			if out, ok := stepOutputs[inp.From]; ok {
				for k, v := range out {
					merged[k] = v
				}
			}
		}
		currentData = merged
	}

	// Apply ops sequentially
	for _, op := range step.Transform.Ops {
		for opName, opVal := range op {
			var err error
			currentData, err = applyOp(opName, opVal, currentData, stepOutputs)
			if err != nil {
				return nil, fmt.Errorf("op %s: %w", opName, err)
			}
		}
	}

	return map[string]interface{}{"result": currentData}, nil
}

// applyOp applies a single transform operation.
func applyOp(opName string, opVal interface{}, currentData interface{}, stepOutputs map[string]map[string]interface{}) (interface{}, error) {
	switch opName {
	case "merge":
		stepIDs, ok := opVal.([]interface{})
		if !ok {
			return currentData, fmt.Errorf("merge: expected []interface{}, got %T", opVal)
		}
		return applyMerge(currentData, stepIDs, stepOutputs)

	case "filter":
		opMap, ok := opVal.(map[string]interface{})
		if !ok {
			return currentData, fmt.Errorf("filter: expected map, got %T", opVal)
		}
		expr, _ := opMap["expr"].(string)
		return applyFilter(currentData, expr)

	case "sort":
		opMap, ok := opVal.(map[string]interface{})
		if !ok {
			return currentData, fmt.Errorf("sort: expected map, got %T", opVal)
		}
		field, _ := opMap["field"].(string)
		order, _ := opMap["order"].(string)
		return applySort(currentData, field, order)

	case "map":
		opMap, ok := opVal.(map[string]interface{})
		if !ok {
			return currentData, fmt.Errorf("map: expected map, got %T", opVal)
		}
		expr, _ := opMap["expr"].(string)
		return applyMap(currentData, expr)

	case "flatten":
		return applyFlatten(currentData)

	case "deduplicate":
		opMap, ok := opVal.(map[string]interface{})
		if !ok {
			return currentData, fmt.Errorf("deduplicate: expected map, got %T", opVal)
		}
		field, _ := opMap["field"].(string)
		return applyDeduplicate(currentData, field)

	default:
		return currentData, fmt.Errorf("unknown op: %s", opName)
	}
}

// applyMerge deep-merges the named steps' outputs.
func applyMerge(currentData interface{}, stepIDs []interface{}, stepOutputs map[string]map[string]interface{}) (interface{}, error) {
	var result interface{} = currentData

	for _, id := range stepIDs {
		stepID, _ := id.(string)
		stepOut, ok := stepOutputs[stepID]
		if !ok {
			continue
		}
		result = deepMerge(result, stepOut)
	}

	return result, nil
}

// deepMerge recursively merges b into a. For maps, keys from b override a.
// For arrays, they are concatenated.
func deepMerge(a, b interface{}) interface{} {
	aMap, aIsMap := a.(map[string]interface{})
	bMap, bIsMap := b.(map[string]interface{})
	if aIsMap && bIsMap {
		result := make(map[string]interface{})
		for k, v := range aMap {
			result[k] = v
		}
		for k, v := range bMap {
			if existing, ok := result[k]; ok {
				result[k] = deepMerge(existing, v)
			} else {
				result[k] = v
			}
		}
		return result
	}

	aSlice, aIsSlice := a.([]interface{})
	bSlice, bIsSlice := b.([]interface{})
	if aIsSlice && bIsSlice {
		combined := make([]interface{}, 0, len(aSlice)+len(bSlice))
		combined = append(combined, aSlice...)
		combined = append(combined, bSlice...)
		return combined
	}

	// Default: b wins
	return b
}

// applyFilter applies a JMESPath filter expression to the data.
func applyFilter(currentData interface{}, expr string) (interface{}, error) {
	// Ensure we have an array
	var items []interface{}
	switch v := currentData.(type) {
	case []interface{}:
		items = v
	case map[string]interface{}:
		items = []interface{}{v}
	default:
		if currentData != nil {
			items = []interface{}{currentData}
		}
	}

	result, err := jmespath.Search("[?"+sanitizeExpr(expr)+"]", items)
	if err != nil {
		return nil, fmt.Errorf("jmespath filter [?%s]: %w", expr, err)
	}
	return result, nil
}

// applySort sorts an array by a field.
func applySort(currentData interface{}, field string, order string) (interface{}, error) {
	items, ok := currentData.([]interface{})
	if !ok {
		return currentData, fmt.Errorf("sort: expected []interface{}, got %T", currentData)
	}

	sorted := make([]interface{}, len(items))
	copy(sorted, items)

	sort.SliceStable(sorted, func(i, j int) bool {
		iVal := extractField(sorted[i], field)
		jVal := extractField(sorted[j], field)

		less := compareValues(iVal, jVal)
		if order == "desc" {
			return !less
		}
		return less
	})

	return sorted, nil
}

// extractField extracts a field value from an item using JMESPath.
func extractField(item interface{}, field string) interface{} {
	val, err := jmespath.Search(sanitizeExpr(field), item)
	if err != nil {
		return nil
	}
	return val
}

// compareValues compares two values for sorting (returns true if a < b).
// nil values always sort last.
func compareValues(a, b interface{}) bool {
	if a == nil {
		return false // nil sorts last
	}
	if b == nil {
		return true // nil sorts last
	}
	aFloat, aOk := toFloat64(a)
	bFloat, bOk := toFloat64(b)
	if aOk && bOk {
		return aFloat < bFloat
	}
	return fmt.Sprintf("%v", a) < fmt.Sprintf("%v", b)
}

func toFloat64(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case int32:
		return float64(val), true
	}
	return 0, false
}

// applyMap applies a JMESPath expression to each item.
func applyMap(currentData interface{}, expr string) (interface{}, error) {
	var items []interface{}
	switch v := currentData.(type) {
	case []interface{}:
		items = v
	case map[string]interface{}:
		items = []interface{}{v}
	default:
		if currentData != nil {
			items = []interface{}{currentData}
		}
	}

	result := make([]interface{}, 0, len(items))
	for _, item := range items {
		val, err := jmespath.Search(sanitizeExpr(expr), item)
		if err != nil {
			return nil, fmt.Errorf("jmespath map %q: %w", expr, err)
		}
		result = append(result, val)
	}
	return result, nil
}

// applyFlatten flattens one level of nested arrays.
func applyFlatten(currentData interface{}) (interface{}, error) {
	items, ok := currentData.([]interface{})
	if !ok {
		return currentData, nil
	}

	result := make([]interface{}, 0)
	for _, item := range items {
		if nested, ok := item.([]interface{}); ok {
			result = append(result, nested...)
		} else {
			result = append(result, item)
		}
	}
	return result, nil
}

// applyDeduplicate removes duplicates based on a field (first occurrence wins).
func applyDeduplicate(currentData interface{}, field string) (interface{}, error) {
	items, ok := currentData.([]interface{})
	if !ok {
		return currentData, nil
	}

	seen := make(map[string]bool)
	var result []interface{}
	for _, item := range items {
		val := extractField(item, field)
		key := fmt.Sprintf("%v", val)
		if !seen[key] {
			seen[key] = true
			result = append(result, item)
		}
	}
	return result, nil
}

// isFalsy returns true if a value is nil, false, empty string, or 0.
func isFalsy(v interface{}) bool {
	if v == nil {
		return true
	}
	switch val := v.(type) {
	case bool:
		return !val
	case string:
		return val == ""
	case float64:
		return val == 0
	case int:
		return val == 0
	case int64:
		return val == 0
	}
	return false
}

// ProcessReservedFields extracts and processes ktsu_* fields from output.
// Returns: (cleanOutput, reservedFields, skipReason, error)
func ProcessReservedFields(rawOutput map[string]interface{}, threshold float64) (map[string]interface{}, *types.ReservedFields, string, error) {
	reserved := &types.ReservedFields{}

	// 1. Check injection attempt
	if v, ok := rawOutput["ktsu_injection_attempt"]; ok {
		if b, ok := v.(bool); ok && b {
			return nil, nil, "", fmt.Errorf("injection attempt detected")
		}
	}

	// 2. Check untrusted content
	if v, ok := rawOutput["ktsu_untrusted_content"]; ok {
		if b, ok := v.(bool); ok && b {
			return nil, nil, "", fmt.Errorf("untrusted content detected")
		}
	}

	// 3. Check low quality
	if v, ok := rawOutput["ktsu_low_quality"]; ok {
		if b, ok := v.(bool); ok && b {
			return nil, nil, "", fmt.Errorf("low quality output")
		}
	}

	// 4. Check needs human
	if v, ok := rawOutput["ktsu_needs_human"]; ok {
		if b, ok := v.(bool); ok && b {
			return nil, nil, "", fmt.Errorf("needs_human_review")
		}
	}

	// 5. Check confidence
	var confidence float64
	if v, ok := rawOutput["ktsu_confidence"]; ok {
		if f, ok := v.(float64); ok {
			confidence = f
		}
	}
	reserved.Confidence = confidence
	if threshold > 0 && confidence < threshold {
		return nil, nil, "", fmt.Errorf("confidence below threshold")
	}

	// 6. Check skip reason
	var skipReason string
	if v, ok := rawOutput["ktsu_skip_reason"]; ok {
		if s, ok := v.(string); ok && s != "" {
			skipReason = s
			reserved.SkipReason = s
		}
	}

	// 7. Extract flags and rationale
	if v, ok := rawOutput["ktsu_flags"]; ok {
		if flags, ok := v.([]string); ok {
			reserved.Flags = flags
		} else if flags, ok := v.([]interface{}); ok {
			for _, f := range flags {
				if s, ok := f.(string); ok {
					reserved.Flags = append(reserved.Flags, s)
				}
			}
		}
	}
	if v, ok := rawOutput["ktsu_rationale"]; ok {
		if s, ok := v.(string); ok {
			reserved.Rationale = s
		}
	}

	// Strip all ktsu_* keys from output
	cleanOutput := make(map[string]interface{})
	for k, v := range rawOutput {
		if !strings.HasPrefix(k, types.ReservedPrefix) {
			cleanOutput[k] = v
		}
	}

	return cleanOutput, reserved, skipReason, nil
}
