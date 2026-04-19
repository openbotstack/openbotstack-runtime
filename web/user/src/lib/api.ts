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

  return resp.json()
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
  } catch {
    return []
  }
}

export async function deleteSession(sessionId: string): Promise<void> {
  await apiCall<void>(`/v1/sessions/${sessionId}`, { method: 'DELETE' })
}

export async function getSessionHistory(sessionId: string): Promise<{ role: string; content: string }[]> {
  const data = await apiCall<{ session_id: string; messages: { role: string; content: string }[] }>(`/v1/sessions/${sessionId}/history`)
  return data.messages || []
}
