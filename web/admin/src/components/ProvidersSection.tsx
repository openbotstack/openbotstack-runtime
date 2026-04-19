import { useState, useEffect, useCallback } from 'react'
import { apiCall } from '../lib/api'

interface Provider {
  id: string
  capabilities: string[]
}

export function ProvidersSection() {
  const [providers, setProviders] = useState<Provider[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const fetchProviders = useCallback(async () => {
    try {
      const data = await apiCall<Provider[]>('/v1/admin/providers')
      setProviders(Array.isArray(data) ? data : [])
      setError('')
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchProviders() }, [fetchProviders])

  if (loading) return <div className="loading">Loading providers...</div>

  return (
    <div className="admin-section">
      <div className="section-header">
        <h2>Model Providers ({providers.length})</h2>
        <button className="btn-primary" onClick={fetchProviders}>Refresh</button>
      </div>

      {error && <div className="error">{error}</div>}

      {providers.length === 0 ? (
        <div className="empty">No providers registered. Configure providers in config.yaml.</div>
      ) : (
        <table className="data-table">
          <thead>
            <tr>
              <th>Provider ID</th>
              <th>Capabilities</th>
            </tr>
          </thead>
          <tbody>
            {providers.map(p => (
              <tr key={p.id}>
                <td className="mono">{p.id}</td>
                <td>
                  {p.capabilities.map(c => (
                    <span key={c} className={`type-badge type-${badgeClass(c)}`}>{c}</span>
                  ))}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}

function badgeClass(cap: string): string {
  if (cap.includes('text_generation')) return 'declarative'
  if (cap.includes('tool_calling')) return 'llm'
  if (cap.includes('embedding')) return 'deterministic'
  return 'other'
}
