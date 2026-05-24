import { useState, type ReactNode } from 'react'
import { AuthProvider, useAuth } from './components/AuthProvider'
import { TenantsSection } from './components/TenantsSection'
import { UsersSection } from './components/UsersSection'
import { ApiKeysSection } from './components/ApiKeysSection'
import { ExecutionsSection } from './components/ExecutionsSection'
import { SystemInfoSection } from './components/SystemInfoSection'
import { ProvidersSection } from './components/ProvidersSection'
import { AuditSection } from './components/AuditSection'
import { SkillsAdminSection } from './components/SkillsAdminSection'
import { SessionsSection } from './components/SessionsSection'
import { TelemetryHealth } from './components/TelemetryHealth'
import { TelemetryExplorer } from './components/TelemetryExplorer'
import { TelemetryFailures } from './components/TelemetryFailures'
import { MCPServersSection } from './components/MCPServersSection'
import { CapabilitiesSection } from './components/CapabilitiesSection'
import {
  Building2, Users, KeyRound, Sparkles, Server, Layers,
  Brain, Clock, PlayCircle, ScrollText, Activity, Info,
} from 'lucide-react'

type AdminTab = 'tenants' | 'users' | 'keys' | 'sessions' | 'executions' | 'skills' | 'providers' | 'audit' | 'system' | 'telemetry' | 'mcp' | 'capabilities'

type TelemetryPage = 'health' | 'explorer' | 'failures'

interface NavGroup {
  label: string
  items: { id: AdminTab; label: string; icon: ReactNode }[]
}

const navGroups: NavGroup[] = [
  {
    label: 'Access Control',
    items: [
      { id: 'tenants', label: 'Tenants', icon: <Building2 size={15} /> },
      { id: 'users', label: 'Users', icon: <Users size={15} /> },
      { id: 'keys', label: 'API Keys', icon: <KeyRound size={15} /> },
    ],
  },
  {
    label: 'Capabilities',
    items: [
      { id: 'skills', label: 'Skills', icon: <Sparkles size={15} /> },
      { id: 'mcp', label: 'MCP Servers', icon: <Server size={15} /> },
      { id: 'capabilities', label: 'Capabilities', icon: <Layers size={15} /> },
    ],
  },
  {
    label: 'AI Service',
    items: [
      { id: 'providers', label: 'Providers', icon: <Brain size={15} /> },
    ],
  },
  {
    label: 'Operations',
    items: [
      { id: 'sessions', label: 'Sessions', icon: <Clock size={15} /> },
      { id: 'executions', label: 'Executions', icon: <PlayCircle size={15} /> },
      { id: 'audit', label: 'Audit', icon: <ScrollText size={15} /> },
    ],
  },
  {
    label: 'System',
    items: [
      { id: 'telemetry', label: 'Telemetry', icon: <Activity size={15} /> },
      { id: 'system', label: 'System Info', icon: <Info size={15} /> },
    ],
  },
]

function LoginForm({ onLogin }: { onLogin: (key: string) => Promise<void> }) {
  const [key, setKey] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!key.trim()) return
    setLoading(true)
    setError('')
    try {
      await onLogin(key.trim())
    } catch {
      setError('Invalid API key or insufficient permissions')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="login-page">
      <div className="login-form">
        <h2>OpenBotStack Admin</h2>
        <p>Connect with an admin API Key</p>
        <form onSubmit={handleSubmit}>
          <input
            type="password"
            value={key}
            onChange={e => setKey(e.target.value)}
            placeholder="obs_..."
            disabled={loading}
            autoFocus
          />
          {error && <div className="error">{error}</div>}
          <button type="submit" disabled={loading || !key.trim()}>
            {loading ? 'Connecting...' : 'Connect'}
          </button>
        </form>
      </div>
    </div>
  )
}

function AdminApp() {
  const { authenticated, user, role, login, logout } = useAuth()
  const [tab, setTab] = useState<AdminTab>('tenants')
  const [telPage, setTelPage] = useState<TelemetryPage>('health')

  if (!authenticated) {
    return <LoginForm onLogin={login} />
  }

  return (
    <div className="app">
      <header className="header">
        <h1>OpenBotStack Admin</h1>
        <div className="header-right">
          <span className="user-name">{user?.name || 'Admin'}</span>
          {role && <span className={`role-badge role-${role}`}>{role}</span>}
          <button className="btn-logout" onClick={logout}>Logout</button>
        </div>
      </header>
      <div className="admin-body">
        <nav className="sidebar">
          {navGroups.map(group => (
            <div className="sidebar-group" key={group.label}>
              <div className="sidebar-group-label">{group.label}</div>
              {group.items.map(item => (
                <button
                  key={item.id}
                  className={`sidebar-item ${tab === item.id ? 'active' : ''}`}
                  onClick={() => setTab(item.id)}
                >
                  {item.icon}
                  {item.label}
                </button>
              ))}
            </div>
          ))}
        </nav>
        <main className="admin-main">
          {tab === 'tenants' && <TenantsSection />}
          {tab === 'users' && <UsersSection />}
          {tab === 'keys' && <ApiKeysSection />}
          {tab === 'sessions' && <SessionsSection />}
          {tab === 'executions' && <ExecutionsSection />}
          {tab === 'skills' && <SkillsAdminSection />}
          {tab === 'providers' && <ProvidersSection />}
          {tab === 'audit' && <AuditSection />}
          {tab === 'system' && <SystemInfoSection />}
          {tab === 'telemetry' && (
            <>
              <div className="sub-tabs">
                <button className={telPage === 'health' ? 'active' : ''} onClick={() => setTelPage('health')}>Health</button>
                <button className={telPage === 'explorer' ? 'active' : ''} onClick={() => setTelPage('explorer')}>Explorer</button>
                <button className={telPage === 'failures' ? 'active' : ''} onClick={() => setTelPage('failures')}>Failures</button>
              </div>
              {telPage === 'health' && <TelemetryHealth />}
              {telPage === 'explorer' && <TelemetryExplorer />}
              {telPage === 'failures' && <TelemetryFailures />}
            </>
          )}
          {tab === 'mcp' && <MCPServersSection />}
          {tab === 'capabilities' && <CapabilitiesSection />}
        </main>
      </div>
    </div>
  )
}

function App() {
  return (
    <AuthProvider>
      <AdminApp />
    </AuthProvider>
  )
}

export default App
