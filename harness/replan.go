package harness

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/openbotstack/openbotstack-core/audit"
	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/planner"
)

func (h *ExecutionHarness) attemptReplan(
	ctx context.Context,
	originalPlan *execution.ExecutionPlan,
	failedStep execution.ExecutionStep,
	stepResult *execution.StepResult,
	execErr error,
	result *HarnessResult,
	ec *execution.ExecutionContext,
	checkResult ReplanCheckResult,
) (newPlan *execution.ExecutionPlan, retErr error) {
	defer func() {
		if r := recover(); r != nil {
			retErr = fmt.Errorf("replan panicked: %v", r)
		}
	}()

	prevResults := h.buildPrevResults(result.StepResults)

	pCtx := ec.PlannerContext()
	if pCtx == nil {
		return nil, fmt.Errorf("replan: no planner context available")
	}

	rCtx := &planner.ReplanContext{
		OriginalPlan:     originalPlan,
		FailedStep:       failedStep,
		FailedStepResult: stepResult,
		Trigger:          checkResult.Trigger,
		PreviousResults:  prevResults,
		PlannerContext:   pCtx,
		ErrorMessage:     checkResult.Reason,
	}

	replanTimeout := 120 * time.Second
	replanCtx, cancel := context.WithTimeout(ctx, replanTimeout)
	defer cancel()

	newPlan, err := h.replanner.Replan(replanCtx, rCtx)
	if err != nil {
		return nil, fmt.Errorf("replan call failed: %w", err)
	}
	if newPlan == nil || len(newPlan.Steps) == 0 {
		return nil, fmt.Errorf("replan returned empty plan")
	}

	if newPlan.ID == "" {
		if err := newPlan.Validate(); err != nil {
			return nil, fmt.Errorf("replan validation failed: %w", err)
		}
	}

	return newPlan, nil
}

func (h *ExecutionHarness) emitReplanAudit(
	ctx context.Context,
	originalPlan *execution.ExecutionPlan,
	newPlan *execution.ExecutionPlan,
	failedStep execution.ExecutionStep,
	checkResult ReplanCheckResult,
	ec *execution.ExecutionContext,
) {
	if h.auditLogger == nil {
		return
	}

	event := audit.AuditEvent{
		ID:        uuid.NewString(),
		TenantID:  ec.TenantID,
		UserID:    ec.UserID,
		RequestID: ec.RequestID,
		Source:    audit.SourceReplan,
		Action:    "harness.replan",
		Resource:  originalPlan.ID,
		Outcome:   "replanned",
		Timestamp: time.Now().UTC(),
		StepID:    failedStep.StepID,
		StepName:  failedStep.Name,
		Status:    "replanned",
		TraceID:   ec.RequestID,
		Error:     checkResult.Reason,
		Metadata: map[string]string{
			"original_plan_id": originalPlan.ID,
			"new_plan_id":      newPlan.ID,
			"trigger":          string(checkResult.Trigger),
			"failed_step":      failedStep.Name,
		},
	}
	if err := h.auditLogger.Log(ctx, event); err != nil {
		h.emitProgress(ec, ProgressEvent{Type: "replan_audit_error", Content: err.Error()})
	}
}
