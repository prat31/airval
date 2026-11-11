/* eslint-disable react-hooks/set-state-in-effect -- local drafts are seeded from server data */
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, Outlet, useParams } from '@tanstack/react-router'
import { Bell, KeyRound, Plus, Send, Settings, Trash2, UserPlus } from 'lucide-react'
import { useEffect, useState } from 'react'
import { settingsSections, type SettingsSection } from '@/app/nav'
import { Screen } from '@/app/screen'
import { themeOptions } from '@/app/theme-toggle'
import { Badge } from '@/components/ui/badge'
import { Button, IconButton } from '@/components/ui/button'
import { Card, CardBody, CardHeader } from '@/components/ui/card'
import { CopyButton } from '@/components/ui/code-block'
import { Dialog } from '@/components/ui/dialog'
import { EmptyState } from '@/components/ui/empty-state'
import { Field } from '@/components/ui/field'
import { Input, Textarea } from '@/components/ui/input'
import { PageHeader } from '@/components/ui/page-header'
import { SegmentedControl } from '@/components/ui/segmented-control'
import { Select } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { toast } from '@/components/ui/toast'
import { cn } from '@/lib/cn'
import { useTheme, type ThemePreference } from '@/lib/theme'
import { errorMessage, fetchAll, http, unwrap, type Obj, type S } from '../shared/api'
import { useProjectCtx } from '../shared/context'
import { ConfirmDialog, ErrorPanel, KvEditor, RelTime, TagInput } from '../shared/ui'
import { SecretPicker } from '../shared/secrets'

export function SettingsLayout() {
  const { params, org } = useProjectCtx()
  const { section } = useParams({ strict: false }) as { section?: string }
  return (
    <Screen>
      <PageHeader icon={<Settings />} title="Settings" description={`Manage ${org?.name ?? 'your organization'}.`} />
      <div className="grid grid-cols-1 gap-6 md:grid-cols-[200px_1fr]">
        <nav aria-label="Settings" className="flex gap-1 overflow-x-auto md:flex-col">
          {settingsSections.map((s) => (
            <Link
              key={s.id}
              to="/o/$org/p/$project/settings/$section"
              params={{ ...params, section: s.id }}
              className={cn('flex h-8 shrink-0 items-center rounded-md px-2.5 text-sm font-medium transition-colors focus-ring', section === s.id ? 'bg-surface text-text shadow-[0_0_0_1px_var(--border)]' : 'text-text-2 hover:bg-surface-hover hover:text-text')}
            >
              {s.label}
            </Link>
          ))}
        </nav>
        <div className="min-w-0">
          <Outlet />
        </div>
      </div>
    </Screen>
  )
}

const roles: { value: S['Role']; label: string }[] = [
  { value: 'owner', label: 'Owner' },
  { value: 'admin', label: 'Admin' },
  { value: 'editor', label: 'Editor' },
  { value: 'viewer', label: 'Viewer' },
]

function SectionCard({ title, description, actions, children }: { title: string; description?: string; actions?: React.ReactNode; children: React.ReactNode }) {
  return (
    <Card>
      <CardHeader title={title} description={description} actions={actions} />
      {children}
    </Card>
  )
}

function Loading() {
  return <CardBody><Skeleton className="h-24 w-full" /></CardBody>
}

/* ------------------------------------------------------------------------------------------ general */
function General() {
  const { org } = useProjectCtx()
  const qc = useQueryClient()
  const [name, setName] = useState('')
  const [slug, setSlug] = useState('')
  useEffect(() => {
    if (org) {
      setName(org.name)
      setSlug(org.slug)
    }
  }, [org])
  const save = useMutation({
    mutationFn: () => unwrap(http.PATCH('/v1/orgs/{org}', { params: { path: { org: org!.id } }, body: { name: name.trim(), slug } })),
    onSuccess: () => {
      toast.success('Organization updated')
      void qc.invalidateQueries({ queryKey: ['orgs'] })
    },
  })
  if (!org) return <SectionCard title="General"><Loading /></SectionCard>
  const dirty = name !== org.name || slug !== org.slug
  return (
    <SectionCard title="General" description="Organization name and URL slug.">
      <CardBody className="flex max-w-md flex-col gap-4">
        <Field label="Name"><Input value={name} onChange={(e) => setName(e.target.value)} /></Field>
        <Field label="Slug" hint="Used in URLs. Changing it breaks existing links."><Input className="font-mono text-xs" value={slug} onChange={(e) => setSlug(e.target.value)} /></Field>
        <div><Button variant="primary" disabled={!dirty || !name.trim()} loading={save.isPending} onClick={() => save.mutate()}>Save changes</Button></div>
      </CardBody>
    </SectionCard>
  )
}

/* ------------------------------------------------------------------------------------------ members */
function Members() {
  const { orgId } = useProjectCtx()
  const qc = useQueryClient()
  const members = useQuery({
    queryKey: ['members', orgId],
    enabled: !!orgId,
    queryFn: () => fetchAll<S['Member']>(async (cursor) => (await http.GET('/v1/orgs/{org}/members', { params: { path: { org: orgId! }, query: { cursor, limit: 200 } } })).data ?? { items: [] }),
  })
  const refresh = () => qc.invalidateQueries({ queryKey: ['members', orgId] })
  const setRole = useMutation({
    mutationFn: ({ user, role }: { user: string; role: S['Role'] }) => unwrap(http.PATCH('/v1/orgs/{org}/members/{user}', { params: { path: { org: orgId!, user } }, body: { role } })),
    onSuccess: refresh,
  })
  const remove = useMutation({
    mutationFn: (user: string) => unwrap(http.DELETE('/v1/orgs/{org}/members/{user}', { params: { path: { org: orgId!, user } } })),
    onSuccess: refresh,
  })
  const [inviting, setInviting] = useState(false)
  const [email, setEmail] = useState('')
  const [role, setRole_] = useState<S['Role']>('editor')
  const [password, setPassword] = useState('')
  const [toRemove, setToRemove] = useState<S['Member'] | null>(null)
  const invite = useMutation({
    mutationFn: () => unwrap(http.POST('/v1/orgs/{org}/members', { params: { path: { org: orgId! } }, body: { email: email.trim(), role, password: password || undefined } })),
    onSuccess: () => {
      toast.success('Member added')
      setInviting(false)
      setEmail('')
      setPassword('')
      void refresh()
    },
  })
  return (
    <SectionCard title="Members" description="People with access to this organization." actions={<Button size="sm" icon={<UserPlus />} onClick={() => setInviting(true)}>Invite</Button>}>
      {members.isLoading ? <Loading /> : members.error ? <ErrorPanel error={members.error} onRetry={() => void members.refetch()} /> : (
        <ul className="divide-y divide-border">
          {members.data!.map((m) => (
            <li key={m.user.id} className="flex items-center gap-3 px-4 py-2.5">
              <span className="flex size-8 shrink-0 items-center justify-center rounded-full bg-accent-subtle text-xs font-semibold text-accent">{(m.user.name || m.user.email).slice(0, 2).toUpperCase()}</span>
              <div className="min-w-0 flex-1">
                <div className="truncate text-sm font-medium text-text">{m.user.name || m.user.email}</div>
                <div className="truncate text-xs text-text-muted">{m.user.email}</div>
              </div>
              <Select size="sm" aria-label={`Role for ${m.user.email}`} className="w-28" value={m.role} onValueChange={(r) => setRole.mutate({ user: m.user.id, role: r as S['Role'] }, { onError: (e) => toast.error(errorMessage(e)) })} options={roles} />
              <IconButton label={`Remove ${m.user.email}`} size="sm" onClick={() => setToRemove(m)}><Trash2 /></IconButton>
            </li>
          ))}
        </ul>
      )}
      <Dialog open={inviting} onOpenChange={setInviting} size="sm" title="Add a member" description="They get access to every project in this organization."
        footer={<><Button variant="ghost" onClick={() => setInviting(false)}>Cancel</Button><Button variant="primary" disabled={!email.includes('@')} loading={invite.isPending} onClick={() => invite.mutate()}>Add member</Button></>}>
        <div className="flex flex-col gap-4">
          <Field label="Email"><Input type="email" autoFocus value={email} onChange={(e) => setEmail(e.target.value)} placeholder="teammate@company.com" /></Field>
          <Field label="Role"><Select value={role} onValueChange={(r) => setRole_(r as S['Role'])} options={roles} /></Field>
          <Field label="Initial password" optional hint="Only needed when this email has no account yet."><Input type="password" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} /></Field>
        </div>
      </Dialog>
      <ConfirmDialog open={!!toRemove} onOpenChange={(o) => !o && setToRemove(null)} title={`Remove ${toRemove?.user.email}?`} description="They lose access immediately." confirmLabel="Remove" loading={remove.isPending}
        onConfirm={() => toRemove && remove.mutate(toRemove.user.id, { onSuccess: () => setToRemove(null), onError: (e) => toast.error(errorMessage(e)) })} />
    </SectionCard>
  )
}

/* ------------------------------------------------------------------------------------------ API keys */
function ApiKeys() {
  const { orgId } = useProjectCtx()
  const qc = useQueryClient()
  const keys = useQuery({
    queryKey: ['api-keys', orgId],
    enabled: !!orgId,
    queryFn: () => fetchAll<S['ApiKey']>(async (cursor) => (await http.GET('/v1/orgs/{org}/api-keys', { params: { path: { org: orgId! }, query: { cursor, limit: 200 } } })).data ?? { items: [] }),
  })
  const [creating, setCreating] = useState(false)
  const [name, setName] = useState('')
  const [role, setRole] = useState<S['Role']>('editor')
  const [created, setCreated] = useState<S['ApiKeyCreated'] | null>(null)
  const [toRevoke, setToRevoke] = useState<S['ApiKey'] | null>(null)
  const create = useMutation({
    mutationFn: () => unwrap(http.POST('/v1/orgs/{org}/api-keys', { params: { path: { org: orgId! } }, body: { name: name.trim(), role } })),
    onSuccess: (k) => {
      setCreating(false)
      setName('')
      setCreated(k)
      void qc.invalidateQueries({ queryKey: ['api-keys', orgId] })
    },
  })
  const revoke = useMutation({
    mutationFn: (id: string) => unwrap(http.DELETE('/v1/orgs/{org}/api-keys/{key}', { params: { path: { org: orgId!, key: id } } })),
    onSuccess: () => {
      setToRevoke(null)
      toast.success('Key revoked')
      void qc.invalidateQueries({ queryKey: ['api-keys', orgId] })
    },
  })
  return (
    <SectionCard title="API keys" description="Keys for CI and the airval CLI. Send as Authorization: Bearer." actions={<Button size="sm" icon={<Plus />} onClick={() => setCreating(true)}>Create key</Button>}>
      {keys.isLoading ? <Loading /> : keys.error ? <ErrorPanel error={keys.error} onRetry={() => void keys.refetch()} /> : keys.data!.length === 0 ? (
        <EmptyState compact illustration="generic" title="No API keys" description="Create a key to start runs from CI." action={<Button size="sm" variant="primary" onClick={() => setCreating(true)}>Create key</Button>} />
      ) : (
        <ul className="divide-y divide-border">
          {keys.data!.map((k) => (
            <li key={k.id} className="flex items-center gap-3 px-4 py-2.5">
              <KeyRound className="size-4 text-text-muted" />
              <div className="min-w-0 flex-1">
                <div className="truncate text-sm font-medium text-text">{k.name}</div>
                <div className="text-xs text-text-muted"><code className="font-mono">{k.prefix}…</code> · last used <RelTime iso={k.last_used_at} className="text-xs text-text-muted" /></div>
              </div>
              <Badge>{k.role}</Badge>
              <Button size="sm" variant="ghost" onClick={() => setToRevoke(k)}>Revoke</Button>
            </li>
          ))}
        </ul>
      )}
      <Dialog open={creating} onOpenChange={setCreating} size="sm" title="Create API key"
        footer={<><Button variant="ghost" onClick={() => setCreating(false)}>Cancel</Button><Button variant="primary" disabled={!name.trim()} loading={create.isPending} onClick={() => create.mutate()}>Create key</Button></>}>
        <div className="flex flex-col gap-4">
          <Field label="Name"><Input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="GitHub Actions" /></Field>
          <Field label="Role" hint="Editors can start runs; viewers are read-only."><Select value={role} onValueChange={(r) => setRole(r as S['Role'])} options={roles.filter((r) => r.value !== 'owner')} /></Field>
        </div>
      </Dialog>
      <Dialog open={!!created} onOpenChange={(o) => !o && setCreated(null)} title="Your new API key" description="Copy it now. It is shown only once." footer={<Button variant="primary" onClick={() => setCreated(null)}>Done</Button>}>
        <div className="flex items-center gap-2 rounded-sm border border-border bg-surface-2 px-2.5 py-2">
          <code className="min-w-0 flex-1 break-all font-mono text-xs text-text">{created?.key}</code>
          <CopyButton value={created?.key ?? ''} label="Copy key" />
        </div>
      </Dialog>
      <ConfirmDialog open={!!toRevoke} onOpenChange={(o) => !o && setToRevoke(null)} title={`Revoke ${toRevoke?.name}?`} description="Anything using this key stops working immediately." confirmLabel="Revoke" loading={revoke.isPending} onConfirm={() => toRevoke && revoke.mutate(toRevoke.id)} />
    </SectionCard>
  )
}

/* ------------------------------------------------------------------------------------------ secrets */
function Secrets() {
  const { orgId } = useProjectCtx()
  const qc = useQueryClient()
  const secrets = useQuery({
    queryKey: ['secrets', orgId],
    enabled: !!orgId,
    queryFn: () => fetchAll<S['Secret']>(async (cursor) => (await http.GET('/v1/orgs/{org}/secrets', { params: { path: { org: orgId! }, query: { all: true, limit: 1000, cursor } } })).data ?? { items: [] }),
  })
  const [editing, setEditing] = useState<S['Secret'] | 'new' | null>(null)
  const [name, setName] = useState('')
  const [value, setValue] = useState('')
  const [description, setDescription] = useState('')
  const [toDelete, setToDelete] = useState<S['Secret'] | null>(null)
  useEffect(() => {
    if (editing) {
      setName(editing === 'new' ? '' : editing.name)
      setDescription(editing === 'new' ? '' : editing.description)
      setValue('')
    }
  }, [editing])
  const done = () => {
    setEditing(null)
    void qc.invalidateQueries({ queryKey: ['secrets', orgId] })
  }
  const save = useMutation({
    mutationFn: () =>
      editing === 'new' || !editing
        ? unwrap(http.POST('/v1/orgs/{org}/secrets', { params: { path: { org: orgId! } }, body: { name, value, description } }))
        : unwrap(http.PATCH('/v1/orgs/{org}/secrets/{secret}', { params: { path: { org: orgId!, secret: editing.id } }, body: { value: value || undefined, description } })),
    onSuccess: () => {
      toast.success('Secret saved')
      done()
    },
  })
  const remove = useMutation({
    mutationFn: (id: string) => unwrap(http.DELETE('/v1/orgs/{org}/secrets/{secret}', { params: { path: { org: orgId!, secret: id } } })),
    onSuccess: () => {
      setToDelete(null)
      void qc.invalidateQueries({ queryKey: ['secrets', orgId] })
    },
  })
  const nameOk = /^[A-Za-z_][A-Za-z0-9_]{0,127}$/.test(name)
  return (
    <SectionCard title="Secrets" description="Encrypted credentials. Reference them as {{ secret &quot;NAME&quot; }}. Values are write-only." actions={<Button size="sm" icon={<Plus />} onClick={() => setEditing('new')}>Add secret</Button>}>
      {secrets.isLoading ? <Loading /> : secrets.error ? <ErrorPanel error={secrets.error} onRetry={() => void secrets.refetch()} /> : secrets.data!.length === 0 ? (
        <EmptyState compact illustration="generic" title="No secrets yet" description="Store API keys for your targets and LLM judges here." action={<Button size="sm" variant="primary" onClick={() => setEditing('new')}>Add secret</Button>} />
      ) : (
        <ul className="divide-y divide-border">
          {secrets.data!.map((s) => (
            <li key={s.id} className="flex items-center gap-3 px-4 py-2.5">
              <div className="min-w-0 flex-1">
                <code className="font-mono text-sm text-text">{s.name}</code>
                <div className="truncate text-xs text-text-muted">{s.description || 'No description'} · updated <RelTime iso={s.updated_at} className="text-xs text-text-muted" /></div>
              </div>
              <span className="font-mono text-xs text-text-muted" aria-label="Value hidden">••••••••</span>
              <Button size="sm" variant="ghost" onClick={() => setEditing(s)}>Replace value</Button>
              <IconButton label={`Delete ${s.name}`} size="sm" onClick={() => setToDelete(s)}><Trash2 /></IconButton>
            </li>
          ))}
        </ul>
      )}
      <Dialog open={!!editing} onOpenChange={(o) => !o && setEditing(null)} size="sm" title={editing === 'new' ? 'Add secret' : `Replace ${name}`} description="The value is encrypted at rest and can't be viewed again."
        footer={<><Button variant="ghost" onClick={() => setEditing(null)}>Cancel</Button><Button variant="primary" disabled={(editing === 'new' && (!nameOk || !value)) || (editing !== 'new' && !value && editing?.description === description)} loading={save.isPending} onClick={() => save.mutate()}>Save</Button></>}>
        <div className="flex flex-col gap-4">
          <Field label="Name" error={editing === 'new' && name && !nameOk ? 'Letters, digits and underscores; must not start with a digit' : undefined}>
            <Input className="font-mono text-xs" disabled={editing !== 'new'} value={name} onChange={(e) => setName(e.target.value)} placeholder="OPENAI_API_KEY" autoFocus={editing === 'new'} />
          </Field>
          <Field label="Value"><Textarea mono rows={3} autoComplete="off" value={value} onChange={(e) => setValue(e.target.value)} placeholder={editing === 'new' ? 'Paste the secret' : 'Paste a new value'} /></Field>
          <Field label="Description" optional><Input value={description} onChange={(e) => setDescription(e.target.value)} /></Field>
        </div>
      </Dialog>
      <ConfirmDialog open={!!toDelete} onOpenChange={(o) => !o && setToDelete(null)} title={`Delete ${toDelete?.name}?`} description="Targets and judges referencing it will fail until updated." loading={remove.isPending} onConfirm={() => toDelete && remove.mutate(toDelete.id)} />
    </SectionCard>
  )
}

/* ------------------------------------------------------------------------------------------ notifications */
const eventOptions: { value: S['NotificationEvent']; label: string }[] = [
  { value: 'run.finished', label: 'Run finished' },
  { value: 'run.failed', label: 'Run failed' },
  { value: 'run.errored', label: 'Run errored' },
  { value: 'gate.failed', label: 'Gate failed' },
]

function Notifications() {
  const { orgId } = useProjectCtx()
  const qc = useQueryClient()
  const channels = useQuery({
    queryKey: ['channels', orgId],
    enabled: !!orgId,
    queryFn: () => fetchAll<S['NotificationChannel']>(async (cursor) => (await http.GET('/v1/orgs/{org}/notification-channels', { params: { path: { org: orgId! }, query: { cursor, limit: 100 } } })).data ?? { items: [] }),
  })
  const refresh = () => qc.invalidateQueries({ queryKey: ['channels', orgId] })
  const [editing, setEditing] = useState<S['NotificationChannel'] | 'new' | null>(null)
  const [name, setName] = useState('')
  const [kind, setKind] = useState<S['NotificationChannelKind']>('slack')
  const [url, setUrl] = useState('')
  const [headers, setHeaders] = useState<Record<string, string>>({})
  const [signing, setSigning] = useState('')
  const [recipients, setRecipients] = useState<string[]>([])
  const [events, setEvents] = useState<S['NotificationEvent'][]>(['gate.failed', 'run.failed'])
  const [toDelete, setToDelete] = useState<S['NotificationChannel'] | null>(null)
  useEffect(() => {
    if (!editing) return
    const c = editing === 'new' ? null : editing
    const cfg = (c?.config ?? {}) as Obj
    setName(c?.name ?? '')
    setKind(c?.kind ?? 'slack')
    setUrl(String(cfg.url ?? ''))
    setHeaders((cfg.headers as Record<string, string>) ?? {})
    setSigning(String(cfg.signing_secret ?? ''))
    setRecipients((cfg.recipients as string[]) ?? [])
    setEvents(c?.events ?? ['gate.failed', 'run.failed'])
  }, [editing])
  const config = (): S['NotificationChannelConfig'] => (kind === 'email' ? { recipients } : kind === 'slack' ? { url } : { url, headers, signing_secret: signing || undefined })
  const save = useMutation({
    mutationFn: () =>
      editing === 'new' || !editing
        ? unwrap(http.POST('/v1/orgs/{org}/notification-channels', { params: { path: { org: orgId! } }, body: { name: name.trim(), kind, config: config(), events, enabled: true } }))
        : unwrap(http.PATCH('/v1/orgs/{org}/notification-channels/{channel}', { params: { path: { org: orgId!, channel: editing.id } }, body: { name: name.trim(), config: config(), events } })),
    onSuccess: () => {
      toast.success('Channel saved')
      setEditing(null)
      void refresh()
    },
  })
  const toggle = useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) => unwrap(http.PATCH('/v1/orgs/{org}/notification-channels/{channel}', { params: { path: { org: orgId!, channel: id } }, body: { enabled } })),
    onSuccess: refresh,
  })
  const test = useMutation({
    mutationFn: (id: string) => unwrap(http.POST('/v1/orgs/{org}/notification-channels/{channel}:test', { params: { path: { org: orgId!, channel: id } } })),
    onSuccess: () => toast.success('Test notification sent'),
    onError: (e) => toast.error(`Test failed: ${errorMessage(e)}`),
  })
  const remove = useMutation({
    mutationFn: (id: string) => unwrap(http.DELETE('/v1/orgs/{org}/notification-channels/{channel}', { params: { path: { org: orgId!, channel: id } } })),
    onSuccess: () => {
      setToDelete(null)
      void refresh()
    },
  })
  const valid = name.trim() && (kind === 'email' ? recipients.length > 0 : /^https?:\/\//.test(url) || url.includes('{{'))
  return (
    <SectionCard title="Notifications" description="Tell Slack, a webhook or email when runs finish or gates fail." actions={<Button size="sm" icon={<Plus />} onClick={() => setEditing('new')}>Add channel</Button>}>
      {channels.isLoading ? <Loading /> : channels.error ? <ErrorPanel error={channels.error} onRetry={() => void channels.refetch()} /> : channels.data!.length === 0 ? (
        <EmptyState compact illustration="generic" title="No channels yet" description="Add a Slack webhook, a generic webhook or an email list." action={<Button size="sm" variant="primary" onClick={() => setEditing('new')}>Add channel</Button>} />
      ) : (
        <ul className="divide-y divide-border">
          {channels.data!.map((c) => (
            <li key={c.id} className="flex items-center gap-3 px-4 py-2.5">
              <Bell className="size-4 text-text-muted" />
              <button type="button" className="min-w-0 flex-1 text-left focus-ring rounded-sm" onClick={() => setEditing(c)}>
                <div className="flex items-center gap-2"><span className="truncate text-sm font-medium text-text">{c.name}</span><Badge>{c.kind}</Badge></div>
                <div className="truncate text-xs text-text-muted">{c.events.map((e) => e.replace('.', ' ')).join(', ') || 'No events'}</div>
              </button>
              <Button size="sm" variant="ghost" icon={<Send />} loading={test.isPending && test.variables === c.id} onClick={() => test.mutate(c.id)}>Test</Button>
              <Switch size="sm" aria-label={`${c.name} enabled`} checked={c.enabled} onCheckedChange={(enabled) => toggle.mutate({ id: c.id, enabled })} />
              <IconButton label={`Delete ${c.name}`} size="sm" onClick={() => setToDelete(c)}><Trash2 /></IconButton>
            </li>
          ))}
        </ul>
      )}
      <Dialog open={!!editing} onOpenChange={(o) => !o && setEditing(null)} title={editing === 'new' ? 'Add channel' : 'Edit channel'}
        footer={<><Button variant="ghost" onClick={() => setEditing(null)}>Cancel</Button><Button variant="primary" disabled={!valid} loading={save.isPending} onClick={() => save.mutate()}>Save channel</Button></>}>
        <div className="flex flex-col gap-4">
          <SegmentedControl aria-label="Channel type" value={kind} onValueChange={(k) => editing === 'new' && setKind(k)} options={[{ value: 'slack', label: 'Slack' }, { value: 'webhook', label: 'Webhook' }, { value: 'email', label: 'Email' }]} />
          <Field label="Name"><Input value={name} onChange={(e) => setName(e.target.value)} placeholder="#evals-alerts" /></Field>
          {kind !== 'email' && (
            <Field label={kind === 'slack' ? 'Incoming webhook URL' : 'Destination URL'} hint={'You can reference a secret: {{ secret "SLACK_URL" }}'}>
              <Input className="font-mono text-xs" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://hooks.slack.com/services/…" />
            </Field>
          )}
          {kind === 'webhook' && (
            <>
              <Field label="Signing secret" optional hint="Payloads are signed with HMAC-SHA256."><SecretPicker value={signing} onChange={setSigning} /></Field>
              <Field label="Headers" optional><KvEditor value={headers} onChange={setHeaders} addLabel="Add header" monoValue /></Field>
            </>
          )}
          {kind === 'email' && <Field label="Recipients"><TagInput value={recipients} onChange={setRecipients} placeholder="name@company.com" /></Field>}
          <Field label="Notify on">
            <div className="flex flex-wrap gap-1.5">
              {eventOptions.map((o) => {
                const on = events.includes(o.value)
                return <button key={o.value} type="button" aria-pressed={on} onClick={() => setEvents(on ? events.filter((x) => x !== o.value) : [...events, o.value])} className={cn('inline-flex h-6 items-center rounded-sm px-2 text-xs ring-1 ring-inset transition-colors focus-ring', on ? 'bg-accent-subtle text-accent ring-accent' : 'text-text-2 ring-border hover:bg-surface-hover')}>{o.label}</button>
              })}
            </div>
          </Field>
        </div>
      </Dialog>
      <ConfirmDialog open={!!toDelete} onOpenChange={(o) => !o && setToDelete(null)} title={`Delete ${toDelete?.name}?`} loading={remove.isPending} onConfirm={() => toDelete && remove.mutate(toDelete.id)} />
    </SectionCard>
  )
}

function Appearance() {
  const { preference, setPreference } = useTheme()
  return (
    <Card>
      <CardHeader title="Theme" description="Use light, dark, or follow your system setting." />
      <CardBody>
        <SegmentedControl aria-label="Theme" value={preference} onValueChange={(v) => setPreference(v as ThemePreference)} options={themeOptions.map((o) => ({ value: o.value, label: o.label, icon: <o.icon /> }))} />
      </CardBody>
    </Card>
  )
}

export function SettingsSectionScreen() {
  const { section } = useParams({ strict: false }) as { section: SettingsSection }
  switch (section) {
    case 'general': return <General />
    case 'members': return <Members />
    case 'api-keys': return <ApiKeys />
    case 'secrets': return <Secrets />
    case 'notifications': return <Notifications />
    case 'appearance': return <Appearance />
    default: return <Card><EmptyState compact illustration="generic" title="Unknown section" /></Card>
  }
}
