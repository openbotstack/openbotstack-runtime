import { type ReactNode, useState, useEffect, useCallback, createContext, useContext } from 'react'
import { validateKey, storeAuth, clearAuth, getStoredKey, type MeResponse } from '../lib/api'

interface AuthContextType {
  authenticated: boolean
  role: string | null
  user: MeResponse | null
  login: (key: string) => Promise<void>
  logout: () => void
}

const AuthContext = createContext<AuthContextType>({
  authenticated: false,
  role: null,
  user: null,
  login: async () => {},
  logout: () => {},
})

export function useAuth() {
  return useContext(AuthContext)
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [authenticated, setAuthenticated] = useState(false)
  const [role, setRole] = useState<string | null>(null)
  const [user, setUser] = useState<MeResponse | null>(null)
  const [checking, setChecking] = useState(true)

  useEffect(() => {
    const key = getStoredKey()
    if (!key) {
      setChecking(false)
      return
    }
    validateKey(key)
      .then(me => {
        storeAuth(key, me.role)
        setAuthenticated(true)
        setRole(me.role)
        setUser(me)
      })
      .catch(() => {
        clearAuth()
      })
      .finally(() => setChecking(false))
  }, [])

  const login = useCallback(async (key: string) => {
    const me = await validateKey(key)
    storeAuth(key, me.role)
    setAuthenticated(true)
    setRole(me.role)
    setUser(me)
  }, [])

  const logout = useCallback(() => {
    clearAuth()
    setAuthenticated(false)
    setRole(null)
    setUser(null)
  }, [])

  if (checking) {
    return <div className="loading">Checking authentication...</div>
  }

  return (
    <AuthContext.Provider value={{ authenticated, role, user, login, logout }}>
      {children}
    </AuthContext.Provider>
  )
}

export function LoginForm({ onLogin }: { onLogin: (key: string) => Promise<void> }) {
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
    <form onSubmit={handleSubmit} className="login-form">
      <h3 style={{ marginBottom: '8px' }}>Connect with API Key</h3>
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
  )
}
