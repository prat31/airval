import { GitCompare } from 'lucide-react'
import { Screen } from '@/app/screen'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { EmptyState } from '@/components/ui/empty-state'
import { PageHeader } from '@/components/ui/page-header'

export function CompareScreen() {
  return (
    <Screen width="wide">
      <PageHeader icon={<GitCompare />} title="Compare runs" description="Pick 2–4 runs to see score deltas, regressions and improvements side by side." />
      <Card>
        <EmptyState
          illustration="chart"
          title="Choose runs to compare"
          description="Select runs from the Runs list and choose Compare, or pick a baseline and a candidate here."
          action={<Button variant="primary">Select runs</Button>}
        />
      </Card>
    </Screen>
  )
}
