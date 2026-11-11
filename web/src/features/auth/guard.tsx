/**
 * Route guards: resolve session → organization → project before rendering the app shell.
 */
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Link, Navigate, useParams, useRouterState } from '@tanstack/react-router'
import { Plus, RotateCw } from 'lucide-react'
import { AppShell } from '@/app/shell'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/ui/empty-state'
import { Skeleton } from '@/components/ui/skeleton'
import { isUnauthorized, meQuery, projectsQuery, setupStatusQuery } from './api'
import { NewOrgDialog } from './workspace-dialogs'

/** Calm full-frame placeholder that matches the shell layout while the workspace loads. */
function ShellSkeleton() {
  return (
    <div className="flex h-dvh overflow-hidden bg-bg" aria-busy="true" aria-label="Loading workspace">
      <div className="hidden w-(--sidebar-width) shrink-0 flex-col gap-2 border-r border-border p-2 md:flex">
        <Skeleton className="h-9 w-full" />
        <Skeleton className="h-8 w-full" />
        <div className="mt-4 flex flex-col gap-1.5">
          {Array.from({ length: 7 }, (_, i) => (
            <Skeleton key={i} className="h-7 w-full" />
          ))}
        </div>
      </div>
      <div className="flex flex-1 flex-col">
        <div className="flex h-(--topbar-height) items-center gap-3 border-b border-border px-3">
          <Skeleton className="h-4 w-48" />
        </div>
        <div className="mx-auto flex w-full max-w-[1280px] flex-col gap-6 px-4 py-6 sm:px-6 lg:px-8">
          <Skeleton className="h-9 w-64" />
          <div className="grid grid-cols-2 gap-3 xl:grid-cols-4">
            {Array.from({ length: 4 }, (_, i) => (
              <Skeleton key={i} className="h-24" />
            ))}
          </div>
          <Skeleton className="h-72" />
        </div>
      </div>
    </div>
  )
}

function GateError({ error, retry }: { error: unknown; retry: () => void }) {
  return (
    <div className="flex min-h-dvh items-center justify-center bg-bg px-4">
      <EmptyState
        illustration="error"
        title="Can’t reach the airval server"
        description={error instanceof Error ? error.message : 'The API did not respond.'}
        action={
          <Button variant="primary" icon={<RotateCw />} onClick={retry}>
            Try again
          </Button>
        }
      />
    </div>
  )
}

/**
 * Redirect to /login, remembering where the user was. The `next` target is captured once, on first render:
 * reading it live made the guard (still mounted while the router commits the redirect) see `/login?next=…`
 * as the new location and navigate again with that nested inside `next`, growing the URL exponentially
 * until the tab crashed.
 */
function useLoginRedirect() {
  const href = useRouterState({ select: (s) => s.location.href })
  const [next] = useState(() => (href.startsWith('/login') || href.startsWith('/setup') ? undefined : href))
  return <Navigate to="/login" search={next ? { next } : {}} replace />
}

/** `/` → first org → first project (or setup / login / onboarding). */
export function EntryRedirect() {
  const setup = useQuery(setupStatusQuery)
  const me = useQuery({ ...meQuery, enabled: setup.data?.needs_setup === false })
  const org = me.data?.orgs[0]
  const projects = useQuery({ ...projectsQuery(org?.id ?? ''), enabled: !!org })
  const login = useLoginRedirect()

  if (setup.data?.needs_setup) return <Navigate to="/setup" replace />
  if (setup.error) return <GateError error={setup.error} retry={() => setup.refetch()} />
  if (isUnauthorized(me.error)) return login
  if (me.error) return <GateError error={me.error} retry={() => me.refetch()} />
  if (me.data && !org) return <NoOrganization />
  if (projects.data) {
    const first = projects.data[0]
    if (!first) return <Navigate to="/o/$org" params={{ org: org!.slug }} replace />
    return <Navigate to="/o/$org/p/$project/overview" params={{ org: org!.slug, project: first.slug }} replace />
  }
  return <ShellSkeleton />
}

/** A signed-in user who belongs to no organization: say so and let them create one. */
function NoOrganization() {
  const [open, setOpen] = useState(false)
  return (
    <div className="flex min-h-dvh items-center justify-center bg-bg px-4">
      <NewOrgDialog open={open} onOpenChange={setOpen} />
      <EmptyState
        illustration="generic"
        title="You’re not in an organization yet"
        description="Create one to start adding projects, or ask an admin to invite you to theirs."
        action={
          <Button variant="primary" icon={<Plus />} onClick={() => setOpen(true)}>
            Create organization
          </Button>
        }
      />
    </div>
  )
}

/**
 * Org-level pages (`/o/$org`, `/o/$org/settings/…`): the app shell with no project selected, so an
 * organization with zero projects is a normal, navigable state.
 */
export function OrgGate() {
  const { org: orgSlug } = useParams({ strict: false }) as { org: string }
  const me = useQuery(meQuery)
  const org = me.data?.orgs.find((o) => o.slug === orgSlug)
  const projects = useQuery({ ...projectsQuery(org?.id ?? ''), enabled: !!org })
  const login = useLoginRedirect()
  if (isUnauthorized(me.error)) return login
  if (me.error) return <GateError error={me.error} retry={() => me.refetch()} />
  if (me.data && !org) return <Navigate to="/" replace />
  if (projects.error) return <GateError error={projects.error} retry={() => projects.refetch()} />
  if (!projects.data) return <ShellSkeleton />
  return <AppShell />
}

/** Renders the app shell only once the session, org and project are resolved. */
export function WorkspaceGate() {
  const params = useParams({ strict: false }) as { org: string; project: string }
  const me = useQuery(meQuery)
  const org = me.data?.orgs.find((o) => o.slug === params.org)
  const projects = useQuery({ ...projectsQuery(org?.id ?? ''), enabled: !!org })
  const login = useLoginRedirect()

  if (isUnauthorized(me.error)) return login
  if (me.error) return <GateError error={me.error} retry={() => me.refetch()} />
  if (me.data && !org) return <Navigate to="/" replace />
  if (projects.error) return <GateError error={projects.error} retry={() => projects.refetch()} />
  if (!projects.data) return <ShellSkeleton />
  if (!projects.data.some((p) => p.slug === params.project)) {
    const first = projects.data[0]
    if (!first) return <Navigate to="/o/$org" params={{ org: params.org }} replace />
    return (
      <div className="flex min-h-dvh items-center justify-center bg-bg px-4">
        <EmptyState
          illustration="search"
          title="Project not found"
          description={`There’s no project “${params.project}” in this organization, or you don’t have access to it.`}
          action={
            <Button variant="primary" asChild>
              <Link to="/o/$org/p/$project/overview" params={{ org: params.org, project: first.slug }}>
                Open {first.name}
              </Link>
            </Button>
          }
        />
      </div>
    )
  }
  return <AppShell />
}
