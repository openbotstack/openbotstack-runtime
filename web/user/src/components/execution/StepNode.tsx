import type { StepModel } from '../../lib/executionModel'
import { formatDuration, durationColor } from '../../lib/executionModel'
import { Wrench, Eye, Brain, AlertTriangle } from 'lucide-react'

interface StepNodeProps {
  step: StepModel
  expanded: boolean
  selected: boolean
  onToggle: () => void
  onSelect: () => void
}

export function StepNode({ step, expanded, selected, onToggle, onSelect }: StepNodeProps) {
  const Icon = getStepIcon(step.type)
  const typeClass = `step-type-${step.type}`
  const hasError = !!step.error || step.status === 'failed'

  return (
    <div className={`step-node ${typeClass} ${selected ? 'selected' : ''} ${hasError ? 'has-error' : ''}`}>
      <div className="step-row" onClick={onSelect}>
        <button className="step-chevron" onClick={e => { e.stopPropagation(); onToggle() }}>
          {(step.children?.length || 0) > 0 ? (expanded ? '▾' : '▸') : '·'}
        </button>
        <span className="step-icon"><Icon size={14} /></span>
        <span className="step-summary">{step.summary}</span>
        {hasError && <AlertTriangle size={12} className="step-error-icon" />}
        <span className="step-duration" style={{ color: durationColor(step.durationMs) }}>
          {formatDuration(step.durationMs)}
        </span>
      </div>
      {expanded && step.children?.map((child: StepModel, i: number) => (
        <StepNode
          key={child.id || i}
          step={child}
          expanded={false}
          selected={false}
          onToggle={() => {}}
          onSelect={() => {}}
        />
      ))}
    </div>
  )
}

function getStepIcon(type: string) {
  switch (type) {
    case 'tool_call': return Wrench
    case 'observation': return Eye
    case 'thought': return Brain
    default: return Wrench
  }
}
