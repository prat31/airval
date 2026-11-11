/**
 * Shared API plumbing for the configuration screens.
 *
 * The generated paths already carry the `/v1` prefix, so this client uses an empty base URL.
 * (Same CSRF + error normalization as `@/lib/api/client`.)
 */
import createClient, { type Middleware } from 'openapi-fetch'
import { ApiError, readCookie, toApiError } from '@/lib/api/client'
import type { components, paths } from '@/lib/api/schema'

export { ApiError, unwrap } from '@/lib/api/client'
export type S = components['schemas']
/** Loosely-typed JSON object (configs are open-ended by design). */
export type Obj = Record<string, unknown>

const SAFE = new Set(['GET', 'HEAD', 'OPTIONS'])

const csrf: Middleware = {
  onRequest({ request }) {
    if (!SAFE.has(request.method)) {
      const token = readCookie('airval_csrf')
      if (token) request.headers.set('X-CSRF-Token', token)
    }
    return request
  },
}

const errors: Middleware = {
  async onResponse({ response }) {
    if (!response.ok) throw await toApiError(response.clone())
    return response
  },
}

export const http = createClient<paths>({ baseUrl: '', credentials: 'include', headers: { Accept: 'application/json' } })
http.use(csrf, errors)

/** Raw fetch with CSRF + ApiError normalization (multipart uploads, file downloads, endpoints not yet in the schema). */
export async function rawFetch(url: string, init: RequestInit = {}): Promise<Response> {
  const method = (init.method ?? 'GET').toUpperCase()
  const headers = new Headers(init.headers)
  if (!SAFE.has(method)) {
    const token = readCookie('airval_csrf')
    if (token) headers.set('X-CSRF-Token', token)
  }
  const res = await fetch(url, { ...init, headers, credentials: 'include' })
  if (!res.ok) throw await toApiError(res)
  return res
}

export async function rawJson<T>(url: string, init: RequestInit & { json?: unknown } = {}): Promise<T> {
  const { json, ...rest } = init
  const headers = new Headers(rest.headers)
  headers.set('Accept', 'application/json')
  if (json !== undefined) headers.set('Content-Type', 'application/json')
  const res = await rawFetch(url, { ...rest, headers, body: json !== undefined ? JSON.stringify(json) : rest.body })
  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

export function errorMessage(err: unknown, fallback = 'Something went wrong'): string {
  return err instanceof ApiError ? err.message : err instanceof Error ? err.message : fallback
}

/** Save a Blob as a file download. */
export function saveBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

/** Fetch every page of a cursor-paginated list (capped). */
export async function fetchAll<T>(page: (cursor?: string) => Promise<{ items: T[]; next_cursor?: string | null }>, cap = 5000): Promise<T[]> {
  const out: T[] = []
  let cursor: string | undefined
  do {
    const res = await page(cursor)
    out.push(...res.items)
    cursor = res.next_cursor ?? undefined
  } while (cursor && out.length < cap)
  return out
}

export const isUuid = (s: string) => /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(s)
