import type { ViewModel } from '../../lib/executionModel'
import { formatDuration } from '../../lib/executionModel'

interface ExecutionOverviewProps {
  viewModel: ViewModel
}

export function ExecutionOverview({ viewModel }: ExecutionOverviewProps) {
  const { phases, metrics, stopCondition } = viewModel

  const toolCount = phases.filter(p => p.type === 'tool_phase').length
  const skillCount = phases.filter(p => p.type === 'skill_phase').length
  const llmPhases = phases.filter(p => p.type === 'llm_phase')
  const llmCount = llmPhases.length
  const totalTurns = llmPhases.reduce((sum, p) => sum + (p.turns?.length || 0), 0)

  const parts: string[] = []
  if (toolCount) parts.push(`${toolCount} tool`)
  if (skillCount) parts.push(`${skillCount} skill`)
  if (llmCount) parts.push(`${llmCount} LLM (${totalTurns} turns)`)

  const status = stopCondition?.stopped === false ? 'running' : (stopCondition?.reason === 'goal_achieved' ? 'completed' : 'stopped')
  const statusClass = status === 'completed' ? 'status-completed' : status === 'running' ? 'status-running' : 'status-stopped'

  return (
    <div className="execution-overview">
      <div className="stat-card">
        <span className="stat-label">Duration</span>
        <span className="stat-value">{formatDuration(metrics?.total_runtime_ms || 0)}</span>
      </div>
      <div className="stat-card">
        <span className="stat-label">Steps</span>
        <span className="stat-value">{parts.join(', ') || '—'}</span>
      </div>
      <div className="stat-card">
        <span className="stat-label">Status</span>
        <span className={`stat-value ${statusClass}`}>{status}</span>
      </div>
      {stopCondition?.reason && stopCondition.reason !== 'goal_achieved' && (
        <div className="stat-card">
          <span className="stat-label">Stop Reason</span>
          <span className="stat-value stat-reason">{stopCondition.reason}</span>
        </div>
      )}
    </div>
  )
}
