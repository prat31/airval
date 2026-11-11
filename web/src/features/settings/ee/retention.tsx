/* eslint-disable react-hooks/set-state-in-effect -- local drafts are seeded from server data */
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Archive, Infinity as InfinityIcon } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { CardBody } from '@/components/ui/card'
import { Dialog } from '@/components/ui/dialog'
import { EmptyState } from '@/components/ui/empty-state'
import { Field } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { toast } from '@/components/ui/toast'
import { projectsQuery } from '@/features/auth/api'
import { http, unwrap, type S } from '../../shared/api'
import { useProjectCtx } from '../../shared/context'
import { ErrorPanel, RelTime } from '../../shared/ui'
import { Loading, SectionCard } from './common'
import { ToggleRow } from './sso'

export function RetentionSettings() {
  const { orgId, org } = useProjectCtx()
  const qc = useQueryClient()
  const canEdit = org?.role === 'owner' || org?.role === 'admin'
  const projects = useQuery(projectsQuery(orgId ?? ''))
  const policies = useQuery({
    queryKey: ['retention', orgId],
    enabled: !!orgId && canEdit,
    queryFn: async () => (await unwrap(http.GET('/v1/orgs/{org}/retention-policies', { params: { path: { org: orgId! } } }))).items,
  })
  const [editing, setEditing] = useState<S['Project'] | null>(null)
  const [enabled, setEnabled] = useState(true)
  const [days, setDays] = useState('90')
  const [keepBaselines, setKeepBaselines] = useState(true)
  const policyOf = (pid: string) => policies.data?.find((p) => p.project_id === pid)
  useEffect(() => {
    if (!editing) return
    const p = policyOf(editing.id)
    setEnabled(p?.enabled ?? true)
    setDays(String(p?.run_days ?? 90))
    setKeepBaselines(p?.keep_baselines ?? true)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [editing])
  const refresh = () => qc.invalidateQueries({ queryKey: ['retention', orgId] })
  const save = useMutation({
    mutationFn: () => unwrap(http.PUT('/v1/projects/{project}/retention-policy', { params: { path: { project: editing!.id } }, body: { enabled, run_days: Number(days), keep_baselines: keepBaselines } })),
    onSuccess: (p) => {
      toast.success(p.enabled ? `Runs older than ${p.run_days} days will be deleted` : 'Retention paused')
      setEditing(null)
      void refresh()
    },
  })
  const clear = useMutation({
    mutationFn: () => unwrap(http.DELETE('/v1/projects/{project}/retention-policy', { params: { path: { project: editing!.id } } })),
    onSuccess: () => {
      toast.success('Runs are kept forever')
      setEditing(null)
      void refresh()
    },
  })
  const n = Number(days)
  const daysValid = Number.isInteger(n) && n >= 1 && n <= 3650
  if (!canEdit) {
    return <SectionCard title="Data retention"><CardBody className="text-sm text-text-muted">Only admins can manage data retention.</CardBody></SectionCard>
  }
  const current = editing ? policyOf(editing.id) : undefined
  return (
    <SectionCard title="Data retention" description="Delete finished runs, with their results and scores, after a number of days. Applied every hour. Runs in progress are never touched.">
      {projects.isPending || policies.isPending ? <Loading rows={3} /> : projects.error || policies.error ? <ErrorPanel error={projects.error ?? policies.error} onRetry={() => void policies.refetch()} /> : projects.data!.length === 0 ? (
        <EmptyState compact illustration="generic" title="No projects yet" description="Retention is set per project." />
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="border-b border-border bg-surface-2 text-xs text-text-muted">
              <tr>
                <th scope="col" className="px-4 py-2 text-left font-medium">Project</th>
                <th scope="col" className="px-4 py-2 text-left font-medium">Policy</th>
                <th scope="col" className="hidden px-4 py-2 text-left font-medium sm:table-cell">Last applied</th>
                <th scope="col" className="hidden px-4 py-2 text-right font-medium sm:table-cell">Runs deleted</th>
                <th scope="col" className="px-4 py-2"><span className="sr-only">Actions</span></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {projects.data!.map((pr) => {
                const p = policyOf(pr.id)
                return (
                  <tr key={pr.id}>
                    <td className="px-4 py-2.5 font-medium text-text">{pr.name}</td>
                    <td className="px-4 py-2.5">
                      {!p ? (
                        <span className="inline-flex items-center gap-1.5 text-text-muted"><InfinityIcon className="size-3.5" aria-hidden />Keep forever</span>
                      ) : (
                        <span className="flex flex-wrap items-center gap-1.5">
                          <span className="inline-flex items-center gap-1.5 text-text"><Archive className="size-3.5 text-text-muted" aria-hidden />After {p.run_days} days</span>
                          {!p.enabled && <Badge variant="warn">Paused</Badge>}
                          {p.keep_baselines && <Badge variant="outline">Keeps baselines</Badge>}
                        </span>
                      )}
                    </td>
                    <td className="hidden px-4 py-2.5 sm:table-cell"><RelTime iso={p?.last_applied_at} className="text-sm" /></td>
                    <td className="hidden px-4 py-2.5 text-right tabular-nums text-text-2 sm:table-cell">{p ? p.total_deleted.toLocaleString() : '—'}</td>
                    <td className="px-4 py-2.5 text-right"><Button size="sm" variant="ghost" onClick={() => setEditing(pr)}>{p ? 'Edit' : 'Set policy'}</Button></td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
      <Dialog open={!!editing} onOpenChange={(o) => !o && setEditing(null)} size="sm" title={`Retention for ${editing?.name ?? ''}`}
        footer={<>
          {current && <Button variant="ghost" className="mr-auto" loading={clear.isPending} onClick={() => clear.mutate()}>Keep forever</Button>}
          <Button variant="ghost" onClick={() => setEditing(null)}>Cancel</Button>
          <Button variant="primary" disabled={!daysValid} loading={save.isPending} onClick={() => save.mutate()}>Save policy</Button>
        </>}>
        <div className="flex flex-col gap-4">
          <Field label="Delete finished runs older than" error={days && !daysValid ? 'Between 1 and 3650 days' : undefined}>
            <Input type="number" min={1} max={3650} inputMode="numeric" trailing={<span className="text-xs text-text-muted">days</span>} value={days} onChange={(e) => setDays(e.target.value)} />
          </Field>
          <div className="flex flex-col gap-3 rounded-md border border-border p-3">
            <ToggleRow label="Keep pinned baselines" hint="Never delete a run pinned as a suite baseline." checked={keepBaselines} onChange={setKeepBaselines} />
            <ToggleRow label="Enabled" hint="Pause to keep the policy without deleting anything." checked={enabled} onChange={setEnabled} />
          </div>
          {current && current.eligible_runs > 0 && current.run_days === n && current.keep_baselines === keepBaselines && (
            <p className="rounded-md bg-warn-subtle px-3 py-2 text-sm text-warn">{current.eligible_runs >= 1000 ? '1,000+' : current.eligible_runs} runs are past this limit and will be deleted within the hour.</p>
          )}
        </div>
      </Dialog>
    </SectionCard>
  )
}
