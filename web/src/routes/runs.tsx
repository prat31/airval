import { Link, useParams } from '@tanstack/react-router'
import { GitCompare, Play } from 'lucide-react'
import { useState } from 'react'
import { Screen } from '@/app/screen'
import { useWorkspace } from '@/app/workspace'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { defaultDatePresets, DateRangePicker, type DateRange } from '@/components/ui/date-range-picker'
import { EmptyState } from '@/components/ui/empty-state'
import { FilterBar } from '@/components/ui/filter-bar'
import { PageHeader } from '@/components/ui/page-header'
import { Skeleton } from '@/components/ui/skeleton'
import { StatTile } from '@/components/ui/stat-tile'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'

export function RunsScreen() {
  const { org, project } = useWorkspace()
  const params = { org: org.slug, project: project?.slug ?? '' }
  const [search, setSearch] = useState('')
  const [status, setStatus] = useState<string[]>([])
  const [trigger, setTrigger] = useState<string[]>([])
  const [range, setRange] = useState<DateRange>(() => ({ ...defaultDatePresets[1]!.range(), preset: '7d' }))
  return (
    <Screen>
      <PageHeader
        icon={<Play />}
        title="Runs"
        description="Every execution of a suite against a target."
        actions={
          <>
            <Button asChild>
              <Link to="/o/$org/p/$project/compare" params={params}>
                <GitCompare />
                Compare
              </Link>
            </Button>
            <Button variant="primary" icon={<Play />}>
              Run suite
            </Button>
          </>
        }
      />
      <FilterBar
        search={search}
        onSearchChange={setSearch}
        searchPlaceholder="Search runs, labels, git sha…"
        filters={[
          {
            id: 'status',
            label: 'Status',
            value: status,
            onChange: setStatus,
            options: ['passed', 'failed', 'running', 'cancelled'].map((s) => ({ value: s, label: s[0]!.toUpperCase() + s.slice(1) })),
          },
          {
            id: 'trigger',
            label: 'Trigger',
            value: trigger,
            onChange: setTrigger,
            options: ['manual', 'schedule', 'webhook', 'api'].map((s) => ({ value: s, label: s === 'api' ? 'API / CI' : s[0]!.toUpperCase() + s.slice(1) })),
          },
        ]}
        actions={<DateRangePicker value={range} onChange={setRange} />}
      />
      <Card>
        <EmptyState
          illustration="runs"
          title="No runs yet"
          description={
            <>
              Start a run from a suite, or trigger one from CI with{' '}
              <code className="rounded-sm bg-surface-2 px-1 py-0.5 font-mono text-xs text-text">airval run --gate</code>.
            </>
          }
          action={
            <Button variant="primary" asChild>
              <Link to="/o/$org/p/$project/suites" params={params}>
                Go to suites
              </Link>
            </Button>
          }
          secondaryAction={<Button variant="ghost">Read the CI guide</Button>}
        />
      </Card>
    </Screen>
  )
}

export function RunDetailScreen() {
  const { run } = useParams({ strict: false }) as { run: string }
  return (
    <Screen width="wide">
      <PageHeader
        title={<span className="font-mono">{run}</span>}
        description="Run detail — scorecards, charts, results and gate verdicts land in milestone 7."
        meta={<Skeleton className="h-6 w-20 rounded-full" />}
        tabs={
          <Tabs defaultValue="results">
            <TabsList>
              <TabsTrigger value="results">Results</TabsTrigger>
              <TabsTrigger value="charts">Charts</TabsTrigger>
              <TabsTrigger value="config">Configuration</TabsTrigger>
            </TabsList>
          </Tabs>
        }
      />
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        {Array.from({ length: 4 }, (_, i) => (
          <StatTile key={i} label="Evaluator" value="—" loading />
        ))}
      </div>
      <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
        <Skeleton className="h-[260px] rounded-md" />
        <Skeleton className="h-[260px] rounded-md" />
      </div>
      <Skeleton className="h-[360px] rounded-md" />
    </Screen>
  )
}
