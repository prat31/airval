/**
 * URL search-param schemas for run screens (the URL holds filters, tabs and the open result).
 */
const str = (v: unknown) => (typeof v === 'string' && v !== '' ? v : undefined)
const num = (v: unknown) => {
  const n = typeof v === 'number' ? v : typeof v === 'string' && v !== '' ? Number(v) : NaN
  return Number.isFinite(n) ? n : undefined
}
const strip = <T extends Record<string, unknown>>(o: T) => Object.fromEntries(Object.entries(o).filter(([, v]) => v !== undefined)) as T

export interface RunsSearch {
  q?: string
  /** Comma-separated RunStatus values. */
  status?: string
  trigger?: string
  suite?: string
  target?: string
  /** Date preset id (24h, 7d, 30d, 90d) or "custom" with from/to. Default: all time. */
  range?: string
  from?: string
  to?: string
}

export function validateRunsSearch(s: Record<string, unknown>): RunsSearch {
  return strip({
    q: str(s.q),
    status: str(s.status),
    trigger: str(s.trigger),
    suite: str(s.suite),
    target: str(s.target),
    range: str(s.range),
    from: str(s.from),
    to: str(s.to),
  })
}

export type ResultFilter = 'all' | 'failed' | 'passed' | 'errored'
export type DrawerTab = 'output' | 'context' | 'trace' | 'scores' | 'raw'

export interface RunDetailSearch {
  /** Open result (drawer). */
  result?: string
  tab?: DrawerTab
  status?: ResultFilter
  /** Evaluator for score range / pass filters. */
  ev?: string
  min?: number
  max?: number
  tag?: string
  q?: string
  /** Evaluator shown in the score histogram. */
  hist?: string
}

const tabs: DrawerTab[] = ['output', 'context', 'trace', 'scores', 'raw']
const filters: ResultFilter[] = ['all', 'failed', 'passed', 'errored']

export function validateRunDetailSearch(s: Record<string, unknown>): RunDetailSearch {
  const tab = str(s.tab) as DrawerTab | undefined
  const status = str(s.status) as ResultFilter | undefined
  return strip({
    result: str(s.result),
    tab: tab && tabs.includes(tab) ? tab : undefined,
    status: status && filters.includes(status) ? status : undefined,
    ev: str(s.ev),
    min: num(s.min),
    max: num(s.max),
    tag: str(s.tag),
    q: str(s.q),
    hist: str(s.hist),
  })
}
