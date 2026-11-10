import { Link } from '@tanstack/react-router'
import {
  ArrowLeft,
  Bell,
  Calendar,
  CircleDollarSign,
  Copy,
  Download,
  Gauge,
  GitBranch,
  Monitor,
  Moon,
  MoreHorizontal,
  Pencil,
  Play,
  Plus,
  RefreshCw,
  Sun,
  Target,
  Trash2,
  Webhook,
  Zap,
} from 'lucide-react'
import { useMemo, useState, type ReactNode } from 'react'
import { LiveRunIndicatorView } from '@/app/live-runs'
import { Heatmap, Histogram, HorizontalBars, LatencyDistribution, Scatter, Sparkline, TrendLine, WinRateBar } from '@/charts'
import {
  Badge,
  Breadcrumbs,
  Button,
  Card,
  CardBody,
  CardHeader,
  Checkbox,
  CodeBlock,
  Combobox,
  CommandPalette,
  CronInput,
  DateRangePicker,
  defaultDatePresets,
  Dialog,
  DiffViewer,
  Drawer,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  EmptyState,
  Field,
  FilterBar,
  IconButton,
  Input,
  JsonViewer,
  Kbd,
  KeyValue,
  PageHeader,
  Popover,
  PopoverContent,
  PopoverTrigger,
  RadioGroup,
  RadioItem,
  ScoreChip,
  SegmentedControl,
  Select,
  selectionColumn,
  Skeleton,
  SkeletonTable,
  SkeletonText,
  StatTile,
  StatusPill,
  Stepper,
  Switch,
  Table,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  Textarea,
  toast,
  Tooltip,
  type ColumnDef,
  type DateRange,
  type RowSelectionState,
} from '@/components/ui'
import { cn } from '@/lib/cn'
import { formatDuration, formatRelative, formatUsd } from '@/lib/format'
import { useTheme, type ThemePreference } from '@/lib/theme'
import {
  evaluators,
  expectedText,
  heatValues,
  latencies,
  makeRuns,
  outputText,
  passByTag,
  sampleJson,
  scatterPoints,
  scoresA,
  scoresB,
  sparkData,
  tags,
  trendSeries,
  type FakeRun,
} from './fake-data'

const sections = [
  'Tokens',
  'Typography',
  'Buttons',
  'Forms',
  'Navigation',
  'Overlays',
  'Status',
  'Data display',
  'Table',
  'Code & diffs',
  'Scheduling',
  'Feedback',
  'Charts',
] as const

const slug = (s: string) => s.toLowerCase().replace(/[^a-z]+/g, '-')

function Section({ title, description, children }: { title: (typeof sections)[number]; description?: string; children: ReactNode }) {
  return (
    <section id={slug(title)} className="scroll-mt-20 border-t border-border pt-8 first:border-t-0 first:pt-0">
      <h2 className="text-lg font-semibold tracking-tight text-text">{title}</h2>
      {description && <p className="mt-0.5 text-sm text-text-2">{description}</p>}
      <div className="mt-5 flex flex-col gap-6">{children}</div>
    </section>
  )
}

function Demo({ label, children, className }: { label: string; children: ReactNode; className?: string }) {
  return (
    <div className="flex flex-col gap-2">
      <div className="text-xs font-medium text-text-muted">{label}</div>
      <div className={cn('flex flex-wrap items-center gap-3', className)}>{children}</div>
    </div>
  )
}

function Swatch({ name, varName }: { name: string; varName: string }) {
  return (
    <div className="flex items-center gap-2.5">
      <span className="size-8 shrink-0 rounded-sm ring-1 ring-inset ring-border" style={{ background: `var(${varName})` }} />
      <span className="flex flex-col">
        <span className="text-sm text-text">{name}</span>
        <span className="font-mono text-[11px] text-text-muted">{varName}</span>
      </span>
    </div>
  )
}

function TokensSection() {
  const neutrals = ['bg', 'surface', 'surface-2', 'surface-hover', 'border', 'border-strong', 'text', 'text-2', 'text-muted']
  return (
    <Section title="Tokens" description="Every color comes from tokens.css. Toggle the theme to see the dark values.">
      <Demo label="Neutrals (Slate)" className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
        {neutrals.map((n) => (
          <Swatch key={n} name={n} varName={`--${n}`} />
        ))}
      </Demo>
      <Demo label="Accent & status" className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
        {['accent', 'accent-hover', 'accent-subtle', 'pass', 'fail', 'warn', 'running', 'skipped', 'pass-subtle', 'fail-subtle'].map((n) => (
          <Swatch key={n} name={n} varName={`--${n}`} />
        ))}
      </Demo>
      <Demo label="Iris ramp (sequential)">
        <div className="flex w-full overflow-hidden rounded-md ring-1 ring-inset ring-border">
          {[50, 100, 200, 300, 400, 500, 600, 700, 800, 900].map((s) => (
            <div key={s} className="flex h-12 flex-1 items-end p-1.5" style={{ background: `var(--iris-${s})` }}>
              <span className={cn('font-mono text-[10px]', s >= 500 ? 'text-on-accent' : 'text-iris-900')} style={s >= 500 ? { color: 'var(--iris-50)' } : undefined}>
                {s}
              </span>
            </div>
          ))}
        </div>
      </Demo>
      <Demo label="Categorical chart slots (fixed order, max 6) · diverging">
        <div className="flex gap-1.5">
          {[1, 2, 3, 4, 5, 6].map((i) => (
            <div key={i} className="flex h-10 w-14 items-end rounded-sm p-1.5" style={{ background: `var(--chart-${i})` }}>
              <span className="font-mono text-[10px] text-on-accent">{i}</span>
            </div>
          ))}
        </div>
        <div className="h-10 w-64 rounded-sm" style={{ background: 'linear-gradient(90deg, var(--div-neg), var(--div-mid), var(--div-pos))' }} />
      </Demo>
    </Section>
  )
}

function TypographySection() {
  const scale = [
    ['2xl', '32/40 · hero numbers'],
    ['xl', '24/32'],
    ['lg', '20/28 · page titles'],
    ['md', '16/24'],
    ['base', '14/20'],
    ['sm', '13/20 · default UI'],
    ['xs', '12/16 · captions'],
  ] as const
  return (
    <Section title="Typography" description="Inter for UI, JetBrains Mono for code and IO.">
      <Card>
        <CardBody className="flex flex-col divide-y divide-border py-1">
          {scale.map(([k, note]) => (
            <div key={k} className="flex items-baseline justify-between gap-4 py-2.5">
              <span className={cn('text-text', `text-${k}`, k === '2xl' && 'tabular font-semibold tracking-tight')}>
                {k === '2xl' ? '94.2%' : 'Calm precision for evaluation'}
              </span>
              <span className="shrink-0 font-mono text-xs text-text-muted">
                text-{k} · {note}
              </span>
            </div>
          ))}
          <div className="py-2.5 font-mono text-sm text-text-2">{'{ "input": "How do I reset my password?" }'}</div>
        </CardBody>
      </Card>
    </Section>
  )
}

function ButtonsSection() {
  return (
    <Section title="Buttons">
      <Demo label="Variants · md">
        <Button variant="primary" icon={<Play />}>
          Run suite
        </Button>
        <Button icon={<Plus />}>New dataset</Button>
        <Button variant="ghost">Cancel</Button>
        <Button variant="danger" icon={<Trash2 />}>
          Delete
        </Button>
        <Button variant="primary" loading>
          Saving
        </Button>
        <Button disabled>Disabled</Button>
      </Demo>
      <Demo label="Size sm">
        <Button size="sm" variant="primary">
          Primary
        </Button>
        <Button size="sm">Secondary</Button>
        <Button size="sm" variant="ghost" icon={<RefreshCw />}>
          Re-run failed
        </Button>
        <Button size="sm" variant="danger">
          Cancel run
        </Button>
      </Demo>
      <Demo label="Icon buttons">
        <IconButton label="More">
          <MoreHorizontal />
        </IconButton>
        <IconButton label="Edit" variant="secondary">
          <Pencil />
        </IconButton>
        <IconButton label="Run" variant="primary">
          <Play />
        </IconButton>
        <IconButton label="Copy" size="sm">
          <Copy />
        </IconButton>
      </Demo>
    </Section>
  )
}

function FormsSection() {
  const [target, setTarget] = useState<string | null>('prod')
  const [evals, setEvals] = useState<string[]>(['correctness', 'faithfulness'])
  const [cache, setCache] = useState(true)
  return (
    <Section title="Forms">
      <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
        <Card>
          <CardBody className="flex flex-col gap-4">
            <Field label="Suite name" hint="Shown in run lists and CI summaries.">
              <Input placeholder="Support · smoke" defaultValue="Support · smoke" />
            </Field>
            <Field label="Slug" error="Slug is already used by another suite.">
              <Input defaultValue="support-smoke" className="font-mono" />
            </Field>
            <Field label="Description" optional>
              <Textarea placeholder="What does this suite check?" rows={3} />
            </Field>
            <Field label="Trials">
              <Select
                defaultValue="1"
                options={[
                  { value: '1', label: '1 trial' },
                  { value: '3', label: '3 trials' },
                  { value: '5', label: '5 trials · pass@k' },
                ]}
              />
            </Field>
          </CardBody>
        </Card>
        <Card>
          <CardBody className="flex flex-col gap-4">
            <Field label="Target">
              <Combobox
                value={target}
                onValueChange={setTarget}
                options={[
                  { value: 'prod', label: 'prod · gpt-4.1', description: 'openai · v12' },
                  { value: 'staging', label: 'staging · gpt-4.1', description: 'openai · v13' },
                  { value: 'claude', label: 'staging · claude', description: 'anthropic · v3' },
                  { value: 'http', label: 'support-api', description: 'http · v7' },
                ]}
              />
            </Field>
            <Field label="Evaluators">
              <Combobox
                multiple
                value={evals}
                onValueChange={setEvals}
                options={evaluators.map((e) => ({ value: e, label: e, icon: <Gauge /> }))}
              />
            </Field>
            <Field label="Search">
              <Input size="sm" placeholder="Filter examples…" trailing={<Kbd>/</Kbd>} />
            </Field>
            <div className="flex items-center justify-between">
              <label htmlFor="ks-cache" className="text-sm text-text">
                Cache target responses
              </label>
              <Switch id="ks-cache" checked={cache} onCheckedChange={setCache} />
            </div>
            <div className="flex items-center gap-2">
              <Checkbox id="ks-ff" defaultChecked />
              <label htmlFor="ks-ff" className="text-sm text-text">
                Fail fast on deterministic checks
              </label>
            </div>
            <RadioGroup defaultValue="last-pass" aria-label="Baseline policy">
              <RadioItem value="pinned" label="Pinned run" description="Always compare against one run." />
              <RadioItem value="last-pass" label="Last passing run" description="On the same target." />
              <RadioItem value="none" label="No baseline" />
            </RadioGroup>
          </CardBody>
        </Card>
      </div>
    </Section>
  )
}

function NavigationSection() {
  const [seg, setSeg] = useState<'all' | 'failed' | 'regressions'>('all')
  const [step, setStep] = useState(2)
  return (
    <Section title="Navigation">
      <Demo label="Breadcrumbs">
        <Breadcrumbs items={[{ label: 'Support Bot', href: '#' }, { label: 'Runs', href: '#' }, { label: 'run_4f2a1' }]} />
      </Demo>
      <Demo label="Tabs" className="block">
        <Tabs defaultValue="results">
          <TabsList>
            <TabsTrigger value="results">Results</TabsTrigger>
            <TabsTrigger value="charts">Charts</TabsTrigger>
            <TabsTrigger value="config">Configuration</TabsTrigger>
          </TabsList>
          <TabsContent value="results" className="text-sm text-text-2">
            1,000 results · 37 failed
          </TabsContent>
          <TabsContent value="charts" className="text-sm text-text-2">
            Charts panel
          </TabsContent>
          <TabsContent value="config" className="text-sm text-text-2">
            Configuration panel
          </TabsContent>
        </Tabs>
      </Demo>
      <Demo label="Segmented control">
        <SegmentedControl
          aria-label="Result filter"
          value={seg}
          onValueChange={setSeg}
          options={[
            { value: 'all', label: 'All' },
            { value: 'failed', label: 'Failed' },
            { value: 'regressions', label: 'Regressions' },
          ]}
        />
        <SegmentedControl
          size="sm"
          aria-label="Theme"
          value="light"
          onValueChange={() => {}}
          options={[
            { value: 'light', label: null, icon: <Sun />, ariaLabel: 'Light' },
            { value: 'dark', label: null, icon: <Moon />, ariaLabel: 'Dark' },
            { value: 'system', label: null, icon: <Monitor />, ariaLabel: 'System' },
          ]}
        />
      </Demo>
      <Demo label="Stepper" className="block">
        <Card className="px-4 py-3">
          <Stepper
            current={step}
            onStepClick={setStep}
            steps={[
              { id: 't', label: 'Target' },
              { id: 'd', label: 'Data' },
              { id: 'e', label: 'Evaluators' },
              { id: 'g', label: 'Gates' },
              { id: 'x', label: 'Execution' },
            ]}
          />
        </Card>
        <Card className="mt-3 w-64 p-2">
          <Stepper
            orientation="vertical"
            current={1}
            steps={[
              { id: 'p', label: 'Project', description: 'Support Bot' },
              { id: 't', label: 'Target', description: 'Connect your endpoint' },
              { id: 'd', label: 'Dataset', description: 'Import examples' },
            ]}
          />
        </Card>
      </Demo>
    </Section>
  )
}

function OverlaysSection() {
  const [palette, setPalette] = useState(false)
  return (
    <Section title="Overlays">
      <Demo label="Dialog · Drawer · Popover · Tooltip · Menu · Command palette · Toast">
        <Dialog
          trigger={<Button>Open dialog</Button>}
          title="Cancel run?"
          description="Workers stop after the current examples. Completed results are kept."
          size="sm"
          footer={
            <>
              <Button variant="ghost">Keep running</Button>
              <Button variant="danger">Cancel run</Button>
            </>
          }
        />
        <Drawer
          trigger={<Button>Open drawer</Button>}
          title="Example #128"
          description="run_4f2a1 · trial 1"
          footer={
            <>
              <Button variant="ghost">Send to review</Button>
              <Button variant="primary">Add to dataset</Button>
            </>
          }
        >
          <div className="flex flex-col gap-4">
            <KeyValue
              layout="grid"
              items={[
                { label: 'Status', value: <StatusPill status="passed" size="sm" /> },
                { label: 'Latency', value: '812 ms' },
                { label: 'Cost', value: '$0.0023' },
              ]}
            />
            <DiffViewer oldValue={expectedText} newValue={outputText} />
            <JsonViewer value={sampleJson} rootLabel="result" />
          </div>
        </Drawer>
        <Popover>
          <PopoverTrigger asChild>
            <Button>Popover</Button>
          </PopoverTrigger>
          <PopoverContent className="w-64">
            <p className="text-sm font-medium text-text">Gate</p>
            <p className="mt-1 font-mono text-xs text-text-2">mean(faithfulness) ≥ 0.85</p>
          </PopoverContent>
        </Popover>
        <Tooltip content="Re-run only failed examples" shortcut={['⇧', 'R']}>
          <Button variant="ghost">Hover me</Button>
        </Tooltip>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button iconRight={<MoreHorizontal />}>Actions</Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent>
            <DropdownMenuLabel>Run</DropdownMenuLabel>
            <DropdownMenuItem icon={<RefreshCw />} shortcut="⇧R">
              Re-run failed
            </DropdownMenuItem>
            <DropdownMenuItem icon={<Zap />}>Re-score</DropdownMenuItem>
            <DropdownMenuItem icon={<Download />}>Export JSONL</DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem icon={<Trash2 />} destructive>
              Delete run
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        <Button onClick={() => setPalette(true)}>Command palette</Button>
        <Button onClick={() => toast.success('Run finished', { description: 'support-smoke · 96.4% pass rate' })}>Success toast</Button>
        <Button onClick={() => toast.error('Gate failed', { description: 'mean(faithfulness) 0.81 < 0.85', action: { label: 'View', onClick: () => {} } })}>
          Error toast
        </Button>
        <Button
          onClick={() =>
            toast.promise(new Promise((r) => setTimeout(r, 1500)), { loading: 'Starting run…', success: 'Run queued', error: 'Failed' })
          }
        >
          Promise toast
        </Button>
      </Demo>
      <CommandPalette
        open={palette}
        onOpenChange={setPalette}
        groups={[
          {
            heading: 'Go to',
            items: [
              { id: 'runs', label: 'Runs', icon: <Play />, shortcut: ['G', 'R'], onSelect: () => {} },
              { id: 'targets', label: 'Targets', icon: <Target />, shortcut: ['G', 'T'], onSelect: () => {} },
            ],
          },
          { heading: 'Actions', items: [{ id: 'new', label: 'New dataset…', icon: <Plus />, onSelect: () => toast('New dataset') }] },
        ]}
      />
    </Section>
  )
}

function StatusSection() {
  return (
    <Section title="Status" description="Status colors are reserved and always paired with an icon and a label.">
      <Demo label="StatusPill">
        {(['passed', 'failed', 'warning', 'running', 'queued', 'skipped', 'cancelled', 'errored'] as const).map((s) => (
          <StatusPill key={s} status={s} />
        ))}
      </Demo>
      <Demo label="StatusPill · sm / icon-only">
        <StatusPill status="passed" size="sm" />
        <StatusPill status="failed" size="sm" />
        <StatusPill status="running" size="sm" iconOnly />
        <StatusPill status="passed" iconOnly />
      </Demo>
      <Demo label="ScoreChip (threshold 0.80)">
        <ScoreChip value={0.94} threshold={0.8} />
        <ScoreChip value={0.82} threshold={0.8} />
        <ScoreChip value={0.61} threshold={0.8} />
        <ScoreChip value={0.73} />
        <ScoreChip value={null} label="n/a" />
        <ScoreChip value={0.9} threshold={0.8} size="sm" />
        <ScoreChip value={null} passed label="correct" />
      </Demo>
      <Demo label="Badge">
        <Badge>refunds</Badge>
        <Badge variant="accent">baseline</Badge>
        <Badge variant="outline">v12</Badge>
        <Badge mono>
          <GitBranch />
          a1b2c3d
        </Badge>
        <Badge variant="pass">+2.4 pts</Badge>
        <Badge variant="fail">−3.1 pts</Badge>
        <Badge variant="warn">rate-limited 12%</Badge>
      </Demo>
      <Demo label="Live run indicator · Kbd">
        <LiveRunIndicatorView count={2} />
        <span className="flex gap-1">
          <Kbd>⌘</Kbd>
          <Kbd>K</Kbd>
        </span>
        <span className="flex items-center gap-1 text-xs text-text-muted">
          <Kbd>G</Kbd> then <Kbd>R</Kbd>
        </span>
      </Demo>
    </Section>
  )
}

function DataDisplaySection() {
  const [range, setRange] = useState<DateRange>(() => ({ ...defaultDatePresets[1]!.range(), preset: '7d' }))
  const [status, setStatus] = useState<string[]>(['failed'])
  const [tagF, setTagF] = useState<string[]>([])
  const [q, setQ] = useState('')
  return (
    <Section title="Data display">
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <StatTile label="Runs · last 7 days" icon={<Play />} value="128" delta={{ value: '+18', direction: 'up', context: 'vs prior week' }} />
        <StatTile
          label="Latest pass rate"
          icon={<Target />}
          value="94.2"
          unit="%"
          delta={{ value: '+2.4 pts', direction: 'up', context: 'vs previous run' }}
          trend={sparkData}
        />
        <StatTile label="p95 latency" icon={<Gauge />} value="1.42" unit="s" delta={{ value: '+180 ms', direction: 'up', upIsGood: false, context: 'vs baseline' }} />
        <StatTile label="Spend · 7 days" icon={<CircleDollarSign />} value="$41.87" delta={{ value: '0%', direction: 'flat', context: 'vs prior week' }} />
      </div>
      <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
        <Card>
          <CardHeader title="Run summary" description="KeyValue · rows" actions={<Badge variant="accent">baseline</Badge>} />
          <CardBody className="pt-0">
            <KeyValue
              items={[
                { label: 'Suite', value: 'support-smoke' },
                { label: 'Target', value: 'prod · gpt-4.1 (v12)' },
                { label: 'Dataset', value: 'support-golden · v7' },
                { label: 'Trigger', value: 'webhook · deploy preview' },
                { label: 'Git SHA', value: 'a1b2c3d', mono: true },
              ]}
            />
          </CardBody>
        </Card>
        <Card>
          <CardHeader title="Sparklines" description="Last 10 runs" />
          <CardBody className="flex flex-col gap-3 pt-1">
            {evaluators.slice(0, 4).map((e, i) => (
              <div key={e} className="flex items-center justify-between gap-4">
                <span className="text-sm text-text-2">{e}</span>
                <span className="flex items-center gap-3">
                  <Sparkline data={sparkData.map((v) => v - i * 0.04 + (i % 2 ? 0.02 : 0))} width={120} height={24} />
                  <ScoreChip value={sparkData.at(-1)! - i * 0.04} threshold={0.75} size="sm" />
                </span>
              </div>
            ))}
          </CardBody>
        </Card>
      </div>
      <Demo label="FilterBar + DateRangePicker" className="block">
        <FilterBar
          search={q}
          onSearchChange={setQ}
          searchPlaceholder="Search runs…"
          filters={[
            {
              id: 'status',
              label: 'Status',
              value: status,
              onChange: setStatus,
              options: [
                { value: 'passed', label: 'Passed', count: 812 },
                { value: 'failed', label: 'Failed', count: 37 },
                { value: 'running', label: 'Running', count: 2 },
              ],
            },
            { id: 'tag', label: 'Tag', value: tagF, onChange: setTagF, options: tags.map((t) => ({ value: t, label: t })) },
          ]}
          actions={<DateRangePicker value={range} onChange={setRange} />}
        />
      </Demo>
    </Section>
  )
}

const triggerIcon = { manual: Play, schedule: Calendar, webhook: Webhook, api: Bell } as const

function TableSection() {
  const runs = useMemo(() => makeRuns(10_000), [])
  const [selection, setSelection] = useState<RowSelectionState>({})
  const [active, setActive] = useState<FakeRun | null>(null)
  const columns = useMemo<ColumnDef<FakeRun, unknown>[]>(
    () => [
      selectionColumn<FakeRun>(),
      { accessorKey: 'status', header: 'Status', size: 120, cell: (c) => <StatusPill status={c.row.original.status} size="sm" /> },
      {
        accessorKey: 'suite',
        header: 'Suite',
        size: 190,
        meta: { grow: true },
        cell: (c) => (
          <span className="flex min-w-0 flex-col">
            <span className="truncate font-medium text-text">{c.row.original.suite}</span>
            <span className="truncate font-mono text-[11px] text-text-muted">{c.row.original.id}</span>
          </span>
        ),
      },
      { accessorKey: 'target', header: 'Target', size: 150, cell: (c) => <span className="text-text-2">{c.getValue() as string}</span> },
      {
        accessorKey: 'trigger',
        header: 'Trigger',
        size: 110,
        cell: (c) => {
          const Icon = triggerIcon[c.row.original.trigger]
          return (
            <span className="flex items-center gap-1.5 text-text-2">
              <Icon className="size-3.5 text-text-muted" />
              {c.row.original.trigger === 'api' ? 'API' : c.row.original.trigger}
            </span>
          )
        },
      },
      {
        accessorKey: 'passRate',
        header: 'Pass rate',
        size: 140,
        cell: (c) => {
          const v = c.getValue() as number
          return (
            <span className="flex items-center gap-2">
              <span className="tabular w-11 text-right">{(v * 100).toFixed(1)}%</span>
              <span className="h-1.5 w-16 overflow-hidden rounded-full bg-surface-2">
                <span className="block h-full rounded-full bg-chart-1" style={{ width: `${v * 100}%` }} />
              </span>
            </span>
          )
        },
      },
      { accessorKey: 'score', header: 'Correctness', size: 120, cell: (c) => <ScoreChip value={c.getValue() as number} threshold={0.75} size="sm" /> },
      { accessorKey: 'durationMs', header: 'Duration', size: 100, meta: { align: 'right', numeric: true }, cell: (c) => formatDuration(c.getValue() as number) },
      { accessorKey: 'cost', header: 'Cost', size: 90, meta: { align: 'right', numeric: true }, cell: (c) => formatUsd(c.getValue() as number) },
      {
        accessorKey: 'createdAt',
        header: 'Created',
        size: 100,
        meta: { align: 'right' },
        cell: (c) => <span className="text-text-muted">{formatRelative(new Date(c.getValue() as number), new Date(Date.UTC(2026, 10, 10)))}</span>,
      },
    ],
    [],
  )
  const selected = Object.keys(selection).length
  return (
    <Section title="Table" description="10,000 virtualized rows. Click the table, then use j/k, Enter and x. Headers sort.">
      <div className="flex h-8 items-center justify-between">
        <span className="text-sm text-text-2">{selected ? `${selected} selected` : `${runs.length.toLocaleString()} runs`}</span>
        {selected > 0 && (
          <span className="flex gap-2">
            <Button size="sm" variant="ghost" onClick={() => setSelection({})}>
              Clear
            </Button>
            <Button size="sm" variant="primary" disabled={selected < 2 || selected > 4}>
              Compare {selected}
            </Button>
          </span>
        )}
      </div>
      <Table
        aria-label="Runs"
        data={runs}
        columns={columns}
        getRowId={(r) => r.id}
        maxHeight={440}
        enableRowSelection
        rowSelection={selection}
        onRowSelectionChange={setSelection}
        onRowActivate={setActive}
        activeRowId={active?.id}
      />
      <Drawer open={!!active} onOpenChange={(o) => !o && setActive(null)} title={active?.suite ?? ''} description={active?.id} size="md">
        {active && (
          <KeyValue
            items={[
              { label: 'Status', value: <StatusPill status={active.status} size="sm" /> },
              { label: 'Target', value: active.target },
              { label: 'Pass rate', value: `${(active.passRate * 100).toFixed(1)}%` },
              { label: 'Duration', value: formatDuration(active.durationMs) },
              { label: 'Git SHA', value: active.sha, mono: true },
            ]}
          />
        )}
      </Drawer>
      <Demo label="Small table · empty · loading" className="grid grid-cols-1 items-start gap-3 lg:grid-cols-3">
        <Table aria-label="Small" data={runs.slice(0, 4)} columns={columns.slice(1, 4)} getRowId={(r) => r.id} />
        <Table
          aria-label="Empty"
          data={[]}
          columns={columns.slice(1, 4)}
          empty={<EmptyState compact illustration="search" title="No matching runs" description="Try clearing a filter." />}
        />
        <Table aria-label="Loading" data={[]} columns={columns.slice(1, 4)} loading />
      </Demo>
    </Section>
  )
}

function CodeSection() {
  return (
    <Section title="Code & diffs">
      <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
        <CodeBlock
          title="curl"
          language="bash"
          code={`curl -X POST https://airval.example.com/hooks/trg_8f2k \\
  -H "X-Airval-Signature: sha256=…" \\
  -H "Content-Type: application/json" \\
  -d '{"labels": {"git_sha": "a1b2c3d", "branch": "main"}}'`}
        />
        <CodeBlock
          language="yaml"
          showLineNumbers
          code={`suite: support-smoke
target: prod-gpt-4.1
dataset: support-golden@7
evaluators:
  - correctness: { threshold: 0.8, severity: critical }
  - faithfulness: { threshold: 0.85 }
gate: mean(faithfulness) >= 0.85 and count(critical_fail) == 0`}
        />
      </div>
      <JsonViewer value={sampleJson} rootLabel="result" />
      <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
        <DiffViewer oldValue={expectedText} newValue={outputText} />
        <DiffViewer
          mode="lines"
          oldLabel="v12"
          newLabel="v13"
          oldValue={'{\n  "model": "gpt-4.1",\n  "temperature": 0.2,\n  "max_tokens": 512\n}'}
          newValue={'{\n  "model": "gpt-4.1",\n  "temperature": 0,\n  "max_tokens": 512,\n  "seed": 7\n}'}
        />
      </div>
    </Section>
  )
}

function SchedulingSection() {
  const [cron, setCron] = useState('0 9 * * 1-5')
  const [bad, setBad] = useState('61 * * *')
  return (
    <Section title="Scheduling">
      <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
        <Field label="Schedule">
          <CronInput value={cron} onChange={setCron} timezone="Europe/Berlin" />
        </Field>
        <Field label="Invalid expression">
          <CronInput value={bad} onChange={setBad} />
        </Field>
      </div>
    </Section>
  )
}

function FeedbackSection() {
  return (
    <Section title="Feedback">
      <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-4">
        {(['runs', 'datasets', 'suites', 'targets', 'evaluators', 'review', 'chart', 'error'] as const).map((n) => (
          <Card key={n}>
            <EmptyState compact illustration={n} title={`No ${n} yet`} description="Every empty state has an illustration and a primary action." action={<Button size="sm">Primary action</Button>} />
          </Card>
        ))}
      </div>
      <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
        <Card className="p-4">
          <SkeletonText lines={4} />
        </Card>
        <Card className="flex items-center gap-3 p-4">
          <Skeleton className="size-10 rounded-full" />
          <div className="flex flex-1 flex-col gap-2">
            <Skeleton className="h-3 w-2/3" />
            <Skeleton className="h-3 w-1/3" />
          </div>
        </Card>
        <StatTile label="Loading tile" value="" loading />
      </div>
      <SkeletonTable rows={4} columns={5} />
    </Section>
  )
}

function ChartsSection() {
  return (
    <Section title="Charts" description="ECharts with token-generated themes. Every chart has a View as table toggle (top right).">
      <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
        <Histogram
          title="Score distribution"
          description="faithfulness · this run vs baseline"
          series={[
            { name: 'Baseline', values: scoresA },
            { name: 'This run', values: scoresB },
          ]}
          threshold={0.8}
        />
        <LatencyDistribution title="Latency" description="2,000 results" values={latencies} />
      </div>
      <TrendLine title="Pass rate by suite" description="Last 30 days" series={trendSeries} yDomain={[0.4, 1]} threshold={{ value: 0.8, label: 'gate 80%' }} />
      <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
        <TrendLine title="Mean correctness" description="Single series" series={[trendSeries[0]!]} format="percent" />
        <HorizontalBars title="Pass rate by tag" description="This run" items={passByTag} threshold={0.8} />
      </div>
      <Heatmap title="Evaluator × tag" description="Mean score" rows={evaluators} columns={tags} values={heatValues} />
      <div className="grid grid-cols-1 gap-3 lg:grid-cols-[2fr_1fr]">
        <Scatter title="Per-example score" description="Baseline vs candidate · correctness" points={scatterPoints} labelA="run_3e9 (baseline)" labelB="run_4f2a1" />
        <Card>
          <CardHeader title="Pairwise win rate" description="LLM judge, position-swapped" />
          <CardBody className="flex flex-col gap-6">
            <WinRateBar wins={118} ties={41} losses={61} labelA="Candidate" labelB="Baseline" />
            <WinRateBar wins={52} ties={12} losses={80} labelA="Claude" labelB="GPT-4.1" />
          </CardBody>
        </Card>
      </div>
      <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
        <Histogram title="Empty chart" series={[{ name: 'Scores', values: [] }]} empty={<EmptyState compact illustration="chart" title="No scores yet" />} />
        <Histogram title="Loading chart" series={[{ name: 'Scores', values: [] }]} loading />
      </div>
    </Section>
  )
}

/** Dev-only showcase of every primitive and chart, for visual review in both themes. */
export function KitchenSink() {
  const { preference, setPreference } = useTheme()
  return (
    <div className="min-h-dvh bg-bg">
      <header className="sticky top-0 z-20 flex h-14 items-center gap-3 border-b border-border bg-bg/85 px-4 backdrop-blur-sm sm:px-6">
        <IconButton label="Back to app" onClick={() => history.back()}>
          <ArrowLeft />
        </IconButton>
        <Link to="/" className="text-sm font-semibold text-text focus-ring rounded-sm">
          airval
        </Link>
        <span className="text-sm text-text-muted">/ kitchen sink</span>
        <div className="ml-auto">
          <SegmentedControl
            size="sm"
            aria-label="Theme"
            value={preference}
            onValueChange={(v) => setPreference(v as ThemePreference)}
            options={[
              { value: 'light', label: 'Light', icon: <Sun /> },
              { value: 'dark', label: 'Dark', icon: <Moon /> },
              { value: 'system', label: 'System', icon: <Monitor /> },
            ]}
          />
        </div>
      </header>
      <div className="mx-auto flex max-w-[1400px] gap-10 px-4 py-8 sm:px-6">
        <nav aria-label="Sections" className="sticky top-22 hidden h-fit w-44 shrink-0 flex-col gap-0.5 lg:flex">
          {sections.map((s) => (
            <a key={s} href={`#${slug(s)}`} className="rounded-sm px-2 py-1 text-sm text-text-2 transition-colors hover:bg-surface-hover hover:text-text focus-ring">
              {s}
            </a>
          ))}
        </nav>
        <main className="flex min-w-0 flex-1 flex-col gap-10">
          <PageHeader title="Kitchen sink" description="Every UI primitive and chart with fake data. Use it to review visual quality in light and dark." />
          <TokensSection />
          <TypographySection />
          <ButtonsSection />
          <FormsSection />
          <NavigationSection />
          <OverlaysSection />
          <StatusSection />
          <DataDisplaySection />
          <TableSection />
          <CodeSection />
          <SchedulingSection />
          <FeedbackSection />
          <ChartsSection />
        </main>
      </div>
    </div>
  )
}
