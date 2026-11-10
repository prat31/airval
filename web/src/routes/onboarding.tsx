import { ArrowLeft, ArrowRight, Sparkles } from 'lucide-react'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { EmptyState, type IllustrationName } from '@/components/ui/empty-state'
import { Stepper } from '@/components/ui/stepper'

const steps: { id: string; label: string; title: string; description: string; illustration: IllustrationName }[] = [
  { id: 'project', label: 'Project', title: 'Name your project', description: 'A project is one AI service under evaluation.', illustration: 'generic' },
  { id: 'target', label: 'Target', title: 'Connect a target', description: 'An HTTP endpoint or LLM provider, with a live connection test.', illustration: 'targets' },
  { id: 'dataset', label: 'Dataset', title: 'Add examples', description: 'Import a file, pull from Hugging Face, or generate examples.', illustration: 'datasets' },
  { id: 'evaluators', label: 'Evaluators', title: 'Pick evaluators', description: 'Start from presets — you can tune thresholds later.', illustration: 'evaluators' },
  { id: 'run', label: 'First run', title: 'Run it', description: 'Watch results stream in live.', illustration: 'runs' },
]

export function OnboardingScreen() {
  const [step, setStep] = useState(0)
  const s = steps[step]!
  return (
    <div className="mx-auto flex w-full max-w-2xl flex-col gap-6">
      <div className="text-center">
        <h1 className="text-xl font-semibold tracking-tight text-text">Let’s evaluate your AI service</h1>
        <p className="mt-1 text-sm text-text-2">About three minutes. Or load the sample project and explore.</p>
      </div>
      <div className="rounded-lg border border-border bg-surface shadow-popover">
        <div className="border-b border-border px-5 py-4">
          <Stepper steps={steps} current={step} onStepClick={setStep} />
        </div>
        <EmptyState illustration={s.illustration} title={s.title} description={s.description} />
        <div className="flex items-center justify-between border-t border-border px-5 py-3">
          <Button variant="ghost" icon={<ArrowLeft />} disabled={step === 0} onClick={() => setStep((x) => x - 1)}>
            Back
          </Button>
          <Button variant="primary" iconRight={<ArrowRight />} disabled={step === steps.length - 1} onClick={() => setStep((x) => x + 1)}>
            Continue
          </Button>
        </div>
      </div>
      <div className="flex justify-center">
        <Button variant="ghost" icon={<Sparkles />}>
          Load sample project instead
        </Button>
      </div>
    </div>
  )
}
