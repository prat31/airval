import createClient, { type Middleware } from 'openapi-fetch'
import type { paths } from './schema'

/** Normalized API error. Every failed request rejects with this shape. */
export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly details?: unknown
  readonly requestId?: string

  constructor(init: { status: number; code: string; message: string; details?: unknown; requestId?: string }) {
    super(init.message)
    this.name = 'ApiError'
    this.status = init.status
    this.code = init.code
    this.details = init.details
    this.requestId = init.requestId
  }

  get isUnauthorized() {
    return this.status === 401
  }
  get isForbidden() {
    return this.status === 403
  }
  get isNotFound() {
    return this.status === 404
  }
}

const CSRF_COOKIE = 'airval_csrf'
const CSRF_HEADER = 'X-CSRF-Token'
const SAFE_METHODS = new Set(['GET', 'HEAD', 'OPTIONS'])

export function readCookie(name: string): string | undefined {
  if (typeof document === 'undefined') return undefined
  const match = document.cookie.split('; ').find((c) => c.startsWith(`${name}=`))
  return match ? decodeURIComponent(match.slice(name.length + 1)) : undefined
}

const csrfMiddleware: Middleware = {
  onRequest({ request }) {
    if (!SAFE_METHODS.has(request.method)) {
      const token = readCookie(CSRF_COOKIE)
      if (token) request.headers.set(CSRF_HEADER, token)
    }
    return request
  },
}

/** Turn any error body (problem+json, {error:{code,message}}, plain text) into an ApiError. */
export async function toApiError(response: Response): Promise<ApiError> {
  const requestId = response.headers.get('X-Request-Id') ?? undefined
  let body: unknown
  const text = await response.text().catch(() => '')
  try {
    body = text ? JSON.parse(text) : undefined
  } catch {
    body = text
  }
  const obj = (body && typeof body === 'object' ? body : {}) as Record<string, unknown>
  const nested = (obj.error && typeof obj.error === 'object' ? obj.error : obj) as Record<string, unknown>
  const message =
    (typeof nested.message === 'string' && nested.message) ||
    (typeof obj.detail === 'string' && obj.detail) ||
    (typeof obj.title === 'string' && obj.title) ||
    (typeof body === 'string' && body) ||
    response.statusText ||
    `Request failed with status ${response.status}`
  const code = (typeof nested.code === 'string' && nested.code) || `http_${response.status}`
  return new ApiError({ status: response.status, code, message, details: nested.details ?? body, requestId })
}

const errorMiddleware: Middleware = {
  async onResponse({ response }) {
    if (!response.ok) throw await toApiError(response.clone())
    return response
  },
}

export const api = createClient<paths>({
  // Paths in schema.d.ts already carry the /v1 prefix.
  baseUrl: '',
  credentials: 'include',
  headers: { Accept: 'application/json' },
})
api.use(csrfMiddleware, errorMiddleware)

/**
 * Unwrap an openapi-fetch result: returns `data`, throws ApiError otherwise.
 * Use inside TanStack Query `queryFn`/`mutationFn`:
 *   queryFn: () => unwrap(api.GET('/v1/projects/{project}', { params: { path: { project } } }))
 */
export async function unwrap<T>(promise: Promise<{ data?: T; error?: unknown; response: Response }>): Promise<T> {
  const { data, error, response } = await promise
  if (error !== undefined || !response.ok) {
    if (error instanceof ApiError) throw error
    throw new ApiError({
      status: response.status,
      code: `http_${response.status}`,
      message: response.statusText || 'Request failed',
      details: error,
    })
  }
  return data as T
}
