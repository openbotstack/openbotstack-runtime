import { useState, useEffect, useCallback } from 'react'
import { apiCall } from '../lib/api'

interface SessionSummary {
  session_id: string
  tenant_id: string
  last_entry: string
  entry_count: number
  created_at: string
  updated_at: string
}

export function SessionsSection() {
  const [sessions, setSessions] = useState<SessionSummary[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const fetchSessions = useCallback(async () => {
    try {
      const data = await apiCall<SessionSummary[]>('/v1/admin/sessions')
      setSessions(Array.isArray(data) ? data : [])
      setError('')
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchSessions() }, [fetchSessions])

  const handleDelete = async (sessionId: string) => {
    if (!confirm(`Delete session "${sessionId.slice(0, 12)}..."?`)) return
    try {
      // Use admin session delete - query param approach since admin router doesn't have session-specific routes
      await apiCall(`/v1/admin/sessions/${sessionId}`, { method: 'DELETE' })
      await fetchSessions()
    } catch (e) {
      setError((e as Error).message)
    }
  }

  if (loading) return <div className="loading">Loading sessions...</div>

  return (
    <div className="admin-section">
      <div className="section-header">
        <h2>Sessions ({sessions.length})</h2>
        <button className="btn-primary" onClick={fetchSessions}>Refresh</button>
      </div>

      {error && <div className="error">{error}</div>}

      {sessions.length === 0 ? (
        <div className="empty">No sessions found</div>
      ) : (
        <table className="data-table">
          <thead>
            <tr>
              <th>Session ID</th>
              <th>Tenant</th>
              <th>Last Entry</th>
              <th>Messages</th>
              <th>Last Active</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {sessions.map(s => (
              <tr key={s.session_id}>
                <td className="mono">{s.session_id.slice(0, 12)}...</td>
                <td className="mono">{s.tenant_id}</td>
                <td className="skill-desc">{s.last_entry}</td>
                <td>{s.entry_count}</td>
                <td>{s.updated_at ? new Date(s.updated_at).toLocaleString() : '-'}</td>
                <td className="actions">
                  <button className="btn-sm btn-danger" onClick={() => handleDelete(s.session_id)}>Delete</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
