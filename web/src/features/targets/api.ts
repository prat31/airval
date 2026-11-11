import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { fetchAll, http, unwrap, type Obj, type S } from '../shared/api'

export const targetKeys = {
  all: (project?: string) => ['targets', project] as const,
  one: (id?: string) => ['target', id] as const,
  versions: (id?: string) => ['target-versions', id] as const,
}

export function useTargets(projectId?: string) {
  return useQuery({
    queryKey: targetKeys.all(projectId),
    enabled: !!projectId,
    queryFn: () =>
      fetchAll<S['Target']>(async (cursor) => {
        const { data } = await http.GET('/v1/projects/{project}/targets', { params: { path: { project: projectId! }, query: { cursor, limit: 200 } } })
        return data ?? { items: [] }
      }),
  })
}

export function useTarget(id?: string) {
  return useQuery({
    queryKey: targetKeys.one(id),
    enabled: !!id && id !== 'new',
    queryFn: () => unwrap(http.GET('/v1/targets/{target}', { params: { path: { target: id! } } })),
  })
}

export function useTargetVersions(id?: string) {
  return useQuery({
    queryKey: targetKeys.versions(id),
    enabled: !!id && id !== 'new',
    queryFn: () =>
      fetchAll<S['TargetVersion']>(async (cursor) => {
        const { data } = await http.GET('/v1/targets/{target}/versions', { params: { path: { target: id! }, query: { cursor, limit: 100 } } })
        return data ?? { items: [] }
      }),
  })
}

export function useCreateTarget(projectId?: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: S['TargetCreate']) => unwrap(http.POST('/v1/projects/{project}/targets', { params: { path: { project: projectId! } }, body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: targetKeys.all(projectId) }),
  })
}

export function useUpdateTarget(id?: string, projectId?: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: S['TargetUpdate']) => unwrap(http.PATCH('/v1/targets/{target}', { params: { path: { target: id! } }, body })),
    onSuccess: (t) => {
      qc.setQueryData(targetKeys.one(id), t)
      void qc.invalidateQueries({ queryKey: targetKeys.all(projectId) })
      void qc.invalidateQueries({ queryKey: targetKeys.versions(id) })
    },
  })
}

export function useDeleteTarget(projectId?: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => unwrap(http.DELETE('/v1/targets/{target}', { params: { path: { target: id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: targetKeys.all(projectId) }),
  })
}

export interface TestInput {
  kind: S['TargetKind']
  config: Obj
  example?: S['ExampleData']
  variables?: Record<string, string>
}

/** Tests an unsaved (draft) configuration: the form's live "Test connection". */
export function useTestDraft(projectId?: string) {
  return useMutation({
    mutationFn: (b: TestInput) =>
      unwrap(http.POST('/v1/projects/{project}/targets:test', { params: { path: { project: projectId! } }, body: b as unknown as S['TargetDraftTestRequest'] })),
    onError: () => undefined, // surfaced inline in the playground
  })
}

/** Recent health: latency and pass-rate series for one target. */
export function useTargetTrend(projectId: string | undefined, targetId: string | undefined, metric: S['TrendMetric']) {
  return useQuery({
    queryKey: ['target-trend', projectId, targetId, metric],
    enabled: !!projectId && !!targetId && targetId !== 'new',
    queryFn: () => unwrap(http.GET('/v1/projects/{project}/trends', { params: { path: { project: projectId! }, query: { target_id: targetId!, metric, bucket: 'run' } } })),
  })
}

export function useTargetRuns(projectId: string | undefined, targetId: string | undefined) {
  return useQuery({
    queryKey: ['target-runs', projectId, targetId],
    enabled: !!projectId && !!targetId && targetId !== 'new',
    queryFn: () => unwrap(http.GET('/v1/runs', { params: { query: { project_id: projectId!, target_id: targetId!, limit: 30 } } })),
  })
}
