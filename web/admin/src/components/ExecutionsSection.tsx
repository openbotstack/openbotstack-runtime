import { useState, useEffect, useCallback } from 'react'
import { apiCall } from '../lib/api'

interface Execution {
  execution_id: string
  session_id: string
  skill_id: string
  duration_ms: number
  status: string
  error: string
}

function truncate(s: string, maxLen: number): string {
  if (s.length <= maxLen) return s
  return s.slice(0, maxLen) + '...'
}

function statusBadgeClass(status: string): string {
  switch (status) {
    case 'success': return 'status-badge status-success'
    case 'timeout': return 'status-badge status-timeout'
    case 'failure':
    case 'error':
    default:
      return 'status-badge status-failure'
  }
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

export function ExecutionsSection() {
  const [executions, setExecutions] = useState<Execution[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const fetchExecutions = useCallback(async () => {
    setLoading(true)
    try {
      const data = await apiCall<Execution[]>('/v1/executions')
      setExecutions(Array.isArray(data) ? data : [])
      setError('')
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchExecutions() }, [fetchExecutions])

  if (loading) return <div className="loading">Loading executions...</div>

  return (
    <div className="admin-section">
      <div className="section-header">
        <h2>Executions ({executions.length})</h2>
        <button className="btn-primary" onClick={fetchExecutions}>Refresh</button>
      </div>

      {error && <div className="error">{error}</div>}

      {executions.length === 0 ? (
        <div className="empty">No executions found</div>
      ) : (
        <table className="data-table">
          <thead>
            <tr>
              <th>Execution ID</th>
              <th>Session</th>
              <th>Skill</th>
              <th>Duration</th>
              <th>Status</th>
              <th>Error</th>
            </tr>
          </thead>
          <tbody>
            {executions.map(ex => (
              <tr key={ex.execution_id}>
                <td className="mono" title={ex.execution_id}>{truncate(ex.execution_id, 12)}</td>
                <td className="mono" title={ex.session_id}>{truncate(ex.session_id, 12)}</td>
                <td>{ex.skill_id}</td>
                <td>{formatDuration(ex.duration_ms)}</td>
                <td><span className={statusBadgeClass(ex.status)}>{ex.status}</span></td>
                <td className="mono" style={{ color: ex.error ? 'var(--error)' : 'var(--text-secondary)', maxWidth: 200, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                  {ex.error || '-'}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
