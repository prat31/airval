import { describe, expect, it } from 'vitest'
import { describeCron, previewCron } from './cron-input'

describe('previewCron', () => {
  it('lists the next fire times in order', () => {
    const from = new Date('2026-11-10T10:00:00Z')
    const p = previewCron('0 9 * * 1-5', 'UTC', 3, from)
    expect(p.valid).toBe(true)
    expect(p.next.map((d) => d.toISOString())).toEqual([
      '2026-11-11T09:00:00.000Z',
      '2026-11-12T09:00:00.000Z',
      '2026-11-13T09:00:00.000Z',
    ])
    expect(p.description).toBe('Weekdays at 09:00')
  })

  it('rejects malformed expressions with a helpful message', () => {
    expect(previewCron('61 * * *').valid).toBe(false)
    expect(previewCron('61 * * *').error).toMatch(/5 fields/)
    expect(previewCron('61 * * * *').valid).toBe(false)
  })

  it('describes common shapes', () => {
    expect(describeCron('*/15 * * * *')).toBe('Every 15 minutes')
    expect(describeCron('0 8 * * 1')).toBe('Every Monday at 08:00')
    expect(describeCron('5 4 1,15 * *')).toBeUndefined()
  })
})
