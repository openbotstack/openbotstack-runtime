import { useState, useEffect, useCallback } from 'react'
import { apiCall } from '../lib/api'

interface AuditEntry {
  id: string
  tenant_id: string
  user_id: string
  action: string
  resource: string
  outcome: string
  duration_ms: number
  timestamp: string
}

export function AuditSection() {
  const [entries, setEntries] = useState<AuditEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [filterTenant, setFilterTenant] = useState('')
  const [filterAction, setFilterAction] = useState('')

  const fetchAudit = useCallback(async () => {
    setLoading(true)
    try {
      const params = new URLSearchParams()
      if (filterTenant) params.set('tenant_id', filterTenant)
      if (filterAction) params.set('action', filterAction)
      params.set('limit', '100')
      const qs = params.toString()
      const path = `/v1/admin/audit${qs ? '?' + qs : ''}`
      const data = await apiCall<AuditEntry[]>(path)
      setEntries(Array.isArray(data) ? data : [])
      setError('')
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [filterTenant, filterAction])

  useEffect(() => { fetchAudit() }, [fetchAudit])

  return (
    <div className="admin-section">
      <div className="section-header">
        <h2>Audit Log ({entries.length})</h2>
        <button className="btn-primary" onClick={fetchAudit}>Refresh</button>
      </div>

      <div className="selector-row" style={{ display: 'flex', gap: '12px', marginBottom: '12px' }}>
        <label>Tenant ID:
          <input value={filterTenant} onChange={e => setFilterTenant(e.target.value)} placeholder="Filter by tenant..." style={{ marginLeft: '6px' }} />
        </label>
        <label>Action:
          <input value={filterAction} onChange={e => setFilterAction(e.target.value)} placeholder="Filter by action..." style={{ marginLeft: '6px' }} />
        </label>
      </div>

      {error && <div className="error">{error}</div>}
      {loading && <div className="loading">Loading audit log...</div>}

      {!loading && entries.length === 0 && (
        <div className="empty">No audit entries found</div>
      )}

      {!loading && entries.length > 0 && (
        <table className="data-table">
          <thead>
            <tr>
              <th>Timestamp</th>
              <th>Tenant</th>
              <th>User</th>
              <th>Action</th>
              <th>Resource</th>
              <th>Outcome</th>
              <th>Duration</th>
            </tr>
          </thead>
          <tbody>
            {entries.map(e => (
              <tr key={e.id}>
                <td className="mono">{e.timestamp ? new Date(e.timestamp).toLocaleString() : '-'}</td>
                <td className="mono">{e.tenant_id}</td>
                <td className="mono">{e.user_id}</td>
                <td>{e.action}</td>
                <td className="mono">{e.resource}</td>
                <td>
                  <span className={`status-badge status-${e.outcome === 'success' ? 'success' : 'failure'}`}>
                    {e.outcome}
                  </span>
                </td>
                <td>{e.duration_ms}ms</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
