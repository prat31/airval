import { useNavigate, useSearch } from '@tanstack/react-router'
import { useCallback } from 'react'

type Search = Record<string, unknown>

/**
 * URL-backed view state. `search` is the raw parsed query; `patch` merges keys (null / '' / undefined removes a key).
 * Writes replace history so filters don't pollute back-navigation.
 */
export function useSearchState(): [Search, (patch: Search, opts?: { push?: boolean }) => void] {
  const search = useSearch({ strict: false }) as Search
  const navigate = useNavigate()
  const patch = useCallback(
    (p: Search, opts?: { push?: boolean }) => {
      void navigate({
        to: '.',
        replace: !opts?.push,
        search: ((prev: Search) => {
          const next = { ...prev }
          for (const [k, v] of Object.entries(p)) {
            if (v === null || v === undefined || v === '' || v === false) delete next[k]
            else next[k] = v
          }
          return next
        }) as never,
      })
    },
    [navigate],
  )
  return [search, patch]
}

export const str = (v: unknown, def = ''): string => (v === undefined || v === null ? def : String(v))
export const num = (v: unknown): number | undefined => {
  const n = typeof v === 'number' ? v : typeof v === 'string' ? Number(v) : NaN
  return Number.isFinite(n) ? n : undefined
}
