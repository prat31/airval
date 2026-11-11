/**
 * Human names for detail-route breadcrumbs (run / suite / dataset / target), resolved through the
 * same queries the detail screens use, so they usually come straight from the cache.
 */
import { useQuery } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
import { useDataset } from '@/features/datasets/api'
import { runQuery, suitesQuery } from '@/features/runs/api'
import { runTitle } from '@/features/runs/ui'
import { useSuite } from '@/features/suites/api'
import { useTarget } from '@/features/targets/api'

export type CrumbEntity = 'run' | 'suite' | 'dataset' | 'target'

export function useCrumbNames(projectId: string | undefined): Partial<Record<CrumbEntity, string>> {
  const p = useParams({ strict: false }) as Partial<Record<CrumbEntity, string>>
  const run = useQuery(runQuery(p.run ?? ''))
  const suites = useQuery({ ...suitesQuery(projectId ?? ''), enabled: !!projectId && !!p.run })
  const suite = useSuite(projectId, p.suite && p.suite !== 'new' ? p.suite : undefined)
  const dataset = useDataset(p.dataset)
  const target = useTarget(p.target)
  const runSuite = suites.data?.find((s) => s.id === run.data?.suite_id)?.name
  return {
    run: run.data ? runTitle(run.data, runSuite) : undefined,
    suite: suite.data?.name,
    dataset: dataset.data?.name,
    target: target.data?.name,
  }
}
