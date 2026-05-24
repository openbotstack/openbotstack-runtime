import { useState, useCallback, useEffect, useRef } from 'react'
import { useAuth } from './AuthProvider'
import { listSessions, deleteSession, getSessionHistory, authHeaders, checkAuthStatus, getReasoning, type ServerSession, type ReasoningResponse } from '../lib/api'
import { MessageContent } from './MessageContent'
import { ReasoningView } from './ReasoningView'

interface Message {
  id: string
  role: 'user' | 'assistant'
  content: string
  skillUsed?: string
  executionId?: string
  streaming?: boolean
}

export function ChatPage() {
  const { user } = useAuth()
  const [messages, setMessages] = useState<Message[]>([])
  const [input, setInput] = useState('')
  const [loading, setLoading] = useState(false)
  const [sessionId, setSessionId] = useState<string>('')
  const [sessions, setSessions] = useState<ServerSession[]>([])
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const [progressText, setProgressText] = useState('')
  const messagesEnd = useRef<HTMLDivElement>(null)
  const abortRef = useRef<AbortController | null>(null)

  const refreshSessions = useCallback(async () => {
    try {
      const data = await listSessions()
      setSessions(data)
    } catch {
      // Auth errors handled by AuthProvider; non-critical failures show empty sidebar
    }
  }, [])

  useEffect(() => {
    refreshSessions()
  }, [refreshSessions])

  useEffect(() => {
    messagesEnd.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages])

  useEffect(() => {
    return () => {
      abortRef.current?.abort()
    }
  }, [])

  const loadSessionHistory = useCallback(async (sid: string) => {
    try {
      const historyMessages = await getSessionHistory(sid)
      const history: Message[] = historyMessages.map((m, i) => ({
        id: `${sid}-${i}`,
        role: m.role as 'user' | 'assistant',
        content: m.content,
        executionId: m.execution_id || undefined,
      }))
      setMessages(history)
      setSessionId(sid)
      setSidebarOpen(false)
    } catch {
      // Session not found or error
    }
  }, [])

  const handleDeleteSession = useCallback(async (e: React.MouseEvent, sid: string) => {
    e.stopPropagation()
    if (!confirm('Delete this session?')) return
    try {
      await deleteSession(sid)
      if (sid === sessionId) {
        setSessionId('')
        setMessages([])
      }
      refreshSessions()
    } catch {
      // Ignore delete errors
    }
  }, [sessionId, refreshSessions])

  const startNewSession = useCallback(() => {
    abortRef.current?.abort()
    setSessionId('')
    setMessages([])
    setSidebarOpen(false)
  }, [])

  /** SSE streaming via fetch + ReadableStream. Falls back to JSON on error. */
  const sendMessageStream = useCallback(async (
    messageText: string,
    assistantMsgId: string,
  ) => {
    const abort = new AbortController()
    abortRef.current = abort

    try {
      const headers = authHeaders()
      const resp = await fetch('/v1/chat/stream', {
        method: 'POST',
        headers,
        body: JSON.stringify({
          tenant_id: user?.tenant_id || 'default',
          user_id: user?.user_id || 'dev-user',
          session_id: sessionId || undefined,
          message: messageText,
        }),
        signal: abort.signal,
      })

      checkAuthStatus(resp)

      if (!resp.ok) {
        throw new Error(`HTTP ${resp.status}`)
      }

      const reader = resp.body?.getReader()
      if (!reader) throw new Error('No response body')

      const decoder = new TextDecoder()
      let buffer = ''
      let finalSessionId = sessionId

      while (true) {
        const { done, value } = await reader.read()
        if (done) break

        buffer += decoder.decode(value, { stream: true })

        const blocks = buffer.split('\n\n')
        buffer = blocks.pop() ?? ''

        for (const block of blocks) {
          let eventType = 'token'
          const dataLines: string[] = []

          for (const line of block.split('\n')) {
            if (line.startsWith('event: ')) {
              eventType = line.slice(7).trim()
            } else if (line.startsWith('data: ')) {
              dataLines.push(line.slice(6))
            }
          }
          const dataLine = dataLines.join('\n')

          if (!dataLine) continue

          try {
            const parsed = JSON.parse(dataLine)

            switch (eventType) {
              case 'progress': {
                const progType: string = parsed.type || ''
                const progContent: string = parsed.content || parsed.tool || ''

                // Token events feed message content. planning_token events
                // carry raw plan JSON — internal feedback only, not user-facing.
                if (progType === 'token') {
                  if (progContent) {
                    setMessages(prev =>
                      prev.map(m => {
                        if (m.id !== assistantMsgId) return m
                        return { ...m, content: m.content + progContent }
                      })
                    )
                  }
                  break
                }

                let status = ''
                switch (progType) {
                  case 'analyzing':
                    status = progContent || '正在分析请求...'
                    break
                  case 'loading_context':
                    status = progContent || '正在加载上下文...'
                    break
                  case 'planning':
                    status = '正在规划...'
                    break
                  case 'planning_generating':
                    status = '正在生成执行计划...'
                    break
                  case 'planning_complete':
                    status = '执行计划已生成，开始执行...'
                    break
                  case 'step_start':
                    status = progContent ? `正在执行: ${progContent}` : '开始执行...'
                    break
                  case 'step_complete':
                    status = progContent ? `已完成: ${progContent}` : '步骤完成'
                    break
                  case 'approval_required':
                    status = progContent ? `等待审批: ${progContent}` : '等待审批...'
                    break
                  case 'approval_granted':
                    status = progContent ? `审批通过: ${progContent}` : '审批通过'
                    break
                  default:
                    status = progContent || progType
                }
                if (status) {
                  setProgressText(status)
                }
                break
              }

              case 'session':
                if (parsed.session_id) {
                  finalSessionId = parsed.session_id
                  if (parsed.session_id !== sessionId) {
                    setSessionId(parsed.session_id)
                  }
                }
                break

              // Kept for backward compatibility with older backends that may send
              // SSE event: token directly. Current backend routes all tokens through
              // event: progress (handled in case 'progress' above).
              case 'token': {
                const token: string = parsed.token ?? ''
                setMessages(prev =>
                  prev.map(m => {
                    if (m.id !== assistantMsgId) return m
                    return { ...m, content: m.content + token }
                  })
                )
                break
              }

              case 'done': {
                const finalContent: string = parsed.message ?? ''
                setProgressText('')
                setMessages(prev =>
                  prev.map(m => {
                    if (m.id !== assistantMsgId) return m
                    return {
                      ...m,
                      content: finalContent || m.content || 'No response',
                      skillUsed: parsed.skill_used || undefined,
                      executionId: parsed.execution_id || undefined,
                      streaming: false,
                    }
                  })
                )
                if (finalSessionId) {
                  refreshSessions()
                }
                break
              }

              case 'error': {
                const errMsg: string = parsed.error || 'Unknown error'
                setMessages(prev =>
                  prev.map(m => {
                    if (m.id !== assistantMsgId) return m
                    return { ...m, content: `Error: ${errMsg}`, streaming: false }
                  })
                )
                break
              }
            }
          } catch {
            // Data is not JSON — for 'done' events, use raw text as content
            if (eventType === 'done' && dataLine) {
              setMessages(prev =>
                prev.map(m => {
                  if (m.id !== assistantMsgId) return m
                  return { ...m, content: dataLine, streaming: false }
                })
              )
              if (finalSessionId) {
                refreshSessions()
              }
            }
          }
        }
      }

      setMessages(prev =>
        prev.map(m => {
          if (m.id !== assistantMsgId || !m.streaming) return m
          return { ...m, streaming: false, content: m.content || 'No response' }
        })
      )
    } catch (err) {
      if ((err as Error).name === 'AbortError') return
      if ((err as Error).name === 'AuthError') {
        setMessages(prev =>
          prev.map(m => {
            if (m.id !== assistantMsgId) return m
            return { ...m, content: 'Session expired. Please log in again.', streaming: false }
          })
        )
        return
      }
      await sendMessageFallback(messageText, assistantMsgId)
    } finally {
      abortRef.current = null
    }
  }, [sessionId, user, refreshSessions])

  /** Fallback: call the non-streaming /v1/chat endpoint */
  const sendMessageFallback = useCallback(async (
    messageText: string,
    assistantMsgId: string,
  ) => {
    const headers = authHeaders()
    try {
      const resp = await fetch('/v1/chat', {
        method: 'POST',
        headers,
        body: JSON.stringify({
          tenant_id: user?.tenant_id || 'default',
          user_id: user?.user_id || 'dev-user',
          session_id: sessionId || undefined,
          message: messageText,
        }),
      })

      if (!resp.ok) {
        throw new Error(`HTTP ${resp.status}`)
      }
      const data = await resp.json()

      if (data.session_id && data.session_id !== sessionId) {
        setSessionId(data.session_id)
      }
      if (data.session_id) {
        refreshSessions()
      }

      setMessages(prev =>
        prev.map(m => {
          if (m.id !== assistantMsgId) return m
          return {
            ...m,
            content: data.message || 'No response',
            skillUsed: data.skill_used || undefined,
            executionId: data.execution_id || undefined,
            streaming: false,
          }
        })
      )
    } catch {
      setMessages(prev =>
        prev.map(m => {
          if (m.id !== assistantMsgId) return m
          return { ...m, content: 'Error: Failed to send message', streaming: false }
        })
      )
    }
  }, [sessionId, user, refreshSessions])

  const sendMessage = useCallback(async () => {
    if (!input.trim() || loading) return

    const userMessage: Message = {
      id: crypto.randomUUID(),
      role: 'user',
      content: input,
    }

    const assistantMsgId = crypto.randomUUID()
    const assistantMessage: Message = {
      id: assistantMsgId,
      role: 'assistant',
      content: '',
      streaming: true,
    }

    setMessages(prev => [...prev, userMessage, assistantMessage])
    const messageText = input
    setInput('')
    setLoading(true)
    setProgressText('')

    try {
      await sendMessageStream(messageText, assistantMsgId)
    } finally {
      setLoading(false)
    }
  }, [input, loading, sendMessageStream])

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      sendMessage()
    }
  }

  const formatTime = (isoStr: string) => {
    try {
      return new Date(isoStr).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    } catch {
      return ''
    }
  }

  return (
    <div className="chat-layout">
      {/* Sidebar */}
      <div className={`session-sidebar ${sidebarOpen ? 'open' : ''}`}>
        <div className="sidebar-header">
          <span>Sessions</span>
          <button className="btn-new-session" onClick={startNewSession}>+ New</button>
        </div>
        <div className="session-list">
          {sessions.length === 0 && (
            <div className="empty-sm">No sessions yet</div>
          )}
          {sessions.map(s => (
            <div key={s.session_id} className={`session-item ${s.session_id === sessionId ? 'active' : ''}`} onClick={() => loadSessionHistory(s.session_id)}>
              <span className="session-msg">{s.last_entry || `Session ${s.session_id.slice(0, 8)}`}</span>
              <span className="session-time">{formatTime(s.updated_at)} ({s.entry_count})</span>
              {s.session_id === sessionId && (
                <button className="session-delete" onClick={(e) => handleDeleteSession(e, s.session_id)} title="Delete session">&times;</button>
              )}
            </div>
          ))}
        </div>
      </div>

      {/* Chat Main */}
      <div className="chat-main">
        {/* Mobile toggle */}
        <button className="sidebar-toggle" onClick={() => setSidebarOpen(!sidebarOpen)}>
          {sidebarOpen ? '\u2715' : '\u2630'}
        </button>

        <div className="messages">
          {messages.length === 0 && (
            <div className="empty">Send a message to start chatting</div>
          )}
          {messages.map(msg => (
            <div key={msg.id} className={`message ${msg.role} ${msg.streaming ? 'streaming' : ''}`}>
              <div className="role">{msg.role}</div>
              {msg.streaming && progressText && (
                <div className="progress-status">{progressText}</div>
              )}
              <MessageContent content={msg.content} streaming={!!msg.streaming} />
              {msg.skillUsed && (
                <div className="skill-tag">Skill: {msg.skillUsed}</div>
              )}
              {msg.executionId && !msg.streaming && (
                <ReasoningPanel executionId={msg.executionId} />
              )}
            </div>
          ))}
          <div ref={messagesEnd} />
        </div>

        <div className="input-area">
          <textarea
            value={input}
            onChange={e => setInput(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="Type a message..."
            disabled={loading}
          />
          <button onClick={sendMessage} disabled={loading || !input.trim()}>
            Send
          </button>
        </div>
      </div>
    </div>
  )
}

function ReasoningPanel({ executionId }: { executionId: string }) {
  const [data, setData] = useState<ReasoningResponse | null>(null)
  const [visible, setVisible] = useState(false)
  const [debug, _setDebug] = useState(false)

  const loadReasoning = useCallback(async () => {
    if (data) {
      setVisible(!visible)
      return
    }
    try {
      const resp = await getReasoning(executionId, debug)
      setData(resp)
      setVisible(true)
    } catch {
      // Reasoning not available for this execution
    }
  }, [executionId, debug, data, visible])

  return (
    <div>
      <button className="btn-reasoning" onClick={loadReasoning}>
        {visible ? 'Hide Reasoning' : 'View Reasoning'}
      </button>
      {visible && data && (
        <ReasoningView data={data} debug={debug} />
      )}
    </div>
  )
}
