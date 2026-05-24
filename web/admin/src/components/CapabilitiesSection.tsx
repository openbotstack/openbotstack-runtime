import { useState, useEffect, useCallback } from 'react'
import { apiCall } from '../lib/api'

interface CapabilityDescriptor {
  id: string
  name: string
  description: string
  kind: string // "skill" | "mcp" | "native" | "external"
  source_id: string
}

export function CapabilitiesSection() {
  const [capabilities, setCapabilities] = useState<CapabilityDescriptor[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const kindBadgeClass = (kind: string) => {
    switch (kind) {
      case 'skill': return 'type-declarative'
      case 'mcp': return 'type-llm'
      case 'native': return 'type-deterministic'
      case 'external': return 'type-other'
      default: return 'type-other'
    }
  }

  const fetchCapabilities = useCallback(async () => {
    try {
      const data = await apiCall<CapabilityDescriptor[]>('/v1/admin/capabilities')
      setCapabilities(Array.isArray(data) ? data : [])
      setError('')
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchCapabilities() }, [fetchCapabilities])

  if (loading) return <div className="loading">Loading capabilities...</div>

  return (
    <div className="admin-section">
      <div className="section-header">
        <h2>Capabilities ({capabilities.length})</h2>
        <button className="btn-secondary" onClick={fetchCapabilities}>Refresh</button>
      </div>

      {error && <div className="error">{error}</div>}

      {capabilities.length === 0 ? (
        <div className="empty">No capabilities registered. Capabilities appear when skills or MCP tools are loaded.</div>
      ) : (
        <table className="data-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Name</th>
              <th>Kind</th>
              <th>Description</th>
              <th>Source</th>
            </tr>
          </thead>
          <tbody>
            {capabilities.map(c => (
              <tr key={c.id}>
                <td className="mono">{c.id}</td>
                <td>{c.name}</td>
                <td>
                  <span className={`type-badge ${kindBadgeClass(c.kind)}`}>{c.kind}</span>
                </td>
                <td className="skill-desc">{c.description || '-'}</td>
                <td className="mono">{c.source_id}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
