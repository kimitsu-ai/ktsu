# Ktsu CLI Documentation Audit
**Date:** 2026-04-25

## Section 1: Priority Matrix

| # | Category | Issue | Affected File(s) | Recommended Fix | Priority |
|---|---|---|---|---|---|
| 1 | Accuracy | Reserved output field `ktsu_low_quality` mentioned in agent.md but not documented in reserved-outputs.md (the authoritative reference) | agent.md, reserved-outputs.md | Add `ktsu_low_quality` to the Processing Order section in reserved-outputs.md with trigger behavior; add a note in agent.md pointing to reserved-outputs.md | P0 |
| 2 | Accuracy | `env:VAR_NAME` is documented inconsistently — accepted by code in param declarations but violates the design invariant that agents must never access env vars directly. Docs should state this is unsupported everywhere in agent/server config. Blocked on code fix (Item #33). | tool-servers.md, concepts/variables.md, yaml-spec/agent.md | After code fix: update all docs to clearly state `env:VAR_NAME` is never valid in agent or server config. Values must arrive via the param chain. | P0 (blocked on #33) |
| 3 | Accuracy | pipeline-primitives.md shows Transform step with bare `step.parse.text` but the YAML spec shows transforms require JMESPath operations with `map:` / `filter:` keys — bare reference syntax is misleading | pipeline-primitives.md, yaml-spec/workflow.md | Revise the Transform example to show complete JMESPath operation syntax with explanatory text | P0 |
| 4 | Accuracy | Agent reflection documented as "one additional LLM call" in invariants.md but agent.md doesn't mention that reflection is triggered by `ktsu_confidence` below `confidence_threshold` — the relationship is implicit | agent.md, invariants.md | Add explicit section to agent.md: "Reflection is triggered when `ktsu_confidence` falls below a step's `confidence_threshold`. The reflect prompt runs once on the draft output." | P0 |
| 5 | Accuracy | Webhook `condition` field documented as "JMESPath condition" in workflow.md but no statement that conditions are evaluated by the Orchestrator, not by agents (per invariant #30) | yaml-spec/workflow.md | Add clarifying note: "Conditions are evaluated by the Orchestrator against the step's completed output. The agent does not evaluate conditions." | P0 |
| 6 | Accuracy | ~~configuration.md has a comment `kind: agent` but agent files have NO kind field~~ **FALSE POSITIVE** — configuration.md already correctly states agents are identified by filename/location and have no `kind` field. No change needed. | architecture/configuration.md | N/A — already correct | RESOLVED |
| 7 | Completeness | No documentation of `for_each` fanout despite invariant #31 stating fanout output is `{"results": [...]}` — it's a core feature | concepts/, yaml-spec/ | Create `docs/concepts/fanout.md` explaining: `for_each` syntax, iteration, automatic `results` wrapping, downstream addressing, and cost aggregation | P0 |
| 8 | Completeness | CLI reference only documents 3 commands (`start`, `invoke`, `validate`); missing all subcommands, flags, and exit codes | reference/cli.md | Expand cli.md with full command reference: all subcommands, flags, required/optional status, examples, exit codes | P0 |
| 9 | Quality | installation.md refers to "private repo" and private GitHub access, which conflicts with a public launch; confusing to new users | installation.md | Clarify at the top which options apply to public vs. private/enterprise deployments, or update to reflect open-source status | P0 |
| 10 | Completeness | No troubleshooting or debugging guide (failed runs, error codes, bootstrap failures) | docs/ | Create `docs/troubleshooting.md` with sections: bootstrap failures, step failures, schema validation errors, secret propagation errors, tool access denials | P1 |
| 11 | Completeness | `sub_agents` feature mentioned in agent.md but no documentation of how sub-agents work, visibility constraints, or comparison to Workflow steps | agent.md | Create `docs/concepts/sub-agents.md` explaining: invocation semantics, access inheritance (invariant #8), comparison to workflow steps | P1 |
| 12 | Completeness | Transform JMESPath operations not documented beyond one example | pipeline-primitives.md | Create `docs/reference/jmespath-transforms.md` with table of available operations (`map`, `filter`, etc.) and examples | P1 |
| 13 | Completeness | Version management not documented despite invariant #16 ("Version everything") — no guidance on semver, lockfile, or version constraints | docs/ | Create `docs/concepts/versioning.md` explaining semver enforcement, lockfile generation, and version constraint semantics | P1 |
| 14 | Completeness | `require_approval` HITL feature lacks workflow-level documentation of how approvals pause execution, approval APIs, or rejection handling | architecture/tool-servers.md, yaml-spec/agent.md | Add section to tool-servers.md: how approvals pause execution, how orchestrator routes requests, example rejection handling | P1 |
| 15 | Completeness | Secret propagation has no worked failure example — no way to debug a missing `secret: true` flag | concepts/variables.md | Add subsection "Secret Propagation Failures" with annotated example showing a missing `secret: true` flag and the resulting error | P1 |
| 16 | Completeness | Cost budgeting documented only as a YAML field; no guide on estimating costs, monitoring spend, or recovering from budget overruns | yaml-spec/workflow.md | Create `docs/cost-management.md` with: cost formula, per-model pricing, fanout aggregation, circuit breaker behavior, tuning strategies | P1 |
| 17 | Quality | runtime.md uses the "kernel" metaphor without clarifying the orchestrator is stateless and load-balanceable — implies it's a bottleneck | architecture/runtime.md | Add explicit statement: "The Orchestrator is stateless and load-balanceable. All state lives in the Store." | P1 |
| 18 | Quality | invariants.md (38 dense rules) has no introductory context; reads like internal specs, not developer documentation | concepts/invariants.md | Add 3-paragraph introduction: these are the contract between components, why they matter for security/reliability, how they guide design decisions | P1 |
| 19 | Quality | overview.md oscillates between design philosophy and technical architecture details (Air-Lock, Secret Propagation sections) that belong elsewhere | overview.md | Move "Air-Lock" section to `docs/architecture/air-lock.md`; move "Secret Propagation" to concepts/variables.md; keep overview.md as a 1-page conceptual intro | P1 |
| 20 | Terminology | "Server" used interchangeably for: tool server, marketplace server, local server, and gateway. "Step" used for both workflow steps and agent steps | Multiple files | Create `docs/glossary.md` defining: tool server, step, primitive, agent, workflow, root workflow, sub-workflow, orchestrator, pipeline | P1 |
| 21 | Terminology | "Pipeline" sometimes means workflow steps, sometimes the entire execution model; "orchestrator" sometimes means the component, sometimes the pattern | concepts/, architecture/ | Add clear definitions in glossary.md and reference them in overview.md | P1 |
| 22 | Narrative | Concepts section presents files in isolation with no connective examples tying them together; jump from variables → invariants is abrupt | concepts/ | Create `docs/concepts/README.md` with a learning path and a worked example showing all four concepts in one workflow | P1 |
| 23 | Narrative | Architecture section jumps between high-level and implementation details without orientation | architecture/ | Create `docs/architecture/README.md` explaining: runtime = how it executes, tool-servers = MCP contract, configuration = files you write | P1 |
| 24 | Completeness | No production deployment guide (container orchestration, scaling, state store selection, monitoring, cost controls) | docs/ | Create `docs/deployment.md` covering: production readiness, scaling, state persistence, observability, and cost controls | P2 |
| 25 | Structure | quickstart.md is tightly coupled to a complex hello-world example; new users need a simpler entry point | quickstart.md | Create `docs/hello-world-minimal.md` with a single-step agent workflow; link from quickstart.md | P2 |
| 26 | Structure | yaml-spec/index.md mixes TOC function with field tables; some redundancy with individual spec files | yaml-spec/index.md | Keep index.md as a pure TOC; confirm field tables aren't duplicated in individual spec files | P2 |

---

## Section 2: Recommended Site Map

### Existing Files

| File | Fate | Notes |
|---|---|---|
| `docs/overview.md` | **KEEP** | Move Air-Lock and Secret Propagation sections out (per #19) |
| `docs/installation.md` | **KEEP** | Clarify public vs. private deployment options (per #9) |
| `docs/quickstart.md` | **KEEP** | Add cross-link to minimal hello-world (per #25) |
| `docs/concepts/pipeline-primitives.md` | **KEEP** | Fix Transform example (per #3); add fanout reference (per #7) |
| `docs/concepts/variables.md` | **KEEP** | Absorb Secret Propagation from overview.md (per #19); add failure example (per #15) |
| `docs/concepts/invariants.md` | **KEEP** | Add introductory context (per #18) |
| `docs/concepts/reserved-outputs.md` | **KEEP** | Add `ktsu_low_quality` documentation (per #1) |
| `docs/architecture/runtime.md` | **KEEP** | Clarify statelessness (per #17) |
| `docs/architecture/tool-servers.md` | **KEEP** | Clarify variable syntax (per #2); add HITL section (per #14) |
| `docs/architecture/configuration.md` | **KEEP** | Clarify agent `kind` field (per #6) |
| `docs/reference/cli.md` | **EXPAND** | Document all commands and flags (per #8) |
| `docs/reference/yaml-spec/index.md` | **KEEP** | Serve as pure TOC only |
| `docs/reference/yaml-spec/workflow.md` | **KEEP** | Add condition evaluation clarification (per #5) |
| `docs/reference/yaml-spec/agent.md` | **KEEP** | Add reflection trigger explanation (per #4); fix `ktsu_low_quality` ref (per #1) |
| `docs/reference/yaml-spec/server.md` | **KEEP** | |
| `docs/reference/yaml-spec/servers.md` | **KEEP** | |
| `docs/reference/yaml-spec/gateway.md` | **KEEP** | |
| `docs/reference/yaml-spec/env.md` | **KEEP** | |

### New Files to Create

| File | Description | Priority | Word Target |
|---|---|---|---|
| `docs/concepts/README.md` | Learning path for the concepts section with worked example showing all four concepts in one workflow | P1 | 400 |
| `docs/concepts/fanout.md` | `for_each` fanout semantics, iteration, automatic `results` wrapping, downstream addressing, cost aggregation | P0 | 500 |
| `docs/concepts/sub-agents.md` | Sub-agent invocation, visibility constraints, access inheritance, comparison to Workflow steps | P1 | 600 |
| `docs/concepts/versioning.md` | Semver enforcement for agents, workflows, tool servers; lockfile generation; version constraints | P1 | 400 |
| `docs/architecture/README.md` | Navigation and context for the architecture section | P1 | 300 |
| `docs/architecture/air-lock.md` | Dedicated page for Air-Lock validation, schema enforcement, and error handling | P1 | 400 |
| `docs/reference/jmespath-transforms.md` | Reference table of Transform operations (`map`, `filter`, `reduce`, etc.) with examples | P1 | 500 |
| `docs/troubleshooting.md` | Debugging guide: bootstrap failures, step failures, schema errors, secret propagation errors, tool access denials | P1 | 800 |
| `docs/deployment.md` | Production readiness, scaling, state persistence, observability, cost controls | P2 | 1000 |
| `docs/cost-management.md` | Cost calculation, per-model pricing, fanout aggregation, budget circuit breaker, tuning | P1 | 600 |
| `docs/glossary.md` | Definitions: tool server, step, primitive, agent, workflow, root workflow, sub-workflow, orchestrator, pipeline | P1 | 300 |
| `docs/hello-world-minimal.md` | Minimal single-step agent example as gentler entry point before the full quickstart | P2 | 300 |

---

## Section 3: Code Accuracy Validation Amendments

Cross-referenced all accuracy claims against the Go source. The code is authoritative. The following items amend or extend the Priority Matrix above.

### Amendments to Existing Items

**Item #1 — `ktsu_low_quality`:** CONFIRMED exists in code (`pkg/types/reserved.go` line 18). All 8 `ktsu_` fields are defined. The gap is documentation-only: the field is real but missing from `reserved-outputs.md`. P0 stands.

**Item #2 — Variable syntax (CODE BUG, not a doc error):** The audit flagged docs saying `env:VAR_NAME` is "NO LONGER SUPPORTED." The validation agent then incorrectly marked this as a doc error because the code *does* accept `env:VAR_NAME` in param declarations. **Both are wrong.** The intended design is that agents must never access env vars directly — values must flow through the param chain (`env.yaml → workflow params → agent params → server params`). The `env:VAR_NAME` support in `ResolveAgentParams()` and `lookupEnvValue()` (`internal/config/params.go` lines 23–33, 126–162) is an **unintended implementation** that violates this invariant. The code needs to be fixed to remove `env:` support from param declaration resolution entirely. Until it is removed, the docs should explicitly state `env:VAR_NAME` is unsupported and will be rejected in a future version. This is a **P0 code fix** before public launch, not a doc fix. See Item #33 in new items below.

**Item #3 — Transform ops (CORRECTION):** The audit assumed `map:`/`filter:` as top-level keys. The code (`runner.go` lines 511–598) shows the actual supported ops are: `merge`, `filter`, `sort`, `map`, `flatten`, `deduplicate` — applied as a sequential `ops:` array. The Transform example in pipeline-primitives.md is still misleading, but the correct reference is to these 6 ops, not generic JMESPath. The reference doc to create (`docs/reference/jmespath-transforms.md`) should list and demonstrate all 6. P0 stands; revise the recommended fix.

**Item #6 — Agent `kind` field:** CONFIRMED by code — `AgentConfig` struct (`config/types.go` lines 159–171) has no `kind` field. P0 stands.

**Item #7 — Fanout output shape:** CONFIRMED. `runner.go` line 417 explicitly returns `map[string]interface{}{"results": outputs}`. P0 stands.

**Item #8 — CLI commands (EXPANDED):** The code reveals the CLI has significantly more commands than the 3 documented. Full list from `cmd/ktsu/main.go`:
- `start` → subcommands: `orchestrator`, `runtime`, `gateway`, `envelope`
- `validate`
- `invoke`
- `lock`
- `new` → subcommand: `project`
- `runs` → subcommand: `get`
- `workflow` → subcommand: `tree`
- `hub` (conditional on `KTSU_HUB_ENABLED=true`) → subcommands: `login`, `install`, `update`, `publish`, `search`

The CLI reference is missing `lock`, `new`, `runs get`, `workflow tree`, and the entire `hub` group. P0 stands; scope of the fix is larger than originally estimated.

---

### New Items from Code Validation

| # | Category | Issue | Affected File(s) | Recommended Fix | Priority |
|---|---|---|---|---|---|
| 27 | Accuracy | `cost_budget_usd` is documented as a circuit breaker that stops execution when exceeded, but the code only parses the field — no enforcement logic exists in `runner.go` or `loop.go` | yaml-spec/workflow.md, cost-management references | Mark `cost_budget_usd` as "planned / not yet enforced" in docs, or remove circuit breaker claim until implemented | P0 |
| 28 | Accuracy | `visibility` field on `WorkflowConfig` (documented as controlling sub-workflow access) is parsed but has no functional implementation — no code reads it at runtime | yaml-spec/workflow.md, concepts/ | Document as "reserved field, not yet enforced" or remove from schema docs until implementation is complete | P1 |
| 29 | Accuracy | `sub_agents` is documented as a runtime composition feature, but the code uses it only for validation/dependency checking at `ktsu validate` time — there is no runtime dispatch to sub-agents | yaml-spec/agent.md, concepts/ | Clarify: `sub_agents` declares agent dependencies for validation; it is not a runtime call mechanism | P0 |
| 30 | Accuracy | Air-Lock is described as running "at every step boundary" but code shows it runs *after* `ProcessReservedFields()` extracts `ktsu_` fields from output — it only validates the *remainder* of the output, not the full envelope | architecture/air-lock concept, reserved-outputs.md | Clarify the sequence: (1) agent returns output, (2) reserved fields extracted, (3) Air-Lock validates remaining fields | P1 |
| 31 | Accuracy | Secret propagation boot-time enforcement works in exactly the opposite direction from what docs imply: agent params must use `env:` syntax (not `{{ env.NAME }}`); server params must reference secret agent params. Docs conflate template substitution syntax with param declaration syntax throughout | concepts/variables.md, architecture/tool-servers.md | Rewrite variable/secret docs with a clear two-column model: "Param declaration syntax (`env:`, `param:`, backtick literal)" vs "Template substitution syntax (`{{ params.X }}`, `{{ step.ID.field }}`)" | P0 |
| 32 | Completeness | Transform ops are concrete and enumerable (merge, filter, sort, map, flatten, deduplicate) with specific sub-fields (`expr`, `field`, `order`). None of these are documented. | pipeline-primitives.md, reference/ | In `docs/reference/transforms.md`, document all 6 ops with their fields, semantics, and a worked example for each | P0 |
| 33 | Accuracy | **CODE BUG:** `env:VAR_NAME` is accepted in agent param declarations (`ResolveAgentParams`, `lookupEnvValue` in `internal/config/params.go`). This violates the core design invariant that agents must never access env vars directly — all values must flow through the param chain (`env.yaml → workflow → agent → server`). The code needs to be changed to reject `env:` references at the agent param layer. | `internal/config/params.go` lines 23–33, 126–162; `internal/config/types.go` | Remove `env:` prefix handling from `lookupEnvValue` calls inside `ResolveAgentParams` and `ResolveServerParams`. Replace with an error: "env: references are not permitted in agent params — declare the value in env.yaml and pass it through workflow params." Update docs to match. | P0 (code fix) |

---

## Summary by Dimension

**Accuracy (9 P0s, 3 P1s):** After code validation, the most critical accuracy issues are: (1) variable/secret syntax conflation — the docs treat `env:VAR_NAME` param-declaration syntax and `{{ params.NAME }}` template syntax as if they are alternatives when they operate in different scopes; (2) `sub_agents` is documented as a runtime feature but is validation-only; (3) `cost_budget_usd` is documented as an enforced circuit breaker but has no implementation; (4) Transform ops are concrete (merge, filter, sort, map, flatten, deduplicate) but documented with wrong/generic syntax; (5) `ktsu_low_quality` is real but undocumented in reserved-outputs.md. Additional: `visibility` field has no functional implementation, Air-Lock sequence is mis-ordered in docs.

**Completeness (7 P1s, 1 P2, 2 P0s):** Major gaps confirmed by code: fanout/`for_each` is fully implemented but entirely undocumented; CLI has 8 command groups (not 3); Transform has 6 concrete ops with no reference; HITL approval is fully implemented with a resume mechanism but only briefly mentioned; sub-agents is validation-only (docs overstate it). New gaps: the two-scope variable model (declaration vs. template) needs its own explanation.

**Narrative & Order (2 P1s):** Concepts section lacks connective tissue — files are isolated with no worked example linking them. Architecture section lacks an orienting README. The quickstart is too complex for absolute beginners.

**File Structure (2 P0s, 1 P2):** `kind: agent` comment in configuration.md is a launch blocker (confirmed by code — no such field exists). overview.md mixes philosophy with architecture details that belong elsewhere.

**Tone & Voice (2 P1s):** invariants.md reads as internal specs. overview.md oscillates between philosophy and technical details. Both need reframing for a public developer audience.

**Terminology (2 P1s):** "Server," "step," "pipeline," and "orchestrator" are overloaded terms. A glossary and a clearer two-scope model for variables would resolve most of the confusion without rewriting every file.
