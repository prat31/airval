import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { fetchAll, http, isUuid, unwrap, type S } from '../shared/api'

export const suiteKeys = {
  all: (project?: string) => ['suites', project] as const,
  one: (key?: string) => ['suite', key] as const,
  gates: (id?: string) => ['gates', id] as const,
  triggers: (id?: string) => ['triggers', id] as const,
}

export function useSuites(projectId?: string) {
  return useQuery({
    queryKey: suiteKeys.all(projectId),
    enabled: !!projectId,
    queryFn: () =>
      fetchAll<S['Suite']>(async (cursor) => {
        const { data } = await http.GET('/v1/projects/{project}/suites', { params: { path: { project: projectId! }, query: { cursor, limit: 200 } } })
        return data ?? { items: [] }
      }),
  })
}

/** Resolve a route param (slug or id) to a suite. */
export function useSuite(projectId: string | undefined, key: string | undefined) {
  return useQuery({
    queryKey: suiteKeys.one(`${projectId}:${key}`),
    enabled: !!projectId && !!key,
    queryFn: () =>
      isUuid(key!)
        ? unwrap(http.GET('/v1/suites/{suite}', { params: { path: { suite: key! } } }))
        : unwrap(http.GET('/v1/projects/{project}/suites/by-slug/{slug}', { params: { path: { project: projectId!, slug: key! } } })),
  })
}

export function useGates(suiteId?: string) {
  return useQuery({ queryKey: suiteKeys.gates(suiteId), enabled: !!suiteId, queryFn: () => unwrap(http.GET('/v1/suites/{suite}/gates', { params: { path: { suite: suiteId! } } })) })
}

export function useTriggers(suiteId?: string) {
  return useQuery({ queryKey: suiteKeys.triggers(suiteId), enabled: !!suiteId, queryFn: () => unwrap(http.GET('/v1/suites/{suite}/triggers', { params: { path: { suite: suiteId! } } })) })
}

export function useDeleteSuite(projectId?: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => unwrap(http.DELETE('/v1/suites/{suite}', { params: { path: { suite: id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: suiteKeys.all(projectId) }),
  })
}

export function useRunSuite() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: S['RunCreate']) => unwrap(http.POST('/v1/runs', { body })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['runs'] })
      void qc.invalidateQueries({ queryKey: ['suites'] })
    },
  })
}

export function useSuiteRuns(projectId?: string, suiteId?: string, limit = 20) {
  return useQuery({
    queryKey: ['suite-runs', projectId, suiteId, limit],
    enabled: !!projectId && !!suiteId,
    queryFn: () => unwrap(http.GET('/v1/runs', { params: { query: { project_id: projectId!, suite_id: suiteId!, limit } } })),
  })
}

export function useSuiteTrend(projectId: string | undefined, suiteId: string | undefined, metric: S['TrendMetric'], evaluatorId?: string) {
  return useQuery({
    queryKey: ['suite-trend', projectId, suiteId, metric, evaluatorId],
    enabled: !!projectId && !!suiteId,
    queryFn: () => unwrap(http.GET('/v1/projects/{project}/trends', { params: { path: { project: projectId! }, query: { suite_id: suiteId!, metric, evaluator_id: evaluatorId, bucket: 'run' } } })),
  })
}

export function useWebhookMutations(suiteId?: string) {
  const qc = useQueryClient()
  const refresh = () => qc.invalidateQueries({ queryKey: suiteKeys.triggers(suiteId) })
  return {
    create: useMutation({
      mutationFn: (body: S['TriggerCreate']) => unwrap(http.POST('/v1/suites/{suite}/triggers', { params: { path: { suite: suiteId! } }, body })),
      onSuccess: refresh,
    }),
    update: useMutation({
      mutationFn: ({ id, body }: { id: string; body: S['TriggerUpdate'] }) => unwrap(http.PATCH('/v1/suites/{suite}/triggers/{trigger}', { params: { path: { suite: suiteId!, trigger: id } }, body })),
      onSuccess: refresh,
    }),
    remove: useMutation({
      mutationFn: (id: string) => unwrap(http.DELETE('/v1/suites/{suite}/triggers/{trigger}', { params: { path: { suite: suiteId!, trigger: id } } })),
      onSuccess: refresh,
    }),
    rotate: useMutation({
      mutationFn: (id: string) => unwrap(http.POST('/v1/suites/{suite}/triggers/{trigger}:rotate-secret', { params: { path: { suite: suiteId!, trigger: id } } })),
      onSuccess: refresh,
    }),
  }
}
