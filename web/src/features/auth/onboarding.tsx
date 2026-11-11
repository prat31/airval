import { useQuery } from '@tanstack/react-query'
import { Link, Navigate, useSearch } from '@tanstack/react-router'
import { ArrowRight, Crosshair, Database, Gauge, Layers, type LucideIcon } from 'lucide-react'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Stepper } from '@/components/ui/stepper'
import { isUnauthorized, meQuery, type ApiProject } from './api'
import { ProjectForm } from './workspace-dialogs'

const steps = [
  { id: 'project', label: 'Project' },
  { id: 'target', label: 'Target' },
  { id: 'dataset', label: 'Dataset' },
  { id: 'evaluators', label: 'Evaluators' },
  { id: 'run', label: 'First run' },
]

const next: { icon: LucideIcon; title: string; description: string; to: string; cta: string }[] = [
  {
    icon: Crosshair,
    title: 'Connect a target',
    description: 'An HTTP endpoint or LLM provider model, with a live connection test.',
    to: '/o/$org/p/$project/targets',
    cta: 'Add target',
  },
  {
    icon: Database,
    title: 'Add a dataset',
    description: 'Import CSV / JSONL, or start from a handful of hand-written examples.',
    to: '/o/$org/p/$project/datasets',
    cta: 'Add dataset',
  },
  {
    icon: Gauge,
    title: 'Pick evaluators',
    description: 'Start from presets such as correctness and faithfulness, then tune thresholds.',
    to: '/o/$org/p/$project/evaluators',
    cta: 'Browse presets',
  },
  {
    icon: Layers,
    title: 'Create a suite and run it',
    description: 'Combine target, data, evaluators and gates — then watch results stream in.',
    to: '/o/$org/p/$project/suites/new',
    cta: 'New suite',
  },
]

/** First-run flow: create the project, then hand off to the setup screens with a clear checklist. */
export function OnboardingScreen() {
  const { org: orgSlug } = useSearch({ strict: false }) as { org?: string }
  const me = useQuery(meQuery)
  const [project, setProject] = useState<ApiProject | null>(null)

  if (isUnauthorized(me.error)) return <Navigate to="/login" search={{ next: '/onboarding' }} replace />
  const org = me.data?.orgs.find((o) => o.slug === orgSlug) ?? me.data?.orgs[0]

  return (
    <div className="mx-auto flex w-full max-w-2xl flex-col gap-6">
      <div className="text-center">
        <h1 className="text-xl font-semibold tracking-tight text-text">Let’s evaluate your AI service</h1>
        <p className="mt-1 text-sm text-text-2">
          {org ? (
            <>
              Start with a project in <span className="font-medium text-text">{org.name}</span>. It takes about three minutes.
            </>
          ) : (
            'Start with a project. It takes about three minutes.'
          )}
        </p>
      </div>
      <div className="rounded-lg border border-border bg-surface shadow-popover">
        <div className="overflow-x-auto border-b border-border px-5 py-4">
          <Stepper steps={steps} current={project ? 1 : 0} />
        </div>
        {!org ? (
          <div className="flex flex-col gap-4 p-5">
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-16 w-full" />
          </div>
        ) : !project ? (
          <div className="p-5">
            <h2 className="text-md font-semibold text-text">Name your project</h2>
            <p className="mb-5 mt-0.5 text-sm text-text-2">A project is one AI service under evaluation. It owns targets, datasets, suites and runs.</p>
            <ProjectForm orgId={org.id} onCreated={setProject} submitLabel="Create project" />
          </div>
        ) : (
          <div className="p-5">
            <h2 className="text-md font-semibold text-text">{project.name} is ready</h2>
            <p className="mb-4 mt-0.5 text-sm text-text-2">Next, wire it up. You can do these in any order.</p>
            <ol className="flex flex-col divide-y divide-border rounded-md border border-border">
              {next.map((s, i) => (
                <li key={s.title} className="flex items-center gap-3 px-4 py-3">
                  <span className="flex size-8 shrink-0 items-center justify-center rounded-md border border-border bg-surface-2 text-text-2 [&_svg]:size-4">
                    <s.icon />
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="text-sm font-medium text-text">
                      <span className="tabular mr-1.5 text-text-muted">{i + 2}.</span>
                      {s.title}
                    </div>
                    <p className="text-xs text-text-muted">{s.description}</p>
                  </div>
                  <Button size="sm" asChild>
                    <Link to={s.to} params={{ org: org.slug, project: project.slug }}>
                      {s.cta}
                    </Link>
                  </Button>
                </li>
              ))}
            </ol>
            <div className="mt-5 flex justify-end">
              <Button variant="primary" asChild>
                <Link to="/o/$org/p/$project/overview" params={{ org: org.slug, project: project.slug }}>
                  Go to project
                  <ArrowRight />
                </Link>
              </Button>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
