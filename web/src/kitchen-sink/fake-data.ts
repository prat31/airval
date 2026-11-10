/** Deterministic fake data for the kitchen sink (seeded so screenshots are stable). */
export function rng(seed = 42) {
  let s = seed >>> 0
  return () => {
    s = (s + 0x6d2b79f5) >>> 0
    let t = s
    t = Math.imul(t ^ (t >>> 15), t | 1)
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61)
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

const r = rng(7)
const clamp = (v: number, lo = 0, hi = 1) => Math.max(lo, Math.min(hi, v))
/** Approximate normal via sum of uniforms. */
const normal = (mean: number, sd: number, rand = r) => mean + sd * ((rand() + rand() + rand() + rand() + rand() + rand() - 3) / 1)

export const scoresA = Array.from({ length: 800 }, () => clamp(normal(0.78, 0.13)))
export const scoresB = Array.from({ length: 800 }, () => clamp(normal(0.84, 0.11)))
export const latencies = Array.from({ length: 2000 }, () => Math.max(40, Math.exp(normal(6.4, 0.45))))

const day = 86_400_000
const start = Date.UTC(2026, 9, 12)
const walk = (base: number, drift: number, n = 30, sd = 0.025) => {
  let v = base
  return Array.from({ length: n }, (_, i) => {
    v = clamp(v + drift + normal(0, sd))
    return { t: start + i * day, v: +v.toFixed(3) }
  })
}
export const trendSeries = [
  { name: 'Support / smoke', points: walk(0.82, 0.003) },
  { name: 'Support / full', points: walk(0.74, 0.004) },
  { name: 'Escalations', points: walk(0.66, 0.002) },
  { name: 'Refund policy', points: walk(0.88, -0.002) },
]

export const tags = ['billing', 'refunds', 'shipping', 'account', 'technical', 'returns', 'promotions', 'privacy', 'escalation']
export const passByTag = tags.map((label) => ({ label, value: +clamp(normal(0.8, 0.12)).toFixed(3), n: Math.round(40 + r() * 160) }))

export const evaluators = ['correctness', 'faithfulness', 'answer_relevance', 'conciseness', 'toxicity', 'json_schema']
export const heatValues = evaluators.map(() => tags.map(() => (r() < 0.05 ? null : +clamp(normal(0.76, 0.16)).toFixed(2))))

export const scatterPoints = Array.from({ length: 220 }, (_, i) => {
  const a = clamp(normal(0.72, 0.17))
  const b = clamp(a + normal(0.03, 0.09))
  return { id: `ex_${(1000 + i).toString(36)}`, a: +a.toFixed(3), b: +b.toFixed(3), label: `Example #${i + 1}` }
})

export const sparkData = Array.from({ length: 10 }, () => +clamp(normal(0.82, 0.04)).toFixed(3))

const suites = ['support-smoke', 'support-full', 'escalations', 'refund-policy', 'nightly-regression']
const targetsList = ['prod · gpt-4.1', 'staging · gpt-4.1', 'staging · claude', 'canary · v2']
const statuses = ['passed', 'passed', 'passed', 'failed', 'running', 'passed', 'cancelled', 'queued', 'passed', 'failed'] as const
const triggers = ['manual', 'schedule', 'webhook', 'api'] as const

export interface FakeRun {
  id: string
  status: (typeof statuses)[number]
  suite: string
  target: string
  trigger: (typeof triggers)[number]
  passRate: number
  score: number
  durationMs: number
  cost: number
  sha: string
  createdAt: number
}

export function makeRuns(n: number): FakeRun[] {
  const rr = rng(99)
  return Array.from({ length: n }, (_, i) => ({
    id: `run_${(0x4f2a1 + i * 7919).toString(36)}`,
    status: statuses[Math.floor(rr() * statuses.length)]!,
    suite: suites[Math.floor(rr() * suites.length)]!,
    target: targetsList[Math.floor(rr() * targetsList.length)]!,
    trigger: triggers[Math.floor(rr() * triggers.length)]!,
    passRate: +clamp(normal(0.86, 0.09, rr)).toFixed(3),
    score: +clamp(normal(0.8, 0.07, rr)).toFixed(3),
    durationMs: Math.round(4000 + rr() * 90000),
    cost: +(rr() * 4).toFixed(3),
    sha: Math.floor(rr() * 0xfffffff).toString(16).padStart(7, '0'),
    createdAt: Date.UTC(2026, 10, 10) - i * 3_600_000 * (1 + rr() * 3),
  }))
}

export const sampleJson = {
  id: 'res_01JC8Z3K',
  status: 'passed',
  latency_ms: 812,
  output: { answer: 'You can request a refund within 30 days of delivery.', citations: ['policy/refunds.md#window'] },
  usage: { tokens_in: 1432, tokens_out: 58, cost_usd: 0.00231 },
  scores: [
    { evaluator: 'correctness', value: 0.92, passed: true },
    { evaluator: 'faithfulness', value: 0.88, passed: true, reason: 'All claims are supported by the retrieved policy.' },
  ],
  cached: false,
  trace: null,
}

export const expectedText =
  'Customers can request a refund within 30 days of delivery. Refunds are issued to the original payment method within 5 business days.'
export const outputText =
  'You can request a refund within 30 days of delivery. Refunds go back to your original payment method, usually within 3–5 business days.'
