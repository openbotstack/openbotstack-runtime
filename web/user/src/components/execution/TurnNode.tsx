import type { TurnModel, StepModel } from '../../lib/executionModel'
import { formatDuration } from '../../lib/executionModel'
import { StepNode } from './StepNode'
import { ChevronRight } from 'lucide-react'

interface TurnNodeProps {
  turn: TurnModel
  selectedStepId: string | null
  onSelectStep: (step: StepModel) => void
}

export function TurnNode({ turn, selectedStepId, onSelectStep }: TurnNodeProps) {
  const hasContent = turn.planText || turn.actions.length > 0 || (turn.observations && turn.observations.length > 0)

  return (
    <div className="turn-node">
      <div className="turn-header">
        <ChevronRight size={12} className="turn-chevron" />
        <span className="turn-label">Turn {turn.turnNumber}</span>
        <span className="turn-duration">{formatDuration(turn.durationMs)}</span>
        {turn.stopReason && <span className="turn-stop-reason">{turn.stopReason}</span>}
      </div>
      {turn.planText && (
        <div className="turn-plan-text">
          <pre>{turn.planText}</pre>
        </div>
      )}
      {turn.observations && turn.observations.length > 0 && (
        <div className="turn-observations">
          {turn.observations.map((obs, i) => (
            <div key={i} className="turn-observation-item">{obs}</div>
          ))}
        </div>
      )}
      <div className="turn-actions">
        {turn.actions.map((action: StepModel, i: number) => (
          <StepNode
            key={action.id || i}
            step={action}
            expanded={action.id === selectedStepId}
            selected={action.id === selectedStepId}
            onToggle={() => onSelectStep(action)}
            onSelect={() => onSelectStep(action)}
          />
        ))}
      </div>
      {!hasContent && (
        <div className="turn-empty">No actions in this turn</div>
      )}
    </div>
  )
}
