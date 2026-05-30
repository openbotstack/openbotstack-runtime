import { useState, useEffect, useCallback, useRef } from 'react'
import { useExecutionViewer } from './ExecutionViewerContext'
import { getReasoning, type ReasoningResponse } from '../lib/api'
import { buildViewModel, type ViewModel, type StepModel, type TurnModel } from '../lib/executionModel'
import { X, Bug, Loader, ChevronDown, ChevronRight, Clock, Zap, Activity, Eye } from 'lucide-react'
import './execution/ExecutionViewer.css'

export function ExecutionDrawer() {
  const { executionId, closeViewer } = useExecutionViewer()
  const [viewModel, setViewModel] = useState<ViewModel | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [debug, setDebug] = useState(false)
  const [selectedStep, setSelectedStep] = useState<StepModel | null>(null)
  const [selectedPhaseId, setSelectedPhaseId] = useState<string | null>(null)
  const [expandedPhases, setExpandedPhases] = useState<Set<string>>(new Set())
  const fetchSeq = useRef(0)

  const open = executionId !== null

  useEffect(() => {
    if (!executionId) {
      setViewModel(null)
      setSelectedStep(null)
      return
    }

    const seq = ++fetchSeq.current
    setLoading(true)
    setError('')

    getReasoning(executionId, debug)
      .then((resp: ReasoningResponse) => {
        if (seq !== fetchSeq.current) return
        const vm = buildViewModel(resp)
        setViewModel(vm)
        // Auto-expand all phases
        setExpandedPhases(new Set(vm.phases.map(p => p.id)))
      })
      .catch(err => {
        if (seq !== fetchSeq.current) return
        setError(err.message || 'Failed to load execution data')
      })
      .finally(() => {
        if (seq === fetchSeq.current) setLoading(false)
      })
  }, [executionId, debug])

  const selectStep = useCallback((step: StepModel) => {
    setSelectedStep(prev => prev?.id === step.id ? null : step)
  }, [])

  const togglePhase = useCallback((id: string) => {
    setExpandedPhases(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [])

  const handleOverlayClick = useCallback((e: React.MouseEvent) => {
    if (e.target === e.currentTarget) closeViewer()
  }, [closeViewer])

  // Filter phases based on selection
  const visiblePhases = selectedPhaseId
    ? viewModel?.phases.filter(p => p.id === selectedPhaseId) || []
    : viewModel?.phases || []

  const toolCount = viewModel?.phases.filter(p => p.type === 'tool_phase').length || 0
  const skillCount = viewModel?.phases.filter(p => p.type === 'skill_phase').length || 0
  const llmCount = viewModel?.phases.filter(p => p.type === 'llm_phase').length || 0

  if (!open) return null

  return (
    <div className="execution-drawer-overlay" onClick={handleOverlayClick}>
      <div className="exec-drawer" role="dialog" aria-modal="true" aria-label="Execution details">
        {/* Header */}
        <div className="exec-header">
          <div className="exec-header-left">
            <Activity size={16} className="exec-header-icon" />
            <span className="exec-id">{executionId?.slice(0, 8)}</span>
            {viewModel?.stopCondition && (
              <span className={`exec-badge ${viewModel.stopCondition.reason === 'goal_achieved' ? 'badge-ok' : 'badge-warn'}`}>
                {viewModel.stopCondition.reason === 'goal_achieved' ? 'Goal Achieved' : viewModel.stopCondition.reason.replace(/_/g, ' ')}
              </span>
            )}
          </div>
          <div className="exec-header-right">
            <button className={`exec-debug-btn ${debug ? 'active' : ''}`} onClick={() => setDebug(!debug)}>
              <Bug size={13} />
              {debug ? 'Debug' : 'Debug'}
            </button>
            <button className="exec-close-btn" onClick={closeViewer}><X size={16} /></button>
          </div>
        </div>

        {/* Stats bar */}
        {viewModel && !loading && (
          <div className="exec-stats">
            <div className="stat"><Clock size={13} /><span>{formatDur(viewModel.totalDurationMs || viewModel.metrics?.total_runtime_ms || 0)}</span></div>
            {toolCount > 0 && <div className="stat stat-tool"><Zap size={13} /><span>{toolCount} tool</span></div>}
            {skillCount > 0 && <div className="stat stat-skill"><Zap size={13} /><span>{skillCount} skill</span></div>}
            {llmCount > 0 && <div className="stat stat-llm"><Zap size={13} /><span>{llmCount} LLM</span></div>}
          </div>
        )}

        {/* Phase filter bar */}
        {viewModel && viewModel.phases.length > 1 && (
          <div className="exec-phase-bar">
            <button className={`phase-btn ${!selectedPhaseId ? 'active' : ''}`} onClick={() => setSelectedPhaseId(null)}>
              All ({viewModel.phases.length})
            </button>
            {viewModel.phases.map(phase => (
              <button
                key={phase.id}
                className={`phase-btn ${selectedPhaseId === phase.id ? 'active' : ''}`}
                style={{ '--phase-color': phase.color } as React.CSSProperties}
                onClick={() => setSelectedPhaseId(selectedPhaseId === phase.id ? null : phase.id)}
              >
                {phase.icon} {phase.label}
                <span className="phase-btn-dur">{formatDur(phase.durationMs)}</span>
              </button>
            ))}
          </div>
        )}

        {/* Body */}
        <div className="exec-body">
          {loading && (
            <div className="exec-loading"><Loader size={20} className="spin" /><span>Loading...</span></div>
          )}
          {error && <div className="exec-error">{error}</div>}
          {viewModel && !loading && (
            <div className="exec-content">
              {/* Step tree */}
              <div className="exec-tree">
                {visiblePhases.map(phase => (
                  <div key={phase.id} className="exec-phase">
                    <div className="exec-phase-header" onClick={() => togglePhase(phase.id)}>
                      <span className="exec-phase-chevron">
                        {expandedPhases.has(phase.id) ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
                      </span>
                      <span className="exec-phase-icon">{phase.icon}</span>
                      <span className="exec-phase-label">{phase.label}</span>
                      <span className="exec-phase-dur">{formatDur(phase.durationMs)}</span>
                      {phase.status === 'failed' && <span className="exec-phase-err">failed</span>}
                    </div>

                    {expandedPhases.has(phase.id) && (
                      <div className="exec-phase-body">
                        {phase.type === 'llm_phase' && phase.turns ? (
                          phase.turns.map(turn => (
                            <TurnView key={turn.turnNumber} turn={turn} selectedStep={selectedStep} onSelectStep={selectStep} />
                          ))
                        ) : (
                          phase.children.map((child, i) => (
                            <StepRow key={child.id || i} step={child} selected={selectedStep} onSelect={selectStep} />
                          ))
                        )}
                        {/* Main step input/output placeholder for future use */}
                      </div>
                    )}
                  </div>
                ))}
              </div>

              {/* Detail panel */}
              {selectedStep && (
                <div className="exec-detail">
                  <div className="exec-detail-header">
                    <span className="exec-detail-title">{selectedStep.summary}</span>
                    <div className="exec-detail-meta">
                      {selectedStep.stepType && (
                        <span className={`exec-detail-type type-${selectedStep.stepType}`}>{selectedStep.stepType.toUpperCase()}</span>
                      )}
                      <span className="exec-detail-dur">{formatDur(selectedStep.durationMs)}</span>
                      <button className="exec-detail-close" onClick={() => setSelectedStep(null)}><X size={14} /></button>
                    </div>
                  </div>
                  <div className="exec-detail-body">
                    {selectedStep.input != null && (
                      <DetailBlock label="Input" data={selectedStep.input} />
                    )}
                    {selectedStep.output != null && (
                      <DetailBlock label="Output" data={selectedStep.output} />
                    )}
                    {selectedStep.error && (
                      <div className="exec-detail-err">
                        <strong>Error:</strong> {selectedStep.error}
                      </div>
                    )}
                    {selectedStep.children.length > 0 && selectedStep.children.map((child, i) => (
                      <div key={i} className="exec-detail-child">
                        <span className="child-badge">{child.type}</span>
                        <span className="child-summary">{child.summary}</span>
                        {child.output != null && <pre className="exec-pre">{JSON.stringify(child.output, null, 2).slice(0, 500)}</pre>}
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {/* Debug panel */}
              {debug && viewModel.auditTrail && viewModel.auditTrail.length > 0 && (
                <div className="exec-debug-panel">
                  <h4 className="exec-debug-title"><Eye size={13} /> Audit Trail</h4>
                  <div className="exec-debug-table-wrap">
                    <table className="exec-debug-table">
                      <thead>
                        <tr>
                          <th>Step</th>
                          <th>Type</th>
                          <th>Status</th>
                          <th>Duration</th>
                          <th>Error</th>
                        </tr>
                      </thead>
                      <tbody>
                        {viewModel.auditTrail.map((e, i) => (
                          <tr key={i}>
                            <td className="mono">{e.step_name || e.step_id?.slice(0, 8)}</td>
                            <td><span className={`type-pill type-${e.step_type}`}>{e.step_type}</span></td>
                            <td className={e.status === 'completed' ? 'ok' : 'err'}>{e.status}</td>
                            <td className="mono">{e.duration_ms}ms</td>
                            <td className="err-text">{e.error || '—'}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

// --- Sub-components ---

function StepRow({ step, selected, onSelect }: { step: StepModel; selected: StepModel | null; onSelect: (s: StepModel) => void }) {
  const isSelected = selected?.id === step.id
  return (
    <div className={`exec-step ${isSelected ? 'selected' : ''} ${step.error ? 'has-err' : ''}`} onClick={() => onSelect(step)}>
      <span className="exec-step-type">{step.type === 'tool_call' ? '🔧' : step.type === 'observation' ? '👁' : '·'}</span>
      <span className="exec-step-summary">{step.summary}</span>
      <span className="exec-step-dur" style={{ color: durColor(step.durationMs) }}>{formatDur(step.durationMs)}</span>
    </div>
  )
}

function TurnView({ turn, selectedStep, onSelectStep }: { turn: TurnModel; selectedStep: StepModel | null; onSelectStep: (s: StepModel) => void }) {
  return (
    <div className="exec-turn">
      <div className="exec-turn-header">
        <span className="exec-turn-label">Turn {turn.turnNumber}</span>
        <span className="exec-turn-dur">{formatDur(turn.durationMs)}</span>
        {turn.stopReason && <span className="exec-turn-stop">{turn.stopReason}</span>}
      </div>
      {turn.planText && (
        <div className="exec-turn-plan">
          <pre>{turn.planText}</pre>
        </div>
      )}
      {turn.observations && turn.observations.length > 0 && (
        <div className="exec-turn-obs">
          {turn.observations.map((obs, i) => (
            <div key={i} className="exec-turn-obs-item">{obs}</div>
          ))}
        </div>
      )}
      {turn.actions.map((action, i) => (
        <StepRow key={action.id || i} step={action} selected={selectedStep} onSelect={onSelectStep} />
      ))}
      {turn.actions.length === 0 && !turn.planText && (
        <div className="exec-turn-empty">No actions in this turn</div>
      )}
    </div>
  )
}

function DetailBlock({ label, data }: { label: string; data: unknown }) {
  const [collapsed, setCollapsed] = useState(false)
  const formatted = JSON.stringify(data, null, 2)
  if (!formatted || formatted === 'null') return null

  return (
    <div className="exec-detail-block">
      <div className="exec-detail-block-header" onClick={() => setCollapsed(!collapsed)}>
        <span className="exec-detail-block-label">{label}</span>
        <span className="exec-detail-block-toggle">{collapsed ? '▸' : '▾'}</span>
      </div>
      {!collapsed && <pre className="exec-pre">{formatted.length > 5000 ? formatted.slice(0, 5000) + '\n... (truncated)' : formatted}</pre>}
    </div>
  )
}

function formatDur(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`
  return `${(ms / 60000).toFixed(1)}m`
}

function durColor(ms: number): string {
  if (ms < 100) return '#84a98c'
  if (ms < 500) return '#c9a54e'
  return '#c7605b'
}
