import {
  type ReasoningEvent,
  type ReasoningResponse,
  type AuditEntry,
  getConfidenceFromOutput,
} from './api'

// --- UI Model Types ---

export interface ViewModel {
  executionId: string
  planId?: string
  phases: PhaseModel[]
  metrics?: { total_steps: number; total_tool_calls: number; total_llm_turns: number; total_runtime_ms: number }
  stopCondition?: { stopped: boolean; reason: string; detail: string }
  rawText?: string
  auditTrail?: AuditEntry[]
}

export interface PhaseModel {
  id: string
  type: 'tool_phase' | 'skill_phase' | 'llm_phase'
  label: string
  durationMs: number
  status: string
  turns?: TurnModel[]
  children: StepModel[]
}

export interface StepModel {
  id: string
  type: ReasoningEvent['type']
  stepType?: 'tool' | 'skill' | 'llm'
  summary: string
  input?: unknown
  output?: unknown
  durationMs: number
  status?: string
  error?: string
  confidence?: number
  turnNumber?: number
  planText?: string
  stopReason?: string
  children: StepModel[]
}

export interface TurnModel {
  turnNumber: number
  planText?: string
  actions: StepModel[]
  observations?: string[]
  stopReason?: string
  durationMs: number
}

// --- Build ViewModel from API Response ---

export function buildViewModel(response: ReasoningResponse): ViewModel {
  const tree = response.tree
  if (!tree) {
    return {
      executionId: response.execution_id,
      planId: response.plan_id,
      phases: [],
      metrics: response.metrics,
      stopCondition: response.stop_condition,
      rawText: response.text,
      auditTrail: response.debug?.audit_trail,
    }
  }

  const phases: PhaseModel[] = []

  for (const child of tree.children || []) {
    if (child.type === 'decision') continue

    if (child.type === 'thought' && child.step_type === 'llm') {
      const turns: TurnModel[] = []
      for (const turnChild of child.children || []) {
        if (turnChild.type === 'thought' && turnChild.turn_number != null) {
          const actions: StepModel[] = (turnChild.children || [])
            .filter(a => a.type === 'tool_call')
            .map(a => eventToStepModel(a))
          turns.push({
            turnNumber: turnChild.turn_number,
            planText: turnChild.plan_text,
            actions,
            observations: turnChild.observations,
            stopReason: turnChild.stop_reason,
            durationMs: turnChild.duration_ms || 0,
          })
        }
      }
      phases.push({
        id: child.step_id || `llm-${phases.length}`,
        type: 'llm_phase',
        label: child.summary,
        durationMs: child.duration_ms || 0,
        status: child.status || 'completed',
        turns,
        children: [],
      })
    } else if (child.type === 'tool_call') {
      const stepType = child.step_type || 'tool'
      phases.push({
        id: child.step_id || `step-${phases.length}`,
        type: stepType === 'skill' ? 'skill_phase' : 'tool_phase',
        label: child.summary,
        durationMs: child.duration_ms || 0,
        status: child.status || 'completed',
        children: (child.children || []).map(c => eventToStepModel(c)),
      })
    }
  }

  return {
    executionId: response.execution_id,
    planId: response.plan_id,
    phases,
    metrics: response.metrics,
    stopCondition: response.stop_condition,
    rawText: response.text,
    auditTrail: response.debug?.audit_trail,
  }
}

function eventToStepModel(event: ReasoningEvent): StepModel {
  return {
    id: event.step_id || Math.random().toString(36).slice(2),
    type: event.type,
    stepType: event.step_type,
    summary: event.summary,
    input: event.input,
    output: event.output,
    durationMs: event.duration_ms || 0,
    status: event.status,
    error: event.error,
    confidence: getConfidenceFromOutput(event.output) ?? undefined,
    turnNumber: event.turn_number,
    planText: event.plan_text,
    stopReason: event.stop_reason,
    children: (event.children || []).map(c => eventToStepModel(c)),
  }
}

// --- Formatting helpers ---

export function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`
  return `${(ms / 60000).toFixed(1)}m`
}

export function durationColor(ms: number): string {
  if (ms < 100) return 'var(--success)'
  if (ms < 500) return 'var(--warning)'
  return 'var(--error)'
}
