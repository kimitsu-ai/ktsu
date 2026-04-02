package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type heartbeatEntry struct {
	RunID  string `json:"run_id"`
	StepID string `json:"step_id"`
}

type heartbeatPayload struct {
	RuntimeID string           `json:"runtime_id"`
	Active    []heartbeatEntry `json:"active"`
}

func (r *Runtime) heartbeatLoop(ctx context.Context) {
	if r.cfg.OrchestratorURL == "" {
		return // no orchestrator configured; skip heartbeat
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.sendHeartbeat(ctx)
		}
	}
}

func (r *Runtime) sendHeartbeat(ctx context.Context) {
	var active []heartbeatEntry
	r.srv.activeInvocations.Range(func(key, _ any) bool {
		k, _ := key.(string)
		parts := strings.SplitN(k, "/", 2)
		if len(parts) == 2 {
			active = append(active, heartbeatEntry{RunID: parts[0], StepID: parts[1]})
		}
		return true
	})

	payload := heartbeatPayload{
		RuntimeID: "rt-1",
		Active:    active,
	}
	if payload.Active == nil {
		payload.Active = []heartbeatEntry{} // always send an array, never null
	}

	body, err := json.Marshal(payload)
	if err != nil {
		r.logf("heartbeat: marshal failed: %v", err)
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.cfg.OrchestratorURL+"/heartbeat", bytes.NewReader(body))
	if err != nil {
		r.logf("heartbeat: create request failed: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.logf("heartbeat: POST failed: %v", err)
		return
	}
	resp.Body.Close()
}
