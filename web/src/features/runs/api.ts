/**
 * Runs, results, aggregates and the project catalog (suites / targets / evaluators) used by run screens.
 */
import { infiniteQueryOptions, keepPreviousData, queryOptions, useMutation, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '@/lib/api/client'
import type { components, operations } from '@/lib/api/schema'

type S = components['schemas']
export type Run = S['Run']
export type RunStatus = S['RunStatus']
export type TriggerKind = S['TriggerKind']
export type Result = S['Result']
export type ResultStatus = S['ResultStatus']
export type Score = S['Score']
export type RunAggregate = S['RunAggregate']
export type RunEvent = S['RunEvent']
export type Suite = S['Suite']
export type Target = S['Target']
export type Evaluator = S['Evaluator']
export type Dataset = S['Dataset']
export type ReviewQueue = S['ReviewQueue']
export type GateResult = S['GateResult']
export type RunCreate = S['RunCreate']
export type Trend = S['Trend']
export type TrendMetric = S['TrendMetric']

export type RunListQuery = NonNullable<operations['listRuns']['parameters']['query']>
export type ResultListQuery = Omit<NonNullable<operations['listResults']['parameters']['query']>, 'cursor' | 'limit'>

export const ACTIVE_STATUSES: RunStatus[] = ['queued', 'running']
export const isActive = (s: RunStatus) => s === 'queued' || s === 'running'

export const runKeys = {
  all: ['runs'] as const,
  lists: () => ['runs', 'list'] as const,
  list: (q: RunListQuery) => ['runs', 'list', q] as const,
  live: (projectId: string) => ['runs', 'live', projectId] as const,
  detail: (id: string) => ['runs', 'detail', id] as const,
  results: (id: string) => ['runs', 'results', id] as const,
  resultList: (id: string, q: ResultListQuery) => ['runs', 'results', id, q] as const,
  result: (runId: string, id: string) => ['runs', 'result', runId, id] as const,
  aggregates: (id: string) => ['runs', 'aggregates', id] as const,
}

const RUN_PAGE = 200
const RESULT_PAGE = 500

export const runsInfiniteQuery = (q: RunListQuery) =>
  infiniteQueryOptions({
    queryKey: runKeys.list(q),
    queryFn: ({ pageParam }) => unwrap(api.GET('/v1/runs', { params: { query: { ...q, cursor: pageParam, limit: q.limit ?? RUN_PAGE } } })),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor ?? undefined,
    placeholderData: keepPreviousData,
    enabled: !!q.project_id,
  })

/** One page of runs (overview, pickers). */
export const runsQuery = (q: RunListQuery) =>
  queryOptions({
    queryKey: [...runKeys.list(q), 'page'] as const,
    queryFn: async () => (await unwrap(api.GET('/v1/runs', { params: { query: q } }))).items,
    enabled: !!q.project_id,
  })

export const liveRunsQuery = (projectId: string) =>
  queryOptions({
    queryKey: runKeys.live(projectId),
    queryFn: async () =>
      (await unwrap(api.GET('/v1/runs', { params: { query: { project_id: projectId, status: ACTIVE_STATUSES, limit: 50 } } }))).items,
    enabled: !!projectId,
    refetchInterval: (q) => ((q.state.data?.length ?? 0) > 0 ? 5_000 : 20_000),
    refetchIntervalInBackground: false,
  })

export const runQuery = (id: string) =>
  queryOptions({
    queryKey: runKeys.detail(id),
    queryFn: () => unwrap(api.GET('/v1/runs/{run}', { params: { path: { run: id } } })),
    enabled: !!id,
  })

export const aggregatesQuery = (id: string | null | undefined) =>
  queryOptions({
    queryKey: runKeys.aggregates(id ?? ''),
    queryFn: async () => (await unwrap(api.GET('/v1/runs/{run}/aggregates', { params: { path: { run: id! } } }))).items,
    enabled: !!id,
  })

export const resultsInfiniteQuery = (runId: string, q: ResultListQuery) =>
  infiniteQueryOptions({
    queryKey: runKeys.resultList(runId, q),
    queryFn: ({ pageParam }) =>
      unwrap(api.GET('/v1/runs/{run}/results', { params: { path: { run: runId }, query: { ...q, cursor: pageParam, limit: RESULT_PAGE } } })),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor ?? undefined,
    placeholderData: keepPreviousData,
    enabled: !!runId,
  })

export const resultQuery = (runId: string, id: string | undefined) =>
  queryOptions({
    queryKey: runKeys.result(runId, id ?? ''),
    queryFn: () => unwrap(api.GET('/v1/runs/{run}/results/{result}', { params: { path: { run: runId, result: id! } } })),
    enabled: !!id,
  })

export const trendsQuery = (projectId: string, q: NonNullable<operations['getTrends']['parameters']['query']>, enabled = true) =>
  queryOptions({
    queryKey: ['trends', projectId, q] as const,
    queryFn: () => unwrap(api.GET('/v1/projects/{project}/trends', { params: { path: { project: projectId }, query: q } })),
    enabled: !!projectId && enabled,
    retry: false,
    staleTime: 60_000,
  })

// ------------------------------------------------------------------ catalog
export const suitesQuery = (projectId: string) =>
  queryOptions({
    queryKey: ['suites', projectId, 'catalog'] as const,
    queryFn: async () => (await unwrap(api.GET('/v1/projects/{project}/suites', { params: { path: { project: projectId }, query: { limit: 1000 } } }))).items,
    enabled: !!projectId,
    staleTime: 60_000,
  })

export const targetsQuery = (projectId: string) =>
  queryOptions({
    queryKey: ['targets', projectId, 'catalog'] as const,
    queryFn: async () => (await unwrap(api.GET('/v1/projects/{project}/targets', { params: { path: { project: projectId }, query: { limit: 1000 } } }))).items,
    enabled: !!projectId,
    staleTime: 60_000,
  })

export const evaluatorsQuery = (projectId: string) =>
  queryOptions({
    queryKey: ['evaluators', projectId, 'catalog'] as const,
    queryFn: async () =>
      (await unwrap(api.GET('/v1/projects/{project}/evaluators', { params: { path: { project: projectId }, query: { limit: 1000 } } }))).items,
    enabled: !!projectId,
    staleTime: 60_000,
  })

export const datasetsQuery = (projectId: string) =>
  queryOptions({
    queryKey: ['datasets', projectId, 'catalog'] as const,
    queryFn: async () => (await unwrap(api.GET('/v1/projects/{project}/datasets', { params: { path: { project: projectId }, query: { limit: 1000 } } }))).items,
    enabled: !!projectId,
    staleTime: 60_000,
  })

export const reviewQueuesQuery = (projectId: string) =>
  queryOptions({
    queryKey: ['review-queues', projectId, 'catalog'] as const,
    queryFn: async () => (await unwrap(api.GET('/v1/projects/{project}/review-queues', { params: { path: { project: projectId } } }))).items,
    enabled: !!projectId,
    staleTime: 60_000,
  })

/** id → entity lookup for rendering names in tables. */
export function indexById<T extends { id: string }>(items: T[] | undefined): Map<string, T> {
  return new Map((items ?? []).map((i) => [i.id, i]))
}

// ------------------------------------------------------------------ mutations
export function useCreateRun() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: RunCreate) => unwrap(api.POST('/v1/runs', { body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: runKeys.all }),
    onError: () => {},
  })
}

export function useRunAction(runId: string) {
  const qc = useQueryClient()
  const done = () => qc.invalidateQueries({ queryKey: runKeys.all })
  return {
    cancel: useMutation({
      mutationFn: () => unwrap(api.POST('/v1/runs/{run}:cancel', { params: { path: { run: runId } } })),
      onSuccess: (run) => {
        qc.setQueryData(runKeys.detail(runId), run)
        void done()
      },
      onError: () => {},
    }),
    rerunFailed: useMutation({
      mutationFn: () => unwrap(api.POST('/v1/runs/{run}:rerun-failed', { params: { path: { run: runId } } })),
      onSuccess: done,
      onError: () => {},
    }),
    rescore: useMutation({
      mutationFn: (evaluatorIds?: string[]) =>
        unwrap(api.POST('/v1/runs/{run}:rescore', { params: { path: { run: runId } }, body: { evaluator_ids: evaluatorIds } })),
      onSuccess: done,
      onError: () => {},
    }),
  }
}

export function useAddToDataset() {
  return useMutation({
    mutationFn: ({ datasetId, resultIds, useOutput }: { datasetId: string; resultIds: string[]; useOutput: boolean }) =>
      unwrap(
        api.POST('/v1/datasets/{dataset}:from-results', {
          params: { path: { dataset: datasetId } },
          body: { result_ids: resultIds, use_output_as_expected: useOutput },
        }),
      ),
    onError: () => {},
  })
}

export function useSendToReview() {
  return useMutation({
    mutationFn: ({ queueId, resultIds }: { queueId: string; resultIds: string[] }) =>
      unwrap(api.POST('/v1/review-queues/{queue}/items', { params: { path: { queue: queueId } }, body: { result_ids: resultIds, select: 'all' } })),
    onError: () => {},
  })
}

export type ProjectOverview = S['ProjectOverview']
export const overviewQuery = (projectId: string) =>
  queryOptions({
    queryKey: ['overview', projectId] as const,
    queryFn: () => unwrap(api.GET('/v1/projects/{project}/overview', { params: { path: { project: projectId } } })),
    enabled: !!projectId,
    refetchInterval: 30_000,
  })
