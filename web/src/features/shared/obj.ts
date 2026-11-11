import type { Obj } from './api'

export const isObj = (v: unknown): v is Obj => !!v && typeof v === 'object' && !Array.isArray(v)

export function getIn<T = unknown>(o: unknown, path: string[], fallback?: T): T {
  let cur: unknown = o
  for (const k of path) {
    if (!isObj(cur)) return fallback as T
    cur = cur[k]
  }
  return (cur === undefined ? fallback : cur) as T
}

/** Immutable set; empty strings / undefined remove the key and empty parents are pruned. */
export function setIn(o: Obj, path: string[], value: unknown): Obj {
  const [head, ...rest] = path
  if (head === undefined) return o
  const next: Obj = { ...o }
  if (rest.length === 0) {
    if (value === undefined || value === '' || (Array.isArray(value) && value.length === 0) || (isObj(value) && Object.keys(value).length === 0)) delete next[head]
    else next[head] = value
  } else {
    const child = setIn(isObj(o[head]) ? o[head] : {}, rest, value)
    if (Object.keys(child).length === 0) delete next[head]
    else next[head] = child
  }
  return next
}

export const stableStringify = (v: unknown): string =>
  JSON.stringify(v, (_k, val) => (isObj(val) ? Object.fromEntries(Object.entries(val).sort(([a], [b]) => a.localeCompare(b))) : val), 2)

/** Mask values whose key looks like a credential (defense in depth for displayed requests). */
export function redact(v: unknown): unknown {
  if (Array.isArray(v)) return v.map(redact)
  if (isObj(v)) {
    return Object.fromEntries(
      Object.entries(v).map(([k, val]) => [k, /authorization|api[-_]?key|token|secret|password|cookie/i.test(k) && typeof val === 'string' ? '••••••••' : redact(val)]),
    )
  }
  return v
}
