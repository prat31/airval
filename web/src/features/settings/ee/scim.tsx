/* eslint-disable react-hooks/set-state-in-effect -- local drafts are seeded from server data */
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound, Plus } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Button } from '@/components/ui/button'
import { CardBody, CardFooter } from '@/components/ui/card'
import { CopyButton } from '@/components/ui/code-block'
import { Dialog } from '@/components/ui/dialog'
import { EmptyState } from '@/components/ui/empty-state'
import { Field } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { toast } from '@/components/ui/toast'
import { http, unwrap, type S } from '../../shared/api'
import { useProjectCtx } from '../../shared/context'
import { ConfirmDialog, ErrorPanel, RelTime } from '../../shared/ui'
import { builtinRoles, DetailRow, Loading, MappingEditor, SectionCard } from './common'
import { useCustomRoles } from './roles'

export function SCIMSettings() {
  const { org } = useProjectCtx()
  if (org && org.role !== 'owner') {
    return <SectionCard title="SCIM provisioning"><CardBody className="text-sm text-text-muted">Only owners can manage SCIM provisioning.</CardBody></SectionCard>
  }
  return (
    <div className="flex flex-col gap-6">
      <TokensCard />
      <MappingCard />
    </div>
  )
}

function TokensCard() {
  const { orgId } = useProjectCtx()
  const qc = useQueryClient()
  const tokens = useQuery({
    queryKey: ['scim-tokens', orgId],
    enabled: !!orgId,
    queryFn: () => unwrap(http.GET('/v1/orgs/{org}/scim-tokens', { params: { path: { org: orgId! } } })),
  })
  const [creating, setCreating] = useState(false)
  const [name, setName] = useState('')
  const [created, setCreated] = useState<S['SCIMTokenCreated'] | null>(null)
  const [toRevoke, setToRevoke] = useState<S['SCIMToken'] | null>(null)
  const refresh = () => qc.invalidateQueries({ queryKey: ['scim-tokens', orgId] })
  const create = useMutation({
    mutationFn: () => unwrap(http.POST('/v1/orgs/{org}/scim-tokens', { params: { path: { org: orgId! } }, body: { name: name.trim() } })),
    onSuccess: (t) => {
      setCreating(false)
      setName('')
      setCreated(t)
      void refresh()
    },
  })
  const revoke = useMutation({
    mutationFn: (id: string) => unwrap(http.DELETE('/v1/orgs/{org}/scim-tokens/{id}', { params: { path: { org: orgId!, id } } })),
    onSuccess: () => {
      setToRevoke(null)
      toast.success('Token revoked')
      void refresh()
    },
  })
  return (
    <SectionCard title="SCIM provisioning" description="Let your identity provider create, update and deactivate members over SCIM 2.0."
      actions={<Button size="sm" icon={<Plus />} onClick={() => setCreating(true)}>Create token</Button>}>
      {tokens.isPending ? <Loading rows={2} /> : tokens.error ? <ErrorPanel error={tokens.error} onRetry={() => void tokens.refetch()} /> : (
        <>
          <CardBody className="border-b border-border">
            <dl><DetailRow label="SCIM base URL"><span className="flex items-center gap-2"><code className="min-w-0 flex-1 break-all font-mono text-xs">{tokens.data!.base_url}</code><CopyButton value={tokens.data!.base_url} /></span></DetailRow></dl>
          </CardBody>
          {tokens.data!.items.length === 0 ? (
            <EmptyState compact illustration="generic" title="No SCIM tokens" description="Create a token and paste it, with the base URL, into your IdP's provisioning settings." action={<Button size="sm" variant="primary" onClick={() => setCreating(true)}>Create token</Button>} />
          ) : (
            <ul className="divide-y divide-border">
              {tokens.data!.items.map((t) => (
                <li key={t.id} className="flex items-center gap-3 px-4 py-2.5">
                  <KeyRound className="size-4 text-text-muted" aria-hidden />
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-sm font-medium text-text">{t.name}</div>
                    <div className="text-xs text-text-muted"><code className="font-mono">{t.prefix}…</code> · last used <RelTime iso={t.last_used_at} className="text-xs text-text-muted" /></div>
                  </div>
                  <Button size="sm" variant="ghost" onClick={() => setToRevoke(t)}>Revoke</Button>
                </li>
              ))}
            </ul>
          )}
        </>
      )}
      <Dialog open={creating} onOpenChange={setCreating} size="sm" title="Create SCIM token"
        footer={<><Button variant="ghost" onClick={() => setCreating(false)}>Cancel</Button><Button variant="primary" disabled={!name.trim()} loading={create.isPending} onClick={() => create.mutate()}>Create token</Button></>}>
        <Field label="Name" hint="Where the token is used."><Input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="Okta provisioning" /></Field>
      </Dialog>
      <Dialog open={!!created} onOpenChange={(o) => !o && setCreated(null)} title="Your SCIM token" description="Copy it now. It is shown only once." footer={<Button variant="primary" onClick={() => setCreated(null)}>Done</Button>}>
        <div className="flex flex-col gap-3">
          <Secret label="Token" value={created?.token ?? ''} />
          <Secret label="Base URL" value={created?.base_url ?? ''} />
        </div>
      </Dialog>
      <ConfirmDialog open={!!toRevoke} onOpenChange={(o) => !o && setToRevoke(null)} title={`Revoke ${toRevoke?.name}?`} description="Provisioning with this token stops immediately." confirmLabel="Revoke" loading={revoke.isPending} onConfirm={() => toRevoke && revoke.mutate(toRevoke.id)} />
    </SectionCard>
  )
}

function Secret({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="mb-1 text-xs font-medium text-text-muted">{label}</div>
      <div className="flex items-center gap-2 rounded-sm border border-border bg-surface-2 px-2.5 py-2">
        <code className="min-w-0 flex-1 break-all font-mono text-xs text-text">{value}</code>
        <CopyButton value={value} label={`Copy ${label.toLowerCase()}`} />
      </div>
    </div>
  )
}

function MappingCard() {
  const { orgId } = useProjectCtx()
  const qc = useQueryClient()
  const roles = useCustomRoles()
  const settings = useQuery({
    queryKey: ['scim-settings', orgId],
    enabled: !!orgId,
    queryFn: () => unwrap(http.GET('/v1/orgs/{org}/scim-settings', { params: { path: { org: orgId! } } })),
  })
  const [defaultRole, setDefaultRole] = useState<S['Role']>('viewer')
  const [rules, setRules] = useState<S['RoleMappingRule'][]>([])
  useEffect(() => {
    if (settings.data) {
      setDefaultRole(settings.data.default_role)
      setRules(settings.data.group_roles)
    }
  }, [settings.data])
  const save = useMutation({
    mutationFn: () => unwrap(http.PUT('/v1/orgs/{org}/scim-settings', { params: { path: { org: orgId! } }, body: { default_role: defaultRole, group_roles: rules.filter((r) => r.value.trim()) } })),
    onSuccess: () => {
      toast.success('Group mapping saved')
      void qc.invalidateQueries({ queryKey: ['scim-settings', orgId] })
    },
  })
  const dirty = settings.data && (defaultRole !== settings.data.default_role || JSON.stringify(rules) !== JSON.stringify(settings.data.group_roles))
  return (
    <SectionCard title="Group → role mapping" description="Provisioned members get the highest built-in role of their SCIM groups, or the default. Owners are never changed.">
      {settings.isPending ? <Loading rows={2} /> : settings.error ? <ErrorPanel error={settings.error} onRetry={() => void settings.refetch()} /> : (
        <>
          <CardBody className="flex flex-col gap-4">
            <Field label="Default role" className="max-w-xs"><Select value={defaultRole} onValueChange={(r) => setDefaultRole(r as S['Role'])} options={builtinRoles.filter((r) => r.value !== 'owner').map((r) => ({ value: r.value, label: r.label }))} /></Field>
            <MappingEditor value={rules} onChange={setRules} custom={roles.data?.custom} valueLabel="SCIM group" valuePlaceholder="Engineering" />
          </CardBody>
          <CardFooter className="flex justify-end">
            <Button variant="primary" disabled={!dirty} loading={save.isPending} onClick={() => save.mutate()}>Save mapping</Button>
          </CardFooter>
        </>
      )}
    </SectionCard>
  )
}
