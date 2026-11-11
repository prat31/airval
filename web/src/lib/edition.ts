import { queryOptions, useQuery } from '@tanstack/react-query'
import { api, unwrap } from './api/client'
import type { components } from './api/schema'

export type EnterpriseFeature = components['schemas']['EnterpriseFeature']

/** GET /v1/edition: which edition is running and which enterprise features it enables. */
export const editionQuery = queryOptions({
  queryKey: ['edition'] as const,
  queryFn: () => unwrap(api.GET('/v1/edition')),
  staleTime: Infinity,
})

export function useEdition() {
  const q = useQuery(editionQuery)
  const features = q.data?.features ?? []
  return {
    isLoading: q.isLoading,
    enterprise: q.data?.edition === 'enterprise',
    has: (f: EnterpriseFeature) => features.includes(f),
  }
}

/** GET /v1/auth/providers: SSO options for the sign-in page (empty in the community edition). */
export const authProvidersQuery = queryOptions({
  queryKey: ['auth', 'providers'] as const,
  queryFn: async () => (await unwrap(api.GET('/v1/auth/providers'))).items,
  staleTime: 60_000,
  retry: false,
})
