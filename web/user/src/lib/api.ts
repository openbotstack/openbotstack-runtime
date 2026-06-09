export class AuthError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'AuthError'
  }
}

export interface MeResponse {
  user_id: string
  tenant_id: string
  name: string
  role: string
}

// authHeaders returns the standard auth headers for API requests.
export function authHeaders(): Record<string, string> {
  const key = sessionStorage.getItem('obs_api_key')
  if (!key) throw new AuthError('No API key')
  return {
    'Content-Type': 'application/json',
    'X-API-Key': key,
  }
}

// checkAuthStatus handles 401/403 by clearing stored credentials.
export function checkAuthStatus(resp: Response): void {
  if (resp.status === 401 || resp.status === 403) {
    sessionStorage.removeItem('obs_api_key')
    sessionStorage.removeItem('obs_user_role')
    throw new AuthError('Authentication failed')
  }
}

export async function apiCall<T>(path: string, options?: RequestInit): Promise<T> {
  const headers = authHeaders()

  const resp = await fetch(path, {
    ...options,
    headers: {
      ...headers,
      ...options?.headers,
    },
  })

  checkAuthStatus(resp)

  if (!resp.ok) {
    const body = await resp.json().catch(() => ({ error: { message: `HTTP ${resp.status}` } }))
    throw new Error(body.error?.message || `HTTP ${resp.status}`)
  }

  const text = await resp.text()
  const trimmed = text.trim()
  if (!trimmed) return undefined as T
  return JSON.parse(trimmed)
}

export function getStoredKey(): string | null {
  return sessionStorage.getItem('obs_api_key')
}

export function storeAuth(key: string, role: string): void {
  sessionStorage.setItem('obs_api_key', key)
  sessionStorage.setItem('obs_user_role', role)
}

export function clearAuth(): void {
  sessionStorage.removeItem('obs_api_key')
  sessionStorage.removeItem('obs_user_role')
}

export async function validateKey(key: string): Promise<MeResponse> {
  const resp = await fetch('/v1/me', {
    headers: {
      'Content-Type': 'application/json',
      'X-API-Key': key,
    },
  })

  if (!resp.ok) {
    throw new Error('Invalid API key')
  }

  return resp.json()
}

// Server-side session types
export interface ServerSession {
  session_id: string
  tenant_id: string
  last_entry: string
  entry_count: number
  created_at: string
  updated_at: string
}

export async function listSessions(): Promise<ServerSession[]> {
  try {
    return await apiCall<ServerSession[]>('/v1/sessions')
  } catch (e) {
    if (e instanceof AuthError) throw e
    return []
  }
}

export async function deleteSession(sessionId: string): Promise<void> {
  await apiCall<void>(`/v1/sessions/${sessionId}`, { method: 'DELETE' })
}

export async function getSessionHistory(sessionId: string): Promise<{ role: string; content: string; execution_id?: string }[]> {
  const data = await apiCall<{ session_id: string; messages: { role: string; content?: string; contents?: { type: string; text: string }[]; execution_id?: string }[] }>(`/v1/sessions/${sessionId}/history`)
  return (data.messages || []).map(m => ({
    role: m.role,
    content: m.content || (m.contents?.map(c => c.text).join('') ?? ''),
    execution_id: m.execution_id,
  }))
}

// --- Reasoning API ---

export interface ReasoningEvent {
  step_id?: string
  type: 'plan' | 'thought' | 'tool_call' | 'observation' | 'decision'
  step_type?: 'tool' | 'skill' | 'llm'
  summary: string
  input?: unknown
  output?: unknown
  duration_ms?: number
  status?: 'completed' | 'failed'
  error?: string
  turn_number?: number
  plan_text?: string
  stop_reason?: string
  observations?: string[]
  children?: ReasoningEvent[]
}

export interface AuditEntry {
  trace_id: string
  step_id: string
  step_name: string
  step_type: string
  timestamp: string
  status: string
  error?: string
  duration_ms: number
}

export interface ReasoningResponse {
  execution_id: string
  plan_id?: string
  tree: ReasoningEvent
  text: string
  metrics?: {
    total_steps: number
    total_tool_calls: number
    total_llm_turns: number
    total_runtime_ms: number
  }
  stop_condition?: {
    stopped: boolean
    reason: string
    detail: string
  }
  debug?: {
    audit_trail: AuditEntry[]
  }
}

export async function getReasoning(executionId: string, debug?: boolean): Promise<ReasoningResponse> {
  const path = `/v1/execution/${encodeURIComponent(executionId)}/reasoning${debug ? '?debug=true' : ''}`
  return apiCall<ReasoningResponse>(path)
}

// --- Reasoning display helpers ---

// isSafeContent checks that reasoning content doesn't contain obvious
// injection vectors. The UI uses react-markdown which sanitizes HTML,
// but defense-in-depth requires server-side validation too.
export function isSafeContent(text: string): boolean {
  if (!text) return true
  // Block script tags and event handlers
  if (/<script[\s>]/i.test(text)) return false
  if (/\bon\w+\s*=/i.test(text)) return false
  if (/javascript:/i.test(text)) return false
  return true
}

// formatStepSummary produces a human-readable step label.
export function formatStepSummary(step: ReasoningEvent, index: number): string {
  const name = step.summary || 'Unknown step'
  const duration = step.duration_ms ? ` (${step.duration_ms}ms)` : ''
  return `Step ${index + 1}: ${name}${duration}`
}

// getConfidenceFromOutput extracts a confidence/score from tool output.
// Returns null if no confidence indicator found.
export function getConfidenceFromOutput(output: unknown): number | null {
  if (!output || typeof output !== 'object') return null
  const obj = output as Record<string, unknown>
  if (typeof obj.score === 'number') return obj.score
  if (typeof obj.confidence === 'number') return obj.confidence
  return null
}

// isUncertain checks if the output indicates low confidence.
export function isUncertain(output: unknown, threshold = 0.5): boolean {
  const conf = getConfidenceFromOutput(output)
  if (conf === null) return false
  return conf < threshold
}

// UNCERTAINTY_PHRASES are prepended to outputs with low confidence.
export const UNCERTAINTY_PHRASES = [
  '⚠️ This assessment has low confidence.',
  '⚠️ Results are uncertain — verify with additional data.',
  '⚠️ Insufficient data for a reliable conclusion.',
]

// getUncertaintyPhrase returns a warning phrase based on confidence level.
export function getUncertaintyPhrase(output: unknown): string {
  const conf = getConfidenceFromOutput(output)
  if (conf === null) return ''
  if (conf < 0.3) return UNCERTAINTY_PHRASES[2]
  if (conf < 0.5) return UNCERTAINTY_PHRASES[1]
  if (conf < 0.7) return UNCERTAINTY_PHRASES[0]
  return ''
}

// hasConflictMarker checks if tool output contains conflict indicators.
export function hasConflictMarker(output: unknown): boolean {
  if (!output || typeof output !== 'object') return false
  const obj = output as Record<string, unknown>
  return obj.conflict !== undefined || obj.mismatch !== undefined || obj.discrepancy !== undefined
}
