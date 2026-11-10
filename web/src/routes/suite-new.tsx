import { ArrowLeft, ArrowRight, Layers } from 'lucide-react'
import { useState } from 'react'
import { Screen } from '@/app/screen'
import { Button } from '@/components/ui/button'
import { Card, CardHeader } from '@/components/ui/card'
import { EmptyState } from '@/components/ui/empty-state'
import { KeyValue } from '@/components/ui/key-value'
import { PageHeader } from '@/components/ui/page-header'
import { Stepper } from '@/components/ui/stepper'

const steps = [
  { id: 'target', label: 'Target', description: 'What to evaluate' },
  { id: 'data', label: 'Data', description: 'Dataset version' },
  { id: 'evaluators', label: 'Evaluators', description: 'How to score' },
  { id: 'gates', label: 'Gates', description: 'Pass criteria' },
  { id: 'execution', label: 'Execution', description: 'Trials, sampling, triggers' },
]

export function SuiteNewScreen() {
  const [step, setStep] = useState(0)
  const current = steps[step]!
  return (
    <Screen>
      <PageHeader icon={<Layers />} title="New suite" description="Define what to run, how to score it and when it passes." />
      <Card className="px-4 py-3">
        <Stepper steps={steps} current={step} onStepClick={setStep} />
      </Card>
      <div className="grid grid-cols-1 gap-3 lg:grid-cols-3">
        <Card className="lg:col-span-2">
          <CardHeader title={current.label} description={current.description} />
          <EmptyState compact illustration="suites" title={`${current.label} step`} description="The suite editor form lands in milestone 6." />
          <div className="flex items-center justify-between border-t border-border px-4 py-3">
            <Button variant="ghost" icon={<ArrowLeft />} disabled={step === 0} onClick={() => setStep((s) => s - 1)}>
              Back
            </Button>
            <Button variant="primary" iconRight={<ArrowRight />} disabled={step === steps.length - 1} onClick={() => setStep((s) => s + 1)}>
              Continue
            </Button>
          </div>
        </Card>
        <Card className="h-fit lg:sticky lg:top-6">
          <CardHeader title="Summary" description="Estimated for one run" />
          <div className="px-4 pb-3">
            <KeyValue
              items={[
                { label: 'Target', value: '—' },
                { label: 'Examples', value: '—' },
                { label: 'Evaluators', value: '—' },
                { label: 'Est. duration', value: '—' },
                { label: 'Est. cost', value: '—' },
              ]}
            />
          </div>
        </Card>
      </div>
    </Screen>
  )
}
