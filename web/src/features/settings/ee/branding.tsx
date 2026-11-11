/* eslint-disable react-hooks/set-state-in-effect -- local drafts are seeded from server data */
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, ImageUp, Trash2 } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { Button } from '@/components/ui/button'
import { CardBody, CardFooter } from '@/components/ui/card'
import { Field } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { toast } from '@/components/ui/toast'
import { BrandMark } from '@/components/brand/brand'
import { cn } from '@/lib/cn'
import { useEdition } from '@/lib/edition'
import { http, unwrap, type S } from '../../shared/api'
import { useProjectCtx } from '../../shared/context'
import { ErrorPanel } from '../../shared/ui'
import { Loading, SectionCard } from './common'

export type Accent = NonNullable<S['Branding']['accent']>

export const accents: { id: Accent; label: string }[] = [
  { id: 'iris', label: 'Iris' },
  { id: 'indigo', label: 'Indigo' },
  { id: 'ocean', label: 'Ocean' },
  { id: 'teal', label: 'Teal' },
  { id: 'plum', label: 'Plum' },
]

const MAX_LOGO = 128 * 1024

export const brandingKey = (orgId?: string) => ['branding', orgId] as const

/** The org's branding (enterprise only; undefined in community builds). */
export function useBranding(orgId?: string) {
  const { has } = useEdition()
  const enabled = !!orgId && has('branding')
  return useQuery({
    queryKey: brandingKey(orgId),
    enabled,
    staleTime: 5 * 60_000,
    queryFn: () => unwrap(http.GET('/v1/orgs/{org}/branding', { params: { path: { org: orgId! } } })),
  })
}

/** Applies an accent ramp to the document (tokens.css `:root[data-accent=…]`). */
export function applyAccent(accent?: string) {
  const el = document.documentElement
  if (!accent || accent === 'iris') delete el.dataset.accent
  else el.dataset.accent = accent
}

/** Swatch: the ramp rendered from tokens (`[data-accent-swatch]` restates the ramp locally). */
function Swatch({ accent, selected, label, onSelect }: { accent: Accent; selected: boolean; label: string; onSelect: () => void }) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      onClick={onSelect}
      data-accent-swatch={accent}
      className={cn('group flex flex-col items-start gap-2 rounded-md border p-2.5 text-left transition-colors focus-ring', selected ? 'border-[var(--iris-600)] bg-surface-2' : 'border-border hover:bg-surface-hover')}
    >
      <span className="flex h-6 w-full overflow-hidden rounded-sm" aria-hidden>
        {[200, 400, 600, 800].map((s) => <span key={s} className="flex-1" style={{ background: `var(--iris-${s})` }} />)}
      </span>
      <span className="flex w-full items-center justify-between text-sm font-medium text-text">
        {label}
        {selected && <Check className="size-3.5 text-text" aria-hidden />}
      </span>
    </button>
  )
}

export function BrandingSettings() {
  const { orgId, org } = useProjectCtx()
  const qc = useQueryClient()
  const canEdit = org?.role === 'owner' || org?.role === 'admin'
  const branding = useBranding(orgId)
  const [name, setName] = useState('')
  const [logo, setLogo] = useState<string | null>(null)
  const [accent, setAccent] = useState<Accent>('iris')
  const [logoError, setLogoError] = useState<string>()
  const file = useRef<HTMLInputElement>(null)
  useEffect(() => {
    if (branding.data) {
      setName(branding.data.product_name ?? '')
      setLogo(branding.data.logo ?? null)
      setAccent(branding.data.accent ?? 'iris')
    }
  }, [branding.data])
  // Live preview: the whole app shows the chosen accent until you leave without saving.
  useEffect(() => {
    applyAccent(accent)
    return () => applyAccent(qc.getQueryData<S['Branding']>(brandingKey(orgId))?.accent)
  }, [accent, orgId, qc])
  const save = useMutation({
    mutationFn: () => unwrap(http.PUT('/v1/orgs/{org}/branding', { params: { path: { org: orgId! } }, body: { product_name: name.trim(), logo, accent } })),
    onSuccess: (b) => {
      qc.setQueryData(brandingKey(orgId), b)
      toast.success('Branding saved')
    },
  })
  const pick = (f: File | undefined) => {
    setLogoError(undefined)
    if (!f) return
    if (!['image/png', 'image/jpeg', 'image/webp', 'image/svg+xml'].includes(f.type)) return setLogoError('Use a PNG, JPEG, WebP or SVG image.')
    if (f.size > MAX_LOGO) return setLogoError('Logos must be 128 KB or smaller.')
    const r = new FileReader()
    r.onload = () => setLogo(String(r.result))
    r.readAsDataURL(f)
  }
  if (branding.isPending) return <SectionCard title="Branding"><Loading rows={3} /></SectionCard>
  if (branding.error) return <SectionCard title="Branding"><ErrorPanel error={branding.error} onRetry={() => void branding.refetch()} /></SectionCard>
  const b = branding.data!
  const dirty = name.trim() !== (b.product_name ?? '') || logo !== (b.logo ?? null) || accent !== (b.accent ?? 'iris')
  const shownName = name.trim() || 'airval'
  return (
    <SectionCard title="Branding" description="Make the app yours. Changes apply to everyone in this organization.">
      <CardBody className="flex flex-col gap-6">
        <div className="flex items-center gap-3 rounded-md border border-border bg-surface-2 px-4 py-3" aria-label="Preview">
          {logo ? <img src={logo} alt="" className="size-8 rounded-md object-contain" /> : <DefaultMark />}
          <span className="text-md font-semibold tracking-tight text-text">{shownName}</span>
          <span className="ml-auto"><Button size="sm" variant="primary" tabIndex={-1}>Primary action</Button></span>
        </div>
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <Field label="Product name" optional hint="Replaces “airval” in the app header (40 characters max)."><Input value={name} maxLength={40} disabled={!canEdit} onChange={(e) => setName(e.target.value)} placeholder="airval" /></Field>
          <Field label="Logo" optional hint="Square PNG, JPEG, WebP or SVG, up to 128 KB." error={logoError}>
            <div className="flex items-center gap-2">
              <input ref={file} type="file" accept="image/png,image/jpeg,image/webp,image/svg+xml" className="sr-only" onChange={(e) => pick(e.target.files?.[0])} />
              <Button size="md" icon={<ImageUp />} disabled={!canEdit} onClick={() => file.current?.click()}>{logo ? 'Replace' : 'Upload'}</Button>
              {logo && <Button size="md" variant="ghost" icon={<Trash2 />} disabled={!canEdit} onClick={() => setLogo(null)}>Remove</Button>}
            </div>
          </Field>
        </div>
        <div>
          <div className="mb-2 text-sm font-medium text-text">Accent</div>
          <div role="radiogroup" aria-label="Accent" className="grid grid-cols-2 gap-2 sm:grid-cols-5">
            {accents.map((a) => <Swatch key={a.id} accent={a.id} label={a.label} selected={accent === a.id} onSelect={() => canEdit && setAccent(a.id)} />)}
          </div>
          <p className="mt-2 text-xs text-text-muted">Accents are curated ramps that keep text and focus states accessible in light and dark.</p>
        </div>
      </CardBody>
      {canEdit && (
        <CardFooter className="flex justify-end gap-2">
          <Button variant="ghost" disabled={!dirty} onClick={() => { setName(b.product_name ?? ''); setLogo(b.logo ?? null); setAccent(b.accent ?? 'iris') }}>Discard</Button>
          <Button variant="primary" disabled={!dirty} loading={save.isPending} onClick={() => save.mutate()}>Save branding</Button>
        </CardFooter>
      )}
    </SectionCard>
  )
}

export function DefaultMark({ className }: { className?: string }) {
  return <BrandMark className={className} />
}
