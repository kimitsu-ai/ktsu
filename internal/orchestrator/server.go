package orchestrator

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kimitsu-ai/ktsu/internal/config"
	"github.com/kimitsu-ai/ktsu/internal/orchestrator/airlock"
	"github.com/kimitsu-ai/ktsu/internal/orchestrator/runner"
	"github.com/kimitsu-ai/ktsu/internal/orchestrator/state"
	"github.com/kimitsu-ai/ktsu/internal/runtime/agent"
	"github.com/kimitsu-ai/ktsu/pkg/types"
)

// defaultProjectDir returns "." when projectDir is empty.
func defaultProjectDir(d string) string {
	if d == "" {
		return "."
	}
	return d
}

// stepCallbackKey is used as a map key for pending step callbacks.
type stepCallbackKey struct{ runID, stepID string }

type server struct {
	o                *Orchestrator
	store            state.Store
	runner           *runner.Runner
	mux              *http.ServeMux
	pendingMu        sync.Mutex
	pendingCallbacks map[stepCallbackKey]chan agent.CallbackPayload
	logger           *log.Logger
}

func (s *server) logf(format string, args ...any) {
	if s.logger != nil {
		s.logger.Printf(format, args...)
	} else {
		log.Printf(format, args...)
	}
}

func newServer(o *Orchestrator) (*server, error) {
	store, err := state.NewStore(state.StoreConfig{
		Type: o.cfg.StoreType,
		DSN:  o.cfg.StoreDSN,
	})
	if err != nil {
		return nil, fmt.Errorf("init store: %w", err)
	}

	s := &server{
		o:                o,
		store:            store,
		mux:              http.NewServeMux(),
		pendingCallbacks: make(map[stepCallbackKey]chan agent.CallbackPayload),
		logger:           o.logger,
	}

	var r *runner.Runner
	if o.cfg.RuntimeURL != "" {
		r = runner.NewWithDispatcher(store, &runtimeDispatcher{
			runtimeURL:       o.cfg.RuntimeURL,
			ownURL:           o.cfg.OwnURL,
			projectDir:       defaultProjectDir(o.cfg.ProjectDir),
			pendingMu:        &s.pendingMu,
			pendingCallbacks: s.pendingCallbacks,
		})
	} else {
		r = runner.New(store)
	}
	s.runner = r
	s.routes()
	return s, nil
}

func (s *server) requireAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.o.cfg.APIKey == "" {
			h(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+s.o.cfg.APIKey {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{
				"error":   "unauthorized",
				"message": "Set Authorization: Bearer <key> header. See KTSU_API_KEY.",
			})
			return
		}
		h(w, r)
	}
}

func (s *server) routes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("POST /invoke/{workflow}", s.requireAuth(s.handleInvoke))
	s.mux.HandleFunc("GET /runs/{run_id}", s.requireAuth(s.handleGetRun))
	s.mux.HandleFunc("GET /envelope/{run_id}", s.requireAuth(s.handleGetEnvelope))
	s.mux.HandleFunc("POST /heartbeat", s.handleHeartbeat)
	s.mux.HandleFunc("POST /runs/{run_id}/steps/{step_id}/complete", s.handleStepComplete)
	s.mux.HandleFunc("GET /runs/{run_id}/steps/{step_id}/approval", s.handleGetApproval)
	s.mux.HandleFunc("POST /runs/{run_id}/steps/{step_id}/approval/decide", s.handleDecideApproval)
	s.mux.HandleFunc("GET /approvals", s.handleListApprovals)
}

type healthResponse struct {
	Status   string            `json:"status"`
	Services map[string]string `json:"services"`
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	results := make(map[string]string)
	results["orchestrator"] = "ok"

	var wg sync.WaitGroup
	var mu sync.Mutex

	check := func(name, url string) {
		if url == "" {
			mu.Lock()
			results[name] = "unconfigured"
			mu.Unlock()
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			status := s.checkService(ctx, url)
			mu.Lock()
			results[name] = status
			mu.Unlock()
		}()
	}

	check("runtime", s.o.cfg.RuntimeURL)
	check("gateway", s.o.cfg.GatewayURL)

	wg.Wait()

	overall := "ok"
	for _, status := range results {
		if status != "ok" && status != "unconfigured" {
			overall = "error"
			break
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if overall != "ok" {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	json.NewEncoder(w).Encode(healthResponse{
		Status:   overall,
		Services: results,
	})
}

func (s *server) checkService(ctx context.Context, url string) string {
	// url is already checked for empty in handleHealth
	req, err := http.NewRequestWithContext(ctx, "GET", url+"/health", nil)
	if err != nil {
		return "error"
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "down"
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "unhealthy"
	}
	return "ok"
}

func (s *server) handleInvoke(w http.ResponseWriter, r *http.Request) {
	workflow := r.PathValue("workflow")

	input := map[string]interface{}{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.logf("handleInvoke: non-JSON body (treating as empty): %v", err)
		input = map[string]interface{}{}
	}

	wfPath := filepath.Join(s.o.cfg.WorkflowDir, workflow+".workflow.yaml")
	wf, err := config.LoadWorkflow(wfPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("workflow not found: %s", workflow), http.StatusNotFound)
		return
	}

	// Sub-workflow visibility: direct invocation via /invoke is forbidden.
	// Return 404 (not 403) to avoid leaking information about existence.
	if wf.Visibility == "sub-workflow" {
		http.Error(w, fmt.Sprintf("workflow not found: %s", workflow), http.StatusNotFound)
		return
	}

	if err := airlock.ValidateInput(input, wf.Input.Schema); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	runID := generateRunID()

	go func() {
		ctx := context.Background()
		if err := s.runner.Execute(ctx, workflow, runID, wf, input, nil); err != nil {
			s.logf("run %s failed: %v", runID, err)
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"run_id": runID, "status": "accepted"})
}

// generateRunID creates a random run identifier.
func generateRunID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "run_" + hex.EncodeToString(b)
}

func (s *server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("run_id")
	run, err := s.store.GetRun(r.Context(), runID)
	if err != nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	steps, _ := s.store.ListSteps(r.Context(), runID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"run":   run,
		"steps": steps,
	})
}

func (s *server) handleGetEnvelope(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("run_id")
	env, err := s.store.GetEnvelope(r.Context(), runID)
	if err != nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(env)
}

func (s *server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	s.logf("heartbeat: %v", payload)
	w.WriteHeader(http.StatusOK)
}

func (s *server) handleStepComplete(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("run_id")
	stepID := r.PathValue("step_id")

	var payload agent.CallbackPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	// Handle pending_approval — store the approval, do not signal the dispatcher channel.
	if payload.Status == "pending_approval" {
		s.handlePendingApproval(w, r, runID, stepID, payload)
		return
	}

	// Process reserved fields if the step succeeded.
	if payload.Status == "ok" && payload.Output != nil {
		clean, reserved, skipReason, err := runner.ProcessReservedFields(payload.Output, 0)
		if err != nil {
			payload.Status = "failed"
			payload.Error = err.Error()
			payload.Output = nil
		} else if skipReason != "" {
			payload.Status = "skipped"
			payload.Output = clean
		} else {
			if err := airlock.Validate(clean, nil, reserved); err != nil {
				payload.Status = "failed"
				payload.Error = "air-lock: " + err.Error()
				payload.Output = nil
			} else {
				payload.Output = clean
			}
		}
	}

	// Accumulate metrics from the pending_approval leg when this is a resume callback.
	if payload.IsResume && s.store != nil {
		if approval, approvalErr := s.store.GetApproval(r.Context(), runID, stepID); approvalErr == nil {
			payload.Metrics.TokensIn += approval.PartialMetrics.TokensIn
			payload.Metrics.TokensOut += approval.PartialMetrics.TokensOut
			payload.Metrics.CostUSD += approval.PartialMetrics.CostUSD
			payload.Metrics.LLMCalls += approval.PartialMetrics.LLMCalls
			payload.Metrics.ToolCalls += approval.PartialMetrics.ToolCalls
		}
	}

	// Signal the waiting runner goroutine.
	key := stepCallbackKey{runID, stepID}
	s.pendingMu.Lock()
	ch, ok := s.pendingCallbacks[key]
	s.pendingMu.Unlock()

	if ok {
		ch <- payload
	} else {
		s.logf("handleStepComplete: no pending waiter for %s/%s", runID, stepID)
	}

	w.WriteHeader(http.StatusOK)
}

func (s *server) handlePendingApproval(w http.ResponseWriter, r *http.Request, runID, stepID string, payload agent.CallbackPayload) {
	ctx := r.Context()

	// Update step status to pending_approval and save partial metrics + messages.
	if s.store != nil {
		if step, err := s.store.GetStep(ctx, runID, stepID); err == nil {
			step.Status = types.StepStatusPendingApproval
			m := payload.Metrics
			step.Metrics = types.StepMetrics{
				TokensIn:  m.TokensIn,
				TokensOut: m.TokensOut,
				CostUSD:   m.CostUSD,
				LLMCalls:  m.LLMCalls,
				ToolCalls: m.ToolCalls,
			}
			if len(payload.Messages) > 0 {
				if msgsJSON, err := json.Marshal(payload.Messages); err == nil {
					step.Messages = msgsJSON
				}
			}
			_ = s.store.UpdateStep(ctx, step)
		}

		if pa := payload.PendingApproval; pa != nil {
			approval := &types.Approval{
				RunID:           runID,
				StepID:          stepID,
				ToolName:        pa.ToolName,
				ToolUseID:       pa.ToolUseID,
				Arguments:       pa.Arguments,
				OnReject:        pa.OnReject,
				TimeoutMS:       pa.TimeoutMS,
				TimeoutBehavior: pa.TimeoutBehavior,
				Status:          types.ApprovalStatusPending,
				CreatedAt:       time.Now(),
				OriginalRequest: payload.OriginalRequest,
				PartialMetrics: types.StepMetrics{
					TokensIn:  payload.Metrics.TokensIn,
					TokensOut: payload.Metrics.TokensOut,
					CostUSD:   payload.Metrics.CostUSD,
					LLMCalls:  payload.Metrics.LLMCalls,
					ToolCalls: payload.Metrics.ToolCalls,
				},
			}
			if createErr := s.store.CreateApproval(ctx, approval); createErr == nil {
				// Fire on:approval webhook steps asynchronously.
				go s.fireApprovalWebhooks(runID, stepID, approval)

				// Start timeout goroutine if a timeout is configured.
				if pa.TimeoutMS > 0 {
					go s.runApprovalTimeout(runID, stepID, pa.TimeoutMS, pa.TimeoutBehavior)
				}
			}
		}
	}

	w.WriteHeader(http.StatusOK)
}

func (s *server) handleGetApproval(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("run_id")
	stepID := r.PathValue("step_id")

	if s.store == nil {
		http.Error(w, "store not configured", http.StatusInternalServerError)
		return
	}

	approval, err := s.store.GetApproval(r.Context(), runID, stepID)
	if err != nil {
		http.Error(w, "approval not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(approval)
}

func (s *server) handleListApprovals(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "store not configured", http.StatusInternalServerError)
		return
	}

	approvals, err := s.store.ListPendingApprovals(r.Context())
	if err != nil {
		http.Error(w, "failed to list approvals", http.StatusInternalServerError)
		return
	}
	if approvals == nil {
		approvals = []*types.Approval{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(approvals)
}

func (s *server) handleDecideApproval(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("run_id")
	stepID := r.PathValue("step_id")
	ctx := r.Context()

	if s.store == nil {
		http.Error(w, "store not configured", http.StatusInternalServerError)
		return
	}

	var body struct {
		Decision string `json:"decision"` // "approved" | "rejected"
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || (body.Decision != "approved" && body.Decision != "rejected") {
		http.Error(w, `decision must be "approved" or "rejected"`, http.StatusBadRequest)
		return
	}

	approval, err := s.store.GetApproval(ctx, runID, stepID)
	if err != nil {
		http.Error(w, "approval not found", http.StatusNotFound)
		return
	}
	if approval.Status != types.ApprovalStatusPending {
		http.Error(w, "approval already decided", http.StatusConflict)
		return
	}

	now := time.Now()
	if body.Decision == "approved" {
		approval.Status = types.ApprovalStatusApproved
		approval.DecidedAt = &now
		if err := s.store.UpdateApproval(ctx, approval); err != nil {
			http.Error(w, "failed to update approval", http.StatusInternalServerError)
			return
		}
		if err := s.redispatchApproved(ctx, approval); err != nil {
			log.Printf("redispatchApproved run=%s step=%s: %v", runID, stepID, err)
			http.Error(w, "failed to dispatch resume", http.StatusInternalServerError)
			return
		}
	} else { // rejected
		approval.Status = types.ApprovalStatusRejected
		approval.DecidedAt = &now
		if err := s.store.UpdateApproval(ctx, approval); err != nil {
			http.Error(w, "failed to update approval", http.StatusInternalServerError)
			return
		}
		s.redispatchRejected(ctx, approval)
	}

	w.WriteHeader(http.StatusOK)
}

// redispatchApproved re-invokes the runtime with the approved tool call.
func (s *server) redispatchApproved(ctx context.Context, approval *types.Approval) error {
	if s.o == nil || s.o.cfg.RuntimeURL == "" {
		return fmt.Errorf("runtime URL not configured")
	}

	var req agent.InvokeRequest
	if err := json.Unmarshal(approval.OriginalRequest, &req); err != nil {
		return fmt.Errorf("unmarshal original request: %w", err)
	}

	// Restore the checkpoint messages from the step.
	if s.store != nil {
		if step, err := s.store.GetStep(ctx, approval.RunID, approval.StepID); err == nil && len(step.Messages) > 0 {
			var msgs []agent.Message
			if jsonErr := json.Unmarshal(step.Messages, &msgs); jsonErr == nil {
				req.Messages = msgs
			}
		}
	}

	req.IsResume = true
	req.ApprovedToolCalls = []string{approval.ToolUseID}

	return s.postToRuntime(ctx, req)
}

// redispatchRejected handles a rejected approval according to on_reject policy.
func (s *server) redispatchRejected(ctx context.Context, approval *types.Approval) {
	switch approval.OnReject {
	case "recover":
		// Re-invoke runtime with a tool_result saying the call was rejected.
		if err := s.redispatchRecover(ctx, approval); err != nil {
			log.Printf("redispatchRecover run=%s step=%s: %v", approval.RunID, approval.StepID, err)
			// Fall through to fail the step.
			s.signalStepFailed(approval.RunID, approval.StepID, "approval rejected (recover dispatch failed): "+err.Error(), approval.PartialMetrics)
		}
	default: // "fail"
		s.signalStepFailed(approval.RunID, approval.StepID, "tool call rejected by reviewer: "+approval.ToolName, approval.PartialMetrics)
	}
}

// redispatchRecover re-invokes the runtime with a rejection message so the agent can try an alternative.
func (s *server) redispatchRecover(ctx context.Context, approval *types.Approval) error {
	if s.o == nil || s.o.cfg.RuntimeURL == "" {
		return fmt.Errorf("runtime URL not configured")
	}

	var req agent.InvokeRequest
	if err := json.Unmarshal(approval.OriginalRequest, &req); err != nil {
		return fmt.Errorf("unmarshal original request: %w", err)
	}

	// Restore checkpoint messages and append a tool_result indicating rejection.
	var msgs []agent.Message
	if s.store != nil {
		if step, err := s.store.GetStep(ctx, approval.RunID, approval.StepID); err == nil && len(step.Messages) > 0 {
			_ = json.Unmarshal(step.Messages, &msgs)
		}
	}

	rejectionResult, _ := json.Marshal([]map[string]any{{
		"type":        "tool_result",
		"tool_use_id": approval.ToolUseID,
		"content":     "Tool call rejected by human reviewer. Please find an alternative approach.",
	}})
	msgs = append(msgs, agent.Message{Role: "user", Content: string(rejectionResult)})

	req.Messages = msgs
	req.IsResume = true
	// No ApprovedToolCalls — agent must find a new approach.

	return s.postToRuntime(ctx, req)
}

// postToRuntime POSTs an InvokeRequest to the runtime /invoke endpoint.
func (s *server) postToRuntime(ctx context.Context, req agent.InvokeRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal invoke request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.o.cfg.RuntimeURL+"/invoke", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("post to runtime: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("runtime returned %d", resp.StatusCode)
	}
	return nil
}

// signalStepFailed signals the dispatcher channel with a failed payload.
func (s *server) signalStepFailed(runID, stepID, errMsg string, partial types.StepMetrics) {
	payload := agent.CallbackPayload{
		RunID:  runID,
		StepID: stepID,
		Status: "failed",
		Error:  errMsg,
		Metrics: agent.Metrics{
			TokensIn:  partial.TokensIn,
			TokensOut: partial.TokensOut,
			CostUSD:   partial.CostUSD,
			LLMCalls:  partial.LLMCalls,
			ToolCalls: partial.ToolCalls,
		},
	}
	key := stepCallbackKey{runID, stepID}
	s.pendingMu.Lock()
	ch, ok := s.pendingCallbacks[key]
	s.pendingMu.Unlock()
	if ok {
		ch <- payload
	} else {
		log.Printf("signalStepFailed: no pending waiter for %s/%s", runID, stepID)
	}
}

// runApprovalTimeout fires after timeoutMS and either rejects or fails the pending approval.
func (s *server) runApprovalTimeout(runID, stepID string, timeoutMS int64, behavior string) {
	timer := time.NewTimer(time.Duration(timeoutMS) * time.Millisecond)
	defer timer.Stop()
	<-timer.C

	ctx := context.Background()
	approval, err := s.store.GetApproval(ctx, runID, stepID)
	if err != nil || approval.Status != types.ApprovalStatusPending {
		return // Already decided.
	}

	now := time.Now()
	approval.Status = types.ApprovalStatusTimeout
	approval.DecidedAt = &now
	_ = s.store.UpdateApproval(ctx, approval)

	switch behavior {
	case "reject":
		s.redispatchRejected(ctx, approval)
	default: // "fail"
		s.signalStepFailed(runID, stepID, "approval timed out: "+approval.ToolName, approval.PartialMetrics)
	}
}

// fireApprovalWebhooks finds pipeline steps with on:approval that depend on stepID and fires their webhooks.
func (s *server) fireApprovalWebhooks(runID, stepID string, approval *types.Approval) {
	if s.store == nil || s.o == nil {
		return
	}
	ctx := context.Background()

	run, err := s.store.GetRun(ctx, runID)
	if err != nil {
		return
	}

	wfPath := filepath.Join(s.o.cfg.WorkflowDir, run.WorkflowName+".workflow.yaml")
	wf, err := config.LoadWorkflow(wfPath)
	if err != nil {
		return
	}

	approvalBody, _ := json.Marshal(map[string]any{
		"run_id":      approval.RunID,
		"step_id":     approval.StepID,
		"tool_name":   approval.ToolName,
		"tool_use_id": approval.ToolUseID,
		"arguments":   approval.Arguments,
		"status":      "pending",
	})

	for _, step := range wf.Pipeline {
		if step.On != "approval" || step.Webhook == nil {
			continue
		}
		// Check if this step depends on the pending step.
		depends := false
		for _, dep := range step.DependsOn {
			if dep == stepID {
				depends = true
				break
			}
		}
		if !depends {
			continue
		}

		url := step.Webhook.URL
		if strings.HasPrefix(url, "env:") {
			url = os.Getenv(strings.TrimPrefix(url, "env:"))
		}
		if url == "" {
			continue
		}

		method := step.Webhook.Method
		if method == "" {
			method = http.MethodPost
		}

		timeout := time.Duration(step.Webhook.TimeoutS) * time.Second
		if timeout == 0 {
			timeout = 30 * time.Second
		}

		httpCtx, cancel := context.WithTimeout(ctx, timeout)
		req, err := http.NewRequestWithContext(httpCtx, method, url, bytes.NewReader(approvalBody))
		if err != nil {
			cancel()
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			log.Printf("fireApprovalWebhook run=%s step=%s webhook=%s: %v", runID, stepID, step.ID, err)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			log.Printf("fireApprovalWebhook run=%s step=%s webhook=%s: status %d", runID, stepID, step.ID, resp.StatusCode)
		}
	}
}

func (s *server) serve(ctx context.Context) error {
	port := s.o.cfg.Port
	if port == 0 {
		port = 5050
	}
	addr := net.JoinHostPort(s.o.cfg.Host, strconv.Itoa(port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("orchestrator failed to bind to %s: %w", addr, err)
	}
	s.logf("orchestrator listening on %s", addr)
	srv := &http.Server{Handler: s.mux}
	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()
	return srv.Serve(ln)
}

// runtimeDispatcher implements runner.AgentDispatcher by POSTing to the runtime.
type runtimeDispatcher struct {
	runtimeURL       string
	ownURL           string
	projectDir       string
	pendingMu        *sync.Mutex
	pendingCallbacks map[stepCallbackKey]chan agent.CallbackPayload
}

func (d *runtimeDispatcher) Dispatch(ctx context.Context, runID, stepID string, step *config.PipelineStep, input map[string]interface{}) (map[string]interface{}, types.StepMetrics, error) {
	callbackURL := d.ownURL + "/runs/" + runID + "/steps/" + stepID + "/complete"

	// Register a channel before sending to avoid a race with a fast callback.
	ch := make(chan agent.CallbackPayload, 1)
	key := stepCallbackKey{runID, stepID}
	d.pendingMu.Lock()
	d.pendingCallbacks[key] = ch
	d.pendingMu.Unlock()
	defer func() {
		d.pendingMu.Lock()
		delete(d.pendingCallbacks, key)
		d.pendingMu.Unlock()
	}()

	// Load agent config and resolve tool servers when an agent path is provided.
	var (
		agentName     string
		system        string
		reflectPrompt string
		maxTurns      = 10
		modelGroup    string
		maxTokens     = 1024
		toolServers   []agent.ToolServerSpec
		outputSchema  map[string]any
	)

	var zero types.StepMetrics
	if step != nil && step.Agent != "" {
		agentPath := filepath.Join(d.projectDir, config.StripVersion(step.Agent))
		agentCfg, err := config.LoadAgent(agentPath)
		if err != nil {
			return nil, zero, fmt.Errorf("load agent %s: %w", step.Agent, err)
		}
		// Parse the agent's params schema
		declaredParams, parseErr := config.ParseParamsSchema(agentCfg.Params.Schema)
		if parseErr != nil {
			return nil, zero, fmt.Errorf("agent %s params schema: %w", agentCfg.Name, parseErr)
		}
		// Validate prompt refs against declared params
		if err := config.ValidatePromptRefs(agentCfg.Prompt.System, declaredParams); err != nil {
			return nil, zero, fmt.Errorf("agent %s prompt validation: %w", agentCfg.Name, err)
		}
		agentName = agentCfg.Name
		modelGroup = agentCfg.Model

		// Resolve agent params and interpolate system prompt.
		resolvedAgentParams, resolveErr := config.ResolveAgentParams(declaredParams, step.AgentParams())
		if resolveErr != nil {
			return nil, zero, fmt.Errorf("agent %s param resolution: %w", agentCfg.Name, resolveErr)
		}
		var interpErr error
		system, interpErr = config.InterpolatePrompt(agentCfg.Prompt.System, resolvedAgentParams)
		if interpErr != nil {
			return nil, zero, fmt.Errorf("agent %s prompt interpolation: %w", agentCfg.Name, interpErr)
		}
		if agentCfg.Reflect != "" {
			var reflectErr error
			reflectPrompt, reflectErr = config.InterpolatePrompt(agentCfg.Reflect, resolvedAgentParams)
			if reflectErr != nil {
				return nil, zero, fmt.Errorf("agent %s reflect interpolation: %w", agentCfg.Name, reflectErr)
			}
		}
		if agentCfg.MaxTurns > 0 {
			maxTurns = agentCfg.MaxTurns
		}
		if agentCfg.Output == nil || len(agentCfg.Output.Schema) == 0 {
			return nil, zero, fmt.Errorf("agent %s has no output schema defined", agentCfg.Name)
		}
		outputSchema = agentCfg.Output.Schema
		for _, srv := range agentCfg.Servers {
			serverPath := srv.Path
			if srv.Path != "" && !filepath.IsAbs(srv.Path) {
				// Relative to agent file
				serverPath = filepath.Join(filepath.Dir(agentPath), srv.Path)
			} else if srv.Path != "" && filepath.IsAbs(srv.Path) {
				// If it's absolute but doesn't exist, try project-relative.
				// This handles "/servers/foo.yaml" in a container where project root is /.
				if _, err := os.Stat(srv.Path); os.IsNotExist(err) {
					serverPath = filepath.Join(d.projectDir, srv.Path)
				}
			}

			serverCfg, err := config.LoadToolServer(serverPath)
			if err != nil {
				return nil, zero, fmt.Errorf("load server %s: %w", srv.Path, err)
			}
			authVal, authErr := config.ResolveValue(serverCfg.Auth, true, resolvedAgentParams)
			if authErr != nil {
				return nil, zero, fmt.Errorf("server %q auth: %w", srv.Name, authErr)
			}
			authToken := authVal
			// Resolve server params (nil map access on step.ServerParams() returns nil safely).
			resolvedServerParams, serverParamErr := config.ResolveServerParams(
				serverCfg.Params,
				srv.Params,
				step.ServerParams()[srv.Name],
			)
			if serverParamErr != nil {
				return nil, zero, fmt.Errorf("server %s param resolution: %w", srv.Name, serverParamErr)
			}
			var serverParamsAny map[string]any
			if len(resolvedServerParams) > 0 {
				serverParamsAny = make(map[string]any, len(resolvedServerParams))
				for k, v := range resolvedServerParams {
					serverParamsAny[k] = v
				}
			}
			var allowlist []string
			var approvalRules []agent.ToolApprovalRule
			for _, ta := range srv.Access.Allowlist {
				allowlist = append(allowlist, ta.Name)
				if ta.RequireApproval != nil {
					approvalRules = append(approvalRules, agent.ToolApprovalRule{
						Pattern:         ta.Name,
						OnReject:        ta.RequireApproval.OnReject,
						TimeoutMS:       ta.RequireApproval.Timeout.Milliseconds(),
						TimeoutBehavior: ta.RequireApproval.TimeoutBehavior,
					})
				}
			}
			toolServers = append(toolServers, agent.ToolServerSpec{
				Name:          serverCfg.Name,
				URL:           serverCfg.URL,
				Allowlist:     allowlist,
				AuthToken:     authToken,
				Params:        serverParamsAny,
				ApprovalRules: approvalRules,
			})
		}
	}

	// Step-level model overrides agent default.
	if step != nil && step.Model != nil {
		if step.Model.Group != "" {
			modelGroup = step.Model.Group
		}
		if step.Model.MaxTokens > 0 {
			maxTokens = step.Model.MaxTokens
		}
	}

	var confidenceThreshold float64
	if step != nil {
		confidenceThreshold = step.ConfidenceThreshold
	}

	req := agent.InvokeRequest{
		RunID:               runID,
		StepID:              stepID,
		AgentName:           agentName,
		System:              system,
		Reflect:             reflectPrompt,
		ConfidenceThreshold: confidenceThreshold,
		MaxTurns:            maxTurns,
		Model:               agent.ModelSpec{Group: modelGroup, MaxTokens: maxTokens},
		ToolServers:         toolServers,
		Input:               input,
		CallbackURL:         callbackURL,
		OutputSchema:        outputSchema,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, zero, fmt.Errorf("marshal invoke request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, d.runtimeURL+"/invoke", bytes.NewReader(body))
	if err != nil {
		return nil, zero, fmt.Errorf("create runtime request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, zero, fmt.Errorf("dispatch to runtime: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return nil, zero, fmt.Errorf("runtime returned %d", resp.StatusCode)
	}

	// Wait for the callback.
	select {
	case payload := <-ch:
		m := payload.Metrics
		metrics := types.StepMetrics{
			DurationMS:   m.DurationMS,
			TokensIn:     m.TokensIn,
			TokensOut:    m.TokensOut,
			CostUSD:      m.CostUSD,
			LLMCalls:     m.LLMCalls,
			ToolCalls:    m.ToolCalls,
			Reflected:    m.ReflectCalls > 0,
			ReflectCalls: m.ReflectCalls,
		}
		if payload.Status == "failed" {
			return nil, metrics, fmt.Errorf("agent step failed: %s", payload.Error)
		}
		return payload.Output, metrics, nil
	case <-ctx.Done():
		return nil, zero, ctx.Err()
	}
}
