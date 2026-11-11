import { Plus, Trash2 } from 'lucide-react'
import type { ReactNode } from 'react'
import { Button, IconButton } from '@/components/ui/button'
import { Card, CardBody, CardHeader } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Select, type SelectOption } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import type { S } from '../../shared/api'

export function SectionCard({ title, description, actions, children }: { title: string; description?: ReactNode; actions?: ReactNode; children: ReactNode }) {
  return (
    <Card>
      <CardHeader title={title} description={description} actions={actions} />
      {children}
    </Card>
  )
}

export function Loading({ rows = 1 }: { rows?: number }) {
  return (
    <CardBody className="flex flex-col gap-2">
      {Array.from({ length: rows }, (_, i) => (
        <Skeleton key={i} className="h-10 w-full" />
      ))}
    </CardBody>
  )
}

export const builtinRoles: { value: S['Role']; label: string; description: string }[] = [
  { value: 'owner', label: 'Owner', description: 'Everything, including deleting the organization' },
  { value: 'admin', label: 'Admin', description: 'Members, API keys, secrets and settings' },
  { value: 'editor', label: 'Editor', description: 'Create, edit and run' },
  { value: 'viewer', label: 'Viewer', description: 'Read-only' },
]

/** Options for a RoleRef select: built-in roles (never owner) and the org's custom roles. */
export function roleRefOptions(custom: S['CustomRole'][] | undefined, { builtins = true }: { builtins?: boolean } = {}): SelectOption[] {
  const out: SelectOption[] = builtins ? builtinRoles.filter((r) => r.value !== 'owner').map((r) => ({ value: r.value, label: r.label })) : []
  for (const c of custom ?? []) out.push({ value: `custom:${c.id}`, label: c.name })
  return out
}

export function roleRefLabel(ref: string, custom: S['CustomRole'][] | undefined): string {
  if (ref.startsWith('custom:')) return custom?.find((c) => `custom:${c.id}` === ref)?.name ?? 'Deleted role'
  return builtinRoles.find((r) => r.value === ref)?.label ?? ref
}

/** Editable list of "value → role" rules (IdP groups / SCIM groups). */
export function MappingEditor({
  value,
  onChange,
  custom,
  valueLabel,
  valuePlaceholder,
}: {
  value: S['RoleMappingRule'][]
  onChange: (v: S['RoleMappingRule'][]) => void
  custom?: S['CustomRole'][]
  valueLabel: string
  valuePlaceholder: string
}) {
  const options = roleRefOptions(custom)
  const set = (i: number, patch: Partial<S['RoleMappingRule']>) => onChange(value.map((r, j) => (j === i ? { ...r, ...patch } : r)))
  return (
    <div className="flex flex-col gap-2">
      {value.length > 0 && (
        <div className="grid grid-cols-[1fr_160px_28px] gap-2 text-xs font-medium text-text-muted">
          <span>{valueLabel}</span>
          <span>Role</span>
          <span />
        </div>
      )}
      {value.map((r, i) => (
        <div key={i} className="grid grid-cols-[1fr_160px_28px] items-center gap-2">
          <Input size="sm" aria-label={`${valueLabel} ${i + 1}`} value={r.value} placeholder={valuePlaceholder} onChange={(e) => set(i, { value: e.target.value })} />
          <Select size="sm" aria-label={`Role for rule ${i + 1}`} value={r.role} onValueChange={(role) => set(i, { role })} options={options} />
          <IconButton label="Remove rule" size="sm" onClick={() => onChange(value.filter((_, j) => j !== i))}>
            <Trash2 />
          </IconButton>
        </div>
      ))}
      <div>
        <Button size="sm" variant="ghost" icon={<Plus />} onClick={() => onChange([...value, { value: '', role: 'viewer' }])}>
          Add rule
        </Button>
      </div>
    </div>
  )
}

/** A label/value row for read-only details (redirect URIs, metadata URLs). */
export function DetailRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid grid-cols-1 gap-1 py-2 sm:grid-cols-[180px_1fr] sm:gap-4">
      <dt className="text-sm text-text-muted">{label}</dt>
      <dd className="min-w-0 text-sm text-text">{children}</dd>
    </div>
  )
}
