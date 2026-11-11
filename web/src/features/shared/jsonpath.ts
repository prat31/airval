/**
 * A tiny JSONPath evaluator for the target mapping playground. Supports `$`, `.name`, `['name']`, `[0]`, `[-1]`, `[*]`, `.*`
 * and recursive descent `..name`. The server is the source of truth; this only previews matches against a raw response.
 */
type Token = { kind: 'key'; key: string } | { kind: 'index'; index: number } | { kind: 'wild' } | { kind: 'deep'; key: string }

export function parsePath(path: string): Token[] | null {
  const p = path.trim()
  if (!p.startsWith('$')) return null
  const tokens: Token[] = []
  let i = 1
  while (i < p.length) {
    const c = p[i]
    if (c === '.') {
      if (p[i + 1] === '.') {
        i += 2
        const m = /^[A-Za-z_][\w-]*/.exec(p.slice(i))
        if (!m) return null
        tokens.push({ kind: 'deep', key: m[0] })
        i += m[0].length
      } else {
        i += 1
        if (p[i] === '*') {
          tokens.push({ kind: 'wild' })
          i += 1
          continue
        }
        const m = /^[^.[\]]+/.exec(p.slice(i))
        if (!m) return null
        tokens.push({ kind: 'key', key: m[0] })
        i += m[0].length
      }
    } else if (c === '[') {
      const end = p.indexOf(']', i)
      if (end < 0) return null
      const inner = p.slice(i + 1, end).trim()
      i = end + 1
      if (inner === '*') tokens.push({ kind: 'wild' })
      else if (/^-?\d+$/.test(inner)) tokens.push({ kind: 'index', index: Number(inner) })
      else if (/^(['"]).*\1$/.test(inner)) tokens.push({ kind: 'key', key: inner.slice(1, -1) })
      else return null
    } else return null
  }
  return tokens
}

function deep(value: unknown, key: string, out: unknown[]) {
  if (Array.isArray(value)) value.forEach((v) => deep(v, key, out))
  else if (value && typeof value === 'object') {
    for (const [k, v] of Object.entries(value)) {
      if (k === key) out.push(v)
      deep(v, key, out)
    }
  }
}

export interface PathResult {
  valid: boolean
  matches: unknown[]
}

export function evalPath(root: unknown, path: string): PathResult {
  const tokens = parsePath(path)
  if (!tokens) return { valid: false, matches: [] }
  let cur: unknown[] = [root]
  for (const t of tokens) {
    const next: unknown[] = []
    for (const v of cur) {
      if (t.kind === 'key') {
        if (v && typeof v === 'object' && !Array.isArray(v) && t.key in v) next.push((v as Record<string, unknown>)[t.key])
      } else if (t.kind === 'index') {
        if (Array.isArray(v)) {
          const item = v[t.index < 0 ? v.length + t.index : t.index]
          if (item !== undefined) next.push(item)
        }
      } else if (t.kind === 'wild') {
        if (Array.isArray(v)) next.push(...v)
        else if (v && typeof v === 'object') next.push(...Object.values(v))
      } else deep(v, t.key, next)
    }
    cur = next
  }
  return { valid: true, matches: cur }
}
