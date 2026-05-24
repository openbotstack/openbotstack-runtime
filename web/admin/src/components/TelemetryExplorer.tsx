import { useState, useCallback } from 'react'
import { apiCall } from '../lib/api'

interface Span {
  trace_id: string
  span_id: string
  parent_span_id: string
  name: string
  kind: string
  start_time: string
  end_time: string
  status: string
  attributes: { [key: string]: string }
}

export function TelemetryExplorer() {
  const [searchId, setSearchId] = useState('')
  const [spans, setSpans] = useState<Span[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [expanded, setExpanded] = useState<Set<string>>(new Set())

  const handleSearch = useCallback(async () => {
    if (!searchId.trim()) return
    setLoading(true)
    setError('')
    try {
      const params = new URLSearchParams()
      params.set('execution_id', searchId.trim())
      params.set('trace_id', searchId.trim())
      const data = await apiCall<Span[]>(`/v1/admin/telemetry/spans?${params}`)
      setSpans(Array.isArray(data) ? data : [])
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [searchId])

  const toggleExpanded = (id: string) => {
    setExpanded(prev => {
      const next = new Set(prev)
      next.has(id) ? next.delete(id) : next.add(id)
      return next
    })
  }

  const statusClass = (s: string) => {
    switch (s) {
      case 'ok': return 'span-status-ok'
      case 'error': return 'span-status-error'
      case 'timeout': return 'span-status-timeout'
      default: return ''
    }
  }

  const renderSpans = (parentId: string | null, depth: number) => {
    const nodes = parentId === null
      ? spans.filter(s => !s.parent_span_id)
      : spans.filter(s => s.parent_span_id === parentId)

    return nodes.map(span => (
      <div key={span.span_id} style={{ marginLeft: depth > 0 ? 20 : 0 }}>
        <div
          className={`span-node ${statusClass(span.status)}`}
          onClick={() => toggleExpanded(span.span_id)}
        >
          <span className="span-meta">[{depth}]</span>{' '}
          <span className="span-name">{span.name}</span>{' '}
          <span className="span-meta">{span.kind}</span>{' '}
          <span className={`span-meta`} style={{ color: span.status === 'ok' ? 'var(--success)' : span.status === 'error' ? 'var(--error)' : 'var(--text-muted)' }}>
            {span.status}
          </span>
          {span.attributes.execution_id && (
            <span className="span-meta" style={{ marginLeft: 8 }}>
              {span.attributes.execution_id}
            </span>
          )}
        </div>
        {expanded.has(span.span_id) && renderSpans(span.span_id, depth + 1)}
      </div>
    ))
  }

  return (
    <div className="admin-section">
      <div className="section-header">
        <h2>Execution Explorer</h2>
      </div>

      <div className="explorer-search">
        <input
          type="text"
          placeholder="execution_id or trace_id"
          value={searchId}
          onChange={e => setSearchId(e.target.value)}
          onKeyDown={e => e.key === 'Enter' && handleSearch()}
        />
        <button className="btn-primary" onClick={handleSearch} disabled={loading}>
          {loading ? 'Searching...' : 'Search'}
        </button>
      </div>

      {error && <div className="error">{error}</div>}

      {spans.length === 0 && !loading ? (
        <div className="empty">Enter an execution_id or trace_id to explore spans</div>
      ) : (
        <div style={{ fontFamily: 'var(--font-mono)', fontSize: 13 }}>
          {renderSpans(null, 0)}
        </div>
      )}
    </div>
  )
}
