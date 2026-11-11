/**
 * Session, organizations and projects: the queries every authenticated screen depends on.
 */
import { queryOptions, useMutation, useQueryClient } from '@tanstack/react-query'
import { api, ApiError, unwrap } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

export type Me = components['schemas']['Me']
export type ApiOrganization = components['schemas']['Organization']
export type ApiProject = components['schemas']['Project']
export type ProjectCreate = components['schemas']['ProjectCreate']
export type Role = components['schemas']['Role']

export const authKeys = {
  setup: ['auth', 'setup'] as const,
  me: ['auth', 'me'] as const,
  projects: (orgId: string) => ['projects', 'by-org', orgId] as const,
}

export const setupStatusQuery = queryOptions({
  queryKey: authKeys.setup,
  queryFn: () => unwrap(api.GET('/v1/setup')),
  staleTime: 60_000,
})

export const meQuery = queryOptions({
  queryKey: authKeys.me,
  queryFn: () => unwrap(api.GET('/v1/auth/me')),
  staleTime: 5 * 60_000,
  retry: false,
})

export const projectsQuery = (orgId: string) =>
  queryOptions({
    queryKey: authKeys.projects(orgId),
    queryFn: async () => (await unwrap(api.GET('/v1/projects', { params: { query: { org_id: orgId, limit: 1000 } } }))).items,
    enabled: !!orgId,
    staleTime: 60_000,
  })

export const isUnauthorized = (err: unknown) => err instanceof ApiError && err.status === 401

export function useLogin() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: { email: string; password: string }) => unwrap(api.POST('/v1/auth/login', { body })),
    onSuccess: (me) => qc.setQueryData(authKeys.me, me),
    onError: () => {},
  })
}

export function useSetup() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: components['schemas']['SetupRequest']) => unwrap(api.POST('/v1/setup', { body })),
    onSuccess: (me) => {
      qc.setQueryData(authKeys.me, me)
      qc.setQueryData(authKeys.setup, { needs_setup: false })
    },
    onError: () => {},
  })
}

export function useLogout() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => unwrap(api.POST('/v1/auth/logout')).catch(() => undefined),
    onSettled: () => qc.clear(),
  })
}

export function useCreateProject() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: ProjectCreate) => unwrap(api.POST('/v1/projects', { body })),
    onSuccess: (p) => qc.invalidateQueries({ queryKey: authKeys.projects(p.org_id) }),
    onError: () => {},
  })
}

export function useCreateOrg() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: { name: string; slug?: string }) => unwrap(api.POST('/v1/orgs', { body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: authKeys.me }),
    onError: () => {},
  })
}

/** "Support Bot" → "support-bot" (matches the server's slug pattern). */
export function slugify(name: string) {
  return name
    .toLowerCase()
    .normalize('NFKD')
    .replace(/[^\w\s-]/g, '')
    .trim()
    .replace(/[\s_]+/g, '-')
    .replace(/-+/g, '-')
    .slice(0, 63)
}

/** Categorical chart slot names, in token order (--chart-1 … --chart-6). */
export const PROJECT_COLORS = ['iris', 'aqua', 'orange', 'blue', 'magenta', 'yellow'] as const

export function colorSlot(color: string, fallbackSeed: string): 1 | 2 | 3 | 4 | 5 | 6 {
  const i = PROJECT_COLORS.indexOf(color as (typeof PROJECT_COLORS)[number])
  if (i >= 0) return (i + 1) as 1 | 2 | 3 | 4 | 5 | 6
  const n = /^[1-6]$/.test(color) ? Number(color) : null
  if (n) return n as 1 | 2 | 3 | 4 | 5 | 6
  let h = 0
  for (const c of fallbackSeed) h = (h * 31 + c.charCodeAt(0)) | 0
  return ((Math.abs(h) % 6) + 1) as 1 | 2 | 3 | 4 | 5 | 6
}
