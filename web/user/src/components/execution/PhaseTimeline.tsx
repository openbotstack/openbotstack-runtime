import type { PhaseModel } from '../../lib/executionModel'
import { formatDuration } from '../../lib/executionModel'

interface PhaseTimelineProps {
  phases: PhaseModel[]
  selectedPhaseId: string | null
  onSelectPhase: (id: string) => void
}

export function PhaseTimeline({ phases, selectedPhaseId, onSelectPhase }: PhaseTimelineProps) {
  if (phases.length === 0) return null

  const totalDuration = phases.reduce((sum, p) => sum + p.durationMs, 0)
  if (totalDuration === 0) return null

  return (
    <div className="phase-timeline">
      {phases.map(phase => {
        const widthPct = Math.max(8, (phase.durationMs / totalDuration) * 100)
        const isSelected = phase.id === selectedPhaseId
        return (
          <div
            key={phase.id}
            className={`phase-segment phase-${phase.type} ${isSelected ? 'selected' : ''}`}
            style={{ width: `${widthPct}%` }}
            onClick={() => onSelectPhase(phase.id)}
            title={`${phase.label} — ${formatDuration(phase.durationMs)}`}
          >
            <span className="phase-label">{phaseLabel(phase)}</span>
            <span className="phase-duration">{formatDuration(phase.durationMs)}</span>
          </div>
        )
      })}
    </div>
  )
}

function phaseLabel(phase: PhaseModel): string {
  switch (phase.type) {
    case 'llm_phase': {
      const turns = phase.turns?.length || 0
      return `LLM (${turns} turns)`
    }
    case 'skill_phase':
      return 'Skill'
    case 'tool_phase':
      return 'Tool'
    default:
      return phase.label
  }
}
