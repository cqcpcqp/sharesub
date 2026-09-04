import type { APIError } from '../types'

const tokenKey = 'sharesub_session'

export function sessionToken(): string { return localStorage.getItem(tokenKey) ?? '' }
export function setSessionToken(token: string): void { localStorage.setItem(tokenKey, token) }
export function clearSessionToken(): void { localStorage.removeItem(tokenKey) }

export async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  if (!(init.body instanceof FormData)) headers.set('Content-Type', 'application/json')
  const token = sessionToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)
  const controller = new AbortController()
  const abortFromCaller = () => controller.abort(init.signal?.reason)
  if (init.signal?.aborted) abortFromCaller()
  else init.signal?.addEventListener('abort', abortFromCaller, { once: true })
  const timeout = setTimeout(() => controller.abort(new DOMException('请求超时', 'TimeoutError')), 120_000)
  try {
    const response = await fetch(path, { ...init, headers, signal: controller.signal })
    const body = await response.json() as T | APIError
    if (!response.ok) {
      const error = (body as APIError).error
      const retryAfter = Number.parseInt(response.headers.get('Retry-After') ?? '', 10)
      throw new APIRequestError(response.status, error.code, error.message, Number.isFinite(retryAfter) ? retryAfter : null)
    }
    return body as T
  } finally {
    clearTimeout(timeout)
    init.signal?.removeEventListener('abort', abortFromCaller)
  }
}

export class APIRequestError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly retryAfterSeconds: number | null = null,
  ) {
    super(message)
    this.name = 'APIRequestError'
  }
}
