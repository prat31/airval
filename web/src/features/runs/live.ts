/**
 * Live run updates over SSE (`GET /v1/runs/{run}/events`).
 *
 * Events are buffered and applied to the TanStack Query cache in batches (every 250 ms), so a run
 * streaming thousands of results per second costs a handful of renders, not thousands.
 */
import { useQueryClient, type InfiniteData, type QueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import type { components } from '@/lib/api/schema'
import { runKeys, type Result, type Run, type RunEvent } from './api'

type ResultList = components['schemas']['ResultList']

export interface EventSourceLike {
  readyState: number
  onopen: ((e: Event) => void) | null
  onerror: ((e: Event) => void) | null
  onmessage: ((e: MessageEvent) => void) | null
  addEventListener(type: string, fn: (e: MessageEvent) => void): void
  close(): void
}

let factory: (url: string) => EventSourceLike = (url) => new EventSource(url, { withCredentials: true }) as unknown as EventSourceLike
/** Swapped by the dev fixture mode. */
export function setEventSourceFactory(f: (url: string) => EventSourceLike) {
  factory = f
}

export type LiveState = 'idle' | 'connecting' | 'live' | 'reconnecting'

const EVENT_TYPES: RunEvent['type'][] = ['run.status', 'run.progress', 'result', 'run.finished']
const RUN_FIELDS = new Set<keyof Run>([
  'status',
  'total',
  'completed',
  'failed',
  'errored',
  'started_at',
  'finished_at',
  'gate_passed',
  'summary',
  'cost_usd',
  'tokens_in',
  'tokens_out',
  'error',
])
const FLUSH_MS = 250
const MAX_BACKOFF = 30_000

/**
 * Rows carried by one `result` event. The engine batches them (`{items: [...], count}`, up to 100
 * compact rows per flush); a single row object is accepted too. Compact rows lack some fields of
 * the full Result, so defaults keep the table's renderers safe until the list is refetched.
 */
export function resultRows(runId: string, ev: RunEvent, data: Record<string, unknown>): Result[] {
  const raw: unknown[] = Array.isArray(data.items) ? data.items : 'id' in data ? [data] : []
  const out: Result[] = []
  for (const r of raw) {
    if (!r || typeof r !== 'object' || !('id' in r)) continue
    out.push({
      run_id: runId,
      example_revision_id: '',
      trial: 0,
      output: '',
      latency_ms: 0,
      tokens_in: 0,
      tokens_out: 0,
      cost_usd: 0,
      error: '',
      cached: false,
      tags: [],
      scores: [],
      created_at: ev.at,
      ...(r as Partial<Result>),
      // Compact scores carry no reasoning.
      ...('scores' in r && Array.isArray(r.scores) ? { scores: (r.scores as Result['scores']).map((s) => ({ ...s, reason: s.reason ?? '', label: s.label ?? '' })) } : {}),
    } as Result)
  }
  return out
}

function applyEvents(qc: QueryClient, runId: string, events: RunEvent[]) {
  let patch: Partial<Run> = {}
  const results: Result[] = []
  let finished = false
  for (const ev of events) {
    const data = (ev.data ?? {}) as Record<string, unknown>
    if (ev.type === 'result') {
      results.push(...resultRows(runId, ev, data))
      continue
    }
    for (const [k, v] of Object.entries(data)) if (RUN_FIELDS.has(k as keyof Run)) (patch as Record<string, unknown>)[k] = v
    if (ev.type === 'run.finished') finished = true
  }
  if (Object.keys(patch).length) qc.setQueryData<Run>(runKeys.detail(runId), (old) => (old ? { ...old, ...patch } : old))
  patch = {}
  if (results.length) {
    // Stream rows into the unfiltered results list (the default view while a run is live).
    qc.setQueryData<InfiniteData<ResultList, string | undefined>>(runKeys.resultList(runId, {}), (old) => {
      if (!old || !old.pages.length) return old
      const seen = new Set(old.pages.flatMap((p) => p.items.map((r) => r.id)))
      const fresh = results.filter((r) => !seen.has(r.id))
      if (!fresh.length) return old
      const pages = old.pages.slice()
      const last = pages[pages.length - 1]!
      // Only append when the list is fully loaded, so pagination cursors stay valid.
      if (last.next_cursor) return old
      pages[pages.length - 1] = { ...last, items: [...last.items, ...fresh] }
      return { ...old, pages }
    })
  }
  if (finished) {
    void qc.invalidateQueries({ queryKey: runKeys.detail(runId) })
    void qc.invalidateQueries({ queryKey: runKeys.aggregates(runId) })
    void qc.invalidateQueries({ queryKey: runKeys.results(runId) })
    void qc.invalidateQueries({ queryKey: ['runs', 'live'] })
    void qc.invalidateQueries({ queryKey: runKeys.lists() })
  }
}

/** Subscribe to a run's event stream while `enabled`. Reconnects with exponential backoff. */
export function useRunEvents(runId: string, enabled: boolean): LiveState {
  const qc = useQueryClient()
  const [status, setStatus] = useState<{ runId: string; state: LiveState }>({ runId: '', state: 'idle' })

  useEffect(() => {
    if (!enabled || !runId) return
    const setState = (state: LiveState) => setStatus({ runId, state })
    let es: EventSourceLike | null = null
    let attempt = 0
    let retryTimer: ReturnType<typeof setTimeout> | undefined
    let flushTimer: ReturnType<typeof setTimeout> | undefined
    let disposed = false
    const buffer: RunEvent[] = []

    const flush = () => {
      flushTimer = undefined
      if (buffer.length) applyEvents(qc, runId, buffer.splice(0))
    }
    const onEvent = (e: MessageEvent) => {
      try {
        const ev = JSON.parse(e.data) as RunEvent
        buffer.push(ev)
        if (ev.type === 'run.finished') {
          flush()
          es?.close()
          setState('idle')
          return
        }
        flushTimer ??= setTimeout(flush, FLUSH_MS)
      } catch {
        /* ignore malformed frames */
      }
    }
    const connect = () => {
      if (disposed) return
      es = factory(`/v1/runs/${runId}/events`)
      es.onopen = () => {
        if (attempt) void qc.invalidateQueries({ queryKey: runKeys.detail(runId) })
        attempt = 0
        setState('live')
      }
      for (const t of EVENT_TYPES) es.addEventListener(t, onEvent)
      es.onmessage = onEvent
      es.onerror = () => {
        if (!es || es.readyState !== 2) {
          setState('reconnecting') // the browser retries on its own
          return
        }
        es.close()
        setState('reconnecting')
        const wait = Math.min(MAX_BACKOFF, 1000 * 2 ** attempt++)
        retryTimer = setTimeout(connect, wait)
        // Keep counters fresh while the stream is down.
        void qc.invalidateQueries({ queryKey: runKeys.detail(runId) })
      }
    }
    connect()
    // Aggregates (charts, scorecards) are recomputed server-side; refresh them at a calm cadence.
    const aggTimer = setInterval(() => void qc.invalidateQueries({ queryKey: runKeys.aggregates(runId) }), 5_000)

    return () => {
      disposed = true
      clearTimeout(retryTimer)
      clearTimeout(flushTimer)
      clearInterval(aggTimer)
      es?.close()
    }
  }, [runId, enabled, qc])

  if (!enabled) return 'idle'
  return status.runId === runId ? status.state : 'connecting'
}
