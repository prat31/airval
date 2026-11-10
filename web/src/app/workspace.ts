/**
 * Workspace context (current user, organizations, projects).
 *
 * TEMPORARY: static fixtures until the auth/orgs API lands (milestone 2). Replace the bodies of
 * `useWorkspace` with TanStack Query calls (`/auth/me`, `/orgs`, `/projects`) — the shape stays the same.
 */
import { useParams } from '@tanstack/react-router'

export interface Org {
  slug: string
  name: string
}

export interface Project {
  slug: string
  name: string
  org: string
  /** Categorical chart slot (1–6) used as the project's identity color. */
  color: 1 | 2 | 3 | 4 | 5 | 6
  description?: string
}

export interface User {
  name: string
  email: string
}

const orgs: Org[] = [
  { slug: 'acme', name: 'Acme Inc.' },
  { slug: 'personal', name: 'Personal' },
]

const projects: Project[] = [
  { slug: 'support-bot', name: 'Support Bot', org: 'acme', color: 1, description: 'Customer support assistant (RAG)' },
  { slug: 'search-rerank', name: 'Search Rerank', org: 'acme', color: 2, description: 'Semantic search reranker' },
  { slug: 'sql-agent', name: 'SQL Agent', org: 'acme', color: 3, description: 'Text-to-SQL agent with tools' },
  { slug: 'sandbox', name: 'Sandbox', org: 'personal', color: 4 },
]

const user: User = { name: 'Ada Lovelace', email: 'ada@acme.dev' }

export const defaultOrg = orgs[0]!.slug
export const defaultProject = projects[0]!.slug

export function useWorkspace() {
  const params = useParams({ strict: false }) as { org?: string; project?: string }
  const org = orgs.find((o) => o.slug === params.org) ?? orgs[0]!
  const orgProjects = projects.filter((p) => p.org === org.slug)
  const project = orgProjects.find((p) => p.slug === params.project) ?? null
  return { user, orgs, org, projects: orgProjects, project }
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
