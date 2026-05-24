import { useState } from 'react'
import {
  type ReasoningEvent,
  type ReasoningResponse,
  formatStepSummary,
  isUncertain,
  getUncertaintyPhrase,
  hasConflictMarker,
  isSafeContent,
} from '../lib/api'

interface ReasoningViewProps {
  data: ReasoningResponse
  debug?: boolean
}

export function ReasoningView({ data, debug: initialDebug = false }: ReasoningViewProps) {
  const [debug, setDebug] = useState(initialDebug)
  const [expandedSteps, setExpandedSteps] = useState<Set<string>>(new Set())

  if (!data.tree) return null

  const toggleStep = (stepId: string) => {
    setExpandedSteps(prev => {
      const next = new Set(prev)
      if (next.has(stepId)) next.delete(stepId)
      else next.add(stepId)
      return next
    })
  }

  return (
    <div className="reasoning-view">
      <div className="reasoning-header">
        <span className="reasoning-title">Execution Reasoning</span>
        <div className="reasoning-controls">
          <button
            className={`btn-debug ${debug ? 'active' : ''}`}
            onClick={() => setDebug(!debug)}
          >
            {debug ? 'Hide Debug' : 'Show Debug'}
          </button>
        </div>
      </div>

      <div className="reasoning-tree">
        {data.tree.children?.map((child, i) => (
          <ReasoningStep
            key={child.step_id || i}
            event={child}
            index={i}
            expanded={expandedSteps.has(child.step_id || `step-${i}`)}
            onToggle={() => toggleStep(child.step_id || `step-${i}`)}
            debug={debug}
            auditTrail={data.debug?.audit_trail}
          />
        ))}
      </div>

      {debug && data.text && (
        <div className="reasoning-text">
          <h4>Text Summary</h4>
          <pre>{data.text}</pre>
        </div>
      )}
    </div>
  )
}

interface ReasoningStepProps {
  event: ReasoningEvent
  index: number
  expanded: boolean
  onToggle: () => void
  debug: boolean
  auditTrail?: { step_id: string; step_name: string; status: string; error?: string; duration_ms: number }[]
}

function ReasoningStep({ event, index, expanded, onToggle, debug, auditTrail }: ReasoningStepProps) {
  const icon = getEventIcon(event.type)
  const summary = formatStepSummary(event, index)
  const uncertain = event.output != null ? isUncertain(event.output) : false
  const uncertaintyPhrase = event.output != null ? getUncertaintyPhrase(event.output) : ''
  const hasConflict = event.output != null ? hasConflictMarker(event.output) : false

  const safeSummary = isSafeContent(summary) ? summary : 'Step content redacted (unsafe content detected)'
  const inputJSON = event.input != null ? safeJSON(event.input) : null
  const outputJSON = event.output != null ? safeJSON(event.output) : null

  return (
    <div className={`reasoning-step ${event.type} ${uncertain ? 'uncertain' : ''} ${hasConflict ? 'conflict' : ''}`}>
      <div className="step-header" onClick={onToggle}>
        <span className="step-icon">{icon}</span>
        <span className="step-summary">{safeSummary}</span>
        <span className="step-expand">{expanded ? '▾' : '▸'}</span>
      </div>

      {uncertain && uncertaintyPhrase && (
        <div className="uncertainty-warning">{uncertaintyPhrase}</div>
      )}

      {hasConflict && (
        <div className="conflict-warning">Data conflict detected</div>
      )}

      {expanded && (
        <div className="step-detail">
          {inputJSON && (
            <div className="step-input">
              <strong>Input:</strong>
              <pre>{inputJSON}</pre>
            </div>
          )}

          {event.children?.map((child, ci) => {
            const childOutputJSON = debug && child.output != null ? safeJSON(child.output) : null
            return (
              <div key={ci} className={`step-child ${child.type}`}>
                <span className="child-icon">{child.type === 'observation' ? '→' : '•'}</span>
                <span className="child-summary">{isSafeContent(child.summary) ? child.summary : 'Content redacted'}</span>
                {childOutputJSON && (
                  <pre className="child-output">{childOutputJSON}</pre>
                )}
              </div>
            )
          })}

          {debug && outputJSON && (
            <div className="step-output-debug">
              <strong>Output:</strong>
              <pre>{outputJSON}</pre>
            </div>
          )}

          {debug && auditTrail && event.step_id && (
            <AuditEntries stepId={event.step_id} trail={auditTrail} />
          )}
        </div>
      )}
    </div>
  )
}

function AuditEntries({ stepId, trail }: { stepId: string; trail: { step_id: string; step_name: string; status: string; error?: string; duration_ms: number }[] }) {
  const entries = trail.filter(e => e.step_id === stepId)
  if (entries.length === 0) return null

  return (
    <div className="audit-entries">
      <strong>Audit:</strong>
      {entries.map((e, i) => (
        <div key={i} className={`audit-entry ${e.status}`}>
          <span className="audit-status">{e.status}</span>
          {e.error && <span className="audit-error">{e.error}</span>}
          <span className="audit-duration">{e.duration_ms}ms</span>
        </div>
      ))}
    </div>
  )
}

function getEventIcon(type: string): string {
  switch (type) {
    case 'plan': return '📋'
    case 'thought': return '💭'
    case 'tool_call': return '🔧'
    case 'observation': return '👁'
    case 'decision': return '✅'
    default: return '•'
  }
}

function safeJSON(data: unknown): string | null {
  try {
    return JSON.stringify(data, null, 2)
  } catch {
    return null
  }
}
