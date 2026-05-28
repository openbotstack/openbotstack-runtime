import { useState, useEffect, useCallback, useRef } from 'react'
import { useExecutionViewer } from './ExecutionViewerContext'
import { getReasoning, type ReasoningResponse } from '../lib/api'
import { buildViewModel, type ViewModel, type StepModel } from '../lib/executionModel'
import { ExecutionOverview } from './execution/ExecutionOverview'
import { PhaseTimeline } from './execution/PhaseTimeline'
import { StepTree } from './execution/StepTree'
import { StepDetailPanel } from './execution/StepDetailPanel'
import { X, Bug, Loader } from 'lucide-react'
import './execution/ExecutionViewer.css'

export function ExecutionDrawer() {
  const { executionId, closeViewer } = useExecutionViewer()
  const [viewModel, setViewModel] = useState<ViewModel | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [debug, setDebug] = useState(false)
  const [selectedStep, setSelectedStep] = useState<StepModel | null>(null)
  const [expandedSteps, setExpandedSteps] = useState<Set<string>>(new Set())
  const [selectedPhaseId, setSelectedPhaseId] = useState<string | null>(null)
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
        setViewModel(buildViewModel(resp))
      })
      .catch(err => {
        if (seq !== fetchSeq.current) return
        setError(err.message || 'Failed to load execution data')
      })
      .finally(() => {
        if (seq === fetchSeq.current) setLoading(false)
      })
  }, [executionId, debug])

  const toggleStep = useCallback((id: string) => {
    setExpandedSteps(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [])

  const selectStep = useCallback((step: StepModel) => {
    setSelectedStep(prev => prev?.id === step.id ? null : step)
  }, [])

  const handleOverlayClick = useCallback((e: React.MouseEvent) => {
    if (e.target === e.currentTarget) closeViewer()
  }, [closeViewer])

  if (!open) return null

  return (
    <div className="execution-drawer-overlay" onClick={handleOverlayClick}>
      <div className="execution-drawer" role="dialog" aria-modal="true" aria-label="Execution details">
        <div className="drawer-header">
          <div className="drawer-header-left">
            <span className="drawer-execution-id">{executionId?.slice(0, 12)}...</span>
            {viewModel?.stopCondition && (
              <span className={`drawer-status drawer-status-${viewModel.stopCondition.reason === 'goal_achieved' ? 'completed' : 'stopped'}`}>
                {viewModel.stopCondition.reason === 'goal_achieved' ? 'Completed' : viewModel.stopCondition.reason}
              </span>
            )}
          </div>
          <div className="drawer-header-right">
            <button
              className={`btn-debug ${debug ? 'active' : ''}`}
              onClick={() => setDebug(!debug)}
              title="Toggle debug mode"
            >
              <Bug size={14} />
              {debug ? 'Debug On' : 'Debug'}
            </button>
            <button className="btn-close-drawer" onClick={closeViewer}><X size={16} /></button>
          </div>
        </div>

        <div className="drawer-body">
          {loading && (
            <div className="drawer-loading">
              <Loader size={20} className="spin" />
              <span>Loading execution data...</span>
            </div>
          )}
          {error && (
            <div className="drawer-error">{error}</div>
          )}
          {viewModel && !loading && (
            <>
              <ExecutionOverview viewModel={viewModel} />
              <PhaseTimeline
                phases={viewModel.phases}
                selectedPhaseId={selectedPhaseId}
                onSelectPhase={setSelectedPhaseId}
              />
              <StepTree
                phases={viewModel.phases}
                expandedSteps={expandedSteps}
                selectedStepId={selectedStep?.id || null}
                onToggleStep={toggleStep}
                onSelectStep={selectStep}
              />
              <StepDetailPanel
                step={selectedStep}
                auditTrail={viewModel.auditTrail}
                debug={debug}
                onClose={() => setSelectedStep(null)}
              />
            </>
          )}
        </div>
      </div>
    </div>
  )
}
