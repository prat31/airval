import { describe, expect, it } from 'vitest'
import type { RunEvent } from './api'
import { resultRows } from './live'

const ev = (data: Record<string, unknown>): RunEvent => ({ type: 'result', run_id: 'r1', at: '2026-01-01T00:00:00Z', data })

describe('resultRows', () => {
  it('unpacks batched {items, count} result events with safe defaults', () => {
    const data = { count: 2, items: [{ id: 'a', status: 'passed', scores: [{ evaluator_id: 'e', value: 1, passed: true }] }, { id: 'b', status: 'failed' }] }
    const rows = resultRows('r1', ev(data), data)
    expect(rows.map((r) => r.id)).toEqual(['a', 'b'])
    expect(rows[0]!.run_id).toBe('r1')
    expect(rows[0]!.scores[0]!.reason).toBe('')
    expect(rows[1]!.tags).toEqual([])
  })

  it('accepts a single result object and ignores junk', () => {
    expect(resultRows('r1', ev({ id: 'x', status: 'passed' }), { id: 'x', status: 'passed' })).toHaveLength(1)
    expect(resultRows('r1', ev({ count: 3 }), { count: 3 })).toHaveLength(0)
  })
})
