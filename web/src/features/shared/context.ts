import { useQuery } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
import { meQuery, projectsQuery } from '@/features/auth/api'
import type { S } from './api'

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
 * Resolves the route's org/project slugs to API ids. Backed by the same `/auth/me` + `/projects`
 * queries as `useWorkspace()` (app/workspace.ts), so there is one source of truth.
 */
export function useProjectCtx(): ProjectCtx {
  const { org: orgSlug = '', project: projectSlug = '' } = useParams({ strict: false }) as { org?: string; project?: string }
  const me = useQuery(meQuery)
  const org = me.data?.orgs.find((o) => o.slug === orgSlug)
  const projects = useQuery(projectsQuery(org?.id ?? ''))
  const project = projects.data?.find((p) => p.slug === projectSlug)
  return {
    orgSlug,
    projectSlug,
    org,
    project,
    orgId: org?.id,
    projectId: project?.id,
    params: { org: orgSlug, project: projectSlug },
    isLoading: me.isLoading || (!!org && projects.isLoading),
    error: me.error ?? projects.error,
  }
}
