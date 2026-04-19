import { useState, useEffect, useCallback } from 'react'
import { apiCall } from '../lib/api'
import { Dialog, ConfirmDialog } from './Dialog'

interface Tenant { id: string; name: string; created_at: string }
interface User { id: string; tenant_id: string; name: string; role: string; created_at: string }

export function UsersSection() {
  const [tenants, setTenants] = useState<Tenant[]>([])
  const [selectedTenant, setSelectedTenant] = useState('')
  const [users, setUsers] = useState<User[]>([])
  const [loading, setLoading] = useState(true)
  const [usersLoading, setUsersLoading] = useState(false)
  const [error, setError] = useState('')
  const [showCreate, setShowCreate] = useState(false)
  const [newId, setNewId] = useState('')
  const [newName, setNewName] = useState('')
  const [newRole, setNewRole] = useState('member')
  const [creating, setCreating] = useState(false)
  const [editUser, setEditUser] = useState<User | null>(null)
  const [editName, setEditName] = useState('')
  const [editRole, setEditRole] = useState('')
  const [saving, setSaving] = useState(false)
  const [deleteId, setDeleteId] = useState<string | null>(null)

  useEffect(() => {
    apiCall<Tenant[]>('/v1/admin/tenants')
      .then(data => {
        const list = Array.isArray(data) ? data : []
        setTenants(list)
        if (list.length > 0) setSelectedTenant(list[0].id)
      })
      .catch(e => setError((e as Error).message))
      .finally(() => setLoading(false))
  }, [])

  const fetchUsers = useCallback(async (tenantId: string) => {
    if (!tenantId) return
    setUsersLoading(true)
    try {
      const data = await apiCall<User[]>(`/v1/admin/tenants/${tenantId}/users`)
      setUsers(Array.isArray(data) ? data : [])
      setError('')
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setUsersLoading(false)
    }
  }, [])

  useEffect(() => {
    if (selectedTenant) fetchUsers(selectedTenant)
  }, [selectedTenant, fetchUsers])

  const handleCreate = async () => {
    if (!newId.trim() || !newName.trim() || !selectedTenant) return
    setCreating(true)
    try {
      await apiCall(`/v1/admin/tenants/${selectedTenant}/users`, {
        method: 'POST',
        body: JSON.stringify({ id: newId.trim(), name: newName.trim(), role: newRole }),
      })
      setShowCreate(false)
      setNewId('')
      setNewName('')
      setNewRole('member')
      await fetchUsers(selectedTenant)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setCreating(false)
    }
  }

  const handleUpdate = async () => {
    if (!editUser) return
    setSaving(true)
    try {
      await apiCall(`/v1/admin/users/${editUser.id}`, {
        method: 'PUT',
        body: JSON.stringify({ name: editName.trim(), role: editRole }),
      })
      setEditUser(null)
      await fetchUsers(selectedTenant)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setSaving(false)
    }
  }

  const handleDelete = async (id: string) => {
    try {
      await apiCall(`/v1/admin/users/${id}`, { method: 'DELETE' })
      setDeleteId(null)
      await fetchUsers(selectedTenant)
    } catch (e) {
      setError((e as Error).message)
    }
  }

  if (loading) return <div className="loading">Loading tenants...</div>

  return (
    <div className="admin-section">
      <div className="section-header">
        <h2>Users</h2>
        <button className="btn-primary" onClick={() => setShowCreate(true)} disabled={!selectedTenant}>+ New User</button>
      </div>

      <div className="selector-row">
        <label>Tenant:
          <select value={selectedTenant} onChange={e => setSelectedTenant(e.target.value)}>
            <option value="">Select tenant...</option>
            {tenants.map(t => <option key={t.id} value={t.id}>{t.name} ({t.id})</option>)}
          </select>
        </label>
      </div>

      {error && <div className="error">{error}</div>}
      {usersLoading && <div className="loading">Loading users...</div>}

      {!usersLoading && selectedTenant && users.length === 0 && (
        <div className="empty">No users in this tenant</div>
      )}

      {!usersLoading && users.length > 0 && (
        <table className="data-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Name</th>
              <th>Role</th>
              <th>Created</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {users.map(u => (
              <tr key={u.id}>
                <td className="mono">{u.id}</td>
                <td>{u.name}</td>
                <td><span className={`role-badge role-${u.role}`}>{u.role}</span></td>
                <td>{u.created_at ? new Date(u.created_at).toLocaleDateString() : '-'}</td>
                <td className="actions">
                  <button className="btn-sm" onClick={() => { setEditUser(u); setEditName(u.name); setEditRole(u.role) }}>Edit</button>
                  <button className="btn-sm btn-danger" onClick={() => setDeleteId(u.id)}>Delete</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <Dialog open={showCreate} onClose={() => setShowCreate(false)} title="New User">
        <label>ID <input value={newId} onChange={e => setNewId(e.target.value)} placeholder="e.g. alice" /></label>
        <label>Name <input value={newName} onChange={e => setNewName(e.target.value)} placeholder="e.g. Alice" /></label>
        <label>Role
          <select value={newRole} onChange={e => setNewRole(e.target.value)}>
            <option value="admin">admin</option>
            <option value="member">member</option>
          </select>
        </label>
        <div className="dialog-actions">
          <button className="btn-primary" onClick={handleCreate} disabled={creating || !newId.trim() || !newName.trim()}>
            {creating ? 'Creating...' : 'Create'}
          </button>
          <button onClick={() => setShowCreate(false)}>Cancel</button>
        </div>
      </Dialog>

      <Dialog open={!!editUser} onClose={() => setEditUser(null)} title={`Edit User: ${editUser?.id}`}>
        <label>Name <input value={editName} onChange={e => setEditName(e.target.value)} /></label>
        <label>Role
          <select value={editRole} onChange={e => setEditRole(e.target.value)}>
            <option value="admin">admin</option>
            <option value="member">member</option>
          </select>
        </label>
        <div className="dialog-actions">
          <button className="btn-primary" onClick={handleUpdate} disabled={saving || !editName.trim()}>
            {saving ? 'Saving...' : 'Save'}
          </button>
          <button onClick={() => setEditUser(null)}>Cancel</button>
        </div>
      </Dialog>

      <ConfirmDialog
        open={!!deleteId}
        onClose={() => setDeleteId(null)}
        onConfirm={() => deleteId && handleDelete(deleteId)}
        title="Delete User"
        message={`Delete user "${deleteId}"? Associated API keys will also be removed.`}
      />
    </div>
  )
}
