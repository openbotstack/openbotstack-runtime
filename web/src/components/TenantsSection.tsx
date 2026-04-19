import { useState, useEffect, useCallback } from 'react'
import { apiCall } from '../lib/api'
import { Dialog } from './Dialog'

interface Tenant {
  id: string
  name: string
  created_at: string
}

export function TenantsSection() {
  const [tenants, setTenants] = useState<Tenant[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [showCreate, setShowCreate] = useState(false)
  const [newId, setNewId] = useState('')
  const [newName, setNewName] = useState('')
  const [creating, setCreating] = useState(false)

  const fetchTenants = useCallback(async () => {
    try {
      const data = await apiCall<Tenant[]>('/v1/admin/tenants')
      setTenants(Array.isArray(data) ? data : [])
      setError('')
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchTenants() }, [fetchTenants])

  const handleCreate = async () => {
    if (!newId.trim() || !newName.trim()) return
    setCreating(true)
    try {
      await apiCall('/v1/admin/tenants', {
        method: 'POST',
        body: JSON.stringify({ id: newId.trim(), name: newName.trim() }),
      })
      setShowCreate(false)
      setNewId('')
      setNewName('')
      setLoading(true)
      await fetchTenants()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setCreating(false)
    }
  }

  if (loading) return <div className="loading">Loading tenants...</div>

  return (
    <div className="admin-section">
      <div className="section-header">
        <h2>Tenants ({tenants.length})</h2>
        <button className="btn-primary" onClick={() => setShowCreate(true)}>+ New Tenant</button>
      </div>

      {error && <div className="error">{error}</div>}

      {tenants.length === 0 ? (
        <div className="empty">No tenants found</div>
      ) : (
        <table className="data-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Name</th>
              <th>Created</th>
            </tr>
          </thead>
          <tbody>
            {tenants.map(t => (
              <tr key={t.id}>
                <td className="mono">{t.id}</td>
                <td>{t.name}</td>
                <td>{t.created_at ? new Date(t.created_at).toLocaleDateString() : '-'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <Dialog open={showCreate} onClose={() => setShowCreate(false)} title="New Tenant">
        <label>ID <input value={newId} onChange={e => setNewId(e.target.value)} placeholder="e.g. acme" /></label>
        <label>Name <input value={newName} onChange={e => setNewName(e.target.value)} placeholder="e.g. Acme Corp" /></label>
        <div className="dialog-actions">
          <button className="btn-primary" onClick={handleCreate} disabled={creating || !newId.trim() || !newName.trim()}>
            {creating ? 'Creating...' : 'Create'}
          </button>
          <button onClick={() => setShowCreate(false)}>Cancel</button>
        </div>
      </Dialog>
    </div>
  )
}
