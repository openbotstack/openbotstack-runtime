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
  const key = sessionStorage.getItem('obs_admin_key')
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
    sessionStorage.removeItem('obs_admin_key')
    sessionStorage.removeItem('obs_admin_role')
    throw new AuthError('Authentication failed')
  }

  if (!resp.ok) {
    const body = await resp.json().catch(() => ({ error: { message: `HTTP ${resp.status}` } }))
    throw new Error(body.error?.message || `HTTP ${resp.status}`)
  }

  return resp.json()
}

export function getStoredKey(): string | null {
  return sessionStorage.getItem('obs_admin_key')
}

export function storeAuth(key: string, role: string): void {
  sessionStorage.setItem('obs_admin_key', key)
  sessionStorage.setItem('obs_admin_role', role)
}

export function clearAuth(): void {
  sessionStorage.removeItem('obs_admin_key')
  sessionStorage.removeItem('obs_admin_role')
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
