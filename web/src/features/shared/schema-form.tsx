/**
 * Renders a form from a JSON Schema (the evaluator catalog's `config_schema`).
 * Honors `x-order` (field order) and `x-ui`: textarea | code | secret | json | list.
 */
import { useMemo } from 'react'
import { Field } from '@/components/ui/field'
import { Input, Textarea } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/cn'
import type { Obj } from './api'
import { SecretPicker } from './secrets'
import { JsonEditor, KvEditor, TagInput } from './ui'

export interface JsonSchema {
  type?: string | string[]
  title?: string
  description?: string
  properties?: Record<string, JsonSchema>
  required?: string[]
  enum?: unknown[]
  default?: unknown
  minimum?: number
  maximum?: number
  items?: JsonSchema
  additionalProperties?: boolean | JsonSchema
  'x-ui'?: string
  'x-order'?: string[]
}

const typeOf = (s: JsonSchema): string => (Array.isArray(s.type) ? (s.type.find((t) => t !== 'null') ?? 'string') : (s.type ?? 'any'))

export const humanize = (key: string) => key.replace(/_/g, ' ').replace(/^\w/, (c) => c.toUpperCase())

export function orderedKeys(schema: JsonSchema): string[] {
  const keys = Object.keys(schema.properties ?? {})
  const order = schema['x-order'] ?? []
  return [...order.filter((k) => keys.includes(k)), ...keys.filter((k) => !order.includes(k))]
}

/** Defaults declared in the schema (recursively), for seeding a new evaluator's config. */
export function schemaDefaults(schema: JsonSchema | undefined): Obj {
  const out: Obj = {}
  for (const [k, p] of Object.entries(schema?.properties ?? {})) {
    if (p.default !== undefined) out[k] = p.default
    else if (typeOf(p) === 'object' && p.properties) {
      const nested = schemaDefaults(p)
      if (Object.keys(nested).length) out[k] = nested
    }
  }
  return out
}

/** Validation errors keyed by dotted path. Mirrors the rendering rules so the form can block saving. */
export function validateSchema(schema: JsonSchema | undefined, value: Obj, prefix = ''): Record<string, string> {
  const errors: Record<string, string> = {}
  if (!schema?.properties) return errors
  for (const [k, p] of Object.entries(schema.properties)) {
    const path = prefix + k
    const v = value?.[k]
    const empty = v === undefined || v === null || v === '' || (Array.isArray(v) && v.length === 0)
    if (schema.required?.includes(k) && empty && p.default === undefined && typeOf(p) !== 'boolean') {
      errors[path] = 'Required'
      continue
    }
    if (typeof v === 'number') {
      if (p.minimum !== undefined && v < p.minimum) errors[path] = `Must be at least ${p.minimum}`
      if (p.maximum !== undefined && v > p.maximum) errors[path] = `Must be at most ${p.maximum}`
    }
    if (typeOf(p) === 'object' && p.properties && v && typeof v === 'object') Object.assign(errors, validateSchema(p, v as Obj, `${path}.`))
  }
  return errors
}

export function SchemaForm({
  schema,
  value,
  onChange,
  errors = {},
  prefix = '',
  className,
}: {
  schema: JsonSchema
  value: Obj
  onChange: (v: Obj) => void
  errors?: Record<string, string>
  prefix?: string
  className?: string
}) {
  const keys = useMemo(() => orderedKeys(schema), [schema])
  const set = (k: string, v: unknown) => {
    const next = { ...value }
    if (v === undefined || v === '' || (Array.isArray(v) && v.length === 0)) delete next[k]
    else next[k] = v
    onChange(next)
  }
  return (
    <div className={cn('flex flex-col gap-4', className)}>
      {keys.map((k) => (
        <SchemaField
          key={k}
          name={k}
          schema={schema.properties![k]!}
          required={!!schema.required?.includes(k)}
          value={value?.[k]}
          onChange={(v) => set(k, v)}
          errors={errors}
          path={prefix + k}
        />
      ))}
    </div>
  )
}

function SchemaField({
  name,
  schema,
  required,
  value,
  onChange,
  errors,
  path,
}: {
  name: string
  schema: JsonSchema
  required: boolean
  value: unknown
  onChange: (v: unknown) => void
  errors: Record<string, string>
  path: string
}) {
  const label = schema.title ?? humanize(name)
  const type = typeOf(schema)
  const ui = schema['x-ui']
  const error = errors[path]
  const hint = schema.description
  const optional = !required

  // Nested object with known properties → grouped fieldset.
  if (type === 'object' && schema.properties) {
    return (
      <fieldset className="flex flex-col gap-3 rounded-md border border-border bg-surface-2/40 p-3">
        <legend className="px-1 text-sm font-medium text-text">{label}</legend>
        {hint && <p className="-mt-1 text-xs text-text-muted">{hint}</p>}
        <SchemaForm schema={schema} value={(value as Obj) ?? {}} onChange={(v) => onChange(Object.keys(v).length ? v : undefined)} errors={errors} prefix={`${path}.`} />
      </fieldset>
    )
  }

  if (type === 'boolean') {
    return (
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="text-sm font-medium text-text">{label}</div>
          {hint && <p className="mt-0.5 text-xs text-text-muted">{hint}</p>}
        </div>
        <Switch aria-label={label} checked={(value as boolean | undefined) ?? (schema.default as boolean | undefined) ?? false} onCheckedChange={(v) => onChange(v)} />
      </div>
    )
  }

  let control: React.ReactElement<Record<string, unknown>>
  if (ui === 'secret') {
    control = <SecretPicker value={(value as string) ?? ''} onChange={(v) => onChange(v || undefined)} />
  } else if (schema.enum && type !== 'array') {
    control = (
      <Select
        value={value === undefined ? '' : String(value)}
        onValueChange={(v) => onChange(type === 'integer' || type === 'number' ? Number(v) : v)}
        placeholder={schema.default !== undefined ? `${String(schema.default)} (default)` : 'Select…'}
        options={schema.enum.map((e) => ({ value: String(e), label: String(e).replace(/_/g, ' ') }))}
      />
    )
  } else if (type === 'array' && (typeOf(schema.items ?? {}) === 'string' || ui === 'list')) {
    control = <TagInput value={(value as string[]) ?? []} onChange={(v) => onChange(v)} placeholder="Type and press Enter" suggestions={schema.items?.enum as string[] | undefined} />
  } else if (type === 'object' && typeOf((schema.additionalProperties as JsonSchema) ?? {}) === 'string' && ui !== 'json') {
    control = <KvEditor value={value as Record<string, string>} onChange={(v) => onChange(Object.keys(v).length ? v : undefined)} />
  } else if (ui === 'json' || type === 'object' || type === 'array' || type === 'any') {
    control = <JsonEditor value={value} onChange={onChange} rows={5} placeholder="{ }" />
  } else if (type === 'integer' || type === 'number') {
    control = (
      <Input
        type="number"
        inputMode="decimal"
        min={schema.minimum}
        max={schema.maximum}
        step={type === 'integer' ? 1 : 'any'}
        value={value === undefined ? '' : String(value)}
        placeholder={schema.default !== undefined ? String(schema.default) : undefined}
        onChange={(e) => onChange(e.target.value === '' ? undefined : Number(e.target.value))}
      />
    )
  } else if (ui === 'textarea') {
    control = <Textarea rows={5} value={(value as string) ?? ''} onChange={(e) => onChange(e.target.value)} />
  } else if (ui === 'code') {
    control = <Textarea mono rows={2} spellCheck={false} value={(value as string) ?? ''} onChange={(e) => onChange(e.target.value)} />
  } else {
    control = <Input value={(value as string) ?? ''} placeholder={schema.default !== undefined ? String(schema.default) : undefined} onChange={(e) => onChange(e.target.value)} />
  }
  return (
    <Field label={label} hint={hint} error={error} optional={optional}>
      {control}
    </Field>
  )
}
