import { useState, useEffect } from 'react'
import { useAuth } from './AuthProvider'
import { apiCall } from '../lib/api'
import { TenantsSection } from './TenantsSection'
import { UsersSection } from './UsersSection'
import { ApiKeysSection } from './ApiKeysSection'

type AdminTab = 'tenants' | 'users' | 'keys'

export function AdminPage() {
  const [tab, setTab] = useState<AdminTab>('tenants')
  const { role, logout } = useAuth()

  // Defense in depth: re-verify admin role on mount
  const [verified, setVerified] = useState(false)
  const [forbidden, setForbidden] = useState(false)

  useEffect(() => {
    apiCall<{ role: string }>('/v1/me')
      .then(me => {
        if (me.role !== 'admin') {
          setForbidden(true)
        } else {
          setVerified(true)
        }
      })
      .catch(() => {
        // Auth error — apiCall already clears session
        setForbidden(true)
      })
  }, [])

  if (forbidden) {
    return (
      <div className="page">
        <div className="error">Admin access required. Your role: {role || 'unknown'}</div>
        <button onClick={logout}>Logout</button>
      </div>
    )
  }

  if (!verified) {
    return <div className="loading">Verifying admin access...</div>
  }

  return (
    <div className="page admin-page">
      <div className="admin-tabs">
        <button className={tab === 'tenants' ? 'active' : ''} onClick={() => setTab('tenants')}>Tenants</button>
        <button className={tab === 'users' ? 'active' : ''} onClick={() => setTab('users')}>Users</button>
        <button className={tab === 'keys' ? 'active' : ''} onClick={() => setTab('keys')}>API Keys</button>
      </div>
      {tab === 'tenants' && <TenantsSection />}
      {tab === 'users' && <UsersSection />}
      {tab === 'keys' && <ApiKeysSection />}
    </div>
  )
}
