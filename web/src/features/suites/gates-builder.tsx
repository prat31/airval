import { CircleAlert, CircleCheck, Plus, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { Button, IconButton } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { EmptyState } from '@/components/ui/empty-state'
import { Input } from '@/components/ui/input'
import { SegmentedControl } from '@/components/ui/segmented-control'
import { Select } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/cn'
import type { S } from '../shared/api'
import {
  comparators,
  conditionProblem,
  conditionText,
  conditionsFromExpr,
  defaultMetricFor,
  exprFromConditions,
  exprText,
  fnOptions,
  metricOptions,
  newCondition,
  parseGateText,
  presets,
  selectorOptions,
  type Cmp,
  type Condition,
  type Fn,
  type Joiner,
  type Metric,
  type Selector,
} from './gates'

export interface GateDraft {
  key: string
  id?: string
  name: string
  enabled: boolean
  joiner: Joiner
  conds: Condition[]
  /** Expression the builder can't represent (nested logic); edited as text only. */
  advanced?: S['GateExpr']
}

let seq = 0
export const newGateKey = () => `g${++seq}`

export function gateFromServer(g: S['Gate']): GateDraft {
  const flat = conditionsFromExpr(g.expr)
  return { key: g.id, id: g.id, name: g.name, enabled: g.enabled, joiner: flat?.joiner ?? 'and', conds: flat?.conds ?? [], advanced: flat ? undefined : g.expr }
}

export function gateExpr(g: GateDraft): S['GateExpr'] | null {
  return g.advanced ?? exprFromConditions(g.conds, g.joiner)
}

export function gateProblem(g: GateDraft): string | null {
  if (g.advanced) return null
  if (g.conds.length === 0) return 'Add at least one condition'
  for (const c of g.conds) {
    const p = conditionProblem(c)
    if (p) return p
  }
  return null
}

type Ev = { id: string; name: string }

function ConditionRow({ c, evaluators, onChange, onRemove, removable }: { c: Condition; evaluators: Ev[]; onChange: (c: Condition) => void; onRemove: () => void; removable: boolean }) {
  const problem = conditionProblem(c)
  const needsEvaluator = c.metric === 'evaluator' || c.fn === 'regression_vs_baseline'
  const showSelector = c.fn === 'count' || c.fn === 'pass_rate'
  const set = (patch: Partial<Condition>) => onChange({ ...c, ...patch })
  return (
    <div className="flex flex-col gap-1">
      <div className="grid grid-cols-2 items-center gap-2 sm:grid-cols-[repeat(auto-fit,minmax(130px,1fr))_auto]">
        <Select
          size="sm"
          aria-label="Function"
          value={c.fn}
          onValueChange={(fn) => {
            const metric = defaultMetricFor(fn as Fn)
            set({ fn: fn as Fn, metric, selector: fn === 'count' ? 'failed' : undefined, evaluator_id: metric === 'evaluator' || fn === 'regression_vs_baseline' ? (c.evaluator_id ?? evaluators[0]?.id) : undefined })
          }}
          options={fnOptions.map((f) => ({ value: f.value, label: f.label }))}
        />
        {c.fn !== 'regression_vs_baseline' && (
          <Select
            size="sm"
            aria-label="Metric"
            value={c.metric}
            onValueChange={(metric) => set({ metric: metric as Metric, evaluator_id: metric === 'evaluator' ? (c.evaluator_id ?? evaluators[0]?.id) : undefined })}
            options={metricOptions.filter((m) => (c.fn === 'count' ? m.value === 'results' : true)).map((m) => ({ value: m.value, label: m.label }))}
          />
        )}
        {needsEvaluator && (
          <Select
            size="sm"
            aria-label="Evaluator"
            value={c.evaluator_id ?? ''}
            invalid={!c.evaluator_id}
            placeholder="Evaluator"
            onValueChange={(id) => set({ evaluator_id: id })}
            options={evaluators.map((e) => ({ value: e.id, label: e.name }))}
          />
        )}
        {showSelector && (c.metric === 'results' || c.fn === 'count') && (
          <Select size="sm" aria-label="Which results" value={c.selector ?? 'all'} onValueChange={(s) => set({ selector: s as Selector })} options={selectorOptions.map((s) => ({ value: s.value, label: s.label }))} />
        )}
        <Select size="sm" aria-label="Comparator" value={c.comparator} onValueChange={(cmp) => set({ comparator: cmp as Cmp })} options={comparators.map((x) => ({ value: x, label: x }))} />
        <Input size="sm" type="number" step="any" aria-label="Threshold" value={Number.isFinite(c.value) ? String(c.value) : ''} onChange={(e) => set({ value: e.target.value === '' ? NaN : Number(e.target.value) })} className="tabular" />
        <Input size="sm" aria-label="Tag filter" placeholder="Tag (optional)" value={c.tag ?? ''} onChange={(e) => set({ tag: e.target.value || undefined })} />
        <IconButton label="Remove condition" size="sm" disabled={!removable} onClick={onRemove}>
          <Trash2 />
        </IconButton>
      </div>
      {problem && <p className="text-xs text-fail">{problem}</p>}
    </div>
  )
}

function GateCard({ gate, evaluators, onChange, onRemove }: { gate: GateDraft; evaluators: Ev[]; onChange: (g: GateDraft) => void; onRemove: () => void }) {
  const [mode, setMode] = useState<'builder' | 'text'>(gate.advanced ? 'text' : 'builder')
  const name = (id?: string) => evaluators.find((e) => e.id === id)?.name ?? 'evaluator'
  const text = gate.advanced ? exprText(gate.advanced, name) : gate.conds.length === 0 ? '' : gate.conds.map((c) => conditionText(c, name)).join(` ${gate.joiner} `)
  const [draftText, setDraftText] = useState(text)
  const parsed = mode === 'text' && !gate.advanced ? parseGateText(draftText, evaluators) : null
  const problem = gateProblem(gate)

  return (
    <Card className={cn('flex flex-col gap-3 p-4', !gate.enabled && 'opacity-70')}>
      <div className="flex items-center gap-2">
        <Input size="sm" aria-label="Gate name" value={gate.name} onChange={(e) => onChange({ ...gate, name: e.target.value })} placeholder="Gate name, e.g. Quality bar" className="max-w-xs font-medium" />
        <div className="ml-auto flex items-center gap-2">
          {!gate.advanced && (
            <SegmentedControl
              aria-label="Editing mode"
              size="sm"
              value={mode}
              onValueChange={(m) => {
                if (m === 'text') setDraftText(text)
                setMode(m)
              }}
              options={[
                { value: 'builder', label: 'Builder' },
                { value: 'text', label: 'Text' },
              ]}
            />
          )}
          <Switch size="sm" aria-label="Gate enabled" checked={gate.enabled} onCheckedChange={(v) => onChange({ ...gate, enabled: v })} />
          <IconButton label="Delete gate" size="sm" onClick={onRemove}>
            <Trash2 />
          </IconButton>
        </div>
      </div>

      {mode === 'builder' && !gate.advanced ? (
        <div className="flex flex-col gap-2">
          {gate.conds.map((c, i) => (
            <div key={i} className="flex flex-col gap-2">
              {i > 0 && (
                <div className="flex items-center gap-2">
                  <SegmentedControl aria-label="Join conditions" size="sm" value={gate.joiner} onValueChange={(j) => onChange({ ...gate, joiner: j })} options={[{ value: 'and', label: 'and' }, { value: 'or', label: 'or' }]} />
                </div>
              )}
              <ConditionRow
                c={c}
                evaluators={evaluators}
                removable={gate.conds.length > 1}
                onChange={(next) => onChange({ ...gate, conds: gate.conds.map((x, j) => (j === i ? next : x)) })}
                onRemove={() => onChange({ ...gate, conds: gate.conds.filter((_, j) => j !== i) })}
              />
            </div>
          ))}
          <div>
            <Button size="sm" variant="ghost" icon={<Plus />} onClick={() => onChange({ ...gate, conds: [...gate.conds, newCondition(evaluators[0]?.id)] })}>
              Add condition
            </Button>
          </div>
        </div>
      ) : gate.advanced ? (
        <p className="text-sm text-text-2">This gate uses nested logic, so it is shown as text. Edit it via the API or recreate it with the builder.</p>
      ) : (
        <div className="flex flex-col gap-1.5">
          <Input
            aria-label="Gate expression"
            aria-invalid={parsed && !parsed.ok ? true : undefined}
            className="font-mono text-xs"
            value={draftText}
            spellCheck={false}
            placeholder="mean(faithfulness) >= 0.85 and p95(latency_ms) < 2000"
            onChange={(e) => {
              setDraftText(e.target.value)
              const r = parseGateText(e.target.value, evaluators)
              if (r.ok) onChange({ ...gate, conds: r.conds, joiner: r.joiner })
            }}
          />
          {parsed && (parsed.ok ? (
            <p className="flex items-center gap-1.5 text-xs text-pass"><CircleCheck className="size-3.5" /> Valid expression</p>
          ) : (
            <p className="flex items-center gap-1.5 text-xs text-fail" role="alert"><CircleAlert className="size-3.5" /> {parsed.error}</p>
          ))}
        </div>
      )}

      <div className="flex items-center gap-2 rounded-md bg-surface-2 px-3 py-2">
        <span className="text-xs font-medium text-text-muted">Reads as</span>
        <code className="min-w-0 truncate text-xs text-text">{text || '—'}</code>
        {!problem && gate.conds.length > 0 && <CircleCheck className="ml-auto size-3.5 shrink-0 text-pass" aria-label="Valid" />}
      </div>
    </Card>
  )
}

export function GatesBuilder({ gates, onChange, evaluators }: { gates: GateDraft[]; onChange: (g: GateDraft[]) => void; evaluators: Ev[] }) {
  const add = (c?: Condition, name = '') => onChange([...gates, { key: newGateKey(), name, enabled: true, joiner: 'and', conds: [c ?? newCondition(evaluators[0]?.id)] }])
  return (
    <div className="flex flex-col gap-3">
      <p className="text-sm text-text-2">A run passes when every enabled gate holds. Gates decide the verdict, the CI exit code and notifications.</p>
      <div className="flex flex-wrap gap-1.5" aria-label="Gate presets">
        {presets(evaluators[0]?.id).map((p) => (
          <button
            key={p.label}
            type="button"
            onClick={() => add(p.cond, p.label)}
            className="inline-flex h-6 items-center gap-1 rounded-sm px-2 text-xs text-text-2 ring-1 ring-inset ring-border transition-colors hover:bg-surface-hover hover:text-text focus-ring"
          >
            <Plus className="size-3" />
            {p.label}
          </button>
        ))}
      </div>
      {gates.length === 0 ? (
        <Card>
          <EmptyState compact illustration="suites" title="No gates yet" description="Without gates a run always finishes as passed. Add a pass criterion, or pick a preset above." action={<Button size="sm" variant="primary" icon={<Plus />} onClick={() => add()}>Add gate</Button>} />
        </Card>
      ) : (
        <>
          {gates.map((g) => (
            <GateCard key={g.key} gate={g} evaluators={evaluators} onChange={(n) => onChange(gates.map((x) => (x.key === g.key ? n : x)))} onRemove={() => onChange(gates.filter((x) => x.key !== g.key))} />
          ))}
          <div>
            <Button icon={<Plus />} onClick={() => add()}>
              Add gate
            </Button>
          </div>
        </>
      )}
    </div>
  )
}
