import { useQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { Sparkles } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { toast } from '@/components/ui/toast'
import { cn } from '@/lib/cn'
import { errorMessage } from '../shared/api'
import { metaQuery, useLoadSample } from './api'

/** Whether this server can load the sample project (the built-in mock service is on). */
export function useSampleAvailable(): boolean {
  const meta = useQuery(metaQuery)
  return meta.data?.features.sample_project === true
}

/** Loads the sample project and opens its overview. */
function useLoadAndOpen(orgId: string, orgSlug: string) {
  const load = useLoadSample()
  const navigate = useNavigate()
  const start = () =>
    load.mutate(orgId, {
      onSuccess: (s) => {
        toast.success('Sample project ready', { description: '2 runs of 1,000 examples each, with gates and a baseline to compare against.' })
        void navigate({ to: '/o/$org/p/$project/overview', params: { org: orgSlug, project: s.project.slug } })
      },
      onError: (err) => toast.error('Couldn’t load the sample project', { description: errorMessage(err) }),
    })
  return { start, pending: load.isPending }
}

/** Card offering the one-click sample project (onboarding). Renders nothing when unavailable. */
export function SampleProjectCard({ orgId, orgSlug, className }: { orgId: string; orgSlug: string; className?: string }) {
  const available = useSampleAvailable()
  const { start, pending } = useLoadAndOpen(orgId, orgSlug)
  if (!available) return null
  return (
    <div className={cn('flex flex-col gap-3 rounded-lg border border-accent/40 bg-accent-subtle p-4 sm:flex-row sm:items-center', className)}>
      <span className="flex size-9 shrink-0 items-center justify-center rounded-md bg-accent text-on-accent [&_svg]:size-4" aria-hidden>
        <Sparkles />
      </span>
      <div className="min-w-0 flex-1">
        <div className="text-sm font-semibold text-text">Explore a sample project</div>
        <p className="mt-0.5 text-xs text-text-2">
          A support copilot on airval’s built-in mock service: 1,000 examples, four evaluators, gates and two finished runs to compare. No API keys needed.
        </p>
      </div>
      <Button variant="primary" onClick={start} loading={pending} className="shrink-0" data-testid="load-sample">
        {pending ? 'Running 2,000 examples…' : 'Load sample project'}
      </Button>
    </div>
  )
}

/** Compact button form (empty project overview). Renders nothing when unavailable. */
export function LoadSampleButton({ orgId, orgSlug, variant = 'ghost' }: { orgId: string; orgSlug: string; variant?: 'ghost' | 'secondary' }) {
  const available = useSampleAvailable()
  const { start, pending } = useLoadAndOpen(orgId, orgSlug)
  if (!available) return null
  return (
    <Button variant={variant} icon={<Sparkles />} onClick={start} loading={pending} data-testid="load-sample">
      {pending ? 'Loading sample…' : 'Load sample project'}
    </Button>
  )
}
