/**
 * Organization home (`/o/$org`). With projects it opens the first one; with none it is a real page
 * inside the app shell, not a forced setup flow. Nothing is created on the user's behalf.
 */
import { useQuery } from '@tanstack/react-query'
import { Link, Navigate, useParams } from '@tanstack/react-router'
import { FolderPlus, Wand2 } from 'lucide-react'
import { useState } from 'react'
import { Screen } from '@/app/screen'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/ui/empty-state'
import { PageHeader } from '@/components/ui/page-header'
import { meQuery, projectsQuery } from './api'
import { SampleProjectCard } from './sample-project'
import { NewProjectDialog } from './workspace-dialogs'

export function OrgHomeScreen() {
  const { org: orgSlug } = useParams({ strict: false }) as { org: string }
  const me = useQuery(meQuery)
  const org = me.data?.orgs.find((o) => o.slug === orgSlug)
  const projects = useQuery({ ...projectsQuery(org?.id ?? ''), enabled: !!org })
  const [creating, setCreating] = useState(false)

  // OrgGate only renders this once both queries have resolved.
  if (!org || !projects.data) return null
  const first = projects.data[0]
  if (first) return <Navigate to="/o/$org/p/$project/overview" params={{ org: org.slug, project: first.slug }} replace />

  return (
    <Screen>
      <PageHeader title={org.name} description="Projects in this organization." />
      <NewProjectDialog open={creating} onOpenChange={setCreating} orgId={org.id} orgSlug={org.slug} />
      <div className="flex flex-col gap-6">
        <EmptyState
          illustration="generic"
          title="No projects yet"
          description="A project is one AI service under evaluation. It owns its targets, datasets, suites and runs."
          action={
            <Button variant="primary" icon={<FolderPlus />} onClick={() => setCreating(true)}>
              New project
            </Button>
          }
          secondaryAction={
            <Button variant="secondary" icon={<Wand2 />} asChild>
              <Link to="/onboarding" search={{ org: org.slug }}>
                Guided setup
              </Link>
            </Button>
          }
        />
        <SampleProjectCard orgId={org.id} orgSlug={org.slug} />
      </div>
    </Screen>
  )
}
