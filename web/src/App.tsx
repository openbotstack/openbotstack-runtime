import { useState, useCallback, useEffect } from 'react'
import './App.css'

// ==================== Types ====================

interface Message {
  id: string
  role: 'user' | 'assistant'
  content: string
  skillUsed?: string
}

interface SkillInfo {
  id: string
  name: string
  description: string
  type: string
  input_schema: unknown
  output_schema: unknown
  version: string
  enabled: boolean
}

interface ExecutionInfo {
  execution_id: string
  session_id: string
  skill_id: string
  duration_ms: number
  status: string
  error?: string
}

// ==================== Navigation ====================

type Page = 'chat' | 'skills' | 'executions'

function Nav({ page, setPage }: { page: Page; setPage: (p: Page) => void }) {
  return (
    <nav className="nav">
      <button className={page === 'chat' ? 'active' : ''} onClick={() => setPage('chat')}>Chat</button>
      <button className={page === 'skills' ? 'active' : ''} onClick={() => setPage('skills')}>Skills</button>
      <button className={page === 'executions' ? 'active' : ''} onClick={() => setPage('executions')}>Executions</button>
    </nav>
  )
}

// ==================== Chat Page ====================

function ChatPage() {
  const [messages, setMessages] = useState<Message[]>([])
  const [input, setInput] = useState('')
  const [loading, setLoading] = useState(false)
  const [sessionId, setSessionId] = useState<string>('')
  const [executionStatus, setExecutionStatus] = useState<string>('')
  const [lastDuration, setLastDuration] = useState<number>(0)

  const sendMessage = useCallback(async () => {
    if (!input.trim() || loading) return

    const userMessage: Message = {
      id: crypto.randomUUID(),
      role: 'user',
      content: input,
    }

    setMessages(prev => [...prev, userMessage])
    setInput('')
    setLoading(true)
    setExecutionStatus('planning')

    const startTime = Date.now()

    try {
      setExecutionStatus('executing')

      const response = await fetch('/v1/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          tenant_id: 'default',
          user_id: 'dev-user',
          session_id: sessionId || undefined,
          message: input,
        }),
      })

      const data = await response.json()
      const duration = Date.now() - startTime
      setLastDuration(duration)

      if (data.session_id) {
        setSessionId(data.session_id)
      }

      const assistantMessage: Message = {
        id: crypto.randomUUID(),
        role: 'assistant',
        content: data.message || 'No response',
        skillUsed: data.skill_used,
      }

      setMessages(prev => [...prev, assistantMessage])
      setExecutionStatus('done')
    } catch (error) {
      console.error('Chat error:', error)
      setMessages(prev => [...prev, {
        id: crypto.randomUUID(),
        role: 'assistant',
        content: 'Error: Failed to send message',
      }])
      setExecutionStatus('error')
    } finally {
      setLoading(false)
    }
  }, [input, loading, sessionId])

  const handleKeyPress = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      sendMessage()
    }
  }

  return (
    <div className="page chat-page">
      {/* Status Bar */}
      <div className="status-bar">
        {sessionId && <span className="session-tag">Session: {sessionId.slice(0, 8)}…</span>}
        {executionStatus && (
          <span className={`status-tag status-${executionStatus}`}>
            {executionStatus === 'planning' && '🔄 Planning…'}
            {executionStatus === 'executing' && '⚡ Executing…'}
            {executionStatus === 'done' && `✅ Done (${lastDuration}ms)`}
            {executionStatus === 'error' && '❌ Error'}
          </span>
        )}
      </div>

      {/* Messages */}
      <div className="messages">
        {messages.length === 0 && (
          <div className="empty">Send a message to start chatting</div>
        )}
        {messages.map(msg => (
          <div key={msg.id} className={`message ${msg.role}`}>
            <div className="role">{msg.role}</div>
            <div className="content">{msg.content}</div>
            {msg.skillUsed && (
              <div className="skill-tag">Skill: {msg.skillUsed}</div>
            )}
          </div>
        ))}
        {loading && <div className="loading">Thinking…</div>}
      </div>

      {/* Input */}
      <div className="input-area">
        <textarea
          value={input}
          onChange={e => setInput(e.target.value)}
          onKeyDown={handleKeyPress}
          placeholder="Type a message…"
          disabled={loading}
        />
        <button onClick={sendMessage} disabled={loading || !input.trim()}>
          Send
        </button>
      </div>
    </div>
  )
}

// ==================== Skills Page ====================

function SkillsPage() {
  const [skills, setSkills] = useState<SkillInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    fetch('/v1/skills')
      .then(r => r.json())
      .then(data => {
        setSkills(Array.isArray(data) ? data : [])
        setLoading(false)
      })
      .catch(err => {
        setError('Failed to load skills: ' + err.message)
        setLoading(false)
      })
  }, [])

  if (loading) return <div className="page"><div className="loading">Loading skills…</div></div>
  if (error) return <div className="page"><div className="error">{error}</div></div>

  return (
    <div className="page skills-page">
      <h2>Registered Skills ({skills.length})</h2>
      {skills.length === 0 ? (
        <div className="empty">No skills registered</div>
      ) : (
        <table className="data-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Name</th>
              <th>Type</th>
              <th>Version</th>
              <th>Status</th>
            </tr>
          </thead>
          <tbody>
            {skills.map(skill => (
              <tr key={skill.id}>
                <td className="mono">{skill.id}</td>
                <td>{skill.name}</td>
                <td><span className={`type-badge type-${skill.type}`}>{skill.type}</span></td>
                <td className="mono">{skill.version}</td>
                <td>{skill.enabled ? '✅ Active' : '⏸ Disabled'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}

// ==================== Executions Page ====================

function ExecutionsPage() {
  const [executions, setExecutions] = useState<ExecutionInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    fetch('/v1/executions')
      .then(r => r.json())
      .then(data => {
        setExecutions(Array.isArray(data) ? data : [])
        setLoading(false)
      })
      .catch(err => {
        setError('Failed to load executions: ' + err.message)
        setLoading(false)
      })
  }, [])

  if (loading) return <div className="page"><div className="loading">Loading executions…</div></div>
  if (error) return <div className="page"><div className="error">{error}</div></div>

  return (
    <div className="page executions-page">
      <h2>Recent Executions ({executions.length})</h2>
      {executions.length === 0 ? (
        <div className="empty">No executions recorded</div>
      ) : (
        <table className="data-table">
          <thead>
            <tr>
              <th>Execution ID</th>
              <th>Session</th>
              <th>Skill</th>
              <th>Duration</th>
              <th>Status</th>
              <th>Error</th>
            </tr>
          </thead>
          <tbody>
            {executions.map(exec => (
              <tr key={exec.execution_id} className={exec.status === 'success' ? '' : 'error-row'}>
                <td className="mono">{exec.execution_id.slice(0, 8)}…</td>
                <td className="mono">{exec.session_id ? exec.session_id.slice(0, 8) + '…' : '—'}</td>
                <td className="mono">{exec.skill_id}</td>
                <td>{exec.duration_ms}ms</td>
                <td><span className={`status-badge status-${exec.status}`}>{exec.status}</span></td>
                <td className="error-text">{exec.error || '—'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}

// ==================== App ====================

function App() {
  const [page, setPage] = useState<Page>('chat')

  return (
    <div className="app">
      <header className="header">
        <h1>OpenBotStack</h1>
        <Nav page={page} setPage={setPage} />
      </header>

      {page === 'chat' && <ChatPage />}
      {page === 'skills' && <SkillsPage />}
      {page === 'executions' && <ExecutionsPage />}
    </div>
  )
}

export default App
