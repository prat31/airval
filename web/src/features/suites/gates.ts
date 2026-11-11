/**
 * Gate expressions: the visual builder's model, AST conversion, human-readable text and a text parser.
 * The server stores the AST (`S['GateExpr']`); text is derived and also accepted as input for power users.
 */
import type { S } from '../shared/api'

export type Cmp = '>=' | '>' | '<=' | '<' | '==' | '!='
export type Fn = NonNullable<S['GateExpr']['fn']>
export type Metric = NonNullable<S['GateExpr']['metric']>
export type Selector = NonNullable<S['GateExpr']['selector']>

export interface Condition {
  fn: Fn
  metric: Metric
  evaluator_id?: string
  selector?: Selector
  tag?: string
  comparator: Cmp
  value: number
}

export type Joiner = 'and' | 'or'

export const fnOptions: { value: Fn; label: string; hint: string }[] = [
  { value: 'mean', label: 'Mean', hint: 'Average score' },
  { value: 'pass_rate', label: 'Pass rate', hint: 'Share of results that pass' },
  { value: 'count', label: 'Count', hint: 'Number of matching results' },
  { value: 'p50', label: 'p50', hint: 'Median' },
  { value: 'p90', label: 'p90', hint: '90th percentile' },
  { value: 'p95', label: 'p95', hint: '95th percentile' },
  { value: 'p99', label: 'p99', hint: '99th percentile' },
  { value: 'min', label: 'Min', hint: 'Lowest value' },
  { value: 'max', label: 'Max', hint: 'Highest value' },
  { value: 'sum', label: 'Sum', hint: 'Total' },
  { value: 'stddev', label: 'Std. deviation', hint: 'Spread' },
  { value: 'regression_vs_baseline', label: 'Change vs baseline', hint: 'Score delta against the baseline run' },
]

export const metricOptions: { value: Metric; label: string }[] = [
  { value: 'evaluator', label: 'Evaluator score' },
  { value: 'results', label: 'Results' },
  { value: 'latency_ms', label: 'Latency (ms)' },
  { value: 'ttft_ms', label: 'Time to first token (ms)' },
  { value: 'cost_usd', label: 'Cost (USD)' },
  { value: 'tokens', label: 'Tokens' },
]

export const selectorOptions: { value: Selector; label: string }[] = [
  { value: 'all', label: 'All results' },
  { value: 'passed', label: 'Passed' },
  { value: 'failed', label: 'Failed' },
  { value: 'errored', label: 'Errored' },
  { value: 'critical_fail', label: 'Critical failures' },
  { value: 'major_fail', label: 'Major failures' },
  { value: 'minor_fail', label: 'Minor failures' },
]

export const comparators: Cmp[] = ['>=', '>', '<=', '<', '==', '!=']
const PERCENTILES: Fn[] = ['p50', 'p90', 'p95', 'p99', 'min', 'max', 'sum', 'stddev']

/** Sensible metric for a function, used when the user switches function. */
export function defaultMetricFor(fn: Fn): Metric {
  if (fn === 'count') return 'results'
  if (PERCENTILES.includes(fn) && fn !== 'stddev') return 'latency_ms'
  return 'evaluator'
}

export function newCondition(first?: string): Condition {
  return { fn: 'mean', metric: 'evaluator', evaluator_id: first, comparator: '>=', value: 0.8 }
}

export const presets = (firstEvaluator?: string): { label: string; cond: Condition }[] => [
  { label: 'Mean score ≥ 0.85', cond: { fn: 'mean', metric: 'evaluator', evaluator_id: firstEvaluator, comparator: '>=', value: 0.85 } },
  { label: 'Pass rate ≥ 95%', cond: { fn: 'pass_rate', metric: 'results', selector: 'all', comparator: '>=', value: 0.95 } },
  { label: 'No critical failures', cond: { fn: 'count', metric: 'results', selector: 'critical_fail', comparator: '==', value: 0 } },
  { label: 'p95 latency < 2s', cond: { fn: 'p95', metric: 'latency_ms', comparator: '<', value: 2000 } },
  { label: 'No regression vs baseline', cond: { fn: 'regression_vs_baseline', metric: 'evaluator', evaluator_id: firstEvaluator, comparator: '>', value: -0.02 } },
]

export function toExpr(c: Condition): S['GateExpr'] {
  const e: S['GateExpr'] = { op: 'compare', fn: c.fn, metric: c.metric, comparator: c.comparator, value: c.value }
  if (c.metric === 'evaluator' && c.evaluator_id) e.evaluator_id = c.evaluator_id
  if (c.selector && (c.metric === 'results' || c.fn === 'count')) e.selector = c.selector
  if (c.tag) e.tag = c.tag
  return e
}

export function exprFromConditions(conds: Condition[], joiner: Joiner): S['GateExpr'] | null {
  if (conds.length === 0) return null
  if (conds.length === 1) return toExpr(conds[0]!)
  return { op: joiner, args: conds.map(toExpr) }
}

function fromCompare(e: S['GateExpr']): Condition | null {
  if (e.op !== 'compare' || !e.fn || !e.metric || !e.comparator || e.value === undefined) return null
  return { fn: e.fn, metric: e.metric, evaluator_id: e.evaluator_id, selector: e.selector, tag: e.tag, comparator: e.comparator, value: e.value }
}

/** Flat `compare` / and / or expressions map back to the builder; anything else is "advanced" (text only). */
export function conditionsFromExpr(expr: S['GateExpr']): { joiner: Joiner; conds: Condition[] } | null {
  if (expr.op === 'compare') {
    const c = fromCompare(expr)
    return c ? { joiner: 'and', conds: [c] } : null
  }
  if ((expr.op === 'and' || expr.op === 'or') && expr.args?.length) {
    const conds = expr.args.map((a) => fromCompare(a))
    if (conds.some((c) => !c)) return null
    return { joiner: expr.op, conds: conds as Condition[] }
  }
  return null
}

const fmtValue = (c: Condition) => {
  const isPct = (c.fn === 'pass_rate') && c.value >= 0 && c.value <= 1
  return isPct ? `${+(c.value * 100).toFixed(2)}%` : String(c.value)
}

export function conditionText(c: Condition, evalName: (id?: string) => string): string {
  let target: string
  if (c.metric === 'evaluator') target = evalName(c.evaluator_id)
  else if (c.metric === 'results') target = c.selector ?? 'all'
  else target = c.metric
  if (c.metric === 'evaluator' && c.selector && c.selector !== 'all' && c.fn !== 'regression_vs_baseline') target += `, ${c.selector}`
  if (c.tag) target += `, tag=${c.tag}`
  return `${c.fn}(${target}) ${c.comparator} ${fmtValue(c)}`
}

export function exprText(expr: S['GateExpr'] | null | undefined, evalName: (id?: string) => string): string {
  if (!expr) return ''
  if (expr.op === 'compare') {
    const c = fromCompare(expr)
    return c ? conditionText(c, evalName) : '(incomplete)'
  }
  if (expr.op === 'not') return `not (${exprText(expr.args?.[0], evalName)})`
  return (expr.args ?? []).map((a) => (a.op === 'compare' ? exprText(a, evalName) : `(${exprText(a, evalName)})`)).join(` ${expr.op} `)
}

/** Validation for a condition in the builder. Returns an error message or null. */
export function conditionProblem(c: Condition): string | null {
  if ((c.metric === 'evaluator' || c.fn === 'regression_vs_baseline') && !c.evaluator_id) return 'Choose an evaluator'
  if (!Number.isFinite(c.value)) return 'Enter a number'
  if (c.fn === 'count' && c.metric !== 'results') return 'Count works on results'
  if (c.fn === 'pass_rate' && c.metric !== 'results' && c.metric !== 'evaluator') return 'Pass rate works on results or an evaluator'
  if (['p50', 'p90', 'p95', 'p99'].includes(c.fn) && (c.metric === 'results' || c.metric === 'evaluator') && c.metric !== 'evaluator') return 'Percentiles need a numeric metric'
  return null
}

/* --------------------------------------------------------------------------------- text → conditions */

export type ParseResult = { ok: true; joiner: Joiner; conds: Condition[] } | { ok: false; error: string }

const FNS = new Set<string>(fnOptions.map((f) => f.value))
const METRICS = new Set<string>(['latency_ms', 'ttft_ms', 'cost_usd', 'tokens'])
const SELECTORS = new Set<string>(selectorOptions.map((s) => s.value))

/**
 * Parses `mean(faithfulness) >= 0.85 and p95(latency_ms) < 2000`. Evaluators are matched by name (case-insensitive).
 * Mixed and/or must be built in the visual builder.
 */
export function parseGateText(text: string, evaluators: { id: string; name: string }[]): ParseResult {
  const src = text.trim()
  if (!src) return { ok: false, error: 'Write an expression, e.g. mean(faithfulness) >= 0.85' }
  const hasAnd = /\s+and\s+/i.test(src)
  const hasOr = /\s+or\s+/i.test(src)
  if (hasAnd && hasOr) return { ok: false, error: 'Mixing “and” and “or” needs the visual builder' }
  const joiner: Joiner = hasOr ? 'or' : 'and'
  const parts = src.split(/\s+(?:and|or)\s+/i)
  const conds: Condition[] = []
  for (const part of parts) {
    const m = /^([a-z0-9_]+)\(([^)]*)\)\s*(>=|<=|==|!=|>|<)\s*(-?\d+(?:\.\d+)?)(%?)$/i.exec(part.trim())
    if (!m) return { ok: false, error: `Can't read “${part.trim()}”. Expected fn(metric) comparator number` }
    const [, fnRaw, argRaw, cmp, numRaw, pct] = m
    const fn = fnRaw!.toLowerCase()
    if (!FNS.has(fn)) return { ok: false, error: `Unknown function “${fnRaw}”. Try ${[...FNS].slice(0, 6).join(', ')}…` }
    let value = Number(numRaw)
    if (pct) value /= 100
    const [first, ...rest] = argRaw!.split(',').map((s) => s.trim())
    let tag: string | undefined
    let selector: Selector | undefined
    for (const r of rest) {
      if (r.startsWith('tag=')) tag = r.slice(4)
      else if (SELECTORS.has(r)) selector = r as Selector
      else return { ok: false, error: `Unknown option “${r}”` }
    }
    const cond: Condition = { fn: fn as Fn, metric: 'evaluator', comparator: cmp as Cmp, value, tag, selector }
    const arg = (first ?? '').toLowerCase()
    if (!arg) return { ok: false, error: `${fn}() needs a metric` }
    if (METRICS.has(arg)) cond.metric = arg as Metric
    else if (SELECTORS.has(arg) && (fn === 'count' || fn === 'pass_rate')) {
      cond.metric = 'results'
      cond.selector = arg as Selector
    } else {
      const ev = evaluators.find((e) => e.name.toLowerCase() === arg)
      if (!ev) return { ok: false, error: `No evaluator named “${first}” in this suite` }
      cond.evaluator_id = ev.id
    }
    const problem = conditionProblem(cond)
    if (problem) return { ok: false, error: problem }
    conds.push(cond)
  }
  return { ok: true, joiner, conds }
}
