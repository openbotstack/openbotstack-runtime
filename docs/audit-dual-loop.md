# Dual Bounded Harness Loop — Architecture Audit Report

**Date:** 2026-04-18
**Auditor:** Claude Code
**Scope:** openbotstack-core + openbotstack-runtime + openbotstack-apps
**Reference:** ADR-012 (Dual Bounded Loop Kernel)

---

## SECTION 1 — Loop Structure Verification

### Outer Loop

- **File:** `runtime/loop/outer_loop.go`
- **Entry point:** `DefaultOuterLoop.Run(ctx, tasks, ec)`
- **State machine:** `OuterState` type in `loop_state.go` (6 states)
- **Loop mechanism:** `for taskIdx, task := range tasks` (line 59)

### Inner Loop

- **File:** `runtime/loop/inner_loop.go`
- **Entry point:** `DefaultInnerLoop.Run(ctx, task, ec)`
- **State machine:** `InnerState` type in `loop_state.go` (7 states)
- **Loop mechanism:** `for { ... }` with break-on-stop (line 67)
- **Planner usage:** `l.planner.Plan(ctx, task.PlannerContext)` at PLAN state (line 86)
- **Tool usage:** `l.toolRunner.Execute(ctx, step.Name, step.Arguments, ec)` at ACT state (line 129)

### Verification

- ✅ Inner loop is called INSIDE outer loop: `outer_loop.go:89` → `l.innerLoop.Run(ctx, task, ec)`
- ✅ NOT merged into one loop: separate structs, separate files, separate interfaces
- ✅ NOT simulated via recursion: both use explicit `for` loops

### Call Graph

```
DefaultOuterLoop.Run()
  ├── for each task:
  │   ├── OuterStopEvaluator.Evaluate()      [stop check]
  │   ├── PolicyCheckpoint.Check()           [governance gate]
  │   ├── DefaultInnerLoop.Run()             [delegation]
  │   │   └── for {                          [inner loop]
  │   │       ├── planner.Plan()             [PLAN state]
  │   │       ├── for each plan.Step:
  │   │       │   └── toolRunner.Execute()   [ACT state]
  │   │       ├── InnerStopEvaluator.Evaluate() [CHECK_STOP]
  │   │       └── ContextCompactor.Compact() [NEXT_TURN]
  │   ├── Checkpoint.Save()                  [CHECKPOINT state]
  │   └── OuterStopEvaluator.Evaluate()      [stop check]
  └── return WorkflowResult
```

---

## SECTION 2 — State Machine Audit

### Outer Loop States

| Required State | Defined? | Constant |
|---------------|----------|----------|
| INIT | ✅ | `OuterInit = "init"` |
| TASK_SELECT | ✅ | `OuterTaskSelect = "task_select"` |
| TASK_EXECUTE | ✅ | `OuterTaskExecute = "task_execute"` |
| CHECKPOINT | ✅ | `OuterCheckpoint = "checkpoint"` |
| NEXT_TASK | ✅ | `OuterNextTask = "next_task"` |
| DONE | ✅ | `OuterDone = "done"` |

**Assessment:** Explicit string type `OuterState` with all 6 states as constants. ✅

### Inner Loop States

| Required State | Defined? | Constant |
|---------------|----------|----------|
| TURN_INIT | ✅ | `InnerTurnInit = "turn_init"` |
| PLAN | ✅ | `InnerPlan = "plan"` |
| ACT | ✅ | `InnerAct = "act"` |
| OBSERVE | ✅ | `InnerObserve = "observe"` |
| CHECK_STOP | ✅ | `InnerCheckStop = "check_stop"` |
| NEXT_TURN | ✅ | `InnerNextTurn = "next_turn"` |
| DONE | ✅ | `InnerDone = "done"` |

**Assessment:** Explicit string type `InnerState` with all 7 states as constants. ✅

**⚠️ Issue:** States are defined as constants but NOT used as runtime state tracking variables. The code uses comments like `// STATE: PLAN` (line 85, inner_loop.go) and `// STATE: TASK_EXECUTE` (line 88, outer_loop.go) but never actually stores or transitions a state variable. States are IMPLICIT in the procedural flow, not explicitly tracked.

**Severity:** Minor — states are correct but not observable at runtime.

---

## SECTION 3 — Bounding Guarantees (CRITICAL)

### Outer Loop Bounds

| Bound | Defined? | Default | Enforced? | Where |
|-------|---------|---------|-----------|-------|
| max_workflow_steps | ✅ | 5 | ✅ | `OuterStopEvaluator.Evaluate()` line 148 |
| max_session_runtime | ✅ | 30s | ✅ | `OuterStopEvaluator.Evaluate()` line 138 |

**Enforcement location:** `stop_conditions.go` — `OuterStopEvaluator.Evaluate(stepsElapsed, startTime, ctx)`

### Inner Loop Bounds

| Bound | Defined? | Default | Enforced? | Where |
|-------|---------|---------|-----------|-------|
| max_turns | ✅ | 8 | ✅ | `InnerStopEvaluator.Evaluate()` line 89 |
| max_tool_calls | ✅ | 20 | ✅ | `InnerStopEvaluator.Evaluate()` line 98 |
| max_turn_runtime | ✅ | 15s | ✅ | `InnerStopEvaluator.Evaluate()` line 79 |

**Enforcement location:** `stop_conditions.go` — `InnerStopEvaluator.Evaluate(turnsElapsed, toolCallsUsed, startTime, plannerStopped, ctx)`

### What Happens on Violation

- **Context cancellation:** Returns `StopReasonContextCanceled`, propagates error via `ctx.Err()`
- **Planner stopped:** Returns `StopReasonPlannerStopped`, clean completion
- **Max runtime:** Returns `StopReasonMaxRuntime`, clean completion
- **Max turns/steps:** Returns `StopReasonMaxTurns`/`StopReasonMaxWorkflowSteps`, clean completion
- **Max tool calls:** Returns `StopReasonMaxToolCalls`, clean completion
- **Inner loop safety limit hit:** Outer loop halts entire workflow (Defect 2 fix, `outer_loop.go:106-114`)

**Assessment:** All bounds enforced in CODE with explicit stop condition evaluators. ✅

---

## SECTION 4 — Execution Traceability

### Log Distinguishability

- ✅ Outer loop logs: `LogStep("task_N_start")`, `LogStep("task_N_end")` with `StepType: "workflow_step"`
- ✅ Inner loop tool logs: `LogStep(step.Name)` with `StepType: "tool"` or `StepType: "skill"`
- ✅ Context compaction logged: `LogStep("context_compaction")`

### ExecutionContext Tracking

- ❌ **ExecutionContext does NOT carry loop level** (outer/inner)
- ❌ **ExecutionContext does NOT carry step index**
- ❌ **ExecutionContext does NOT carry turn index**
- ✅ ExecutionContext carries: RequestID, AssistantID, SessionID, TenantID, UserID, StartedAt, Deadline

### Tool Call Traceability

- ⚠️ Tool calls logged with step name and type, but NOT linked back to turn number or workflow step index
- The `ExecutionLogRecord` has no `TurnIndex` or `WorkflowStepIndex` field

### Example Trace Structure (actual)

```json
{"step_name": "task_0_start", "step_type": "workflow_step", "status": "running"}
{"step_name": "fetch_patient", "step_type": "tool", "status": "success"}
{"step_name": "context_compaction", "step_type": "", "status": "failed"}
{"step_name": "task_0_end", "step_type": "workflow_step", "status": "success"}
```

**Assessment:** Partial — logs distinguish loop levels by naming convention, but lack structured indexing. ⚠️

---

## SECTION 5 — Control Plane vs Execution Plane

### Control Plane (core)

| Responsibility | In core? | File |
|---------------|---------|------|
| Planning | ✅ | `core/planner/execution_planner.go` |
| State machine (agent) | ✅ | `core/control/assistants/statemachine.go` |
| Policy | ✅ | `core/control/policy/` |
| Skill registry | ✅ | `core/registry/skills/` |
| Execution interfaces | ✅ | `core/execution/` |

### Execution Plane (runtime)

| Responsibility | In runtime? | File |
|---------------|-----------|------|
| Tool execution | ✅ | `runtime/toolrunner/` |
| Skill execution (Wasm) | ✅ | `runtime/sandbox/wasm/`, `runtime/executor/` |
| Inner loop | ✅ | `runtime/loop/inner_loop.go` |
| Outer loop | ✅ | `runtime/loop/outer_loop.go` |

### Leakage Check

- ⚠️ `InnerLoop` directly calls `planner.ExecutionPlanner.Plan()` — planner is a core interface called from runtime. This is by design (runtime executes plans, core defines the interface).
- ✅ No planning logic implemented in runtime
- ✅ No tool/skill execution logic in core
- ⚠️ Loop state types (`OuterState`, `InnerState`) are in runtime — could argue they belong in core, but ADR-012 says this is intentional (opt-in runtime feature)

**Assessment:** Clean separation. ✅

---

## SECTION 6 — Workflow Integration

### Workflow Package

- **File:** `apps/workflows/workflow.go`
- **Interface:** `Workflow` with `ID()`, `Name()`, `Steps()`, `Timeout()`, `Validate()`
- **Conversion:** `BuildPlan(w, input) → *execution.ExecutionPlan`
- **Example workflows:** `patient_summary.go`, `shift_handover.go`

### Integration with Outer Loop

- ❌ **Outer loop is NOT wired to workflows.** The `DefaultOuterLoop.Run()` takes `[]TaskInput` — these are NOT derived from `Workflow.Steps()`.
- ❌ **main.go does NOT reference the `loop` package at all.**
- ❌ **No adapter converts `Workflow` → `[]TaskInput` for the outer loop.**

**FLAG:** Workflows and the dual loop exist as independent, unconnected components. ⚠️

---

## SECTION 7 — Skill / Tool Execution Flow

### Inner Loop Execution

1. **PLAN:** `planner.Plan(ctx, plannerContext)` → returns `ExecutionPlan` with steps
2. **ACT:** Iterates `plan.Steps`, calls `toolRunner.Execute()` for each tool step
3. **OBSERVE:** Appends observations to `PlannerContext.MemoryContext`
4. **CHECK_STOP:** `InnerStopEvaluator.Evaluate()` checks all bounds
5. **NEXT_TURN:** `ContextCompactor.Compact()` truncates history

### Is There a Real Loop?

- ✅ **Yes.** `inner_loop.go:67` is `for { ... }` with explicit break conditions
- ✅ Each iteration re-plans via LLM, executes tools, observes results
- ✅ Observations feed back into planner context for next turn
- ✅ Context compaction prevents unbounded memory growth

**Assessment:** Real iterative loop with genuine multi-turn reasoning. ✅

---

## SECTION 8 — Failure Handling

### Timeout Handling

- ✅ `context.Context` cancellation checked at every loop iteration start
- ✅ Wall-clock timeout enforced via `stopEvaluator.Evaluate(startTime)`
- ✅ Defect 3 fix: partial turn data preserved on context cancellation (`inner_loop.go:104-110`)

### Tool Failure

- ✅ Tool errors break out of current plan execution (`inner_loop.go:143-145`)
- ✅ Tool errors propagated as `StopReasonError` with original error wrapped
- ✅ Tool failures logged via `ExecutionLogger.LogStep()`

### Partial Completion

- ✅ `TurnResult` captures partial data before error
- ✅ `TaskResult.TurnResults` accumulates all turns including failed ones
- ✅ `WorkflowResult.TaskResults` accumulates all tasks including failed ones

### State Recording

- ✅ Checkpoint interface (`Checkpoint.Save()`) persists state after each task
- ✅ Policy checkpoint (`PolicyCheckpoint.Check()`) gates between tasks
- ✅ Defect 2 fix: workflow halts if inner loop hits safety limit

**Assessment:** Comprehensive failure handling with structured recovery. ✅

---

## SECTION 9 — Global Rules Compliance

| Rule | Status | Evidence |
|------|--------|----------|
| No infinite loops | ✅ | Both loops have explicit bounds (counters + wall-clock timeouts) |
| Bounded execution | ✅ | 5 independent bounds enforced in `stop_conditions.go` |
| No background agents | ✅ | All execution is synchronous within `Run()` calls |
| Request-scoped execution | ✅ | `context.Context` scoped, no persistent goroutines |
| No self-modifying prompts | ✅ | Planner context only grows with observations, never mutates skill definitions |

**FLAG:** No violations detected. ✅

---

## SECTION 10 — Gap Report

### ✅ Correctly Implemented

1. **Structurally separated dual loop** — separate files, interfaces, types
2. **Explicit state enums** — all 13 states defined as typed constants
3. **All 5 bounds enforced in code** — not comments, real enforcement
4. **Priority-ordered stop conditions** — context > planner > runtime > turns > tools
5. **Context compaction** — prevents unbounded memory growth
6. **Checkpoint + Policy interfaces** — governance and state persistence
7. **Defect fixes** — all 3 documented defects fixed with tests
8. **Comprehensive test coverage** — 6 test files, integration tests, edge cases
9. **Clean control/execution plane separation**
10. **Full compliance with global rules** — no infinite loops, bounded, request-scoped

### ⚠️ Partially Implemented

1. **State tracking is implicit** — States are defined as constants but NOT tracked as runtime variables. Code uses comments (`// STATE: PLAN`) instead of explicit state transitions.
   - **Severity:** Minor
   - **File:** `runtime/loop/inner_loop.go`, `runtime/loop/outer_loop.go`
   - **Fix:** Add `currentState InnerState` / `currentState OuterState` field to loop structs, transition explicitly.

2. **ExecutionContext lacks loop-level context** — No `LoopLevel`, `StepIndex`, `TurnIndex` fields.
   - **Severity:** Minor
   - **File:** `core/execution/execution_context.go`
   - **Fix:** Add `LoopLevel string`, `OuterStepIndex int`, `InnerTurnIndex int` fields.

3. **ExecutionLogRecord lacks traceability** — No link from tool call to turn/workflow step.
   - **Severity:** Minor
   - **File:** `core/execution/execution_logger.go`
   - **Fix:** Add `TurnIndex int`, `WorkflowStepIndex int` fields.

### ❌ Missing or Incorrect

1. **Dual Loop is NOT integrated into the production agent pipeline.**
   - **Severity:** CRITICAL
   - **Current:** `DefaultAgent.HandleMessage()` in `core/control/agent/agent.go` uses a **single-pass pipeline**: `Plan → Execute`. No loop.
   - **The `loop/` package is an independent module** that is never imported or called from `main.go` or the agent.
   - **File:** `runtime/cmd/openbotstack/main.go` — zero references to `loop` package
   - **File:** `core/control/agent/agent.go` — single-pass: gatherSkills → loadHistory → planner.Plan → executor.ExecuteFromPlan
   - **Fix:** Either:
     - (a) Wire `DefaultOuterLoop`/`DefaultInnerLoop` into `DefaultAgent.HandleMessage()` as the execution path
     - (b) Create an alternative agent that uses the dual loop and register it in main.go

2. **Workflow package is NOT connected to Outer Loop.**
   - **Severity:** Major
   - **Current:** `apps/workflows/` produces `ExecutionPlan` via `BuildPlan()`, but there is no adapter to convert `Workflow.Steps()` → `[]TaskInput` for the outer loop.
   - **File:** `apps/workflows/workflow.go`
   - **Fix:** Create `WorkflowToTasks(w Workflow, input map[string]any) []TaskInput` adapter function.

---

## SECTION 11 — Final Verdict

### **PASS WITH RISKS**

### Explanation

The Dual Bounded Loop implementation is **architecturally correct and well-designed**:

- Two structurally separated loops ✅
- All bounds enforced in code ✅
- Explicit state definitions ✅
- Comprehensive test coverage ✅
- Clean separation of concerns ✅
- Full compliance with global rules ✅

However, the critical risk is:

> **The dual loop is a standalone, untested-in-production module.** The actual production agent (`DefaultAgent`) uses a **single-pass pipeline** (Plan → Execute). The `loop/` package exists but is never called. The dual loop has zero integration with the agent, the HTTP handler, or the workflow system.

This means:
- The production system does NOT currently have bounded iterative reasoning
- The production system does NOT currently have workflow orchestration with checkpoints
- All the excellent bounding, stop conditions, and defect fixes in `loop/` are dormant

**To achieve PASS (production ready), the dual loop must be wired into the agent pipeline as the execution engine, replacing or augmenting the current single-pass path.**
