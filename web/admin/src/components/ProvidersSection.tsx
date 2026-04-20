import { useState, useEffect, useCallback } from 'react'
import { apiCall } from '../lib/api'

interface ProviderConfigEntry {
  name: string
  base_url: string
  api_key_set: boolean
  model: string
  is_default: boolean
}

interface ProviderConfigResponse {
  default: string
  providers: Record<string, ProviderConfigEntry>
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
  const [config, setConfig] = useState<ProviderConfigResponse>({ default: '', providers: {} })
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  // Edit form state
  const [editing, setEditing] = useState<string | null>(null)
  const [formProvider, setFormProvider] = useState('openai')
  const [formBaseURL, setFormBaseURL] = useState('')
  const [formAPIKey, setFormAPIKey] = useState('')
  const [formModel, setFormModel] = useState('')
  const [formDefault, setFormDefault] = useState(false)
  const [saving, setSaving] = useState(false)

  // Test connection state
  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<TestResult | null>(null)

  const fetchConfig = useCallback(async () => {
    try {
      const data = await apiCall<ProviderConfigResponse>('/v1/admin/providers/config')
      setConfig(data)
      setError('')
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchConfig() }, [fetchConfig])

  const startEdit = (providerName?: string) => {
    const entry = providerName ? config.providers[providerName] : null
    setFormProvider(providerName || 'openai')
    setFormBaseURL(entry?.base_url || '')
    setFormAPIKey('')
    setFormModel(entry?.model || '')
    setFormDefault(entry?.is_default || !providerName)
    setTestResult(null)
    setEditing(providerName || 'new')
  }

  const handleProviderChange = (id: string) => {
    const opt = PROVIDER_OPTIONS.find(o => o.id === id)
    setFormProvider(id)
    if (!formBaseURL && opt) setFormBaseURL(opt.defaultURL)
    if (!formModel && opt) setFormModel(opt.defaultModel)
  }

  const handleSave = async () => {
    if (!formModel.trim()) return
    setSaving(true)
    setError('')
    try {
      await apiCall('/v1/admin/providers/config', {
        method: 'PUT',
        body: JSON.stringify({
          provider: formProvider,
          base_url: formBaseURL.trim(),
          api_key: formAPIKey.trim(),
          model: formModel.trim(),
          is_default: formDefault ? 'true' : 'false',
        }),
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

  const handleTest = async () => {
    if (!formAPIKey.trim() && !config.providers[formProvider]?.api_key_set) return
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

  if (loading) return <div className="loading">Loading providers...</div>

  const configuredProviders = Object.values(config.providers)
  const defaultProvider = config.default

  return (
    <div className="admin-section">
      <div className="section-header">
        <h2>Model Providers ({configuredProviders.length})</h2>
        <div>
          <button className="btn-primary" onClick={() => startEdit()}>+ Configure Provider</button>
          <button className="btn-secondary" style={{ marginLeft: 8 }} onClick={fetchConfig}>Refresh</button>
        </div>
      </div>

      {error && <div className="error">{error}</div>}

      {/* Current Configuration */}
      {configuredProviders.length === 0 ? (
        <div className="empty">
          No providers configured. Click "+ Configure Provider" to set up your first LLM provider.
        </div>
      ) : (
        <table className="data-table">
          <thead>
            <tr>
              <th>Provider</th>
              <th>Base URL</th>
              <th>Model</th>
              <th>API Key</th>
              <th>Default</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {configuredProviders.map(p => (
              <tr key={p.name}>
                <td className="mono">
                  <span className={`type-badge type-${p.name === 'openai' ? 'declarative' : p.name === 'modelscope' ? 'deterministic' : 'llm'}`}>
                    {PROVIDER_OPTIONS.find(o => o.id === p.name)?.label || p.name}
                  </span>
                </td>
                <td className="mono" style={{ fontSize: '0.85em' }}>{p.base_url || '-'}</td>
                <td className="mono">{p.model || '-'}</td>
                <td>
                  <span className={`type-badge ${p.api_key_set ? 'status-active' : 'status-revoked'}`}>
                    {p.api_key_set ? 'Set' : 'Not Set'}
                  </span>
                </td>
                <td>
                  {p.is_default && <span className="type-badge status-active">Default</span>}
                  {!p.is_default && p.name === defaultProvider && <span className="type-badge status-active">Active</span>}
                </td>
                <td className="actions">
                  <button className="btn-sm" onClick={() => startEdit(p.name)}>Edit</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {/* Configuration Form */}
      {editing && (
        <div className="dialog-overlay" onClick={() => setEditing(null)}>
          <div className="dialog" onClick={e => e.stopPropagation()} style={{ maxWidth: 520 }}>
            <div className="dialog-header">
              <h3>{editing === 'new' ? 'Configure Provider' : `Edit: ${editing}`}</h3>
              <button className="dialog-close" onClick={() => setEditing(null)}>x</button>
            </div>
            <div className="dialog-body">
              <label>
                Provider
                <select value={formProvider} onChange={e => handleProviderChange(e.target.value)} disabled={editing !== 'new'}>
                  {PROVIDER_OPTIONS.map(o => (
                    <option key={o.id} value={o.id}>{o.label}</option>
                  ))}
                </select>
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
                  placeholder={editing !== 'new' && config.providers[editing]?.api_key_set ? 'Leave empty to keep current key' : 'Enter API key'}
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
                <button
                  className="btn-secondary"
                  onClick={handleTest}
                  disabled={testing || (!formAPIKey.trim() && !(editing !== 'new' && config.providers[editing]?.api_key_set)) || !formModel.trim()}
                >
                  {testing ? 'Testing...' : 'Test Connection'}
                </button>
                <button
                  className="btn-primary"
                  onClick={handleSave}
                  disabled={saving || !formModel.trim()}
                >
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
