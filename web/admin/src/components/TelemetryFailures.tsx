import { useState, useEffect, useCallback } from 'react'
import { apiCall } from '../lib/api'

interface FailureGroup {
  [spanKind: string]: number
}

function kindLabel(k: string): string {
  switch (k) {
    case 'planner': return 'Planner'
    case 'tool_call': return 'Tool'
    case 'skill': return 'Skill'
    case 'wasm': return 'Wasm'
    case 'provider': return 'Provider'
    default: return k
  }
}

function statusColor(s: string): string {
  switch (s) {
    case 'error': return 'var(--error)'
    case 'timeout': return 'var(--warning)'
    case 'cancelled': return 'var(--warning)'
    default: return 'var(--text-muted)'
  }
}

export function TelemetryFailures() {
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [failures, setFailures] = useState<FailureGroup>({})

  const loadData = useCallback(async () => {
    setLoading(true)
    try {
      const data = await apiCall<FailureGroup>('/v1/admin/telemetry/failures')
      setFailures(typeof data === 'object' ? data : {})
      setError('')
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { loadData() }, [loadData])

  const entries = Object.entries(failures).sort((a, b) => b[1] - a[1])
  const totalCount = entries.reduce((sum, [, count]) => sum + count, 0)

  if (loading) return <div className="loading">Loading failures...</div>

  return (
    <div className="admin-section">
      <div className="section-header">
        <h2>Failure Center ({totalCount} total)</h2>
        <button className="btn-primary" onClick={loadData}>Refresh</button>
      </div>

      {error && <div className="error">{error}</div>}

      {entries.length === 0 ? (
        <div className="empty">No failures recorded</div>
      ) : (
        <table className="data-table">
          <thead>
            <tr>
              <th>Type</th>
              <th>Count</th>
            </tr>
          </thead>
          <tbody>
            {entries.map(([key, count]) => {
              const [kind, status] = key.split('.')
              return (
                <tr key={key}>
                  <td>
                    <span className="mono" style={{ color: 'var(--text-secondary)' }}>
                      {kindLabel(kind)}.{kindLabel(status)}
                    </span>
                  </td>
                  <td>
                    <span style={{
                      color: statusColor(status),
                      fontWeight: 600,
                      fontFamily: 'var(--font-mono)',
                      fontSize: 15,
                    }}>
                      {count}
                    </span>
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      )}
    </div>
  )
}
