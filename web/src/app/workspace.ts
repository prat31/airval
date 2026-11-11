/**
 * Workspace context (current user, organizations, projects), backed by `/auth/me` and `/projects`.
 *
 * Routes under /o/$org/p/$project render inside <WorkspaceGate>, which waits for both queries,
 * so inside the shell `org` and `project` are always resolved. Slugs are used in URLs; ids for API calls.
 */
import { useQuery } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
import { colorSlot, meQuery, projectsQuery, type ApiProject, type Role } from '@/features/auth/api'

export interface Org {
  id: string
  slug: string
  name: string
  role?: Role
}

export interface Project {
  id: string
  slug: string
  name: string
  /** Organization slug. */
  org: string
  orgId: string
  /** Categorical chart slot (1–6) used as the project's identity color. */
  color: 1 | 2 | 3 | 4 | 5 | 6
  description?: string
}

export interface User {
  id: string
  name: string
  email: string
}

const emptyOrg: Org = { id: '', slug: '', name: '' }
const emptyUser: User = { id: '', name: '', email: '' }

export function toProject(p: ApiProject, orgSlug: string): Project {
  return {
    id: p.id,
    slug: p.slug,
    name: p.name,
    org: orgSlug,
    orgId: p.org_id,
    color: colorSlot(p.color, p.id),
    description: p.description || undefined,
  }
}

export function useWorkspace() {
  const params = useParams({ strict: false }) as { org?: string; project?: string }
  const me = useQuery(meQuery).data
  const orgs: Org[] = (me?.orgs ?? []).map((o) => ({ id: o.id, slug: o.slug, name: o.name, role: o.role }))
  const org = orgs.find((o) => o.slug === params.org) ?? orgs[0] ?? emptyOrg
  const projectList = useQuery(projectsQuery(org.id)).data
  const projects = (projectList ?? []).map((p) => toProject(p, org.slug))
  const project = projects.find((p) => p.slug === params.project) ?? null
  const u = me?.user
  const user: User = u ? { id: u.id, name: u.name || u.email.split('@')[0]!, email: u.email } : emptyUser
  return { user, orgs, org, projects, project, ready: !!me && !!projectList }
}

export function initials(name: string) {
  return name
    .split(/\s+/)
    .map((p) => p[0])
    .filter(Boolean)
    .slice(0, 2)
    .join('')
    .toUpperCase()
}
