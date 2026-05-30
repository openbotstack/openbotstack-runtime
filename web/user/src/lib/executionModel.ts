import {
  type ReasoningEvent,
  type ReasoningResponse,
  type AuditEntry,
  getConfidenceFromOutput,
} from './api'

// --- UI Model Types ---

export type PhaseType = 'tool_phase' | 'skill_phase' | 'llm_phase' | 'planning_phase'

export interface ViewModel {
  executionId: string
  planId?: string
  phases: PhaseModel[]
  metrics?: { total_steps: number; total_tool_calls: number; total_llm_turns: number; total_runtime_ms: number }
  stopCondition?: { stopped: boolean; reason: string; detail: string }
  rawText?: string
  auditTrail?: AuditEntry[]
  planReasoning?: string
  totalDurationMs: number
}

export interface PhaseModel {
  id: string
  type: PhaseType
  label: string
  icon: string
  durationMs: number
  status: string
  turns?: TurnModel[]
  children: StepModel[]
  color: string
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

// --- Phase Config ---

const PHASE_CONFIG: Record<string, { label: string; icon: string; color: string }> = {
  planning_phase: { label: 'Planning', icon: '🧠', color: '#a78bfa' },
  tool_phase:     { label: 'Tool',     icon: '🔧', color: '#527870' },
  skill_phase:    { label: 'Skill',    icon: '⚡', color: '#84a98c' },
  llm_phase:      { label: 'LLM',      icon: '💬', color: '#8b5cf6' },
}

// --- Build ViewModel from API Response ---

export function buildViewModel(response: ReasoningResponse): ViewModel {
  const tree = response.tree
  const phases: PhaseModel[] = []
  let totalDurationMs = response.metrics?.total_runtime_ms || 0

  // Extract plan reasoning from tree if available
  let planReasoning: string | undefined
  if (tree) {
    planReasoning = extractPlanReasoning(tree)
  }

  if (tree?.children) {
    for (const child of tree.children) {
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
        const cfg = PHASE_CONFIG.llm_phase
        phases.push({
          id: child.step_id || `llm-${phases.length}`,
          type: 'llm_phase',
          label: child.summary || cfg.label,
          icon: cfg.icon,
          durationMs: child.duration_ms || 0,
          status: child.status || 'completed',
          turns,
          children: [],
          color: cfg.color,
        })
      } else if (child.type === 'tool_call') {
        const stepType = child.step_type || 'tool'
        const phaseType = stepType === 'skill' ? 'skill_phase' : 'tool_phase'
        const cfg = PHASE_CONFIG[phaseType]
        phases.push({
          id: child.step_id || `step-${phases.length}`,
          type: phaseType,
          label: child.summary || cfg.label,
          icon: cfg.icon,
          durationMs: child.duration_ms || 0,
          status: child.status || 'completed',
          children: (child.children || []).map(c => eventToStepModel(c)),
          color: cfg.color,
        })
      }
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
    planReasoning,
    totalDurationMs,
  }
}

function extractPlanReasoning(tree: ReasoningEvent): string | undefined {
  // Try to find plan reasoning in the tree structure
  if (tree.type === 'plan' && tree.summary) {
    return tree.summary
  }
  for (const child of tree.children || []) {
    if (child.type === 'thought' && child.plan_text) {
      return child.plan_text
    }
  }
  return undefined
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
  if (ms < 100) return 'var(--success, #84a98c)'
  if (ms < 500) return 'var(--warning, #c9a54e)'
  return 'var(--error, #c7605b)'
}

export function truncateOutput(data: unknown, maxLen = 300): string {
  if (data == null) return ''
  const str = typeof data === 'string' ? data : JSON.stringify(data, null, 2)
  if (str.length <= maxLen) return str
  return str.slice(0, maxLen) + '...'
}

export function stepTypeBadge(stepType?: string): { label: string; color: string } {
  switch (stepType) {
    case 'tool': return { label: 'TOOL', color: '#527870' }
    case 'skill': return { label: 'SKILL', color: '#84a98c' }
    case 'llm': return { label: 'LLM', color: '#8b5cf6' }
    default: return { label: 'STEP', color: '#6b7280' }
  }
}
