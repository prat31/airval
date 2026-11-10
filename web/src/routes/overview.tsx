import { Link } from '@tanstack/react-router'
import { Activity, CircleDollarSign, Gauge, LayoutDashboard, Play, Target } from 'lucide-react'
import { Screen } from '@/app/screen'
import { useWorkspace } from '@/app/workspace'
import { Button } from '@/components/ui/button'
import { Card, CardHeader } from '@/components/ui/card'
import { EmptyState } from '@/components/ui/empty-state'
import { PageHeader } from '@/components/ui/page-header'
import { Skeleton } from '@/components/ui/skeleton'
import { StatTile } from '@/components/ui/stat-tile'

export function OverviewScreen() {
  const { org, project } = useWorkspace()
  const params = { org: org.slug, project: project?.slug ?? '' }
  return (
    <Screen>
      <PageHeader
        icon={<LayoutDashboard />}
        title="Overview"
        description={project?.description ?? 'Health of this project across suites and targets.'}
        actions={
          <Button variant="primary" asChild>
            <Link to="/o/$org/p/$project/suites" params={params}>
              <Play />
              Run suite
            </Link>
          </Button>
        }
      />
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <StatTile label="Runs · last 7 days" icon={<Activity />} value="—" loading />
        <StatTile label="Latest pass rate" icon={<Target />} value="—" loading />
        <StatTile label="Average score" icon={<Gauge />} value="—" loading />
        <StatTile label="Spend · last 7 days" icon={<CircleDollarSign />} value="—" loading />
      </div>
      <div className="grid grid-cols-1 gap-3 lg:grid-cols-3">
        <Card className="lg:col-span-2">
          <CardHeader title="Pass rate by suite" description="Per run, last 30 days" />
          <div className="px-4 pb-4">
            <Skeleton className="h-[240px] w-full rounded-md" />
          </div>
        </Card>
        <Card>
          <CardHeader title="Failing gates" />
          <div className="flex flex-col gap-3 px-4 pb-4 pt-2">
            {[0, 1, 2].map((i) => (
              <div key={i} className="flex items-center gap-3">
                <Skeleton className="size-6 rounded-full" />
                <div className="flex flex-1 flex-col gap-1.5">
                  <Skeleton className="h-3 w-3/4" />
                  <Skeleton className="h-2.5 w-1/2" />
                </div>
              </div>
            ))}
          </div>
        </Card>
      </div>
      <Card>
        <CardHeader title="Recent runs" />
        <EmptyState
          compact
          illustration="runs"
          title="No runs yet"
          description="Runs appear here as soon as a suite is executed — manually, on a schedule, from a webhook or CI."
          action={
            <Button variant="primary" size="sm" asChild>
              <Link to="/o/$org/p/$project/suites/new" params={params}>
                Create a suite
              </Link>
            </Button>
          }
        />
      </Card>
    </Screen>
  )
}
