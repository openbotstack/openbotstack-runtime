import { useState, useEffect } from 'react'
import { apiCall } from '../lib/api'

interface VersionInfo {
  version: string
  commit: string
  branch: string
  buildTime: string
  goVersion: string
}

interface ReadyzResponse {
  status: string
  components: Record<string, string>
}

export function SystemInfoSection() {
  const [version, setVersion] = useState<VersionInfo | null>(null)
  const [health, setHealth] = useState<ReadyzResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    async function fetchAll() {
      setLoading(true)
      setError('')
      try {
        const [v, h] = await Promise.all([
          apiCall<VersionInfo>('/version'),
          apiCall<ReadyzResponse>('/readyz'),
        ])
        setVersion(v)
        setHealth(h)
      } catch (e) {
        setError((e as Error).message)
      } finally {
        setLoading(false)
      }
    }
    fetchAll()
  }, [])

  if (loading) return <div className="loading">Loading system info...</div>

  return (
    <div className="admin-section">
      {error && <div className="error">{error}</div>}

      <div className="system-cards">
        {version && (
          <div className="system-card">
            <h2>Version</h2>
            <table className="info-table">
              <tbody>
                <tr><td className="info-label">Version</td><td className="mono">{version.version}</td></tr>
                <tr><td className="info-label">Commit</td><td className="mono">{version.commit}</td></tr>
                <tr><td className="info-label">Branch</td><td className="mono">{version.branch}</td></tr>
                <tr><td className="info-label">Build Time</td><td className="mono">{version.buildTime ? new Date(version.buildTime).toLocaleString() : '-'}</td></tr>
                <tr><td className="info-label">Go Version</td><td className="mono">{version.goVersion}</td></tr>
              </tbody>
            </table>
          </div>
        )}

        {health && (
          <div className="system-card">
            <h2>Health</h2>
            <div className="health-overall">
              Overall: <span className={`status-badge ${health.status === 'ok' ? 'status-success' : 'status-failure'}`}>{health.status}</span>
            </div>
            <table className="info-table">
              <tbody>
                {Object.entries(health.components).map(([name, status]) => (
                  <tr key={name}>
                    <td className="info-label">{name}</td>
                    <td><span className={`status-badge ${status === 'ok' ? 'status-success' : 'status-failure'}`}>{status}</span></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  )
}
