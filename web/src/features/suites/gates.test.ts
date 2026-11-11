import { describe, expect, it } from 'vitest'
import { conditionsText, parseGateText } from './gates'

const evals = [
  { id: 'e1', name: 'Faithfulness' },
  { id: 'e2', name: 'json' },
]

describe('gate text', () => {
  it('parses flat expressions and round-trips to canonical text', () => {
    const r = parseGateText('mean(faithfulness) >= 0.85 and count(critical_fail) == 0 and p95(latency_ms) < 2000', evals)
    expect(r.ok).toBe(true)
    if (!r.ok) return
    expect(r.conds).toHaveLength(3)
    const name = (id?: string) => evals.find((e) => e.id === id)?.name ?? '?'
    expect(conditionsText(r.conds, r.joiner, name)).toBe('mean(Faithfulness) >= 0.85 and count(critical_fail) == 0 and p95(latency_ms) < 2000')
  })
  it('understands percentages and pass_rate(all)', () => {
    const r = parseGateText('pass_rate(all) >= 95%', evals)
    expect(r.ok && r.conds[0]?.value).toBe(0.95)
  })
  it('reports unknown evaluators and mixed logic', () => {
    expect(parseGateText('mean(nope) > 1', evals)).toMatchObject({ ok: false })
    expect(parseGateText('mean(json) > 1 and mean(json) < 3 or mean(json) > 0', evals)).toMatchObject({ ok: false })
  })
})
