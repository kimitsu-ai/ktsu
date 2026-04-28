---
description: "How the Air-Lock validation gate works: validates agent output against the declared schema after every step, halts the run on failure, no unvalidated data reaches downstream steps."
---

# Air-Lock

---

## What Air-Lock Is

The Air-Lock is a validation gate built into the orchestrator. It runs automatically after every agent step completes, before the step's output is passed to any downstream step. No pipeline step receives unvalidated output from an upstream agent.

---

## What It Validates

The Air-Lock validates the agent's output against the `output.schema` declared in the agent's YAML file. Specifically, it checks that:

- All fields listed in `output.schema.required` are present in the output.
- All present fields conform to their declared types and constraints.

`ktsu_*` reserved fields (such as `ktsu_skip_reason`) are stripped from the output before Air-Lock runs. They are consumed by the orchestrator upstream and never appear in the payload that reaches the validator.

Air-Lock does not restrict extra fields beyond what the schema declares — only missing required fields and type violations cause failure.

---

## Why It Exists

Agent outputs are LLM-generated text parsed into structured data. Even well-prompted agents occasionally omit fields, return wrong types, or produce malformed JSON. Without a validation gate, these defects propagate silently — a downstream step reads a missing field as null, or a transform operates on unexpected input, and the failure surface widens.

Air-Lock enforces the contract between steps at the boundary where it can be caught precisely and cheaply: immediately after each step, before any data moves forward. This keeps pipeline failures local, traceable, and actionable.

---

## What Happens on Failure

When Air-Lock detects a validation error:

1. The step is marked **failed**.
2. The run **halts immediately** (fail-fast semantics — no other steps start or continue).
3. The error message identifies which required fields were missing or which values failed type validation.

The orchestrator surfaces this error in the run envelope under the failed step's result, alongside the raw output that triggered it. There is no automatic retry at the Air-Lock layer — retries are a concern of the Agent Runtime's reasoning loop, which runs before Air-Lock validation.

---

## YAML Example

### Agent Declaration

```yaml
# agents/classifier.agent.yaml
params:
  schema:
    type: object
    required: [text]
    properties:
      text: { type: string }

output:
  schema:
    type: object
    required: [category, confidence]
    properties:
      category:   { type: string, enum: [billing, technical, general] }
      confidence: { type: number, minimum: 0, maximum: 1 }
```

### Valid Output (passes Air-Lock)

```json
{
  "category": "billing",
  "confidence": 0.94
}
```

Both required fields are present and match their declared types. Air-Lock passes the output to downstream steps.

### Invalid Output (fails Air-Lock)

```json
{
  "category": "billing"
}
```

`confidence` is missing. Air-Lock halts the run and reports:

```
air-lock validation failed on step "classify":
  missing required field: confidence
```

The run envelope includes the raw output and the schema that was checked against it, so the failure is fully reproducible without re-running the pipeline.

---

*Revised April 2026*
