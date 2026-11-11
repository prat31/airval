/* eslint-disable react-hooks/set-state-in-effect -- local drafts are seeded from server data */
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CircleAlert, CircleCheck, Download, Search } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { CardBody, CardFooter } from '@/components/ui/card'
import { DateRangePicker, defaultDatePresets, type DateRange } from '@/components/ui/date-range-picker'
import { Drawer } from '@/components/ui/drawer'
import { EmptyState } from '@/components/ui/empty-state'
import { Field } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { JsonViewer } from '@/components/ui/json-viewer'
import { Select } from '@/components/ui/select'
import { toast } from '@/components/ui/toast'
import { formatDateTime } from '@/lib/format'
import { cn } from '@/lib/cn'
import { errorMessage, http, rawFetch, saveBlob, unwrap, type S } from '../../shared/api'
import { useProjectCtx } from '../../shared/context'
import { ErrorPanel, RelTime, useDebounced } from '../../shared/ui'
import { DetailRow, Loading, SectionCard } from './common'

const allTime: DateRange = { from: new Date(0), to: new Date(8.64e15), preset: 'all' }

export function AuditSettingsPage() {
  const { org } = useProjectCtx()
  return (
    <div className="flex flex-col gap-6">
      <AuditLogCard />
      {org?.role === 'owner' && <AuditRetentionCard />}
    </div>
  )
}

function AuditLogCard() {
  const { orgId } = useProjectCtx()
  const [actor, setActor] = useState('')
  const [action, setAction] = useState('')
  const [range, setRange] = useState<DateRange>(allTime)
  const [open, setOpen] = useState<S['AuditEntry'] | null>(null)
  const dActor = useDebounced(actor.trim())
  const dAction = useDebounced(action.trim())
  const query = useMemo(() => {
    const q: { actor?: string; action?: string; from?: string; to?: string } = {}
    if (dActor) q.actor = dActor
    if (dAction) q.action = dAction
    if (range.preset !== 'all') {
      q.from = range.from.toISOString()
      q.to = range.to.toISOString()
    }
    return q
  }, [dActor, dAction, range])
  const log = useInfiniteQuery({
    queryKey: ['audit-log', orgId, query],
    enabled: !!orgId,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => unwrap(http.GET('/v1/orgs/{org}/audit-log', { params: { path: { org: orgId! }, query: { ...query, cursor: pageParam, limit: 50 } } })),
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  })
  const items = log.data?.pages.flatMap((p) => p.items) ?? []
  const exportCsv = useMutation({
    mutationFn: async () => {
      const qs = new URLSearchParams(query as Record<string, string>).toString()
      const res = await rawFetch(`/v1/orgs/${orgId}/audit-log/export${qs ? `?${qs}` : ''}`)
      saveBlob(await res.blob(), `audit-log-${new Date().toISOString().slice(0, 10)}.csv`)
    },
    onError: (e) => toast.error(`Export failed: ${errorMessage(e)}`),
  })
  const filtered = !!(dActor || dAction || range.preset !== 'all')
  return (
    <SectionCard title="Audit log" description="Every change made through the API, with who made it and from where."
      actions={<Button size="sm" icon={<Download />} loading={exportCsv.isPending} onClick={() => exportCsv.mutate()}>Export CSV</Button>}>
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-2.5">
        <Input size="sm" className="w-48" leading={<Search />} aria-label="Filter by actor" placeholder="Actor" value={actor} onChange={(e) => setActor(e.target.value)} />
        <Input size="sm" className="w-48 font-mono text-xs" aria-label="Filter by action" placeholder="Action, e.g. secret." value={action} onChange={(e) => setAction(e.target.value)} />
        <DateRangePicker value={range} onChange={setRange} presets={[{ id: 'all', label: 'All time', short: 'All time', range: () => ({ from: allTime.from, to: allTime.to }) }, ...defaultDatePresets]} />
        {filtered && <Button size="sm" variant="ghost" onClick={() => { setActor(''); setAction(''); setRange(allTime) }}>Clear</Button>}
      </div>
      {log.isPending ? <Loading rows={5} /> : log.error ? <ErrorPanel error={log.error} onRetry={() => void log.refetch()} /> : items.length === 0 ? (
        <EmptyState compact illustration="search" title={filtered ? 'No matching entries' : 'Nothing recorded yet'} description={filtered ? 'Try a broader filter.' : 'Changes to projects, datasets, suites, members and settings appear here.'} />
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="border-b border-border bg-surface-2 text-xs text-text-muted">
              <tr>
                <th scope="col" className="px-4 py-2 text-left font-medium">When</th>
                <th scope="col" className="px-4 py-2 text-left font-medium">Actor</th>
                <th scope="col" className="px-4 py-2 text-left font-medium">Action</th>
                <th scope="col" className="px-4 py-2 text-left font-medium">Result</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {items.map((e) => (
                <tr key={e.id} tabIndex={0} className="cursor-pointer transition-colors hover:bg-surface-hover focus-visible:bg-surface-hover focus-visible:outline-none" onClick={() => setOpen(e)} onKeyDown={(k) => k.key === 'Enter' && setOpen(e)}>
                  <td className="whitespace-nowrap px-4 py-2"><RelTime iso={e.at} className="text-sm" /></td>
                  <td className="max-w-[200px] truncate px-4 py-2 text-text" title={e.actor}>{e.actor}</td>
                  <td className="px-4 py-2">
                    <code className="block font-mono text-xs text-text">{e.action}</code>
                    <span className="block font-mono text-[11px] text-text-muted" title={e.target}>{shortTarget(e.target)}</span>
                  </td>
                  <td className="whitespace-nowrap px-4 py-2"><Outcome status={e.status} /></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {log.hasNextPage && (
        <CardFooter className="flex justify-center">
          <Button size="sm" variant="ghost" loading={log.isFetchingNextPage} onClick={() => void log.fetchNextPage()}>Load more</Button>
        </CardFooter>
      )}
      <Drawer open={!!open} onOpenChange={(o) => !o && setOpen(null)} size="md" title={open?.action ?? ''} description={open ? formatDateTime(new Date(open.at)) : undefined}>
        {open && (
          <div className="flex flex-col gap-4 p-4">
            <dl className="divide-y divide-border rounded-md border border-border px-3">
              <DetailRow label="Actor">{open.actor}</DetailRow>
              <DetailRow label="Target"><code className="break-all font-mono text-xs">{open.target || '—'}</code></DetailRow>
              <DetailRow label="Result"><Outcome status={open.status} /></DetailRow>
              <DetailRow label="IP address"><code className="font-mono text-xs">{open.ip || '—'}</code></DetailRow>
              <DetailRow label="User agent"><span className="break-all text-xs text-text-2">{open.user_agent || '—'}</span></DetailRow>
              {open.request_id && <DetailRow label="Request ID"><code className="break-all font-mono text-xs">{open.request_id}</code></DetailRow>}
            </dl>
            {open.diff != null && <JsonViewer value={open.diff} rootLabel="details" />}
          </div>
        )}
      </Drawer>
    </SectionCard>
  )
}

/** "dataset:<uuid>" → "dataset …<last 8>" (UUIDv7 prefixes are timestamps) (the full value is in the title and the drawer). */
function shortTarget(t: string): string {
  if (!t) return '—'
  const [kind, id] = t.split(':', 2)
  return id && id.length > 12 ? `${kind} …${id.slice(-8)}` : t
}

function Outcome({ status }: { status?: number }) {
  // Entries written by handlers themselves (no HTTP status) are only recorded on success.
  const ok = !status || status < 400
  return (
    <span className={cn('inline-flex items-center gap-1 text-xs font-medium', ok ? 'text-pass' : 'text-fail')}>
      {ok ? <CircleCheck className="size-3.5" aria-hidden /> : <CircleAlert className="size-3.5" aria-hidden />}
      {ok ? 'Succeeded' : status === 403 ? 'Denied' : 'Failed'}
      {!!status && <span className="font-mono font-normal text-text-muted">{status}</span>}
    </span>
  )
}

const retentionOptions = [
  { value: '0', label: 'Keep forever' },
  { value: '30', label: '30 days' },
  { value: '90', label: '90 days' },
  { value: '180', label: '180 days' },
  { value: '365', label: '1 year' },
  { value: '730', label: '2 years' },
  { value: '2555', label: '7 years' },
]

function AuditRetentionCard() {
  const { orgId } = useProjectCtx()
  const qc = useQueryClient()
  const settings = useQuery({
    queryKey: ['audit-settings', orgId],
    enabled: !!orgId,
    queryFn: () => unwrap(http.GET('/v1/orgs/{org}/audit-settings', { params: { path: { org: orgId! } } })),
  })
  const [days, setDays] = useState('0')
  useEffect(() => {
    if (settings.data) setDays(String(settings.data.retention_days))
  }, [settings.data])
  const save = useMutation({
    mutationFn: () => unwrap(http.PUT('/v1/orgs/{org}/audit-settings', { params: { path: { org: orgId! } }, body: { retention_days: Number(days) } })),
    onSuccess: () => {
      toast.success('Audit retention saved')
      void qc.invalidateQueries({ queryKey: ['audit-settings', orgId] })
    },
  })
  const options = retentionOptions.some((o) => o.value === days) ? retentionOptions : [...retentionOptions, { value: days, label: `${days} days` }]
  return (
    <SectionCard title="Audit retention" description="Entries older than this are deleted hourly.">
      {settings.isPending ? <Loading /> : settings.error ? <ErrorPanel error={settings.error} onRetry={() => void settings.refetch()} /> : (
        <CardBody className="flex flex-wrap items-end gap-3">
          <Field label="Keep entries for" className="w-56"><Select value={days} onValueChange={setDays} options={options} /></Field>
          <Button variant="primary" disabled={String(settings.data!.retention_days) === days} loading={save.isPending} onClick={() => save.mutate()}>Save</Button>
          {Number(days) > 0 && <Badge variant="warn">Older entries are removed permanently</Badge>}
        </CardBody>
      )}
    </SectionCard>
  )
}
