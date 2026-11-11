import { useQuery } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
import { http, unwrap, type S } from './api'

export interface ProjectCtx {
  orgSlug: string
  projectSlug: string
  org?: S['Organization']
  project?: S['Project']
  orgId?: string
  projectId?: string
  /** Route params for links to project-scoped screens. */
  params: { org: string; project: string }
  isLoading: boolean
  error: unknown
}

/**
 * Resolves the route's org/project slugs to API ids. Every feature hook takes `projectId` from here.
 * TODO: switch to the shared workspace provider once `app/workspace.ts` is backed by the API.
 */
export function useProjectCtx(): ProjectCtx {
  const { org: orgSlug = '', project: projectSlug = '' } = useParams({ strict: false }) as { org?: string; project?: string }
  const orgs = useQuery({ queryKey: ['orgs'], queryFn: () => unwrap(http.GET('/v1/orgs', {})), staleTime: 60_000 })
  const org = orgs.data?.items.find((o) => o.slug === orgSlug)
  const projects = useQuery({
    queryKey: ['project-by-slug', org?.id, projectSlug],
    enabled: !!org,
    staleTime: 60_000,
    queryFn: () => unwrap(http.GET('/v1/projects', { params: { query: { org_id: org!.id, slug: projectSlug } } })),
  })
  const project = projects.data?.items.find((p) => p.slug === projectSlug)
  return {
    orgSlug,
    projectSlug,
    org,
    project,
    orgId: org?.id,
    projectId: project?.id,
    params: { org: orgSlug, project: projectSlug },
    isLoading: orgs.isLoading || (!!org && projects.isLoading),
    error: orgs.error ?? projects.error,
  }
}
