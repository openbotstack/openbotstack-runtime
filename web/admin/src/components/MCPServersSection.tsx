import { useState, useEffect, useCallback } from 'react'
import { apiCall } from '../lib/api'
import { ConfirmDialog } from './Dialog'

interface MCPServerStatus {
  id: string
  name: string
  transport: string
  status: string // "connected" | "disconnected" | "error"
  tool_count: number
  error?: string
}

interface MCPTool {
  name: string
  description: string
}

interface ServerAuth {
  type: string // "bearer" | "api_key" | "custom" | "none"
  token?: string
  header?: string
  headers?: Record<string, string>
  env_auth?: Record<string, string>
}

interface ServerConfig {
  id?: string
  name: string
  transport: string
  url?: string
  command?: string
  args?: string[]
  env?: Record<string, string>
  auth?: ServerAuth
  enabled?: boolean
}

const TRANSPORT_OPTIONS = [
  { id: 'sse', label: 'SSE (Server-Sent Events)' },
  { id: 'stdio', label: 'Stdio' },
  { id: 'streamable-http', label: 'Streamable HTTP' },
]

export function MCPServersSection() {
  const [servers, setServers] = useState<MCPServerStatus[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  // Add form state
  const [showAdd, setShowAdd] = useState(false)
  const [formName, setFormName] = useState('')
  const [formTransport, setFormTransport] = useState('sse')
  const [formURL, setFormURL] = useState('')
  const [formCommand, setFormCommand] = useState('')
  const [formArgs, setFormArgs] = useState('')
  const [formAuthType, setFormAuthType] = useState('none')
  const [formAuthToken, setFormAuthToken] = useState('')
  const [formAuthHeader, setFormAuthHeader] = useState('')
  const [formCustomHeaders, setFormCustomHeaders] = useState<{ key: string; value: string }[]>([])
  const [formEnvAuth, setFormEnvAuth] = useState<{ key: string; value: string }[]>([])
  const [saving, setSaving] = useState(false)

  // Delete confirmation
  const [deleteTarget, setDeleteTarget] = useState<MCPServerStatus | null>(null)

  // Expanded row: show tools
  const [expanded, setExpanded] = useState<string | null>(null)
  const [tools, setTools] = useState<MCPTool[]>([])
  const [toolsLoading, setToolsLoading] = useState(false)

  // Reconnecting
  const [reconnecting, setReconnecting] = useState<string | null>(null)

  const fetchServers = useCallback(async () => {
    try {
      const data = await apiCall<MCPServerStatus[]>('/v1/admin/mcp/servers')
      setServers(Array.isArray(data) ? data : [])
      setError('')
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchServers() }, [fetchServers])

  const statusBadgeClass = (status: string) => {
    switch (status) {
      case 'connected': return 'status-active'
      case 'disconnected': return 'status-revoked'
      case 'error': return 'status-error'
      default: return ''
    }
  }

  const statusLabel = (status: string) => {
    switch (status) {
      case 'connected': return 'Connected'
      case 'disconnected': return 'Disconnected'
      case 'error': return 'Error'
      default: return status
    }
  }

  const transportBadgeClass = (transport: string) => {
    switch (transport) {
      case 'sse': return 'type-declarative'
      case 'stdio': return 'type-deterministic'
      case 'streamable-http': return 'type-llm'
      default: return 'type-other'
    }
  }

  const resetForm = () => {
    setFormName('')
    setFormURL('')
    setFormCommand('')
    setFormArgs('')
    setFormAuthType('none')
    setFormAuthToken('')
    setFormAuthHeader('')
    setFormCustomHeaders([])
    setFormEnvAuth([])
  }

  const buildAuth = (): ServerAuth | undefined => {
    if (formAuthType === 'none') return undefined
    const auth: ServerAuth = { type: formAuthType }
    if (formAuthType === 'bearer') {
      if (!formAuthToken.trim()) return undefined
      auth.token = formAuthToken.trim()
    } else if (formAuthType === 'api_key') {
      if (!formAuthToken.trim()) return undefined
      auth.token = formAuthToken.trim()
      if (formAuthHeader.trim()) auth.header = formAuthHeader.trim()
    } else if (formAuthType === 'custom') {
      const headers: Record<string, string> = {}
      for (const { key, value } of formCustomHeaders) {
        if (key.trim()) headers[key.trim()] = value
      }
      if (Object.keys(headers).length > 0) auth.headers = headers
      const envAuth: Record<string, string> = {}
      for (const { key, value } of formEnvAuth) {
        if (key.trim()) envAuth[key.trim()] = value
      }
      if (Object.keys(envAuth).length > 0) auth.env_auth = envAuth
    }
    return auth
  }

  const handleAdd = async () => {
    if (!formName.trim()) return
    setSaving(true)
    setError('')
    try {
      const body: ServerConfig = {
        id: formName.trim().toLowerCase().replace(/[^a-z0-9-]/g, '-').replace(/-+/g, '-'),
        name: formName.trim(),
        transport: formTransport,
        enabled: false,
      }
      if (formTransport === 'sse' || formTransport === 'streamable-http') {
        body.url = formURL.trim()
      }
      if (formTransport === 'stdio') {
        body.command = formCommand.trim()
        if (formArgs.trim()) {
          body.args = formArgs.trim().split(/\s+/)
        }
      }
      const auth = buildAuth()
      if (auth) body.auth = auth
      await apiCall('/v1/admin/mcp/servers', {
        method: 'POST',
        body: JSON.stringify(body),
      })
      setShowAdd(false)
      resetForm()
      await fetchServers()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setSaving(false)
    }
  }

  const handleDelete = async () => {
    if (!deleteTarget) return
    setError('')
    try {
      await apiCall(`/v1/admin/mcp/servers/${deleteTarget.id}`, { method: 'DELETE' })
      setDeleteTarget(null)
      if (expanded === deleteTarget.id) {
        setExpanded(null)
        setTools([])
      }
      await fetchServers()
    } catch (e) {
      setError((e as Error).message)
    }
  }

  const handleReconnect = async (id: string) => {
    setReconnecting(id)
    setError('')
    try {
      await apiCall(`/v1/admin/mcp/servers/${id}/reconnect`, { method: 'POST' })
      await fetchServers()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setReconnecting(null)
    }
  }

  const toggleTools = async (serverId: string) => {
    if (expanded === serverId) {
      setExpanded(null)
      setTools([])
      return
    }
    setExpanded(serverId)
    setToolsLoading(true)
    try {
      const data = await apiCall<MCPTool[]>(`/v1/admin/mcp/servers/${serverId}/tools`)
      setTools(Array.isArray(data) ? data : [])
    } catch {
      setTools([])
    } finally {
      setToolsLoading(false)
    }
  }

  if (loading) return <div className="loading">Loading MCP servers...</div>

  return (
    <div className="admin-section">
      <div className="section-header">
        <h2>MCP Servers ({servers.length})</h2>
        <div>
          <button className="btn-primary" onClick={() => setShowAdd(true)}>Add Server</button>
          <button className="btn-secondary" style={{ marginLeft: 8 }} onClick={fetchServers}>Refresh</button>
        </div>
      </div>

      {error && <div className="error">{error}</div>}

      {servers.length === 0 ? (
        <div className="empty">No MCP servers configured. Click "Add Server" to connect an MCP server.</div>
      ) : (
        <table className="data-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Transport</th>
              <th>Status</th>
              <th>Tools</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {servers.map(s => (
              <>
                <tr key={s.id} className={expanded === s.id ? 'expanded-row' : ''} style={{ cursor: 'pointer' }} onClick={() => toggleTools(s.id)}>
                  <td className="mono">{s.name}</td>
                  <td>
                    <span className={`type-badge ${transportBadgeClass(s.transport)}`}>{s.transport}</span>
                  </td>
                  <td>
                    <span className={`status-badge ${statusBadgeClass(s.status)}`}>
                      {statusLabel(s.status)}
                    </span>
                    {s.status === 'error' && s.error && (
                      <span className="error-hint" title={s.error}> (hover for details)</span>
                    )}
                  </td>
                  <td className="mono">{s.tool_count}</td>
                  <td className="actions" onClick={e => e.stopPropagation()}>
                    <button
                      className="btn-sm"
                      onClick={() => handleReconnect(s.id)}
                      disabled={reconnecting === s.id}
                    >
                      {reconnecting === s.id ? 'Reconnecting...' : 'Reconnect'}
                    </button>
                    <button className="btn-sm btn-danger" style={{ marginLeft: 4 }} onClick={() => setDeleteTarget(s)}>Delete</button>
                  </td>
                </tr>
                {expanded === s.id && (
                  <tr key={`${s.id}-tools`}>
                    <td colSpan={5} style={{ padding: 0 }}>
                      <div style={{ padding: '12px 16px', background: 'var(--bg-secondary, #f8f9fa)' }}>
                        <strong>Tools:</strong>
                        {toolsLoading ? (
                          <span style={{ marginLeft: 8 }}>Loading tools...</span>
                        ) : tools.length === 0 ? (
                          <span style={{ marginLeft: 8, color: '#888' }}>No tools available</span>
                        ) : (
                          <table className="data-table" style={{ marginTop: 8, marginBottom: 0 }}>
                            <thead>
                              <tr>
                                <th>Tool Name</th>
                                <th>Description</th>
                              </tr>
                            </thead>
                            <tbody>
                              {tools.map(t => (
                                <tr key={t.name}>
                                  <td className="mono">{t.name}</td>
                                  <td>{t.description || '-'}</td>
                                </tr>
                              ))}
                            </tbody>
                          </table>
                        )}
                      </div>
                    </td>
                  </tr>
                )}
              </>
            ))}
          </tbody>
        </table>
      )}

      {/* Add Server Dialog */}
      {showAdd && (
        <div className="dialog-overlay" onClick={() => setShowAdd(false)}>
          <div className="dialog" onClick={e => e.stopPropagation()} style={{ maxWidth: 520 }}>
            <div className="dialog-header">
              <h3>Add MCP Server</h3>
              <button className="dialog-close" onClick={() => setShowAdd(false)}>x</button>
            </div>
            <div className="dialog-body">
              <label>
                Server Name
                <input
                  value={formName}
                  onChange={e => setFormName(e.target.value)}
                  placeholder="e.g. my-mcp-server"
                  autoFocus
                />
              </label>

              <label>
                Transport
                <select value={formTransport} onChange={e => setFormTransport(e.target.value)}>
                  {TRANSPORT_OPTIONS.map(o => (
                    <option key={o.id} value={o.id}>{o.label}</option>
                  ))}
                </select>
              </label>

              {(formTransport === 'sse' || formTransport === 'streamable-http') && (
                <label>
                  URL
                  <input
                    value={formURL}
                    onChange={e => setFormURL(e.target.value)}
                    placeholder="http://localhost:3000/sse"
                  />
                </label>
              )}

              {formTransport === 'stdio' && (
                <>
                  <label>
                    Command
                    <input
                      value={formCommand}
                      onChange={e => setFormCommand(e.target.value)}
                      placeholder="e.g. npx"
                    />
                  </label>
                  <label>
                    Arguments (space-separated)
                    <input
                      value={formArgs}
                      onChange={e => setFormArgs(e.target.value)}
                      placeholder="e.g. -y @modelcontextprotocol/server-memory"
                    />
                  </label>
                </>
              )}

              <label>
                Authentication
                <select value={formAuthType} onChange={e => setFormAuthType(e.target.value)}>
                  <option value="none">None</option>
                  <option value="bearer">Bearer Token</option>
                  <option value="api_key">API Key</option>
                  <option value="custom">Custom Headers / Env Vars</option>
                </select>
              </label>

              {formAuthType === 'bearer' && (
                <label>
                  Bearer Token
                  <input
                    type="password"
                    value={formAuthToken}
                    onChange={e => setFormAuthToken(e.target.value)}
                    placeholder="Enter bearer token"
                  />
                </label>
              )}

              {formAuthType === 'api_key' && (
                <>
                  <label>
                    API Key Value
                    <input
                      type="password"
                      value={formAuthToken}
                      onChange={e => setFormAuthToken(e.target.value)}
                      placeholder="Enter API key"
                    />
                  </label>
                  <label>
                    Header Name (optional, default: X-API-Key)
                    <input
                      value={formAuthHeader}
                      onChange={e => setFormAuthHeader(e.target.value)}
                      placeholder="X-API-Key"
                    />
                  </label>
                </>
              )}

              {formAuthType === 'custom' && (
                <>
                  {(formTransport === 'sse' || formTransport === 'streamable-http') && (
                    <div style={{ marginBottom: 12 }}>
                      <div style={{ fontWeight: 600, marginBottom: 4, fontSize: 13 }}>Custom Headers</div>
                      {formCustomHeaders.map((h, i) => (
                        <div key={i} style={{ display: 'flex', gap: 4, marginBottom: 4 }}>
                          <input
                            style={{ flex: 1 }}
                            value={h.key}
                            onChange={e => {
                              const next = [...formCustomHeaders]
                              next[i] = { ...next[i], key: e.target.value }
                              setFormCustomHeaders(next)
                            }}
                            placeholder="Header name"
                          />
                          <input
                            style={{ flex: 1 }}
                            value={h.value}
                            onChange={e => {
                              const next = [...formCustomHeaders]
                              next[i] = { ...next[i], value: e.target.value }
                              setFormCustomHeaders(next)
                            }}
                            placeholder="Value"
                          />
                          <button
                            className="btn-sm btn-danger"
                            onClick={() => setFormCustomHeaders(formCustomHeaders.filter((_, j) => j !== i))}
                          >x</button>
                        </div>
                      ))}
                      <button
                        className="btn-sm"
                        onClick={() => setFormCustomHeaders([...formCustomHeaders, { key: '', value: '' }])}
                      >+ Add Header</button>
                    </div>
                  )}
                  <div style={{ marginBottom: 12 }}>
                    <div style={{ fontWeight: 600, marginBottom: 4, fontSize: 13 }}>
                      Environment Variables {formTransport === 'stdio' ? '(for subprocess)' : '(passed as env)'}
                    </div>
                    {formEnvAuth.map((h, i) => (
                      <div key={i} style={{ display: 'flex', gap: 4, marginBottom: 4 }}>
                        <input
                          style={{ flex: 1 }}
                          value={h.key}
                          onChange={e => {
                            const next = [...formEnvAuth]
                            next[i] = { ...next[i], key: e.target.value }
                            setFormEnvAuth(next)
                          }}
                          placeholder="ENV_VAR_NAME"
                        />
                        <input
                          style={{ flex: 1 }}
                          value={h.value}
                          onChange={e => {
                            const next = [...formEnvAuth]
                            next[i] = { ...next[i], value: e.target.value }
                            setFormEnvAuth(next)
                          }}
                          placeholder="Value"
                        />
                        <button
                          className="btn-sm btn-danger"
                          onClick={() => setFormEnvAuth(formEnvAuth.filter((_, j) => j !== i))}
                        >x</button>
                      </div>
                    ))}
                    <button
                      className="btn-sm"
                      onClick={() => setFormEnvAuth([...formEnvAuth, { key: '', value: '' }])}
                    >+ Add Env Var</button>
                  </div>
                </>
              )}

              <div className="dialog-actions">
                <button
                  className="btn-primary"
                  onClick={handleAdd}
                  disabled={saving || !formName.trim()}
                >
                  {saving ? 'Adding...' : 'Add Server'}
                </button>
                <button onClick={() => setShowAdd(false)}>Cancel</button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Delete Confirmation */}
      <ConfirmDialog
        open={!!deleteTarget}
        onClose={() => setDeleteTarget(null)}
        onConfirm={handleDelete}
        title="Delete MCP Server"
        message={`Are you sure you want to delete "${deleteTarget?.name}"? This will disconnect and remove the server.`}
        confirmLabel="Delete"
      />
    </div>
  )
}
