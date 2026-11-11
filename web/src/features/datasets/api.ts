import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { fetchAll, http, rawFetch, rawJson, saveBlob, unwrap, type S } from '../shared/api'

export const datasetKeys = {
  all: (project?: string) => ['datasets', project] as const,
  one: (id?: string) => ['dataset', id] as const,
  versions: (id?: string) => ['dataset-versions', id] as const,
  examples: (id?: string, version?: number, q?: string, tag?: string) => ['examples', id, version ?? 'draft', q ?? '', tag ?? ''] as const,
}

export function useDatasets(projectId?: string) {
  return useQuery({
    queryKey: datasetKeys.all(projectId),
    enabled: !!projectId,
    queryFn: () =>
      fetchAll<S['Dataset']>(async (cursor) => {
        const { data } = await http.GET('/v1/projects/{project}/datasets', { params: { path: { project: projectId! }, query: { cursor, limit: 200 } } })
        return data ?? { items: [] }
      }),
  })
}

export function useDataset(id?: string) {
  return useQuery({ queryKey: datasetKeys.one(id), enabled: !!id, queryFn: () => unwrap(http.GET('/v1/datasets/{dataset}', { params: { path: { dataset: id! } } })) })
}

export function useDatasetVersions(id?: string) {
  return useQuery({
    queryKey: datasetKeys.versions(id),
    enabled: !!id,
    queryFn: () =>
      fetchAll<S['DatasetVersion']>(async (cursor) => {
        const { data } = await http.GET('/v1/datasets/{dataset}/versions', { params: { path: { dataset: id! }, query: { cursor, limit: 200 } } })
        return data ?? { items: [] }
      }),
  })
}

const PAGE = 200

/** Paged examples (draft, or a published version). Pages load as the grid scrolls. */
export function useExamples(id: string | undefined, opts: { version?: number; q?: string; tag?: string } = {}) {
  return useInfiniteQuery({
    queryKey: datasetKeys.examples(id, opts.version, opts.q, opts.tag),
    enabled: !!id,
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor ?? undefined,
    queryFn: ({ pageParam }) =>
      unwrap(http.GET('/v1/datasets/{dataset}/examples', { params: { path: { dataset: id! }, query: { cursor: pageParam, limit: PAGE, version: opts.version, q: opts.q || undefined, tag: opts.tag || undefined } } })),
  })
}

/** Every example of a version (capped), for stats and diffs. */
export function useAllExamples(id: string | undefined, version: number | undefined, enabled = true) {
  return useQuery({
    queryKey: ['examples-all', id, version ?? 'draft'],
    enabled: !!id && enabled,
    queryFn: () =>
      fetchAll<S['Example']>(async (cursor) => {
        const { data } = await http.GET('/v1/datasets/{dataset}/examples', { params: { path: { dataset: id! }, query: { cursor, limit: 1000, version } } })
        return data ?? { items: [] }
      }, 10000),
  })
}

export function useCreateDataset(projectId?: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: S['DatasetCreate']) => unwrap(http.POST('/v1/projects/{project}/datasets', { params: { path: { project: projectId! } }, body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: datasetKeys.all(projectId) }),
  })
}

export function useUpdateDataset(id?: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: S['DatasetUpdate']) => unwrap(http.PATCH('/v1/datasets/{dataset}', { params: { path: { dataset: id! } }, body })),
    onSuccess: (d) => {
      qc.setQueryData(datasetKeys.one(id), d)
      void qc.invalidateQueries({ queryKey: ['datasets'] })
    },
  })
}

export function useDeleteDataset(projectId?: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => unwrap(http.DELETE('/v1/datasets/{dataset}', { params: { path: { dataset: id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: datasetKeys.all(projectId) }),
  })
}

/** Invalidate everything derived from a dataset's draft. */
export function useRefreshDraft(id?: string) {
  const qc = useQueryClient()
  return () => {
    void qc.invalidateQueries({ queryKey: ['examples', id] })
    void qc.invalidateQueries({ queryKey: ['examples-all', id] })
    void qc.invalidateQueries({ queryKey: datasetKeys.one(id) })
    void qc.invalidateQueries({ queryKey: ['datasets'] })
  }
}

export function useUpsertExamples(id?: string) {
  const refresh = useRefreshDraft(id)
  return useMutation({
    mutationFn: (examples: S['ExampleData'][]) => unwrap(http.POST('/v1/datasets/{dataset}/examples', { params: { path: { dataset: id! } }, body: { examples } })),
    onSuccess: refresh,
  })
}

export function useDeleteExamples(id?: string) {
  const refresh = useRefreshDraft(id)
  return useMutation({
    mutationFn: (ids: string[]) => unwrap(http.POST('/v1/datasets/{dataset}/examples:delete', { params: { path: { dataset: id! } }, body: { ids } })),
    onSuccess: refresh,
  })
}

export function usePublishVersion(id?: string) {
  const qc = useQueryClient()
  const refresh = useRefreshDraft(id)
  return useMutation({
    mutationFn: (note: string) => unwrap(http.POST('/v1/datasets/{dataset}/versions', { params: { path: { dataset: id! } }, body: { note } })),
    onSuccess: () => {
      refresh()
      void qc.invalidateQueries({ queryKey: datasetKeys.versions(id) })
    },
  })
}

export type ImportRequest = S['DatasetImportRequest']

/** JSON import / preview (inline data, URL, Hugging Face). */
export function importJson(id: string, body: ImportRequest): Promise<S['DatasetImportResult']> {
  return rawJson(`/v1/datasets/${id}:import`, { method: 'POST', json: body })
}

/** Multipart import / preview of an uploaded file. */
export function importFile(id: string, file: File, options: ImportRequest): Promise<S['DatasetImportResult']> {
  const fd = new FormData()
  fd.append('file', file)
  fd.append('options', JSON.stringify(options))
  return rawJson(`/v1/datasets/${id}:import`, { method: 'POST', body: fd })
}

export async function downloadExport(id: string, version: number, format: 'jsonl' | 'csv', name: string) {
  const res = await rawFetch(`/v1/datasets/${id}/versions/${version}/export?format=${format}`)
  saveBlob(await res.blob(), `${name}-v${version}.${format}`)
}
