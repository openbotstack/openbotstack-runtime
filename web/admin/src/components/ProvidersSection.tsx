import { useState, useEffect, useCallback } from 'react'
import { apiCall } from '../lib/api'

interface ProviderConfigEntry {
  id: string
  provider: string
  name: string
  base_url: string
  api_key_set: boolean
  model: string
  is_default: boolean
}

interface ProviderConfigResponse {
  providers: ProviderConfigEntry[]
}

interface TestResult {
  success: boolean
  message: string
  latency_ms: number
}

const PROVIDER_OPTIONS = [
  { id: 'openai', label: 'OpenAI', defaultURL: 'https://api.openai.com/v1', defaultModel: 'gpt-4o' },
  { id: 'modelscope', label: 'ModelScope', defaultURL: 'https://api-inference.modelscope.cn/v1', defaultModel: 'qwen-plus' },
  { id: 'siliconflow', label: 'SiliconFlow', defaultURL: 'https://api.siliconflow.cn/v1', defaultModel: 'deepseek-ai/DeepSeek-V3' },
  { id: 'claude', label: 'Claude (via proxy)', defaultURL: 'https://api.anthropic.com/v1', defaultModel: 'claude-sonnet-4-20250514' },
]

export function ProvidersSection() {
  const [configs, setConfigs] = useState<ProviderConfigEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const [editing, setEditing] = useState<string | null>(null) // null | 'new' | config.id
  const [formProvider, setFormProvider] = useState('openai')
  const [formName, setFormName] = useState('')
  const [formBaseURL, setFormBaseURL] = useState('')
  const [formAPIKey, setFormAPIKey] = useState('')
  const [formModel, setFormModel] = useState('')
  const [formDefault, setFormDefault] = useState(false)
  const [saving, setSaving] = useState(false)
  const [deleting, setDeleting] = useState<string | null>(null)

  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<TestResult | null>(null)

  const fetchConfig = useCallback(async () => {
    try {
      const data = await apiCall<ProviderConfigResponse>('/v1/admin/providers/config')
      setConfigs(data.providers || [])
      setError('')
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchConfig() }, [fetchConfig])

  const startEdit = (cfg?: ProviderConfigEntry) => {
    if (cfg) {
      setEditing(cfg.id)
      setFormProvider(cfg.provider)
      setFormName(cfg.name || '')
      setFormBaseURL(cfg.base_url || '')
      setFormAPIKey('')
      setFormModel(cfg.model || '')
      setFormDefault(cfg.is_default || false)
    } else {
      const first = PROVIDER_OPTIONS[0]
      setEditing('new')
      setFormProvider(first.id)
      setFormName('')
      setFormBaseURL(first.defaultURL)
      setFormAPIKey('')
      setFormModel(first.defaultModel)
      setFormDefault(true)
    }
    setTestResult(null)
    setError('')
  }

  const handleProviderChange = (id: string) => {
    const opt = PROVIDER_OPTIONS.find(o => o.id === id)
    setFormProvider(id)
    if (opt) {
      setFormBaseURL(opt.defaultURL)
      setFormModel(opt.defaultModel)
    }
  }

  const handleTest = async () => {
    if (!formAPIKey.trim() && editing && editing !== 'new') {
      const existing = configs.find(c => c.id === editing)
      if (!existing?.api_key_set) return
    } else if (!formAPIKey.trim()) {
      return
    }
    setTesting(true)
    setTestResult(null)
    try {
      const result = await apiCall<TestResult>('/v1/admin/providers/test', {
        method: 'POST',
        body: JSON.stringify({
          provider: formProvider,
          base_url: formBaseURL.trim(),
          api_key: formAPIKey.trim(),
          model: formModel.trim(),
        }),
      })
      setTestResult(result)
    } catch (e) {
      setTestResult({ success: false, message: (e as Error).message, latency_ms: 0 })
    } finally {
      setTesting(false)
    }
  }

  const canTest = !testing && !!formModel.trim() && (
    !!formAPIKey.trim() ||
    (!!editing && editing !== 'new' && !!configs.find(c => c.id === editing)?.api_key_set)
  )

  const handleSave = async () => {
    if (!formModel.trim()) return
    setSaving(true)
    setError('')
    try {
      const body: Record<string, string> = {
        provider: formProvider,
        name: formName.trim() || formProvider,
        base_url: formBaseURL.trim(),
        api_key: formAPIKey.trim(),
        model: formModel.trim(),
        is_default: formDefault ? 'true' : 'false',
      }
      if (editing && editing !== 'new') {
        body.id = editing
      }
      await apiCall('/v1/admin/providers/config', {
        method: 'PUT',
        body: JSON.stringify(body),
      })
      setEditing(null)
      setFormAPIKey('')
      await fetchConfig()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setSaving(false)
    }
  }

  const handleDelete = async (cfg: ProviderConfigEntry) => {
    if (!confirm(`Remove "${cfg.name || cfg.id}"?`)) return
    setDeleting(cfg.id)
    try {
      await apiCall('/v1/admin/providers/config', {
        method: 'DELETE',
        body: JSON.stringify({ id: cfg.id }),
      })
      await fetchConfig()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setDeleting(null)
    }
  }

  if (loading) return <div className="loading">Loading providers...</div>

  const providerLabel = (p: string) => PROVIDER_OPTIONS.find(o => o.id === p)?.label || p

  return (
    <div className="admin-section">
      <div className="section-header">
        <h2>Model Providers ({configs.length})</h2>
        <div>
          <button className="btn-primary" onClick={() => startEdit()}>+ Configure Provider</button>
          <button className="btn-secondary" style={{ marginLeft: 8 }} onClick={fetchConfig}>Refresh</button>
        </div>
      </div>

      {error && <div className="error">{error}</div>}

      {configs.length === 0 ? (
        <div className="empty">
          No providers configured. Click "+ Configure Provider" to set up your first LLM provider.
        </div>
      ) : (
        <table className="data-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Driver</th>
              <th>Base URL</th>
              <th>Model</th>
              <th>API Key</th>
              <th>Default</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {configs.map(cfg => (
              <tr key={cfg.id}>
                <td className="mono" style={{ fontWeight: 600 }}>{cfg.name || cfg.id}</td>
                <td>
                  <span className="type-badge type-declarative">{providerLabel(cfg.provider)}</span>
                </td>
                <td className="mono" style={{ fontSize: '0.85em' }}>{cfg.base_url || '-'}</td>
                <td className="mono">{cfg.model || '-'}</td>
                <td>
                  <span className={`type-badge ${cfg.api_key_set ? 'status-active' : 'status-revoked'}`}>
                    {cfg.api_key_set ? 'Set' : 'Not Set'}
                  </span>
                </td>
                <td>
                  {cfg.is_default && <span className="type-badge status-active">Default</span>}
                </td>
                <td className="actions">
                  <button className="btn-sm" onClick={() => startEdit(cfg)}>Edit</button>
                  <button
                    className="btn-sm btn-danger"
                    onClick={() => handleDelete(cfg)}
                    disabled={deleting === cfg.id}
                    style={{ marginLeft: 4 }}
                  >
                    {deleting === cfg.id ? '...' : 'Delete'}
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {editing && (
        <div className="dialog-overlay">
          <div className="dialog" style={{ maxWidth: 520 }}>
            <div className="dialog-header">
              <h3>{editing === 'new' ? 'Configure Provider' : 'Edit Provider'}</h3>
              <button className="dialog-close" onClick={() => setEditing(null)}>x</button>
            </div>
            <div className="dialog-body">
              <label>
                Driver
                <select value={formProvider} onChange={e => handleProviderChange(e.target.value)}>
                  {PROVIDER_OPTIONS.map(o => (
                    <option key={o.id} value={o.id}>{o.label}</option>
                  ))}
                </select>
              </label>

              <label>
                Name
                <input
                  value={formName}
                  onChange={e => setFormName(e.target.value)}
                  placeholder="e.g. Production OpenAI, Local LLM"
                />
                <span className="hint">A display name for this configuration. Leave empty to use driver name.</span>
              </label>

              <label>
                Base URL
                <input
                  value={formBaseURL}
                  onChange={e => setFormBaseURL(e.target.value)}
                  placeholder="https://api.openai.com/v1"
                />
              </label>

              <label>
                API Key
                <input
                  type="password"
                  value={formAPIKey}
                  onChange={e => setFormAPIKey(e.target.value)}
                  placeholder={editing !== 'new' && configs.find(c => c.id === editing)?.api_key_set ? 'Leave empty to keep current key' : 'Enter API key'}
                />
              </label>

              <label>
                Model
                <input
                  value={formModel}
                  onChange={e => setFormModel(e.target.value)}
                  placeholder="e.g. gpt-4o"
                />
              </label>

              <label className="checkbox-label">
                <input
                  type="checkbox"
                  checked={formDefault}
                  onChange={e => setFormDefault(e.target.checked)}
                />
                Set as default provider
              </label>

              {testResult && (
                <div className={`test-result ${testResult.success ? 'success' : 'error'}`}>
                  {testResult.success ? 'Connection successful' : `Failed: ${testResult.message}`}
                  {testResult.latency_ms > 0 && <span className="latency"> ({testResult.latency_ms}ms)</span>}
                </div>
              )}

              <div className="dialog-actions">
                <button className="btn-secondary" onClick={handleTest} disabled={!canTest}>
                  {testing ? 'Testing...' : 'Test Connection'}
                </button>
                <button className="btn-primary" onClick={handleSave} disabled={saving || !formModel.trim()}>
                  {saving ? 'Saving...' : 'Save'}
                </button>
                <button onClick={() => setEditing(null)}>Cancel</button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
