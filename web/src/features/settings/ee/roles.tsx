/* eslint-disable react-hooks/set-state-in-effect -- local drafts are seeded from server data */
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Lock, Plus, Shield, Trash2, UserCog } from 'lucide-react'
import { Fragment, useEffect, useMemo, useState } from 'react'
import { Badge } from '@/components/ui/badge'
import { Button, IconButton } from '@/components/ui/button'
import { CardBody } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog } from '@/components/ui/dialog'
import { EmptyState } from '@/components/ui/empty-state'
import { Field } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { toast } from '@/components/ui/toast'
import { projectsQuery } from '@/features/auth/api'
import { fetchAll, http, unwrap, type S } from '../../shared/api'
import { useProjectCtx } from '../../shared/context'
import { ConfirmDialog, ErrorPanel } from '../../shared/ui'
import { builtinRoles, Loading, roleRefLabel, roleRefOptions, SectionCard } from './common'

export function useCustomRoles() {
  const { orgId } = useProjectCtx()
  return useQuery({
    queryKey: ['roles', orgId],
    enabled: !!orgId,
    queryFn: () => unwrap(http.GET('/v1/orgs/{org}/roles', { params: { path: { org: orgId! } } })),
  })
}

export function useMembers() {
  const { orgId } = useProjectCtx()
  return useQuery({
    queryKey: ['members', orgId],
    enabled: !!orgId,
    queryFn: () => fetchAll<S['Member']>(async (cursor) => (await http.GET('/v1/orgs/{org}/members', { params: { path: { org: orgId! }, query: { cursor, limit: 200 } } })).data ?? { items: [] }),
  })
}

export function RolesSettings() {
  return (
    <div className="flex flex-col gap-6">
      <RolesCard />
      <AssignmentsCard />
    </div>
  )
}

const actionOrder = ['read', 'write', 'execute', 'annotate', 'manage']
const resourceLabels: Record<string, string> = {
  projects: 'Projects', targets: 'Targets', datasets: 'Datasets', evaluators: 'Evaluators', suites: 'Suites', runs: 'Runs',
  comparisons: 'Comparisons', review: 'Review', members: 'Members', secrets: 'Secrets', notifications: 'Notifications',
  api_keys: 'API keys', audit: 'Audit log',
}

function RolesCard() {
  const { orgId, org } = useProjectCtx()
  const qc = useQueryClient()
  const roles = useCustomRoles()
  const canEdit = org?.role === 'owner' || org?.role === 'admin'
  const [editing, setEditing] = useState<S['CustomRole'] | 'new' | null>(null)
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [perms, setPerms] = useState<string[]>([])
  const [toDelete, setToDelete] = useState<S['CustomRole'] | null>(null)
  useEffect(() => {
    if (!editing) return
    const r = editing === 'new' ? null : editing
    setName(r?.name ?? '')
    setDescription(r?.description ?? '')
    setPerms(r?.permissions ?? [])
  }, [editing])
  const refresh = () => qc.invalidateQueries({ queryKey: ['roles', orgId] })
  const save = useMutation({
    mutationFn: () => {
      const body = { name: name.trim(), description, permissions: perms }
      return editing === 'new' || !editing
        ? unwrap(http.POST('/v1/orgs/{org}/roles', { params: { path: { org: orgId! } }, body }))
        : unwrap(http.PATCH('/v1/orgs/{org}/roles/{id}', { params: { path: { org: orgId!, id: editing.id } }, body }))
    },
    onSuccess: () => {
      toast.success('Role saved')
      setEditing(null)
      void refresh()
    },
  })
  const remove = useMutation({
    mutationFn: (id: string) => unwrap(http.DELETE('/v1/orgs/{org}/roles/{id}', { params: { path: { org: orgId!, id } } })),
    onSuccess: () => {
      setToDelete(null)
      void refresh()
      void qc.invalidateQueries({ queryKey: ['role-assignments', orgId] })
    },
  })

  // Group the catalog by resource for the permission matrix.
  const grid = useMemo(() => {
    const byRes = new Map<string, S['Permission'][]>()
    for (const p of roles.data?.permissions ?? []) byRes.set(p.resource, [...(byRes.get(p.resource) ?? []), p])
    const actions = actionOrder.filter((a) => (roles.data?.permissions ?? []).some((p) => p.action === a))
    return { rows: [...byRes.entries()], actions }
  }, [roles.data])
  const builtinPerms = (r: S['Role']) => roles.data?.builtin.find((b) => b.role === r)?.permissions ?? []
  const toggle = (id: string, on: boolean) => setPerms((p) => (on ? [...p, id] : p.filter((x) => x !== id)))

  return (
    <SectionCard title="Roles" description="Built-in roles apply org-wide. Custom roles grant exactly the permissions you pick, for project resources."
      actions={canEdit && <Button size="sm" icon={<Plus />} onClick={() => setEditing('new')}>New role</Button>}>
      {roles.isPending ? <Loading rows={3} /> : roles.error ? <ErrorPanel error={roles.error} onRetry={() => void roles.refetch()} /> : (
        <ul className="divide-y divide-border">
          {builtinRoles.map((r) => (
            <li key={r.value} className="flex items-center gap-3 px-4 py-2.5">
              <Lock className="size-4 text-text-muted" aria-hidden />
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2"><span className="text-sm font-medium text-text">{r.label}</span><Badge variant="outline">Built-in</Badge></div>
                <div className="truncate text-xs text-text-muted">{r.description}</div>
              </div>
            </li>
          ))}
          {roles.data!.custom.map((r) => (
            <li key={r.id} className="flex items-center gap-3 px-4 py-2.5">
              <Shield className="size-4 text-accent" aria-hidden />
              <button type="button" disabled={!canEdit} className="min-w-0 flex-1 rounded-sm text-left focus-ring" onClick={() => setEditing(r)}>
                <div className="flex items-center gap-2"><span className="truncate text-sm font-medium text-text">{r.name}</span><Badge variant="accent">{r.permissions.length} permissions</Badge></div>
                <div className="truncate text-xs text-text-muted">{r.description || r.permissions.join(', ') || 'No permissions'}</div>
              </button>
              {canEdit && <IconButton label={`Delete ${r.name}`} size="sm" onClick={() => setToDelete(r)}><Trash2 /></IconButton>}
            </li>
          ))}
        </ul>
      )}
      <Dialog open={!!editing} onOpenChange={(o) => !o && setEditing(null)} size="lg" title={editing === 'new' ? 'New custom role' : `Edit ${name}`}
        description="Organization administration (members, keys, secrets, settings) always follows the member's built-in role."
        footer={<><Button variant="ghost" onClick={() => setEditing(null)}>Cancel</Button><Button variant="primary" disabled={!name.trim()} loading={save.isPending} onClick={() => save.mutate()}>Save role</Button></>}>
        <div className="flex flex-col gap-5">
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Field label="Name"><Input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="Evaluation runner" /></Field>
            <Field label="Description" optional><Input value={description} onChange={(e) => setDescription(e.target.value)} placeholder="Runs suites but can't edit them" /></Field>
          </div>
          <div className="flex flex-wrap items-center gap-2 text-sm text-text-2">
            <span>Start from</span>
            {(['viewer', 'editor', 'admin'] as const).map((r) => (
              <Button key={r} size="sm" variant="ghost" onClick={() => setPerms(builtinPerms(r))}>{builtinRoles.find((b) => b.value === r)!.label}</Button>
            ))}
            <Button size="sm" variant="ghost" onClick={() => setPerms([])}>Clear</Button>
          </div>
          <div className="overflow-x-auto rounded-md border border-border">
            <table className="w-full text-sm">
              <thead className="bg-surface-2 text-xs text-text-muted">
                <tr>
                  <th scope="col" className="px-3 py-2 text-left font-medium">Resource</th>
                  {grid.actions.map((a) => <th key={a} scope="col" className="px-3 py-2 text-center font-medium capitalize">{a}</th>)}
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {grid.rows.map(([res, ps]) => (
                  <tr key={res}>
                    <th scope="row" className="px-3 py-2 text-left font-medium text-text">{resourceLabels[res] ?? res}</th>
                    {grid.actions.map((a) => {
                      const p = ps.find((x) => x.action === a)
                      return (
                        <td key={a} className="px-3 py-2 text-center">
                          {p ? (
                            <span title={p.description} className="inline-flex">
                              <Checkbox aria-label={`${resourceLabels[res] ?? res}: ${a}`} checked={perms.includes(p.id)} onCheckedChange={(v) => toggle(p.id, v === true)} />
                            </span>
                          ) : <span className="text-text-muted" aria-hidden>·</span>}
                        </td>
                      )
                    })}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      </Dialog>
      <ConfirmDialog open={!!toDelete} onOpenChange={(o) => !o && setToDelete(null)} title={`Delete ${toDelete?.name}?`} description="Members assigned this role fall back to their built-in role." loading={remove.isPending} onConfirm={() => toDelete && remove.mutate(toDelete.id)} />
    </SectionCard>
  )
}

function AssignmentsCard() {
  const { orgId, org } = useProjectCtx()
  const qc = useQueryClient()
  const canEdit = org?.role === 'owner' || org?.role === 'admin'
  const roles = useCustomRoles()
  const members = useMembers()
  const projects = useQuery(projectsQuery(orgId ?? ''))
  const assignments = useQuery({
    queryKey: ['role-assignments', orgId],
    enabled: !!orgId && canEdit,
    queryFn: async () => (await unwrap(http.GET('/v1/orgs/{org}/role-assignments', { params: { path: { org: orgId! } } }))).items,
  })
  const [adding, setAdding] = useState(false)
  const [user, setUser] = useState('')
  const [scope, setScope] = useState('org')
  const [role, setRole] = useState('')
  const [toRemove, setToRemove] = useState<S['RoleAssignment'] | null>(null)
  const refresh = () => qc.invalidateQueries({ queryKey: ['role-assignments', orgId] })
  const add = useMutation({
    mutationFn: () => unwrap(http.PUT('/v1/orgs/{org}/role-assignments', { params: { path: { org: orgId! } }, body: { user_id: user, project_id: scope === 'org' ? null : scope, role } })),
    onSuccess: () => {
      toast.success('Access updated')
      setAdding(false)
      void refresh()
    },
  })
  const remove = useMutation({
    mutationFn: (a: S['RoleAssignment']) => unwrap(http.DELETE('/v1/orgs/{org}/role-assignments', { params: { path: { org: orgId! }, query: { user_id: a.user_id, project_id: a.project_id ?? undefined } } })),
    onSuccess: () => {
      setToRemove(null)
      void refresh()
    },
  })
  const member = (id: string) => members.data?.find((m) => m.user.id === id)
  const project = (id?: string | null) => (id ? projects.data?.find((p) => p.id === id)?.name ?? 'Deleted project' : null)
  const eligible = (members.data ?? []).filter((m) => m.role !== 'owner')
  const roleOptions = roleRefOptions(roles.data?.custom, { builtins: scope !== 'org' })
  useEffect(() => {
    if (!roleOptions.some((o) => o.value === role)) setRole(String(roleOptions[0]?.value ?? ''))
  }, [scope, roleOptions, role])

  if (!canEdit) {
    return <SectionCard title="Access overrides"><CardBody className="text-sm text-text-muted">Only admins can view and change per-project access.</CardBody></SectionCard>
  }
  const rows = assignments.data ?? []
  return (
    <SectionCard title="Access overrides" description="Give a member a custom role, or a different role in a single project. Project overrides win."
      actions={<Button size="sm" icon={<UserCog />} onClick={() => { setUser(eligible[0]?.user.id ?? ''); setScope('org'); setAdding(true) }}>Add override</Button>}>
      {assignments.isPending ? <Loading rows={2} /> : assignments.error ? <ErrorPanel error={assignments.error} onRetry={() => void assignments.refetch()} /> : rows.length === 0 ? (
        <EmptyState compact illustration="generic" title="Everyone has their built-in role" description="Overrides let you grant a custom role or per-project access." />
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="border-b border-border bg-surface-2 text-xs text-text-muted">
              <tr>
                <th scope="col" className="px-4 py-2 text-left font-medium">Member</th>
                <th scope="col" className="px-4 py-2 text-left font-medium">Scope</th>
                <th scope="col" className="px-4 py-2 text-left font-medium">Role</th>
                <th scope="col" className="px-4 py-2"><span className="sr-only">Actions</span></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {rows.map((a) => {
                const m = member(a.user_id)
                return (
                  <tr key={`${a.user_id}-${a.project_id ?? 'org'}`}>
                    <td className="px-4 py-2.5"><div className="font-medium text-text">{m?.user.name || m?.user.email || 'Former member'}</div><div className="text-xs text-text-muted">{m?.user.email} · {m?.role ?? '—'}</div></td>
                    <td className="px-4 py-2.5 text-text-2">{project(a.project_id) ?? 'Whole organization'}</td>
                    <td className="px-4 py-2.5"><span className="flex items-center gap-2"><Badge variant={a.role.startsWith('custom:') ? 'accent' : 'neutral'}>{roleRefLabel(a.role, roles.data?.custom)}</Badge>{a.source !== 'manual' && <Badge variant="outline">{a.source.toUpperCase()}</Badge>}</span></td>
                    <td className="px-4 py-2.5 text-right"><IconButton label="Remove override" size="sm" onClick={() => setToRemove(a)}><Trash2 /></IconButton></td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
      <Dialog open={adding} onOpenChange={setAdding} size="sm" title="Add access override"
        footer={<><Button variant="ghost" onClick={() => setAdding(false)}>Cancel</Button><Button variant="primary" disabled={!user || !role} loading={add.isPending} onClick={() => add.mutate()}>Save</Button></>}>
        <div className="flex flex-col gap-4">
          <Field label="Member" hint="Owners always have full access."><Select value={user} onValueChange={setUser} options={eligible.map((m) => ({ value: m.user.id, label: m.user.email }))} placeholder="Choose a member" /></Field>
          <Field label="Scope"><Select value={scope} onValueChange={setScope} options={[{ value: 'org', label: 'Whole organization' }, ...(projects.data ?? []).map((p) => ({ value: p.id, label: p.name }))]} /></Field>
          <Field label="Role" hint={scope === 'org' ? 'Org-wide built-in roles are set on the Members page.' : undefined}>
            {roleOptions.length ? <Select value={role} onValueChange={setRole} options={roleOptions} /> : <Fragment><Input disabled value="Create a custom role first" /></Fragment>}
          </Field>
        </div>
      </Dialog>
      <ConfirmDialog open={!!toRemove} onOpenChange={(o) => !o && setToRemove(null)} title="Remove this override?" description="The member goes back to their built-in role." confirmLabel="Remove" loading={remove.isPending} onConfirm={() => toRemove && remove.mutate(toRemove)} />
    </SectionCard>
  )
}

