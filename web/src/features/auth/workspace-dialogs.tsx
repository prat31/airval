import { useNavigate } from '@tanstack/react-router'
import { Check } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Field } from '@/components/ui/field'
import { Input, Textarea } from '@/components/ui/input'
import { toast } from '@/components/ui/toast'
import { ApiError } from '@/lib/api/client'
import { cn } from '@/lib/cn'
import { PROJECT_COLORS, slugify, useCreateOrg, useCreateProject, type ApiProject } from './api'

/** Six categorical swatches; the project's identity color across the app. */
export function ColorPicker({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <div role="radiogroup" aria-label="Project color" className="flex items-center gap-2">
      {PROJECT_COLORS.map((c, i) => {
        const checked = value === c
        return (
          <button
            key={c}
            type="button"
            role="radio"
            aria-checked={checked}
            aria-label={c}
            onClick={() => onChange(c)}
            className={cn(
              'flex size-7 items-center justify-center rounded-full ring-offset-2 ring-offset-surface transition-shadow focus-ring',
              checked ? 'ring-2 ring-[var(--swatch)]' : 'hover:ring-2 hover:ring-border-strong',
            )}
            style={{ ['--swatch' as string]: `var(--chart-${i + 1})` }}
          >
            <span className="flex size-5 items-center justify-center rounded-full" style={{ background: `var(--chart-${i + 1})` }}>
              {checked && <Check className="size-3 text-on-accent" strokeWidth={3} />}
            </span>
          </button>
        )
      })}
    </div>
  )
}

export function ProjectForm({
  orgId,
  onCreated,
  submitLabel = 'Create project',
  autoFocus = true,
}: {
  orgId: string
  onCreated: (p: ApiProject) => void
  submitLabel?: string
  autoFocus?: boolean
}) {
  const create = useCreateProject()
  const [name, setName] = useState('')
  const [slug, setSlug] = useState<string | null>(null)
  const [description, setDescription] = useState('')
  const [color, setColor] = useState<string>('iris')
  const effectiveSlug = slug ?? slugify(name)
  const slugInvalid = effectiveSlug.length > 0 && !/^[a-z0-9][a-z0-9-]{0,62}$/.test(effectiveSlug)

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (!name.trim() || slugInvalid) return
    create.mutate(
      { org_id: orgId, name: name.trim(), slug: effectiveSlug || undefined, description: description.trim() || undefined, color },
      {
        onSuccess: onCreated,
        onError: (err) => toast.error(err instanceof ApiError ? err.message : 'Could not create the project'),
      },
    )
  }

  return (
    <form id="project-form" className="flex flex-col gap-4" onSubmit={submit}>
      <Field label="Name" hint="One AI service under evaluation, e.g. “Support Bot”.">
        <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Support Bot" required autoFocus={autoFocus} />
      </Field>
      <Field label="URL slug" error={slugInvalid ? 'Lowercase letters, numbers and dashes.' : undefined}>
        <Input value={effectiveSlug} onChange={(e) => setSlug(e.target.value)} placeholder="support-bot" className="font-mono" />
      </Field>
      <Field label="Description" optional>
        <Textarea value={description} onChange={(e) => setDescription(e.target.value)} rows={2} placeholder="Customer support assistant (RAG)" />
      </Field>
      <div className="flex flex-col gap-1.5">
        <span className="text-sm font-medium text-text">Color</span>
        <ColorPicker value={color} onChange={setColor} />
      </div>
      <Button type="submit" variant="primary" className="mt-1 self-end" loading={create.isPending} disabled={!name.trim() || slugInvalid}>
        {submitLabel}
      </Button>
    </form>
  )
}

export function NewProjectDialog({ open, onOpenChange, orgId, orgSlug }: { open: boolean; onOpenChange: (o: boolean) => void; orgId: string; orgSlug: string }) {
  const navigate = useNavigate()
  return (
    <Dialog open={open} onOpenChange={onOpenChange} title="New project" description="Projects own their targets, datasets, suites and runs.">
      {open && (
        <div className="pb-2">
          <ProjectForm
            orgId={orgId}
            onCreated={(p) => {
              onOpenChange(false)
              toast.success(`Created ${p.name}`)
              navigate({ to: '/o/$org/p/$project/overview', params: { org: orgSlug, project: p.slug } })
            }}
          />
        </div>
      )}
    </Dialog>
  )
}

export function NewOrgDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const create = useCreateOrg()
  const navigate = useNavigate()
  const [name, setName] = useState('')
  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (!name.trim()) return
    create.mutate(
      { name: name.trim(), slug: slugify(name) || undefined },
      {
        onSuccess: (o) => {
          onOpenChange(false)
          setName('')
          navigate({ to: '/onboarding', search: { org: o.slug } })
        },
        onError: (err) => toast.error(err instanceof ApiError ? err.message : 'Could not create the organization'),
      },
    )
  }
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      size="sm"
      title="New organization"
      description="A separate tenant with its own members, secrets and projects."
      footer={
        <>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button type="submit" form="org-form" variant="primary" loading={create.isPending} disabled={!name.trim()}>
            Create
          </Button>
        </>
      }
    >
      <form id="org-form" onSubmit={submit} className="pb-1">
        <Field label="Name" hint={name ? <span className="font-mono">/o/{slugify(name)}</span> : undefined}>
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Acme Research" autoFocus />
        </Field>
      </form>
    </Dialog>
  )
}
