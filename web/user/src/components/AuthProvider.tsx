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
