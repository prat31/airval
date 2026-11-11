import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { fetchAll, http, unwrap, type Obj, type S } from '../shared/api'
import type { JsonSchema } from '../shared/schema-form'

/** One evaluator kind as served by the catalog (mirrors internal/evaluators KindInfo). */
export interface KindInfo {
  kind: S['EvaluatorKind']
  name: string
  category: S['EvaluatorCategory']
  group: string
  description: string
  cost?: number | string
  default_threshold?: number
  config_schema?: JsonSchema
  apply_to?: string[]
  requires?: string[]
  prompt_version?: string
  prompt?: string
}

export const humanizeKind = (k: string) => k.replace(/_/g, ' ').replace(/^\w/, (c) => c.toUpperCase())

/** Evaluator catalog with JSON-schema configs (renders the evaluator forms). */
export function useCatalog() {
  return useQuery({
    queryKey: ['evaluator-catalog'],
    staleTime: 10 * 60_000,
    queryFn: async () => ({ items: (await unwrap(http.GET('/v1/evaluators/catalog', {}))).items as unknown as KindInfo[] }),
  })
}

export function useEvaluators(projectId?: string) {
  return useQuery({
    queryKey: ['evaluators', projectId],
    enabled: !!projectId,
    queryFn: () =>
      fetchAll<S['Evaluator']>(async (cursor) => {
        const { data } = await http.GET('/v1/projects/{project}/evaluators', { params: { path: { project: projectId! }, query: { cursor, limit: 200 } } })
        return data ?? { items: [] }
      }),
  })
}

export function useEvaluator(id?: string) {
  return useQuery({ queryKey: ['evaluator', id], enabled: !!id, queryFn: () => unwrap(http.GET('/v1/evaluators/{evaluator}', { params: { path: { evaluator: id! } } })) })
}

export function useSaveEvaluator(projectId?: string, id?: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (b: { name: string; description: string; kind: S['EvaluatorKind']; config: Obj }) =>
      id
        ? unwrap(http.PATCH('/v1/evaluators/{evaluator}', { params: { path: { evaluator: id } }, body: b as never }))
        : unwrap(http.POST('/v1/projects/{project}/evaluators', { params: { path: { project: projectId! } }, body: b as never })),
    onSuccess: (e) => {
      qc.setQueryData(['evaluator', e.id], e)
      void qc.invalidateQueries({ queryKey: ['evaluators', projectId] })
    },
  })
}

export function useDeleteEvaluator(projectId?: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => unwrap(http.DELETE('/v1/evaluators/{evaluator}', { params: { path: { evaluator: id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['evaluators', projectId] }),
  })
}

export function useAgreement(id?: string) {
  return useQuery({ queryKey: ['evaluator-agreement', id], enabled: !!id, queryFn: () => unwrap(http.GET('/v1/evaluators/{evaluator}/agreement', { params: { path: { evaluator: id! } } })) })
}

export function usePreviewEvaluator() {
  return useMutation({
    mutationFn: (b: S['EvaluatorPreviewRequest']) => unwrap(http.POST('/v1/evaluators:preview', { body: b })),
    onError: () => undefined, // shown inline
  })
}

export const categoryLabel: Record<S['EvaluatorCategory'], string> = { deterministic: 'Deterministic', model: 'LLM judge', human: 'Human' }

export function costLabel(c: KindInfo['cost']): string {
  if (c === undefined) return ''
  const key = typeof c === 'string' ? c : (['free', 'model', 'human'][c] ?? '')
  return ({ free: 'Instant', model: 'Model call', human: 'Human' } as Record<string, string>)[key] ?? key
}
