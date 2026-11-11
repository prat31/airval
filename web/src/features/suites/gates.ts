/**
 * Gate expressions as the visual builder sees them. The server's canonical form is text
 * (`mean(faithfulness) >= 0.85 and count(critical_fail) == 0`); the builder renders conditions to that text and the
 * API stores the parsed AST. Flat and/or expressions round-trip into the builder; anything else stays text-only.
 */

export type Cmp = '>=' | '>' | '<=' | '<' | '==' | '!='
export type Fn = 'mean' | 'median' | 'pass_rate' | 'count' | 'p50' | 'p90' | 'p95' | 'p99' | 'min' | 'max' | 'sum' | 'stddev' | 'regression_vs_baseline'
export type Metric = 'evaluator' | 'results' | 'latency_ms' | 'cost_usd' | 'tokens' | 'input_tokens' | 'output_tokens'
export type Selector = 'fail' | 'critical_fail' | 'major_fail' | 'minor_fail' | 'error' | 'pass' | 'results'

export interface Condition {
  fn: Fn
  metric: Metric
  /** Evaluator the metric reads (or scopes a count to). */
  evaluator_id?: string
  /** count(): which results to count. */
  selector?: Selector
  comparator: Cmp
  value: number
}

export type Joiner = 'and' | 'or'

export const fnOptions: { value: Fn; label: string }[] = [
  { value: 'mean', label: 'Mean' },
  { value: 'median', label: 'Median' },
  { value: 'pass_rate', label: 'Pass rate' },
  { value: 'count', label: 'Count' },
  { value: 'p50', label: 'p50' },
  { value: 'p90', label: 'p90' },
  { value: 'p95', label: 'p95' },
  { value: 'p99', label: 'p99' },
  { value: 'min', label: 'Min' },
  { value: 'max', label: 'Max' },
  { value: 'sum', label: 'Sum' },
  { value: 'stddev', label: 'Std. deviation' },
  { value: 'regression_vs_baseline', label: 'Change vs baseline' },
]

export const metricOptions: { value: Metric; label: string }[] = [
  { value: 'evaluator', label: 'Evaluator score' },
  { value: 'results', label: 'All results' },
  { value: 'latency_ms', label: 'Latency (ms)' },
  { value: 'cost_usd', label: 'Cost (USD)' },
  { value: 'tokens', label: 'Tokens' },
  { value: 'input_tokens', label: 'Input tokens' },
  { value: 'output_tokens', label: 'Output tokens' },
]

export const selectorOptions: { value: Selector; label: string }[] = [
  { value: 'fail', label: 'Failures' },
  { value: 'critical_fail', label: 'Critical failures' },
  { value: 'major_fail', label: 'Major failures' },
  { value: 'minor_fail', label: 'Minor failures' },
  { value: 'error', label: 'Errors' },
  { value: 'pass', label: 'Passes' },
  { value: 'results', label: 'All results' },
]

export const comparators: Cmp[] = ['>=', '>', '<=', '<', '==', '!=']
const NUMERIC_FNS: Fn[] = ['p50', 'p90', 'p95', 'p99', 'min', 'max', 'sum', 'stddev', 'median']

/** Sensible metric for a function, used when the user switches function. */
export function defaultMetricFor(fn: Fn): Metric {
  if (fn === 'count') return 'results'
  if (fn === 'pass_rate') return 'results'
  if (NUMERIC_FNS.includes(fn) && fn !== 'median' && fn !== 'stddev') return 'latency_ms'
  return 'evaluator'
}

export function newCondition(first?: string): Condition {
  return { fn: 'mean', metric: 'evaluator', evaluator_id: first, comparator: '>=', value: 0.8 }
}

export const presets = (firstEvaluator?: string): { label: string; cond: Condition }[] => [
  { label: 'Mean score ≥ 0.85', cond: { fn: 'mean', metric: 'evaluator', evaluator_id: firstEvaluator, comparator: '>=', value: 0.85 } },
  { label: 'Pass rate ≥ 95%', cond: { fn: 'pass_rate', metric: 'results', comparator: '>=', value: 0.95 } },
  { label: 'No critical failures', cond: { fn: 'count', metric: 'results', selector: 'critical_fail', comparator: '==', value: 0 } },
  { label: 'p95 latency < 2s', cond: { fn: 'p95', metric: 'latency_ms', comparator: '<', value: 2000 } },
  { label: 'No regression vs baseline', cond: { fn: 'regression_vs_baseline', metric: 'evaluator', evaluator_id: firstEvaluator, comparator: '>', value: -0.02 } },
]

type EvalName = (id?: string) => string

export function conditionText(c: Condition, evalName: EvalName): string {
  let args: string
  if (c.fn === 'count') args = [c.selector ?? 'fail', ...(c.evaluator_id ? [evalName(c.evaluator_id)] : [])].join(', ')
  else if (c.fn === 'pass_rate' && c.metric === 'results') args = 'all'
  else if (c.metric === 'evaluator' || c.fn === 'regression_vs_baseline') args = evalName(c.evaluator_id)
  else args = c.metric
  const value = c.fn === 'pass_rate' && c.value >= 0 && c.value <= 1 ? `${+(c.value * 100).toFixed(2)}%` : String(c.value)
  return `${c.fn}(${args}) ${c.comparator} ${value}`
}

export const conditionsText = (conds: Condition[], joiner: Joiner, evalName: EvalName) => conds.map((c) => conditionText(c, evalName)).join(` ${joiner} `)

/** Validation for a condition in the builder. Returns an error message or null. */
export function conditionProblem(c: Condition): string | null {
  if ((c.metric === 'evaluator' || c.fn === 'regression_vs_baseline') && !c.evaluator_id) return 'Choose an evaluator'
  if (!Number.isFinite(c.value)) return 'Enter a number'
  if (c.fn === 'count' && !c.selector) return 'Choose what to count'
  return null
}

/* --------------------------------------------------------------------------------- text → conditions */

export type ParseResult = { ok: true; joiner: Joiner; conds: Condition[] } | { ok: false; error: string }

const FNS = new Set<string>(fnOptions.map((f) => f.value))
const METRICS = new Set<string>(['latency_ms', 'cost_usd', 'tokens', 'input_tokens', 'output_tokens'])
const SELECTORS = new Set<string>(selectorOptions.map((s) => s.value))

/**
 * Parses flat `fn(arg) cmp number [and|or ...]`. Evaluators match by name (case-insensitive) or id.
 * Mixed and/or, parentheses and `not` are left to the server: the caller keeps them as text.
 */
export function parseGateText(text: string, evaluators: { id: string; name: string }[]): ParseResult {
  const src = text.trim()
  if (!src) return { ok: false, error: 'Write an expression, e.g. mean(faithfulness) >= 0.85' }
  if (/[()]\s*(and|or)\b|\bnot\b|^\(|\|\||&&/i.test(src.replace(/\w+\([^)]*\)/g, 'x'))) return { ok: false, error: 'Nested logic: edit as text' }
  const hasAnd = /\s+and\s+/i.test(src)
  const hasOr = /\s+or\s+/i.test(src)
  if (hasAnd && hasOr) return { ok: false, error: 'Mixing “and” and “or” is kept as text' }
  const joiner: Joiner = hasOr ? 'or' : 'and'
  const conds: Condition[] = []
  for (const part of src.split(/\s+(?:and|or)\s+/i)) {
    const m = /^([a-z0-9_]+)\(([^)]*)\)\s*(>=|<=|==|!=|=|>|<)\s*(-?\d+(?:\.\d+)?)(%?)$/i.exec(part.trim())
    if (!m) return { ok: false, error: `Can't read “${part.trim()}”. Expected fn(metric) comparator number` }
    const [, fnRaw, argRaw, cmp, numRaw, pct] = m
    const fn = fnRaw!.toLowerCase()
    if (!FNS.has(fn)) return { ok: false, error: `Unknown function “${fnRaw}”` }
    let value = Number(numRaw)
    if (pct) value /= 100
    const [first = '', second] = argRaw!.split(',').map((s) => s.trim())
    const byName = (n: string) => evaluators.find((e) => e.name.toLowerCase() === n.toLowerCase() || e.id === n)
    const cond: Condition = { fn: fn as Fn, metric: 'evaluator', comparator: (cmp === '=' ? '==' : cmp) as Cmp, value }
    if (fn === 'count') {
      if (!SELECTORS.has(first)) return { ok: false, error: `count() needs one of ${[...SELECTORS].join(', ')}` }
      cond.metric = 'results'
      cond.selector = first as Selector
      if (second) {
        const ev = byName(second)
        if (!ev) return { ok: false, error: `No evaluator named “${second}” in this suite` }
        cond.evaluator_id = ev.id
      }
    } else if (fn === 'pass_rate' && first.toLowerCase() === 'all') cond.metric = 'results'
    else if (METRICS.has(first.toLowerCase())) cond.metric = first.toLowerCase() as Metric
    else {
      const ev = byName(first)
      if (!ev) return { ok: false, error: first ? `No evaluator named “${first}” in this suite` : `${fn}() needs an argument` }
      cond.evaluator_id = ev.id
    }
    const problem = conditionProblem(cond)
    if (problem) return { ok: false, error: problem }
    conds.push(cond)
  }
  return { ok: true, joiner, conds }
}
