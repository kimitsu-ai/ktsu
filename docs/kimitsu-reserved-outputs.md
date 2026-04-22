# Kimitsu — Reserved Output Fields

## Architecture & Design Reference — v4

---

## Overview

Any agent may include reserved `ktsu_` prefixed fields in its output schema. The Kimitsu orchestrator has hardcoded, deterministic behavior for each field, allowing agents to signal control flow or security events directly through their typed output.

Reserved fields are evaluated by the orchestrator **before** the Air-Lock validation runs. If a fatal reserved condition (like an injection attempt) is triggered, the run terminates immediately, and the output is never passed to downstream steps.

> [!IMPORTANT]
> The `ktsu_` prefix is strictly reserved for Kimitsu. User-defined fields must not start with this prefix.

---

## Security Signals

### `ktsu_injection_attempt: boolean`
**Action: Fail the entire run immediately.**

Indicates the agent detected an attempt to hijack its instructions through untrusted data. The run is terminated with an `injection_attempt` error.

### `ktsu_untrusted_content: boolean`
**Action: Fail the current step.**

Indicates suspicious content was detected. The step fails, and downstream execution is halted, but it does not necessarily terminate the entire orchestrator process if handled.

---

## Quality & Flow Signals

### `ktsu_confidence: number` (0.0 – 1.0)
**Action: Compare against `confidence_threshold`.**

If the agent's self-reported confidence is below the step's `confidence_threshold` (defined in the workflow), the step fails with `confidence_below_threshold`.

### `ktsu_skip_reason: string`
**Action: Mark step as `skipped`.**

If set, the step is consider successful but skipped. Downstream steps depending on this ID are also skipped. This is a "clean exit" for when an agent determines there is no work to be done.

### `ktsu_needs_human: boolean`
**Action: Flag for human review.**

The run is halted with a `needs_human_review` status, signaling that a manual decision is required before proceeding.

---

## Observability Signals

### `ktsu_flags: string[]`
**Action: Log and record.**

Arbitrary labels (e.g., `["pii_detected", "high_value"]`) that are recorded in the run envelope for metrics and audit trails. They have no effect on execution flow.

### `ktsu_rationale: string`
**Action: Record agent reasoning.**

The agent's internal explanation for its decision. This is highly recommended for auditing and debugging complex classifications.

---

## Processing Order

The orchestrator processes these fields in a fixed hierarchy:
1. `ktsu_injection_attempt` (Fatal)
2. `ktsu_untrusted_content` (Step Failure)
3. `ktsu_low_quality` (Step Failure)
4. `ktsu_needs_human` (Human Review Gate)
5. `ktsu_confidence` (Threshold enforcement)
6. `ktsu_skip_reason` (Clean Skip)
7. `ktsu_flags` & `ktsu_rationale` (Observability)

---

*Revised April 2026*
