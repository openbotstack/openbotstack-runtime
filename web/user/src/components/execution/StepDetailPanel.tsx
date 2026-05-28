import { useMemo, type ReactNode } from 'react'
import type { StepModel } from '../../lib/executionModel'
import type { AuditEntry } from '../../lib/api'
import { X } from 'lucide-react'

interface StepDetailPanelProps {
  step: StepModel | null
  auditTrail?: AuditEntry[]
  debug: boolean
  onClose: () => void
}

export function StepDetailPanel({ step, auditTrail, debug, onClose }: StepDetailPanelProps) {
  if (!step) return null

  return (
    <div className="step-detail-panel" role="region" aria-label="Step details">
      <div className="detail-header">
        <span className="detail-title">{step.summary}</span>
        <button className="detail-close" onClick={onClose}><X size={14} /></button>
      </div>
      <div className="detail-body">
        {step.planText && (
          <DetailSection title="Reasoning">
            <pre className="detail-pre plan-text">{step.planText}</pre>
          </DetailSection>
        )}
        {step.input != null && (
          <DetailSection title="Input">
            <JsonBlock data={step.input} />
          </DetailSection>
        )}
        {step.output != null && (
          <DetailSection title="Output">
            <JsonBlock data={step.output} />
          </DetailSection>
        )}
        {step.error && (
          <DetailSection title="Error">
            <pre className="detail-pre error-text">{step.error}</pre>
          </DetailSection>
        )}
        {debug && auditTrail && step.stepType && (
          <DetailSection title="Audit Trail">
            <AuditTable entries={auditTrail} />
          </DetailSection>
        )}
        {step.children.length > 0 && (
          <DetailSection title="Children">
            {step.children.map((child: StepModel, i: number) => (
              <div key={i} className={`detail-child detail-child-${child.type}`}>
                <span className="child-type-badge">{child.type}</span>
                <span className="child-summary">{child.summary}</span>
                {child.output != null && <JsonBlock data={child.output} />}
              </div>
            ))}
          </DetailSection>
        )}
      </div>
    </div>
  )
}

function DetailSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="detail-section">
      <h4 className="detail-section-title">{title}</h4>
      {children}
    </div>
  )
}

function JsonBlock({ data }: { data: unknown }) {
  const formatted = useMemo(() => {
    try {
      return JSON.stringify(data, null, 2)
    } catch {
      return String(data)
    }
  }, [data])

  if (!formatted) return null
  return <pre className="detail-pre json-block">{formatted}</pre>
}

function AuditTable({ entries }: { entries: AuditEntry[] }) {
  if (entries.length === 0) return <div className="detail-empty">No audit entries</div>
  return (
    <table className="audit-table">
      <thead>
        <tr>
          <th>Step</th>
          <th>Status</th>
          <th>Duration</th>
          <th>Error</th>
        </tr>
      </thead>
      <tbody>
        {entries.map((e: AuditEntry, i: number) => (
          <tr key={i}>
            <td>{e.step_name || e.step_id}</td>
            <td className={`audit-status-${e.status}`}>{e.status}</td>
            <td>{e.duration_ms}ms</td>
            <td>{e.error || '—'}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}
