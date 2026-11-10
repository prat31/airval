import { Link, useParams } from '@tanstack/react-router'
import { ClipboardCheck, Crosshair, Database, Gauge, History, Layers, Plus, Upload } from 'lucide-react'
import type { ReactNode } from 'react'
import { Screen } from '@/app/screen'
import { useWorkspace } from '@/app/workspace'
import { Button } from '@/components/ui/button'
import { Card, CardHeader } from '@/components/ui/card'
import { EmptyState, type IllustrationName } from '@/components/ui/empty-state'
import { PageHeader } from '@/components/ui/page-header'
import { Skeleton, SkeletonText } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'

interface ListScreenProps {
  icon: ReactNode
  title: string
  description: string
  actions?: ReactNode
  illustration: IllustrationName
  emptyTitle: string
  emptyDescription: string
  emptyAction?: ReactNode
  emptySecondary?: ReactNode
}

/** Shared scaffold for collection screens until their real implementation lands. */
function ListScreen(p: ListScreenProps) {
  return (
    <Screen>
      <PageHeader icon={p.icon} title={p.title} description={p.description} actions={p.actions} />
      <Card>
        <EmptyState illustration={p.illustration} title={p.emptyTitle} description={p.emptyDescription} action={p.emptyAction} secondaryAction={p.emptySecondary} />
      </Card>
    </Screen>
  )
}

/** Shared scaffold for detail screens: header, tabs and a layout-matching skeleton. */
function DetailScreen({ title, description, tabs }: { title: ReactNode; description: string; tabs: string[] }) {
  return (
    <Screen>
      <PageHeader
        title={title}
        description={description}
        actions={<Skeleton className="h-8 w-24 rounded-md" />}
        tabs={
          <Tabs defaultValue={tabs[0]}>
            <TabsList>
              {tabs.map((t) => (
                <TabsTrigger key={t} value={t}>
                  {t}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
        }
      />
      <div className="grid grid-cols-1 gap-3 lg:grid-cols-3">
        <Card className="lg:col-span-2">
          <CardHeader title={<Skeleton className="h-4 w-40" />} />
          <div className="px-4 pb-4 pt-2">
            <Skeleton className="h-64 w-full rounded-md" />
          </div>
        </Card>
        <Card>
          <CardHeader title={<Skeleton className="h-4 w-24" />} />
          <div className="px-4 pb-4 pt-2">
            <SkeletonText lines={6} />
          </div>
        </Card>
      </div>
    </Screen>
  )
}

function useProjectParams() {
  const { org, project } = useWorkspace()
  return { org: org.slug, project: project?.slug ?? '' }
}

export function SuitesScreen() {
  const params = useProjectParams()
  const newSuite = (
    <Button variant="primary" asChild>
      <Link to="/o/$org/p/$project/suites/new" params={params}>
        <Plus />
        New suite
      </Link>
    </Button>
  )
  return (
    <ListScreen
      icon={<Layers />}
      title="Suites"
      description="Reusable evaluation definitions: data, evaluators, gates and triggers."
      actions={newSuite}
      illustration="suites"
      emptyTitle="Create your first suite"
      emptyDescription="A suite pairs a dataset version with evaluators and gates. Run it manually, on a schedule or from CI."
      emptyAction={newSuite}
    />
  )
}

export function SuiteDetailScreen() {
  const { suite } = useParams({ strict: false }) as { suite: string }
  return <DetailScreen title={suite} description="Configuration, gates, triggers and history." tabs={['Overview', 'Gates', 'Triggers', 'History']} />
}

export function DatasetsScreen() {
  return (
    <ListScreen
      icon={<Database />}
      title="Datasets"
      description="Versioned collections of examples."
      actions={
        <Button variant="primary" icon={<Plus />}>
          New dataset
        </Button>
      }
      illustration="datasets"
      emptyTitle="No datasets yet"
      emptyDescription="Import CSV, JSONL, JSON or Parquet, pull from Hugging Face, or generate examples with an LLM."
      emptyAction={
        <Button variant="primary" icon={<Upload />}>
          Import data
        </Button>
      }
      emptySecondary={<Button variant="ghost">Load sample dataset</Button>}
    />
  )
}

export function DatasetDetailScreen() {
  const { dataset } = useParams({ strict: false }) as { dataset: string }
  return <DetailScreen title={dataset} description="Examples, versions and column stats." tabs={['Examples', 'Versions', 'Stats']} />
}

export function TargetsScreen() {
  return (
    <ListScreen
      icon={<Crosshair />}
      title="Targets"
      description="Connections to the AI service under evaluation."
      actions={
        <Button variant="primary" icon={<Plus />}>
          New target
        </Button>
      }
      illustration="targets"
      emptyTitle="Connect a target"
      emptyDescription="Point airval at an HTTP endpoint or an LLM provider. Test the connection live before you save."
      emptyAction={
        <Button variant="primary" icon={<Plus />}>
          New target
        </Button>
      }
    />
  )
}

export function TargetDetailScreen() {
  const { target } = useParams({ strict: false }) as { target: string }
  return <DetailScreen title={target} description="Configuration, mapping playground, versions and health." tabs={['Configuration', 'Playground', 'Versions', 'Health']} />
}

export function EvaluatorsScreen() {
  return (
    <ListScreen
      icon={<Gauge />}
      title="Evaluators"
      description="Deterministic checks, LLM judges and human review."
      actions={
        <Button variant="primary" icon={<Plus />}>
          New evaluator
        </Button>
      }
      illustration="evaluators"
      emptyTitle="Score outputs your way"
      emptyDescription="Start from presets like correctness, faithfulness or JSON schema, or write a rubric for an LLM judge."
      emptyAction={<Button variant="primary">Browse presets</Button>}
    />
  )
}

export function ReviewScreen() {
  return (
    <ListScreen
      icon={<ClipboardCheck />}
      title="Review"
      description="Human grading queues."
      illustration="review"
      emptyTitle="Nothing to review"
      emptyDescription="Results land here when a suite includes a human evaluator. Grade with 1–5, P / F and comments."
      emptyAction={
        <Button variant="secondary" icon={<History />}>
          View past annotations
        </Button>
      }
    />
  )
}
