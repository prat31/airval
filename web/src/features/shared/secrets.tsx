import { useQuery } from '@tanstack/react-query'
import { KeyRound } from 'lucide-react'
import { Link } from '@tanstack/react-router'
import { Combobox } from '@/components/ui/combobox'
import { fetchAll, http, type S } from './api'
import { useProjectCtx } from './context'

/** Secret metadata for the current org (names only; values are write-only). */
export function useSecrets() {
  const { orgId } = useProjectCtx()
  return useQuery({
    queryKey: ['secrets', orgId],
    enabled: !!orgId,
    queryFn: async () =>
      fetchAll<S['Secret']>(async (cursor) => {
        const { data } = await http.GET('/v1/orgs/{org}/secrets', { params: { path: { org: orgId! }, query: { all: true, limit: 1000, cursor } } })
        return data ?? { items: [] }
      }),
  })
}

/** Picks a secret by name; the reference is written into config as the bare name. */
export function SecretPicker({
  value,
  onChange,
  id,
  placeholder = 'Choose a secret',
  size,
}: {
  value: string
  onChange: (name: string) => void
  id?: string
  placeholder?: string
  size?: 'sm' | 'md'
}) {
  const { data = [], isLoading } = useSecrets()
  const { params } = useProjectCtx()
  const options = data.map((s) => ({ value: s.name, label: s.name, description: s.description || undefined, icon: <KeyRound /> }))
  // Keep a reference visible even when it points at a secret that no longer exists.
  if (value && !options.some((o) => o.value === value)) options.unshift({ value, label: value, description: 'Not found', icon: <KeyRound /> })
  return (
    <div className="flex flex-col gap-1">
      <Combobox
        id={id}
        size={size}
        options={options}
        value={value || null}
        onValueChange={(v) => onChange(v ?? '')}
        placeholder={isLoading ? 'Loading…' : placeholder}
        searchPlaceholder="Search secrets…"
        emptyText="No secrets yet"
      />
      {!isLoading && data.length === 0 && (
        <p className="text-xs text-text-muted">
          <Link to="/o/$org/p/$project/settings/$section" params={{ ...params, section: 'secrets' }} className="text-accent hover:underline">
            Add a secret
          </Link>{' '}
          in Settings first.
        </p>
      )}
    </div>
  )
}
