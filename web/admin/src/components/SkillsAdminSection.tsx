import { useState, useEffect, useCallback } from 'react'
import { apiCall } from '../lib/api'

interface SkillInfo {
  id: string
  name: string
  description: string
  type: string
  enabled: boolean
}

export function SkillsAdminSection() {
  const [skills, setSkills] = useState<SkillInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const fetchSkills = useCallback(async () => {
    try {
      const data = await apiCall<SkillInfo[]>('/v1/admin/skills')
      setSkills(Array.isArray(data) ? data : [])
      setError('')
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchSkills() }, [fetchSkills])

  const toggleSkill = async (id: string, enable: boolean) => {
    try {
      await apiCall(`/v1/admin/skills/${id}/${enable ? 'enable' : 'disable'}`, { method: 'POST' })
      setSkills(prev => prev.map(s => s.id === id ? { ...s, enabled: enable } : s))
    } catch (e) {
      setError((e as Error).message)
    }
  }

  if (loading) return <div className="loading">Loading skills...</div>

  return (
    <div className="admin-section">
      <div className="section-header">
        <h2>Skills ({skills.length})</h2>
        <button className="btn-primary" onClick={fetchSkills}>Refresh</button>
      </div>

      {error && <div className="error">{error}</div>}

      {skills.length === 0 ? (
        <div className="empty">No skills loaded. Place skill directories in the skills path.</div>
      ) : (
        <table className="data-table">
          <thead>
            <tr>
              <th>Skill ID</th>
              <th>Name</th>
              <th>Type</th>
              <th>Description</th>
              <th>Status</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {skills.map(s => (
              <tr key={s.id}>
                <td className="mono">{s.id}</td>
                <td>{s.name}</td>
                <td><span className={`type-badge type-${s.type}`}>{s.type}</span></td>
                <td className="skill-desc">{s.description}</td>
                <td>
                  <span className={`status-badge ${s.enabled ? 'status-active' : 'status-revoked'}`}>
                    {s.enabled ? 'Active' : 'Disabled'}
                  </span>
                </td>
                <td className="actions">
                  {s.enabled ? (
                    <button className="btn-sm btn-danger" onClick={() => toggleSkill(s.id, false)}>Disable</button>
                  ) : (
                    <button className="btn-sm" onClick={() => toggleSkill(s.id, true)}>Enable</button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
