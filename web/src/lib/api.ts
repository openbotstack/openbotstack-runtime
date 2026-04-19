// Shared API fetch wrapper for authenticated requests.
// All admin API calls go through this function to ensure X-API-Key header is set.

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

export async function apiCall<T>(path: string, options?: RequestInit): Promise<T> {
  const key = sessionStorage.getItem('obs_api_key')
  if (!key) throw new AuthError('No API key')

  const resp = await fetch(path, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      'X-API-Key': key,
      ...options?.headers,
    },
  })

  if (resp.status === 401 || resp.status === 403) {
    sessionStorage.removeItem('obs_api_key')
    sessionStorage.removeItem('obs_user_role')
    throw new AuthError('Authentication failed')
  }

  if (!resp.ok) {
    const body = await resp.json().catch(() => ({ error: { message: `HTTP ${resp.status}` } }))
    throw new Error(body.error?.message || `HTTP ${resp.status}`)
  }

  return resp.json()
}

export function getStoredKey(): string | null {
  return sessionStorage.getItem('obs_api_key')
}

export function getStoredRole(): string | null {
  return sessionStorage.getItem('obs_user_role')
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
