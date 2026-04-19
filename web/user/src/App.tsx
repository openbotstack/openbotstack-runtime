import { useState, useEffect, useCallback } from 'react'
import { AuthProvider, useAuth } from './components/AuthProvider'
import { ChatPage } from './components/ChatPage'
import { apiCall } from './lib/api'

// --- Skill type ---
interface SkillInfo {
  id: string
  name: string
  description: string
  type: string
  version: string
  enabled: boolean
}

// --- Skills Panel ---
function SkillsPanel() {
  const [open, setOpen] = useState(false)
  const [skills, setSkills] = useState<SkillInfo[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const fetchSkills = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const data = await apiCall<SkillInfo[]>('/v1/skills')
      setSkills(Array.isArray(data) ? data : [])
    } catch {
      setError('Failed to load skills')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (open && skills.length === 0 && !loading && !error) {
      fetchSkills()
    }
  }, [open, skills.length, loading, error, fetchSkills])

  const typeBadgeClass = (type: string) => {
    switch (type) {
      case 'declarative': return 'type-declarative'
      case 'llm-assisted': return 'type-llm'
      case 'deterministic': return 'type-deterministic'
      default: return 'type-other'
    }
  }

  return (
    <div className="skills-container">
      <button
        className={`btn-skills ${open ? 'active' : ''}`}
        onClick={() => setOpen(!open)}
        title="Browse Skills"
      >
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"/>
        </svg>
        Skills
      </button>
      {open && (
        <div className="skills-panel">
          <div className="skills-panel-header">
            <span>Available Skills</span>
            <button className="btn-skills-refresh" onClick={fetchSkills} title="Refresh">
              &#x21bb;
            </button>
            <button className="btn-skills-close" onClick={() => setOpen(false)} title="Close">
              &#x2715;
            </button>
          </div>
          <div className="skills-list">
            {loading && <div className="loading">Loading skills...</div>}
            {error && <div className="error">{error}</div>}
            {!loading && !error && skills.length === 0 && (
              <div className="empty-sm">No skills available</div>
            )}
            {skills.map(skill => (
              <div key={skill.id} className="skill-card">
                <div className="skill-card-top">
                  <span className="skill-card-name">{skill.name}</span>
                  <span className={`type-badge ${typeBadgeClass(skill.type)}`}>{skill.type}</span>
                </div>
                <div className="skill-card-desc">{skill.description}</div>
                <div className="skill-card-meta">
                  <span className="skill-card-id">{skill.id}</span>
                  <span className="skill-card-ver">v{skill.version}</span>
                  {!skill.enabled && <span className="skill-card-disabled">disabled</span>}
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

// --- Login Form ---
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
      setError('Invalid API key')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="login-page">
      <div className="login-form">
        <h2>OpenBotStack</h2>
        <p>Connect with your API Key</p>
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

// --- App Content ---
function AppContent() {
  const { authenticated, user, role, login, logout } = useAuth()

  if (!authenticated) {
    return <LoginForm onLogin={login} />
  }

  return (
    <div className="app">
      <header className="header">
        <div className="header-left">
          <h1>OpenBotStack</h1>
          <SkillsPanel />
        </div>
        <div className="header-right">
          <span className="user-name">{user?.name || 'User'}</span>
          {role && <span className={`role-badge role-${role}`}>{role}</span>}
          <button className="btn-logout" onClick={logout}>Logout</button>
        </div>
      </header>
      <ChatPage />
    </div>
  )
}

function App() {
  return (
    <AuthProvider>
      <AppContent />
    </AuthProvider>
  )
}

export default App
