import { describe, expect, it } from 'vitest'
import { ApiError, toApiError } from './client'

describe('toApiError', () => {
  it('reads {error:{code,message}} bodies', async () => {
    const res = new Response(JSON.stringify({ error: { code: 'not_found', message: 'Suite not found' } }), {
      status: 404,
      headers: { 'X-Request-Id': 'req_1' },
    })
    const err = await toApiError(res)
    expect(err).toBeInstanceOf(ApiError)
    expect(err.code).toBe('not_found')
    expect(err.message).toBe('Suite not found')
    expect(err.requestId).toBe('req_1')
    expect(err.isNotFound).toBe(true)
  })

  it('reads problem+json bodies', async () => {
    const err = await toApiError(new Response(JSON.stringify({ title: 'Forbidden', detail: 'Editors only' }), { status: 403 }))
    expect(err.message).toBe('Editors only')
    expect(err.code).toBe('http_403')
    expect(err.isForbidden).toBe(true)
  })

  it('falls back to plain text', async () => {
    const err = await toApiError(new Response('upstream timeout', { status: 502 }))
    expect(err.message).toBe('upstream timeout')
  })
})
