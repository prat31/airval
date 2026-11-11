export interface CompareSearch {
  /** Comma-separated run ids; the first is the baseline. */
  runs?: string
  /** Evaluator used to rank regressions / improvements and plot the scatter. */
  ev?: string
  /** Example open in the aligned outputs panel. */
  example?: string
}

const str = (v: unknown) => (typeof v === 'string' && v !== '' ? v : undefined)

export function validateCompareSearch(s: Record<string, unknown>): CompareSearch {
  const out: CompareSearch = {}
  const runs = str(s.runs)
  const ev = str(s.ev)
  const example = str(s.example)
  if (runs) out.runs = runs
  if (ev) out.ev = ev
  if (example) out.example = example
  return out
}
