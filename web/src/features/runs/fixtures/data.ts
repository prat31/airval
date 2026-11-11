/**
 * DEV-ONLY fixture backend. Deterministic, realistic data for building and reviewing run screens
 * without a running engine. Never imported by production code paths (see ./index.ts).
 */
import type { components } from '@/lib/api/schema'

type S = components['schemas']
type Run = S['Run']
type Result = S['Result']
type RunAggregate = S['RunAggregate']

// ------------------------------------------------------------------ rng
function mulberry32(a: number) {
  return () => {
    a |= 0
    a = (a + 0x6d2b79f5) | 0
    let t = Math.imul(a ^ (a >>> 15), 1 | a)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}
function hash(s: string) {
  let h = 2166136261
  for (let i = 0; i < s.length; i++) h = Math.imul(h ^ s.charCodeAt(i), 16777619)
  return h >>> 0
}
const clamp01 = (v: number) => Math.max(0, Math.min(1, v))
function normal(r: () => number, mu = 0, sd = 1) {
  const u = 1 - r()
  const v = r()
  return mu + sd * Math.sqrt(-2 * Math.log(u)) * Math.cos(2 * Math.PI * v)
}
const seedRng = mulberry32(20261111)
function uuid(r: () => number = seedRng) {
  const h = () => Math.floor(r() * 16).toString(16)
  const s = (n: number) => Array.from({ length: n }, h).join('')
  return `0193${s(4)}-${s(4)}-7${s(3)}-${'89ab'[Math.floor(r() * 4)]}${s(3)}-${s(12)}`
}

const NOW = Date.now()
const iso = (t: number) => new Date(t).toISOString()
const DAY = 86_400_000

// ------------------------------------------------------------------ workspace
const userId = uuid()
export const orgs: S['Organization'][] = [
  { id: uuid(), name: 'Acme Inc.', slug: 'acme', role: 'owner', created_at: iso(NOW - 90 * DAY), updated_at: iso(NOW - 90 * DAY) },
  { id: uuid(), name: 'Personal', slug: 'personal', role: 'owner', created_at: iso(NOW - 60 * DAY), updated_at: iso(NOW - 60 * DAY) },
]
export const me: S['Me'] = {
  auth_method: 'session',
  user: { id: userId, email: 'ada@acme.dev', name: 'Ada Lovelace', avatar_url: '', created_at: iso(NOW - 90 * DAY) },
  orgs,
  csrf_token: 'fixture',
}

const mkProject = (org: S['Organization'], name: string, slug: string, color: string, description: string): S['Project'] => ({
  id: uuid(),
  org_id: org.id,
  name,
  slug,
  description,
  color,
  icon: '',
  settings: {},
  created_at: iso(NOW - 80 * DAY),
  updated_at: iso(NOW - 80 * DAY),
})
export const projects: S['Project'][] = [
  mkProject(orgs[0]!, 'Support Bot', 'support-bot', 'iris', 'Customer support assistant (RAG) — answers from the help center.'),
  mkProject(orgs[0]!, 'Search Rerank', 'search-rerank', 'aqua', 'Semantic search reranker'),
  mkProject(orgs[0]!, 'SQL Agent', 'sql-agent', 'orange', 'Text-to-SQL agent with tools'),
  mkProject(orgs[1]!, 'Sandbox', 'sandbox', 'blue', ''),
]
const main = projects[0]!

// ------------------------------------------------------------------ catalog (Support Bot)
const base = { org_id: main.org_id, project_id: main.id, created_at: iso(NOW - 70 * DAY), updated_at: iso(NOW - 3 * DAY) }

interface EvalDef {
  name: string
  kind: S['EvaluatorKind']
  category: S['EvaluatorCategory']
  mean: number
  sd: number
  threshold: number
  severity: S['Severity']
  judge?: boolean
}
const evalDefs: EvalDef[] = [
  { name: 'correctness', kind: 'correctness', category: 'model', mean: 0.82, sd: 0.14, threshold: 0.7, severity: 'critical', judge: true },
  { name: 'faithfulness', kind: 'faithfulness', category: 'model', mean: 0.87, sd: 0.11, threshold: 0.75, severity: 'major', judge: true },
  { name: 'answer_relevance', kind: 'answer_relevance', category: 'model', mean: 0.85, sd: 0.1, threshold: 0.7, severity: 'minor', judge: true },
  { name: 'conciseness', kind: 'conciseness', category: 'model', mean: 0.74, sd: 0.15, threshold: 0.6, severity: 'minor', judge: true },
  { name: 'no_pii', kind: 'no_pii', category: 'deterministic', mean: 0.985, sd: 0, threshold: 1, severity: 'critical' },
  { name: 'json_schema', kind: 'json_schema', category: 'deterministic', mean: 0.96, sd: 0, threshold: 1, severity: 'major' },
]
export const evaluators: S['Evaluator'][] = evalDefs.map((d) => ({
  ...base,
  id: uuid(),
  name: d.name,
  description: '',
  kind: d.kind,
  category: d.category,
  config: d.judge ? { judge: { provider: 'openai', model: 'gpt-4o-mini' } } : {},
  version: 1,
}))
const evalIds = evaluators.map((e) => e.id)

export const targets: S['Target'][] = [
  { ...base, id: uuid(), name: 'prod · gpt-4o', description: 'Production assistant', kind: 'openai' },
  { ...base, id: uuid(), name: 'staging · claude-sonnet', description: 'Candidate model', kind: 'anthropic' },
  { ...base, id: uuid(), name: 'support-gateway', description: 'HTTP gateway in front of the RAG service', kind: 'http' },
]

export const datasets: S['Dataset'][] = [
  { ...base, id: uuid(), name: 'support-golden', description: 'Curated golden set', example_count: 1200, has_draft_changes: false, latest_version: { id: uuid(), number: 14, created_at: iso(NOW - 4 * DAY) } },
  { ...base, id: uuid(), name: 'faithfulness-probe', description: 'Questions with tricky retrieved context', example_count: 400, has_draft_changes: true, latest_version: { id: uuid(), number: 3, created_at: iso(NOW - 9 * DAY) } },
  { ...base, id: uuid(), name: 'smoke', description: 'Fast sanity checks', example_count: 60, has_draft_changes: false, latest_version: { id: uuid(), number: 6, created_at: iso(NOW - 20 * DAY) } },
]

export const reviewQueues: S['ReviewQueue'][] = [
  { id: uuid(), project_id: main.id, name: 'Failure triage', description: '', config: { mode: 'failures' }, pending_count: 37, done_count: 210, created_at: base.created_at, updated_at: base.updated_at },
  { id: uuid(), project_id: main.id, name: 'Weekly spot check', description: '', config: { mode: 'sample', sample_pct: 2 }, pending_count: 8, done_count: 64, created_at: base.created_at, updated_at: base.updated_at },
]

const suiteEval = (ids: number[]): S['SuiteEvaluator'][] =>
  ids.map((i) => ({ evaluator_id: evalIds[i]!, weight: 1, threshold: evalDefs[i]!.threshold, severity: evalDefs[i]!.severity }))

export const suites: S['Suite'][] = [
  {
    ...base,
    id: uuid(),
    name: 'Regression',
    slug: 'regression',
    description: 'Full golden set on every deploy',
    config: { trials: 1, target_ids: [targets[0]!.id], datasets: [{ dataset_id: datasets[0]!.id }], evaluators: suiteEval([0, 1, 2, 3, 4, 5]) },
  },
  {
    ...base,
    id: uuid(),
    name: 'RAG faithfulness · nightly',
    slug: 'rag-faithfulness',
    description: 'Nightly faithfulness probe',
    config: { trials: 1, target_ids: [targets[0]!.id], datasets: [{ dataset_id: datasets[1]!.id }], evaluators: suiteEval([1, 2, 4]) },
  },
  {
    ...base,
    id: uuid(),
    name: 'Smoke',
    slug: 'smoke',
    description: 'Fast checks on every PR',
    config: { trials: 1, target_ids: [targets[2]!.id], datasets: [{ dataset_id: datasets[2]!.id }], evaluators: suiteEval([0, 4, 5]) },
  },
]
const suiteSize = [1200, 400, 60]

const gateTexts: Record<string, { text: string; ev?: number; fn: 'mean' | 'pass_rate' | 'count' | 'p95'; cmp: number }[]> = {
  [suites[0]!.id]: [
    { text: 'mean(correctness) >= 0.80', ev: 0, fn: 'mean', cmp: 0.8 },
    { text: 'pass_rate(all) >= 0.85', fn: 'pass_rate', cmp: 0.85 },
    { text: 'count(critical_fail) <= 15', fn: 'count', cmp: 15 },
    { text: 'p95(latency_ms) < 2500', fn: 'p95', cmp: 2500 },
  ],
  [suites[1]!.id]: [
    { text: 'mean(faithfulness) >= 0.85', ev: 1, fn: 'mean', cmp: 0.85 },
    { text: 'pass_rate(all) >= 0.80', fn: 'pass_rate', cmp: 0.8 },
  ],
  [suites[2]!.id]: [{ text: 'pass_rate(all) >= 0.90', fn: 'pass_rate', cmp: 0.9 }],
}

// ------------------------------------------------------------------ content
const TAGS = ['billing', 'refunds', 'shipping', 'account', 'technical', 'returns', 'privacy', 'escalation']
const QA: { q: string; a: string; ctx: string[]; tag: string }[] = [
  {
    tag: 'refunds',
    q: 'I was charged twice for my order #{n}. How do I get a refund for the duplicate?',
    a: 'Sorry about the double charge. Duplicate charges are refunded automatically within 3–5 business days. If it has been longer, reply with the order number and we will issue the refund manually.',
    ctx: ['Duplicate card authorizations are released automatically within 3–5 business days.', 'Agents can issue manual refunds from the Orders console after 5 business days.', 'Refunds go back to the original payment method.'],
  },
  {
    tag: 'shipping',
    q: 'My package was supposed to arrive on Monday but tracking hasn’t updated in four days. What should I do?',
    a: 'Tracking can pause while a parcel is between carrier hubs. If there is no update after 5 business days we will open a trace with the carrier and send a replacement or refund.',
    ctx: ['Carrier scans can pause for up to 5 business days during hub transfers.', 'After 5 business days without a scan, open a carrier trace.', 'Lost parcels qualify for a replacement or a full refund.'],
  },
  {
    tag: 'account',
    q: 'How do I change the email address on my account? I no longer have access to the old one.',
    a: 'Go to Settings → Account → Email and enter the new address. Because you cannot access the old inbox, we will verify you with your last order number and the last 4 digits of the card used.',
    ctx: ['Email changes require confirmation from the old inbox.', 'If the old inbox is unavailable, verify identity with the last order number and card last-4.'],
  },
  {
    tag: 'billing',
    q: 'Can I switch from monthly to annual billing mid-cycle, and will I be credited for the unused days?',
    a: 'Yes. Switching to annual applies immediately and the unused part of your monthly plan is prorated as a credit on the first annual invoice.',
    ctx: ['Plan changes take effect immediately.', 'Unused time on the current plan is prorated as account credit.'],
  },
  {
    tag: 'returns',
    q: 'What is your return window for opened electronics?',
    a: 'Opened electronics can be returned within 30 days of delivery in their original packaging. A 10% restocking fee applies unless the item is defective.',
    ctx: ['Return window: 30 days from delivery.', 'Opened electronics carry a 10% restocking fee unless defective.', 'Original packaging is required for electronics.'],
  },
  {
    tag: 'technical',
    q: 'The app keeps logging me out every few minutes on Android 15. Is this a known issue?',
    a: 'Yes — version 8.4.0 has a session bug on Android 15. Please update to 8.4.2 from the Play Store, which fixes it. Clearing the app cache also helps if the problem persists.',
    ctx: ['Known issue: v8.4.0 drops sessions on Android 15.', 'Fixed in v8.4.2 (released Nov 3).'],
  },
  {
    tag: 'privacy',
    q: 'Please delete all the personal data you have about me.',
    a: 'We can do that. Submit a deletion request from Settings → Privacy → Delete my data. We confirm by email and complete deletion within 30 days, as required by GDPR.',
    ctx: ['Data deletion requests are handled under GDPR Art. 17 within 30 days.', 'Requests are submitted from Settings → Privacy.'],
  },
  {
    tag: 'escalation',
    q: 'This is the third time I’m contacting you about the same broken blender. I want to speak to a manager.',
    a: 'I’m sorry this has taken three contacts. I’ve escalated your case to a senior specialist, who will call you within one business day, and I’ve flagged it for a replacement.',
    ctx: ['Escalate after the second unresolved contact on the same issue.', 'Senior specialists call back within one business day.'],
  },
]
const WRONG = [
  'Refunds are not possible once an order has shipped.',
  'Please contact your bank directly; we are unable to help with this.',
  'You can expect a response within 2–3 weeks.',
  'Our return window is 14 days for all products.',
]

interface ExampleFeature {
  qa: (typeof QA)[number]
  tags: string[]
  difficulty: number
  multiTurn: boolean
  tools: boolean
}
const exampleCache = new Map<number, ExampleFeature>()
function example(i: number): ExampleFeature {
  let e = exampleCache.get(i)
  if (!e) {
    const r = mulberry32(hash(`ex${i}`))
    const qa = QA[i % QA.length]!
    const extra = r() < 0.35 ? TAGS[Math.floor(r() * TAGS.length)]! : null
    e = { qa, tags: extra && extra !== qa.tag ? [qa.tag, extra] : [qa.tag], difficulty: r(), multiTurn: i % 9 === 4, tools: i % 5 === 2 }
    exampleCache.set(i, e)
  }
  return e
}

// ------------------------------------------------------------------ runs
interface RunMeta {
  run: Run
  shift: number[]
  evalIdx: number[]
  datasetSize: number
  /** Where completion stops for active runs (live). */
  failRate: number
}
export const runMeta = new Map<string, RunMeta>()
export const runs: Run[] = []

function addRun(opts: {
  suite: number
  target?: number
  ago: number
  status?: Run['status']
  shift?: number
  name?: string
  trigger?: Run['trigger_kind']
  labels?: Record<string, string>
  total?: number
  completed?: number
  parent?: string
  trials?: number
}): Run {
  const r = mulberry32(hash(`run${runs.length}${opts.ago}`))
  const suite = suites[opts.suite]!
  const evalIdx = (suite.config.evaluators ?? []).map((se) => evalIds.indexOf(se.evaluator_id))
  const datasetSize = suiteSize[opts.suite]!
  const total = opts.total ?? datasetSize * (opts.trials ?? 1)
  const status = opts.status ?? 'succeeded'
  const created = NOW - opts.ago
  const sha = Math.floor(r() * 0xfffffff).toString(16).padStart(7, '0')
  const run: Run = {
    id: uuid(r),
    org_id: main.org_id,
    project_id: main.id,
    suite_id: suite.id,
    target_id: targets[opts.target ?? (opts.suite === 2 ? 2 : 0)]!.id,
    target_version_id: uuid(r),
    dataset_version_id: datasets[opts.suite]!.latest_version!.id,
    name: opts.name ?? '',
    trigger_kind: opts.trigger ?? (['api', 'api', 'schedule', 'manual', 'webhook'] as const)[Math.floor(r() * 5)]!,
    trigger_ref: '',
    labels: opts.labels ?? { git_sha: sha, branch: r() < 0.7 ? 'main' : `feat/${['rerank', 'prompt-v7', 'tools'][Math.floor(r() * 3)]}` },
    status,
    total,
    completed: status === 'queued' ? 0 : (opts.completed ?? total),
    failed: 0,
    errored: 0,
    started_at: status === 'queued' ? null : iso(created + 2000),
    finished_at: status === 'queued' || status === 'running' ? null : iso(created + 2000 + total * (40 + r() * 30)),
    baseline_run_id: null,
    parent_run_id: opts.parent ?? null,
    gate_passed: null,
    cost_usd: 0,
    tokens_in: 0,
    tokens_out: 0,
    error: status === 'errored' ? 'target returned HTTP 503 for 12% of requests (circuit breaker opened)' : '',
    created_by: userId,
    created_at: iso(created),
    updated_at: iso(created),
  }
  const shift = evalDefs.map(() => (opts.shift ?? 0) + normal(r, 0, 0.015))
  runMeta.set(run.id, { run, shift, evalIdx, datasetSize, failRate: status === 'errored' ? 0.12 : 0.004 })
  runs.push(run)
  return run
}

// History: Regression every ~14h over 30 days with a regression dip and recovery; nightly + smoke.
for (let k = 46; k >= 1; k--) {
  const ago = k * 15 * 3_600_000 + 1_800_000
  const dip = k >= 18 && k <= 21 ? -0.07 : 0
  const trend = (46 - k) * 0.0012
  addRun({ suite: 0, ago, shift: trend + dip - 0.02, status: k === 30 ? 'canceled' : k === 12 ? 'errored' : 'succeeded', completed: k === 30 ? 640 : undefined })
}
for (let k = 28; k >= 1; k--) addRun({ suite: 1, ago: k * DAY + 3 * 3_600_000, shift: (28 - k) * 0.001 - 0.03, trigger: 'schedule' })
for (let k = 16; k >= 1; k--) addRun({ suite: 2, ago: k * 1.7 * DAY, shift: 0.02, trigger: 'webhook' })
addRun({ suite: 0, target: 1, ago: 5 * 3_600_000, shift: 0.035, name: 'claude-sonnet candidate', trigger: 'manual', labels: { git_sha: '4be1c09', branch: 'feat/claude', model: 'claude-sonnet' } })
addRun({ suite: 0, ago: 3 * 3_600_000, shift: 0.01, name: 'Load test · 100k', trigger: 'manual', total: 100_000, labels: { git_sha: '9f3a2d1', purpose: 'load-test' } })
addRun({ suite: 0, ago: 2 * 60_000, status: 'running', completed: 312, shift: 0.012, trigger: 'api', labels: { git_sha: 'c7e19aa', branch: 'main', pr: '482' } })
addRun({ suite: 2, ago: 20_000, status: 'queued', trigger: 'webhook', labels: { git_sha: 'c7e19aa', branch: 'main' } })
runs.reverse() // newest first

// Baselines: previous succeeded run of the same suite/target.
for (let i = 0; i < runs.length; i++) {
  const r = runs[i]!
  const prev = runs.slice(i + 1).find((p) => p.suite_id === r.suite_id && p.status === 'succeeded' && p.total <= 5000)
  r.baseline_run_id = prev?.id ?? null
}

// ------------------------------------------------------------------ results
interface Light {
  i: number
  status: Result['status']
  scores: (number | null)[]
  passed: (boolean | null)[]
  latency: number
  tokensIn: number
  tokensOut: number
  cost: number
  judgeCost: number[]
  cached: boolean
}
function light(meta: RunMeta, i: number): Light {
  const ex = example(i % meta.datasetSize)
  const r = mulberry32(hash(meta.run.id) ^ Math.imul(i + 1, 2654435761))
  const errored = r() < meta.failRate
  const scores: (number | null)[] = []
  const passed: (boolean | null)[] = []
  let failed = false
  for (const ei of meta.evalIdx) {
    const d = evalDefs[ei]!
    if (errored) {
      scores.push(null)
      passed.push(null)
      continue
    }
    let v: number
    if (d.sd === 0) v = r() < d.mean + meta.shift[ei]! * 0.3 - ex.difficulty * 0.02 ? 1 : 0
    else v = clamp01(normal(r, d.mean + meta.shift[ei]! + (0.5 - ex.difficulty) * 0.28, d.sd))
    v = Math.round(v * 1000) / 1000
    const p = v >= d.threshold
    scores.push(v)
    passed.push(p)
    if (!p && d.severity !== 'minor') failed = true
  }
  const tokensIn = Math.round(700 + ex.difficulty * 900 + r() * 200)
  const tokensOut = Math.round(60 + r() * 240)
  const cached = r() < 0.18
  const latency = cached ? 4 + r() * 8 : Math.max(120, Math.exp(normal(r, 6.65, 0.42)))
  return {
    i,
    status: errored ? 'errored' : failed ? 'failed' : 'passed',
    scores,
    passed,
    latency: Math.round(latency),
    tokensIn,
    tokensOut,
    cost: cached ? 0 : (tokensIn * 2.5 + tokensOut * 10) / 1e6,
    judgeCost: meta.evalIdx.map((ei) => (evalDefs[ei]!.judge && !errored ? 0.00011 + r() * 0.00004 : 0)),
    cached,
  }
}

const lightCache = new Map<string, Light[]>()
export function lights(runId: string): Light[] {
  const meta = runMeta.get(runId)
  if (!meta) return []
  const n = meta.run.status === 'running' || meta.run.status === 'canceled' ? meta.run.completed : meta.run.status === 'queued' ? 0 : meta.run.total
  let arr = lightCache.get(runId)
  if (!arr || arr.length < n) {
    arr = arr ?? []
    for (let i = arr.length; i < n; i++) arr.push(light(meta, i))
    lightCache.set(runId, arr)
  }
  return arr.length > n ? arr.slice(0, n) : arr
}

const resultId = (runId: string, i: number) => `${runId.slice(0, 24)}${i.toString(16).padStart(12, '0')}`
export const parseResultId = (id: string) => ({ prefix: id.slice(0, 24), i: parseInt(id.slice(24), 16) })
export function runForResultId(id: string) {
  const { prefix, i } = parseResultId(id)
  const run = runs.find((r) => r.id.startsWith(prefix))
  return run ? { run, i } : null
}

export function toResult(runId: string, l: Light, full = false): Result {
  const meta = runMeta.get(runId)!
  const exIdx = l.i % meta.datasetSize
  const ex = example(exIdx)
  const r = mulberry32(hash(`${runId}out${l.i}`))
  const correctness = l.scores[0] ?? 0
  const q = ex.qa.q.replace('{n}', String(48_000 + exIdx * 7))
  const output =
    l.status === 'errored'
      ? ''
      : correctness < 0.55
        ? `${WRONG[Math.floor(r() * WRONG.length)]} ${ex.qa.a.split('. ').slice(1).join('. ')}`.trim()
        : correctness < 0.8
          ? ex.qa.a.replace(/within (\d)/, (_, d) => `within ${Number(d) + 2}`).replace('Sorry about', 'Thanks for flagging')
          : ex.qa.a
  const scores: S['Score'][] = meta.evalIdx.map((ei, k) => {
    const d = evalDefs[ei]!
    const v = l.scores[k]
    const p = l.passed[k]
    return {
      evaluator_id: evalIds[ei]!,
      value: v,
      passed: p,
      label: v == null ? 'error' : p ? 'pass' : 'fail',
      reason:
        v == null
          ? 'Not scored: the target call errored.'
          : d.judge
            ? p
              ? `The answer is consistent with the reference and grounded in the retrieved passages. ${d.name === 'conciseness' ? 'It stays under 60 words without dropping required steps.' : 'No unsupported claims were found.'}`
              : `The answer ${d.name === 'faithfulness' ? 'introduces a claim that is not supported by any retrieved passage' : d.name === 'conciseness' ? 'repeats the apology and adds filler that does not help the customer' : 'contradicts the reference on a key fact (timeline or policy)'}. Score reflects ${Math.round((v ?? 0) * 100)}% rubric coverage.`
            : p
              ? 'Check passed.'
              : d.name === 'no_pii'
                ? 'Detected an email address in the output.'
                : 'Output did not match the response schema: missing required property "intent".',
      latency_ms: d.judge ? Math.round(400 + r() * 900) : 1,
      cost_usd: l.judgeCost[k] ?? 0,
    }
  })
  const res: Result = {
    id: resultId(runId, l.i),
    run_id: runId,
    example_id: `0193ex00-0000-7000-8000-${exIdx.toString(16).padStart(12, '0')}`,
    example_revision_id: `0193rv00-0000-7000-8000-${exIdx.toString(16).padStart(12, '0')}`,
    trial: Math.floor(l.i / meta.datasetSize),
    status: l.status,
    input: { question: q, customer_tier: ['free', 'plus', 'pro'][exIdx % 3] },
    expected: ex.qa.a,
    output,
    latency_ms: l.latency,
    ttft_ms: l.cached ? null : Math.round(l.latency * 0.32),
    tokens_in: l.tokensIn,
    tokens_out: l.tokensOut,
    cost_usd: l.cost,
    error: l.status === 'errored' ? 'POST https://api.openai.com/v1/chat/completions: 503 Service Unavailable (after 3 retries)' : '',
    cached: l.cached,
    tags: ex.tags,
    scores,
    created_at: iso(Date.parse(meta.run.started_at ?? meta.run.created_at) + l.i * 45),
  }
  if (full) {
    res.context = ex.qa.ctx
    const t0 = Date.parse(res.created_at)
    const ns = (ms: number) => Math.round(ms * 1e6)
    res.trace = [
      { kind: 'retrieval', name: 'help-center.search', input: { query: q, k: 4 }, output: ex.qa.ctx, started_at: iso(t0), duration: ns(l.latency * 0.12) },
      ...(ex.tools
        ? [
            { kind: 'llm' as const, name: 'gpt-4o (plan)', input: { messages: 2 }, output: { tool_call: 'lookup_order' }, started_at: iso(t0 + l.latency * 0.12), duration: ns(l.latency * 0.25) },
            { kind: 'tool' as const, name: 'lookup_order', input: { order_id: 48_000 + exIdx * 7 }, output: { status: 'delivered', charged: 2 }, started_at: iso(t0 + l.latency * 0.37), duration: ns(l.latency * 0.08) },
          ]
        : []),
      { kind: 'llm', name: 'gpt-4o', input: { messages: ex.tools ? 4 : 2 }, output: { tokens: l.tokensOut }, started_at: iso(t0 + l.latency * (ex.tools ? 0.45 : 0.12)), duration: ns(l.latency * (ex.tools ? 0.55 : 0.88)) },
    ]
    if (ex.tools) res.tool_calls = [{ id: 'call_1', name: 'lookup_order', arguments: { order_id: 48_000 + exIdx * 7 }, result: { status: 'delivered', charged: 2 } }]
    if (ex.multiTurn)
      res.transcript = [
        { role: 'system', content: 'You are Acme’s support assistant. Answer only from the help center.' },
        { role: 'user', content: q },
        { role: 'assistant', content: 'I can help with that. Could you confirm the email on the account?' },
        { role: 'user', content: 'Sure, it’s the one ending in @fastmail.com.' },
        { role: 'assistant', content: output || '(no response)' },
      ]
    res.request = {
      method: 'POST',
      url: 'https://api.openai.com/v1/chat/completions',
      headers: { 'Content-Type': 'application/json', Authorization: 'Bearer {{ secret "OPENAI_KEY" }}' },
      body: {
        model: 'gpt-4o-2024-08-06',
        temperature: 0.2,
        messages: [
          { role: 'system', content: 'You are Acme’s support assistant. Answer only from the help center.' },
          { role: 'user', content: `Context:\n${ex.qa.ctx.join('\n')}\n\nQuestion: ${q}` },
        ],
      },
    }
    res.response =
      l.status === 'errored'
        ? { status: 503, body: { error: { message: 'The server is overloaded. Please retry.' } } }
        : {
            status: 200,
            body: {
              id: `chatcmpl-${resultId(runId, l.i).slice(-10)}`,
              model: 'gpt-4o-2024-08-06',
              choices: [{ index: 0, finish_reason: 'stop', message: { role: 'assistant', content: output } }],
              usage: { prompt_tokens: l.tokensIn, completion_tokens: l.tokensOut },
            },
          }
  }
  return res
}

// ------------------------------------------------------------------ aggregates
function hist(values: number[], lo: number, hi: number, bins: number): S['Histogram'] {
  const counts = new Array(bins).fill(0)
  const w = (hi - lo) / bins || 1
  for (const v of values) counts[Math.min(bins - 1, Math.max(0, Math.floor((v - lo) / w)))]++
  return { edges: Array.from({ length: bins + 1 }, (_, i) => +(lo + i * w).toFixed(4)), counts }
}
function stats(values: number[]) {
  const s = [...values].sort((a, b) => a - b)
  const n = s.length
  const q = (p: number) => (n ? s[Math.min(n - 1, Math.floor(p * (n - 1)))]! : null)
  const sum = s.reduce((a, b) => a + b, 0)
  const mean = n ? sum / n : null
  const sd = n && mean != null ? Math.sqrt(s.reduce((a, b) => a + (b - mean) ** 2, 0) / n) : null
  return { n, mean, min: s[0] ?? null, max: s[n - 1] ?? null, p50: q(0.5), p90: q(0.9), p95: q(0.95), p99: q(0.99), stddev: sd, sum }
}

const aggCache = new Map<string, { n: number; items: RunAggregate[] }>()
export function aggregates(runId: string): RunAggregate[] {
  const meta = runMeta.get(runId)
  if (!meta) return []
  const ls = lights(runId)
  const cached = aggCache.get(runId)
  if (cached && cached.n === ls.length) return cached.items
  const items: RunAggregate[] = []
  const slices: [string, Light[]][] = [['', ls]]
  for (const t of TAGS) slices.push([`tag:${t}`, ls.filter((l) => example(l.i % meta.datasetSize).tags.includes(t))])
  for (const [slice, rows] of slices) {
    meta.evalIdx.forEach((ei, k) => {
      const vals = rows.map((l) => l.scores[k]).filter((v): v is number => v != null)
      const ps = rows.map((l) => l.passed[k]).filter((v): v is boolean => v != null)
      const st = stats(vals)
      items.push({
        run_id: runId,
        evaluator_id: evalIds[ei]!,
        metric: 'score',
        slice_key: slice,
        ...st,
        pass_rate: ps.length ? ps.filter(Boolean).length / ps.length : null,
        histogram: slice ? undefined : hist(vals, 0, 1, 20),
      })
      if (!slice && evalDefs[ei]!.judge) {
        const c = rows.map((l) => l.judgeCost[k]!)
        items.push({ run_id: runId, evaluator_id: evalIds[ei]!, metric: 'cost_usd', slice_key: '', ...stats(c) })
      }
    })
    const scored = rows.filter((l) => l.status !== 'errored' && l.status !== 'skipped')
    items.push({ run_id: runId, evaluator_id: null, metric: 'pass_rate', slice_key: slice, n: scored.length, mean: null, pass_rate: scored.length ? scored.filter((l) => l.status === 'passed').length / scored.length : null })
    if (!slice) {
      const lat = rows.filter((l) => l.status !== 'errored').map((l) => l.latency)
      const st = stats(lat)
      items.push({ run_id: runId, evaluator_id: null, metric: 'latency_ms', slice_key: '', ...st, histogram: hist(lat, 0, st.p99 ? st.p99 * 1.3 : 1, 40) })
      items.push({ run_id: runId, evaluator_id: null, metric: 'cost_usd', slice_key: '', ...stats(rows.map((l) => l.cost)) })
      items.push({ run_id: runId, evaluator_id: null, metric: 'tokens', slice_key: '', ...stats(rows.map((l) => l.tokensIn + l.tokensOut)) })
    }
  }
  aggCache.set(runId, { n: ls.length, items })
  return items
}

/** Fill in counters, summary and gate verdicts from the generated results. */
export function hydrate(run: Run): Run {
  const meta = runMeta.get(run.id)
  if (!meta) return run
  const ls = lights(run.id)
  if (!ls.length) return run
  const agg = aggregates(run.id)
  const all = agg.filter((a) => a.slice_key === '')
  const passRate = all.find((a) => a.metric === 'pass_rate')?.pass_rate ?? 0
  const scoreAggs = all.filter((a) => a.metric === 'score')
  const meanScore = scoreAggs.reduce((s, a) => s + (a.mean ?? 0), 0) / (scoreAggs.length || 1)
  const lat = all.find((a) => a.metric === 'latency_ms')
  const judge = all.filter((a) => a.metric === 'cost_usd' && a.evaluator_id).reduce((s, a) => s + (a.sum ?? 0), 0)
  const target = all.find((a) => a.metric === 'cost_usd' && !a.evaluator_id)?.sum ?? 0
  run.failed = ls.filter((l) => l.status === 'failed').length
  run.errored = ls.filter((l) => l.status === 'errored').length
  run.cost_usd = target + judge
  run.tokens_in = ls.reduce((s, l) => s + l.tokensIn, 0)
  run.tokens_out = ls.reduce((s, l) => s + l.tokensOut, 0)
  const start = Date.parse(run.started_at ?? run.created_at)
  const end = run.finished_at ? Date.parse(run.finished_at) : Date.now()
  const criticalFails = ls.filter((l) => meta.evalIdx.some((ei, k) => evalDefs[ei]!.severity === 'critical' && l.passed[k] === false)).length
  const done = run.status !== 'running' && run.status !== 'queued'
  const gates: S['GateResult'][] = (gateTexts[run.suite_id ?? ''] ?? []).map((g, gi) => {
    let value: number
    let passed: boolean
    if (g.fn === 'mean') {
      value = scoreAggs.find((a) => a.evaluator_id === evalIds[g.ev!])?.mean ?? 0
      passed = value >= g.cmp
    } else if (g.fn === 'pass_rate') {
      value = passRate
      passed = value >= g.cmp
    } else if (g.fn === 'count') {
      value = criticalFails
      passed = value <= Math.max(g.cmp, Math.round(ls.length * 0.0125))
    } else {
      value = lat?.p95 ?? 0
      passed = value < g.cmp
    }
    return { gate_id: `${run.suite_id}-g${gi}`, name: g.text, text: g.text, passed, value }
  })
  run.summary = {
    pass_rate: passRate,
    mean_score: meanScore,
    gates,
    duration_ms: end - start,
    throughput_per_min: ls.length / Math.max(1 / 60, (end - start) / 60_000),
    cache_hit_ratio: ls.filter((l) => l.cached).length / ls.length,
    rate_limited_ratio: 0.012,
    latency_p50_ms: lat?.p50 ?? undefined,
    latency_p95_ms: lat?.p95 ?? undefined,
  }
  if (done && run.status !== 'canceled') {
    run.gate_passed = gates.every((g) => g.passed)
    if (run.status === 'succeeded' && !run.gate_passed) run.status = 'failed'
  }
  return run
}

let hydrated = false
export function hydrateAll() {
  if (hydrated) return
  hydrated = true
  for (const r of runs) hydrate(r)
}

// ------------------------------------------------------------------ live engine
type Listener = (type: S['RunEvent']['type'], data: Record<string, unknown>) => void
const listeners = new Map<string, Set<Listener>>()
let seq = 0
export function subscribe(runId: string, fn: Listener) {
  const set = listeners.get(runId) ?? new Set()
  set.add(fn)
  listeners.set(runId, set)
  return () => set.delete(fn)
}
const emit = (runId: string, type: S['RunEvent']['type'], data: Record<string, unknown>) => {
  seq++
  listeners.get(runId)?.forEach((fn) => fn(type, data))
}
export const nextSeq = () => seq

const progress = (r: Run) => ({ status: r.status, completed: r.completed, failed: r.failed, errored: r.errored, total: r.total, cost_usd: r.cost_usd, tokens_in: r.tokens_in, tokens_out: r.tokens_out })

function tick() {
  for (const r of runs) {
    if (r.status === 'queued' && runs.filter((x) => x.status === 'running').length < 2 && Date.now() - Date.parse(r.created_at) > 6000) {
      r.status = 'running'
      r.started_at = iso(Date.now())
      emit(r.id, 'run.status', { status: 'running', started_at: r.started_at })
    }
    if (r.status !== 'running') continue
    const before = r.completed
    const step = Math.max(1, Math.round(r.total / 90))
    r.completed = Math.min(r.total, r.completed + step)
    const ls = lights(r.id)
    hydrate(r)
    for (let i = before; i < r.completed && i < before + 40; i++) emit(r.id, 'result', toResult(r.id, ls[i]!) as unknown as Record<string, unknown>)
    emit(r.id, 'run.progress', progress(r))
    if (r.completed >= r.total) {
      r.status = 'succeeded'
      r.finished_at = iso(Date.now())
      hydrate(r)
      emit(r.id, 'run.finished', { ...progress(r), status: r.status, gate_passed: r.gate_passed, summary: r.summary, finished_at: r.finished_at })
    }
  }
}
let timer: ReturnType<typeof setInterval> | undefined
export function startEngine() {
  timer ??= setInterval(tick, 300)
}

export function createRun(body: S['RunCreate']): Run[] {
  const si = suites.findIndex((s) => s.id === body.suite_id)
  if (si < 0) throw new Error('unknown suite')
  const targetIds = body.target_ids?.length ? body.target_ids : (suites[si]!.config.target_ids ?? [targets[0]!.id])
  const size = suiteSize[si]!
  const sampled = body.sampling?.mode === 'random' ? (body.sampling.n ?? Math.round(size * (body.sampling.percent ?? 100) / 100)) : size
  return targetIds.map((tid) => {
    const r = addRun({
      suite: si,
      target: Math.max(0, targets.findIndex((t) => t.id === tid)),
      ago: 0,
      status: 'queued',
      name: body.name,
      trigger: 'manual',
      labels: body.labels ?? {},
      total: Math.min(size, sampled) * (body.trials ?? 1),
      shift: 0.015,
    })
    runs.unshift(runs.pop()!)
    r.created_at = iso(Date.now() - 5000)
    r.baseline_run_id = body.baseline_run_id ?? runs.find((p) => p.id !== r.id && p.suite_id === r.suite_id && p.status === 'succeeded')?.id ?? null
    return r
  })
}

export function childRun(parent: Run, kind: 'rerun' | 'rescore'): Run {
  const si = suites.findIndex((s) => s.id === parent.suite_id)
  const r = addRun({
    suite: Math.max(0, si),
    target: Math.max(0, targets.findIndex((t) => t.id === parent.target_id)),
    ago: 0,
    status: 'queued',
    name: kind === 'rerun' ? `Re-run failed of ${parent.name || parent.id.slice(-7)}` : `Re-score of ${parent.name || parent.id.slice(-7)}`,
    trigger: 'manual',
    labels: parent.labels,
    total: kind === 'rerun' ? Math.max(1, parent.failed + parent.errored) : parent.total,
    parent: parent.id,
  })
  runs.unshift(runs.pop()!)
  r.created_at = iso(Date.now() - 5000)
  r.baseline_run_id = parent.id
  return r
}

// ------------------------------------------------------------------ comparison
export function compare(runIds: string[], evaluatorId?: string, limit = 50): S['ComparisonReport'] {
  const rs = runIds.map((id) => runs.find((r) => r.id === id)).filter((r): r is Run => !!r)
  const metas = rs.map((r) => runMeta.get(r.id)!)
  const evIds = [...new Set(metas.flatMap((m) => m.evalIdx.map((ei) => evalIds[ei]!)))]
  const common = evIds.filter((id) => metas.every((m) => m.evalIdx.some((ei) => evalIds[ei] === id)))
  const evId = evaluatorId && common.includes(evaluatorId) ? evaluatorId : common[0]
  const aggs = rs.map((r) => aggregates(r.id).filter((a) => a.slice_key === ''))
  const row = (key: string, label: string, pick: (a: RunAggregate[]) => { n: number; mean: number | null; pass_rate: number | null }, evaluator_id?: string): S['ScorecardRow'] => {
    const vals = aggs.map(pick)
    return {
      key,
      label,
      evaluator_id,
      cells: vals.map((v, i) => ({
        run_id: rs[i]!.id,
        n: v.n,
        mean: v.mean,
        pass_rate: v.pass_rate,
        delta_mean: i && v.mean != null && vals[0]!.mean != null ? v.mean - vals[0]!.mean : null,
        delta_pass_rate: i && v.pass_rate != null && vals[0]!.pass_rate != null ? v.pass_rate - vals[0]!.pass_rate : null,
      })),
    }
  }
  const scorecard: S['ScorecardRow'][] = [
    row('pass_rate', 'Pass rate', (a) => {
      const x = a.find((y) => y.metric === 'pass_rate')
      return { n: x?.n ?? 0, mean: null, pass_rate: x?.pass_rate ?? null }
    }),
    ...evIds.map((id) =>
      row(id, evaluators.find((e) => e.id === id)!.name, (a) => {
        const x = a.find((y) => y.metric === 'score' && y.evaluator_id === id)
        return { n: x?.n ?? 0, mean: x?.mean ?? null, pass_rate: x?.pass_rate ?? null }
      }, id),
    ),
    row('latency_p95', 'Latency p95 (ms)', (a) => {
      const x = a.find((y) => y.metric === 'latency_ms')
      return { n: x?.n ?? 0, mean: x?.p95 ?? null, pass_rate: null }
    }),
    row('cost_usd', 'Cost (USD)', (a) => {
      const sum = a.filter((y) => y.metric === 'cost_usd').reduce((s, y) => s + (y.sum ?? 0), 0)
      return { n: 0, mean: sum, pass_rate: null }
    }),
  ]
  // Per-example deltas on the ranking evaluator (trial 0, aligned by dataset index).
  const ls = rs.map((r) => lights(r.id))
  const ks = metas.map((m) => m.evalIdx.findIndex((ei) => evalIds[ei] === evId))
  const n = Math.min(...metas.map((m) => m.datasetSize), ...ls.map((l) => l.length))
  const deltas: S['ExampleDelta'][] = []
  const scatter: NonNullable<S['ComparisonReport']['scatter']> = []
  let wins = 0
  let ties = 0
  let losses = 0
  for (let i = 0; i < n; i++) {
    const vals = ls.map((l, j) => l[i]!.scores[ks[j]!] ?? null)
    const a = vals[0]
    const b = vals[vals.length - 1]
    if (a == null || b == null) continue
    const d = b - a
    if (scatter.length < 3000) scatter.push({ example_id: `ex${i}`, a, b })
    if (d > 0.05) wins++
    else if (d < -0.05) losses++
    else ties++
    deltas.push({ example_id: `ex${i}`, delta: d, cells: [] })
  }
  const detail = (d: S['ExampleDelta']): S['ExampleDelta'] => {
    const i = Number(d.example_id.slice(2))
    const full = rs.map((r, j) => toResult(r.id, ls[j]![i]!))
    return {
      example_id: full[0]!.example_id,
      input: full[0]!.input,
      expected: full[0]!.expected,
      delta: d.delta,
      cells: full.map((res, j) => ({ run_id: rs[j]!.id, result_id: res.id, value: res.scores[ks[j]!]?.value ?? null, passed: res.scores[ks[j]!]?.passed ?? null, output: res.output })),
    }
  }
  deltas.sort((x, y) => x.delta - y.delta)
  const regressions = deltas.filter((d) => d.delta < -0.02).slice(0, limit).map(detail)
  const improvements = deltas.filter((d) => d.delta > 0.02).reverse().slice(0, limit).map(detail)
  return { runs: rs, evaluator_id: evId, scorecard, regressions, improvements, scatter, pairwise: { wins, ties, losses } }
}

// ------------------------------------------------------------------ trends
export function trends(q: { suite_id?: string; evaluator_id?: string; metric?: string; from?: string }): S['Trend'] {
  const metric = (q.metric ?? 'pass_rate') as S['TrendMetric']
  const from = q.from ? Date.parse(q.from) : 0
  const done = runs.filter((r) => r.summary && r.status !== 'running' && r.status !== 'queued' && r.status !== 'canceled' && Date.parse(r.created_at) >= from && r.total <= 5000)
  const pts = (rs: Run[], v: (r: Run) => number | null) => rs.slice().reverse().map((r) => ({ t: r.created_at, value: v(r), run_id: r.id, n: r.total }))
  if (metric === 'score_mean') {
    const rs = done.filter((r) => !q.suite_id || r.suite_id === q.suite_id)
    const ids = q.evaluator_id ? [q.evaluator_id] : evalIds
    return {
      metric,
      series: ids.map((id) => ({
        key: id,
        label: evaluators.find((e) => e.id === id)?.name ?? id,
        evaluator_id: id,
        points: pts(rs, (r) => aggregates(r.id).find((a) => a.metric === 'score' && a.evaluator_id === id && a.slice_key === '')?.mean ?? null).filter((p) => p.value != null),
      })),
    }
  }
  return {
    metric,
    series: suites
      .filter((s) => !q.suite_id || s.id === q.suite_id)
      .map((s) => ({ key: s.id, label: s.name, suite_id: s.id, points: pts(done.filter((r) => r.suite_id === s.id), (r) => r.summary?.pass_rate ?? null) })),
  }
}
