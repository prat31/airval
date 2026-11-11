/**
 * DEV-ONLY: serves the fixture backend through an openapi-fetch middleware and a fake EventSource.
 * Endpoints not handled here fall through to the real server.
 */
import type { Middleware } from 'openapi-fetch'
import { api } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'
import { setEventSourceFactory, type EventSourceLike } from '../live'
import * as fx from './data'

type S = components['schemas']

const json = (body: unknown, status = 200) =>
  new Response(status === 204 ? null : JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
const notFound = () => json({ code: 'not_found', message: 'Not found (fixtures)' }, 404)
const delay = (ms: number) => new Promise((r) => setTimeout(r, ms))

function page<T>(items: T[], url: URL, defaultLimit = 50) {
  const limit = Number(url.searchParams.get('limit') ?? defaultLimit)
  const start = Number(url.searchParams.get('cursor') ?? 0)
  const slice = items.slice(start, start + limit)
  return { items: slice, next_cursor: start + limit < items.length ? String(start + limit) : null }
}

const projectFor = (id: string) => fx.projects.find((p) => p.id === id)
const isMain = (projectId: string) => projectId === fx.projects[0]!.id

async function handle(method: string, url: URL, request: Request): Promise<Response | undefined> {
  const p = url.pathname.replace(/^\/v1/, '')
  const seg = p.split('/').filter(Boolean)
  const body = method === 'GET' ? undefined : await request.clone().json().catch(() => ({}))
  fx.hydrateAll()

  // auth & workspace
  if (p === '/setup' && method === 'GET') return json({ needs_setup: false })
  if (p === '/auth/me' || p === '/auth/login' || p === '/setup') return json(fx.me)
  if (p === '/auth/logout') return json(null, 204)
  if (p === '/orgs' && method === 'POST') {
    const org: S['Organization'] = { id: crypto.randomUUID(), name: body.name, slug: body.slug || body.name.toLowerCase(), role: 'owner', created_at: new Date().toISOString(), updated_at: new Date().toISOString() }
    fx.orgs.push(org)
    return json(org, 201)
  }
  if (p === '/projects' && method === 'GET') {
    const orgId = url.searchParams.get('org_id')
    return json(page(fx.projects.filter((x) => !orgId || x.org_id === orgId), url, 1000))
  }
  if (p === '/projects' && method === 'POST') {
    const proj: S['Project'] = { id: crypto.randomUUID(), org_id: body.org_id, name: body.name, slug: body.slug, description: body.description ?? '', color: body.color ?? '', icon: '', settings: {}, created_at: new Date().toISOString(), updated_at: new Date().toISOString() }
    fx.projects.push(proj)
    return json(proj, 201)
  }
  if (seg[0] === 'projects' && seg[1] && seg.length === 3) {
    const pid = seg[1]
    if (!projectFor(pid)) return notFound()
    const scoped = <T>(items: T[]) => json(page(isMain(pid) ? items : [], url, 1000))
    switch (seg[2]) {
      case 'suites':
        return scoped(fx.suites)
      case 'targets':
        return scoped(fx.targets)
      case 'evaluators':
        return scoped(fx.evaluators)
      case 'datasets':
        return scoped(fx.datasets)
      case 'review-queues':
        return scoped(fx.reviewQueues)
      case 'trends':
        if (!isMain(pid)) return json({ metric: url.searchParams.get('metric') ?? 'pass_rate', series: [] })
        return json(fx.trends(Object.fromEntries(url.searchParams)))
    }
  }

  // runs
  if (p === '/runs' && method === 'GET') {
    const q = url.searchParams
    const status = q.get('status')?.split(',')
    const from = q.get('from') ? Date.parse(q.get('from')!) : null
    const to = q.get('to') ? Date.parse(q.get('to')!) : null
    const labels = q.getAll('label')
    const items = fx.runs.filter(
      (r) =>
        (!q.get('project_id') || r.project_id === q.get('project_id')) &&
        (!q.get('suite_id') || r.suite_id === q.get('suite_id')) &&
        (!q.get('target_id') || r.target_id === q.get('target_id')) &&
        (!q.get('trigger') || r.trigger_kind === q.get('trigger')) &&
        (!status || status.includes(r.status)) &&
        (from == null || Date.parse(r.created_at) >= from) &&
        (to == null || Date.parse(r.created_at) <= to) &&
        labels.every((l) => {
          const [k, v] = l.split(':')
          return r.labels[k!] === v
        }),
    )
    await delay(120)
    return json(page(items, url))
  }
  if (p === '/runs' && method === 'POST') {
    await delay(300)
    return json({ runs: fx.createRun(body) }, 201)
  }
  if (seg[0] === 'runs' && seg[1]) {
    const [id, action] = seg[1].split(':')
    const run = fx.runs.find((r) => r.id === id)
    if (!run) return notFound()
    if (action === 'cancel') {
      run.status = 'canceled'
      run.finished_at = new Date().toISOString()
      return json(fx.hydrate(run))
    }
    if (action === 'rerun-failed') return json(fx.childRun(run, 'rerun'), 201)
    if (action === 'rescore') return json(fx.childRun(run, 'rescore'), 201)
    if (seg.length === 2) return json(run)
    if (seg[2] === 'aggregates') {
      await delay(150)
      return json({ items: fx.aggregates(run.id) })
    }
    if (seg[2] === 'results' && seg[3]) {
      const hit = fx.runForResultId(seg[3])
      const l = hit ? fx.lights(hit.run.id)[hit.i] : undefined
      await delay(120)
      return hit && l ? json(fx.toResult(hit.run.id, l, true)) : notFound()
    }
    if (seg[2] === 'results') {
      const q = url.searchParams
      const status = q.get('status')?.split(',')
      const meta = fx.runMeta.get(run.id)!
      const evIdx = q.get('evaluator_id') ? meta.evalIdx.findIndex((ei) => fx.evaluators[ei]!.id === q.get('evaluator_id')) : -1
      const passed = q.get('passed')
      const min = q.get('score_min') != null ? Number(q.get('score_min')) : null
      const max = q.get('score_max') != null ? Number(q.get('score_max')) : null
      const tag = q.get('tag')
      const text = q.get('q')?.toLowerCase()
      const ls = fx.lights(run.id).filter((l) => {
        if (status && !status.includes(l.status)) return false
        if (evIdx >= 0) {
          const v = l.scores[evIdx]
          if (passed != null && String(l.passed[evIdx]) !== passed) return false
          if (min != null && (v == null || v < min)) return false
          if (max != null && (v == null || v > max)) return false
        }
        return true
      })
      let rows = ls
      if (tag || text) {
        rows = ls.filter((l) => {
          const r = fx.toResult(run.id, l)
          if (tag && !r.tags.includes(tag)) return false
          if (text && !`${JSON.stringify(r.input)} ${r.output} ${r.error}`.toLowerCase().includes(text)) return false
          return true
        })
      }
      const pg = page(rows, url)
      await delay(140)
      return json({ items: pg.items.map((l) => fx.toResult(run.id, l)), next_cursor: pg.next_cursor })
    }
  }
  if (p === '/comparisons' && method === 'GET') {
    const ids = (url.searchParams.get('runs') ?? '').split(',').filter(Boolean)
    if (ids.length < 2) return json({ code: 'bad_request', message: 'Pick 2–4 runs' }, 400)
    await delay(250)
    return json(fx.compare(ids, url.searchParams.get('evaluator_id') ?? undefined, Number(url.searchParams.get('limit') ?? 50)))
  }
  if (/^\/datasets\/[^/]+:from-results$/.test(p)) return json({ count: body.result_ids?.length ?? 0 })
  if (/^\/review-queues\/[^/]+\/items$/.test(p) && method === 'POST') return json({ count: body.result_ids?.length ?? 0 })
  return undefined
}

const fixtureMiddleware: Middleware = {
  async onRequest({ request }) {
    const url = new URL(request.url, location.origin)
    if (!url.pathname.startsWith('/v1/')) return undefined
    try {
      return await handle(request.method, url, request)
    } catch (err) {
      console.error('[fixtures]', err)
      return json({ code: 'internal', message: String(err) }, 500)
    }
  },
}

/** Mimics EventSource for /v1/runs/{id}/events using the fixture engine. */
class FixtureEventSource implements EventSourceLike {
  readyState = 0
  onopen: ((e: Event) => void) | null = null
  onerror: ((e: Event) => void) | null = null
  onmessage: ((e: MessageEvent) => void) | null = null
  private handlers = new Map<string, Set<(e: MessageEvent) => void>>()
  private unsubscribe: () => void = () => {}
  constructor(url: string) {
    const id = url.match(/\/runs\/([^/]+)\/events/)?.[1] ?? ''
    setTimeout(() => {
      if (this.readyState === 2) return
      this.readyState = 1
      this.onopen?.(new Event('open'))
      this.unsubscribe = fx.subscribe(id, (type, data) => {
        const e = new MessageEvent(type, { data: JSON.stringify({ seq: fx.nextSeq(), type, run_id: id, at: new Date().toISOString(), data }), lastEventId: String(fx.nextSeq()) })
        this.handlers.get(type)?.forEach((h) => h(e))
      })
    }, 200)
  }
  addEventListener(type: string, fn: (e: MessageEvent) => void) {
    const set = this.handlers.get(type) ?? new Set()
    set.add(fn)
    this.handlers.set(type, set)
  }
  close() {
    this.readyState = 2
    this.unsubscribe()
  }
}

export function install() {
  api.use(fixtureMiddleware)
  setEventSourceFactory((url) => new FixtureEventSource(url))
  fx.startEngine()
  console.info('%c[airval] fixture mode', 'color: var(--accent)', '— data is generated locally. Disable with ?fixtures=0')
}
