import type { PhaseModel, StepModel, TurnModel } from '../../lib/executionModel'
import { StepNode } from './StepNode'
import { TurnNode } from './TurnNode'

interface StepTreeProps {
  phases: PhaseModel[]
  expandedSteps: Set<string>
  selectedStepId: string | null
  onToggleStep: (id: string) => void
  onSelectStep: (step: StepModel) => void
}

export function StepTree({ phases, expandedSteps, selectedStepId, onToggleStep, onSelectStep }: StepTreeProps) {
  return (
    <div className="step-tree">
      {phases.map((phase: PhaseModel) => (
        <div key={phase.id} className="phase-group">
          <div className={`phase-header phase-${phase.type}`}>
            {phase.label}
          </div>
          {phase.type === 'llm_phase' && phase.turns ? (
            phase.turns.map((turn: TurnModel) => (
              <TurnNode
                key={turn.turnNumber}
                turn={turn}
                selectedStepId={selectedStepId}
                onSelectStep={onSelectStep}
              />
            ))
          ) : (
            phase.children.map((child: StepModel, i: number) => (
              <StepNode
                key={child.id || i}
                step={child}
                expanded={expandedSteps.has(child.id)}
                selected={child.id === selectedStepId}
                onToggle={() => onToggleStep(child.id)}
                onSelect={() => onSelectStep(child)}
              />
            ))
          )}
        </div>
      ))}
    </div>
  )
}
