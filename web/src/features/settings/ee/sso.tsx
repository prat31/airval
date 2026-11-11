/* eslint-disable react-hooks/set-state-in-effect -- local drafts are seeded from server data */
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound, Plus, ShieldCheck, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Badge } from '@/components/ui/badge'
import { Button, IconButton } from '@/components/ui/button'
import { CardBody } from '@/components/ui/card'
import { CopyButton } from '@/components/ui/code-block'
import { Dialog } from '@/components/ui/dialog'
import { EmptyState } from '@/components/ui/empty-state'
import { Field } from '@/components/ui/field'
import { Input, Textarea } from '@/components/ui/input'
import { SegmentedControl } from '@/components/ui/segmented-control'
import { Select } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { toast } from '@/components/ui/toast'
import { errorMessage, http, unwrap, type S } from '../../shared/api'
import { useProjectCtx } from '../../shared/context'
import { ConfirmDialog, ErrorPanel, FormSection, TagInput } from '../../shared/ui'
import { builtinRoles, DetailRow, Loading, MappingEditor, SectionCard } from './common'
import { useCustomRoles } from './roles'

type Kind = S['SSOProvider']['kind']

interface Draft {
  kind: Kind
  name: string
  slug: string
  enabled: boolean
  issuer: string
  clientId: string
  clientSecret: string
  scopes: string[]
  metadataMode: 'url' | 'xml'
  metadataUrl: string
  metadataXml: string
  emailAttribute: string
  domains: string[]
  roleClaim: string
  mapping: S['RoleMappingRule'][]
  defaultRole: S['Role']
  jit: boolean
  syncRoles: boolean
}

function draftFrom(p: S['SSOProvider'] | null): Draft {
  const c = p?.config ?? {}
  return {
    kind: p?.kind ?? 'oidc',
    name: p?.name ?? '',
    slug: p?.slug ?? '',
    enabled: p?.enabled ?? true,
    issuer: c.issuer ?? '',
    clientId: c.client_id ?? '',
    clientSecret: '',
    scopes: (c.scopes ?? []).filter((s) => !['openid', 'email', 'profile'].includes(s)),
    metadataMode: c.idp_metadata_xml ? 'xml' : 'url',
    metadataUrl: c.idp_metadata_url ?? '',
    metadataXml: c.idp_metadata_xml ?? '',
    emailAttribute: c.email_attribute ?? '',
    domains: c.allowed_domains ?? [],
    roleClaim: c.role_claim ?? '',
    mapping: c.role_mapping ?? [],
    defaultRole: c.default_role ?? 'viewer',
    jit: c.jit_provisioning ?? true,
    syncRoles: c.sync_roles ?? false,
  }
}

function configFrom(d: Draft): S['SSOProviderConfig'] {
  const common = {
    allowed_domains: d.domains,
    role_claim: d.roleClaim.trim() || undefined,
    role_mapping: d.mapping.filter((m) => m.value.trim()),
    default_role: d.defaultRole,
    jit_provisioning: d.jit,
    sync_roles: d.syncRoles,
  }
  return d.kind === 'oidc'
    ? { ...common, issuer: d.issuer.trim(), client_id: d.clientId.trim(), scopes: d.scopes }
    : {
        ...common,
        idp_metadata_url: d.metadataMode === 'url' ? d.metadataUrl.trim() : undefined,
        idp_metadata_xml: d.metadataMode === 'xml' ? d.metadataXml.trim() : undefined,
        email_attribute: d.emailAttribute.trim() || undefined,
      }
}

const isURL = (s: string) => /^https?:\/\/[^\s/]+/.test(s.trim())

export function SSOSettings() {
  const { orgId, org } = useProjectCtx()
  const qc = useQueryClient()
  const isOwner = org?.role === 'owner'
  const providers = useQuery({
    queryKey: ['sso-providers', orgId],
    enabled: !!orgId,
    queryFn: async () => (await unwrap(http.GET('/v1/orgs/{org}/sso-providers', { params: { path: { org: orgId! } } }))).items,
  })
  const customRoles = useCustomRoles()
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: ['sso-providers', orgId] })
    void qc.invalidateQueries({ queryKey: ['auth', 'providers'] })
  }
  const [editing, setEditing] = useState<S['SSOProvider'] | 'new' | null>(null)
  const [draft, setDraft] = useState<Draft>(draftFrom(null))
  const [toDelete, setToDelete] = useState<S['SSOProvider'] | null>(null)
  useEffect(() => {
    if (editing) setDraft(draftFrom(editing === 'new' ? null : editing))
  }, [editing])
  const set = (patch: Partial<Draft>) => setDraft((d) => ({ ...d, ...patch }))

  const save = useMutation({
    mutationFn: () =>
      editing === 'new' || !editing
        ? unwrap(http.POST('/v1/orgs/{org}/sso-providers', { params: { path: { org: orgId! } }, body: { kind: draft.kind, name: draft.name.trim(), slug: draft.slug.trim() || undefined, enabled: draft.enabled, config: configFrom(draft), client_secret: draft.clientSecret || undefined } }))
        : unwrap(http.PATCH('/v1/orgs/{org}/sso-providers/{id}', { params: { path: { org: orgId!, id: editing.id } }, body: { name: draft.name.trim(), enabled: draft.enabled, config: configFrom(draft), client_secret: draft.clientSecret || undefined } })),
    onSuccess: (p) => {
      toast.success(editing === 'new' ? `${p.name} added` : 'Provider saved')
      refresh()
      setEditing(editing === 'new' ? p : null)
    },
  })
  const toggle = useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) => unwrap(http.PATCH('/v1/orgs/{org}/sso-providers/{id}', { params: { path: { org: orgId!, id } }, body: { enabled } })),
    onSuccess: refresh,
  })
  const remove = useMutation({
    mutationFn: (id: string) => unwrap(http.DELETE('/v1/orgs/{org}/sso-providers/{id}', { params: { path: { org: orgId!, id } } })),
    onSuccess: () => {
      setToDelete(null)
      toast.success('Provider removed')
      refresh()
    },
  })

  const existing = editing && editing !== 'new' ? editing : null
  const valid =
    draft.name.trim() &&
    (draft.kind === 'oidc' ? isURL(draft.issuer) && draft.clientId.trim() : draft.metadataMode === 'url' ? isURL(draft.metadataUrl) : draft.metadataXml.includes('EntityDescriptor'))

  return (
    <SectionCard
      title="Single sign-on"
      description="Sign in with your identity provider over OIDC or SAML. Members can still use passwords unless you remove them."
      actions={isOwner && <Button size="sm" icon={<Plus />} onClick={() => setEditing('new')}>Add provider</Button>}
    >
      {providers.isPending ? <Loading rows={2} /> : providers.error ? <ErrorPanel error={providers.error} onRetry={() => void providers.refetch()} /> : providers.data!.length === 0 ? (
        <EmptyState compact illustration="generic" title="No identity providers" description="Connect Okta, Entra ID, Google Workspace or any OIDC / SAML provider." action={isOwner ? <Button size="sm" variant="primary" onClick={() => setEditing('new')}>Add provider</Button> : undefined} />
      ) : (
        <ul className="divide-y divide-border">
          {providers.data!.map((p) => (
            <li key={p.id} className="flex items-center gap-3 px-4 py-2.5">
              <ShieldCheck className="size-4 text-text-muted" aria-hidden />
              <button type="button" disabled={!isOwner} className="min-w-0 flex-1 rounded-sm text-left focus-ring" onClick={() => setEditing(p)}>
                <div className="flex items-center gap-2">
                  <span className="truncate text-sm font-medium text-text">{p.name}</span>
                  <Badge>{p.kind.toUpperCase()}</Badge>
                  {p.config.jit_provisioning && <Badge variant="outline">JIT</Badge>}
                </div>
                <div className="truncate text-xs text-text-muted">
                  <code className="font-mono">{p.slug}</code>
                  {p.config.allowed_domains?.length ? ` · ${p.config.allowed_domains.join(', ')}` : ' · any email domain'}
                </div>
              </button>
              <Switch size="sm" aria-label={`${p.name} enabled`} checked={p.enabled} disabled={!isOwner} onCheckedChange={(enabled) => toggle.mutate({ id: p.id, enabled }, { onError: (e) => toast.error(errorMessage(e)) })} />
              {isOwner && <IconButton label={`Remove ${p.name}`} size="sm" onClick={() => setToDelete(p)}><Trash2 /></IconButton>}
            </li>
          ))}
        </ul>
      )}
      {!isOwner && providers.data && <CardBody className="border-t border-border text-xs text-text-muted">Only owners can change single sign-on.</CardBody>}

      <Dialog open={!!editing} onOpenChange={(o) => !o && setEditing(null)} size="lg" title={existing ? `Edit ${existing.name}` : 'Add identity provider'} description={existing ? undefined : 'You can change everything except the type and URL slug later.'}
        footer={<><Button variant="ghost" onClick={() => setEditing(null)}>Cancel</Button><Button variant="primary" disabled={!valid} loading={save.isPending} onClick={() => save.mutate()}>{existing ? 'Save provider' : 'Add provider'}</Button></>}>
        <div className="flex flex-col gap-6">
          {!existing && <SegmentedControl aria-label="Protocol" value={draft.kind} onValueChange={(kind) => set({ kind })} options={[{ value: 'oidc', label: 'OpenID Connect' }, { value: 'saml', label: 'SAML 2.0' }]} />}
          {existing && (
            <FormSection title="Configure your identity provider" description="Register these values in your IdP.">
              <dl className="divide-y divide-border rounded-md border border-border px-3">
                <DetailRow label={existing.kind === 'oidc' ? 'Redirect URI' : 'ACS URL'}><UrlValue value={existing.redirect_uri} /></DetailRow>
                {existing.kind === 'saml' && existing.entity_id && <DetailRow label="Entity ID / metadata"><UrlValue value={existing.entity_id} /></DetailRow>}
                <DetailRow label="Sign-in URL"><UrlValue value={`${location.origin}/v1/auth/${existing.kind}/${existing.slug}/start`} /></DetailRow>
              </dl>
            </FormSection>
          )}
          <FormSection title="Provider">
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <Field label="Name" hint="Shown on the sign-in button."><Input value={draft.name} onChange={(e) => set({ name: e.target.value })} placeholder="Okta" autoFocus /></Field>
              <Field label="URL slug" optional={!existing} hint={existing ? 'Can’t be changed.' : 'Derived from the name when empty.'}><Input className="font-mono text-xs" disabled={!!existing} value={draft.slug} onChange={(e) => set({ slug: e.target.value.toLowerCase() })} placeholder="okta" /></Field>
            </div>
            {draft.kind === 'oidc' ? (
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <Field label="Issuer URL" error={draft.issuer && !isURL(draft.issuer) ? 'Must be an http(s) URL' : undefined}><Input className="font-mono text-xs" value={draft.issuer} onChange={(e) => set({ issuer: e.target.value })} placeholder="https://acme.okta.com" /></Field>
                <Field label="Client ID"><Input className="font-mono text-xs" value={draft.clientId} onChange={(e) => set({ clientId: e.target.value })} /></Field>
                <Field label="Client secret" optional hint={existing?.has_client_secret ? 'Stored encrypted. Leave empty to keep it.' : 'Encrypted at rest and never shown again.'}>
                  <Input type="password" autoComplete="off" leading={<KeyRound />} value={draft.clientSecret} onChange={(e) => set({ clientSecret: e.target.value })} placeholder={existing?.has_client_secret ? '••••••••' : ''} />
                </Field>
                <Field label="Extra scopes" optional hint="openid, email and profile are always requested."><TagInput value={draft.scopes} onChange={(scopes) => set({ scopes })} placeholder="groups" /></Field>
              </div>
            ) : (
              <div className="flex flex-col gap-4">
                <SegmentedControl size="sm" aria-label="IdP metadata source" value={draft.metadataMode} onValueChange={(metadataMode) => set({ metadataMode })} options={[{ value: 'url', label: 'Metadata URL' }, { value: 'xml', label: 'Paste XML' }]} />
                {draft.metadataMode === 'url' ? (
                  <Field label="IdP metadata URL"><Input className="font-mono text-xs" value={draft.metadataUrl} onChange={(e) => set({ metadataUrl: e.target.value })} placeholder="https://login.microsoftonline.com/…/federationmetadata.xml" /></Field>
                ) : (
                  <Field label="IdP metadata XML"><Textarea mono rows={5} value={draft.metadataXml} onChange={(e) => set({ metadataXml: e.target.value })} placeholder="<EntityDescriptor …>" /></Field>
                )}
                <Field label="Email attribute" optional hint="Defaults to the email / mail attribute, then NameID."><Input className="font-mono text-xs" value={draft.emailAttribute} onChange={(e) => set({ emailAttribute: e.target.value })} /></Field>
              </div>
            )}
          </FormSection>
          <FormSection title="Access" description="Who may sign in, and with which role.">
            <Field label="Allowed email domains" optional hint="Leave empty to accept any domain your IdP vouches for."><TagInput value={draft.domains} onChange={(domains) => set({ domains })} placeholder="acme.com" /></Field>
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <Field label={draft.kind === 'oidc' ? 'Groups claim' : 'Groups attribute'} optional hint="Values are matched against the rules below."><Input className="font-mono text-xs" value={draft.roleClaim} onChange={(e) => set({ roleClaim: e.target.value })} placeholder="groups" /></Field>
              <Field label="Default role" hint="For new members no rule matches."><Select value={draft.defaultRole} onValueChange={(r) => set({ defaultRole: r as S['Role'] })} options={builtinRoles.filter((r) => r.value !== 'owner').map((r) => ({ value: r.value, label: r.label }))} /></Field>
            </div>
            <MappingEditor value={draft.mapping} onChange={(mapping) => set({ mapping })} custom={customRoles.data?.custom} valueLabel="Group" valuePlaceholder="eval-admins" />
            <div className="flex flex-col gap-3 rounded-md border border-border p-3">
              <ToggleRow label="Just-in-time provisioning" hint="Create accounts and memberships on first sign-in." checked={draft.jit} onChange={(jit) => set({ jit })} />
              <ToggleRow label="Sync roles on every sign-in" hint="Re-apply the mapping each time. Owners are never changed." checked={draft.syncRoles} onChange={(syncRoles) => set({ syncRoles })} />
              <ToggleRow label="Enabled" hint="Show this provider on the sign-in page." checked={draft.enabled} onChange={(enabled) => set({ enabled })} />
            </div>
          </FormSection>
        </div>
      </Dialog>
      <ConfirmDialog open={!!toDelete} onOpenChange={(o) => !o && setToDelete(null)} title={`Remove ${toDelete?.name}?`} description="People who sign in with it will need another way in. Accounts and memberships are kept." confirmLabel="Remove" loading={remove.isPending} onConfirm={() => toDelete && remove.mutate(toDelete.id)} />
    </SectionCard>
  )
}

function UrlValue({ value }: { value: string }) {
  return (
    <span className="flex items-center gap-2">
      <code className="min-w-0 flex-1 break-all font-mono text-xs text-text">{value}</code>
      <CopyButton value={value} />
    </span>
  )
}

export function ToggleRow({ label, hint, checked, onChange }: { label: string; hint: string; checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <label className="flex items-center justify-between gap-4">
      <span>
        <span className="block text-sm font-medium text-text">{label}</span>
        <span className="block text-xs text-text-muted">{hint}</span>
      </span>
      <Switch size="sm" checked={checked} onCheckedChange={onChange} aria-label={label} />
    </label>
  )
}
