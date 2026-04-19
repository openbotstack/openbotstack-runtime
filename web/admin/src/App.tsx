import { useState } from 'react'
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

type AdminTab = 'tenants' | 'users' | 'keys' | 'sessions' | 'executions' | 'skills' | 'providers' | 'audit' | 'system'

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
      <div className="page admin-page">
        <div className="admin-tabs">
          <button className={tab === 'tenants' ? 'active' : ''} onClick={() => setTab('tenants')}>Tenants</button>
          <button className={tab === 'users' ? 'active' : ''} onClick={() => setTab('users')}>Users</button>
          <button className={tab === 'keys' ? 'active' : ''} onClick={() => setTab('keys')}>API Keys</button>
          <button className={tab === 'sessions' ? 'active' : ''} onClick={() => setTab('sessions')}>Sessions</button>
          <button className={tab === 'executions' ? 'active' : ''} onClick={() => setTab('executions')}>Executions</button>
          <button className={tab === 'skills' ? 'active' : ''} onClick={() => setTab('skills')}>Skills</button>
          <button className={tab === 'providers' ? 'active' : ''} onClick={() => setTab('providers')}>Providers</button>
          <button className={tab === 'audit' ? 'active' : ''} onClick={() => setTab('audit')}>Audit</button>
          <button className={tab === 'system' ? 'active' : ''} onClick={() => setTab('system')}>System</button>
        </div>
        {tab === 'tenants' && <TenantsSection />}
        {tab === 'users' && <UsersSection />}
        {tab === 'keys' && <ApiKeysSection />}
        {tab === 'sessions' && <SessionsSection />}
        {tab === 'executions' && <ExecutionsSection />}
        {tab === 'skills' && <SkillsAdminSection />}
        {tab === 'providers' && <ProvidersSection />}
        {tab === 'audit' && <AuditSection />}
        {tab === 'system' && <SystemInfoSection />}
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
