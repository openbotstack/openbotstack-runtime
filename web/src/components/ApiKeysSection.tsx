import { useState, useEffect, useCallback } from 'react'
import { apiCall } from '../lib/api'
import { Dialog, ConfirmDialog } from './Dialog'

interface Tenant { id: string; name: string; created_at: string }
interface User { id: string; tenant_id: string; name: string; role: string; created_at: string }
interface APIKey { id: string; key_prefix: string; name: string; created_at: string; revoked: boolean }

export function ApiKeysSection() {
  const [tenants, setTenants] = useState<Tenant[]>([])
  const [selectedTenant, setSelectedTenant] = useState('')
  const [users, setUsers] = useState<User[]>([])
  const [selectedUser, setSelectedUser] = useState('')
  const [keys, setKeys] = useState<APIKey[]>([])
  const [tenantsLoading, setTenantsLoading] = useState(true)
  const [usersLoading, setUsersLoading] = useState(false)
  const [keysLoading, setKeysLoading] = useState(false)
  const [error, setError] = useState('')

  // Create dialog
  const [showCreate, setShowCreate] = useState(false)
  const [newName, setNewName] = useState('')
  const [creating, setCreating] = useState(false)
  const [createdKey, setCreatedKey] = useState('')

  // Revoke dialog
  const [revokeTarget, setRevokeTarget] = useState<APIKey | null>(null)
  const [revoking, setRevoking] = useState(false)

  // Load tenants on mount
  useEffect(() => {
    apiCall<Tenant[]>('/v1/admin/tenants')
      .then(data => {
        const list = Array.isArray(data) ? data : []
        setTenants(list)
        if (list.length > 0) setSelectedTenant(list[0].id)
      })
      .catch(e => setError((e as Error).message))
      .finally(() => setTenantsLoading(false))
  }, [])

  // Load users on tenant selection
  useEffect(() => {
    if (!selectedTenant) { setUsers([]); setSelectedUser(''); return }
    setUsersLoading(true)
    setSelectedUser('')
    apiCall<User[]>(`/v1/admin/tenants/${selectedTenant}/users`)
      .then(data => {
        const list = Array.isArray(data) ? data : []
        setUsers(list)
        if (list.length > 0) setSelectedUser(list[0].id)
      })
      .catch(e => setError((e as Error).message))
      .finally(() => setUsersLoading(false))
  }, [selectedTenant])

  // Load keys on user selection
  const fetchKeys = useCallback(async (userId: string) => {
    if (!userId) return
    setKeysLoading(true)
    try {
      const data = await apiCall<APIKey[]>(`/v1/admin/users/${userId}/keys`)
      setKeys(Array.isArray(data) ? data : [])
      setError('')
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setKeysLoading(false)
    }
  }, [])

  useEffect(() => {
    if (selectedUser) fetchKeys(selectedUser)
  }, [selectedUser, fetchKeys])

  const handleCreate = async () => {
    if (!newName.trim() || !selectedUser) return
    setCreating(true)
    try {
      const resp = await apiCall<{ id: string; key: string }>('/v1/admin/users/' + selectedUser + '/keys', {
        method: 'POST',
        body: JSON.stringify({ name: newName.trim() }),
      })
      setShowCreate(false)
      setNewName('')
      setCreatedKey(resp.key)
      await fetchKeys(selectedUser)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setCreating(false)
    }
  }

  const handleRevoke = async () => {
    if (!revokeTarget) return
    setRevoking(true)
    try {
      await apiCall(`/v1/admin/keys/${revokeTarget.id}`, { method: 'DELETE' })
      setRevokeTarget(null)
      if (selectedUser) await fetchKeys(selectedUser)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setRevoking(false)
    }
  }

  const copyKey = () => {
    navigator.clipboard.writeText(createdKey)
  }

  if (tenantsLoading) return <div className="loading">Loading tenants...</div>

  return (
    <div className="admin-section">
      <div className="section-header">
        <h2>API Keys</h2>
        <button className="btn-primary" onClick={() => { setShowCreate(true); setCreatedKey('') }} disabled={!selectedUser}>+ New Key</button>
      </div>

      <div className="selector-row">
        <label>Tenant:
          <select value={selectedTenant} onChange={e => setSelectedTenant(e.target.value)}>
            <option value="">Select tenant...</option>
            {tenants.map(t => <option key={t.id} value={t.id}>{t.name} ({t.id})</option>)}
          </select>
        </label>
        {usersLoading && <span className="loading-inline">Loading...</span>}
        {!usersLoading && users.length > 0 && (
          <label>User:
            <select value={selectedUser} onChange={e => setSelectedUser(e.target.value)}>
              <option value="">Select user...</option>
              {users.map(u => <option key={u.id} value={u.id}>{u.name} ({u.id})</option>)}
            </select>
          </label>
        )}
      </div>

      {error && <div className="error">{error}</div>}
      {keysLoading && <div className="loading">Loading keys...</div>}

      {!keysLoading && selectedUser && keys.length === 0 && (
        <div className="empty">No API keys for this user</div>
      )}

      {!keysLoading && keys.length > 0 && (
        <table className="data-table">
          <thead>
            <tr>
              <th>Key ID</th>
              <th>Prefix</th>
              <th>Name</th>
              <th>Status</th>
              <th>Action</th>
            </tr>
          </thead>
          <tbody>
            {keys.map(k => (
              <tr key={k.id}>
                <td className="mono">{k.id.slice(0, 12)}...</td>
                <td className="mono">{k.key_prefix}</td>
                <td>{k.name}</td>
                <td><span className={`status-badge ${k.revoked ? 'status-revoked' : 'status-active'}`}>{k.revoked ? 'Revoked' : 'Active'}</span></td>
                <td>{!k.revoked && <button className="btn-danger-sm" onClick={() => setRevokeTarget(k)}>Revoke</button>}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {/* Create Key Dialog */}
      <Dialog open={showCreate && !createdKey} onClose={() => setShowCreate(false)} title="New API Key">
        <label>Name <input value={newName} onChange={e => setNewName(e.target.value)} placeholder="e.g. staging" /></label>
        <div className="dialog-actions">
          <button className="btn-primary" onClick={handleCreate} disabled={creating || !newName.trim()}>
            {creating ? 'Creating...' : 'Create'}
          </button>
          <button onClick={() => setShowCreate(false)}>Cancel</button>
        </div>
      </Dialog>

      {/* Show Created Key */}
      <Dialog open={!!createdKey} onClose={() => setCreatedKey('')} title="API Key Created">
        <div className="key-warning">
          WARNING: Save this key now! It won't be shown again.
        </div>
        <div className="key-display">
          <code>{createdKey}</code>
          <button className="btn-primary" onClick={copyKey}>Copy</button>
        </div>
      </Dialog>

      {/* Revoke Confirmation */}
      <ConfirmDialog
        open={!!revokeTarget}
        onClose={() => setRevokeTarget(null)}
        onConfirm={handleRevoke}
        title="Revoke API Key"
        message={`Revoke key "${revokeTarget?.name}" (${revokeTarget?.key_prefix})? This cannot be undone.`}
        confirmLabel={revoking ? 'Revoking...' : 'Revoke'}
      />
    </div>
  )
}
