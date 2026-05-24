import { useState, useEffect, useCallback } from 'react'
import { apiCall } from '../lib/api'

interface HealthData {
  status: string
  metrics_count?: number
}

interface MetricsData {
  Counters?: { [key: string]: CounterEntry[] }
  Gauges?: { [key: string]: GaugeEntry[] }
  Histograms?: { [key: string]: HistogramEntry[] }
}

interface CounterEntry { Labels: { [key: string]: string }; Value: number }
interface GaugeEntry { Labels: { [key: string]: string }; Value: number }
interface HistogramEntry { Labels: { [key: string]: string }; Values: number[] }

export function TelemetryHealth() {
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [health, setHealth] = useState<HealthData | null>(null)
  const [metrics, setMetrics] = useState<MetricsData | null>(null)

  const loadData = useCallback(async () => {
    setLoading(true)
    try {
      const [h, m] = await Promise.all([
        apiCall<HealthData>('/v1/admin/telemetry/health'),
        apiCall<MetricsData>('/v1/admin/telemetry/metrics'),
      ])
      setHealth(h)
      setMetrics(m)
      setError('')
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { loadData() }, [loadData])

  if (loading) return <div className="loading">Loading health...</div>

  const counterEntries = metrics?.Counters
  const gaugeEntries = metrics?.Gauges
  const histogramEntries = metrics?.Histograms

  return (
    <div className="admin-section">
      <div className="section-header">
        <h2>System Health</h2>
        <button className="btn-primary" onClick={loadData}>Refresh</button>
      </div>

      {error && <div className="error">{error}</div>}

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: 12, marginBottom: 24 }}>
        <div className="health-card">
          <div className="health-card-title">Status</div>
          <div className={`health-card-value ${health?.status === 'healthy' ? 'health-ok' : 'health-error'}`}>
            {health?.status || 'unknown'}
          </div>
        </div>
        <div className="health-card">
          <div className="health-card-title">Metric Families</div>
          <div className="health-card-value">{health?.metrics_count || 0}</div>
        </div>
        <div className="health-card">
          <div className="health-card-title">Counters</div>
          <div className="health-card-value">{counterEntries ? Object.keys(counterEntries).length : 0}</div>
        </div>
        <div className="health-card">
          <div className="health-card-title">Gauges</div>
          <div className="health-card-value">{gaugeEntries ? Object.keys(gaugeEntries).length : 0}</div>
        </div>
      </div>

      {counterEntries && Object.keys(counterEntries).length > 0 && (
        <div className="health-subsection">
          <h3>Counters</h3>
          <table className="data-table">
            <thead><tr><th>Name</th><th>Labels</th><th>Value</th></tr></thead>
            <tbody>
              {Object.entries(counterEntries).map(([name, entries]) =>
                entries.map((e, i) => (
                  <tr key={`${name}-${i}`}>
                    <td className="mono">{name}</td>
                    <td>{e.Labels ? Object.entries(e.Labels).map(([k,v]) => `${k}=${v}`).join(', ') : '-'}</td>
                    <td>{e.Value}</td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      )}

      {gaugeEntries && Object.keys(gaugeEntries).length > 0 && (
        <div className="health-subsection">
          <h3>Gauges</h3>
          <table className="data-table">
            <thead><tr><th>Name</th><th>Labels</th><th>Value</th></tr></thead>
            <tbody>
              {Object.entries(gaugeEntries).map(([name, entries]) =>
                entries.map((e, i) => (
                  <tr key={`${name}-${i}`}>
                    <td className="mono">{name}</td>
                    <td>{e.Labels ? Object.entries(e.Labels).map(([k,v]) => `${k}=${v}`).join(', ') : '-'}</td>
                    <td>{e.Value.toFixed(1)}</td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      )}

      {histogramEntries && Object.keys(histogramEntries).length > 0 && (
        <div className="health-subsection">
          <h3>Histograms</h3>
          <table className="data-table">
            <thead><tr><th>Name</th><th>Labels</th><th>Sample Count</th></tr></thead>
            <tbody>
              {Object.entries(histogramEntries).map(([name, entries]) =>
                entries.map((e, i) => (
                  <tr key={`${name}-${i}`}>
                    <td className="mono">{name}</td>
                    <td>{e.Labels ? Object.entries(e.Labels).map(([k,v]) => `${k}=${v}`).join(', ') : '-'}</td>
                    <td>{e.Values.length}</td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
